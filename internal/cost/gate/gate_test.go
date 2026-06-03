package gate_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/trilamsr/regatta/internal/cost/gate"
	"github.com/trilamsr/regatta/internal/cost/spend"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// fixedEstimator returns a constant USD estimate. Avoids importing
// internal/cost/estimate (T2 scope) so this test compiles in isolation.
type fixedEstimator struct{ usd float64 }

func (f fixedEstimator) Estimate(_ context.Context, _ gate.EstHint, _ string) (float64, error) {
	return f.usd, nil
}

// fixedPricing satisfies gate.Pricing without importing the T2 package.
type fixedPricing struct {
	downgradeTarget string
}

func (f fixedPricing) DowngradeFor(_ string) string { return f.downgradeTarget }

func openTestDB(t *testing.T) *sql.DB {
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
	return db
}

// spendCounter uniqueifies test inserts so UNIQUE(run_id, written_by,
// nonce) never collides on co-located rows.
var spendCounter int64

// insertSpend writes one raw substrate_events row with kind=token_spend.
// Bypasses substrate.AppendEvent so we are free of the Wave-1 validator
// shape (T3 swaps the validator in Wave 2 to align with $.usd / $.dag_id).
func insertSpend(t *testing.T, db *sql.DB, payload string, writtenAt time.Time, tenantID string) {
	t.Helper()
	spendCounter++
	id := fmt.Sprintf("ev-%d-%d", writtenAt.UnixNano(), spendCounter)
	if len(id) > 26 {
		id = id[:26]
	}
	nonce := fmt.Sprintf("nonce-%d-%d", writtenAt.UnixNano(), spendCounter)
	_, err := db.Exec(`INSERT INTO substrate_events
		(id, run_id, work_item_id, tenant_id, trace_id, span_id, kind, key,
		 payload_json, blob_digest, supersedes, written_by, written_at,
		 schema_version, nonce, sig_alg, sig_key_id, sig_mac)
		VALUES (?, 'run-1', NULL, ?, '', '', 'token_spend', '', ?, '', NULL,
		        'tester', ?, 1, ?, 'hmac-sha256', 'test-1', 'mac')`,
		id, tenantID, payload, writtenAt.UnixMilli(), nonce)
	if err != nil {
		t.Fatalf("insert spend: %v", err)
	}
}

func newGate(t *testing.T, db *sql.DB, cfg gate.Config, est float64, downgrade string) *gate.Gate {
	t.Helper()
	r := spend.NewReader(db, func() time.Time { return time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC) })
	g := gate.New(cfg, fixedPricing{downgradeTarget: downgrade}, r, fixedEstimator{usd: est})
	return g
}

func baseScope() gate.WorkItemScope {
	return gate.WorkItemScope{
		WorkItemID: "WI-1",
		DAGID:      "DAG-A",
		OperatorID: "agent-7",
		TenantID:   "default",
		Model:      "claude-sonnet-4-5",
	}
}

func TestGate_NoConfig_AllowsAll(t *testing.T) {
	db := openTestDB(t)
	g := newGate(t, db, gate.Config{}, 0.01, "")
	v, err := g.Evaluate(context.Background(), baseScope())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !v.Allow {
		t.Fatalf("Allow=false; want true (unset cost config -> allow-all)")
	}
}

func TestGate_PerDAGCap_DeniesOverBudget(t *testing.T) {
	db := openTestDB(t)
	// Recorded $95 on DAG-A; cap=$100; estimate=$10 -> $105 > $100 -> deny.
	insertSpend(t, db, `{"usd":95.0,"dag_id":"DAG-A","operator_id":"agent-7","work_item_id":"WI-OLD"}`,
		time.Date(2026, 6, 1, 11, 0, 0, 0, time.UTC), "default")
	cfg := gate.Config{Safety: &gate.SafetyCost{PerDAGUSD: 100, Period: time.Hour, SoftPct: 80}}
	g := newGate(t, db, cfg, 10.0, "")
	v, err := g.Evaluate(context.Background(), baseScope())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if v.Allow {
		t.Fatalf("Allow=true; want deny (recorded=95+est=10=105 > cap=100)")
	}
	if !strings.HasPrefix(v.Reason, "cap_exceeded:dag:") {
		t.Fatalf("Reason=%q; want prefix cap_exceeded:dag:", v.Reason)
	}
}

