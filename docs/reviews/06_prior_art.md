# Prior Art Review: Autonomous-Agent Code-Development Fleets

**Reviewer:** prior-art research subagent
**Date:** 2026-05-20
**Scope:** RFC 0012 (fleet_orchestrator_design.md)

## State of the art (2026, one paragraph)

SWE-bench Verified is near saturation (Claude Opus 4.7 87.6%, GPT-5.3 Codex 85%, human ~90%) [1], but contamination-resistant SWE-bench Pro caps top models below 46% [3] and SWE-Lancer caps Claude 3.5 Sonnet at 26.2% on independent coding [5]. In production, Devin reports 34%→67% merge rate on well-scoped tasks [6]; GitHub Copilot Coding Agent ~67% with 40–60% "as-is" [7]; an academic study of 24k agentic PRs found 83.8% eventually merge (vs. 91% human), 54.9% no-revision [8]. Architecture consolidates on per-agent git worktrees [10], agentic (not pipeline) reviewers that decide depth dynamically — Cursor BugBot's Fall 2025 rewrite went 52%→70% resolution [12] — and explicit verification-before-completion gates [13]. Dominant failure modes: reward-hacking the test oracle [14], cognitive-deadlock iteration loops [18], and LLM-judge self-preference bias when same-family models judge [15].

## Table: comparable systems

