# Adversarial review — RFC 0012 Fleet Orchestrator

## Top three load-bearing objections

### A. The "zero merge conflicts in 50 commits" finding is a measurement artifact, not a property of the repo.

> "Empirical analysis of the last 50 commits found **zero merge conflicts** at the three named 'cross-lane chokepoints' — the chokepoints are largely *theoretical*, not measured." (lines 47–50)
> "**No lock needed; merge queue serializes.**" (repeated six times, lines 251–264)

`git log --merges --oneline -100` in the actual repo returns **two** merge commits total, and one of them is a Dependabot PR. The repo squash-merges everything to a linear history (see `git log --oneline -50`: every entry is a single squashed commit ending in `(#NN)`). **Merge conflicts at squash time never appear in `git log --merges`.** They surface as rebase failures and "this branch has conflicts that must be resolved" UI states that leave no git trace once resolved. The investigation that produced this finding was looking for an artifact that this repo's workflow cannot produce.

The "merge queue serializes" hand-wave is doing the rest of the work. At current velocity (~1 PR/day per the `git log --since='3 months'` sample) the merge queue *is* the lock — there is at most one PR in flight against `main` at a time. The design proposes one agent per lane plus unbounded cross-lane parallelism. The hottest files are exactly the shared docs the design dismisses: `docs/FOLLOWUPS.md` (34 touches in 3 months), `Makefile` (30), `CHANGELOG.md` (28), `MILESTONES.md` (20), `AGENTS.md` (18), `go.mod` (15). Every agent edits MILESTONES.md (to flip rubric prefixes). Every agent edits CHANGELOG.md via the release-notes block. At 6× velocity these will rebase-fight constantly, and the merge-queue strategy becomes "everyone rebases on every push," which is the survivorship bias the design rules out by assertion.

**Change:** retract the "no lock needed" finding. Measure conflict frequency by reconstructing rebase rejections from `gh pr` event logs (or simply by running the pilot in shadow mode and counting how many agent pushes get rejected). The lane scheduler probably *does* need an exclusion lock on at least MILESTONES.md, CHANGELOG.md, and `go.mod`.

### B. The "297 falsifiable rubrics" claim is inflated by ~3×, and a sample of the real rubrics shows many are not single-shot oracles.

> "tracecore has 297 falsifiable rubric bullets indexed by milestone, each citable as a single oracle." (line 35)
> "The rubric *is* the oracle — this is a much sharper review than generic 'does this implement the spec.'" (line 171)

Direct grep against `MILESTONES.md` (`grep -cE "^\s*[-*]\s*[☐⧗☑]"`) returns **101** bullets, not 297. Even if you include legacy text under struck-through milestones the order-of-magnitude is wrong.

More importantly, sampling actual `☐`/`⧗` rubrics, several are *not* the single oracles the design promises. Examples taken verbatim from the file:

- M6 R261: `docs/integrations/{datadog,honeycomb,otel-backend,clickhouse-direct}.md each contain ≥1 fenced \`\`\`yaml block whose first non-blank line resolves to a file under docs/integrations/examples/ that the validator-recipe CI job runs validate --config= against. Gate splits by recipe scope: tracecore validate for examples naming only compiled-in components; otel/opentelemetry-collector-contrib (digest-pinned in lockstep with each recipe's <!-- tested-against: vX.Y.Z --> marker)…`
- M11 R554: `Overhead within NORTHSTARS O2: idle ≤0.01 Mbps, dump-active ≤0.5 Mbps, RSS ≤30 MB. Measured by bench/overhead/nccl_fr_bench_test.go replaying 1 GB of fixtures. (Bench replays fixtures and reports HeapAlloc deltas; the bench is advisory per its preamble — promoting it to a fail-closed CI gate against the three named targets is the carry-forward.)`