func TestGate_PerDAGCap_AllowsUnderBudget(t *testing.T) {
	db := openTestDB(t)
	insertSpend(t, db, `{"usd":80.0,"dag_id":"DAG-A","operator_id":"agent-7","work_item_id":"WI-OLD"}`,
		time.Date(2026, 6, 1, 11, 0, 0, 0, time.UTC), "default")
	cfg := gate.Config{Safety: &gate.SafetyCost{PerDAGUSD: 100, Period: time.Hour, SoftPct: 80}}
	g := newGate(t, db, cfg, 10.0, "")
	v, err := g.Evaluate(context.Background(), baseScope())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !v.Allow {
		t.Fatalf("Allow=false; want allow (80+10=90 < 100)")
	}
}

func TestGate_SoftCapBreached_WarnByDefault(t *testing.T) {
	db := openTestDB(t)
	// 80 recorded, 5 estimate -> projected 85 = 85% of 100; soft_pct=80 -> breach.
	// No allow_downgrade annotation -> WARN-only.
	insertSpend(t, db, `{"usd":80.0,"dag_id":"DAG-A","operator_id":"agent-7","work_item_id":"WI-OLD"}`,
		time.Date(2026, 6, 1, 11, 0, 0, 0, time.UTC), "default")
	cfg := gate.Config{Safety: &gate.SafetyCost{PerDAGUSD: 100, Period: time.Hour, SoftPct: 80}}
	g := newGate(t, db, cfg, 5.0, "claude-haiku-4-5")
	v, err := g.Evaluate(context.Background(), baseScope())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !v.Allow {
		t.Fatalf("Allow=false; want allow (WARN-only default)")
	}
	if !v.SoftCapBreached {
		t.Fatalf("SoftCapBreached=false; want true (85%% > 80%%)")
	}
	if v.DowngradeTo != "" {
		t.Fatalf("DowngradeTo=%q; want empty (opt-in only)", v.DowngradeTo)
	}
}

func TestGate_SoftCapBreached_DowngradeOnlyWithAnnotation(t *testing.T) {
	db := openTestDB(t)
	insertSpend(t, db, `{"usd":80.0,"dag_id":"DAG-A","operator_id":"agent-7","work_item_id":"WI-OLD"}`,
		time.Date(2026, 6, 1, 11, 0, 0, 0, time.UTC), "default")
	cfg := gate.Config{Safety: &gate.SafetyCost{PerDAGUSD: 100, Period: time.Hour, SoftPct: 80}}
	g := newGate(t, db, cfg, 5.0, "claude-haiku-4-5")
	scope := baseScope()
	scope.AllowDowngrade = true
	v, err := g.Evaluate(context.Background(), scope)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if v.DowngradeTo != "claude-haiku-4-5" {
		t.Fatalf("DowngradeTo=%q; want claude-haiku-4-5", v.DowngradeTo)
	}
	if !v.SoftCapBreached {
		t.Fatalf("SoftCapBreached=false; want true")
	}
}

func TestGate_PrecedenceMostRestrictiveWins(t *testing.T) {
	db := openTestDB(t)
	// Operator recorded $48; DAG recorded $48 (same single row covers both).
	// per_dag_usd=100 (headroom 52), per_operator_usd=50 (headroom 2).
	// Estimate $10 -> operator-cap exceeded even though DAG has headroom.
	insertSpend(t, db, `{"usd":48.0,"dag_id":"DAG-A","operator_id":"agent-7","work_item_id":"WI-OLD"}`,
		time.Date(2026, 6, 1, 11, 0, 0, 0, time.UTC), "default")
	cfg := gate.Config{Safety: &gate.SafetyCost{
		PerDAGUSD: 100, PerOperatorUSD: 50, Period: time.Hour, SoftPct: 80,
	}}
	g := newGate(t, db, cfg, 10.0, "")
	v, err := g.Evaluate(context.Background(), baseScope())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if v.Allow {
		t.Fatalf("Allow=true; want deny (operator cap 50 < 48+10=58)")
	}
	if !strings.HasPrefix(v.Reason, "cap_exceeded:operator:") {
		t.Fatalf("Reason=%q; want prefix cap_exceeded:operator:", v.Reason)
	}
}

