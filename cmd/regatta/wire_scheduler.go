package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/trilamsr/regatta/internal/config"
	"github.com/trilamsr/regatta/internal/gates/approval"
	"github.com/trilamsr/regatta/internal/orchestrator/merge"
	"github.com/trilamsr/regatta/internal/orchestrator/rejectionrouter"
	"github.com/trilamsr/regatta/internal/orchestrator/scheduler"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/program"
)

// schedulerEvaluator adapts *program.EdgeEvaluator to the scheduler's
// EdgeEvaluator interface. Schema is `any` on the seam so scheduler
// never imports program; this adapter unboxes back to the concrete
// type, nil-defaulting on a missed resolver (matching the runtime
// evaluator's "schema advisory" contract).
type schedulerEvaluator struct {
	ev *program.EdgeEvaluator
}

func (s schedulerEvaluator) Eval(ctx context.Context, edge state.EdgeRow, schema any, journal state.OutputJournalEntry) (bool, string, error) {
	sch, _ := schema.(*program.OutputsSchema)
	return s.ev.Eval(ctx, edge, sch, journal)
}

// outputsSchemaResolverFor boxes the BriefLoader's per-feature schema
// map into the scheduler-side `any` so scheduler stays import-free of
// package program.
func outputsSchemaResolverFor(loader *program.BriefLoader) scheduler.OutputsSchemaResolver {
	return func(featureID string) (any, bool) {
		sch, ok := loader.OutputsSchemaForFeature(featureID)
		if !ok {
			return nil, false
		}
		return sch, true
	}
}

// buildApprovalGate constructs the scheduler-side HITL gate seam from
// regatta.yaml. Missing or empty regatta.yaml yields (nil, nil, nil) so
// repos without approval gates pay zero runtime cost.
//
// MVP-2 W3 resolution policy: gate name == work_item lane. Richer
// routing (per-feature, predicate-CEL) plugs in via a custom
// GateResolver post-MVP; the seam stays scheduler-agnostic. Keyring is
// shared with the brief loader so REGATTA_HMAC_KEY covers both.
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

// buildRejectionRouter wires the RejectionRouter the orchestrator
// drives per tick. Defaults (K=3, label=needs-human) come from the
// router package via Config zero-value. Labeler is injected so tests
// substitute a capturing fake without spawning gh.
func buildRejectionRouter(db *state.DB, labeler rejectionrouter.PRLabeler, logger *slog.Logger) *rejectionrouter.Router {
	return rejectionrouter.New(rejectionrouter.Config{
		DB:      db,
		Labeler: labeler,
		Logger:  logger,
	})
}

// buildMergeWiring constructs the merge.Coordinator (always — needed
// for the c0 recovery sweep) and, when autoMergeEnabled, the Worker
// for the c2 autonomous gh-pr-merge flow. Nil Worker = deferred Stop()
// is a no-op.
//
// gh ≥ 2.40 floor enforced via boot-time probe (spec §9.10, #656):
// --match-head-commit silently fails on 2.39 and older, defeating the
// SHA-pin guard, so the wiring refuses to fire when gh is too old.
// The Coordinator's Reconcile path still wires so the recovery sweep
// stays live.
func buildMergeWiring(db *state.DB, repoRoot string, autoMergeEnabled bool, logger *slog.Logger) (*merge.Coordinator, *merge.Worker, scheduler.LowRiskGate, error) {
	coord, err := merge.New(merge.Config{
		DB:     db,
		Prober: merge.NewGhProber(nil),
		Logger: logger,
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("merge: new coordinator: %w", err)
	}
	if !autoMergeEnabled {
		return coord, nil, nil, nil
	}
	if err := verifyGhVersionFn(context.Background(), logger); err != nil {
		return nil, nil, nil, fmt.Errorf("merge: gh version check: %w", err)
	}
	coord.SetExecutor(merge.GhExecutor{})
	w := merge.NewWorker(coord, 32, logger)
	return coord, w, buildLowRiskGate(repoRoot, autoMergeEnabled), nil
}

// verifyGhVersionFn is the test seam for the boot-time gh-version
// gate (#656). Production = merge.VerifyGhVersion; tests reassign to
// a pinned-version stub so buildMergeWiring stays hermetic.
var verifyGhVersionFn = func(ctx context.Context, logger *slog.Logger) error {
	return merge.VerifyGhVersion(ctx, nil, logger)
}

// schedulerDeps bundles the composition-root dependencies
// buildScheduler threads into scheduler.Config so runServe stays
// orchestration-only (#975 slice 1). Logger added #1227 so
// scheduler.tick_starved (and every other scheduler INFO/WARN)
// carries the configured time= / level= / source= envelope —
// without it slog.Default() emits bare text lines.
type schedulerDeps struct {
	Evaluator        scheduler.EdgeEvaluator
	OutputsSchemas   scheduler.OutputsSchemaResolver
	Gate             scheduler.ApprovalGate
	GateResolver     scheduler.GateResolver
	CostCap          scheduler.CostCapGate
	Clock            func() time.Time
	MergeCoordinator *merge.Coordinator
	MergeWorker      *merge.Worker
	LowRiskGate      scheduler.LowRiskGate
	Meter            metric.Meter
	Logger           *slog.Logger
}

// buildScheduler hoists the scheduler.New(...) composition out of runServe so each subsystem-touching PR stops dirtying serve.go (#975 slice 1).
func buildScheduler(db *state.DB, f serveFlags, deps schedulerDeps) *scheduler.Scheduler {
	return scheduler.New(db, scheduler.Config{
		LaneCaps:           map[string]int(f.LaneCaps),
		LockTTL:            f.LockTTL,
		ParallelCap:        loadSchedulerParallelCap(f.RepoRoot),
		Evaluator:          deps.Evaluator,
		OutputsSchemas:     deps.OutputsSchemas,
		Gate:               deps.Gate,
		GateResolver:       deps.GateResolver,
		CostCap:            deps.CostCap,
		Clock:              deps.Clock,
		MergeCoordinator:   deps.MergeCoordinator,
		MergeWorker:        deps.MergeWorker,
		LowRiskGate:        deps.LowRiskGate,
		Meter:              deps.Meter,
		Logger:             deps.Logger,
		FileScopeExtractor: scheduler.DefaultFileScopeExtractor,
	})
}
