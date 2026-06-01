// serve wires the orchestrator daemon; spawner selection, scheduler-side
// evaluator/schema adapters, and the brief keyring loader are all
// serve-only concerns and ship together here.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/trilamsr/regatta/internal/canon"
	"github.com/trilamsr/regatta/internal/config"
	"github.com/trilamsr/regatta/internal/gates/approval"
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
	// slogger is the structured-logging sink for orchestrator + future
	// scheduler/spawner/reaper wiring (obs-101). Task E will replace
	// this default text handler with --log-format-controlled JSON.
	slogger := slog.New(slog.NewTextHandler(os.Stderr, nil))

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

	set, err := buildSpawner(*spawnerName, *repoRoot, *claudeBin, *baseRef, slogger)
	if err != nil {
		logger.Printf("spawner: %v", err)
		return 2
	}

	briefsDir := filepath.Join(*repoRoot, ".regatta", "programs")
	if err := os.MkdirAll(briefsDir, 0o750); err != nil {
		logger.Printf("mkdir briefs dir: %v", err)
		return 2
	}
	syncer, err := adaptersync.New(adaptersync.Config{Adapter: ad, DB: db, Logger: slogger})
	if err != nil {
		logger.Printf("adaptersync: %v", err)
		return 2
	}
	// Shared evaluator: BriefLoader materialises edges (and could warm
	// the compile cache); Scheduler.Tick step-0 Evals through the same
	// instance so cached cel.Program survives across ticks.
	evaluator := program.NewEdgeEvaluator()
	loader, err := program.NewBriefLoader(program.BriefLoaderConfig{
		FS:        os.DirFS(briefsDir),
		DB:        db,
		Keyring:   loadBriefKeyring(),
		Evaluator: evaluator,
		Logger:    slogger,
	})
	if err != nil {
		logger.Printf("brief loader: %v", err)
		return 2
	}
	gate, gateResolver, err := buildApprovalGate(db, *repoRoot, slogger)
	if err != nil {
		logger.Printf("approval gates: %v", err)
		return 2
	}
	sched := scheduler.New(db, scheduler.Config{
		LaneCaps:       map[string]int(laneCaps),
		LockTTL:        *lockTTL,
		Evaluator:      schedulerEvaluator{evaluator},
		OutputsSchemas: outputsSchemaResolverFor(loader),
		Gate:           gate,
		GateResolver:   gateResolver,
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
		Logger:            slogger,
	})
	if set.Worktrees != nil {
		o.SetReaper(reaper.New(reaper.Config{
			DB:     db,
			WM:     set.Worktrees,
			Killer: set.Killer,
			Logger: slogger,
		}))
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
//
// The logger parameter is consumed only by the stub branch: the stub
// emits spawn.* structured events through it (spec §5.3). ClaudeSpawner
// currently has no slog callsites — its observability lands when real
// stdout/stderr-stream capture ships (#27, #45), at which point the
// logger will thread through ClaudeSpawnerConfig the same way.
func buildSpawner(name, repoRoot, claudeBin, baseRef string, logger *slog.Logger) (spawnerSet, error) {
	switch name {
	case "", "stub":
		return spawnerSet{Spawner: spawner.New(spawner.Config{Logger: logger})}, nil
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

// buildApprovalGate constructs the scheduler-side HITL gate seam from
// regatta.yaml. Missing or empty regatta.yaml yields (nil, nil, nil) so
// repos that have not adopted approval gates pay zero runtime cost —
// scheduler.Config.Gate=nil disables the gate-pass entirely.
//
// MVP-2 W3 resolution policy: gate name == work_item lane. Operators
// who want richer routing (per-feature gates, predicate-CEL) plug in a
// custom GateResolver post-MVP; the seam stays scheduler-agnostic.
//
// The notifier defaults to the stub (audit-only slog) until the
// channel-adapter PR lands. Keyring + kid are shared with the brief
// loader so an operator who configured REGATTA_HMAC_KEY for briefs
// gets approval-token signing for free.
func buildApprovalGate(db *state.DB, repoRoot string, logger *slog.Logger) (scheduler.ApprovalGate, scheduler.GateResolver, error) {
	cfgPath := filepath.Join(repoRoot, "regatta.yaml")
	data, err := os.ReadFile(cfgPath) // #nosec G304 -- repoRoot is an operator-supplied trust boundary; the path is fixed to regatta.yaml under it.
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("read %s: %w", cfgPath, err)
	}
	gates, err := config.LoadApprovalGates(data)
	if err != nil {
		return nil, nil, fmt.Errorf("load approval gates: %w", err)
	}
	if len(gates) == 0 {
		return nil, nil, nil
	}

	byName := make(map[string]approval.Config, len(gates))
	for _, g := range gates {
		byName[g.Name] = convertApprovalGateConfig(g)
	}

	keyring, kid := approvalKeyring()
	g := approval.NewGate(db, approval.NewStubNotifier(logger), keyring, kid, time.Now, logger)
	resolver := scheduler.GateResolver(func(wi state.WorkItem) (approval.Config, bool) {
		c, ok := byName[wi.Lane]
		return c, ok
	})
	return g, resolver, nil
}

// convertApprovalGateConfig adapts the YAML-loaded config.ApprovalGateConfig
// to the runtime approval.Config. Fields are field-for-field identical —
// the two types differ only by yaml vs runtime tags so the package
// boundary stays clean. A future consolidation may collapse them.
func convertApprovalGateConfig(in config.ApprovalGateConfig) approval.Config {
	tiers := make([]approval.TierConfig, len(in.EscalationChain))
	for i, t := range in.EscalationChain {
		tiers[i] = approval.TierConfig{
			Reviewers:         t.Reviewers,
			Roles:             t.Roles,
			Quorum:            t.Quorum,
			PreventSelfReview: t.PreventSelfReview,
			Timeout:           t.Timeout,
			DecisionWindow:    t.DecisionWindow,
		}
	}
	return approval.Config{
		Name:              in.Name,
		RiskClass:         in.RiskClass,
		Reviewers:         in.Reviewers,
		Roles:             in.Roles,
		Quorum:            in.Quorum,
		PreventSelfReview: in.PreventSelfReview,
		Timeout:           in.Timeout,
		DecisionWindow:    in.DecisionWindow,
		OnTimeout:         in.OnTimeout,
		EscalationChain:   tiers,
		PredicateCEL:      in.PredicateCEL,
	}
}

// approvalKeyring reuses the brief HMAC key for approval-token signing.
// Operators set REGATTA_HMAC_KEY once and both surfaces light up; an
// empty key returns an empty MapKeyring so NewGate's constructor guard
// fires only when the operator has at least one gate defined.
func approvalKeyring() (canon.Keyring, string) {
	keys := loadBriefKeyring()
	keyID := os.Getenv("REGATTA_HMAC_KEY_ID")
	if keyID == "" {
		keyID = "k1"
	}
	return canon.MapKeyring(keys), keyID
}
