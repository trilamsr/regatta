package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/trilamsr/regatta/internal/config"
	"github.com/trilamsr/regatta/internal/gates/approval"
	"github.com/trilamsr/regatta/internal/orchestrator/merge"
	"github.com/trilamsr/regatta/internal/orchestrator/rejectionrouter"
	"github.com/trilamsr/regatta/internal/orchestrator/scheduler"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/program"
)

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

// buildMergeWiring constructs the merge.Coordinator (always when this
// daemon runs so the c0 recovery sweep is live) and — when
// autoMergeEnabled is true — the merge.Worker that drives the c2
// autonomous gh-pr-merge flow. The worker is nil when the gate is
// off so the deferred Stop() shutdown is a no-op.
//
// gh ≥ 2.40 floor: when the worker is enabled we run a boot-time
// gh-version probe (spec §9.10, #656). Refuse to wire the worker if
// gh is too old — --match-head-commit silently fails on 2.39 and
// older, defeating the SHA-pin guard. The Coordinator (Reconcile-only
// path) still gets built so the recovery sweep stays live.
//
// The probe seam stays nil-stubbed for now: the production gh-CLI
// prober ships alongside the W7 L4-as-review wiring; until then the
// Coordinator's Reconcile path is exercised by tests + the executor
// is the load-bearing seam for c2.
func buildMergeWiring(db *state.DB, autoMergeEnabled bool, logger *slog.Logger) (*merge.Coordinator, *merge.Worker, error) {
	coord, err := merge.New(merge.Config{
		DB:     db,
		Prober: merge.NewGhProber(nil),
		Logger: logger,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("merge: new coordinator: %w", err)
	}
	if !autoMergeEnabled {
		return coord, nil, nil
	}
	if err := verifyGhVersionFn(context.Background(), logger); err != nil {
		return nil, nil, fmt.Errorf("merge: gh version check: %w", err)
	}
	coord.SetExecutor(merge.GhExecutor{})
	w := merge.NewWorker(coord, 32, logger)
	return coord, w, nil
}

// verifyGhVersionFn is the test seam for the boot-time gh-version
// gate (#656). Production defaults to merge.VerifyGhVersion with the
// real shell-out probe; tests reassign it to a fake that returns a
// pinned version string so buildMergeWiring stays hermetic.
var verifyGhVersionFn = func(ctx context.Context, logger *slog.Logger) error {
	return merge.VerifyGhVersion(ctx, nil, logger)
}
