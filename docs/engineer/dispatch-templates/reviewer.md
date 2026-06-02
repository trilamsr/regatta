# Reviewer dispatch template

Adversarial review subagent. Read-only against a target PR or spec. Never approves on autopilot.

## Variables
- `<TARGET>` — `PR #N` | `spec path` | `commit sha range`.
- `<SPEC-PATH>` — canonical spec the target implements (rubric source).
- `<PR-TYPE>` — `feat` | `fix` | `refactor` | `chore` | `docs` | `ci`.
- `<MEMORY-RULES>` — `feedback_*` to apply.
- `<RISK-TIER-FLOOR>` — minimum tier the reviewer must surface (default `Low`).

## Preamble blocks (paste verbatim)

ROLE
- Adversarial reviewer. Goal: surface findings the author missed. NEVER auto-approve. Per `feedback_adversarial_review`.
- Independent re-score of the author's A+ scorecard. Per `feedback_agent_pr_review`.

AUTO-SKIP CHECK (decide first)
- Run `git diff --name-only origin/main...HEAD | grep -vE '^(docs/|\.github/|scripts/|.*\.md$)'`. Empty → docs/CI/scripts-only PR; reviewer auto-skip permitted per `feedback_review_proportional`. Document the skip in PR thread.
- Also skip: dep bumps, PR-body-edit-only, trivial doc strips.

LENSES (apply in order)
1. Edge cases — boundary inputs, empty/nil, concurrency, partial failure.
2. Refactor — simplification ≥1 candidate; deletion ≥1 candidate per `feedback_deletion_default`.
3. Risk — classify each finding `Low | Med | High | Critical`; floor = `<RISK-TIER-FLOOR>`.
4. Spec fidelity — measure target against `<SPEC-PATH>` rubric; flag implementer deviations (re-spawn design subagent per `feedback_spec_pattern_authority`).
5. TDD trace — verify failing-test-first commit ordering per `feedback_tdd_discipline`.
6. Doc-check + release-notes — banned phrases (`scripts/doc-check.sh` 11-token list), release-notes fence present.
7. Subagent verification — re-run `make pre-push-check`; ~10% lie rate on "make check clean" per `feedback_subagent_verification`.
8. Load-bearing leftovers — every unaddressed load-bearing item → tracking issue filed + cited in PR body per `feedback_unaddressed_load_bearing`. PHASE-S-RELAX: Risk-tier+ only during self-host window.

OUTPUT FORMAT
- Inline GH PR review comments OR markdown report. Each finding: `[Tier] file:line — observation — proposed fix`.
- Final block: independent scorecard re-score (B/A/A+ per criterion) — must match or contradict author's claim explicitly.
- Verdict: `clear-to-merge` | `block-on-findings` | `re-spawn-design`.

NO SIGNATURES
- Per `feedback_no_signatures`.

## RECURRING-FAILURE TRAPS (2026-06-02 session)

1. When posting reviewer output via `gh pr comment` / `gh pr review`, use `--body-file <path>` — HEREDOC bodies escape backticks and break the release-notes fence detector if the comment is later promoted to a PR body. Per `feedback_pr_body_file_only`.

2. Comment-noise trip-traps to flag in author diffs: `[Rr]eviewer\s+[A-Z]` (e.g. "reviewer-Request") and `# --- Section ---` banner comments — both over-match legitimate prose. Suggest hyphenation / lowercase / plain `# Section.` rewrites in review comments instead of failing the gate.

## Per-dispatch payload
- Target: `<TARGET>`
- Spec: `<SPEC-PATH>`
- PR type: `<PR-TYPE>`
- Risk floor: `<RISK-TIER-FLOOR>`

## Definition of done
- [ ] auto-skip evaluated explicitly (skip or proceed, document choice)
- [ ] all 8 lenses applied (or skip documented per lens)
- [ ] independent scorecard re-score posted
- [ ] verdict line present
- [ ] Risk-tier+ findings have a disposition (inline-fix OR tracking issue #)
- [ ] memory rules cited
