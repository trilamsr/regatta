# Economics review — Fleet Orchestrator (RFC-0012)

Calibrating three under-specified claims in the design: the 1M-token
per-milestone cap, the "maintainer-hour" stop-condition, and the
per-push L3/L4/L5 cost.

## 1. Empirical PR data

Five milestone-shaped PRs from `TraceCoreAI/tracecore`, merged in the
last 36 h. Diff size from `gh pr diff | wc`. Conversation length is
PR body + reviews + comments in chars — note this excludes in-session
agent chatter, which is the real cost driver.

| PR | Title (truncated) | Files | +/− LOC | Diff lines | Diff bytes | Conv chars | Comments+reviews |
|---|---|---:|---:|---:|---:|---:|---:|
| #105 | [m16] kueue scheduler receiver: alpha | 32 | +5647/−114 | 6,116 | 388,050 | 7,162 | 0 |
| #110 | [m14:C] receivers/kineto/ + M14 alpha | 29 | +2117/−18 | 2,351 | 97,613 | 8,512 | 0 |
| #102 | [receiver] M13 pyspy Phase 2 | 35 | +3107/−67 | 3,483 | 129,541 | 3,559 | 0 |
| #101 | [exporter] otlphttp from-scratch | 22 | +3012/−12 | 3,203 | 119,668 | 5,100 | 0 |
| #99  | [m13] Phase 1 pyspy scaffold | 30 | +2400/−0 | 2,612 | 102,639 | 4,961 | 0 |

Median: ~3,200 diff lines, ~120 KB diff, ~5 KB human-visible
conversation. Zero PR review comments — this repo merges via
self-review + AI gates, so conversation length is a poor proxy for
revision rounds; I triangulate iteration count differently below.

Tokenization rule of thumb: **~4 chars/token**. Median PR's *final
diff alone* is ~30K output tokens; all-iterations output (drafts,
deleted code, rewrites) is multiples of that.

## 2. Bottom-up per-milestone agent cost

Each iteration (read context → plan → code → `make ci` → revise) has
roughly this profile. Opus 4.7 with prompt caching on static context
(AGENTS+STYLE+PRINCIPLES+NORTHSTARS ≈ 60K tokens, cached).

**Per-iteration cost (Opus 4.7, prompt caching on):**

| Bucket | Tokens | Notes |
|---|---:|---|
| Cached repo context (read) | 60,000 | AGENTS+STYLE+PRINCIPLES+NORTHSTARS; cache hit after iter 1 |
| Milestone block + RFC (fresh input) | 4,000 | ~1K per milestone block, ~3K RFC |
| Affected dir files (fresh input) | 25,000 | ~10 files at ~2.5K tokens median for a receiver |
| `make ci` output / tool results (fresh input) | 8,000 | Test output, lint, govulncheck |
| Per-iteration fresh input subtotal | **37,000** | non-cached |
| Output: code + commentary | 8,000 | Median PR diff ÷ iterations + scratch |

Pricing (Anthropic public, 2026): Opus 4.7 = $15/MTok input, $75/MTok
output; cached read = $1.50/MTok. (Sonnet 4.6 = $3/MTok in, $15/MTok
out.)

Per iteration:
- Cached input: 60K × $1.50/M = **$0.090**
- Fresh input: 37K × $15/M = **$0.555**
- Output: 8K × $75/M = **$0.600**
- **= $1.245 per iteration, ~45K input + 8K output ≈ 53K tokens.**

**Iteration count.** GitHub is too quiet to count revision rounds.
From ralph-loop Iron Law (max 50) and M13/M14/M16 retro hints:

