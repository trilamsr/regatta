// Package prwatch drives the running→pr_open agent transition by
// polling GitHub for the head SHA of each running agent's branch.
//
// Design spec: docs/engineer/specs/2026-06-02-orchestrator-pr-watcher.md.
//
// The package exposes a single Watcher type with a Sweep method. The
// orchestrator runs Sweep on its existing tick driver alongside
// ReapTerminal and RouteRejections; no new goroutine is introduced.
//
// Cluster fixes baked in vs the original spec (issues #520/#521/#522/#526):
//
//   - #520 (branch-rename strands agent): Sweep counts consecutive
//     empty-head observations per `pr_open` agent and emits
//     `agent_branch_renamed` once at the threshold so the reaper / a
//     future re-router has an actionable substrate signal instead of
//     a silent forever-stranded row.
//   - #521 (multi-host duplicate `agent_pr_opened`): the UNIQUE partial
//     index on events(agent_id, kind='agent_pr_opened') (migration 0014)
//     makes the loser's INSERT a UNIQUE violation; Sweep swallows it
//     and logs `pr_watcher.duplicate_pr_opened_suppressed`.
//   - #522 (fork-PR head not matched): the PRLister contract accepts a
//     branch *suffix* in addition to the literal `regatta/agent-N`
//     branch; the gh-CLI impl ORs `--head regatta/agent-N` with a
//     title-prefix probe `[agent-N]` so fork-pushed PRs that show an
//     owner-qualified head ref are still seen.
//   - #526 (gh CLI version floor): Watcher.Start runs a once-per-boot
//     probe — exec `gh --version`, parse semver, refuse to start when
//     the installed gh is below the required floor.
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

// MinGHVersion is the gh CLI floor enforced at Watcher.Start (issue #526).
// 2.40 is the first release that surfaces `--match-head-commit` on
// `gh pr merge`, which W2-c2 (merge-execute) depends on. Pinning the
// floor at construction time gives a single operator-actionable error
// instead of a runtime failure inside the inner sweep loop.
const MinGHVersion = "2.40.0"

// DefaultBranchRenameThreshold is the consecutive-empty-sweep count after
// which Sweep emits `agent_branch_renamed` for an agent in pr_open
// (issue #520). 12 ticks × 5s default cadence ≈ 1 minute — long
// enough to ride out an in-flight `gh pr list` blip; short enough that
// the operator sees the substrate signal within one minute of
// renaming a branch out from under an agent. Repos with different
// commit cadences override via Config.BranchRenameThreshold (issue #631).
const DefaultBranchRenameThreshold = 12

// BranchRenameThreshold preserves the public name historically used by
// the const so external callers + telemetry payload remain stable.
//
// Deprecated: read Watcher.branchRenameThreshold via the Config knob.
const BranchRenameThreshold = DefaultBranchRenameThreshold

// PullRequest is the GH PR view the watcher consumes. Mirrors gh's
// JSON shape so the production lister can decode `gh pr list --json
// number,headRefOid,state,author,title,headRefName` directly.
type PullRequest struct {
	Number      int    `json:"number"`
	HeadRefOid  string `json:"headRefOid"`
	State       string `json:"state"` // "OPEN" | "CLOSED" | "MERGED"
	HeadRefName string `json:"headRefName,omitempty"`
	Title       string `json:"title,omitempty"`
	AuthorLogin string `json:"authorLogin,omitempty"`
}

// PRLister is the GitHub query seam. The production impl shells the
// gh CLI; tests inject an in-memory stub. The branch argument is the
// literal head ref (e.g. `regatta/agent-7`); titlePrefix is the
// `[agent-7]` prefix the watcher falls back on when --head misses a
// fork-pushed PR (issue #522).
type PRLister interface {
	ListOpenByHead(ctx context.Context, branch, titlePrefix string) ([]PullRequest, error)
}

// GHVersionProbe reports the gh CLI version string ("2.55.0"). The
// production impl shells `gh --version` and parses the first semver
// triple; tests inject a stub so version-floor coverage stays
// hermetic.
type GHVersionProbe interface {
	Version(ctx context.Context) (string, error)
}

