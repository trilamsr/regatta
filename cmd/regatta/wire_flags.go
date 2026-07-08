package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// reportServeFlagError maps a parseServeFlagsValidated error to a runServe
// exit code. -h / --help / -advanced-help surface as flag.ErrHelp — the
// usage text was already printed; exit 0 silently. Everything else is a
// bad flag; print + exit 2.
func reportServeFlagError(err error) int {
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	_, _ = fmt.Fprintln(os.Stderr, "regatta serve:", err)
	return 2
}

// Flag names shared between parseServeFlags registration, the
// serveDefaultFlags tier gate, and the tier-visibility tests. Extracted
// to satisfy goconst (each name would otherwise appear 3+ times across
// the package).
const (
	flagAddr         = "addr"
	flagDB           = "db"
	flagSpawner      = "spawner"
	flagUI           = "ui"
	flagAutoMerge    = "auto-merge"
	flagPublicURL    = "public-url"
	flagRepo         = "repo"
	flagLane         = "lane"
	flagTickOnce     = "tick-once"
	flagAdvancedHelp = "advanced-help"
	flagPoll         = "poll"
	flagTick         = "tick"
	flagHeartbeat    = "heartbeat"
	flagLockTTL      = "lock-ttl"
	flagBaseRef      = "base-ref"
	flagItemsRoot    = "items-root"
	flagClaude       = "claude"
	flagLogFormat    = "log-format"
	flagNoPRWatch    = "no-pr-watch"
)

// serveDefaultFlags is the operator-facing surface for `regatta serve -h`.
// Tuning knobs (heartbeat, lock-ttl, poll, tick, base-ref, items-root,
// claude, log-format, no-pr-watch) hide behind `-advanced-help` so first
// contact with the daemon is not a wall of 22 flags.
var serveDefaultFlags = map[string]bool{
	flagAddr:         true,
	flagDB:           true,
	flagSpawner:      true,
	flagUI:           true,
	flagAutoMerge:    true,
	flagPublicURL:    true,
	flagRepo:         true,
	flagLane:         true,
	flagTickOnce:     true,
	flagAdvancedHelp: true,
}

// printServeFlags renders the flag set. When advanced=false, only flags
// in serveDefaultFlags are shown; the footer nudges the operator at
// -advanced-help for the rest.
func printServeFlags(w io.Writer, fs *flag.FlagSet, advanced bool) {
	_, _ = fmt.Fprintln(w, "Usage: regatta serve [flags]")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Flags:")
	fs.VisitAll(func(fl *flag.Flag) {
		if !advanced && !serveDefaultFlags[fl.Name] {
			return
		}
		def := fl.DefValue
		if def != "" {
			_, _ = fmt.Fprintf(w, "  -%s (default %q)\n        %s\n", fl.Name, def, fl.Usage)
		} else {
			_, _ = fmt.Fprintf(w, "  -%s\n        %s\n", fl.Name, fl.Usage)
		}
	})
	if !advanced {
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintln(w, "Pass -advanced-help for tuning knobs (poll, tick, heartbeat, lock-ttl, base-ref, items-root, claude, log-format, no-pr-watch).")
	}
}

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
	fs.StringVar(&f.DBPath, flagDB, stateDBDefaultLiteral, "Path to sqlite state DB")
	fs.StringVar(&f.ItemsRoot, flagItemsRoot, ".", "Repo root containing .regatta/items/*.md (overrides regatta.yaml spec_adapter.root when set explicitly)")
	fs.BoolVar(&f.TickOnce, flagTickOnce, false, "Run one poll+schedule cycle and exit")
	fs.DurationVar(&f.PollDur, flagPoll, 30*time.Second, "SpecAdapter poll interval")
	fs.DurationVar(&f.TickDur, flagTick, 5*time.Second, "Scheduler tick interval")
	fs.DurationVar(&f.HeartDur, flagHeartbeat, 60*time.Second, "Lock heartbeat interval")
	fs.DurationVar(&f.LockTTL, flagLockTTL, 15*time.Minute, "Hotspot lock heartbeat lease")
	fs.StringVar(&f.SpawnerName, flagSpawner, "stub", "Spawner backend: stub | claude")
	fs.StringVar(&f.RepoRoot, flagRepo, ".", "Repo root for the claude spawner (worktrees live under <repo>/.regatta/worktrees)")
	fs.StringVar(&f.ClaudeBin, flagClaude, spawnerNameClaude, "Path to the claude binary (used when -spawner=claude)")
	fs.StringVar(&f.BaseRef, flagBaseRef, "HEAD", "Git ref a new agent worktree branches from")
	fs.Var(f.LaneCaps, flagLane, "Per-lane concurrency cap, repeatable (e.g. -lane server:1). When omitted and spec_adapter.type=github_issues, regatta serve auto-applies -lane server:1 to prevent cascade-rebase on overlapping issues.")
	fs.Var(&f.LogFormat, flagLogFormat, "Structured-log handler: text | json")
	fs.StringVar(&f.Addr, flagAddr, defaultListenerAddr, "HTTP listener bind address when --ui=true")
	fs.BoolVar(&f.UI, flagUI, true, "Boot the operator HTTP listener; --ui=false skips bind entirely")
	fs.BoolVar(&f.NoPRWatch, flagNoPRWatch, false, "[smoke-test only] disable the PR watcher; running agents stay in 'running' forever")
	fs.BoolVar(&f.AutoMerge, flagAutoMerge, false, "Auto-merge PRs when all gates + human approval land.")
	fs.StringVar(&f.PublicURL, flagPublicURL, "", "Public URL operators reach the listener via (e.g. https://regatta.example.com). Reverse-proxy deployments MUST set this so OriginCheck matches the external hostname instead of the inner pod r.Host.")
	advancedHelp := fs.Bool(flagAdvancedHelp, false, "Show all flags, including tuning knobs (poll, tick, heartbeat, lock-ttl, base-ref, items-root, claude, log-format, no-pr-watch)")
	fs.Usage = func() { printServeFlags(fs.Output(), fs, false) }
	if err := fs.Parse(args); err != nil {
		return f, err
	}
	if *advancedHelp {
		printServeFlags(os.Stdout, fs, true)
		return f, flag.ErrHelp
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
		if fl.Name == flagItemsRoot {
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
