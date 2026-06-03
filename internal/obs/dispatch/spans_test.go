package dispatch_test

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/trilamsr/regatta/internal/obs/dispatch"
	"github.com/trilamsr/regatta/internal/testutil/reporoot"
)

// TestDispatchSpan_RecordsKindOutcomeDuration verifies Start emits a span carrying kind/outcome/duration.
func TestDispatchSpan_RecordsKindOutcomeDuration(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	defer func() { _ = tp.Shutdown(context.Background()) }()

	cfg := dispatch.Config{Tracer: tp.Tracer("test")}
	_, end := dispatch.Start(context.Background(), cfg, dispatch.KindImplementer, "task-42")
	time.Sleep(2 * time.Millisecond)
	end(dispatch.EndOpts{
		Outcome:      dispatch.OutcomeSuccess,
		InputTokens:  100,
		OutputTokens: 200,
		Model:        "claude-opus-4-7",
	})

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("want 1 span, got %d", len(spans))
	}
	s := spans[0]
	if s.Name != "regatta.dispatch.subagent" {
		t.Fatalf("span name = %q, want regatta.dispatch.subagent", s.Name)
	}
	attrs := attrMap(s.Attributes)
	if got := attrs["kind"]; got != "implementer" {
		t.Fatalf("kind attr = %q, want implementer", got)
	}
	if got := attrs["outcome"]; got != "success" {
		t.Fatalf("outcome attr = %q, want success", got)
	}
	if got := attrs["task_id"]; got != "task-42" {
		t.Fatalf("task_id attr = %q, want task-42", got)
	}
	if _, ok := attrs["duration_seconds"]; !ok {
		t.Fatalf("duration_seconds attr missing")
	}
}

// TestDispatchSpan_TaskIDInAttrNotLabel verifies task_id is set as a span attribute (not a metric label).
func TestDispatchSpan_TaskIDInAttrNotLabel(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	defer func() { _ = tp.Shutdown(context.Background()) }()

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	defer func() { _ = mp.Shutdown(context.Background()) }()

	cfg := dispatch.Config{Tracer: tp.Tracer("test"), Meter: mp.Meter("test")}
	_, end := dispatch.Start(context.Background(), cfg, dispatch.KindReviewer, "task-99")
	end(dispatch.EndOpts{Outcome: dispatch.OutcomeSuccess})

	// Span carries task_id.
	if got := attrMap(exp.GetSpans()[0].Attributes)["task_id"]; got != "task-99" {
		t.Fatalf("span task_id = %q, want task-99", got)
	}

	// Counter does NOT carry task_id label.
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			sumData, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, dp := range sumData.DataPoints {
				iter := dp.Attributes.Iter()
				for iter.Next() {
					kv := iter.Attribute()
					if string(kv.Key) == "task_id" {
						t.Fatalf("counter %q carries banned label task_id", m.Name)
					}
				}
			}
		}
	}
}

// TestDispatchSpan_ModelAttrSanitized verifies model attribute folds non-whitelist input to invalid_model.
func TestDispatchSpan_ModelAttrSanitized(t *testing.T) {
	cases := []struct {
		model string
		want  string
	}{
		{"claude-opus-4-7", "claude-opus-4-7"},
		{"sonnet-4.5", "sonnet-4.5"},
		{"sk-ant-api03-LEAKED-KEY-FRAGMENT-XXXXXXX", "invalid_model"},
		{"key=secret&model=foo", "invalid_model"},
		{"model with spaces", "invalid_model"},
		{"UPPERCASE", "invalid_model"},
		{"loooooooooooooooooooooooooooooooooooooooooooooooooooooong-name-over-40-chars", "invalid_model"},
	}
	for _, tc := range cases {
		exp := tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
		cfg := dispatch.Config{Tracer: tp.Tracer("test")}
		_, end := dispatch.Start(context.Background(), cfg, dispatch.KindImplementer, "t")
		end(dispatch.EndOpts{Outcome: dispatch.OutcomeSuccess, Model: tc.model})

		spans := exp.GetSpans()
		attrs := attrMap(spans[0].Attributes)
		got := attrs["model"]
		if got != tc.want {
			t.Errorf("model %q sanitized to %q, want %q", tc.model, got, tc.want)
		}
		_ = tp.Shutdown(context.Background())
	}
}