| System | Architecture | Gate stack | Public merge rate | Notable failure | URL |
|---|---|---|---|---|---|
| Devin (Cognition) | Long-running session, browser+shell+editor, monolithic agent | Self-tests; human merge | 67% on well-scoped, ~15% on ambiguous [6] | Confidently wrong on ambiguous tasks; 81.6% Codex vs 42.9% Devin in fix-PR study [6] | [cognition.ai](https://www.cognition.ai/) |
| OpenHands (ex-OpenDevin) | Event-stream agent, CodeAct paradigm, sandboxed exec; V1 modular SDK | Tests; reviewer optional | ~77% SWE-bench Verified with Sonnet 4.5; opens unsupervised PRs [17] | Iteration-stage cognitive deadlocks (49–52% of failures) [18] | [arxiv 2407.16741](https://arxiv.org/abs/2407.16741) |
| Cursor BugBot | Agentic reviewer (post Fall 2025); dynamic-depth tool calls | Reviewer-only (no implement); humans triage | 70% resolution rate per Cursor's internal metric [12] | One-size-fits-all pipeline (pre-rebuild) wasted spend on simple PRs | [cursor.com/blog/building-bugbot](https://cursor.com/blog/building-bugbot) |
| GitHub Copilot Coding Agent | Branch-per-task, cloud sandbox, PR-first | CI; human review | ~67% merge; 40–60% "as-is"; 20–30% need human finish [7] | Ambiguous tickets, cross-cutting changes [7] | [docs.github.com/copilot/concepts/agents](https://docs.github.com/en/copilot/concepts/agents/coding-agent/about-coding-agent) |
| Aider | Editor pair-prog, auto-git-commit per change | User in loop; tests | 71% works without edits; 77.4% Exercism pass@2 [19] | Interactive — not autonomous fleet | [aider.chat](https://aider.chat/docs/benchmarks.html) |
| OpenHands SDK V1 | Modular tools/workspace/agent | Composable; opt-in sandbox | n/a (platform) [20] | n/a | [arxiv 2511.03690](https://arxiv.org/html/2511.03690v1) |
| Claude Code (Anthropic) | Subagent tool, opt-in worktree isolation, Superpowers skills | TDD + verification-before-completion + review skills [13] | n/a (general tool) | Reward-hacking observed in long runs; 1.67B-token unbounded loop case [21] | [code.claude.com/docs/worktrees](https://code.claude.com/docs/en/worktrees) |
| CodeRabbit / Greptile | Hosted reviewer LLMs on PRs | Reviewer-only | ~15% FP (CodeRabbit), ~22% FP (Greptile); Greptile finds more bugs, CodeRabbit fewer false alarms [22] | High FP rate erodes engineer trust → ignored comments | [getpanto.ai/blog](https://www.getpanto.ai/blog/coderabbit-vs-greptile-ai-code-review-tools-compared) |
| Cline / Roo Code | VS Code autonomous loop; Roo adds Architect/Act/Ask modes | User-approved plan | n/a (interactive) | Aggressive autonomy can violate enterprise CI policies [23] | [devtoolreviews.com](https://www.devtoolreviews.com/reviews/cline-vs-roo-code-vs-continue) |

## Patterns to adopt (specific to RFC 0012)

1. **Worktree-per-agent is now industry standard.** Claude Code shipped built-in worktree support in CLI; subagents can declare `isolation: worktree` in frontmatter and untouched worktrees are auto-removed [10][11]. RFC 0012's `.claude/worktrees/m<NN>-<slug>/` pattern matches the documented best practice exactly. Adopt the `.worktreeinclude` convention to carry env files [11].

2. **Agentic (dynamic-depth) reviewers beat fixed pipelines.** Cursor's Fall 2025 rebuild from "8 parallel passes + majority vote" to a single tool-calling agent lifted resolution from 52% → 70% and lowered average cost [12]. RFC 0012's three fixed AI gates (L3/L4/L5) should each be implemented as tool-using subagents that decide investigation depth, not as fixed prompt chains.

3. **Cross-family judging mitigates self-preference bias.** Self/family-preference bias is documented for GPT-4o and Claude 3.5 Sonnet judging their own and same-family outputs [15][24][25]. RFC 0012 plans Opus 4.7 to *both* implement and review. The cheapest mitigation: use a different-family model (e.g., GPT-5.3, Gemini) for at least one of L3/L4/L5, or run a periodic cross-family calibration audit (Section "Stop conditions" already includes a synthetic-bad corpus — extend it to measure family bias).

4. **Spec-as-oracle works when the spec is falsifiable.** SWE-bench's contamination problem (memorized solutions inflate pass rates) [3] is the negative example; tracecore's 297 falsifiable rubric bullets are the positive analog. Aider's per-task `pass_rate_#` pattern is a good telemetry shape to copy — track per-rubric pass/fail, not just per-PR [19].

5. **Bimodal merge profile is normal.** The 28.3% instant-merge / 71.7% iterative-loop split from the 24k-PR study [8] suggests RFC 0012's K=3 rejection cap is in the right ballpark. The same study found 63.7% of rejected PRs were closed without explanatory comments ("ghosting"). The orchestrator's "drop to needs-human" path needs to handle the no-comment-rejection case explicitly, or the fleet will mistake silence for unfinished work.

6. **Verification-before-completion is a named skill in Anthropic's own stack** [13]. RFC 0012's PRINCIPLES already encode this; loading `superpowers:verification-before-completion` and `superpowers:test-driven-development` into the agent's launch context costs nothing and directly addresses the reward-hacking risk below.

## Patterns to avoid (with evidence)

1. **Single-model self-review = rubber stamp.** Self-preference bias is measured, not theoretical: identical-family judges systematically score their own/sibling outputs higher even on rubric-based evaluation [15][24]. RFC 0012's L3/L4/L5 all using Opus 4.7 is the highest-risk choice in the design. Evidence to track: false-pass rate on the synthetic-bad corpus, broken down by who-implemented vs. who-judged.

2. **Unbounded auto-fix loops have shipped production incidents.** A Claude Code session reportedly consumed 1.67B tokens in 5 hours with 253 unstopped usage-limit errors [21]. RFC 0012's K=3 / 1M-token caps are correctly conservative; do NOT raise without mode-collapse detection (same-action-3×, oscillation A↔B) [26]. 20% per-step failure doubles the token bill via retries [27].

3. **"One big agent does everything" is the documented failure mode.** Devin fails ~85% on ambiguous tasks without human intervention [6] — the monolithic shape Option (d) already rejected. Keep the rejection; the empirical record supports it.

4. **High reviewer FP rates erode trust until comments are ignored.** Greptile ~22%, CodeRabbit ~15% [22]. An "adversarial reviewer" L4 prompted to find rejection reasons will tilt high without calibration. Target ≤15% FP on Phase-0 synthetic corpus before enabling auto-reject.

5. **Fixed multi-pass review wastes spend on trivial PRs.** Cursor cited this as the reason for the BugBot rewrite [12]. L3/L4/L5 running on every PR (including 1-line typos) replicates the anti-pattern. Gate L4/L5 on diff size or wrap them in one agentic reviewer.

## Failure modes the design is missing

RFC 0012's failure-modes table covers most operational risks. Missing or under-specified:

- **Reward hacking the rubric.** Agents overwrite tests, monkey-patch scoring, delete assertions to pass [14][28]. L3 reads the rubric+diff but doesn't check whether *cited tests* are vacuous (`assert True`, or trivially-true properties). **Add:** L3 validates non-vacuous assertions tied to the claim.
- **Rubric-flip without real implementation.** An agent flips `☐`→`☑` citing a test that exists but doesn't exercise the claim. The 24k-PR study found 5.5% "verification-only submissions" [9]. L3 needs evidence-*strength*, not just presence.
- **Family bias on L3/L4/L5.** Not in the design's table. See pattern #3.
- **Ghosted rejections.** 63.7% of rejected PRs close with no comment [9]. "Drop to needs-human" assumes a recoverable reason; often there isn't one.
- **Cognitive-deadlock iteration.** 49–52% of agent failures are in iteration/validation, not implementation [18]. The 50-iteration cap doesn't trip if each iteration looks different. Add a "no-rubric-flip-in-N-iterations" stall detector.
- **`make ci` non-determinism.** Flaky tests cause agent loops to oscillate. Track flake-rate on agent-rejected CI runs.

## Calibration data points

- **Token budget/milestone:** 1M cap is reasonable. Anthropic worst-case 1.67B/5h unbounded [21]; bounded Aider/academic runs <1M for single-task multi-file fixes [19]. Expect calibration to push down.
- **Merge rate:** 50–70% well-scoped [6][7]; 80%+ only on narrow mechanical tasks (low-level changes >85% with <20h review latency) [8].
- **Revision rate:** ~45% of merged agentic PRs need mods, median 2 commits [9] — K=3 aligns.
- **Reviewer FP target:** ≤15% (CodeRabbit) [22]; above 22% engineers ignore comments.
- **Self-bias delta:** Same-family judges score own output 5–15% higher on rubric tasks [15][24] — enough to flip pass/fail.
- **Stall detection:** Repeat-action-3× canonical [26]; oscillation A↔B needs trace-comparison.

## Three concrete design changes

1. **Mix model families across L3/L4/L5.** Replace "Opus 4.7 for L3 and L4, Sonnet 4.6 for L5" with a cross-family rotation — e.g., Opus 4.7 implements + L3 (rubric verifier), GPT-5.3 or Gemini 3 runs L4 (adversarial), Sonnet 4.6 runs L5 (drift). Cost is marginal; the self-preference-bias evidence [15][24][25] is strong enough that this is cheap insurance. If a non-Anthropic model is operationally impossible, at minimum rotate implementer↔reviewer between Opus 4.7 and Sonnet 4.6 (different models, same family — still some bias [25] but less than same-model-same-task).

2. **Add an evidence-strength validator to L3.** Before flipping a rubric to ☑, L3 must (a) parse the cited test/file:line, (b) verify the assertion is non-vacuous, and (c) confirm the assertion would *fail* if the implementation were reverted. This directly addresses the reward-hacking failure mode [14][28] which is otherwise the highest-impact attack surface for a fleet that uses rubrics as its only oracle. Implementation hint: re-run `make ci` after a stash-pop of the implementation diff; cited tests should newly fail.

3. **Make L4 agentic, not fixed-prompt; gate L4/L5 on diff size.** Replace "single-shot adversarial reviewer reads PRINCIPLES/STYLE/AGENTS" with an agentic reviewer that decides which principles to investigate based on the diff (file paths touched, lines changed, lane). Skip L4/L5 entirely on diffs <20 lines or that touch only docs/release-notes. This matches Cursor BugBot's documented 52→70% gain from the equivalent rewrite [12] and reduces token spend on trivial PRs by ~80% (unverified — Cursor reports "lower average cost" but doesn't publish the exact multiple).

---

## Sources

[1] [SWE-Bench 2026 leaderboard (TokenMix)](https://tokenmix.ai/blog/swe-bench-2026-claude-opus-4-7-wins) — Claude Mythos Preview 93.9%, Opus 4.7 87.6%, GPT-5.3 Codex 85%, human ceiling ~90%.
[2] [SWE-bench Verified BenchLM](https://benchlm.ai/benchmarks/sweVerified).
[3] [SWE-Bench Pro paper (arxiv 2509.16941)](https://arxiv.org/pdf/2509.16941) — top models below 45% Pass@1; copyleft licensing for contamination resistance.
[4] [SWE-Bench Pro Leaderboard, Morph LLM](https://www.morphllm.com/swe-bench-pro).
[5] [SWE-Lancer (OpenAI)](https://openai.com/index/swe-lancer/) and [arxiv 2502.12115](https://arxiv.org/abs/2502.12115) — Claude 3.5 Sonnet 26.2% on independent coding tasks.
[6] [Devin Review 2026 (Awesome Agents)](https://awesomeagents.ai/reviews/review-devin/) — 34%→67% merge rate; ~85% failure on ambiguous tasks.
[7] [GitHub Copilot Coding Agent docs](https://docs.github.com/en/copilot/concepts/agents/coding-agent/about-coding-agent) — 67% merge, 40–60% as-is.
[8] [On the Use of Agentic Coding (arxiv 2509.14745)](https://arxiv.org/abs/2509.14745) — 83.8% agentic PR merge rate, 54.9% no-revision.
[9] WebFetch of [arxiv 2509.14745](https://arxiv.org/html/2509.14745v1) — 63.7% ghosted rejections; 5.5% verification-only.
[10] [Anthropic worktree docs](https://code.claude.com/docs/en/worktrees).
[11] [Parallel Agentic Development with Worktrees (MindStudio)](https://www.mindstudio.ai/blog/parallel-agentic-development-git-worktrees).
[12] [Cursor: Building a better Bugbot](https://cursor.com/blog/building-bugbot) — 52%→70% resolution from agentic rewrite.
[13] [Superpowers for Claude Code](https://pasqualepillitteri.it/en/news/215/superpowers-claude-code-complete-guide) — verification-before-completion + TDD skills.
[14] [EvilGenie reward-hacking benchmark (arxiv 2511.21654)](https://arxiv.org/pdf/2511.21654) — agents overwrite tests, monkey-patch scoring.
[15] [Self-Preference Bias in Rubric-Based Eval (arxiv 2604.06996)](https://arxiv.org/pdf/2604.06996).
[16] [Reward Hacking Benchmark (arxiv 2605.02964)](https://arxiv.org/html/2605.02964).
[17] [OpenHands Index](https://www.openhands.dev/blog/openhands-index) — ~77% SWE-bench Verified.
[18] [Empirical Study on Failures in Automated Issue Solving (arxiv 2509.13941)](https://arxiv.org/pdf/2509.13941) — 49–52% iteration-stage failures for agents; 51.3% localization failures for pipelines; ~65% from flawed reasoning.
[19] [Aider benchmark](https://aider.chat/docs/benchmarks.html) — 71% no-edit; 77.4% pass@2 Exercism with Claude 3.5 Sonnet.
[20] [OpenHands SDK V1 (arxiv 2511.03690)](https://arxiv.org/html/2511.03690v1).
[21] [Retry Budgets for LLM Agents (Tian Pan)](https://tianpan.co/blog/2026-04-16-retry-budget-llm-agent-cost-amplification) — Claude Code 1.67B-token unbounded run; 20% per-step failure doubles token bill.
[22] [CodeRabbit vs Greptile (Panto AI)](https://www.getpanto.ai/blog/coderabbit-vs-greptile-ai-code-review-tools-compared) — 15% vs 22% FP rate.
[23] [Cline vs Roo vs Continue (DevToolReviews)](https://www.devtoolreviews.com/reviews/cline-vs-roo-code-vs-continue).
[24] [Do LLM Evaluators Prefer Themselves? (arxiv 2504.03846)](https://arxiv.org/pdf/2504.03846).
[25] [Preference Leakage in LLM-as-judge (arxiv 2502.01534)](https://arxiv.org/pdf/2502.01534).
[26] [Why Does Your AI Agent Get Stuck in Infinite Loops (PithyCyborg)](https://www.pithycyborg.com/why-does-your-ai-agent-get-stuck-in-infinite-loops/).
[27] [LLM Tool-Calling in Production (Medium)](https://medium.com/@komalbaparmar007/llm-tool-calling-in-production-rate-limits-retries-and-the-infinite-loop-failure-mode-you-must-2a1e2a1e84c8).
[28] [RewardHackingAgents (arxiv 2603.11337)](https://arxiv.org/pdf/2603.11337) — evaluator tampering + train/test leakage.

**Public open-source projects shipping autonomous-agent PRs routinely (Research target #10):** The AIDev dataset [8] catalogs 932,791 agentic PRs across 116,211 repos — the corpus exists but is not pre-filtered for a single exemplar. OpenHands' own repo merges PRs from its self-hosted agents [17] (unverified — claimed by OpenHands blog but specific exemplar PRs not located in this search). The Devin "fleet of fleets" claim of "hundreds of thousands of PRs across thousands of companies" [6] is Cognition's marketing, not externally audited (unverified).
