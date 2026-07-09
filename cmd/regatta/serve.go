// serve wires the orchestrator daemon; per-subsystem build helpers
// (spawner, scheduler, keyring, web, authz, config) ship in sibling
// wire_*.go files so this file stays the boot orchestration root —
// flag parsing, signal/ctx wiring, listener bind, and shutdown order.
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/adaptersync"
	"github.com/trilamsr/regatta/internal/orchestrator/reaper"
	"github.com/trilamsr/regatta/internal/orchestrator/rejectionrouter"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
	"github.com/trilamsr/regatta/internal/program"
	"github.com/trilamsr/regatta/internal/secrets"
)

// defaultListenerAddr matches spec §1.3 — `--addr` default is `:8080`.
const defaultListenerAddr = ":8080"

// listenerShutdownBudget mirrors the existing serve.go signal-shutdown contract: 5 s to drain inflight requests before forced exit.
const listenerShutdownBudget = 5 * time.Second

// reconcilerShutdownBudget bounds serve's wait for the cost-reconciler goroutine on signal-shutdown; only a stuck Tick() would exhaust this.
const reconcilerShutdownBudget = 5 * time.Second

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

// logFormatFlag validates `--log-format=text|json` at Parse-time so flag.ExitOnError surfaces a clear error + exit 2 before any startup work (#117).
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
	bootStart := captureBootStart()
	f, ferr := parseServeFlagsValidated(args)
	if ferr != nil {
		return reportServeFlagError(ferr)
	}
	publicHost, publicURLErr := parsePublicURL(f.PublicURL)

	logger := log.New(os.Stderr, "regatta: ", log.LstdFlags|log.Lmicroseconds)
	if publicURLErr != nil {
		logger.Printf("--public-url: %v", publicURLErr)
		return 2
	}
	// logFormat was validated at Parse-time; the err branch fires only
	// if logFormatFlag.Set and newLogHandler ever drift.
	handler, err := newLogHandler(string(f.LogFormat), os.Stderr)
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
	secretFetcher, sfErr := buildSecretFetcherFromRepo(bootSecretsCtx, f.RepoRoot, slogger)
	if sfErr != nil {
		logger.Printf("secrets config: %v", sfErr)
		return 2
	}
	secretCache.Load(bootSecretsCtx, secretFetcher, slogger)
	exportSecretsToEnv(bootSecretsCtx, secretCache, slogger)
	// secretCache.Run + watchSecretsExport are wired AFTER signal-aware
	// `ctx` is installed below so SIGTERM cancels them cleanly. Using
	// bootSecretsCtx here would kill them when runServe returns (any
	// boot-error path), silently terminating the SIGHUP rotation loop.
	// R31-I1 finding.

	// clock is the single composition-root wall-clock source. Threading
	// one source through every subsystem Config (scheduler, orchestrator,
	// reaper, listener) means a future --clock-source flag or a test
	// harness that swaps serve.New can fix one fake clock and have
	// latency metrics, brief.produced_at, and gate audit timestamps all
	// move together. Each subsystem already nil-defaults to time.Now;
	// the explicit wiring exists to remove the silent-default footgun.
	clock := time.Now

	if err := runBootPreflights(f, logger); err != nil {
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	obsShutdown, obsErr := wireObservability(ctx, slogger)
	if obsErr != nil {
		logger.Printf("%v", obsErr)
		return 2
	}
	defer obsShutdown()

	if err := ensureDBParent(f.DBPath); err != nil {
		logger.Printf("db path: %v", err)
		return 2
	}
	db, err := state.Open(ctx, state.DSN(f.DBPath))
	if err != nil {
		logger.Printf("open db: %v", err)
		return 2
	}
	defer func() { _ = db.Close() }()

	go watchSecretsExport(ctx, secretCache, slogger)
	startSecretsRotationLoop(ctx, secretCache, secretFetcher, slogger, db)

	ad, err := buildWorkItemSource(f, slogger)
	if err != nil {
		logger.Printf("adapter: %v", err)
		return 2
	}
	applyDefaultLaneCap(&f, slogger)

	costKeyring, costKeyID := loadBriefKeyringWithActive()
	costKey := costKeyring[costKeyID]
	// haltOnCredit cancels the run context when an agent exits
	// credit-exhausted so dispatch stops instead of burning further
	// invocations against a dead account (MAY-78).
	haltOnCredit := func() {
		slogger.Error("orchestrator.dispatch_halt", "reason", "provider_credit_exhausted")
		stop()
	}
	set, err := buildSpawner(f.SpawnerName, f.RepoRoot, f.ClaudeBin, f.BaseRef, slogger, db, costKey, costKeyID, haltOnCredit)
	if err != nil {
		logger.Printf("spawner: %v", err)
		return 2
	}
	if set.Cleanup != nil {
		defer set.Cleanup()
	}
	warnIfStubWithGitHubIssues(f.SpawnerName, f.RepoRoot, slogger)

	briefsDir := filepath.Join(f.RepoRoot, ".regatta", "programs")
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
	gate, gateResolver, err := buildApprovalGate(db, f.RepoRoot, clock, slogger)
	if err != nil {
		logger.Printf("approval gates: %v", err)
		return 2
	}
	costCapEnf, err := buildCostCapEnforcer(db, f.RepoRoot, clock, slogger)
	if err != nil {
		logger.Printf("cost cap: %v", err)
		return 2
	}
	// Built BEFORE the scheduler so OnGatesPass (#612) picks up
	// Coordinator+Worker+LowRiskGate from Config. Worker is nil when
	// --auto-merge=false, keeping OnGatesPass a no-op by default.
	mergeCoord, mergeWorker, lowRiskGate, err := buildMergeWiring(db, f.RepoRoot, f.AutoMerge, slogger, costKey, costKeyID)
	if err != nil {
		logger.Printf("merge wiring: %v", err)
		return 2
	}

	sched := buildScheduler(db, f, schedulerDeps{
		Evaluator:        schedulerEvaluator{evaluator},
		OutputsSchemas:   outputsSchemaResolverFor(loader),
		Gate:             gate,
		GateResolver:     gateResolver,
		CostCap:          costCapEnf,
		Clock:            clock,
		MergeCoordinator: mergeCoord,
		MergeWorker:      mergeWorker,
		LowRiskGate:      lowRiskGate,
		Logger:           slogger,
	})
	o, healthHB := newOrchestrator(orchestratorWiring{
		Syncer:    syncer,
		Loader:    loader,
		DB:        db,
		Scheduler: sched,
		Spawner:   set.Spawner,
		Flags:     f,
		Logger:    slogger,
		Clock:     clock,
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

	if err := startPRWatcher(ctx, o, db, set, f.RepoRoot, f.NoPRWatch, slogger); err != nil {
		logger.Printf("%v", err)
		return 2
	}

	// PHASE AUTONOMY §11 W2 c2: install the merge coordinator built
	// above so Recover() drives Reconcile against stranded
	// awaiting_merge agents. The auto-merge worker only runs when
	// --auto-merge=true (see buildMergeWiring); the c2 default stays
	// operator-observable-equivalent to the pre-c2 daemon.
	if mergeCoord != nil {
		o.SetMergeCoordinator(mergeCoord)
	}
	defer startMergeWorker(ctx, mergeWorker)()

	if err := startReviewReconciler(ctx, slogger); err != nil {
		logger.Printf("%v", err)
		return 2
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
	authzr, err := buildAuthorizer(ctx, f.RepoRoot, slogger)
	if err != nil {
		logger.Printf("authz: %v", err)
		return 2
	}

	httpSrv, err := bootListener(buildListenerConfig(f, db, clock, authzr, publicHost, healthHB))
	if err != nil {
		logger.Printf("listener boot: %v", err)
		return 2
	}
	if httpSrv != nil {
		stop, err := startHTTPServer(httpSrv, logger)
		if err != nil {
			logger.Printf("listener bind: %v", err)
			return 2
		}
		defer stop()
	}

	emitServeStarted(ctx, db, bootStart, slogger)
	// Cost-reconciler goroutine: opt-in per safety.cost.reconcile_interval.
	// Run() loops forever swallowing tick errors (R6), so startup never
	// blocks on a transient Anthropic Cost API failure. Ctx-cancel via
	// SIGINT/SIGTERM exits the goroutine; the deferred wait pins graceful
	// shutdown — `regatta serve` does not return until the reconciler
	// has cleanly stopped (bounded by reconcilerShutdownBudget).
	reconcileSettings := loadCostReconcileSettingsFor(f.RepoRoot)
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

	// W1 alerthook: opt-in per regatta.yaml::alarm_webhook.listen_addr.
	// The handler ships in internal/alerthook; both this in-process
	// path and cmd/regatta-alarm-webhook drive the same Serve helper so
	// behaviour stays byte-equal. Disabled when listen_addr is empty.
	startAlerthook(ctx, f.RepoRoot, slogger)

	if f.TickOnce {
		if err := runTickOnce(ctx, o); err != nil {
			logger.Printf("%v", err)
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
