// Package prwatch drives running→pr_open by polling GitHub for the
// head SHA of each running agent's branch. Sweep runs on the
// orchestrator's existing tick driver; no new goroutine.
//
// Design spec: docs/engineer/specs/2026-06-02-orchestrator-pr-watcher.md.
//
// Cluster fixes baked in (#520/#521/#522/#526):
//   - #520 (branch-rename strands agent): consecutive-empty-head count
//     emits `agent_branch_renamed` at threshold so the reaper has an
//     actionable signal instead of a forever-stranded row.
//   - #521 (multi-host duplicate `agent_pr_opened`): UNIQUE partial
//     index (migration 0014) makes the loser's INSERT a UNIQUE
//     violation; Sweep swallows + logs `*.duplicate_pr_opened_suppressed`.
//   - #522 (fork-PR head not matched): PRLister contract accepts a
//     branch *suffix* + a title-prefix probe so fork-pushed PRs with an
//     owner-qualified head ref are still seen.
//   - #526 (gh CLI version floor): Watcher.Start runs a once-per-boot
//     `gh --version` probe; refuses to start when below floor.
package prwatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// MinGHVersion is the gh CLI floor enforced at Watcher.Start (#526).
// 2.40 is the first release surfacing `--match-head-commit` on
// `gh pr merge` (W2-c2 dependency); pinning at boot gives one operator-
// actionable error instead of a runtime failure in the sweep loop.
const MinGHVersion = "2.40.0"

// DefaultBranchRenameThreshold is the consecutive-empty-sweep count
// after which Sweep emits `agent_branch_renamed` for a pr_open agent
// (#520). 12 ticks × 5s ≈ 1 minute — rides out a `gh pr list` blip,
// short enough to surface the rename within a minute. Override via
// Config.BranchRenameThreshold (#631).
const DefaultBranchRenameThreshold = 12

// PullRequest mirrors gh's JSON so the production lister can decode
// `gh pr list --json number,headRefOid,state,author,title,headRefName`
// directly.
type PullRequest struct {
	Number      int    `json:"number"`
	HeadRefOid  string `json:"headRefOid"`
	State       string `json:"state"` // "OPEN" | "CLOSED" | "MERGED"
	HeadRefName string `json:"headRefName,omitempty"`
	Title       string `json:"title,omitempty"`
	AuthorLogin string `json:"authorLogin,omitempty"`
	// MergeStateStatus is gh's mergeable rollup — "CLEAN", "DIRTY",
	// "BLOCKED", "BEHIND", "UNSTABLE", "UNKNOWN". Operator-console S0
	// uses DIRTY to drive the agent_pr_dirty chip (spec §3.5).
	MergeStateStatus string `json:"mergeStateStatus,omitempty"`
}

// PRLister is the GitHub query seam (production shells gh; tests
// inject a stub). branch is the literal head ref; titlePrefix is the
// `[agent-N]` fallback the watcher uses when --head misses a fork-
// pushed PR (#522).
type PRLister interface {
	ListOpenByHead(ctx context.Context, branch, titlePrefix string) ([]PullRequest, error)
}

// GHVersionProbe reports the gh CLI version string ("2.55.0").
type GHVersionProbe interface {
	Version(ctx context.Context) (string, error)
}

// Config wires the watcher's dependencies. DB, BranchFn, and Lister
// are required; the rest default safely.
type Config struct {
	DB           *state.DB
	BranchFn     func(agentID int64) string
	TitlePrefix  func(agentID int64) string
	Lister       PRLister
	VersionProbe GHVersionProbe
	Logger       *slog.Logger
	Tracer       trace.Tracer

	// BranchRenameThreshold overrides DefaultBranchRenameThreshold for
	// repos with non-default commit cadence (#631). Zero picks the
	// default.
	BranchRenameThreshold int

	// AllowedForkAuthors gates the title-prefix fallback (#587). When
	// non-empty AND StrictForkAuthor is true, a fork-pushed PR matches
	// only when AuthorLogin is in this list — the head-suffix guard
	// alone accepts `attacker-agent-1`, insufficient against a hostile
	// fork user controlling both branch name AND title. Same-repo
	// branches bypass this gate (the upstream --head filter proves
	// repo-control).
	AllowedForkAuthors []string

	// StrictForkAuthor enables the AllowedForkAuthors gate (#587).
	// Default false preserves historical suffix-only behaviour;
	// operators flip on when the threat model elevates to hostile-
	// fork-user.
	StrictForkAuthor bool

	// LocalHeadFn returns the agent's local worktree HEAD sha so Sweep
	// can detect the BUG-1051 stuck-push pattern: agent pushed sha-A,
	// rebased locally to sha-B, but force-push was denied — PR's
	// HeadRefOid still reads sha-A while the local worktree carries
	// sha-B. Returning ok=false (worktree gone, ref missing) suppresses
	// the probe. Nil disables divergence detection entirely (#1051 c2).
	LocalHeadFn func(agentID int64) (sha string, ok bool)
}

