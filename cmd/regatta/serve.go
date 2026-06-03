// serve wires the orchestrator daemon; spawner selection, scheduler-side
// evaluator/schema adapters, and the brief keyring loader are all
// serve-only concerns and ship together here.
package main

import (
	"context"
	"encoding/hex"
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

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/authz"
	"github.com/trilamsr/regatta/internal/authz/policies/disk"
	"github.com/trilamsr/regatta/internal/authz/policies/embedded"
	"github.com/trilamsr/regatta/internal/authz/policies/reload"
	"github.com/trilamsr/regatta/internal/canon/approvaltoken"
	"github.com/trilamsr/regatta/internal/config"
	validateconfig "github.com/trilamsr/regatta/internal/config/validate"
	"github.com/trilamsr/regatta/internal/cost/spend"
	"github.com/trilamsr/regatta/internal/gates/approval"
	"github.com/trilamsr/regatta/internal/orchestrator"
	"github.com/trilamsr/regatta/internal/orchestrator/adapter"
	"github.com/trilamsr/regatta/internal/orchestrator/adaptersync"
	"github.com/trilamsr/regatta/internal/orchestrator/prwatch"
	"github.com/trilamsr/regatta/internal/orchestrator/reaper"
	"github.com/trilamsr/regatta/internal/orchestrator/rejectionrouter"
	"github.com/trilamsr/regatta/internal/orchestrator/scheduler"
	"github.com/trilamsr/regatta/internal/orchestrator/spawner"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
	"github.com/trilamsr/regatta/internal/program"
	"github.com/trilamsr/regatta/internal/secrets"
	"github.com/trilamsr/regatta/internal/web"
)

// defaultListenerAddr matches spec §1.3 — `--addr` default is `:8080`.
const defaultListenerAddr = ":8080"

// listenerShutdownBudget mirrors the existing serve.go signal-shutdown contract: 5 s to drain inflight requests before forced exit.
const listenerShutdownBudget = 5 * time.Second

