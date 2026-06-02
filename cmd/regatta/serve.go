// serve wires the orchestrator daemon; spawner selection, scheduler-side
// evaluator/schema adapters, and the brief keyring loader are all
// serve-only concerns and ship together here.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/trilamsr/regatta/internal/canon"
	"github.com/trilamsr/regatta/internal/config"
	validateconfig "github.com/trilamsr/regatta/internal/config/validate"
	"github.com/trilamsr/regatta/internal/cost/spend"
	"github.com/trilamsr/regatta/internal/gates/approval"
	"github.com/trilamsr/regatta/internal/orchestrator"
	"github.com/trilamsr/regatta/internal/orchestrator/adapter"
	"github.com/trilamsr/regatta/internal/orchestrator/adaptersync"
	"github.com/trilamsr/regatta/internal/orchestrator/reaper"
	"github.com/trilamsr/regatta/internal/orchestrator/scheduler"
	"github.com/trilamsr/regatta/internal/orchestrator/spawner"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/program"
	"github.com/trilamsr/regatta/internal/web"
)

// defaultListenerAddr matches spec §1.3 — `--addr` default is `:8080`.
const defaultListenerAddr = ":8080"

// listenerShutdownBudget mirrors the existing serve.go signal-shutdown contract: 5 s to drain inflight requests before forced exit.
const listenerShutdownBudget = 5 * time.Second

// healthzBody is the spec §3.3 row 6 literal: `200 OK` with body `ok` and zero DB queries.
const healthzBody = "ok\n"

// defaultLogFormat is the value used when --log-format is omitted —
// the `text (default)` contract from #117.
const defaultLogFormat = "text"

// logFormatJSON is the operator-facing `json` token, shared by both
// `serve --log-format=json` and `approval list --format=json`.
// Renaming it is a deliberate CLI change.
const logFormatJSON = "json"

// newLogHandler is the single source of truth for accepted
// --log-format values; logFormatFlag.Set delegates here so the
// valid-set lives in one switch.
func newLogHandler(format string, w io.Writer) (slog.Handler, error) {
	switch format {
	case defaultLogFormat:
		return slog.NewTextHandler(w, nil), nil
	case logFormatJSON:
		return slog.NewJSONHandler(w, nil), nil
	default:
		return nil, fmt.Errorf("invalid log format %q (want %s|%s)", format, defaultLogFormat, logFormatJSON)
	}
}

// logFormatFlag validates `--log-format=text|json` at Parse time so
// flag.ExitOnError surfaces a clear error + exit 2 before any startup
// work happens (#117).
type logFormatFlag string

func (l *logFormatFlag) String() string { return string(*l) }

func (l *logFormatFlag) Set(s string) error {
	if _, err := newLogHandler(s, io.Discard); err != nil {
		return fmt.Errorf("--log-format: %w", err)
	}
	*l = logFormatFlag(s)
	return nil
}

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
	itemsRoot := fs.String("items-root", ".", "Repo root containing .regatta/items/*.md (overrides regatta.yaml spec_adapter.root when set explicitly)")
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
	logFormat := logFormatFlag(defaultLogFormat)
	fs.Var(&logFormat, "log-format", "Structured-log handler: text | json")
	addr := fs.String("addr", defaultListenerAddr, "HTTP listener bind address when --ui=true")
	ui := fs.Bool("ui", true, "Boot the operator HTTP listener; --ui=false skips bind entirely")
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
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "items-root" {
			itemsRootExplicit = true
		}
	})
	if !itemsRootExplicit {
		if yamlRoot, ok := loadMarkdownCatalogRoot(*repoRoot); ok {
			resolved := yamlRoot
			if !filepath.IsAbs(resolved) {
				resolved = filepath.Join(*repoRoot, resolved)
			}
			*itemsRoot = resolved
		}
	}

	logger := log.New(os.Stderr, "regatta: ", log.LstdFlags|log.Lmicroseconds)
	// logFormat was validated at Parse-time; the err branch fires only
	// if logFormatFlag.Set and newLogHandler ever drift.
	handler, err := newLogHandler(string(logFormat), os.Stderr)
	if err != nil {
		logger.Printf("log-format: %v", err)
		return 2
	}
	slogger := slog.New(handler)

	// Loud-at-boot before any DB open (spec §1.3 open-q 9.8): refuse to
	// start the listener when its HMAC key dependency is missing.
	if err := preflightUIBoot(*ui); err != nil {
		logger.Printf("%v", err)
		return 2
	}

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

	costKey, costKeyID := firstKey(loadBriefKeyring())
	set, err := buildSpawner(*spawnerName, *repoRoot, *claudeBin, *baseRef, slogger, db, costKey, costKeyID)
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

	httpSrv, err := bootListener(listenerConfig{
		UI:      *ui,
		Addr:    *addr,
		DB:      db,
		Keyring: canon.MapKeyring(loadBriefKeyring()),
		Clock:   time.Now,
	})
	if err != nil {
		logger.Printf("listener boot: %v", err)
		return 2
	}
	if httpSrv != nil {
		serveErr := make(chan error, 1)
		go func() { serveErr <- httpSrv.ListenAndServe() }()
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), listenerShutdownBudget)
			defer cancel()
			_ = httpSrv.Shutdown(shutdownCtx)
			if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.Printf("listener: %v", err)
			}
		}()
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
func buildSpawner(name, repoRoot, claudeBin, baseRef string, logger *slog.Logger, db *state.DB, costKey []byte, costKeyID string) (spawnerSet, error) {
	switch name {
	case "", "stub":
		return spawnerSet{Spawner: spawner.New(spawner.Config{Logger: logger})}, nil
	case "claude":
		wm, err := spawner.NewWorktreeManager(spawner.WorktreeManagerConfig{RepoRoot: repoRoot})
		if err != nil {
			return spawnerSet{}, fmt.Errorf("worktree manager: %w", err)
		}
		cfg := spawner.ClaudeSpawnerConfig{Command: claudeBin, BaseRef: baseRef}
		if db != nil && len(costKey) > 0 {
			cfg.OnResultEventFor = spend.SpawnerCallback(db.SQL(),
				spend.WriteOptions{Key: costKey, KeyID: costKeyID},
				spend.CallScope{WrittenBy: "claude-spawner"})
		}
		cs, err := spawner.NewClaudeSpawner(wm, cfg)
		if err != nil {
			return spawnerSet{}, fmt.Errorf("claude spawner: %w", err)
		}
		return spawnerSet{Spawner: cs, Killer: cs, Worktrees: wm}, nil
	default:
		return spawnerSet{}, fmt.Errorf("unknown spawner %q (want stub|claude)", name)
	}
}

