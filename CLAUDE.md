# Regatta — Agent Operating Rules

Critical rules for any agent (main session, subagent, CI) operating in this repo. Subagents do NOT inherit `~/.claude/projects/.../memory/` — rules below MUST be self-contained or cite memory slug for main thread to inject via dispatch prompt.

## Token economy (read first — biggest win)

- **Dispatch brief only**: implementer subagents get per-task brief (spec §12 style), NOT full spec doc.
- **gh minimal fields**: `gh pr list/view` MUST pass explicit `--json` allowlist (default: `number,state,mergeStateStatus,statusCheckRollup,isDraft,headRefName`) + `-L 20`. Never bare `--json`.
- **No memory re-read**: never `cat`/`Read` files under your `~/.claude/projects/<project-hash>/memory/` dir. Auto-loaded via MEMORY.md. Reference by slug.
- **PR body cache per phase**: ONE `gh pr view N --json number,title,body,comments,reviews` per review phase; pass as text to phase subagents.
- **Subagent ci-check compress**: `make ci-check 2>&1 | tee /tmp/cicheck.log | grep -E "^(FAIL|ok|---|Error|error:|PASS)" | tail -40` + exit code. Main thread still re-runs full per `feedback_subagent_verification` (~10% lie rate).
- **ctx capture dedupe**: `ctx_search` before `ctx_batch_execute` on research/spec. Skip batch if recent (<24h) hit covers same content.

## Identity / output

- No AI signatures, no Co-Authored-By, no `🤖 Generated with` footers anywhere.
- WHY not WHAT in comments. Drop superfluous. Sweep on push.
- Deletion default: every PR answers "what got smaller?"

## CI gates (local pre-push)

- `make pre-push-check` before every push (runs `make check` + PR-body release-notes block sanity).
- `make check` = `doc-check doc-check-test prose-dup vet lint tidy-check mod-verify verify-vendored-assets go-check property-test slo-compile-test`. Single source of truth for local gate.
- `make ci-check` = `check stale-todo`.
- `gh pr create` / `gh pr edit` MUST use `--body-file` (HEREDOC escapes backticks and breaks release-notes fence).
- Test/Fuzz/Benchmark godocs: 1 line max (`scripts/doc-check.sh` test-godoc gate enforces).
- Banned-phrase gate: `scripts/doc-check.sh` rejects vague boosters in docs. Wrap literal tokens in backticks if mentioning them.

## Review discipline

- Failing test FIRST (TDD per `feedback_tdd_discipline`).
- Reviewer subagent measures vs A+ rubric per `feedback_grade_rubric`. PR body MUST post scorecard verbatim.
- Risk-tier findings: fix or file tracking issue. Never auto-approve.
- Spec deviation → re-spawn design subagent. Implementer never picks pattern.

## Dispatch

- Cap parallel implementer subagents at 3-4 (shared API quota dies at 5+; heavy-context sessions cap at 2-3).
- Always in worktrees (`.claude/worktrees/agent-*`). Per-merge cleanup.
- Dispatch prompt MUST pin: migration N, output path slug, exact brief text.

## Full rules

Project memory index: `~/.claude/projects/<project-hash>/memory/MEMORY.md` (per-operator, Claude Code derives project-hash from cwd). Auto-loaded in main session. NOT auto-loaded in subagents — main thread injects via dispatch prompt.

Boot prompt for autonomous sessions: `docs/engineer/autonomous-session-prompt.md`.
