// serve wires the orchestrator daemon; spawner selection, scheduler-side
// evaluator/schema adapters, and the brief keyring loader are all
// serve-only concerns and ship together here.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator"
	"github.com/trilamsr/regatta/internal/orchestrator/adapter"
	"github.com/trilamsr/regatta/internal/orchestrator/adaptersync"
	"github.com/trilamsr/regatta/internal/orchestrator/reaper"
	"github.com/trilamsr/regatta/internal/orchestrator/scheduler"
	"github.com/trilamsr/regatta/internal/orchestrator/spawner"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/program"
)

// laneCapsFlag implements flag.Value for repeated `-lane name:cap` flags.
type laneCapsFlag map[string]int

func (l laneCapsFlag) String() string {
	parts := make([]string, 0, len(l))
	for k, v := range l {
		parts = append(parts, fmt.Sprintf("%s:%d", k, v))
	}
	return strings.Join(parts, ",")
}

func (l laneCapsFlag) Set(s string) error {
	name, capStr, ok := strings.Cut(s, ":")
	if !ok {
		return fmt.Errorf("expected name:cap, got %q", s)
	}
	n, err := strconv.Atoi(capStr)
	if err != nil {
		return fmt.Errorf("invalid cap %q: %w", capStr, err)
	}
	if n < 0 {
		return fmt.Errorf("cap must be non-negative, got %d", n)
	}
	l[strings.TrimSpace(name)] = n
	return nil
}

func runServe(args []string) int {
	fs := flag.NewFlagSet(subcmdServe, flag.ExitOnError)
	dbPath := fs.String("db", "regatta.db", "Path to sqlite state DB")
	itemsRoot := fs.String("items-root", ".", "Repo root containing .regatta/items/*.md")
	tickOnce := fs.Bool("tick-once", false, "Run one poll+schedule cycle and exit")
	pollDur := fs.Duration("poll", 30*time.Second, "SpecAdapter poll interval")
	tickDur := fs.Duration("tick", 5*time.Second, "Scheduler tick interval")
	heartDur := fs.Duration("heartbeat", 60*time.Second, "Lock heartbeat interval")
	lockTTL := fs.Duration("lock-ttl", 15*time.Minute, "Hotspot lock heartbeat lease")
	spawnerName := fs.String("spawner", "stub", "Spawner backend: stub | claude")
	repoRoot := fs.String("repo", ".", "Repo root for the claude spawner (worktrees live under <repo>/.regatta/worktrees)")
	claudeBin := fs.String("claude", "claude", "Path to the claude binary (used when -spawner=claude)")
	baseRef := fs.String("base-ref", "HEAD", "Git ref a new agent worktree branches from")
	laneCaps := laneCapsFlag{}
	fs.Var(laneCaps, "lane", "Per-lane concurrency cap, repeatable (e.g. -lane server:1)")
	_ = fs.Parse(args)

	logger := log.New(os.Stderr, "regatta: ", log.LstdFlags|log.Lmicroseconds)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := state.Open(ctx, state.DSN(*dbPath))
	if err != nil {
		logger.Printf("open db: %v", err)
		return 2
	}
	defer func() { _ = db.Close() }()

	ad, err := adapter.NewMarkdownCatalog(adapter.MarkdownCatalogConfig{Root: *itemsRoot})
	if err != nil {
		logger.Printf("adapter: %v", err)
		return 2
	}

	set, err := buildSpawner(*spawnerName, *repoRoot, *claudeBin, *baseRef)
	if err != nil {
		logger.Printf("spawner: %v", err)
		return 2
	}

	briefsDir := filepath.Join(*repoRoot, ".regatta", "programs")
	if err := os.MkdirAll(briefsDir, 0o750); err != nil {
		logger.Printf("mkdir briefs dir: %v", err)
		return 2
	}
	syncer := adaptersync.New(ad, db)
	// Shared evaluator: BriefLoader materialises edges (and could warm
	// the compile cache); Scheduler.Tick step-0 Evals through the same
	// instance so cached cel.Program survives across ticks.
	evaluator := program.NewEdgeEvaluator()
	loader := program.NewBriefLoader(os.DirFS(briefsDir), db, loadBriefKeyring(), evaluator)
	sched := scheduler.New(db, scheduler.Config{
		LaneCaps:       map[string]int(laneCaps),
		LockTTL:        *lockTTL,
		Evaluator:      schedulerEvaluator{evaluator},
		OutputsSchemas: outputsSchemaResolverFor(loader),
	})

	o := orchestrator.New(orchestrator.Config{
		AdapterSync:       syncer,
		BriefLoader:       loader,
		DB:                db,
		Scheduler:         sched,
		Spawner:           set.Spawner,
		DBPath:            *dbPath,
		PollInterval:      *pollDur,
		TickInterval:      *tickDur,
		HeartbeatInterval: *heartDur,
		LockTTL:           *lockTTL,
	})
	o.SetLogger(logger.Printf)
	if set.Worktrees != nil {
		o.SetReaper(reaper.New(db, set.Worktrees, set.Killer))
	}

	if err := o.Recover(ctx); err != nil {
		logger.Printf("recover: %v", err)
		return 1
	}

	if *tickOnce {
		if err := o.PollOnce(ctx); err != nil {
			logger.Printf("poll: %v", err)
			return 1
		}
		if err := o.ScheduleOnce(ctx); err != nil {
			logger.Printf("schedule: %v", err)
			return 1
		}
		if err := o.ReapTerminal(ctx); err != nil {
			logger.Printf("reap: %v", err)
			return 1
		}
		return 0
	}

	if err := o.Run(ctx); err != nil {
		logger.Printf("run: %v", err)
		return 1
	}
	return 0
}