// Watcher owns the running↔pr_open reconciliation. One per
// orchestrator instance; safe for the single-caller Run loop.
type Watcher struct {
	db           *state.DB
	branchFn     func(agentID int64) string
	titlePrefix  func(agentID int64) string
	lister       PRLister
	versionProbe GHVersionProbe
	log          *slog.Logger
	tracer       trace.Tracer

	branchRenameThreshold int
	allowedForkAuthors    map[string]struct{} // empty unless strictForkAuthor.
	strictForkAuthor      bool

	// missCount: per-agent consecutive empty-PR sweep count. Bounded
	// by the pr_open set; cleared on state transition or threshold-fire.
	missCount map[int64]int

	// dirtyEmitted: per-agent flag — true while the watcher has already
	// emitted agent_pr_dirty for the current DIRTY entry. Cleared when
	// the PR transitions back to a non-DIRTY mergeable state so a
	// subsequent DIRTY re-arms one fresh emission. Operator-console S0.
	dirtyEmitted map[int64]bool

	// localHeadFn: optional local-HEAD probe. Nil disables divergence
	// detection. See Config.LocalHeadFn (#1051).
	localHeadFn func(agentID int64) (string, bool)

	// divergedEmitted: per-agent record of the last local sha we
	// emitted prwatch.branch_diverged for. Same tuple suppresses, new
	// sha re-arms — matches the BUG-1051 acceptance criterion "once per
	// (agent_id, divergence_sha) tuple".
	divergedEmitted map[int64]string
}

