package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestInsertApprovalEvent_PersistsTraceIDFromContext(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "decide.db")
	t0 := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return t0 }
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), clock)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := db.UpsertWorkItem(context.Background(), state.WorkItem{
		ID: "wi-trace-1", Kind: state.KindFeature, Title: "wi-trace-1",
		Lane: "server", Status: state.WorkStatusPlanned,
	}, state.SourceBrief, t0); err != nil {
		t.Fatalf("UpsertWorkItem: %v", err)
	}
	apID := "ap-trace-1"
	if err := db.CreateApproval(context.Background(), state.Approval{
		ID:                  apID,
		WorkItemID:          "wi-trace-1",
		GateName:            "test-gate",
		RequestedAt:         t0,
		RequestedBy:         "system",
		ReviewerSetSnapshot: state.ReviewerSet{Reviewers: []string{"alice"}, Quorum: 1},
		Quorum:              1,
		Status:              state.ApprovalStatusPending,
		TimeoutAt:           t0.Add(time.Hour),
	}); err != nil {
		t.Fatalf("CreateApproval: %v", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(tracetest.NewInMemoryExporter())),
	)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	ctx, span := tp.Tracer("test").Start(context.Background(), "decide")
	wantTrace := span.SpanContext().TraceID().String()
	defer span.End()

	tx, err := db.SQL().BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if err := insertApprovalEvent(ctx, tx, state.ApprovalEvent{
		ApprovalID: apID,
		Ts:         t0,
		Kind:       "decision",
		Actor:      "alice",
		TokenJTI:   "jti-trace-1",
	}); err != nil {
		_ = tx.Rollback()
		t.Fatalf("insertApprovalEvent: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	var gotTrace string
	if err := db.SQL().QueryRowContext(context.Background(),
		`SELECT trace_id FROM approval_events WHERE approval_id = ? AND token_jti = ?`,
		apID, "jti-trace-1").Scan(&gotTrace); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if gotTrace != wantTrace {
		t.Fatalf("approval_events.trace_id=%q want %q", gotTrace, wantTrace)
	}
}
