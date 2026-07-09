package main

import (
	"log/slog"
	"path/filepath"
	"time"

	validateconfig "github.com/trilamsr/regatta/internal/config/validate"
	"github.com/trilamsr/regatta/internal/orchestrator/merge/lowrisk"
	"github.com/trilamsr/regatta/internal/orchestrator/scheduler"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// loadMarkdownCatalogRoot reads regatta.yaml at repoRoot and returns
// (work_item_source.root, true) when the adapter type is markdown_catalog.
// Returns ("", false) when the yaml is missing, malformed, or declares
// a different adapter type — callers fall back to the --items-root
// flag default. Malformed-yaml is intentionally not fatal here: the
// approval-gate loader catches the same yaml a few lines later and
// fails loud there, so this codepath stays read-only-best-effort.
func loadMarkdownCatalogRoot(repoRoot string) (string, bool) {
	cfgPath := filepath.Join(repoRoot, "regatta.yaml")
	cfg, err := validateconfig.LoadConfigFile(cfgPath)
	if err != nil {
		return "", false
	}
	root := cfg.MarkdownCatalogRoot()
	if root == "" {
		return "", false
	}
	return root, true
}

// loadSchedulerParallelCap reads regatta.yaml at repoRoot and returns
// the resolved `scheduler.parallel_cap` value (spec
// 2026-06-09-scheduler-parallel-cap-enforcement §3.1; closes #1169).
// Returns 0 when the yaml is missing, malformed, or the scheduler block
// is absent — preserves pre-#1169 lane-cap-only behavior byte-equal.
// Read-only-best-effort mirrors loadMarkdownCatalogRoot's contract:
// downstream loaders (wire_authz, wire_secrets) catch the same yaml
// and surface load errors with operator-visible signals.
func loadSchedulerParallelCap(repoRoot string) int {
	cfgPath := filepath.Join(repoRoot, "regatta.yaml")
	cfg, err := validateconfig.LoadConfigFile(cfgPath)
	if err != nil {
		return 0
	}
	return cfg.SchedulerParallelCap()
}

// loadDestructiveOpLists reads regatta.yaml at repoRoot and returns the
// resolved safety deny + allow lists threaded into the agent brief so
// force-with-lease on an agent's own branch binds at dispatch (MAY-97,
// MAY-258). Missing / malformed yaml returns nils — the brief omits the
// policy section; read-only-best-effort per the siblings above.
func loadDestructiveOpLists(repoRoot string) (deny, allow []string) {
	cfgPath := filepath.Join(repoRoot, "regatta.yaml")
	cfg, err := validateconfig.LoadConfigFile(cfgPath)
	if err != nil {
		return nil, nil
	}
	return cfg.DestructiveOpLists()
}

// buildLowRiskGate resolves the MAY-86 double opt-in into a
// scheduler.LowRiskGate. It returns nil when auto-merge is OFF (the
// Worker is nil anyway, so OnGatesPass no-ops). When auto-merge is ON it
// is the conservative-default boundary:
//   - low_risk_automerge.enabled=false (or block absent) → HoldAll, so
//     auto-merge holds EVERYTHING (never widens past pre-MAY-86).
//   - enabled=true → a real lowrisk.Gate filtering on the embedded
//     load-bearing veto + loc_cap + stateless soak.
//
// An unparseable hold_window also falls back to HoldAll (fail-closed) and
// logs a WARN so the operator learns their config was rejected rather than
// silently reverting to "hold everything".
func buildLowRiskGate(repoRoot string, autoMergeEnabled bool, logger *slog.Logger, db *state.DB, hmacKey []byte, hmacKeyID string) scheduler.LowRiskGate {
	if !autoMergeEnabled {
		return nil
	}
	cfgPath := filepath.Join(repoRoot, "regatta.yaml")
	cfg, err := validateconfig.LoadConfigFile(cfgPath)
	if err != nil {
		return lowrisk.HoldAll{}
	}
	lr := cfg.LowRiskAutoMergeConfig()
	if !lr.Enabled {
		return lowrisk.HoldAll{}
	}
	hold, err := time.ParseDuration(lr.HoldWindow)
	if err != nil || hold <= 0 {
		logger.Warn("lowrisk.hold_window_invalid_holding_all",
			"hold_window", lr.HoldWindow, "err", err)
		return lowrisk.HoldAll{}
	}
	locCap := lr.LOCCap
	if locCap <= 0 {
		locCap = 50
	}
	c := lowrisk.New(lowrisk.Config{LOCCap: locCap, HoldWindow: hold})
	g := lowrisk.NewGate(c, lowrisk.NewGhFetcher())
	if sink := newLowRiskAuditSink(db, hmacKey, hmacKeyID, logger); sink != nil {
		g.SetAuditSink(sink)
	}
	return g
}