// New constructs a Watcher. Returns an error when required deps are
// missing so wiring bugs surface at boot rather than mid-sweep.
func New(cfg Config) (*Watcher, error) {
	if cfg.DB == nil {
		return nil, errors.New("prwatch: DB is required")
	}
	if cfg.BranchFn == nil {
		return nil, errors.New("prwatch: BranchFn is required")
	}
	if cfg.Lister == nil {
		return nil, errors.New("prwatch: Lister is required")
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	tracer := cfg.Tracer
	if tracer == nil {
		tracer = otel.Tracer("prwatch")
	}
	titlePrefix := cfg.TitlePrefix
	if titlePrefix == nil {
		titlePrefix = DefaultTitlePrefix
	}
	threshold := cfg.BranchRenameThreshold
	if threshold <= 0 {
		threshold = DefaultBranchRenameThreshold
	}
	var allow map[string]struct{}
	if len(cfg.AllowedForkAuthors) > 0 {
		allow = make(map[string]struct{}, len(cfg.AllowedForkAuthors))
		for _, a := range cfg.AllowedForkAuthors {
			allow[a] = struct{}{}
		}
	}
	return &Watcher{
		db:                    cfg.DB,
		branchFn:              cfg.BranchFn,
		titlePrefix:           titlePrefix,
		lister:                cfg.Lister,
		versionProbe:          cfg.VersionProbe,
		log:                   log,
		tracer:                tracer,
		branchRenameThreshold: threshold,
		allowedForkAuthors:    allow,
		strictForkAuthor:      cfg.StrictForkAuthor,
		missCount:             make(map[int64]int),
		dirtyEmitted:          make(map[int64]bool),
		localHeadFn:           cfg.LocalHeadFn,
		divergedEmitted:       make(map[int64]string),
	}, nil
}

// DefaultTitlePrefix builds the fork-PR fallback probe key (#522):
// fork-pushed PRs carry head refs the upstream `--head` filter cannot
// match, so the agent ID rides inside the PR title (`[agent-N]`) as a
// non-head-ref correlation key.
func DefaultTitlePrefix(agentID int64) string {
	return fmt.Sprintf("[agent-%d]", agentID)
}

// Start runs the boot-time gh-CLI version probe (#526). Refuses to
// start when gh is below MinGHVersion so operators see one clear
// error instead of every sweep failing differently. Nil VersionProbe
// disables the gate (tests + --no-pr-watch).
func (w *Watcher) Start(ctx context.Context) error {
	if w.versionProbe == nil {
		return nil
	}
	got, err := w.versionProbe.Version(ctx)
	if err != nil {
		return fmt.Errorf("prwatch: gh version probe: %w", err)
	}
	ok, err := VersionAtLeast(got, MinGHVersion)
	if err != nil {
		return fmt.Errorf("prwatch: parse gh version %q: %w", got, err)
	}
	if !ok {
		return fmt.Errorf(
			"prwatch: gh CLI %s below required %s; run 'brew upgrade gh' or pass --no-pr-watch",
			got, MinGHVersion,
		)
	}
	w.log.Info("prwatch.gh_version_probe_ok", "gh_version", got, "floor", MinGHVersion)
	return nil
}

// Sweep walks {running, pr_open} agents and reconciles their pr_sha
// against GitHub once. Per-agent errors are logged + skipped so one
// blip cannot abort the sweep. Decision matrix:
// docs/engineer/specs/2026-06-02-orchestrator-pr-watcher.md §3.3.
func (w *Watcher) Sweep(ctx context.Context) error {
	ctx, span := w.tracer.Start(ctx, "prwatch.sweep")
	defer span.End()

	agents, err := w.db.ListAgentsByState(ctx, state.AgentRunning, state.AgentPROpen)
	if err != nil {
		return fmt.Errorf("prwatch: list agents: %w", err)
	}
	for _, a := range agents {
		w.sweepOne(ctx, a)
	}
	return nil
}

// sweepOne reconciles a single agent. Errors are logged + swallowed
// per the per-agent isolation rule above.
func (w *Watcher) sweepOne(ctx context.Context, a state.Agent) {
	branch := w.branchFn(a.ID)
	titlePrefix := w.titlePrefix(a.ID)
	prs, err := w.lister.ListOpenByHead(ctx, branch, titlePrefix)
	if err != nil {
		w.log.Warn("prwatch.list_failed",
			string(obs.KeyAgentID), a.ID,
			"branch", branch,
			string(obs.KeyErr), err.Error(),
		)
		return
	}
	// #522 / #587 impersonator guard: drop PRs whose head ref carries
	// neither the literal branch nor the agent suffix (and, under
	// strict mode, whose author is not allowlisted).
	prs = w.filterImpersonators(prs, branch, agentSuffix(a.ID), a.ID)
	pr := pickPR(prs, w.log, a.ID)

	w.observeMergeStateStatus(ctx, a, pr)

	switch a.State {
	case state.AgentRunning:
		if pr == nil {
			return
		}
		w.observeBranchRenamedByAgent(a, branch, *pr)
		w.transitionToPROpen(ctx, a, *pr)
	case state.AgentPROpen:
		if pr == nil {
			w.observeBranchLost(ctx, a)
			return
		}
		w.missCount[a.ID] = 0
		w.observeBranchDiverged(a, *pr)
		if pr.HeadRefOid == a.PRSHA {
			return
		}
		w.observeHeadChanged(ctx, a, *pr)
	}
}

// pickPR resolves the >1 open PR case by lowest PR number (spec §3.3
// R1). Logs `pr_watcher.ambiguous_head` once so the operator notices.
func pickPR(prs []PullRequest, log *slog.Logger, agentID int64) *PullRequest {
	switch len(prs) {
	case 0:
		return nil
	case 1:
		return &prs[0]
	}
	best := &prs[0]
	for i := 1; i < len(prs); i++ {
		if prs[i].Number < best.Number {
			best = &prs[i]
		}
	}
	log.Warn("pr_watcher.ambiguous_head",
		string(obs.KeyAgentID), agentID,
		"pr_count", len(prs),
		"picked_pr", best.Number,
	)
	return best
}

// transitionToPROpen drives running→pr_open + records the
// `agent_pr_opened` event. UNIQUE-violation on a multi-host race is
// swallowed (#521).
func (w *Watcher) transitionToPROpen(ctx context.Context, a state.Agent, pr PullRequest) {
	sha := pr.HeadRefOid
	if _, err := w.db.TransitionAgent(ctx, a.ID, state.AgentPROpen, state.AgentMutation{
		PRSHA: &sha,
	}); err != nil {
		// Another instance already drove this agent into pr_open;
		// next sweep takes the SHA-diff branch.
		if errors.Is(err, state.ErrInvalidTransition) {
			return
		}
		w.log.Warn("prwatch.transition_failed",
			string(obs.KeyAgentID), a.ID,
			string(obs.KeyErr), err.Error(),
		)
		return
	}
	payload, _ := json.Marshal(struct {
		PRNumber int    `json:"pr_number"`
		PRSHA    string `json:"pr_sha"`
	}{pr.Number, sha})
	err := w.db.RecordEvent(ctx, a.ID, "agent_pr_opened", string(payload))
	if err == nil {
		w.log.Info("prwatch.agent_pr_opened",
			string(obs.KeyAgentID), a.ID,
			string(obs.KeyWorkItemID), a.WorkItemID,
			"pr_number", pr.Number,
			"pr_sha", sha,
		)
		return
	}
	if IsUniqueViolation(err) {
		w.log.Debug("pr_watcher.duplicate_pr_opened_suppressed",
			string(obs.KeyAgentID), a.ID,
			"pr_number", pr.Number,
		)
		return
	}
	w.log.Warn("prwatch.record_event_failed",
		string(obs.KeyAgentID), a.ID,
		"kind", "agent_pr_opened",
		string(obs.KeyErr), err.Error(),
	)
}

// observeHeadChanged records a pr_sha update + emits
// `agent_pr_head_changed`. Goes through UpdateAgentPRSHA (column-only)
// because pr_open→pr_open is not a registered FSM edge — spec §3.4
// mandates "column update, no transition".
func (w *Watcher) observeHeadChanged(ctx context.Context, a state.Agent, pr PullRequest) {
	sha := pr.HeadRefOid
	if err := w.db.UpdateAgentPRSHA(ctx, a.ID, sha); err != nil {
		w.log.Warn("prwatch.sha_update_failed",
			string(obs.KeyAgentID), a.ID,
			string(obs.KeyErr), err.Error(),
		)
		return
	}
	payload, _ := json.Marshal(struct {
		PRNumber int    `json:"pr_number"`
		PRSHA    string `json:"pr_sha"`
		PrevSHA  string `json:"prev_sha"`
	}{pr.Number, sha, a.PRSHA})
	if err := w.db.RecordEvent(ctx, a.ID, "agent_pr_head_changed", string(payload)); err != nil {
		w.log.Warn("prwatch.record_event_failed",
			string(obs.KeyAgentID), a.ID,
			"kind", "agent_pr_head_changed",
			string(obs.KeyErr), err.Error(),
		)
	}
}

// observeBranchLost handles #520: a pr_open agent whose branch has
// been empty for branchRenameThreshold sweeps. Emits one
// `agent_branch_renamed` so the reaper has an actionable signal, then
// resets the counter so a re-attached branch can re-fire cleanly.
func (w *Watcher) observeBranchLost(ctx context.Context, a state.Agent) {
	w.missCount[a.ID]++
	if w.missCount[a.ID] < w.branchRenameThreshold {
		return
	}
	payload, _ := json.Marshal(struct {
		PrevSHA   string `json:"prev_sha"`
		Threshold int    `json:"miss_threshold"`
	}{a.PRSHA, w.branchRenameThreshold})
	if err := w.db.RecordEvent(ctx, a.ID, "agent_branch_renamed", string(payload)); err != nil {
		w.log.Warn("prwatch.record_event_failed",
			string(obs.KeyAgentID), a.ID,
			"kind", "agent_branch_renamed",
			string(obs.KeyErr), err.Error(),
		)
		return
	}
	w.log.Warn("pr_watcher.branch_lost",
		string(obs.KeyAgentID), a.ID,
		string(obs.KeyWorkItemID), a.WorkItemID,
		"miss_threshold", w.branchRenameThreshold,
	)
	// Reset so a re-attached branch does not re-fire on every sweep.
	w.missCount[a.ID] = 0
}

// observeMergeStateStatus drives the agent_pr_dirty chip (spec §3.5).
// Emits once when the rollup enters DIRTY for an agent; clears the
// re-arm flag when the rollup leaves DIRTY (any non-DIRTY value,
// including CLEAN, BLOCKED, BEHIND, or absent PR) so a subsequent
// DIRTY re-fires exactly one event. Operator-console S0.
func (w *Watcher) observeMergeStateStatus(ctx context.Context, a state.Agent, pr *PullRequest) {
	if pr == nil || !strings.EqualFold(pr.MergeStateStatus, "DIRTY") {
		delete(w.dirtyEmitted, a.ID)
		return
	}
	if w.dirtyEmitted[a.ID] {
		return
	}
	payload, _ := json.Marshal(struct {
		PRNumber         int    `json:"pr_number"`
		MergeStateStatus string `json:"merge_state_status"`
	}{pr.Number, pr.MergeStateStatus})
	if err := w.db.RecordEvent(ctx, a.ID, "agent_pr_dirty", string(payload)); err != nil {
		w.log.Warn("prwatch.record_event_failed",
			string(obs.KeyAgentID), a.ID,
			"kind", "agent_pr_dirty",
			string(obs.KeyErr), err.Error(),
		)
		return
	}
	w.dirtyEmitted[a.ID] = true
}

// observeBranchRenamedByAgent surfaces BUG-1047: the worker pushed the
// PR under a different head ref than the orchestrator-pinned branch, so
// `gh --head <branch>` returned no rows and only the title-prefix
// fallback rescued the correlation. Emitted at running→pr_open so the
// operator sees the prompt drift in logs even when the in-prompt
// branch-name pin slips. Empty HeadRefName comes from in-memory stubs
// and the same-repo upstream --head path — neither is a rename.
func (w *Watcher) observeBranchRenamedByAgent(a state.Agent, branch string, pr PullRequest) {
	if pr.HeadRefName == "" || pr.HeadRefName == branch {
		return
	}
	w.log.Warn("prwatch.branch_renamed_by_agent",
		string(obs.KeyAgentID), a.ID,
		string(obs.KeyWorkItemID), a.WorkItemID,
		"pinned_branch", branch,
		"observed_head_ref_name", pr.HeadRefName,
		"pr_number", pr.Number,
	)
}

// observeBranchDiverged surfaces BUG-1051: the agent pushed sha-A,
// rebased locally to sha-B, then the force-push was denied (or the
// agent silently exited before re-pushing). The PR's HeadRefOid stays
// sha-A while the local worktree carries sha-B; without a signal, the
// orchestrator waits forever for a terminal-PR transition that will
// never arrive. Emits once per (agent_id, local_sha) tuple — same sha
// suppresses, new local sha re-arms.
func (w *Watcher) observeBranchDiverged(a state.Agent, pr PullRequest) {
	if w.localHeadFn == nil {
		return
	}
	localSHA, ok := w.localHeadFn(a.ID)
	if !ok || localSHA == "" {
		return
	}
	if localSHA == pr.HeadRefOid {
		delete(w.divergedEmitted, a.ID)
		return
	}
	if w.divergedEmitted[a.ID] == localSHA {
		return
	}
	w.log.Warn("prwatch.branch_diverged",
		string(obs.KeyAgentID), a.ID,
		string(obs.KeyWorkItemID), a.WorkItemID,
		"pr_number", pr.Number,
		"remote_sha", pr.HeadRefOid,
		"local_sha", localSHA,
	)
	w.divergedEmitted[a.ID] = localSHA
}

func agentSuffix(agentID int64) string {
	return fmt.Sprintf("agent-%d", agentID)
}

// filterImpersonators drops PRs whose head ref carries neither the
// literal branch nor the expected agent suffix (defense-in-depth for
// #522: the title-prefix fallback alone would let a fork user open
// `[agent-N] hijack` on an unrelated branch and steal correlation).
// #587 adds: under strictForkAuthor, suffix-match PRs additionally
// require an allowlisted AuthorLogin. Same-repo `head == branch` PRs
// bypass the gate — the upstream --head filter already proves repo-
// control.
func (w *Watcher) filterImpersonators(prs []PullRequest, branch, suffix string, agentID int64) []PullRequest {
	out := prs[:0]
	for _, pr := range prs {
		head := pr.HeadRefName
		// Same-repo branch (or empty head from in-memory stubs):
		// trusted, bypass suffix + author gates.
		if head == branch || head == "" {
			out = append(out, pr)
			continue
		}
		// #587 strict path: allowlisted authors are the sole fork-
		// trust route. Suffix-only would accept `attacker-agent-1`.
		if w.strictForkAuthor {
			if _, ok := w.allowedForkAuthors[pr.AuthorLogin]; ok {
				out = append(out, pr)
				continue
			}
			w.log.Warn("pr_watcher.fork_author_not_allowed",
				string(obs.KeyAgentID), agentID,
				"pr_number", pr.Number,
				"head_ref_name", head,
				"author", pr.AuthorLogin,
			)
			continue
		}
		// Non-strict legacy path (#522): suffix match suffices.
		if !strings.HasSuffix(head, suffix) {
			w.log.Warn("pr_watcher.title_prefix_impersonator_filtered",
				string(obs.KeyAgentID), agentID,
				"pr_number", pr.Number,
				"head_ref_name", head,
				"author", pr.AuthorLogin,
				"reason", "head_ref_suffix_mismatch",
			)
			continue
		}
		out = append(out, pr)
	}
	return out
}

// IsUniqueViolation matches modernc.org/sqlite's UNIQUE-constraint
// error text — same substring-probe shape as merge.isUniqueViolation
// + substrate.isUniqueNonceViolation; the driver does not surface
// extended error codes through database/sql.
func IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}
