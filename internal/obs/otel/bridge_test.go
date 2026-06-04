package otel_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/trilamsr/regatta/internal/obs"
	obsotel "github.com/trilamsr/regatta/internal/obs/otel"
	"github.com/trilamsr/regatta/internal/obstest"
)

// findAttr walks an OTel log record and returns the value for key, or
// the zero log.Value if absent. Test-local helper — keeps every attr
// assertion a one-liner so the test bodies stay focused on the claim.
func findAttr(r sdklog.Record, key string) (log.Value, bool) {
	var v log.Value
	var found bool
	r.WalkAttributes(func(kv log.KeyValue) bool {
		if kv.Key == key {
			v = kv.Value
			found = true
			return false
		}
		return true
	})
	return v, found
}

// textBuf is an io.Writer that serialises Write under a mutex so
// concurrent TextHandler emissions in -race tests do not corrupt the
// underlying bytes.Buffer.
type textBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *textBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *textBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *textBuf) contains(s string) bool { return strings.Contains(b.String(), s) }

// memProcessor captures every log.Record emitted through the SDK log
// pipeline. Single source of truth for asserting the OTel leg of the
// bridge — keeps the test free of network exporters.
type memProcessor struct {
	mu      sync.Mutex
	records []sdklog.Record
}

func (p *memProcessor) Enabled(context.Context, sdklog.EnabledParameters) bool { return true }

func (p *memProcessor) OnEmit(_ context.Context, r *sdklog.Record) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.records = append(p.records, r.Clone())
	return nil
}

func (p *memProcessor) Shutdown(context.Context) error   { return nil }
func (p *memProcessor) ForceFlush(context.Context) error { return nil }

func (p *memProcessor) snapshot() []sdklog.Record {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]sdklog.Record, len(p.records))
	copy(out, p.records)
	return out
}

// newTestProvider builds a LoggerProvider wired to an in-memory
// processor and returns both so tests can assert on captured records.
func newTestProvider() (*sdklog.LoggerProvider, *memProcessor) {
	p := &memProcessor{}
	lp := sdklog.NewLoggerProvider(sdklog.WithProcessor(p))
	return lp, p
}

func TestBridge_RecordsFanToBoth(t *testing.T) {
	t.Parallel()

	primary := obstest.New()
	lp, mem := newTestProvider()
	t.Cleanup(func() { _ = lp.Shutdown(context.Background()) })

	bridge := obsotel.NewBridgeHandler(primary, "regatta-test", lp)
	lg := slog.New(bridge)

	lg.Info(string(obs.EventTickStarted), string(obs.KeyProgramID), "prog-1")

	got := primary.Records()
	if len(got) != 1 {
		t.Fatalf("primary handler: want 1 record, got %d", len(got))
	}
	if got[0].Message != string(obs.EventTickStarted) {
		t.Fatalf("primary message mismatch: want %q got %q", obs.EventTickStarted, got[0].Message)
	}

	otelRecs := mem.snapshot()
	if len(otelRecs) != 1 {
		t.Fatalf("OTel leg: want 1 record, got %d", len(otelRecs))
	}
	if body := otelRecs[0].Body().AsString(); body != string(obs.EventTickStarted) {
		t.Fatalf("OTel body mismatch: want %q got %q", obs.EventTickStarted, body)
	}

	var sawProgramID bool
	otelRecs[0].WalkAttributes(func(kv log.KeyValue) bool {
		if kv.Key == string(obs.KeyProgramID) && kv.Value.AsString() == "prog-1" {
			sawProgramID = true
			return false
		}
		return true
	})
	if !sawProgramID {
		t.Fatalf("OTel leg did not carry program_id=prog-1 attribute")
	}
}

