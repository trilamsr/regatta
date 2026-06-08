---
title: "Migration-number lock — mechanical gate against parallel-dispatch collision"
status: draft
phase: self-host
summary: "Promote the `feedback_migration_number_lock` rule from CLAUDE.md prose into a mechanical gate. `scripts/check-migration-numbers.sh` fails closed when `internal/orchestrator/state/migrations/` carries duplicate version numbers, when the tail is non-contiguous, or when a PR diff adds more than one new migration. `make next-migration` outputs the next free number so dispatch prompts can call it instead of guessing."
date: 2026-06-08
---

# Migration-number lock — Spec

Memory rules in force: `feedback_migration_number_lock`, `feedback_default_simpler`, `feedback_trap_projection`, `feedback_parallel_safety`, `feedback_conflict_anticipation`, `feedback_root_cause`, `feedback_no_signatures`, `feedback_pr_body_hygiene`.

```release-notes
[DOCS] Spec for a mechanical gate that locks SQLite migration numbering
against parallel-dispatch collisions. `scripts/check-migration-numbers.sh`
fails closed on duplicates, tail gaps, or multi-add PR diffs. Bonus
`make next-migration` target outputs the next free number so dispatch
prompts can call it. No prod-code change in this spec.
```

## §0 Closing trigger

Done when ALL of:

1. This spec lands on `main`.
2. Follow-up implementer PR lands `scripts/check-migration-numbers.sh`, its `_test.sh` companion, `make next-migration`, and the `check-migration-numbers` wire-up under `Makefile.d/lint.mk` `check:` chain.
3. An injected-duplicate fixture passes `scripts/check-migration-numbers_test.sh` (red→green commit order per `feedback_tdd_discipline`).

## §1 Problem

Two parallel implementer subagents land separate PRs, each authoring a SQLite migration. With `internal/orchestrator/state/migrations/` currently at `0017_…`, each implementer's dispatch prompt independently arrives at `0018_…` as the obvious next number. Both PRs go green individually — `make check` does not look at the migrations dir — and the second-to-merge produces a duplicate-version panic at boot:

```
goose: version 18 already registered
```

The current mitigation (`CLAUDE.md` §Dispatch: "Dispatch prompt MUST pin migration N — never let implementer pick — duplicate-version panic") is prose-only. It depends on:

- The operator remembering the rule before every wave (recurring offender per `feedback_trap_projection`).
- The operator correctly computing N from `ls migrations/` at dispatch time (mis-counts when a sibling-stack PR is mid-flight and has already claimed N+1 in an unmerged branch).
- The implementer subagent reading + obeying the pin (drifts in long sessions).

Three failure paths, zero mechanical backstop. The trap fires on roughly every multi-PR wave that touches the state package.

A boot panic is also a downstream symptom. Per `feedback_root_cause`, the primary failure mode is "two parallel authors picked the same number without coordination." The fix lives at PR-time, not boot-time.

## §2 Scope

### In scope

1. `scripts/check-migration-numbers.sh` — mechanical gate fails closed on:
   - **Duplicate version numbers**: two files in `internal/orchestrator/state/migrations/` whose `NNNN_` prefix decodes to the same integer.
   - **Tail non-contiguity**: the highest version `Nmax` MUST equal `count(files)` + `gap_offset`, where `gap_offset` is the count of intentional historical gaps (today: 1, for the missing `0008_*`). The script encodes known gaps as a literal allowlist; new gaps fail closed.
   - **Multi-add PR diffs**: `git diff --name-only origin/main...HEAD -- internal/orchestrator/state/migrations/` MUST surface at most one new `NNNN_*.sql` file. Two new migrations in one PR is permitted ONLY when both share the same version range AND the PR body carries `<!-- migration-multi-add-justified: <reason> -->` (escape hatch mirrors `comment-density-justified`).
2. `scripts/check-migration-numbers_test.sh` — fixture-driven companion. Asserts:
   - Clean fixture dir (sequential `0001`–`0005`) → exit 0.
   - Duplicate-injected fixture (`0001`, `0002`, `0002_dup`) → exit 1 with `duplicate version` in stderr.
   - Tail-gap fixture (`0001`, `0002`, `0004`) → exit 1 with `non-contiguous tail` in stderr.
   - Multi-add fixture (PR diff lists `0006_a.sql` + `0007_b.sql` without justification comment) → exit 1.
   - Justified multi-add fixture → exit 0.
3. `make next-migration` target under `Makefile.d/lint.mk` (or a new `Makefile.d/migrations.mk`). Prints `printf "%04d" $((Nmax + 1))`. Dispatch prompts call it via `$(make next-migration)` instead of operator memory.
4. `Makefile.d/lint.mk` wire-up: append `check-migration-numbers check-migration-numbers-test` to the `check:` chain in top-level `Makefile`, mirroring the `check-tbd` / `check-no-bare-sleep` pattern.

### Out of scope