// Config wires the watcher's dependencies. DB, BranchFn, and Lister
// are required; the rest fall back to safe defaults.
type Config struct {
	DB           *state.DB
	BranchFn     func(agentID int64) string
	TitlePrefix  func(agentID int64) string
	Lister       PRLister
	VersionProbe GHVersionProbe
	Logger       *slog.Logger
	Tracer       trace.Tracer

	// BranchRenameThreshold overrides DefaultBranchRenameThreshold for
	// repos whose commit cadence differs from the spec default. Zero
	// (the zero value) picks DefaultBranchRenameThreshold so existing
	// callers keep the spec'd 12-tick window. Issue #631.
	BranchRenameThreshold int

	// AllowedForkAuthors is the username allowlist that gates the
	// title-prefix fallback (issue #587). When non-empty AND
	// StrictForkAuthor is true, a fork-pushed PR matches the agent
	// only when its AuthorLogin is in this list — the head-suffix
	// guard alone accepts `attacker-agent-1`, which is insufficient
	// against a hostile fork user who controls both branch name AND
	// title. Same-repo branches (HeadRefName == BranchFn(agentID))
	// bypass this gate because the upstream `--head` filter already
	// proves repo-control.
	AllowedForkAuthors []string

	// StrictForkAuthor enables the AllowedForkAuthors gate (issue #587).
	// Default false preserves the historical suffix-only behaviour;
	// operators flip this on when their threat model elevates to
	// hostile-fork-user (multi-tenant repo, public bug-bounty surface,
	// etc.). The reopen-trigger noted on #587.
	StrictForkAuthor bool
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

	// branchRenameThreshold is the per-watcher consecutive-empty-sweep
	// fire count for agent_branch_renamed (issue #631). Resolved at
	// New() from Config.BranchRenameThreshold or DefaultBranchRenameThreshold.
	branchRenameThreshold int

	// allowedForkAuthors is the precomputed set form of Config.AllowedForkAuthors
	// so per-sweep lookups stay O(1). Empty when StrictForkAuthor is false.
	allowedForkAuthors map[string]struct{}

	// strictForkAuthor mirrors Config.StrictForkAuthor (#587).
	strictForkAuthor bool

	// missCount tracks consecutive empty-PR sweeps per agent in
	// pr_open. Indexed by agent ID. Bounded by the size of the
	// pr_open set — at single-operator scale this stays in single
	// digits. Cleared when the agent transitions out of pr_open or
	// the threshold fires.
	missCount map[int64]int
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
	}, nil
}

// DefaultTitlePrefix builds the fork-PR fallback probe key for an
// agent: agents that push to a fork have head refs the upstream
// `--head` filter cannot match (issue #522). The convention encodes
// the agent ID inside the PR title (`[agent-7] ...`) so the watcher
// has a non-head-ref correlation key.
func DefaultTitlePrefix(agentID int64) string {
	return fmt.Sprintf("[agent-%d]", agentID)
}

// Start runs the boot-time gh-CLI version probe (issue #526). It is
// called once at orchestrator construction; the daemon refuses to
// start when gh is below MinGHVersion so operators see one clear
// error instead of every sweep failing in a different way.
//
// A nil VersionProbe disables the gate — used by tests + the
// --no-pr-watch fixture path.
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

// Sweep walks agents in {running, pr_open} and reconciles their
// pr_sha against GitHub once. Errors per-agent are logged and
// skipped — a single agent's network blip must not abort the sweep.
//
// Decision matrix lives in docs/engineer/specs/2026-06-02-orchestrator-pr-watcher.md §3.3
// with cluster-fix additions for #520 / #521 / #522.
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
	// Security guard for issue #522 fork-PR fallback: a fork user
	// can open a PR titled `[agent-N] hijack` to spoof the
	// title-prefix correlation. Require the head ref to either
	// match the expected branch literally OR end with the
	// `agent-N` suffix so an unrelated head ref (e.g. `topic/x`)
	// cannot impersonate the agent via the title fence alone.
	// Issue #587 elevates the guard: when strictForkAuthor is set,
	// the suffix-only path must also pass the AllowedForkAuthors
	// gate so `attacker-agent-1` from an unknown author is rejected.
	prs = w.filterImpersonators(prs, branch, agentSuffix(a.ID), a.ID)
	pr := pickPR(prs, w.log, a.ID)

	switch a.State {
	case state.AgentRunning:
		if pr == nil {
			return // PR not opened yet — no-op.
		}
		w.transitionToPROpen(ctx, a, *pr)
	case state.AgentPROpen:
		if pr == nil {
			w.observeBranchLost(ctx, a)
			return
		}
		w.missCount[a.ID] = 0
		if pr.HeadRefOid == a.PRSHA {
			return
		}
		w.observeHeadChanged(ctx, a, *pr)
	}
}

// pickPR resolves the >1 open PR case deterministically by lowest
// PR number — matches spec §3.3 R1. Logs `pr_watcher.ambiguous_head`
// once so the operator notices the force-push storm.
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