- Optimistic (well-spec'd): **5 iters**
- Median (typical receiver): **12 iters**
- Pessimistic (fixtures break, multiple `make ci` ratchets): **25 iters**

Plus **2 AI-gate revision rounds** at +1 iter each. Median total: 14.

**Per-milestone agent cost:**

| Scenario | Iters | Tokens (in+out) | Dollars |
|---|---:|---:|---:|
| Optimistic | 5  | ~265K | **$6.23** |
| Median | 14 | ~742K | **$17.43** |
| Pessimistic | 27 | ~1.43M | **$33.62** |

Pessimistic case **already exceeds the 1M cap**. Median is 74% of
cap. The cap is in the right ballpark but **tight** for a real
receiver milestone — see §7.

## 3. Per-gate cost (L3/L4/L5, per PR push)

Each gate is one Claude call. Diff median = 30K tokens (from §1).
No caching assumed across gates (conservative).

| Gate | Model | Input tokens | Output tokens | Per-call $ |
|---|---|---:|---:|---:|
| L3 rubric verifier | Opus 4.7 | 30K diff + 5K rubric blocks = **35K** | 2K structured JSON | 35K × $15/M + 2K × $75/M = $0.525 + $0.150 = **$0.675** |
| L4 adversarial reviewer | Opus 4.7 | 30K diff + 40K (PRINCIPLES+STYLE+NORTHSTARS subset) = **70K** | 3K objection list | 70K × $15/M + 3K × $75/M = $1.050 + $0.225 = **$1.275** |
| L5 drift detector | Sonnet 4.6 | 30K diff + 15K (MILESTONES+FOLLOWUPS sections) = **45K** | 1K drift list | 45K × $3/M + 1K × $15/M = $0.135 + $0.015 = **$0.150** |
| **Per-push total** | | | | **$2.10** |

Typical PR with 3 pushes (initial + 2 revisions): **~$6.30** in
gates, ~25% of the median per-milestone budget. **L4 dominates at 2×
L3 cost**; trim its context bundle by lazy-loading only PRINCIPLES
sections referenced by touched paths.

## 4. Per-milestone total + maintainer-hour breakeven

Sum of §2 (agent) + §3 (3 gate runs) per milestone:

| Scenario | Agent $ | Gates $ | **Total $** |
|---|---:|---:|---:|
| Optimistic (5 iters, 1 push) | 6.23 | 2.10 | **$8.33** |
| Median (14 iters, 3 pushes) | 17.43 | 6.30 | **$23.73** |
| Pessimistic (27 iters, 5 pushes) | 33.62 | 10.50 | **$44.12** |
| Catastrophic (hits 1M cap on agent) | ~24.00* | 10.50 | **$34.50** |

\* Cap = 1M at ~85% input / 15% output mix ≈ 850K × $15/M + 150K ×
$75/M = $24.00. Cap limits worst-case loss to ~$35 — works as
designed, but pessimistic scenarios *exceed* it.

**Maintainer-hour proxy.** US senior SWE fully-loaded ~$150/hr (BLS
occupational compensation + ~40% benefits/overhead; cited in Stripe
*Developer Coefficient*, McKinsey *Developer Velocity*, StackOverflow
TC bands). Conservative $100/hr; aggressive $250/hr (staff+).

| Maintainer rate | Hours equivalent to median milestone ($23.73) |
|---|---:|
| $100/hr | 0.24 h (14 min) |
| $150/hr | 0.16 h (9 min) |
| $250/hr | 0.09 h (6 min) |

**Reading:** at median ~$24, the fleet is cheaper than a maintainer
iff a human would spend >10 min on the work. L6 human-merge + reading
3 AI-gate comments easily exceeds that. The fleet is *trivially*
cheaper than maintainer-equivalent. The real threshold isn't
"cheaper than a human" — it's **"does human review burden exceed
work saved"** (the design's own non-economic stop-condition).

## 5. Sensitivity

| Knob | Low | Base | High | $ impact |
|---|---|---|---|---|
| Iterations per milestone | 5 | 14 | 27 | $6 → $34 (5.7×) |
| Rejection rounds (L4 reject rate 5% / 25% / 50%) | 0 pushes | 2 pushes | 4 pushes | $2 → $8 in gates alone |
| Context bundle for L4 (lean / full / kitchen-sink) | 30K | 70K | 120K | $0.50 → $2.00 per call |
| Cache hit rate on repo context (0 / 50% / 100%) | $5/iter | $1.5/iter | $1.25/iter | ~3× swing on agent cost |
| Hits 1M cap | — | — | yes | $34.50 capped, milestone unfinished |

Biggest lever: **prompt caching**. Without it, per-iter cost ~triples
(cached 60K → fresh at $15/M = +$0.90/iter). Make caching mandatory.

Second lever: **iteration count**, sensitive to spec quality. M16
(complete RFC, 7 falsifiable rubrics, no GPU) sits near optimistic
(~5–8 iters). M14/M13-class with fixtures sit near median.

## 6. Pilot comparison: M16 by human vs. by fleet

M16 (kueue receiver, Lane 4, RFC-0011 exists, ~5–6K LOC per sibling
#105). From #99+#102 (M13 pyspy, ~5500 LOC over 2 PRs ~24h apart),
human time-to-ship is **8–16 maintainer-hours** incl. review.

| Path | $ | Wall-clock |
|---|---:|---|
| Human maintainer ($150/hr × 12h) | **$1,800** | days |
| Fleet (median scenario) | **~$24** | hours (parallelizable) |
| Fleet (pessimistic) | **~$44** | hours |
| Fleet (cap-hit + human takeover for last mile, 4h) | **~$635** | ~1 day |

Fleet is ~75× cheaper at median *if it ships*. Real risk: cap-hit +
needs-human-to-finish, where savings evaporate into maintainer
context-reload. **The economic case rests on completion rate, not
per-token cost.**

## 7. Recommendations

1. **Raise per-milestone cap 1M → 1.5M tokens.** Pessimistic real
   milestones (~1.43M) bump against 1M; 50% headroom keeps recoverable
   runs from being killed early. Ceiling rises $24 → $36, still trivial.

2. **Per-lane budgets.** Hardware-fixture lanes (Lane 5 GPU/DCGM,
   Lane 6 kernel) get **2M**; pure-Go receiver lanes (Lane 4) keep
   **1M**; doc-only **300K**. Codifies iteration sensitivity.

3. **Drop the "maintainer-hour equivalent" stop-condition.** Trivial
   to satisfy ($24 ≪ $150), not a real stop. Replace with
   **completion-rate**: *"abandon if <60% of dispatched milestones
   reach human-merge within budget over a rolling 10-milestone
   window."*

4. **Mandate prompt caching** in agent invocations. Document the
   required cache control in `tools/fleet/prompts/milestone.md`. ~3×
   savings is free.

5. **Trim L4 context** to diff-path-relevant PRINCIPLES sections only.
   ~50% L4 cost cut with no review-quality regression expected.

6. **Add cost telemetry** to `tools/fleet/state.json`: per-agent
   tokens-in/out, cumulative $, cache-hit ratio. Without it we have
   no post-pilot data to firm these estimates up.

## 8. Caveats & data we'd need post-pilot

- 0/5 sampled PRs had review comments — review style is opaque to
  GitHub-API analysis. Iter counts are inferred. **Need:** ralph-loop
  iter logs from Phase 1 to ground-truth.
- 4 chars/token is heuristic; Go with long identifiers is closer to
  3.5. Estimates ±15%.
- Pricing assumes interactive (not batch) Anthropic rates. Batch mode
  (50% off) isn't viable for agent loops.
- 100% cache-hit on repo-context from iter 2 is optimistic; AGENTS.md
  edits mid-loop invalidate it.
- $150/hr is a US-senior median, not tracecore-specific. Adjust to
  your team's loaded rate.
- §6 "12h human-ships-M16" anchors on M13 pyspy (sibling lane). If
  M16 is structurally simpler, breakeven shifts fleet's way; more
  complex, the other way.

**Bottom line:** token cap is roughly right but tight; maintainer-hour
stop is the wrong dimension; gates are ~$2/push; a median milestone
runs ~$24 vs. ~$1,800 of maintainer time. ~75× cheaper **conditional
on completion rate** — which the pilot must measure but the design
doesn't codify as a stop. Fix that.
