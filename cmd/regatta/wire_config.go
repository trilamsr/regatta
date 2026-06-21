package main

import (
	"path/filepath"

	validateconfig "github.com/trilamsr/regatta/internal/config/validate"
)

// loadMarkdownCatalogRoot reads regatta.yaml at repoRoot and returns
// (spec_adapter.root, true) when the adapter type is markdown_catalog.
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
