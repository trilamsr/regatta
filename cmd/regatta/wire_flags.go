package main

import (
	"flag"
	"fmt"
	"path/filepath"
	"time"
)

// parseServeFlagsValidated wraps parseServeFlags + validateServeFlags so
// runServe stays under the 400-line ceiling enforced by check-file-size.
func parseServeFlagsValidated(args []string) (serveFlags, error) {
	f, err := parseServeFlags(args)
	if err != nil {
		return f, err
	}
	if err := validateServeFlags(f); err != nil {
		return f, err
	}
	return f, nil
}

// validateServeFlags rejects non-positive durations that orchestrator.New
// would silently substitute with defaults (PollInterval/TickInterval/
// HeartbeatInterval all default-fallback on <= 0 in internal/orchestrator/
// orchestrator.go::New). Operator passing --poll 0 today thinks they
// disabled polling but actually gets the 30s default — silent typo class.
// Same shape as the events tail / review status / self-improve scan
// --since guards from R14 + R15.
func validateServeFlags(f serveFlags) error {
	if f.PollDur <= 0 {
		return fmt.Errorf("--poll must be > 0 (got %s)", f.PollDur)
	}
	if f.TickDur <= 0 {
		return fmt.Errorf("--tick must be > 0 (got %s)", f.TickDur)
	}
	if f.HeartDur <= 0 {
		return fmt.Errorf("--heartbeat must be > 0 (got %s)", f.HeartDur)
	}
	if f.LockTTL <= 0 {
		return fmt.Errorf("--lock-ttl must be > 0 (got %s)", f.LockTTL)
	}
	return nil
}

// serveFlags bundles the parsed flag values runServe consumes. The
// struct stays internal to this package; the public surface is still
// `regatta serve <args>` driven by parseServeFlags.
type serveFlags struct {
	DBPath      string
	ItemsRoot   string
	TickOnce    bool
	PollDur     time.Duration
	TickDur     time.Duration
	HeartDur    time.Duration
	LockTTL     time.Duration
	SpawnerName string
	RepoRoot    string
	ClaudeBin   string
	BaseRef     string
	LaneCaps    laneCapsFlag
	LogFormat   logFormatFlag
	Addr        string
	UI          bool
	NoPRWatch   bool
	AutoMerge   bool
	PublicURL   string
}

// parseServeFlags registers the `regatta serve` flag set against args,
// applies the regatta.yaml-driven items-root fallback (explicit flag
// wins; yaml spec_adapter.root is the second-choice), and returns the
// populated serveFlags. ContinueOnError surfaces the parse error so
// main() can run deferred cleanup before exit (R-MEGA-3 LIVE-15).
func parseServeFlags(args []string) (serveFlags, error) {
	fs := flag.NewFlagSet(subcmdServe, flag.ContinueOnError)
	f := serveFlags{
		LaneCaps:  laneCapsFlag{},
		LogFormat: logFormatFlag(defaultLogFormat),
	}
	fs.StringVar(&f.DBPath, "db", stateDBDefaultLiteral, "Path to sqlite state DB")
	fs.StringVar(&f.ItemsRoot, "items-root", ".", "Repo root containing .regatta/items/*.md (overrides regatta.yaml spec_adapter.root when set explicitly)")
	fs.BoolVar(&f.TickOnce, "tick-once", false, "Run one poll+schedule cycle and exit")
	fs.DurationVar(&f.PollDur, "poll", 30*time.Second, "SpecAdapter poll interval")
	fs.DurationVar(&f.TickDur, "tick", 5*time.Second, "Scheduler tick interval")
	fs.DurationVar(&f.HeartDur, "heartbeat", 60*time.Second, "Lock heartbeat interval")
	fs.DurationVar(&f.LockTTL, "lock-ttl", 15*time.Minute, "Hotspot lock heartbeat lease")
	fs.StringVar(&f.SpawnerName, "spawner", "stub", "Spawner backend: stub | claude")
	fs.StringVar(&f.RepoRoot, "repo", ".", "Repo root for the claude spawner (worktrees live under <repo>/.regatta/worktrees)")
	fs.StringVar(&f.ClaudeBin, "claude", "claude", "Path to the claude binary (used when -spawner=claude)")
	fs.StringVar(&f.BaseRef, "base-ref", "HEAD", "Git ref a new agent worktree branches from")
	fs.Var(f.LaneCaps, "lane", "Per-lane concurrency cap, repeatable (e.g. -lane server:1). When omitted and spec_adapter.type=github_issues, regatta serve auto-applies -lane server:1 to prevent cascade-rebase on overlapping issues (#1048); pass -lane server:N to raise the cap.")
	fs.Var(&f.LogFormat, "log-format", "Structured-log handler: text | json")
	fs.StringVar(&f.Addr, "addr", defaultListenerAddr, "HTTP listener bind address when --ui=true")
	fs.BoolVar(&f.UI, "ui", true, "Boot the operator HTTP listener; --ui=false skips bind entirely")
	fs.BoolVar(&f.NoPRWatch, "no-pr-watch", false, "[smoke-test only] disable the PR watcher; running agents stay in 'running' forever (issue #526)")
	// PHASE AUTONOMY §11 W2 c2: default OFF so the c2 wiring lands
	// without changing operator-observable behavior; once the scheduler-
	// side gates_pass hook ships (c3+), flipping this to true closes the
	// autonomous-loop merge gap.
	fs.BoolVar(&f.AutoMerge, "auto-merge", false, "Enable autonomous gh-pr-merge worker (PHASE AUTONOMY §11 W2 c2)")
	fs.StringVar(&f.PublicURL, "public-url", "", "Public URL operators reach the listener via (e.g. https://regatta.example.com). Reverse-proxy deployments MUST set this so OriginCheck matches the external hostname instead of the inner pod r.Host (#304).")
	if err := fs.Parse(args); err != nil {
		return f, err
	}

	// Resolution priority for the adapter items-root (spec
	// docs/engineer/specs/2026-06-02-s1-t1-self-host-regatta-yaml.md §5):
	//   1. explicit --items-root flag wins;
	//   2. else regatta.yaml spec_adapter.root (markdown_catalog only);
	//   3. else the flag default ".".
	// flag.Visit reports only flags the operator passed; the default
	// value never visits, so an absent --items-root falls through to
	// the yaml field if one is declared.
	itemsRootExplicit := false
	fs.Visit(func(fl *flag.Flag) {
		if fl.Name == "items-root" {
			itemsRootExplicit = true
		}
	})
	if !itemsRootExplicit {
		if yamlRoot, ok := loadMarkdownCatalogRoot(f.RepoRoot); ok {
			resolved := yamlRoot
			if !filepath.IsAbs(resolved) {
				resolved = filepath.Join(f.RepoRoot, resolved)
			}
			f.ItemsRoot = resolved
		}
	}
	return f, nil
}