- **Rollback semantics**. `+goose Down` ordering, partial-apply recovery, and reverse migrations are unaffected by this gate. The script only inspects filenames, not contents.
- **Online migrations**. Long-running schema changes that need staged rollout (read-old / write-both / cutover / drop-old) are an orthogonal design problem; this gate concerns version-number assignment only.
- **Migration content lint**. SQL formatting, `+goose StatementBegin/End` balance, idempotency — all out of scope. The gate is filename-only.
- **Cross-branch coordination beyond `origin/main`**. The PR-diff check uses `origin/main` as the merge base. Sibling-stack PRs (per `feedback_rebase_onto_for_sibling_stacks`) MUST rebase onto their sibling base before this gate is meaningful; the gate does NOT solve "two unmerged branches both claim 0018." Operator-side: dispatch prompt MUST run `make next-migration` AFTER fetching origin/main, not from a stale local view.
- **Auto-renumber on collision**. The gate fails closed; it does NOT rewrite filenames. Renumbering touches index references, fixture goldens, and operator git history — too invasive for a mechanical gate. Resolution path is manual rebase + rename.
- **Scorecard rubric**. Per `feedback_grade_rubric` (operator self-grade, no CI gate as of 2026-06-07), this spec omits the B/A/A+ table.

## §3 Design

### §3.1 `scripts/check-migration-numbers.sh`

Pseudo-Bash sketch (final script lands in the follow-up implementer PR):

```bash
#!/usr/bin/env bash
set -uo pipefail
SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/.." && pwd)

: "${MIGRATIONS_DIR:=$REPO_ROOT/internal/orchestrator/state/migrations}"
: "${PR_BASE:=origin/main}"

# Known historical gaps. Append when a gap is intentionally introduced
# (e.g. abandoned migration during design iteration). Format: bare integers.
KNOWN_GAPS=(8)

# 1. Collect declared versions from filenames.
mapfile -t files < <(find "$MIGRATIONS_DIR" -maxdepth 1 -type f -name '[0-9][0-9][0-9][0-9]_*.sql' | sort)

declare -A seen=()
versions=()
for f in "${files[@]}"; do
  base=$(basename "$f")
  v=$((10#${base:0:4}))     # decode NNNN prefix; force base-10
  if [[ -n "${seen[$v]:-}" ]]; then
    echo "ERROR: duplicate version $v: ${seen[$v]} and $base" >&2
    exit 1
  fi
  seen[$v]=$base
  versions+=("$v")
done

# 2. Tail-contiguity check.
nmax=${versions[-1]}
expected=$(( ${#versions[@]} + ${#KNOWN_GAPS[@]} ))
if (( nmax != expected )); then
  echo "ERROR: non-contiguous tail — Nmax=$nmax expected=$expected (count=${#versions[@]} + known_gaps=${#KNOWN_GAPS[@]})" >&2
  exit 1
fi

# 3. PR-diff multi-add check. Skip when not in a PR context (no PR_BASE ref).
if git rev-parse --verify "$PR_BASE" >/dev/null 2>&1; then
  mapfile -t added < <(git diff --name-only --diff-filter=A "$PR_BASE"...HEAD -- "$MIGRATIONS_DIR" 2>/dev/null | grep -E '\.sql$' || true)
  if (( ${#added[@]} > 1 )); then
    # Allow if PR body carries justification. PR body discovered via gh CLI when GH_PR is set.
    if [[ -n "${GH_PR:-}" ]] && gh pr view "$GH_PR" --json body --jq .body | grep -q 'migration-multi-add-justified:'; then
      :  # justified — pass
    else
      echo "ERROR: PR adds ${#added[@]} migrations: ${added[*]}" >&2
      echo "hint: split into separate PRs OR add <!-- migration-multi-add-justified: <reason> --> to PR body" >&2
      exit 1
    fi
  fi
fi

echo "ok: ${#versions[@]} migrations, Nmax=$nmax"
```

Design notes:

- **`KNOWN_GAPS` is a literal allowlist**, not a heuristic. The current `0008_*` gap (visible in `ls internal/orchestrator/state/migrations/`) is hardcoded; the script ships with `KNOWN_GAPS=(8)`. New intentional gaps require an explicit allowlist edit in the same PR that introduces them — that PR is then load-bearing and gets adversarial review per `feedback_adversarial_review`.
- **`MIGRATIONS_DIR` and `PR_BASE` env overrides** mirror the `DOCS_ROOT` pattern in `scripts/check-tbd.sh`. The fixture test invokes the script against `scripts/testdata/migrations/<scenario>/` without touching the real tree.
- **`GH_PR` opt-in for multi-add justification**. The script checks the PR body only when `GH_PR` is set (CI passes it; local `make check` skips the multi-add branch entirely because there is no PR context locally). This mirrors `check-comment-density.sh`'s PR-body-justified escape pattern.
- **Exit codes**: 0 clean, 1 on first failure (duplicate / non-contiguous / multi-add). One-shot exit keeps the script <50 LoC and matches `check-tbd.sh` ergonomics.

### §3.2 `scripts/check-migration-numbers_test.sh`

Fixture layout under `scripts/testdata/migrations/`:

```
scripts/testdata/migrations/
├── clean/                  # 0001_a.sql 0002_b.sql 0003_c.sql 0004_d.sql 0005_e.sql
├── duplicate/              # 0001_a.sql 0002_b.sql 0002_dup.sql
├── non-contiguous-tail/    # 0001_a.sql 0002_b.sql 0004_d.sql
├── known-gap/              # 0001_a.sql 0002_b.sql (+ KNOWN_GAPS overridden in env to (3))
├── multi-add-pr/           # simulates PR adding 0006_a.sql + 0007_b.sql
└── multi-add-justified/    # same as multi-add-pr + GH_PR mock body fixture
```

Test harness mirrors `scripts/check-tbd_test.sh`: each scenario sets `MIGRATIONS_DIR` to its fixture dir, asserts expected exit code, and grep-matches the expected stderr fragment. Test runs in <500ms (no network, no real git).

### §3.3 `make next-migration`

```make
next-migration:  ## Print the next free SQLite migration number (zero-padded 4 digits). Use in dispatch prompts: `$(make next-migration)`.
	@bash scripts/next-migration.sh
```

`scripts/next-migration.sh` is a 5-line companion that decodes the highest `NNNN_` prefix under `internal/orchestrator/state/migrations/`, increments by 1, and prints `%04d`. The dispatch templates (`docs/engineer/dispatch-templates/implementer.md`) get a bullet:

> When the brief involves a SQLite migration, the migration number is locked at `<NNNN>` — do NOT pick your own. The pin comes from `make next-migration` run by the dispatcher BEFORE prompt authorship.

### §3.4 Wire-up

Add to top-level `Makefile.d/lint.mk` `.PHONY` line and target stanza, mirroring `check-tbd`:

```make
check-migration-numbers:  ## Fail when migrations dir carries duplicates, non-contiguous tail, or PR-diff adds >1 new migration without justification.
	bash scripts/check-migration-numbers.sh

check-migration-numbers-test:  ## Fixture-driven test for check-migration-numbers.sh.
	bash scripts/check-migration-numbers_test.sh
```

Append `check-migration-numbers check-migration-numbers-test` to the top-level `Makefile` `check:` chain.

## §4 Acceptance

Follow-up implementer PR lands when ALL of:

1. **RED first**: `scripts/check-migration-numbers_test.sh` commits BEFORE the script itself. The failing-test commit appears first in `git log --reverse` on the PR branch per `feedback_tdd_discipline`. Captured failing output in PR body.
2. **Duplicate fixture fails closed**: `MIGRATIONS_DIR=scripts/testdata/migrations/duplicate bash scripts/check-migration-numbers.sh` exits 1 with `duplicate version 2:` in stderr.
3. **Clean fixture passes**: `MIGRATIONS_DIR=scripts/testdata/migrations/clean bash scripts/check-migration-numbers.sh` exits 0.
4. **Non-contiguous tail fails closed**: `non-contiguous-tail` fixture exits 1 with `non-contiguous tail` in stderr.
5. **Known-gap fixture passes**: with `KNOWN_GAPS=(3)` env override (or hardcoded for the fixture variant), `known-gap` exits 0.
6. **`make next-migration` returns `0018`** when run against the current `internal/orchestrator/state/migrations/` tree (Nmax=17). When the next migration lands, the output bumps to `0019` without manual intervention.
7. **`make check` includes the new gate**: `grep check-migration-numbers Makefile` matches; `make check` runs the gate and passes on a clean tree.
8. **PR diff multi-add catches the real trap**: a synthetic test scenario where the PR branch adds two migrations (`0018_a.sql` + `0019_b.sql`) without the justification comment surfaces the failure in `make check` BEFORE merge.

## §5 Operator runbook

When the gate fires:

- **`duplicate version N`**: two files claim the same `NNNN_` prefix. Rebase against `origin/main`, run `make next-migration` to learn the current free number, and rename your migration. The rename is the fix; do NOT delete the sibling.
- **`non-contiguous tail`**: a migration was deleted from the dir tail OR a new gap was introduced. If the gap is intentional, edit `KNOWN_GAPS=(…)` in `scripts/check-migration-numbers.sh` in the same PR. Otherwise restore the missing file from git history (`git log --diff-filter=D --name-only -- internal/orchestrator/state/migrations/`).
- **`PR adds N migrations`**: split the PR. Two migrations in one PR almost always means two separate schema changes that should land sequentially with independent reviewer passes. Override only with `<!-- migration-multi-add-justified: <reason> -->` in the PR body when the migrations are genuinely co-dependent (e.g. parent table + child table with FK; landing one without the other breaks boot).

## §6 Trap-projection note (`feedback_trap_projection`)

This spec closes the operator-side trap (forgetting to pin the migration number). The worker-side projection: when a dispatched implementer authoring a migration runs `make check` locally, the gate fires before push if the implementer guessed a colliding number. The gate is the backstop; the dispatch-template bullet (§3.3) is the upstream teach. Both boundaries fixed in the same follow-up PR per `feedback_trap_projection`.
