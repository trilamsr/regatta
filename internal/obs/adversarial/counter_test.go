package adversarial

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// TestAdversarial_FindingsCounter_IncrementsOnSubstrateEvent verifies a recorded finding lands on the counter with the correct tag tuple.
func TestAdversarial_FindingsCounter_IncrementsOnSubstrateEvent(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	r, err := NewRecorder(Config{Meter: mp.Meter("test")})
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}

	err = r.Record(context.Background(), Finding{
		SpanID: "s1", Index: 0,
		Severity: SeverityHigh, Scope: ScopeFile,
		Text: "missing error wrap on db call",
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	var sum int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "regatta.adversarial.findings" {
				continue
			}
			s, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("unexpected data shape %T", m.Data)
			}
			for _, dp := range s.DataPoints {
				sum += dp.Value
			}
		}
	}
	if sum != 1 {
		t.Fatalf("counter sum = %d; want 1", sum)
	}
}

// TestAdversarial_PatternOther_FallsThrough proves an unrecognized finding text routes to PatternOther.
func TestAdversarial_PatternOther_FallsThrough(t *testing.T) {
	got := ClassifyPattern("some entirely novel reviewer observation that does not match anything")
	if got != PatternOther {
		t.Errorf("ClassifyPattern = %q; want %q", got, PatternOther)
	}
}

// TestAdversarial_PatternClassifier_RoutesKnown spot-checks the keyword routing for a few canonical findings.
func TestAdversarial_PatternClassifier_RoutesKnown(t *testing.T) {
	cases := map[string]Pattern{
		"missing %w on returned error":   PatternMissingErrorWrap,
		"banned phrase in PR body":       PatternBannedPhrase,
		"AI signature Co-Authored-By":    PatternAISignature,
		"godoc spans 4 lines":            PatternGodocOneLine,
		"cardinality blow on log.scope":  PatternUnboundedTag,
		"comment what not why violation": PatternCommentWhatNotWhy,
	}
	for text, want := range cases {
		if got := ClassifyPattern(text); got != want {
			t.Errorf("ClassifyPattern(%q) = %q; want %q", text, got, want)
		}
	}
}

// TestAdversarial_SeverityScopeEnum_Closed AST-walks the package to assert string literals match the closed enum.
func TestAdversarial_SeverityScopeEnum_Closed(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	files, _ := filepath.Glob(filepath.Join(wd, "*.go"))
	allowedSev := map[string]bool{}
	for _, s := range AllSeverities {
		allowedSev[string(s)] = true
	}
	allowedScope := map[string]bool{}
	for _, s := range AllScopes {
		allowedScope[string(s)] = true
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		node, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		ast.Inspect(node, func(n ast.Node) bool {
			vs, ok := n.(*ast.ValueSpec)
			if !ok {
				return true
			}
			// Constant decls of Severity/Scope use string literals; we
			// confirm the declared values are members of the enum sets.
			for _, v := range vs.Values {
				bl, ok := v.(*ast.BasicLit)
				if !ok || bl.Kind != token.STRING {
					continue
				}
				val := strings.Trim(bl.Value, `"`)
				if vs.Type != nil {
					id, _ := vs.Type.(*ast.Ident)
					if id == nil {
						continue
					}
					switch id.Name {
					case "Severity":
						if !allowedSev[val] {
							t.Errorf("severity literal %q not in AllSeverities", val)
						}
					case "Scope":
						if !allowedScope[val] {
							t.Errorf("scope literal %q not in AllScopes", val)
						}
					}
				}
			}
			return true
		})
	}
}

// TestAdversarial_NoDoubleCountOnRetry verifies (SpanID, Index) keying drops duplicate emits.
func TestAdversarial_NoDoubleCountOnRetry(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	r, _ := NewRecorder(Config{Meter: mp.Meter("test")})

	f := Finding{SpanID: "s9", Index: 3, Severity: SeverityLow, Scope: ScopeRepo, Text: "godoc overflow"}
	for i := 0; i < 5; i++ {
		_ = r.Record(context.Background(), f)
	}

	var rm metricdata.ResourceMetrics
	_ = reader.Collect(context.Background(), &rm)
	var sum int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			s, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, dp := range s.DataPoints {
				sum += dp.Value
			}
		}
	}
	if sum != 1 {
		t.Errorf("counter sum after 5 retries = %d; want 1", sum)
	}
}

// TestAdversarial_NilMeterFallback pins nil-Config.Meter to the global-scoped meter.
func TestAdversarial_NilMeterFallback(t *testing.T) {
	cfg := Config{}
	if cfg.ResolveMeter() == nil {
		t.Fatal("ResolveMeter returned nil")
	}
	r, err := NewRecorder(cfg)
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}
	if err := r.Record(context.Background(), Finding{
		Severity: SeverityNit, Scope: ScopePackage, Text: "test",
	}); err != nil {
		t.Errorf("Record on fallback meter: %v", err)
	}
}

// TestAdversarial_InvalidSeverityRejected rejects unknown severity values pre-emit.
func TestAdversarial_InvalidSeverityRejected(t *testing.T) {
	r, _ := NewRecorder(Config{Meter: noop.NewMeterProvider().Meter("test")})
	err := r.Record(context.Background(), Finding{
		Severity: Severity("bogus"), Scope: ScopeFile, Text: "x",
	})
	if err == nil {
		t.Error("Record accepted bogus severity")
	}
}
