package obs_test

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/testutil/reporoot"
)

// TestFailureClassifier_RegexFastPath_P95Under5ms sweeps the corpus and asserts P95 classify latency under 5ms.
func TestFailureClassifier_RegexFastPath_P95Under5ms(t *testing.T) {
	corpus := loadCorpus(t)
	if len(corpus) < 8 {
		t.Fatalf("corpus too small: %d entries", len(corpus))
	}
	for _, c := range corpus {
		obs.Classify(c)
	}
	const iterations = 200
	durations := make([]time.Duration, 0, iterations*len(corpus))
	for i := 0; i < iterations; i++ {
		for _, c := range corpus {
			start := time.Now()
			obs.Classify(c)
			durations = append(durations, time.Since(start))
		}
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p95 := durations[int(float64(len(durations))*0.95)]
	if p95 > 5*time.Millisecond {
		t.Fatalf("P95 classify latency = %v, want < 5ms", p95)
	}
	t.Logf("P95 = %v over %d classifications", p95, len(durations))
}

// TestFailureClassifier_UnknownBucketFallthrough verifies unparseable logs route to TaxUnknown.
func TestFailureClassifier_UnknownBucketFallthrough(t *testing.T) {
	cases := []string{
		"",
		"just some text with no signal",
		"INFO: build started\nINFO: build done\n",
	}
	for _, c := range cases {
		if got := obs.Classify(c); got != obs.TaxUnknown {
			t.Errorf("Classify(%q) = %v, want unknown", c, got)
		}
	}
}

// TestFailureClassifier_KnownBucketCoverage maps representative inputs to their expected bucket.
func TestFailureClassifier_KnownBucketCoverage(t *testing.T) {
	cases := []struct {
		log  string
		want obs.Taxonomy
	}{
		{"context deadline exceeded", obs.TaxTimeout},
		{"merge conflict in foo/bar.go", obs.TaxConflict},
		{"gate_reject: CELDecider policy block", obs.TaxGateReject},
		{"reviewer block: changes requested", obs.TaxReviewerBlock},
		{"panic: runtime error", obs.TaxCrash},
		{"FAIL  github.com/foo/bar  exit status 1", obs.TaxCIFail},
		{"cost cap reached for tenant", obs.TaxCostCap},
	}
	for _, tc := range cases {
		if got := obs.Classify(tc.log); got != tc.want {
			t.Errorf("Classify(%q) = %v, want %v", tc.log, got, tc.want)
		}
	}
}

// TestFailureTaxonomyEnum_Closed pins the 8-bucket closed enum.
func TestFailureTaxonomyEnum_Closed(t *testing.T) {
	tax := obs.AllTaxonomies()
	if len(tax) != 8 {
		t.Fatalf("want 8 taxonomies, got %d (%v)", len(tax), tax)
	}
	found := false
	for _, x := range tax {
		if x == obs.TaxUnknown {
			found = true
		}
	}
	if !found {
		t.Fatal("TaxUnknown missing from AllTaxonomies()")
	}
}

// TestFailtaxonomy_NoUnboundedLabel_PRNumber AST-walks the failtaxonomy prod file for pr_number literals.
func TestFailtaxonomy_NoUnboundedLabel_PRNumber(t *testing.T) {
	repoRoot := reporoot.Must(t)
	target := filepath.Join(repoRoot, "internal", "obs", "failtaxonomy_classifier.go")
	fset := token.NewFileSet()
	var failures []string

	f, err := parser.ParseFile(fset, target, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		raw := strings.Trim(lit.Value, "\"`")
		if raw == "pr_number" {
			pos := fset.Position(lit.Pos())
			failures = append(failures, pos.String()+": banned pr_number literal in metric package")
		}
		return true
	})
	if len(failures) > 0 {
		t.Fatalf("cardinality leaks:\n  %s", strings.Join(failures, "\n  "))
	}
}

// TestFailureClassifier_TailWindowOnly verifies the 8KB tail bound bounds scan latency on large logs.
func TestFailureClassifier_TailWindowOnly(t *testing.T) {
	pad := strings.Repeat("INFO: ok\n", 2000)
	sig := pad + "panic: runtime error: nil pointer\n"
	if got := obs.Classify(sig); got != obs.TaxCrash {
		t.Fatalf("trailing signature missed: got %v", got)
	}
	sigBuried := "panic: runtime error\n" + strings.Repeat("INFO: ok\n", 2000)
	if got := obs.Classify(sigBuried); got == obs.TaxCrash {
		t.Fatal("buried-signature outside tail window should not match — design bug")
	}
}

// BenchmarkClassifier_RegexClassify benchmarks Classify against a realistic CI failure log.
func BenchmarkClassifier_RegexClassify(b *testing.B) {
	body := strings.Repeat("2026-06-02T12:00:00Z INFO build step ok\n", 100) +
		"FAIL  github.com/trilamsr/regatta/internal/foo  exit status 1\n"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = obs.Classify(body)
	}
}

// BenchmarkClassifier_8KBTailBoundary stresses the 8KB tail-window truncation path with a 16KB log (#663).
func BenchmarkClassifier_8KBTailBoundary(b *testing.B) {
	// 16KB log — twice the 8KB tailBytes constant — exercises the
	// truncation slice (logTail = logTail[len-tailBytes:]) on every
	// iteration. Signature sits in the tail half so the regex sweep
	// also fires; budget claim is P95 < 5ms even when truncation
	// activates.
	header := strings.Repeat("2026-06-02T12:00:00Z INFO build step ok with a moderately verbose line to push bytes\n", 200)
	tail := strings.Repeat("noise line\n", 50) +
		"panic: runtime error: invalid memory address or nil pointer dereference\n"
	body := header + tail
	if len(body) < 16*1024 {
		body += strings.Repeat("pad\n", (16*1024-len(body))/4+1)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got := obs.Classify(body)
		if got != obs.TaxCrash {
			b.Fatalf("classify on 16KB log = %v, want crash", got)
		}
	}
}

// TestFailtaxonomyRecord_EmitsCounterWithoutPanic verifies Record returns the classified bucket and does not panic on nil meter.
func TestFailtaxonomyRecord_EmitsCounterWithoutPanic(t *testing.T) {
	got := obs.FailtaxonomyRecord(context.Background(), obs.FailtaxonomyConfig{},
		"context deadline exceeded")
	if got != obs.TaxTimeout {
		t.Fatalf("Record bucket = %v, want timeout", got)
	}
}

// loadCorpus returns the inline classifier corpus — keeps the test self-contained.
func loadCorpus(t *testing.T) []string {
	t.Helper()
	return []string{
		"FAIL  github.com/trilamsr/regatta/internal/foo  exit status 1",
		"context deadline exceeded after 120s",
		"merge conflict in internal/foo/bar.go",
		"panic: runtime error: invalid memory address",
		"gate_reject: CELDecider verdict false on rule policy.lint",
		"reviewer block: changes requested by reviewer subagent",
		"cost cap reached for tenant t-1; refusing dispatch",
		"runtime error: segmentation fault\nkilled by signal 11",
		"./foo.go:42: undefined: bar",
		"lint fail: golangci-lint exit status 1",
	}
}