R261 is a compound rubric — it bundles file-existence, fenced-block parsing, path resolution, an external Collector pin, and a CI gate. An L3 verifier reading "evidence_path + evidence_strength" cannot do this without effectively re-implementing the recipe lint. R554 is *self-deferring* — it carries its own carry-forward clause ("promoting it to a fail-closed CI gate … is the carry-forward"), so the rubric is satisfied today by an advisory bench. An adversarial verifier prompted "is this rubric satisfied" will reasonably answer yes; a maintainer reading the same rubric understands "still mostly advisory." The oracle is not single-shot; it depends on which clause you read.

**Change:** before claiming rubrics are oracles, pick 20 random `☐`/`⧗` rubrics and have a current Claude judge them blind. Report false-pass and false-fail rates. Until that grounds the design, the L3 prompt-shape is a hand-wave.

### C. L4 is a downgrade of pr-review-loop dressed up as the same thing, and the design admits it in passing.

> "L4 — AI adversarial reviewer (new). … Single shot per PR revision (no internal looping — the orchestrator runs it once per agent push). Recommended model: Opus 4.7." (line 184)
> "`.claude/skills/pr-review-loop/SKILL.md` — 5-phase review pattern (inspiration for L3/L4/L5 prompt shapes)" (line 445)

The actual `pr-review-loop` SKILL is a 5-phase, ~14-subagent process with a validation cycle (research → validate → contradict → re-validate → synthesize), TDD enforcement on accepted findings, mutation-verify on tests, an evolving rubric, and 5 commits' worth of structured evidence. It explicitly downgrades findings without proposable hard proof to NIT. It refuses promises without a `<readiness-audit>`.

L4 is "one shot, one prompt, output a list of objections." It is the **Phase 3 adversarial pass alone**, missing Phases 1, 2, 4, 5, the validation cycle, and the TDD discipline. Calling it "the adversarial reviewer" trades on the pr-review-loop brand to make a much weaker check sound rigorous. The design lists pr-review-loop under "Available skills" for the *agent* (line 127) but does not require the agent to run it before opening a PR — so the rigorous pass exists in the skill catalog and is never actually invoked.

**Change:** either (1) require agents to run pr-review-loop in the worktree before `gh pr create` (gated by the readiness-audit promise), and downgrade L4 to a secondary check, or (2) name L4 honestly as "single-pass adversarial sniff test" and don't pretend it replaces the loop. The current framing oversells the review surface to maintainers.

---

## Additional findings

### 1. AGENTS.md is at 167 lines against a hard 150-line cap, and the design proposes recursive agent-driven writes to it.

> "Lesson capture. Runs `learn-from-mistakes` capture flow at the end of each agent's life: … draft an AGENTS.md / .claude/notes/ entry and queue a small PR for human approval. The orchestrator itself improves the substrate." (lines 234–238)

`AGENTS.md` is **167 lines today**; the skill enforces a 150-line cap. The file is already over budget. The capture flow rejects on `Brief-cap violation` — meaning the orchestrator's lesson-capture path will fail closed on its first attempt against `AGENTS.md`, or it will silently route every lesson to `docs/notes/<topic>.md` instead. Neither failure mode is addressed.

More importantly, AGENTS.md is **auto-loaded into every agent session.** A bad lesson does not just pollute one PR — it taints every agent spawned thereafter. Containment story in the design: "queue a small PR for human approval." The volume question is unaddressed: at 6× velocity with rejection-driven lesson capture, the maintainer's queue *is* the orchestrator's review burden, and it grows with rejection rate. There is no rate limit on lesson PRs and no rollback story for a lesson that turned out wrong six agents later.

**Change:** budget the lesson-capture rate explicitly (max 1 lesson PR per N merged milestone PRs, say 5). Forbid agent-authored writes to `AGENTS.md`; route everything to `docs/notes/<topic>.md` and `.claude/notes/<topic>.md` where a bad lesson is opt-in. Require lesson PRs to cite the friction trace concretely.

### 2. The "gaslighting" failure mode is missing from the failure-modes table.