func TestGate_NilTracerFallsBackToGlobal(t *testing.T) {
	db := openTestDB(t)
	// Config.Tracer omitted -> Gate must still Evaluate without panic.
	cfg := gate.Config{Safety: &gate.SafetyCost{PerDAGUSD: 100, Period: time.Hour, SoftPct: 80}}
	g := newGate(t, db, cfg, 1.0, "")
	v, err := g.Evaluate(context.Background(), baseScope())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !v.Allow {
		t.Fatalf("Allow=false; want allow")
	}
}

func TestGate_EmitsCostEvaluateSpan(t *testing.T) {
	db := openTestDB(t)
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	tracer := tp.Tracer("test")
	cfg := gate.Config{
		Safety: &gate.SafetyCost{PerDAGUSD: 100, PerOperatorUSD: 50, Period: time.Hour, SoftPct: 80},
		Tracer: tracer,
	}
	g := newGate(t, db, cfg, 0.5, "")
	if _, err := g.Evaluate(context.Background(), baseScope()); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	spans := rec.Ended()
	var found bool
	for _, s := range spans {
		if s.Name() != "cost.evaluate" {
			continue
		}
		found = true
		attrs := map[string]bool{}
		for _, a := range s.Attributes() {
			attrs[string(a.Key)] = true
		}
		want := []string{
			"regatta.cost.usd_estimate",
			"regatta.cost.allow",
			"regatta.cost.cap_dag_usd",
			"regatta.cost.cap_op_usd",
			"regatta.cost.soft_breached",
			"regatta.work_item_id",
			"regatta.dag_id",
			"regatta.operator_id",
		}
		for _, k := range want {
			if !attrs[k] {
				t.Errorf("cost.evaluate span missing attribute %q (have %v)", k, attrs)
			}
		}
	}
	if !found {
		t.Fatalf("no cost.evaluate span emitted; got %d spans", len(spans))
	}
}

// TestGate_PerDAGCap_IntegerPrecisionAtBoundary pins the issue #554
// fix at the cap-decision surface: eleven $0.10 spends sum to
// 1.0999999999999998 in float (< $1.10 cap → buggy allow) but exactly
// 1_100_000 micro in integer (== cap, > 0 estimate → deny). The bug
// surface (float SUM allowed one extra spawn past cap) is closed by
// integer comparison.
func TestGate_PerDAGCap_IntegerPrecisionAtBoundary(t *testing.T) {
	db := openTestDB(t)
	// Eleven legacy float rows of $0.10 each, at distinct timestamps so
	// the substrate UNIQUE(run_id, written_by, nonce) constraint does
	// not collide.
	base := time.Date(2026, 6, 1, 11, 30, 0, 0, time.UTC)
	for i := 0; i < 11; i++ {
		insertSpend(t, db,
			`{"usd":0.1,"dag_id":"DAG-A","operator_id":"agent-7","work_item_id":"WI-OLD"}`,
			base.Add(time.Duration(i)*time.Second), "default")
	}
	// cap = $1.10; estimate = $0.000001 (1 micro — the smallest non-zero
	// projection). Integer projected = 1_100_001 > 1_100_000 cap → deny.
	// Float projected = 1.0999999999999998 + 0.000001 ≈ 1.100001 — also
	// would deny here, BUT the same exact-equality boundary at 11×$0.10
	// with zero estimate is the pathological case (covered by the
	// reader test); this gate test exercises the comparator with a
	// micro-level estimate that integer math distinguishes cleanly.
	cfg := gate.Config{Safety: &gate.SafetyCost{PerDAGUSD: 1.10, Period: time.Hour, SoftPct: 80}}
	g := newGate(t, db, cfg, 0.000001, "")
	v, err := g.Evaluate(context.Background(), baseScope())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if v.Allow {
		t.Fatalf("Allow=true; want deny (11×$0.10=$1.10 + $0.000001 > $1.10 cap, integer-exact)")
	}
	if !strings.HasPrefix(v.Reason, "cap_exceeded:dag:") {
		t.Fatalf("Reason=%q; want prefix cap_exceeded:dag:", v.Reason)
	}
}
