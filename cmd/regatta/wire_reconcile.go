// wire_reconcile wires the cost-reconciler goroutine into serve startup.
// The reconciler is opt-in: only an explicit `safety.cost.reconcile_interval`
// in regatta.yaml fires the loop. Spec §3.4 line 224 + plan §3 "T4 exports
// reconcile.Run(ctx) (long-loop)".
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	validateconfig "github.com/trilamsr/regatta/internal/config/validate"
	"github.com/trilamsr/regatta/internal/cost/reconcile"
	"github.com/trilamsr/regatta/internal/cost/spend"
	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// reconcileWiring bundles the deps startReconciler needs. Test seams
// (BaseURLOverride, HTTPClient, ClockOverride) default to production
// values; the test harness pins httptest URLs + frozen clocks without
// reaching into serve.go's flag plumbing.
type reconcileWiring struct {
	DB                *state.DB
	Key               []byte
	KeyID             string
	ReconcileInterval time.Duration
	UsageAPIKeyEnv    string
	Logger            *slog.Logger

	// Test-only overrides. Production callers pass zero values.
	BaseURLOverride string
	HTTPClient      *http.Client
	ClockOverride   func() time.Time
}

// startReconciler boots the cost-reconciler goroutine when wiring is
// complete (non-zero interval, HMAC key present, DB non-nil). Returns
// (nil, false) when any precondition fails — the operator-facing
// contract is "if you set safety.cost.reconcile_interval and configured
// REGATTA_HMAC_KEY, the loop runs; otherwise we no-op silently".
//
// Returns the done-chan the caller selects on for graceful shutdown.
// The goroutine exits when ctx is canceled (reconcile.Run honours ctx
// per spec §3.4 failure-mode table); the done-chan closes immediately
// after Run returns.
//
// R6 graceful-degradation: Run() loops forever swallowing tick errors,
// so a single reconciliation failure cannot block startup or stop the
// loop. R15 admin-key handling: the Anthropic admin key flows via env
// var (ANTHROPIC_ADMIN_KEY by default; operator-overridable via
// safety.cost.usage_api_key_env) — we never read it at this layer, the
// reconcile.Client reaches into the env on each tick.
func startReconciler(ctx context.Context, w reconcileWiring) (<-chan struct{}, bool) {
	if w.ReconcileInterval <= 0 {
		return nil, false
	}
	if w.DB == nil {
		return nil, false
	}
	if len(w.Key) == 0 || w.KeyID == "" {
		if w.Logger != nil {
			w.Logger.WarnContext(ctx, string(obs.EventCostReconcileSkipped),
				slog.String(string(obs.KeyReason), "no_hmac_key"),
				slog.String(string(obs.KeyEventName), string(obs.EventCostReconcileSkipped)),
			)
		}
		return nil, false
	}

	clock := w.ClockOverride
	if clock == nil {
		clock = time.Now
	}
	usageEnv := w.UsageAPIKeyEnv
	if usageEnv == "" {
		usageEnv = "ANTHROPIC_ADMIN_KEY"
	}
	rec := reconcile.NewReconciler(reconcile.Config{
		Clock:             clock,
		HTTPClient:        w.HTTPClient,
		BaseURL:           w.BaseURLOverride,
		BucketWidth:       time.Hour,
		ReconcileInterval: w.ReconcileInterval,
		UsageAPIKeyEnv:    usageEnv,
		UserAgent:         "regatta-reconcile/1",
		Logger:            w.Logger,
		Appender: reconcile.NewSubstrateAppender(reconcile.SubstrateAppenderConfig{
			DB:    w.DB.SQL(),
			Key:   w.Key,
			KeyID: w.KeyID,
		}),
		RecordedReader: spend.NewReader(w.DB.SQL(), clock).RecordedUSDForWindow,
		TenantID:       substrate.DefaultTenantID,
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		// Run() loops until ctx is canceled; ctx.Err() is the only
		// non-nil return per the spec contract — drop it to keep the
		// shutdown surface quiet (signal handlers already log the cause).
		if err := rec.Run(ctx); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			if w.Logger != nil {
				w.Logger.ErrorContext(ctx, "cost.reconciler_exited",
					slog.String("err", err.Error()),
				)
			}
		}
	}()
	return done, true
}

// loadCostReconcileSettingsFromFile is the file-reading sibling of
// validateconfig.LoadCostReconcileSettings; mirrors loadMarkdownCatalogRoot's
// fail-soft contract — a missing or malformed yaml returns zero settings
// so serve still boots (R6).
func loadCostReconcileSettingsFromFile(path string) (validateconfig.CostReconcileSettings, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is operator-supplied trust boundary fixed to <repo>/regatta.yaml.
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return validateconfig.CostReconcileSettings{}, nil
		}
		return validateconfig.CostReconcileSettings{}, fmt.Errorf("read %s: %w", path, err)
	}
	return validateconfig.LoadCostReconcileSettings(data)
}

// loadCostReconcileSettingsFor returns the parsed safety.cost reconcile
// settings for the repo rooted at repoRoot. Returns zero settings when
// no regatta.yaml exists — opt-in semantics per spec §3.6.
//
// Yaml-decode errors are dropped silently because every serve callsite
// invokes buildApprovalGate against the SAME regatta.yaml earlier in
// boot; a malformed file fails loud there and never reaches this call.
// The defensive drop keeps unit tests file-disjoint from buildApprovalGate.
func loadCostReconcileSettingsFor(repoRoot string) validateconfig.CostReconcileSettings {
	s, err := loadCostReconcileSettingsFromFile(filepath.Join(repoRoot, "regatta.yaml"))
	if err != nil {
		return validateconfig.CostReconcileSettings{}
	}
	return s
}

// loadCostCapSettingsFor returns the parsed safety.cost.cap settings
// for repo rooted at repoRoot. Returns zero settings (CapMicro=0) when
// no regatta.yaml exists OR no cap block is configured — degrades to
// per-scope-only, matching pre-W5 behaviour byte-equal.
func loadCostCapSettingsFor(repoRoot string) validateconfig.CostCapSettings {
	data, err := os.ReadFile(filepath.Join(repoRoot, "regatta.yaml")) //nolint:gosec // operator repo-root; same posture as loadCostReconcileSettingsFor
	if err != nil {
		return validateconfig.CostCapSettings{}
	}
	s, err := validateconfig.LoadCostCapSettings(data)
	if err != nil {
		return validateconfig.CostCapSettings{}
	}
	return s
}
