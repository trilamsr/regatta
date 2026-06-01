package program_test

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
	"github.com/trilamsr/regatta/internal/program"
)

var (
	decTestKey     = []byte("0123456789abcdef0123456789abcdef")
	decTestKeyID   = "test-key-1"
	decTestKeyring = map[string][]byte{decTestKeyID: decTestKey}
)

// openDeciderDB opens a migrated sqlite at a temp path. Substrate's
// clock-regression watermark is reset so test order does not bleed.
func openDeciderDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "decider.db")
	db, err := sql.Open("sqlite", state.DSN(path))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	if err := state.Migrate(context.Background(), db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	substrate.ResetClockForTesting()
	return db
}

func rawMsg(s string) json.RawMessage { return json.RawMessage(s) }

// TestCELDecider_CompileRejectsBadCEL pins constructor-time CEL compile rejection per spec §2.2.
func TestCELDecider_CompileRejectsBadCEL(t *testing.T) {
	db := openDeciderDB(t)
	_, err := program.NewCELDecider("not a cel expression", db, decTestKey, decTestKeyID, "celdecider", "g")
	if err == nil {
		t.Fatalf("NewCELDecider(bad): want compile error, got nil")
	}
	// cel-go classifier surfaces "Syntax error" or "compile" wording;
	// assert either, not a precise string, so a cel-go bump doesn't
	// regress the test.
	msg := err.Error()
	if !strings.Contains(msg, "compile") &&
		!strings.Contains(strings.ToLower(msg), "syntax") {
		t.Fatalf("NewCELDecider(bad): err=%v want compile-shaped", err)
	}
}