func TestBridge_AttachesTraceID(t *testing.T) {
	t.Parallel()

	primary := obstest.New()
	lp, mem := newTestProvider()
	t.Cleanup(func() { _ = lp.Shutdown(context.Background()) })

	tp := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("bridge-test")

	bridge := obsotel.NewBridgeHandler(primary, "regatta-test", lp)
	lg := slog.New(bridge)

	ctx, span := tracer.Start(context.Background(), "unit")
	wantTrace := span.SpanContext().TraceID()
	wantSpan := span.SpanContext().SpanID()
	lg.InfoContext(ctx, string(obs.EventTickStarted))
	span.End()

	recs := mem.snapshot()
	if len(recs) != 1 {
		t.Fatalf("want 1 OTel record, got %d", len(recs))
	}
	if got := recs[0].TraceID(); got != wantTrace {
		t.Fatalf("trace_id: want %s got %s", wantTrace, got)
	}
	if got := recs[0].SpanID(); got != wantSpan {
		t.Fatalf("span_id: want %s got %s", wantSpan, got)
	}
}

func TestBridge_Concurrent_RaceClean(t *testing.T) {
	t.Parallel()

	const goroutines = 1000

	primary := obstest.New()
	lp, mem := newTestProvider()
	t.Cleanup(func() { _ = lp.Shutdown(context.Background()) })

	bridge := obsotel.NewBridgeHandler(primary, "regatta-test", lp)
	lg := slog.New(bridge)

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			lg.Info(string(obs.EventTickCompleted))
		}()
	}
	wg.Wait()

	if got := len(primary.Records()); got != goroutines {
		t.Fatalf("primary leg: want %d records, got %d", goroutines, got)
	}
	if got := len(mem.snapshot()); got != goroutines {
		t.Fatalf("OTel leg: want %d records, got %d", goroutines, got)
	}
}

func TestBridge_PreservesObsEventNames(t *testing.T) {
	t.Parallel()

	primary := obstest.New()
	lp, mem := newTestProvider()
	t.Cleanup(func() { _ = lp.Shutdown(context.Background()) })

	bridge := obsotel.NewBridgeHandler(primary, "regatta-test", lp)
	lg := slog.New(bridge)

	names := obs.AllEventNames()
	for _, n := range names {
		lg.Info(string(n))
	}

	primaryRecs := primary.Records()
	if len(primaryRecs) != len(names) {
		t.Fatalf("primary leg: want %d records, got %d", len(names), len(primaryRecs))
	}
	for i, n := range names {
		if primaryRecs[i].Message != string(n) {
			t.Fatalf("primary[%d]: want msg %q got %q", i, n, primaryRecs[i].Message)
		}
	}

	otelRecs := mem.snapshot()
	if len(otelRecs) != len(names) {
		t.Fatalf("OTel leg: want %d records, got %d", len(names), len(otelRecs))
	}
	for i, n := range names {
		if body := otelRecs[i].Body().AsString(); body != string(n) {
			t.Fatalf("OTel[%d]: want body %q got %q", i, n, body)
		}
	}
}

// TestBridge_WithAttrsPropagatesToBothLegs pins that .With() decoration reaches both legs.
func TestBridge_WithAttrsPropagatesToBothLegs(t *testing.T) {
	t.Parallel()

	var buf textBuf
	primary := slog.NewTextHandler(&buf, nil)
	lp, mem := newTestProvider()
	t.Cleanup(func() { _ = lp.Shutdown(context.Background()) })

	bridge := obsotel.NewBridgeHandler(primary, "regatta-test", lp)
	lg := slog.New(bridge).With(slog.String(string(obs.KeyLane), "default"))

	lg.Info(string(obs.EventTickStarted))

	if !buf.contains("lane=default") {
		t.Fatalf("primary leg lost With(lane); got %q", buf.String())
	}

	recs := mem.snapshot()
	if len(recs) != 1 {
		t.Fatalf("OTel leg: want 1 got %d", len(recs))
	}
	var sawOTelLane bool
	recs[0].WalkAttributes(func(kv log.KeyValue) bool {
		if kv.Key == string(obs.KeyLane) && kv.Value.AsString() == "default" {
			sawOTelLane = true
			return false
		}
		return true
	})
	if !sawOTelLane {
		t.Fatalf("OTel leg lost With(lane) attr")
	}
}