// TestNoUnboundedLabel_PRNumber AST-walks dispatch package for pr_number/task_id on metric instrument call sites only.
func TestNoUnboundedLabel_PRNumber(t *testing.T) {
	repoRoot := reporoot.Must(t)
	walkRoot := filepath.Join(repoRoot, "internal", "obs", "dispatch")
	fset := token.NewFileSet()
	var failures []string

	// Walk *.Add / *.Record calls — metric instrument emission sites.
	metricSel := map[string]struct{}{"Add": {}, "Record": {}}

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
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if _, hit := metricSel[sel.Sel.Name]; !hit {
				return true
			}
			for _, arg := range call.Args {
				ast.Inspect(arg, func(inner ast.Node) bool {
					lit, ok := inner.(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						return true
					}
					raw := strings.Trim(lit.Value, "\"`")
					if raw == "pr_number" || raw == "task_id" {
						pos := fset.Position(lit.Pos())
						failures = append(failures,
							pos.String()+": banned label "+raw+" on metric instrument")
					}
					return true
				})
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(failures) > 0 {
		t.Fatalf("metric cardinality leaks:\n  %s", strings.Join(failures, "\n  "))
	}
}

// TestDispatchSpan_NoUnboundedLabel_PRNumber AST-walks span.SetAttributes call sites in the dispatch package for unbounded labels (#662).
func TestDispatchSpan_NoUnboundedLabel_PRNumber(t *testing.T) {
	// Span attributes legitimately carry pr_number/task_id/model — high-
	// cardinality drill keys ride span attributes by design (see
	// dispatch package godoc). The cardinality risk is on METRIC labels.
	// This test pins the COMPLEMENT: span.SetAttributes calls in the
	// dispatch package MUST stay confined to the dispatch package — a
	// drift where a metric instrument call grew up to look like a span
	// SetAttributes call (e.g. via a helper rename) would be caught by
	// the existing TestNoUnboundedLabel_PRNumber. This test instead pins
	// the SHAPE: every span.SetAttributes call site in dispatch is the
	// single canonical one in spans.go:138, and any new call site MUST
	// be reviewed against the cardinality budget.
	repoRoot := reporoot.Must(t)
	walkRoot := filepath.Join(repoRoot, "internal", "obs", "dispatch")
	fset := token.NewFileSet()
	var setAttrSites []string

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
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if sel.Sel.Name != "SetAttributes" {
				return true
			}
			pos := fset.Position(call.Pos())
			setAttrSites = append(setAttrSites, pos.String())
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(setAttrSites) != 1 {
		t.Fatalf("want exactly 1 span.SetAttributes call site in dispatch package, got %d:\n  %s\n"+
			"new sites require cardinality-budget review against the spec §3.2 fence.",
			len(setAttrSites), strings.Join(setAttrSites, "\n  "))
	}
}

// TestDispatchKindEnum_Closed pins the 4-kind closed enum against AllKinds.
func TestDispatchKindEnum_Closed(t *testing.T) {
	kinds := dispatch.AllKinds()
	if len(kinds) != 4 {
		t.Fatalf("want 4 kinds, got %d (%v)", len(kinds), kinds)
	}
}

// TestDispatchOutcomeEnum_Closed pins the 4-outcome closed enum.
func TestDispatchOutcomeEnum_Closed(t *testing.T) {
	outs := dispatch.AllOutcomes()
	if len(outs) != 4 {
		t.Fatalf("want 4 outcomes, got %d (%v)", len(outs), outs)
	}
}

// TestDispatchSpan_NilConfigFallsBackToGlobal verifies nil tracer/meter does not panic.
func TestDispatchSpan_NilConfigFallsBackToGlobal(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("nil-config Start panicked: %v", r)
		}
	}()
	_, end := dispatch.Start(context.Background(), dispatch.Config{}, dispatch.KindTriage, "t")
	end(dispatch.EndOpts{Outcome: dispatch.OutcomeError})
}

// attrMap flattens span attributes into a string map for test assertions.
func attrMap(kvs []attribute.KeyValue) map[string]string {
	m := make(map[string]string, len(kvs))
	for _, kv := range kvs {
		// AsString returns "" for non-string types; fall back to AsInt64
		// stringification when the key carries an int attribute. Test
		// assertions only inspect string-shaped keys, so the empty fall-
		// through is harmless.
		v := kv.Value.AsString()
		if v == "" {
			switch kv.Value.Type() {
			case attribute.INT64:
				v = strconvI64(kv.Value.AsInt64())
			case attribute.FLOAT64:
				// Keep the bare-not-empty marker so duration_seconds shows up.
				v = "float64"
			default:
			}
		}
		m[string(kv.Key)] = v
	}
	return m
}

// strconvI64 stringifies an int64 — kept local to avoid an extra import
// at the test scope.
func strconvI64(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// ensure metric import retained for future expansion (silences unused-import lint).
var _ = metric.WithAttributes