// schedulerEvaluator adapts a *program.EdgeEvaluator to the scheduler-
// side EdgeEvaluator interface. The scheduler seam types schema as
// `any` so it never imports program; the production evaluator types it
// as *program.OutputsSchema. The adapter unboxes the any back to the
// concrete type, defaulting to nil when the resolver missed (matching
// the runtime evaluator's contract that schema is advisory at eval).
type schedulerEvaluator struct {
	ev *program.EdgeEvaluator
}

func (s schedulerEvaluator) Eval(ctx context.Context, edge state.EdgeRow, schema any, journal state.OutputJournalEntry) (bool, string, error) {
	sch, _ := schema.(*program.OutputsSchema)
	return s.ev.Eval(ctx, edge, sch, journal)
}

// outputsSchemaResolverFor closes over the BriefLoader's per-feature
// schema map so the scheduler can resolve an upstream feature's
// declared OutputsSchema at predicate-eval time. The returned closure
// boxes the *program.OutputsSchema into the scheduler-side `any` so
// the scheduler stays import-free of package program.
func outputsSchemaResolverFor(loader *program.BriefLoader) scheduler.OutputsSchemaResolver {
	return func(featureID string) (any, bool) {
		sch, ok := loader.OutputsSchemaForFeature(featureID)
		if !ok {
			return nil, false
		}
		return sch, true
	}
}

// spawnerSet bundles the three handles a serve invocation needs to
// wire the Spawner + Reaper. Only the claude backend populates
// Killer + Worktrees; the stub leaves them nil so runServe knows to
// skip the Reaper.
type spawnerSet struct {
	Spawner   spawner.Spawner
	Killer    reaper.ChildKiller
	Worktrees *spawner.WorktreeManager
}

// buildSpawner returns the spawnerSet selected by the -spawner flag.
func buildSpawner(name, repoRoot, claudeBin, baseRef string) (spawnerSet, error) {
	switch name {
	case "", "stub":
		return spawnerSet{Spawner: spawner.NewStub()}, nil
	case "claude":
		wm, err := spawner.NewWorktreeManager(spawner.WorktreeManagerConfig{RepoRoot: repoRoot})
		if err != nil {
			return spawnerSet{}, fmt.Errorf("worktree manager: %w", err)
		}
		cs, err := spawner.NewClaudeSpawner(wm, spawner.ClaudeSpawnerConfig{
			Command: claudeBin,
			BaseRef: baseRef,
		})
		if err != nil {
			return spawnerSet{}, fmt.Errorf("claude spawner: %w", err)
		}
		return spawnerSet{Spawner: cs, Killer: cs, Worktrees: wm}, nil
	default:
		return spawnerSet{}, fmt.Errorf("unknown spawner %q (want stub|claude)", name)
	}
}

// loadBriefKeyring reads the HMAC key from REGATTA_HMAC_KEY (or the
// env var named by REGATTA_HMAC_KEY_ENV when set) and returns it as a
// one-entry keyring keyed by REGATTA_HMAC_KEY_ID (default "k1", which
// matches the `program plan -hmac-key-id` default so the offline e2e
// flow round-trips without per-key wiring). Empty keyring when unset --
// BriefLoader skips brief verification only when no briefs exist on
// disk; a brief landing without a configured key surfaces the misconfig
// to the operator via brief.rejected logs.
func loadBriefKeyring() map[string][]byte {
	envName := os.Getenv("REGATTA_HMAC_KEY_ENV")
	if envName == "" {
		envName = "REGATTA_HMAC_KEY"
	}
	v := os.Getenv(envName)
	if v == "" {
		return map[string][]byte{}
	}
	keyID := os.Getenv("REGATTA_HMAC_KEY_ID")
	if keyID == "" {
		keyID = "k1"
	}
	return map[string][]byte{keyID: []byte(v)}
}