func TestBridge_WithGroupPropagatesToBothLegs(t *testing.T) {
	t.Parallel()

	var buf textBuf
	primary := slog.NewTextHandler(&buf, nil)
	lp, mem := newTestProvider()
	t.Cleanup(func() { _ = lp.Shutdown(context.Background()) })

	bridge := obsotel.NewBridgeHandler(primary, "regatta-test", lp)
	lg := slog.New(bridge).WithGroup("attrs").With(slog.String(string(obs.KeyReason), "test"))

	lg.Info(string(obs.EventTickCompleted))

	if !buf.contains("attrs.reason=test") {
		t.Fatalf("primary leg lost WithGroup propagation; got %q", buf.String())
	}

	recs := mem.snapshot()
	if len(recs) != 1 {
		t.Fatalf("OTel leg: want 1 got %d", len(recs))
	}
}

// TestBridge_NilPrimary_FallsBackToTextHandler pins the nil-primary stderr fallback.
func TestBridge_NilPrimary_FallsBackToTextHandler(t *testing.T) {
	t.Parallel()

	lp, mem := newTestProvider()
	t.Cleanup(func() { _ = lp.Shutdown(context.Background()) })

	bridge := obsotel.NewBridgeHandler(nil, "regatta-test", lp)
	lg := slog.New(bridge)

	// Must not panic.
	lg.Info(string(obs.EventTickStarted))

	if got := len(mem.snapshot()); got != 1 {
		t.Fatalf("OTel leg: want 1 got %d", got)
	}
}

// TestObsBridge_CostReconcileFailing_RoutesToOTel pins issue #289: cost.reconcile_failing ERROR slogs surface as OTel SeverityError with period_start, reason, attempt_count attrs preserved.
func TestObsBridge_CostReconcileFailing_RoutesToOTel(t *testing.T) {
	t.Parallel()

	primary := obstest.New()
	lp, mem := newTestProvider()
	t.Cleanup(func() { _ = lp.Shutdown(context.Background()) })

	bridge := obsotel.NewBridgeHandler(primary, "regatta-test", lp)
	lg := slog.New(bridge)

	const wantReason = "upstream_down"
	const wantPeriodStart int64 = 1_733_011_200_000
	const wantAttemptCount int64 = 5

	lg.Error(string(obs.EventCostReconcileFailing),
		slog.String(string(obs.KeyReason), wantReason),
		slog.Int64(string(obs.KeyPeriodStart), wantPeriodStart),
		slog.Int64(string(obs.KeyAttemptCount), wantAttemptCount),
		slog.String(string(obs.KeyEventName), string(obs.EventCostReconcileFailing)),
	)

	recs := mem.snapshot()
	if len(recs) != 1 {
		t.Fatalf("OTel leg: want 1 record, got %d", len(recs))
	}
	r := recs[0]

	if body := r.Body().AsString(); body != string(obs.EventCostReconcileFailing) {
		t.Fatalf("OTel body: want %q got %q", obs.EventCostReconcileFailing, body)
	}
	if got, want := r.Severity(), log.SeverityError; got != want {
		t.Fatalf("OTel severity: want %v (Error) got %v", want, got)
	}

	if v, ok := findAttr(r, string(obs.KeyReason)); !ok || v.AsString() != wantReason {
		t.Fatalf("reason attr: want %q got (%v, found=%v)", wantReason, v, ok)
	}
	if v, ok := findAttr(r, string(obs.KeyPeriodStart)); !ok || v.AsInt64() != wantPeriodStart {
		t.Fatalf("period_start attr: want %d got (%v, found=%v)", wantPeriodStart, v, ok)
	}
	if v, ok := findAttr(r, string(obs.KeyAttemptCount)); !ok || v.AsInt64() != wantAttemptCount {
		t.Fatalf("attempt_count attr: want %d got (%v, found=%v)", wantAttemptCount, v, ok)
	}
}
