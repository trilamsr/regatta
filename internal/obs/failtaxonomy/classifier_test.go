package failtaxonomy_test

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/obs/failtaxonomy"
	"github.com/trilamsr/regatta/internal/testutil/reporoot"
)

// TestFailureClassifier_RegexFastPath_P95Under5ms sweeps the corpus and asserts P95 classify latency under 5ms.
func TestFailureClassifier_RegexFastPath_P95Under5ms(t *testing.T) {
	corpus := loadCorpus(t)
	if len(corpus) < 8 {
		t.Fatalf("corpus too small: %d entries", len(corpus))
	}
	for _, c := range corpus {
		failtaxonomy.Classify(c)
	}
	const iterations = 200
	durations := make([]time.Duration, 0, iterations*len(corpus))
	for i := 0; i < iterations; i++ {
		for _, c := range corpus {
			start := time.Now()
			failtaxonomy.Classify(c)
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
		if got := failtaxonomy.Classify(c); got != failtaxonomy.TaxUnknown {
			t.Errorf("Classify(%q) = %v, want unknown", c, got)
		}
	}
}

// TestFailureClassifier_KnownBucketCoverage maps representative inputs to their expected bucket.
func TestFailureClassifier_KnownBucketCoverage(t *testing.T) {
	cases := []struct {
		log  string
		want failtaxonomy.Taxonomy
	}{
		{"context deadline exceeded", failtaxonomy.TaxTimeout},
		{"merge conflict in foo/bar.go", failtaxonomy.TaxConflict},
		{"gate_reject: CELDecider policy block", failtaxonomy.TaxGateReject},
		{"reviewer block: changes requested", failtaxonomy.TaxReviewerBlock},
		{"panic: runtime error", failtaxonomy.TaxCrash},
		{"FAIL  github.com/foo/bar  exit status 1", failtaxonomy.TaxCIFail},
		{"cost cap reached for tenant", failtaxonomy.TaxCostCap},
	}
	for _, tc := range cases {
		if got := failtaxonomy.Classify(tc.log); got != tc.want {
			t.Errorf("Classify(%q) = %v, want %v", tc.log, got, tc.want)
		}
	}
}

// TestFailureTaxonomyEnum_Closed pins the 8-bucket closed enum.
func TestFailureTaxonomyEnum_Closed(t *testing.T) {
	tax := failtaxonomy.AllTaxonomies()
	if len(tax) != 8 {
		t.Fatalf("want 8 taxonomies, got %d (%v)", len(tax), tax)
	}
	found := false
	for _, x := range tax {
		if x == failtaxonomy.TaxUnknown {
			found = true
		}
	}
	if !found {
		t.Fatal("TaxUnknown missing from AllTaxonomies()")
	}
}

// TestNoUnboundedLabel_PRNumber AST-walks the package for pr_number string literals in production code.
func TestNoUnboundedLabel_PRNumber(t *testing.T) {
	repoRoot := reporoot.Must(t)
	walkRoot := filepath.Join(repoRoot, "internal", "obs", "failtaxonomy")
	fset := token.NewFileSet()
	var failures []string

	err := filepath.WalkDir(walkRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
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
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(failures) > 0 {
		t.Fatalf("cardinality leaks:\n  %s", strings.Join(failures, "\n  "))
	}
}

// TestFailureClassifier_TailWindowOnly verifies the 8KB tail bound bounds scan latency on large logs.
func TestFailureClassifier_TailWindowOnly(t *testing.T) {
	pad := strings.Repeat("INFO: ok\n", 2000)
	sig := pad + "panic: runtime error: nil pointer\n"
	if got := failtaxonomy.Classify(sig); got != failtaxonomy.TaxCrash {
		t.Fatalf("trailing signature missed: got %v", got)
	}
	sigBuried := "panic: runtime error\n" + strings.Repeat("INFO: ok\n", 2000)
	if got := failtaxonomy.Classify(sigBuried); got == failtaxonomy.TaxCrash {
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
		_ = failtaxonomy.Classify(body)
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
		got := failtaxonomy.Classify(body)
		if got != failtaxonomy.TaxCrash {
			b.Fatalf("classify on 16KB log = %v, want crash", got)
		}
	}
}

// TestRecord_EmitsCounterWithoutPanic verifies Record returns the classified bucket and does not panic on nil meter.
func TestRecord_EmitsCounterWithoutPanic(t *testing.T) {
	got := failtaxonomy.Record(context.Background(), failtaxonomy.Config{},
		"context deadline exceeded")
	if got != failtaxonomy.TaxTimeout {
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
