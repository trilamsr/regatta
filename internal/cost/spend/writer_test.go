package spend_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/trilamsr/regatta/internal/cost/pricing"
	"github.com/trilamsr/regatta/internal/cost/spend"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

var testKey = []byte("0123456789abcdef0123456789abcdef")

const testKeyID = "test-writer-key"

func mustParseUnixMs(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t.UTC()
}

func errIsInvalid(err error) bool {
	return errors.Is(err, substrate.ErrInvalidPayload)
}

func openWriterDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "subs.db")
	db, err := sql.Open("sqlite", state.DSN(dbPath))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	if err := state.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	substrate.ResetClockForTesting()
	return db
}

func baseRecord() spend.CallRecord {
	return spend.CallRecord{
		CallID:              "msg_01abc",
		RetrySeq:            0,
		Model:               "claude-sonnet-4-7",
		InputTokens:         1_000_000,
		OutputTokens:        500_000,
		CacheReadTokens:     0,
		CacheCreationTokens: 0,
		OperatorID:          "agent-7",
		DAGID:               "DAG-A",
		WorkItemID:          "WI-1",
		TenantID:            substrate.DefaultTenantID,
		WrittenBy:           "spawner-1",
		RunID:               "run-1",
	}
}

func recordOne(t *testing.T, ctx context.Context, db *sql.DB, r spend.CallRecord) error {
	t.Helper()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := spend.RecordCall(ctx, tx, r, spend.WriteOptions{
		Now:   func() time.Time { return time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC) },
		Key:   testKey,
		KeyID: testKeyID,
	}); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// TestRecordCall_AppendsTokenSpendEvent — single call writes one row with matching payload.
func TestRecordCall_AppendsTokenSpendEvent(t *testing.T) {
	db := openWriterDB(t)
	if err := recordOne(t, context.Background(), db, baseRecord()); err != nil {
		t.Fatalf("RecordCall: %v", err)
	}
	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM substrate_events WHERE kind='token_spend'`).Scan(&rows); err != nil {
		t.Fatalf("count: %v", err)
	}
	if rows != 1 {
		t.Fatalf("rows=%d, want 1", rows)
	}
	var payload string
	if err := db.QueryRow(`SELECT payload_json FROM substrate_events WHERE kind='token_spend'`).Scan(&payload); err != nil {
		t.Fatalf("read payload: %v", err)
	}
	var p spend.TokenSpendPayload
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		t.Fatalf("decode payload: %v\n%s", err, payload)
	}
	if p.Model != "claude-sonnet-4-7" || p.CallID != "msg_01abc" {
		t.Fatalf("payload mis-mapped: %+v", p)
	}
	// 1M input @ $3/Mtok + 0.5M output @ $15/Mtok = 3.00 + 7.50 = 10.50.
	if p.USD < 10.49 || p.USD > 10.51 {
		t.Fatalf("USD=%.4f, want ~10.50", p.USD)
	}
}

// TestRecordCall_PricingMissingErrorsHard — unknown model errors + sets span attr + writes no row.
func TestRecordCall_PricingMissingErrorsHard(t *testing.T) {
	db := openWriterDB(t)
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "llm_call")

	r := baseRecord()
	r.Model = "no-such-model-9000"
	err := recordOne(t, ctx, db, r)
	span.End()

	if !errors.Is(err, pricing.ErrPricingMissing) {
		t.Fatalf("err=%v; want ErrPricingMissing", err)
	}
	// No substrate row was written.
	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM substrate_events WHERE kind='token_spend'`).Scan(&rows); err != nil {
		t.Fatalf("count: %v", err)
	}
	if rows != 0 {
		t.Fatalf("rows=%d, want 0 on pricing-missing", rows)
	}
	// regatta.cost.error=pricing_missing attribute on the span.
	var found bool
	for _, s := range sr.Ended() {
		for _, kv := range s.Attributes() {
			if kv.Key == attribute.Key("regatta.cost.error") && kv.Value.AsString() == "pricing_missing" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("regatta.cost.error=pricing_missing attr missing from any ended span")
	}
}

// TestRecordCall_PayloadIncludesAllFields — every TokenSpendPayload field populated.
func TestRecordCall_PayloadIncludesAllFields(t *testing.T) {
	db := openWriterDB(t)
	r := baseRecord()
	r.CacheReadTokens = 200_000
	r.CacheCreationTokens = 100_000
	if err := recordOne(t, context.Background(), db, r); err != nil {
		t.Fatalf("RecordCall: %v", err)
	}
	var raw string
	if err := db.QueryRow(`SELECT payload_json FROM substrate_events WHERE kind='token_spend'`).Scan(&raw); err != nil {
		t.Fatalf("read: %v", err)
	}
	for _, f := range []string{
		`"usd"`, `"model":"claude-sonnet-4-7"`, `"input_tokens":1000000`,
		`"output_tokens":500000`, `"cache_read_tokens":200000`,
		`"cache_creation_tokens":100000`, `"operator_id":"agent-7"`,
		`"dag_id":"DAG-A"`, `"work_item_id":"WI-1"`, `"pricing_rev"`,
		`"call_id":"msg_01abc"`,
	} {
		if !strings.Contains(raw, f) {
			t.Errorf("payload missing %s: %s", f, raw)
		}
	}
}

// TestRecordCall_IdempotentOnReplay — same CallID+RetrySeq twice → second insert returns ErrReplay.
func TestRecordCall_IdempotentOnReplay(t *testing.T) {
	db := openWriterDB(t)
	r := baseRecord()
	if err := recordOne(t, context.Background(), db, r); err != nil {
		t.Fatalf("first RecordCall: %v", err)
	}
	err := recordOne(t, context.Background(), db, r)
	if !errors.Is(err, substrate.ErrReplay) {
		t.Fatalf("second RecordCall: err=%v; want ErrReplay", err)
	}
	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM substrate_events WHERE kind='token_spend'`).Scan(&rows); err != nil {
		t.Fatalf("count: %v", err)
	}
	if rows != 1 {
		t.Fatalf("rows=%d, want 1 (idempotent)", rows)
	}
}

// TestRecordCall_OneWrittenByPerCallID — reviewer-added; same CallID across different WrittenBy is detectable.
func TestRecordCall_OneWrittenByPerCallID(t *testing.T) {
	db := openWriterDB(t)
	r := baseRecord()
	if err := recordOne(t, context.Background(), db, r); err != nil {
		t.Fatalf("first: %v", err)
	}
	r2 := r
	r2.WrittenBy = "spawner-2"
	if err := recordOne(t, context.Background(), db, r2); err != nil {
		t.Fatalf("second (different WrittenBy): %v", err)
	}
	// The substrate UNIQUE (run_id, written_by, nonce) tuple lets these
	// coexist; the audit query in tools/lint-cost-message-id-collisions
	// (followup) detects that one CallID landed under two WrittenBy
	// values — the scheduler-lane invariant says this should never happen
	// in steady state.
	var distinctWriters int
	if err := db.QueryRow(`SELECT COUNT(DISTINCT written_by)
		FROM substrate_events
		WHERE kind='token_spend'
		  AND json_extract(payload_json, '$.call_id') = ?`, r.CallID).Scan(&distinctWriters); err != nil {
		t.Fatalf("audit query: %v", err)
	}
	if distinctWriters != 2 {
		t.Fatalf("distinct_written_by=%d; want 2 (test fixture)", distinctWriters)
	}
}
