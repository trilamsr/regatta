# 07 — Open Questions: Concrete Recommendations

One section per open question in the design. Each: question, evidence,
recommendation, citation, failure-detection.

---

## Q1. What does the orchestrator do with `⧗`-in-progress milestones?

**Question.** Pick them up (someone abandoned), leave them (human
working), or claim after N days of staleness?

**Evidence.**

- 6 milestones at `⧗` today: M6, M13, M15, M19, M11 cgo-carry, plus
  `⧗`-flagged rubrics inside otherwise-shipped milestones (M2 install,
  M4b nccl-hang, M10 alpha).
- `CONTRIBUTING.md` "Keeping this document current" defines `⧗` as
  *"a PR that **starts** work on a milestone flips its top-line status
  `☐ → ⧗`"*. There is **no per-milestone owner field** in `MILESTONES.md`
  — the lane table tracks state, not assignment.
- `git log -- MILESTONES.md` shows `⧗` flips coupled to landed PRs
  (e.g., #102 M13 Phase 2 left M13 at `⧗` after partial ship). The
  flag means "work in flight", not "human in flight".
- The M11/M13/M19 carry-forward pattern shows `⧗` routinely persists
  for weeks after the actively-coding session ends — the marker
  doesn't decay on its own. There is no `claimed-by` or
  `last-touched` field, only the implicit `git log` timestamp on the
  most recent edit to the rubric block.
- The pr-review-loop skill auto-resolves milestone from branch name
  (`feat/m<X>-*`); the per-branch worktree convention `.claude/
  worktrees/m<NN>-<slug>/` is what tracks active work — not `⧗`.

**Recommendation.** **Do not claim `⧗` milestones automatically.**
The orchestrator treats `⧗` as **off-limits for the autospawner**.
A milestone becomes spawn-eligible only when (a) top-line is `☐`,
or (b) it is `⧗` AND `git log -1 --format=%ct -- "<milestone block
range>"` is older than **14 days** AND there is no open PR with a
branch matching `m<NN>-*` or `feat/m<NN>-*`. Even in case (b), the
orchestrator does not spawn silently — it files a `FOLLOWUPS.md`
row "`⧗` M<NN> stale 14d; candidate for fleet pickup" and waits
for a maintainer to flip the top-line back to `☐` or annotate the
rubric block with a `<!-- fleet-claim-ok -->` HTML comment.

**Justification.** `CONTRIBUTING.md` explicitly says `⧗` marks
*started* work — racing onto it violates the social contract the
status legend encodes. The `learn-from-mistakes` AGENTS lesson on
"Verify named identifiers exist before echoing them as fact" applies
upward: don't assume a milestone is abandoned because a marker is
stale; check the lived state (branches, worktrees, recent activity)
first. 14 days is consistent with the project cadence — most active
milestones see a commit touching their block within 7 days
(per `git log MILESTONES.md`).

**Failure detection.** Orchestrator logs `(spawn-skip, reason=in-
progress)` per cycle. If the same `⧗` milestone is skipped for
>30 days with no `git` activity on its block, file a digest entry
asking the maintainer to triage. If an agent does spawn under
rule (b) and the original human pushes a commit to a colliding
path, the merge queue rebase fails and the orchestrator unspawns
the agent (per Failure Modes table row 6 in the design).

---

## Q2. Can an agent draft an RFC?

**Question.** Several `☐` milestones (M12, M14, M17, M18, M20, M24)
need or could use an RFC. Must a human draft, or can an agent?

**Evidence.**

- The three most recent RFCs (0009 pyspy, 0010 containerstdout,
  0011 kueue) all sit at `draft (design locked)` per
  `docs/rfcs/README.md` status index. All three landed via PRs with
  `Co-authored-by: Claude Opus 4.7` trailers (PRs #79, #94, #96).
  Agents already draft RFCs in this repo today — it just isn't
  formalized.
- `CONTRIBUTING.md` § RFC process: **two maintainer approvals**
  required for acceptance; no rule about authorship.
- `docs/rfcs/README.md` § "Authoring conventions" enumerates
  five rigorous gates RFC bodies must meet (Operator surfaces,
  Rubric trace appendix, Performance budget, Wire-attribute
  table, Sibling-package symbol compile-resolve). These are
  falsifiable — exactly the surface an agent can satisfy.
- `learn-from-mistakes` SKILL bans certain vocabulary ("ralph",
  "subagent", etc.) in commit bodies. RFC bodies fall under
  `docs/STYLE-docs.md` banned-phrase lint — same constraint
  applies.

**Recommendation.** **Agents may draft RFCs.** The orchestrator
treats RFC drafting as a first-class output the agent can
produce, gated by L3/L4/L5 plus the existing **two-maintainer-
approval** rule on acceptance. The Phase 1 agent's plan
explicitly distinguishes "needs RFC" milestones (M12, M14, M17,
M18, M20, M24): the agent's first PR for any such milestone is
the RFC PR, not the implementation; status stays `draft` on
merge. A separate implementation PR follows once the RFC reaches
`accepted`.

**Justification.** RFC-0009 / 0010 / 0011 are existence proofs.
The repo already passed three agent-drafted RFCs through review;
the rubric-trace + compile-resolve + symbol-grep gates in
`docs/rfcs/README.md` are exactly the mechanical checks that
make agent-drafted RFCs reliable. The two-maintainer human gate
already protects against bad designs slipping in.

**Failure detection.** The L4 adversarial reviewer must run the
RFC-specific gates (rubric-trace count match, sibling-package
symbol compile-resolve, performance-budget bench presence) when
the touched path is `docs/rfcs/`. If L4 rejects an agent-drafted
RFC twice in a row with the same finding (e.g., "wire-attribute
table missing"), the orchestrator drops to "needs human" — the
agent is structurally failing the genre. Track per-lane RFC-
reject rates in `state.json`; lanes where ≥2 RFCs in a row need
maintainer-rewrite are downgraded to implementation-only.

---

## Q3. Cross-milestone coupling — who notices?

**Question.** M18 build-couples to M17's `cross_rank.go`. If M17's
agent ships without exposing what M18 needs, M18 blocks. Who
notices?

**Evidence.**

- `find . -name cross_rank*` returns nothing — neither M17 nor M18
  has shipped. The coupling is purely planned.
- `docs/rfcs/README.md` § "Required cross-checks" already has the
  rule: *"Cross-RFC join-keys — label names, attribute
  namespaces, and any field both M<X> and M<Y> read or write MUST
  resolve to one canonical spelling at design-lock. Resolve in
  this PR or the sibling RFC's PR; do not defer to 'whichever
  lands first' (the second author pays)."*
- M18's milestone block explicitly states: *"Build-time coupling
  to Lane 6's M17 means M18 ships only after M17 lifts
  `cross_rank.go`."* This is encoded as `Depends on: M17` in the
  rubric metadata.
- `pkg/nccl/fr_parser/` shipped first; M19's `internal/synthesis/
  patterns/pod_evicted.go` imported `k8sevents.NodeRecord` as a
  typed compile-time join — `pattern_consumer_test.go` is the
  canonical falsifier for "the upstream actually exposes what the
  downstream needs."

**Recommendation.** **The orchestrator enforces a pre-spawn join-
key contract.** For any milestone listing `Depends on: M<X>` where
M<X> exports a typed symbol cited by M<Y>'s rubrics, M<X>'s PR
**must include** the equivalent of M19's `pattern_consumer_test.
go` — a compile-time test that imports the exported symbols and
exercises the interface M<Y> needs. The L5 drift detector adds a
new check: if a PR touches a milestone block whose rubrics name
exported symbols (regex: `\`pkg\.<Sym>\``), there must be a
sibling-package `*_consumer_test.go` importing those symbols. No
test → L5 rejects.

Concretely for M17/M18: the M17 agent's PR must land a
`cross_rank_consumer_test.go` in `internal/synthesis/patterns/`
that imports the M17 join library against a no-op fixture. The
test is the dependency contract. M18's agent is spawned only
when the test exists at HEAD.

**Justification.** This is the existing `docs/rfcs/README.md`
"second author pays" rule, mechanized. M19 → M10 already
demonstrated the pattern (`pattern_consumer_test.go` per the
M19 rubric: *"compile-time gate in `components/receivers/
k8sevents/pattern_consumer_test.go` (Record + NodeRecord + Hint +
NodePressureKind constants + attribute names all pinned)"*). The
repo invented this contract; the fleet just enforces it.

**Failure detection.** L5 drift detector flag: "milestone M<X>
declares exports `<Sym>` consumed by M<Y> but no `*_consumer_
test.go` in M<Y>'s package imports `<Sym>`." If M17's agent
opens a PR without the consumer-test, L5 rejects with that
exact message and the agent revises. If after K=3 rejections
the agent cannot construct the test, drop to "needs human" —
the join contract is then an open design question, not an
implementation gap.

---

## Q4. Token budget — calibrate 1M default.

**Question.** Default per-milestone cap of 1M tokens is a guess.

**Evidence.** Agent 03's economics analysis is on disk and
estimates per-milestone token spend at: optimistic 265K, median
742K, pessimistic 1.43M. The pessimistic case already exceeds
1M. Sample-PR diffs (b954448 M16 alpha = 5,647 insertions over
32 files; 4ae7b76 M19 = 3,397 lines over 55 files; d88da3a M13
Phase 1 = 2,400 lines over 30 files) confirm receiver-class
milestones are large.

**Recommendation.** **Adopt per-lane token budgets, not a flat
1M.** Per the 03 economics analysis §7.2:

- **Lane 4 (orchestrator-signals, pure Go):** 1M tokens.
- **Lane 5 (Python/Kineto fixtures), Lane 6 (kernel/GPU):** 2M
  tokens — these lanes' rubrics demand binary fixtures, cgo
  build-tag layers, and replay corpora that materially expand
  the iter count.
- **Lane 1 release / Lane 3 docs:** 300K tokens — both are
  small-diff, low-iteration genres.
- **Foundation:** 1.5M (touches the runtime; iteration is high).

**Mandate prompt caching** on the AGENTS+STYLE+PRINCIPLES+
NORTHSTARS bundle (~60K tokens). Document the cache flag in
`tools/fleet/prompts/milestone.md`. Without caching, all
estimates triple.

Cap is **soft-warned at 80%**, **hard-killed at 100%**. On
hard-kill the orchestrator opens a `needs-human` flag on the PR
with the agent's last `make ci` state and the most recent gate
verdict; the maintainer decides whether to extend the budget by
500K or close out.

**Justification.** Economics §7.1 + §7.2. The flat 1M default
kills 25% of pessimistic-scenario receiver milestones for a
~$10 saving — false economy when the agent has already burned
$24 to reach that point and the maintainer pickup costs an
hour.

**Failure detection.** `tools/fleet/state.json` records
`tokens_used`, `tokens_cap`, and `cache_hit_ratio` per agent.
Weekly digest computes per-lane median spend; if Lane 4 medians
drift above 80% of cap in a rolling 10-milestone window, raise
the cap by 500K. If cache-hit ratio drops below 50% for
>3 consecutive agents, the AGENTS.md bundle was probably edited
mid-loop — file a FOLLOWUP to pin a cache key per session.

---

## Q5. Maintainer review burden — tune rejection threshold.

**Question.** Advisory-and-rejecting AI gates: if they reject too
much, agents thrash; if too little, humans see noise.

**Evidence.**

- `pr-review-loop/SKILL.md` is 538 lines: 5 phases, ~14 subagent
  dispatches, multi-lens with explicit adversarial pass.
- `.claude/notes/review-patterns.md` documents "graded review
  hits A+, adversarial finds 5 more real issues" (PR #31 memo-
  aliasing class). The rigor baseline for L4 is one
  pr-review-loop adversarial pass.
- AGENTS.md lesson "Ceremony without a falsifiable consumer is
  bloat — `grep -c '<name>' <file> == 0` is the test." Applied
  to gates: a rejection is real only if its complaint can be
  turned into a `grep` or test that fails.

**Recommendation.** **Phase 1 rejection threshold per gate:**

- **L3 rubric verifier:** rejects on any verdict `fail`. No
  threshold — the rubric *is* the oracle, every miss is
  load-bearing. Per-PR rejection rate target: 30–50% on first
  push (forces the agent to evidence claims), <10% by third
  push (else the rubric is unimplementable and milestone needs
  re-scoping).
- **L4 adversarial:** rejects only on **high-severity**
  objections; medium and low become PR comments without
  blocking. "High" = (a) violates a PRINCIPLES.md §-number
  cited in the milestone block, OR (b) violates a NORTHSTARS
  O-number perf budget, OR (c) breaks an AGENTS.md load-
  bearing lesson (the 21 bullets). Other findings advisory.
  Per-PR rejection rate target: 50% on first push, <20% by
  third push.
- **L5 drift detector:** rejects only on (a) status-drift
  (touched milestone scope, MILESTONES.md/FOLLOWUPS.md not
  updated) or (b) the new cross-milestone join-key check
  from Q3. Other drift advisory.

**Per-PR K=3 cap** stays as the design specifies. If an agent
hits K=3 on L3 specifically (rubric still unsatisfied), drop to
"needs human" — the rubric and the implementation are
diverging in a way the agent cannot bridge.

**Justification.** Matches the `pr-review-loop` adversarial
rigor (the existing baseline that human-style PRs go through),
but ratchets the *blocking* surface down to genuine
falsifiable invariants — exactly the AGENTS.md "ceremony
needs a falsifiable consumer" rule. Anything an L4 finding can
be expressed as is at most a PR comment; only findings tied to
a named-and-numbered repo invariant block.

**Failure detection.** Track per-gate **net-helpfulness ratio**
in the weekly digest: (real bugs caught that humans would
have caught) / (PRs blocked total). Synthesis test corpus
from Phase 0 gives the floor. If a gate's net-helpfulness
drops below 0.5 over a rolling 10-PR window, demote its
high-severity rules to advisory and audit which AGENTS.md
lesson it was anchoring; the lesson may have decayed.

---

## Q6. Public exhaust — fleet-attributed PRs or quiet?

**Question.** Do fleet PRs publicly attribute as "fleet agents", or
look like human PRs (current no-AI-attribution convention)?

**Evidence.**

- `learn-from-mistakes/SKILL.md` lines 86–93 enumerate **banned**
  vocabulary in committed lessons: `ralph`, `subagent`, `reviewer
  agent`, `Co-Authored-By: Claude`, `Assisted-by:`. AI attribution
  is **explicitly banned** in committed lesson bodies and commit
  trailers.
- AGENTS.md lesson "Forward-only compliance resolves rule-
  tightening collisions": *"PR #33's commits carry no AI-
  attribution trailer (current rule); the earlier merged
  milestone PRs #28–#32 do (prior rule, frozen in their merged
  history)."* The rule tightened on/around 2026-05-15.
- Per-commit audit of the last 30 commits shows direct contributor
  commits (`git log --no-merges --since="2026-05-15"`) are clean:
  no `Co-authored-by: Claude` trailer in the per-commit body.
- However, squash-merged PR bodies (b954448, ad0c0e8, 2e8af45,
  etc.) **do** carry `🤖 Generated with [Claude Code]` and
  `Co-authored-by: Claude Opus 4.7` because GitHub squash-merge
  preserves the PR description as the commit body. The PR
  descriptions on `main` still show this attribution.
- The repo's principal contributor is solo (Tri Lam); collaborative
  context already includes Claude — there is no audience that
  doesn't know.

**Recommendation.** **Keep current convention: no per-commit
AI attribution; no "fleet-agent" GitHub identity.** Fleet PRs
look like human PRs at the commit level. Transparency lives in
`docs/fleet-digest.md` (weekly) and `tools/fleet/state.json`
(real-time, local). The orchestrator's commit-msg hook must
strip any `Co-authored-by: Claude` / `🤖 Generated` lines from
agent-produced commit bodies before push (the agent is not
expected to remember; the hook is the falsifiable enforcement).

Squash-merge PR bodies are a *separate* surface and currently
contain attribution. **Recommend the orchestrator scrub agent-
produced PR bodies too** — both the commit and the PR-body
surfaces should be clean. The `pr-lint` workflow grows one
banned-phrase check: `🤖 Generated`, `Co-authored-by: Claude`,
`Assisted-by:` in the PR body block triggers a non-blocking
warning (so the existing pre-fleet PR bodies don't retroactively
fail).

**Justification.** Direct citation: `learn-from-mistakes/SKILL.md`
banned-vocabulary section + AGENTS.md "forward-only compliance"
lesson. The author has already paid the cost of writing the rule
twice — once in the skill, once in AGENTS.md. The fleet must
forward-comply.

**Failure detection.** Add to L5 drift detector: scan
`git log --no-merges` on the branch for any banned-vocabulary
trailer; reject the PR. Track in weekly digest: if scrubber-
caught violations rise (agents authoring forbidden trailers
despite the hook), surface as a FOLLOWUP — the prompt template
may be reintroducing the trailer. The PR-body surface gets the
same scan as a `pr-lint` advisory; advisory not blocking,
because making it blocking retroactively breaks pre-fleet PRs.

---

## Cross-cutting note

Questions 1, 3, and 6 all reduce to a single repo pattern: the
*falsifiable contract* — a `grep`/`test`/`gate` that catches
the failure mode. Q1's 14-day staleness check, Q3's `*_consumer_
test.go` requirement, Q6's commit-msg + PR-body scrubber are all
mechanizations of AGENTS.md lesson #4 ("Ceremony needs a
falsifiable consumer"). The orchestrator's job is to enforce
these contracts; the AI-gate stack is where they live.

Q2 (RFCs) and Q4 (tokens) are about *trust calibration* — how
much do we trust an agent before a human looks. The existing
two-maintainer RFC gate (Q2) and the per-lane budget (Q4) are
the trust knobs.

Q5 (review burden) ties everything together: if Q1–Q4 + Q6 are
done right, Q5's rejection threshold ratchets down naturally
because each gate is anchored to a named invariant, not vibes.

## File paths cited (absolute)

- `/Users/tree/Desktop/tracecore/tracecore/MILESTONES.md`
- `/Users/tree/Desktop/tracecore/tracecore/CONTRIBUTING.md`
- `/Users/tree/Desktop/tracecore/tracecore/AGENTS.md`
- `/Users/tree/Desktop/tracecore/tracecore/docs/rfcs/README.md`
- `/Users/tree/Desktop/tracecore/tracecore/docs/rfcs/0009-pyspy-receiver-scope.md`
- `/Users/tree/Desktop/tracecore/tracecore/docs/rfcs/0010-containerstdout-receiver-scope.md`
- `/Users/tree/Desktop/tracecore/tracecore/docs/rfcs/0011-m16-kueue-receiver-scope.md`
- `/Users/tree/Desktop/tracecore/tracecore/.claude/skills/learn-from-mistakes/SKILL.md`
- `/Users/tree/Desktop/tracecore/tracecore/.claude/skills/pr-review-loop/SKILL.md`
- `/Users/tree/Desktop/tracecore/tracecore/.claude/notes/review-patterns.md`
- `/Users/tree/Desktop/tracecore/tracecore/internal/synthesis/patterns/pod_evicted.go`
- `/Users/tree/.claude/jobs/cf57125f/reviews/03_economics.md`