// firstKey picks one (keyID, key) pair from loadBriefKeyring's map so
// the spawner-side cost-governor callback can sign substrate rows with
// the same key the brief loader + approval gate use — single HMAC key
// per process; ranging the map and breaking is intentional.
func firstKey(keys map[string][]byte) ([]byte, string) {
	for k, v := range keys {
		return v, k
	}
	return nil, ""
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

// listenerConfig is the bootListener seam; the test harness drives it directly so integration tests exercise the real boot path.
type listenerConfig struct {
	UI      bool
	Addr    string
	DB      *state.DB
	Keyring canon.Keyring
	Clock   func() time.Time
}

// preflightUIBoot fires BEFORE state.Open so the operator sees the HMAC misconfig at the loud-at-boot moment (spec §1.3 open-q 9.8) rather than as a render-time lie.
func preflightUIBoot(ui bool) error {
	if !ui {
		return nil
	}
	if os.Getenv("REGATTA_HMAC_KEY") != "" {
		return nil
	}
	if envName := os.Getenv("REGATTA_HMAC_KEY_ENV"); envName != "" && os.Getenv(envName) != "" {
		return nil
	}
	return fmt.Errorf("--ui requires REGATTA_HMAC_KEY (or REGATTA_HMAC_KEY_ENV) to be set; refusing to boot")
}

// bootListener returns a configured *http.Server when --ui=true, or nil when --ui=false so the caller skips the listen syscall entirely.
func bootListener(cfg listenerConfig) (*http.Server, error) {
	if !cfg.UI {
		return nil, nil
	}
	if err := preflightUIBoot(true); err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthzHandler)
	cbPath, cbHandler := approval.NewHTTPCallback(approval.Dependencies{
		DB:      cfg.DB,
		Keyring: cfg.Keyring,
		Clock:   cfg.Clock,
	})
	mux.Handle(cbPath, cbHandler)
	// W7.1 T4: mount the operator UI scaffold last so http.ServeMux's
	// longest-prefix-wins rule keeps /healthz + /api/approval/callback above
	// the `/` catch-all (TestServe_RootHandlerWiredIntoBootListener pins it).
	webHandler, err := newWebHandler(cfg)
	if err != nil {
		return nil, err
	}
	mux.Handle("/", webHandler)
	addr := cfg.Addr
	if addr == "" {
		addr = defaultListenerAddr
	}
	return &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}, nil
}

// newWebHandler constructs the W7.1 T4 operator UI handler with templates
// loaded from the package's embed.FS at boot. Template parse failure surfaces
// as a bootListener error (spec §3.4 fail-loud) rather than a render-time lie.
// RouteRegistrar is nil pre-T6; cmd/regatta passes the field unchanged.
func newWebHandler(cfg listenerConfig) (http.Handler, error) {
	tmpls, err := web.LoadTemplates(web.AssetsFS())
	if err != nil {
		return nil, fmt.Errorf("web templates: %w", err)
	}
	return web.NewHandler(web.Dependencies{
		DB:             cfg.DB,
		Keyring:        cfg.Keyring,
		Templates:      tmpls,
		Clock:          cfg.Clock,
		RouteRegistrar: nil,
	}), nil
}

// healthzHandler returns the spec §3.3 row 6 liveness probe — zero DB queries, literal `ok\n`, Cache-Control: no-store.
func healthzHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(healthzBody))
}

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