// transitionToPROpen drives the running→pr_open edge + records the
// `agent_pr_opened` substrate event. The state.RecordEvent INSERT
// fails with a UNIQUE-constraint violation when a competing
// orchestrator instance already wrote the row (issue #521); Sweep
// swallows that as a no-op so multi-host runs stay idempotent.
func (w *Watcher) transitionToPROpen(ctx context.Context, a state.Agent, pr PullRequest) {
	sha := pr.HeadRefOid
	if _, err := w.db.TransitionAgent(ctx, a.ID, state.AgentPROpen, state.AgentMutation{
		PRSHA: &sha,
	}); err != nil {
		// Idempotency: another instance already drove this agent
		// into pr_open. The next sweep observes a.State == pr_open
		// and goes through the SHA-diff branch instead.
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

// observeHeadChanged records a non-state-changing pr_sha update +
// emits `agent_pr_head_changed`. Spec §3.4: the kind is unconstrained
// because the same agent legitimately re-emits once per push. The
// SHA update goes through state.UpdateAgentPRSHA (column-only) rather
// than TransitionAgent because pr_open→pr_open is not a registered
// FSM edge — the spec mandates "column update, no transition" here.
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

// observeBranchLost handles the issue #520 case: an agent in
// pr_open whose branch has gone empty for branchRenameThreshold
// consecutive sweeps. Emits one `agent_branch_renamed` event so the
// reaper / future re-router has an actionable substrate seam, then
// resets the per-agent counter so a re-attached branch can re-fire
// `agent_pr_opened` cleanly. Threshold is per-watcher tunable (#631).
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
	// Reset so the operator can re-attach a branch without the
	// watcher re-firing on every subsequent sweep.
	w.missCount[a.ID] = 0
}

// agentSuffix is the trailing branch fragment a same-repo agent
// branch carries (`agent-7`). Used by filterImpersonators as a soft
// validator on the title-prefix fallback: a fork-pushed PR's head
// ref still ends with this suffix when the agent's worktree was
// branched from `regatta/agent-N` and re-pushed to a fork.
func agentSuffix(agentID int64) string {
	return fmt.Sprintf("agent-%d", agentID)
}

// filterImpersonators drops PRs whose head ref carries neither the
// literal branch nor the expected agent suffix. Defense-in-depth for
// issue #522: the title-prefix fallback alone would let a fork user
// open `[agent-N] hijack` on an unrelated branch and steal the
// correlation. Logged once per drop so the operator notices.
//
// Issue #587 hardens the suffix-only path: a `HasSuffix(head, "agent-1")`
// check accepts `attacker-agent-1` from a hostile fork user. When
// strictForkAuthor is on, suffix-match PRs additionally require an
// allowlisted AuthorLogin; same-repo `head == branch` PRs bypass the
// gate because the upstream --head filter already proves repo-control.
func (w *Watcher) filterImpersonators(prs []PullRequest, branch, suffix string, agentID int64) []PullRequest {
	out := prs[:0]
	for _, pr := range prs {
		head := pr.HeadRefName
		// Same-repo branch (or empty head from in-memory test stubs)
		// — trusted, bypass the suffix + author gates.
		if head == branch || head == "" {
			out = append(out, pr)
			continue
		}
		// Issue #587: allowlisted fork authors are trusted regardless
		// of head ref name. The author identity carries the trust here
		// (typed by the operator at boot), so a fork user the operator
		// vetted can ship from any branch name they choose.
		if w.strictForkAuthor {
			if _, ok := w.allowedForkAuthors[pr.AuthorLogin]; ok {
				out = append(out, pr)
				continue
			}
			// Strict mode + author not allowlisted: drop. We do NOT
			// fall through to the suffix check because suffix-only
			// accepts `attacker-agent-1` (the #587 vector). Once strict
			// mode is on, author allowlist is the sole fork-trust path.
			w.log.Warn("pr_watcher.fork_author_not_allowed",
				string(obs.KeyAgentID), agentID,
				"pr_number", pr.Number,
				"head_ref_name", head,
				"author", pr.AuthorLogin,
			)
			continue
		}
		// Non-strict legacy path (#522): suffix match is sufficient.
		// Preserved for repos whose threat model has not elevated;
		// operators flip StrictForkAuthor on when needed.
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
// error text. Same substring-probe shape merge.isUniqueViolation +
// substrate.isUniqueNonceViolation use — the driver does not surface
// extended error codes through database/sql so the substring is the
// stable seam.
func IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}