An agent that wants to flip a rubric to `☑` knows the L3 verifier reads "the PR diff + cited evidence." The cheapest path to acceptance is a test that *names* the rubric in its docstring and asserts a trivial invariant. R554 above (`bench is advisory per its preamble`) is exactly the surface where this happens: write a test that prints the three numbers, claim it satisfies the rubric, cite line numbers. An Opus verifier reading the diff and the rubric in isolation has no way to know the bench is advisory unless it reads the preamble — and the preamble is not in the diff.

Mutation-verify (from pr-review-loop) is the existing defense; L3 doesn't require it. The failure-modes table covers "agent burns token budget" but not "agent ships a fake test that satisfies the oracle." The cited AGENTS.md lesson #4 (ceremony without falsifiable consumer) is *exactly* the smell L3 produces unless it has a defense.

**Change:** require L3 to invoke the test it cites with a mutation injected (e.g., flip the asserted value), confirm the test fails, restore. This is computable. Without it, L3 grades the proof's existence, not the proof's force.

### 3. The "L4 reliably rejects everything" failure mode is missing.

The design caps rejection loops at K=3 then drops to "needs human." It does not measure whether L4 is *calibrated*. An over-cautious adversarial reviewer that flags every PR with a CONCERN-or-worse finding (entirely plausible at Opus 4.7 with the "find reasons to reject" prompt) routes every agent PR through K=3 rejections and into the maintainer queue. The maintainer-hour saved by the gate goes negative.

Detection requires a ground-truth corpus (the "synthetic-bad" mentioned for L3) plus a parallel known-good corpus. The design has no known-good corpus and no false-reject metric.

**Change:** Phase 0 should build *both* a synthetic-bad and synthetic-good corpus and report L4's confusion matrix before the pilot. Stop condition #1 (`>20% false-pass`) is silent on false-reject rate; add a `>40% false-reject` parallel stop.

### 4. The 1M-token-per-milestone budget is naïve given recent PR sizes.

`gh pr view` on the three milestones the design cites as candidates:

- M16 alpha (#105): **5,647 additions, 32 files**.
- M13 Phase 2 (#102): **3,107 additions, 35 files**.
- M13 Phase 1 (#99): **2,400 additions, 30 files**.

The 5-phase pr-review-loop alone runs ~14 subagent dispatches plus author iteration. With 30+ files of context per dispatch, input cost dominates: a 5k-line diff loaded into 14 subagents is ~70k tokens of diff context alone, before each subagent reads PRINCIPLES.md (176 lines), STYLE.md (191), MILESTONES.md (663), and AGENTS.md (167) — another ~1.2k lines per subagent. Estimated total: 1–2M tokens for review alone, on top of the agent's own implementation budget (read → plan → write tests → write code → iterate `make ci` to green). At Opus 4.7 pricing, this overruns 1M every time for a milestone that ships in one PR; for milestones that need three PRs it overruns by 3×.

The "maintainer-hour equivalent" stop condition is also wrong-shaped. The unit "what a maintainer costs" includes opportunity cost (the maintainer not doing the *next* milestone) and the upside of the maintainer learning the codebase. An agent that costs the same dollars produces no learning. Cost parity is not value parity.

**Change:** budget 5M tokens per milestone for Phase 1, measured. Add a stop condition on token-per-rubric-shipped, not token-per-milestone.

### 5. The "RFC drafting" open question is load-bearing and the deferred answer is wrong.

> "Default: agents *can* draft, but RFC PRs require two maintainer approvals…" (line 398)

Six of the listed `☐` milestones need RFCs that don't exist (M12, M14, M17, M18, M20, M24). The orchestrator's spec watcher will pick these up because their `Depends on:` is satisfied — the dependency graph doesn't encode "RFC must exist first." The agent will then either (a) draft the RFC silently as part of the implementation PR (violating CONTRIBUTING.md and burning a rejection cycle), or (b) halt on spec ambiguity, file a FOLLOWUPS row, and unspawn. Either way the orchestrator burns spawn budget on milestones it cannot ship.

**Change:** the spec watcher must also check "has linked RFC" as a precondition. Milestones without an RFC are NOT eligible until a human (or a different agent class with different gates) drafts one.

### 6. Cross-milestone coupling is dismissed by reference to a doc the design doesn't claim is authoritative.

> "Proposed: the dependency graph in MILESTONES.md is the coupling-detection oracle; the orchestrator only spawns agents whose Depends on: chain is satisfied." (line 405)

Cross-milestone coupling is exactly the failure mode AGENTS.md load-bearing lesson #9 (cross-receiver join contracts) was added to address — "M-X emits Y" is implicitly a contract with "M-Z consumes Y". The `Depends on:` chain in MILESTONES.md captures the coarse-grained build dependency, not the *attribute-name-level* contract that PR #94 caught. The orchestrator spawning M18 the moment M17's `Depends on:` flips to `☑` will reproduce the "first-author-pays, second-author-fights-the-amendment" failure mode at machine speed. No detector for it is proposed.

**Change:** L5 (drift detector) should grep every PR's diff for attribute names against the union of all `*emits*` rubrics across `⧗`+`☐` milestones, and flag introductions that don't reconcile with consumers. This is the load-bearing piece of pre-merge cross-checking the AGENTS.md lesson asks for.

### 7. Brand risk against NORTHSTAR §O3 ("trust under load is the product") is unmentioned.

The repo's positioning is that operators can audit tracecore "without trusting us." The NORTHSTAR explicitly names "trust under load is the product" as a P0 cultural principle. Shipping a binary substantial fraction of which was written by agents under a one-shot adversarial reviewer is a brand-risk decision that needs to happen *in the open*, with the design-partner audience, before the pilot, not silently. The design's "Public exhaust" open question (line 414) defers to "keep current convention, no AI attribution" — i.e., quietly. That is exactly the surface where O3 promises an auditable trail and the design promises an opaque one.

**Change:** raise this to the maintainer audience as a NORTHSTAR-touching decision. At minimum, every fleet PR needs a label and a public exhaust trail that the security team of a hypothetical design partner can audit. "no AI attribution" is incompatible with "trust under load is the product."

### 8. Three things to cut.

Applying AGENTS.md lesson #4 ("Ceremony without a falsifiable consumer is bloat — `grep -c '<name>' <file> == 0` is the test") to the design itself:

- **`tools/fleet/state.json` weekly digest PR to `docs/fleet-digest.md`** (lines 304, 309–311). No falsifiable consumer named. If maintainers do not act on it, it is bureaucracy. Cut until a human asks for it.
- **The "Escape valve" `≥3 merge-queue rebase failures in a rolling 24h window`** (line 273). Detection threshold is arbitrary, the metric is uninstrumented, and the response (throttle to one in-flight PR globally) is what we should do *by default* in Phase 3 — not gate on a fake metric to undo a premature optimization.
- **L5 drift detector as a separate gate** (lines 187–191). The design admits the rule is "did you update the right tracking doc"; this is a pre-commit hook or an `awk` check, not an LLM call. AGENTS.md status-drift rule is already grep-shaped (`PR that touches files under a tracked item's named scope without updating the corresponding tracking doc`). Implement as a `make ci` sub-gate; do not pay Sonnet 4.6 per PR for an `awk` job.

---

## Summary

The design's three load-bearing claims — "no lock needed" (empirically based on the wrong artifact), "297 falsifiable rubrics as oracles" (count is wrong, sample fails the oracle test), and "L4 adversarial review" (a single-pass downgrade of an existing 5-phase skill) — all weaken under direct evidence. The recursive lesson-capture loop is unbounded and writes to a file that is already over budget. The cost model is off by at least 2–5×. Brand risk against the project's stated NORTHSTAR is unexamined.

The design is not unsalvageable. The *pilot* (Phase 0 + Phase 1, one milestone, human-spawned) is exactly the right shape to surface these issues empirically. The error is in the framing of Phase 2–3 as a foregone conclusion. Run Phase 0 first; report the false-pass/false-reject corpus numbers, the token cost, and the conflict-frequency reconstruction; then re-write Phases 2–3 against measured ground truth.
