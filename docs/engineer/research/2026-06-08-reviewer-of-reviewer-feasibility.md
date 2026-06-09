# Reviewer-of-Reviewer (RoR) feasibility — #1087

Verdict: **opt-in** (`gates.reviewer_of_reviewer.enabled: false` default; flip on for load-bearing surfaces).

## 1. Current reviewer surface

- Template: `docs/engineer/dispatch-templates/reviewer.md` (107 LOC; adversarial, read-only, 1 aggregate issue per PR).
- Spawn: operator main-thread dispatch + implementer self-spawn. No code path in `internal/orchestrator/spawner/claude.go` calls reviewer.md — prompt-driven.
- Enforcement: `scripts/check-reviewer-verdict.sh` + `scripts/lib/reviewer-verdict/verdict.sh` require `Reviewer-agent-id:` + `Reviewer-recommendation: APPROVE` on load-bearing PRs; allowlists agent-id shapes; rejects self-tag.

## 2. RoR shape

Second adversarial pass: "audit first reviewer — wrong calls? missed calls?" Distinct agent-id; PR footer carries `Reviewer-agent-id:` AND `Reviewer-of-reviewer-id:`. Gate: both IDs required, must differ, allowlist match. Escape: `<!-- ror-skip: <reason> -->`.

## 3. Session-1106 evidence

Sampled 10 merged PRs (#1068, #1100–#1106): all 10 carry single Reviewer-agent-id + APPROVE. Single-pass discipline is held.

Operator-attested miss: fresh reviewer `a561933efd8dac1ae` audited prior reviewer passes at session end and flagged **5 of 6 prior findings as factually wrong** (#1087 body). Specific session-1106 load-bearing PRs where first-pass attestation was structurally weak:

- **#1050** (`fix/1046-automerge-rebased`, automerge gate) — operator-opened after agent tool-denial; #1106 retro-added bypass marker because original review could not be re-attested.
- **#1052** (`regatta/agent-4`, prwatch branch detection) — operator-opened, same pattern.
- **#1068** (selector honoring) — operator-opened, same.
- **#1023** (retro adversarial fixes) — 4 findings (#1004/#1005/#1006/#1007) caught only when operator spawned a retro reviewer; first-pass reviewers on parent PRs missed all 4.
- **#1021** (goose package-global race) — surfaced via `feedback_double_fail_root_cause` retro, not first-pass review.

Three operator-opened mid-session PRs with weak attestation + 4 retro-caught findings on #1023 + #1021 mis-classified as flake: three independent observation windows confirm first-pass misses material findings.

## 4. Cost

Session-1106 spawned ~8 reviewer subagents on load-bearing PRs. RoR doubles to ~16 — order-of-magnitude same as one implementer wave (3-4 parallel × 2 rounds), under the 5+ shared-quota cap (`feedback_parallel_safety`). Latency: serializes one extra ~5-min round per load-bearing PR. Net ~40 min added per 8-PR session for a 5/6 false-negative rescue. Favorable.

## 5. Confidence calibration

5/6 is an **upper bound** — operator selects findings worth re-auditing. True miss rate likely 30–60% on subtle/cross-package findings, ~0% on grep-able rule violations (already covered by `check-tdd`, `check-reviewer-verdict`, `check-byte-equal-pin`). RoR targets the subtle band. Default-on would over-fire on `[CHORE]/[DOCS]` PRs; opt-in matches the empirical hit band.

## 6. Implementation surface

- **Per-PR**, not per-finding (per-finding would 5×–10× cost with marginal lift).
- Config: `regatta.yaml::gates.reviewer_of_reviewer.enabled` (additive, default false).
- Gate: extend `scripts/lib/reviewer-verdict/verdict.sh` (modularized in #1045); add `check_ror_when_enabled` (≤30 LOC).
- Tests: extend `scripts/check-reviewer-verdict_test.sh` (42 cells today, +4 per #1087 c4).
- Worker-prompt parity: anchored-rule bullet in `docs/engineer/dispatch-templates/implementer.md`; cite in `defaultPromptBuilder`.

## Verdict — opt-in

Ship config flag + gate extension. Default false. Operator flips on per-repo when load-bearing batch density justifies the 2× review cost. Re-evaluate default after 30-day green window with flag on for `cmd/`, `internal/orchestrator/`, `scripts/check-*.sh`.
