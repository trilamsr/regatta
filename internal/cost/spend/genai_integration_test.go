package spend_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/trilamsr/regatta/internal/cost/spend"
	"github.com/trilamsr/regatta/internal/orchestrator/spawner"
)

// openStreamFixture opens a packaged spawner-side stream-json fixture
// reading it through the spawner module's testdata so we exercise the
// real W6 parser path end-to-end.
func openStreamFixture(t *testing.T, name string) *os.File {
	t.Helper()
	// Walk up from the spend package to the repo root, then over to
	// the spawner testdata. Avoids embed.FS coupling for a one-off
	// integration fixture.
	repoRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	p := filepath.Join(repoRoot, "internal", "orchestrator", "spawner", "testdata", "stream-json", name)
	f, err := os.Open(p)
	if err != nil {
		t.Fatalf("open fixture %s: %v", p, err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func newTracer(t *testing.T) *sdktrace.TracerProvider {
	t.Helper()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(tracetest.NewSpanRecorder()))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return tp
}

// recordCallback is the production-shape closure cost-gov wires into
// ClaudeSpawnerConfig.OnResultEvent. The signature mirrors what
// spawner.ResultEventCallback expects; tests pin that the callback
// fires with parser-derived fields.
func recordCallback(t *testing.T, db *sql.DB) spawner.ResultEventCallback {
	return func(ctx context.Context, ev spawner.StreamResultEvent) error {
		t.Helper()
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		err = spend.RecordCall(ctx, tx, spend.CallRecord{
			CallID:              ev.MessageID,
			RetrySeq:            0,
			Model:               ev.Model,
			InputTokens:         ev.InputTokens,
			OutputTokens:        ev.OutputTokens,
			CacheReadTokens:     ev.CacheReadInputTokens,
			CacheCreationTokens: ev.CacheCreationInputTokens,
			OperatorID:          "agent-1",
			DAGID:               "DAG-A",
			WorkItemID:          "WI-1",
			TenantID:            "default",
			WrittenBy:           "spawner-test",
			RunID:               "run-1",
		}, spend.WriteOptions{Key: testKey, KeyID: testKeyID, Now: nil})
		if err != nil {
			return err
		}
		return tx.Commit()
	}
}

// TestGenAIParser_InvokesRecordCallOnResultEvent — feeds stream-json fixture; asserts substrate row written.
func TestGenAIParser_InvokesRecordCallOnResultEvent(t *testing.T) {
	db := openWriterDB(t)
	tp := newTracer(t)
	tracer := tp.Tracer("test")
	cb := recordCallback(t, db)

	fixture := openStreamFixture(t, "success.jsonl")
	if err := spawner.ParseStream(context.Background(), tracer, fixture, cb); err != nil {
		t.Fatalf("ParseStream: %v", err)
	}

	var raw string
	err := db.QueryRow(`SELECT payload_json FROM substrate_events WHERE kind='token_spend'`).Scan(&raw)
	if err != nil {
		t.Fatalf("read row: %v", err)
	}
	if !strings.Contains(raw, `"call_id":"msg_01abc"`) || !strings.Contains(raw, `"model":"claude-sonnet-4-7"`) {
		t.Fatalf("row payload mismatch: %s", raw)
	}
	if !strings.Contains(raw, `"input_tokens":120`) || !strings.Contains(raw, `"output_tokens":42`) {
		t.Fatalf("usage projection mis-mapped: %s", raw)
	}
}

// TestGenAIParser_NoRecordCallWhenStreamJsonOff — legacy plain stdout writes no row.
func TestGenAIParser_NoRecordCallWhenStreamJsonOff(t *testing.T) {
	db := openWriterDB(t)
	tp := newTracer(t)
	tracer := tp.Tracer("test")
	calls := 0
	cb := func(ctx context.Context, ev spawner.StreamResultEvent) error {
		calls++
		return recordCallback(t, db)(ctx, ev)
	}

	fixture := openStreamFixture(t, "legacy_plain.txt")
	if err := spawner.ParseStream(context.Background(), tracer, fixture, cb); err != nil {
		t.Fatalf("ParseStream: %v", err)
	}

	if calls != 0 {
		t.Fatalf("callback fired %d times on legacy stdout; want 0", calls)
	}
	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM substrate_events WHERE kind='token_spend'`).Scan(&rows); err != nil {
		t.Fatalf("count: %v", err)
	}
	if rows != 0 {
		t.Fatalf("rows=%d on legacy stdout; want 0", rows)
	}
}

// TestGenAIParser_CallbackErrorBubblesAsSpanError — callback returning ErrPricingMissing leaves the chat span marked Error.
func TestGenAIParser_CallbackErrorBubblesAsSpanError(t *testing.T) {
	db := openWriterDB(t)
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	cb := func(ctx context.Context, _ spawner.StreamResultEvent) error {
		tx, _ := db.BeginTx(ctx, nil)
		defer func() { _ = tx.Rollback() }()
		// Force pricing-missing via an unknown model.
		err := spend.RecordCall(ctx, tx, spend.CallRecord{
			CallID: "msg_X", RetrySeq: 0, Model: "no-such-model",
			TenantID: "default", WrittenBy: "x", RunID: "r", WorkItemID: "w",
		}, spend.WriteOptions{Key: testKey, KeyID: testKeyID})
		return err
	}

	fixture := openStreamFixture(t, "success.jsonl")
	if err := spawner.ParseStream(context.Background(), tp.Tracer("test"), fixture, cb); err != nil {
		t.Fatalf("ParseStream: %v", err)
	}

	var found bool
	for _, s := range sr.Ended() {
		if !strings.HasPrefix(s.Name(), "chat ") {
			continue
		}
		for _, kv := range s.Attributes() {
			if kv.Key == "error.type" && kv.Value.AsString() == "record_call_failed" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("chat span missing error.type=record_call_failed after callback failure")
	}
}
