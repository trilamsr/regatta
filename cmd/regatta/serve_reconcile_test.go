package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// reconcileTestYAML pins safety.cost with a short reconcile_interval so
// startReconciler returns a non-nil goroutine handle. The CUE schema's
// reconcile_interval enum (1h|5m|15m|30m|6h|24h) is bypassed for tests
// via the raw-yaml decoder — loadCostReconcileSettings parses any Go
// duration so this test can pin a faster wall-clock cadence.
const reconcileTestYAML = `version: 1
repo:
  host: github
  owner: trilamsr
  name: regatta
spec_adapter:
  type: github_issues
  selector: "label:planned"
ci:
  command: "go test ./..."
gates:
  - id: prod-deploy-approval
    type: approval_gate
    name: prod
    risk_class: low
    reviewers: [alice]
    quorum: 1
    timeout: 1h
    decision_window: 30m
    on_timeout: fail
safety:
  destructive_ops_deny: []
  agent_creds_scope: dev_only
  cost:
    per_dag_usd: 50
    reconcile_interval: 5m
`

// TestStartReconciler_LandsBudgetReconciledRowOnTick drives the
// production wiring end-to-end: startReconciler builds a Reconciler
// against state.DB, the goroutine ticks once, the substrate row lands.
// Ctx cancel returns cleanly — no leak.
func TestStartReconciler_LandsBudgetReconciledRowOnTick(t *testing.T) {
	t.Setenv("ANTHROPIC_ADMIN_KEY", "sk-ant-admin-fixture-DO-NOT-LEAK")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/v1/organizations/cost_report/messages") {
			_, _ = w.Write([]byte(`{"data":[{"bucket_start":"2026-06-01T01:00:00Z","bucket_end":"2026-06-01T02:00:00Z","model":"claude-sonnet-4-7","cost_usd":4.2}],"has_more":false}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	tmp := t.TempDir()
	db := openTempDB(t, tmp)
	substrate.ResetClockForTesting()
	key := make([]byte, 32)
	copy(key, "serve-reconcile-test-key-padding-1")

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	done, started := startReconciler(ctx, reconcileWiring{
		DB:                db,
		Key:               key,
		KeyID:             "k1",
		ReconcileInterval: 5 * time.Minute,
		BaseURLOverride:   srv.URL,
		HTTPClient:        srv.Client(),
		// Clock pinned ~500ms before the next jitter mark so Run()'s
		// time.After(wait) returns inside the 5s test deadline. With
		// interval=5min + 2min jitter, current bucket = 01:00 and the
		// next candidate fires at 01:02:00 — wait ≈ 500ms.
		ClockOverride: func() time.Time { return time.Date(2026, 6, 1, 1, 1, 59, 500_000_000, time.UTC) },
		Logger:            logger,
	})
	if !started {
		t.Fatal("startReconciler returned started=false; wiring did not fire")
	}

	// Wait until the goroutine writes the first row, then cancel.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var n int
		if err := db.SQL().QueryRowContext(ctx,
			`SELECT COUNT(*) FROM substrate_events WHERE kind = 'budget_reconciled'`,
		).Scan(&n); err == nil && n >= 1 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("reconciler goroutine did not exit within 2s of ctx cancel")
	}

	var count int
	if err := db.SQL().QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM substrate_events WHERE kind = 'budget_reconciled'`,
	).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count < 1 {
		t.Fatalf("expected ≥1 budget_reconciled row after Tick; got %d", count)
	}
}

// TestStartReconciler_NoOpWhenIntervalZero pins the off-switch: an
// empty/zero reconcile_interval ⇒ no goroutine, no work. Operators on
// MVP-2 (no cost block) pay zero runtime cost.
func TestStartReconciler_NoOpWhenIntervalZero(t *testing.T) {
	tmp := t.TempDir()
	db := openTempDB(t, tmp)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	done, started := startReconciler(context.Background(), reconcileWiring{
		DB:                db,
		Key:               make([]byte, 32),
		KeyID:             "k1",
		ReconcileInterval: 0,
		Logger:            logger,
	})
	if started {
		t.Fatal("startReconciler returned started=true with ReconcileInterval=0; want no-op")
	}
	if done != nil {
		t.Fatal("startReconciler returned non-nil done with ReconcileInterval=0")
	}
}

// TestStartReconciler_NoOpWhenKeyMissing pins R15 fail-soft: HMAC key
// unset ⇒ no goroutine. The reconciler depends on substrate signing;
// starting a loop that will fail every Append is operator footgun
// energy. Reconciler stays cold; serve keeps booting.
func TestStartReconciler_NoOpWhenKeyMissing(t *testing.T) {
	tmp := t.TempDir()
	db := openTempDB(t, tmp)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	done, started := startReconciler(context.Background(), reconcileWiring{
		DB:                db,
		Key:               nil,
		KeyID:             "",
		ReconcileInterval: time.Minute,
		Logger:            logger,
	})
	if started {
		t.Fatal("startReconciler returned started=true with no HMAC key; want no-op")
	}
	if done != nil {
		t.Fatal("startReconciler returned non-nil done with no HMAC key")
	}
}

// TestLoadCostReconcileSettings_ReadsYAML pins the wiring path the
// production caller uses: regatta.yaml on disk → CostReconcileSettings.
func TestLoadCostReconcileSettings_ReadsYAML(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "regatta.yaml")
	if err := os.WriteFile(cfgPath, []byte(reconcileTestYAML), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	settings, err := loadCostReconcileSettingsFromFile(cfgPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if settings.ReconcileInterval != 5*time.Minute {
		t.Errorf("ReconcileInterval=%v; want 5m", settings.ReconcileInterval)
	}
}

// TestLoadCostReconcileSettings_MissingFileReturnsZero pins R6: a repo
// without regatta.yaml returns zero-value settings; the caller no-ops.
func TestLoadCostReconcileSettings_MissingFileReturnsZero(t *testing.T) {
	settings, err := loadCostReconcileSettingsFromFile(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if settings.ReconcileInterval != 0 {
		t.Errorf("ReconcileInterval=%v; want 0 for missing file", settings.ReconcileInterval)
	}
}

