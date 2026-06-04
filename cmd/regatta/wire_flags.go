package main

import (
	"flag"
	"path/filepath"
	"time"
)

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
// populated serveFlags. flag.ExitOnError keeps the original CLI
// contract — bad flags exit 2 before any startup work.
func parseServeFlags(args []string) serveFlags {
	fs := flag.NewFlagSet(subcmdServe, flag.ExitOnError)
	f := serveFlags{
		LaneCaps:  laneCapsFlag{},
		LogFormat: logFormatFlag(defaultLogFormat),
	}
	fs.StringVar(&f.DBPath, "db", "regatta.db", "Path to sqlite state DB")
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
	fs.Var(f.LaneCaps, "lane", "Per-lane concurrency cap, repeatable (e.g. -lane server:1)")
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
	_ = fs.Parse(args)

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
	return f
}