// TestCELDecider_DecideEmitsSignedGateVerdict pins fold+eval+emit happy path with verifiable signature.
func TestCELDecider_DecideEmitsSignedGateVerdict(t *testing.T) {
	db := openDeciderDB(t)
	ctx := context.Background()

	d, err := program.NewCELDecider(`outputs.score >= 0.5`,
		db, decTestKey, decTestKeyID, "celdecider", "approval")
	if err != nil {
		t.Fatalf("NewCELDecider: %v", err)
	}

	res, err := d.Decide(ctx, program.Snapshot{
		RunID:      "run-D",
		WorkItemID: "WI-D",
		TenantID:   substrate.DefaultTenantID,
		Outputs:    map[string]json.RawMessage{"score": rawMsg(`0.7`)},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if !res.Pass {
		t.Fatalf("Decide pass=false reason=%q want pass=true", res.Reason)
	}

	events, err := substrate.Fold(ctx, db, "run-D", substrate.KindGateVerdict)
	if err != nil {
		t.Fatalf("Fold: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("fold got %d events, want 1", len(events))
	}
	e := events[0]
	if e.Kind != substrate.KindGateVerdict {
		t.Errorf("kind=%q want gate_verdict", e.Kind)
	}
	if e.WorkItemID != "WI-D" {
		t.Errorf("work_item_id=%q want WI-D", e.WorkItemID)
	}
	if e.Key != "approval" {
		t.Errorf("key=%q want approval (gate name)", e.Key)
	}
	if err := substrate.Verify(e, decTestKeyring); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if _, err := hex.DecodeString(e.SigMAC); err != nil {
		t.Errorf("sig_mac not hex: %v", err)
	}
}

// TestCELDecider_FailedCELReturnsErrorNoEmit pins tx-rollback: eval failure ⇒ zero substrate rows.
func TestCELDecider_FailedCELReturnsErrorNoEmit(t *testing.T) {
	db := openDeciderDB(t)
	ctx := context.Background()

	// outputs.missing.deeper accesses an absent field on a map<string,
	// dyn>. cel-go surfaces this as a runtime "no such key" error.
	d, err := program.NewCELDecider(`outputs.missing.deeper > 0.5`,
		db, decTestKey, decTestKeyID, "celdecider", "math")
	if err != nil {
		t.Fatalf("NewCELDecider: %v", err)
	}

	_, derr := d.Decide(ctx, program.Snapshot{
		RunID:      "run-F",
		WorkItemID: "WI-F",
		TenantID:   substrate.DefaultTenantID,
		Outputs:    map[string]json.RawMessage{"score": rawMsg(`1.0`)},
	})
	if derr == nil {
		t.Fatalf("Decide: want eval error, got nil")
	}

	// Tx rollback invariant — no gate_verdict row landed.
	events, ferr := substrate.Fold(ctx, db, "run-F", substrate.KindGateVerdict)
	if ferr != nil {
		t.Fatalf("Fold: %v", ferr)
	}
	if len(events) != 0 {
		t.Fatalf("after failed Decide: got %d events, want 0 (tx must roll back)", len(events))
	}
}

// TestCELDecider_OneTxForFoldEvalEmit pins spec §10 #17 one-tx discipline via BeginTx-count tracker.
func TestCELDecider_OneTxForFoldEvalEmit(t *testing.T) {
	db := openDeciderDB(t)
	ctx := context.Background()

	tracker := &beginTxTracker{inner: db}

	d, err := program.NewCELDecider(`outputs.ok == true`,
		tracker, decTestKey, decTestKeyID, "celdecider", "tx")
	if err != nil {
		t.Fatalf("NewCELDecider: %v", err)
	}

	if _, err := d.Decide(ctx, program.Snapshot{
		RunID:      "run-T",
		WorkItemID: "WI-T",
		TenantID:   substrate.DefaultTenantID,
		Outputs:    map[string]json.RawMessage{"ok": rawMsg(`true`)},
	}); err != nil {
		t.Fatalf("Decide: %v", err)
	}

	if got := tracker.Count(); got != 1 {
		t.Fatalf("BeginTx call count=%d want exactly 1 (spec §10 #17)", got)
	}
}

// TestCELDecider_SnapshotCarriesTraceSpanID pins ctx-driven trace/span override on the emitted event (forge defense).
func TestCELDecider_SnapshotCarriesTraceSpanID(t *testing.T) {
	db := openDeciderDB(t)

	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	ctx, span := tp.Tracer("cel_decider_test").Start(context.Background(), "decide")
	defer span.End()
	wantTrace := span.SpanContext().TraceID().String()
	wantSpan := span.SpanContext().SpanID().String()
	if len(wantTrace) != 32 || len(wantSpan) != 16 {
		t.Fatalf("active span produced trace=%q (len %d) span=%q (len %d)",
			wantTrace, len(wantTrace), wantSpan, len(wantSpan))
	}

	d, err := program.NewCELDecider(`outputs.ok == true`,
		db, decTestKey, decTestKeyID, "celdecider", "trace")
	if err != nil {
		t.Fatalf("NewCELDecider: %v", err)
	}

	// Caller injects bogus trace ids — Decide MUST overwrite them
	// from ctx (hostile-caller defense).
	if _, err := d.Decide(ctx, program.Snapshot{
		RunID:      "run-TS",
		WorkItemID: "WI-TS",
		TenantID:   substrate.DefaultTenantID,
		TraceID:    "deadbeefdeadbeefdeadbeefdeadbeef", // hostile injection
		SpanID:     "cafebabecafebabe",                 // hostile injection
		Outputs:    map[string]json.RawMessage{"ok": rawMsg(`true`)},
	}); err != nil {
		t.Fatalf("Decide: %v", err)
	}

	events, err := substrate.Fold(context.Background(), db, "run-TS", substrate.KindGateVerdict)
	if err != nil {
		t.Fatalf("Fold: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("fold got %d events, want 1", len(events))
	}
	if got := events[0].TraceID; got != wantTrace {
		t.Errorf("trace_id=%q want %q (ctx-driven, not caller-injected)", got, wantTrace)
	}
	if got := events[0].SpanID; got != wantSpan {
		t.Errorf("span_id=%q want %q (ctx-driven, not caller-injected)", got, wantSpan)
	}
}

// beginTxTracker wraps *sql.DB and counts BeginTx calls. Satisfies
// program.Beginner so CELDecider treats it as the DB seam.
type beginTxTracker struct {
	inner *sql.DB
	calls atomic.Int64
}

func (b *beginTxTracker) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	b.calls.Add(1)
	return b.inner.BeginTx(ctx, opts)
}

func (b *beginTxTracker) Count() int64 {
	return b.calls.Load()
}

// suppress unused-import vet noise if helpers get pared down later.
var _ = time.Now