// reconcilerShutdownBudget bounds how long serve waits for the cost-
// reconciler goroutine to exit on signal-shutdown. The goroutine's
// inner select honours ctx.Done immediately on the wait branch, so 5s
// is a generous upper bound — a stuck Tick() is the only path that
// would burn through this budget.
const reconcilerShutdownBudget = 5 * time.Second

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
	noPRWatch := fs.Bool("no-pr-watch", false, "[smoke-test only] disable the PR watcher; running agents stay in 'running' forever (issue #526)")
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

	// Resolve operator credentials from OS keychain / pass / env once
	// at boot per PHASE-AUTONOMY W6. Resolved values are exported into
	// the process env so existing readers (loadBriefKeyring, audit
	// signer, LLM dispatcher, GitHub client) consume them unchanged.
	// SIGHUP triggers Cache.Run to re-resolve + republish atomically.
	bootSecretsCtx, bootSecretsCancel := context.WithCancel(context.Background())
	defer bootSecretsCancel()
	secretCache := secrets.NewCache()
	secretFetcher := secrets.Default(bootSecretsCtx)
	secretCache.Load(bootSecretsCtx, secretFetcher, slogger)
	exportSecretsToEnv(bootSecretsCtx, secretCache, slogger)
	go secretCache.Run(bootSecretsCtx, secretFetcher, slogger)
	// Re-export on every rotation so SIGHUP-driven token rotation
	// surfaces to existing env-var readers without a full re-exec.
	go watchSecretsExport(bootSecretsCtx, secretCache, slogger)

	// clock is the single composition-root wall-clock source. Threading
	// one source through every subsystem Config (scheduler, orchestrator,
	// reaper, listener) means a future --clock-source flag or a test
	// harness that swaps serve.New can fix one fake clock and have
	// latency metrics, brief.produced_at, and gate audit timestamps all
	// move together. Each subsystem already nil-defaults to time.Now;
	// the explicit wiring exists to remove the silent-default footgun.
	clock := time.Now

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

	costKeyring, costKeyID := loadBriefKeyringWithActive()
	costKey := costKeyring[costKeyID]
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
		// Issue #80: durable audit sink under the existing brief HMAC
		// key. Zero-key deployments (no REGATTA_HMAC_KEY) fall back to
		// slog-only retention; the BriefAuditConfig.enabled() guard
		// inside the loader keeps the cost zero in that case.
		Audit: program.BriefAuditConfig{
			Key:      costKey,
			KeyID:    costKeyID,
			TenantID: substrate.DefaultTenantID,
			RunID:    "brief-loader",
		},
	})
	if err != nil {
		logger.Printf("brief loader: %v", err)
		return 2
	}
	gate, gateResolver, err := buildApprovalGate(db, *repoRoot, clock, slogger)
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
		Clock:          clock,
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
		Clock:             clock,
	})
	if set.Worktrees != nil {
		o.SetReaper(reaper.New(reaper.Config{
			DB:     db,
			WM:     set.Worktrees,
			Killer: set.Killer,
			Logger: slogger,
			Clock:  clock,
		}))
	}
	// RejectionRouter wakes agents on AI-gate rejections and labels the
	// PR `needs-human` after K=3. Defaults match docs/design.md §Failure
	// modes; no regatta.yaml keys are introduced for MVR-1 — operators
	// who want richer routing land it when a real customer use-case
	// shows up.
	o.SetRejectionRouter(buildRejectionRouter(db, rejectionrouter.GHLabeler{}, slogger))

	// PRWatcher drives the running→pr_open transition via gh CLI
	// head-SHA polling. Spec docs/engineer/specs/2026-06-02-orchestrator-pr-watcher.md
	// + cluster fixes for issues #520, #521, #522, #526. --no-pr-watch
	// keeps the daemon bootable in smoke-test fixtures that have no
	// GitHub remote; production always wires the Watcher.
	if !*noPRWatch && set.Worktrees != nil {
		watcher, err := prwatch.New(prwatch.Config{
			DB:           db,
			BranchFn:     set.Worktrees.BranchFor,
			Lister:       prwatch.NewGHCLILister(),
			VersionProbe: prwatch.NewGHCLIVersionProbe(),
			Logger:       slogger,
		})
		if err != nil {
			logger.Printf("pr-watch: %v", err)
			return 2
		}
		if err := watcher.Start(ctx); err != nil {
			logger.Printf("pr-watch: %v", err)
			return 2
		}
		o.SetPRWatcher(watcher)
	} else {
		slogger.Info("orchestrator.starting", "pr_watch_enabled", false)
	}

	if err := o.Recover(ctx); err != nil {
		logger.Printf("recover: %v", err)
		return 1
	}

	// W8 T1/T-HR: Hydrate the OPA authorizer + (optionally) start the
	// disk-driven hot-reload goroutine. The Authorizer is plumbed to
	// listenerConfig so W8 T3 can wire it through the web handler
	// without touching boot code. Hydrate failure is fail-loud because a
	// broken authz bundle MUST surface at boot — a serve that runs with
	// an unhydrated store would deny every request mid-evaluation.
	authzr, err := buildAuthorizer(ctx, *repoRoot, slogger)
	if err != nil {
		logger.Printf("authz: %v", err)
		return 2
	}

	httpSrv, err := bootListener(listenerConfig{
		UI:         *ui,
		Addr:       *addr,
		DB:         db,
		Keyring:    approvaltoken.MapKeyring(loadBriefKeyring()),
		Clock:      clock,
		Authorizer: authzr,
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

	// Cost-reconciler goroutine: opt-in per safety.cost.reconcile_interval.
	// Run() loops forever swallowing tick errors (R6), so startup never
	// blocks on a transient Anthropic Cost API failure. Ctx-cancel via
	// SIGINT/SIGTERM exits the goroutine; the deferred wait pins graceful
	// shutdown — `regatta serve` does not return until the reconciler
	// has cleanly stopped (bounded by reconcilerShutdownBudget).
	reconcileSettings := loadCostReconcileSettingsFor(*repoRoot)
	reconcileDone, reconcileStarted := startReconciler(ctx, reconcileWiring{
		DB:                db,
		Key:               costKey,
		KeyID:             costKeyID,
		ReconcileInterval: reconcileSettings.ReconcileInterval,
		UsageAPIKeyEnv:    reconcileSettings.UsageAPIKeyEnv,
		Logger:            slogger,
	})
	if reconcileStarted {
		defer func() {
			select {
			case <-reconcileDone:
			case <-time.After(reconcilerShutdownBudget):
				logger.Printf("cost reconciler: shutdown timeout after %s", reconcilerShutdownBudget)
			}
		}()
	}

	// W1 alarm-webhook: opt-in per regatta.yaml::alarm_webhook.listen_addr.
	// The handler ships in internal/alarmwebhook; both this in-process
	// path and cmd/regatta-alarm-webhook drive the same Serve helper so
	// behaviour stays byte-equal. Disabled when listen_addr is empty.
	startAlarmWebhook(ctx, *repoRoot, slogger)

	if *tickOnce {
		if err := o.PollOnce(ctx); err != nil {
			logger.Printf("poll: %v", err)
			return 1
		}
		if err := o.ScheduleOnce(ctx); err != nil {
			logger.Printf("schedule: %v", err)
			return 1
		}
		// Mirror the Run loop order so `--tick-once` is a faithful
		// single-shot rehearsal of one daemon tick.
		if err := o.RouteRejections(ctx); err != nil {
			logger.Printf("route rejections: %v", err)
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

// loadBriefKeyring returns the configured HMAC keyring for brief
// verification + approval-token verify. The verify-side consumes every
// entry; signers must use loadBriefKeyringWithActive to pick the
// active (write) key.
func loadBriefKeyring() map[string][]byte {
	keys, _ := loadBriefKeyringWithActive()
	return keys
}

// loadBriefKeyringWithActive resolves the multi-key keyring + active
// keyID per docs/engineer/specs/2026-06-02-s3-t3-key-rotation-drill.md
// §3.3. Precedence: REGATTA_HMAC_KEYRING (colon-comma list) overrides
// the legacy REGATTA_HMAC_KEY single-key path. Active = last entry in
// keyring order (rotation IS append) unless REGATTA_HMAC_KEY_ID
// overrides — operators recovering under an older key set the explicit
// keyID and sign under it. Empty keyring returns ("", "") and lets the
// brief loader surface the misconfig via brief.rejected logs.
func loadBriefKeyringWithActive() (map[string][]byte, string) {
	if raw := os.Getenv("REGATTA_HMAC_KEYRING"); raw != "" {
		keys, order, err := parseBriefKeyring(raw)
		if err != nil {
			// Boot-time misconfig should fail loud; we surface via empty
			// keyring + brief.rejected. Future PR-C wires log.Fatal at
			// serve entry.
			return map[string][]byte{}, ""
		}
		active := order[len(order)-1]
		if override := os.Getenv("REGATTA_HMAC_KEY_ID"); override != "" {
			if _, ok := keys[override]; ok {
				active = override
			}
		}
		return keys, active
	}

	envName := os.Getenv("REGATTA_HMAC_KEY_ENV")
	if envName == "" {
		envName = "REGATTA_HMAC_KEY"
	}
	v := os.Getenv(envName)
	if v == "" {
		return map[string][]byte{}, ""
	}
	keyID := os.Getenv("REGATTA_HMAC_KEY_ID")
	if keyID == "" {
		keyID = "k1"
	}
	return map[string][]byte{keyID: []byte(v)}, keyID
}

// parseBriefKeyring decodes the colon-comma multi-key env format
// (`k1:hex,k2:hex,...`) into a keyring map plus the keyID order. Hex
// over JSON because kubectl + Docker `--env` accept colons + commas
// unquoted; JSON forces shell-quote drama (spec §5 reviewer note).
// Returns ErrDuplicateKeyID when two entries share a keyID — silent
// overwrite would lose the older verify path mid-rotation.
func parseBriefKeyring(raw string) (map[string][]byte, []string, error) {
	pairs := strings.Split(raw, ",")
	keys := make(map[string][]byte, len(pairs))
	order := make([]string, 0, len(pairs))
	for i, pair := range pairs {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		idx := strings.IndexByte(pair, ':')
		if idx <= 0 || idx == len(pair)-1 {
			return nil, nil, fmt.Errorf("keyring entry %d: want 'keyID:hex', got %q", i, pair)
		}
		keyID := strings.TrimSpace(pair[:idx])
		hexMat := strings.TrimSpace(pair[idx+1:])
		decoded, err := hex.DecodeString(hexMat)
		if err != nil {
			return nil, nil, fmt.Errorf("keyring entry %s: hex decode: %w", keyID, err)
		}
		if len(decoded) < schemas.MinKeyLen {
			return nil, nil, fmt.Errorf("keyring entry %s: %w (got %d bytes)", keyID, schemas.ErrWeakKey, len(decoded))
		}
		if _, dup := keys[keyID]; dup {
			return nil, nil, fmt.Errorf("keyring entry %s: duplicate keyID", keyID)
		}
		keys[keyID] = decoded
		order = append(order, keyID)
	}
	if len(order) == 0 {
		return nil, nil, fmt.Errorf("keyring is empty")
	}
	return keys, order, nil
}

// buildRejectionRouter wires the RejectionRouter the orchestrator
// drives per tick. Defaults — K=3 + label=needs-human — come from the
// router package; we pass them implicitly by leaving the Config
// zero-valued. The labeler is injected so tests can substitute a
// capturing fake without spawning gh; serve.go production wiring hands
// in rejectionrouter.GHLabeler{} which shells out to gh.
func buildRejectionRouter(db *state.DB, labeler rejectionrouter.PRLabeler, logger *slog.Logger) *rejectionrouter.Router {
	return rejectionrouter.New(rejectionrouter.Config{
		DB:      db,
		Labeler: labeler,
		Logger:  logger,
	})
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
func buildApprovalGate(db *state.DB, repoRoot string, clock func() time.Time, logger *slog.Logger) (scheduler.ApprovalGate, scheduler.GateResolver, error) {
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
		byName[g.Name] = g
	}

	keyring, kid := approvalKeyring()
	g := approval.NewGate(db, approval.NewStubNotifier(logger), keyring, kid, clock, logger)
	resolver := scheduler.GateResolver(func(wi state.WorkItem) (approval.Config, bool) {
		c, ok := byName[wi.Lane]
		return c, ok
	})
	return g, resolver, nil
}

// listenerConfig is the bootListener seam; the test harness drives it directly so integration tests exercise the real boot path.
type listenerConfig struct {
	UI         bool
	Addr       string
	DB         *state.DB
	Keyring    approvaltoken.Keyring
	Clock      func() time.Time
	// Authorizer is the OPA-backed gate built at runServe boot. Pre-T3
	// the web handler does not yet consume it; the field is plumbed so
	// W8 T3 can pick it up without touching listener wiring.
	Authorizer *authz.OPAAuthorizer
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

// buildAuthorizer constructs the W8 OPA authorizer and (when the
// operator declares safety.authz.policy_dir) spawns the hot-reload
// goroutine bound to ctx. Empty / missing regatta.yaml ⇒ embed.FS
// default-deny only; the authorizer is still hydrated so call sites
// always see a non-nil store.
//
// Reloader lifecycle: bound to ctx — SIGINT/SIGTERM cancel the parent,
// which drains both the watcher and signal goroutines. SIGHUP is
// claimed by the reloader's own signal.Notify, which does not steal
// from the parent context's signal.NotifyContext (it listens on a
// disjoint signal set).
func buildAuthorizer(ctx context.Context, repoRoot string, slogger *slog.Logger) (*authz.OPAAuthorizer, error) {
	cfg, _ := validateconfig.LoadConfigFile(filepath.Join(repoRoot, "regatta.yaml"))
	authzCfg := cfg.AuthzConfig()

	// disk.Loader with embedded fallback is the canonical wiring: an
	// empty / missing policy_dir falls through to embed.FS so single-
	// tenant deployments boot zero-config.
	loader := &disk.Loader{Fallback: embedded.NewLoader()}
	if authzCfg != nil && authzCfg.PolicyDir != "" {
		dir := authzCfg.PolicyDir
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(repoRoot, dir)
		}
		loader.Dir = dir
	}

	az, err := authz.NewOPAAuthorizer(authz.Config{Loader: loader})
	if err != nil {
		return nil, fmt.Errorf("new authorizer: %w", err)
	}
	if err := az.Hydrate(ctx); err != nil {
		return nil, fmt.Errorf("hydrate: %w", err)
	}

	// Reloader only spins up when policy_dir is set — there is nothing
	// to hot-reload for an embed.FS-only deployment.
	if loader.Dir == "" {
		return az, nil
	}

	r := &reload.Reloader{
		Authorizer: az,
		Loader:     loader,
		Tenant:     authz.DefaultTenant,
		Logger:     slogger,
	}
	if d := parseAuthzDebounce(authzCfg.ReloadDebounce); d > 0 {
		r.Debounce = d
	}
	if authzCfg.ReloadSighup != nil && !*authzCfg.ReloadSighup {
		r.DisableSighup = true
	}
	if authzCfg.ReloadFsnotify != nil && !*authzCfg.ReloadFsnotify {
		r.DisableFsnotify = true
	}
	// OnStart fires after BOTH triggers (watcher + SIGHUP handler) are
	// registered — commit 6341256 added the SIGHUP-readiness gate so
	// operators (and tests) can rely on this log as the
	// pid-can-receive-SIGHUP signal.
	r.OnStart = func() {
		slogger.Info("authz reload: ready",
			"policy_dir", loader.Dir,
			"sighup_enabled", !r.DisableSighup,
			"fsnotify_enabled", !r.DisableFsnotify,
		)
	}
	go func() {
		if err := r.Run(ctx); err != nil {
			slogger.Error("authz reload: exited", "err", err)
		}
	}()
	return az, nil
}

// parseAuthzDebounce parses the CUE-validated `^[0-9]+(ms|s)$` debounce
// string. Returns 0 on empty / parse error — reload.Reloader.Run then
// falls back to reload.DefaultDebounce. The CUE schema already rejects
// malformed values at validate-config time, so the parse-error branch
// covers only a forward-compat drift between schema + Go parsing.
func parseAuthzDebounce(s string) time.Duration {
	if s == "" {
		return 0
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0
	}
	return d
}

// approvalKeyring reuses the brief HMAC key for approval-token signing.
// Operators set REGATTA_HMAC_KEY once and both surfaces light up; an
// empty key returns an empty MapKeyring so NewGate's constructor guard
// fires only when the operator has at least one gate defined.
func approvalKeyring() (approvaltoken.Keyring, string) {
	keys, active := loadBriefKeyringWithActive()
	if active == "" {
		active = "k1"
	}
	return approvaltoken.MapKeyring(keys), active
}
