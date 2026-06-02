package reconcile

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// TestSubstrateAppender_AppendsBudgetReconciledRow pins the production
// Appender contract: one Append call lands one HMAC-signed substrate
// row keyed kind=budget_reconciled.
func TestSubstrateAppender_AppendsBudgetReconciledRow(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	db, err := state.Open(ctx, state.DSN(filepath.Join(tmp, "subs.db")))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	substrate.ResetClockForTesting()

	key := bytes32("appender-test-key-padding-padding")
	a := NewSubstrateAppender(SubstrateAppenderConfig{
		DB:    db.SQL(),
		Key:   key,
		KeyID: "k1",
	})

	at := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	payload := mustJSON(t, BudgetReconciledPayload{
		PeriodStart: at.Add(-time.Hour).UnixMilli(),
		PeriodEnd:   at.UnixMilli(),
		ActualUSD:   1.23,
		RecordedUSD: 1.20,
		DeltaUSD:    0.03,
		DriftPct:    0.025,
	})

	if err := a.Append(ctx, substrate.DefaultTenantID, string(substrate.KindBudgetReconciled), payload, at); err != nil {
		t.Fatalf("Append: %v", err)
	}

	var count int
	if err := db.SQL().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM substrate_events WHERE kind = 'budget_reconciled' AND tenant_id = ?`,
		substrate.DefaultTenantID,
	).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("got %d budget_reconciled rows; want 1", count)
	}

	var sigAlg, sigKeyID, sigMAC string
	if err := db.SQL().QueryRowContext(ctx,
		`SELECT sig_alg, sig_key_id, sig_mac FROM substrate_events WHERE kind = 'budget_reconciled'`,
	).Scan(&sigAlg, &sigKeyID, &sigMAC); err != nil {
		t.Fatalf("scan sig: %v", err)
	}
	if sigAlg == "" || sigMAC == "" || sigKeyID != "k1" {
		t.Fatalf("signature not populated: alg=%q keyID=%q mac_len=%d", sigAlg, sigKeyID, len(sigMAC))
	}
}

// TestSubstrateAppender_TickEndToEnd drives a real Reconciler through one
// Tick against an httptest Cost API and asserts the row lands in sqlite —
// closes the loop on the production wiring: tick → Append → row.
func TestSubstrateAppender_TickEndToEnd(t *testing.T) {
	t.Setenv(adminKeyEnv, adminKeyFixture)
	ctx := context.Background()
	tmp := t.TempDir()
	db, err := state.Open(ctx, state.DSN(filepath.Join(tmp, "subs.db")))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	substrate.ResetClockForTesting()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/v1/organizations/cost_report/messages") {
			_, _ = w.Write(mustReadTestdata(t, "anthropic_cost_2026_06_01_01h.json"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	key := bytes32("end-to-end-key-padding-padding-1")
	rec := NewReconciler(Config{
		Clock:          frozenClock(fixedTime()),
		HTTPClient:     srv.Client(),
		BaseURL:        srv.URL,
		BucketWidth:    time.Hour,
		UsageAPIKeyEnv: adminKeyEnv,
		Appender:       NewSubstrateAppender(SubstrateAppenderConfig{DB: db.SQL(), Key: key, KeyID: "k1"}),
		RecordedReader: mkRecorder(t, 13.25),
		TenantID:       substrate.DefaultTenantID,
		Sleep:          func(time.Duration) {},
	})
	if err := rec.Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	var raw []byte
	if err := db.SQL().QueryRowContext(ctx,
		`SELECT payload_json FROM substrate_events WHERE kind = 'budget_reconciled'`,
	).Scan(&raw); err != nil {
		t.Fatalf("scan payload: %v", err)
	}
	var p BudgetReconciledPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	want := 12.50 + 0.75
	if p.ActualUSD != want {
		t.Fatalf("payload.actual_usd=%v; want %v", p.ActualUSD, want)
	}
}

// bytes32 pads s to exactly 32 bytes so the HMAC weak-key guard passes
// without forcing tests to hand-build 32-byte literals.
func bytes32(s string) []byte {
	out := make([]byte, 32)
	copy(out, s)
	return out
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}
