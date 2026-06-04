# Regatta — Agent Operating Rules

Single source of truth for any agent (main session, subagent, CI) operating in this repo. Subagents do NOT inherit `~/.claude/projects/.../memory/` — rules below MUST be self-sufficient. Memory dir stays per-operator (personal preferences only); load-bearing rules live here.

When a rule cites `feedback_<slug>`, the slug names a per-operator memory file kept only for citation traceability. The rule body is fully captured here — do NOT `Read` the slug file.

## Decision priority (apply to every choice)

UX > ease > performance > best-practices > speed > velocity. Long-term > short-term. NEVER ask user — spawn review subagent + decide via these rules. Tool-checkable facts: verify, never ask. (`feedback_decision_priority`, `feedback_verify_before_asking`)

## Token economy (subagent-injection rules)

- **Dispatch brief only**: implementer subagents receive per-task brief (spec §12 style), NOT full spec doc. Main thread keeps full spec for cross-cutting Qs. (`feedback_dispatch_brief_only`)
- **gh minimal fields**: every `gh pr list/view/issue list` MUST pass explicit `--json` allowlist (default: `number,state,mergeStateStatus,statusCheckRollup,isDraft,headRefName`) + `-L 20`. Never bare `--json`. (`feedback_gh_minimal_fields`)
- **No memory re-read**: never `cat`/`Read` files under `~/.claude/projects/<hash>/memory/`. Reference by slug only. Exception: editing the memory file itself. (`feedback_no_memory_reread`)
- **PR body cache per phase**: ONE `gh pr view N --json number,title,body,comments,reviews` per review phase; pass as text to phase subagents. Re-fetch only on phase boundary. (`feedback_pr_body_cache_per_phase`)
- **Subagent ci-check compress**: implementer reports `make ci-check 2>&1 | tee /tmp/cicheck.log | grep -E "^(FAIL|ok|---|Error|error:|PASS)" | tail -40` + exit code. Main thread re-runs full (~10% lie rate). (`feedback_subagent_cicheck_compress`, `feedback_subagent_verification`)
- **ctx capture dedupe**: `ctx_search` before `ctx_batch_execute` on research/spec; skip batch if recent (<24h) hit covers same content. (`feedback_ctx_capture_dedupe`)

## Identity / output

- **No AI signatures anywhere** — no `Co-Authored-By`, no `🤖 Generated with`, no "written by Claude" in commits, PR bodies/titles, code comments, generated docs. (`feedback_no_signatures`)
- **Root cause only** — fix the primary failure mode, not the symptom. Bug downstream → check upstream API contract. Race → check locking design, not lock primitive. (`feedback_root_cause`)
- **Deletion default** — every PR answers "what got smaller?" (LOC, feature, abstraction, primitive, dep). Pure-addition PRs require A+ defense in body. Adversarial reviewer hunts deletion. (`feedback_deletion_default`)
- **Drop ceremony** — skip 10 zero-reward steps unless feature-grade + load-bearing: mid-stream CHANGELOG bumps, "Root causes addressed" tables on mechanical PRs, decorative PR-body sections, per-commit linting noise, etc. (`feedback_drop_ceremony`)

## Comments discipline

- **WHY not WHAT** — default to no comment. A clear name+signature needs no preface. Drop single-line WHAT-narration. (`feedback_comments_discipline`)
- **Long-term-benefit gate** — keep a comment only if removing it would leave a future reader confused about WHY. Sweep on push.
- **Lint reconcile** — exported godocs MUST open with the symbol name AND capture WHY in 1 sentence: `// UpperBound is the inclusive ceiling enforced by the budget gate.` GOOD. `// UpperBound returns the upper bound.` BAD. (`feedback_comments_lint_reconcile`)
- **Test/Fuzz/Benchmark godocs: 1 line max** — `scripts/doc-check.sh` test-godoc gate rejects multi-line. Collapse to `// TestX asserts O on I (#N).` (`feedback_test_godoc_one_line`)
- **Comment budget enforcement** — dispatch prompts say "WHY not WHAT" but implementers drift; reviewer comment-sweep is the backstop. Recurring offender. Implementer hard rules + reviewer MED-severity sweep live in `docs/engineer/dispatch-templates/implementer.md` §Comments: zero by default + `reviewer.md` lens 9. (`feedback_comment_budget_enforcement`)

## CI gates (local pre-push)

- **`make pre-push-check`** before every push (= `make check` + PR-body release-notes-block sanity).
- **`make check`** = `doc-check doc-check-test prose-dup check-memory-citations check-memory-citations-test check-phase-x-leak check-phase-x-leak-test check-tbd check-tbd-test vet lint tidy-check mod-verify verify-vendored-assets go-check property-test slo-compile-test`. Single source of truth. `check-tbd` fails closed on bare `TBD` placeholders under `docs/engineer/{specs,plans,briefs}/` — wrap in backticks for meta-mentions, cite `#NNN` on the same line, wrap in `<!-- TODO(#NNN) -->`, or move to a `release-notes` fence.
- **`make ci-check`** = `check stale-todo`.
- **Banned-phrase gate** (`scripts/doc-check.sh`): rejects `blazing[- ]fast`, `production[- ]grade`, `world[- ]class`, `seamless`, `cutting[- ]edge`, `state[- ]of[- ]the[- ]art`, and 5 more (11 total). Wrap literal token mentions in backticks. Reword hits to falsifiable claims (version pin, benchmark, named reference). (`feedback_ci_gates`)
- **Banned-phrase doc-check + check-tdd opt-outs**: tag spec-only PRs with `[DOCS]`/`[CI]`/`[CHORE]` release-notes prefix to skip check-tdd. (`feedback_ci_gates`)
- **PR body hygiene**: `gh pr create`/`gh pr edit` MUST use `--body-file <path>` (HEREDOC escapes backticks + silently breaks release-notes fence). Pre-push grep for triple-fence ` ```release-notes ` block presence. (`feedback_pr_body_hygiene`)
- **Windows path tests**: when test assertions compare path strings, canonicalize BOTH sides the same way production code does — or platform-branch the test inputs. 8.3 short-names + `/etc`-literal paths break Windows CI. (`feedback_windows_path_tests`)

## TDD + review

- **Failing test FIRST**, capture failing output in PR body, then impl, then green. Order matters; commit log must show the failing test landed first. (`feedback_tdd_discipline`)
- **Independent reviewer subagent** on every load-bearing PR (security, concurrency, schema, public API). Adversarial framing: hunt edge cases, don't auto-approve. (`feedback_adversarial_review`)
- **A+ rubric scorecard** in every feat-PR body: B/A/A+ per-criterion PASS/FAIL/N-A + 1-line evidence + claimed tier. Spec authors include the rubric. Implementer scorecard re-rates against it. Independent reviewer re-scores. (`feedback_grade_rubric`)
- **Per-criterion citation gate** — every `[x]` mark in the scorecard MUST cite a `Test*`-name OR `file:line` OR `#issue` OR `N/A — <rationale>` ON THE SAME LINE. `scripts/check-scorecard.sh` enforces in pr-lint; vibes-grading fails CI. Auto-skip for release-notes ∈ [CHORE]/[DOCS]/[CI]/[NONE]/[CHANGE].
- **Reviewer measures vs A+ rubric** — solo implementers ship at B-tier by default; independent review pulls up to A. (`feedback_agent_pr_review`)
- **Skip reviewer when proportional**: dep bumps with CI green + <20 LoC + no API change, PR-body-edit-only, trivial doc strips. Auto-skip when `git diff --name-only origin/main...HEAD | grep -vE '^(docs/|\.github/|scripts/|.*\.md$)'` returns empty. (`feedback_review_proportional`)
- **Spec pattern authority**: implementer deviation → re-spawn design subagent. NEVER let implementer pick pattern. (`feedback_spec_pattern_authority`)
- **Risk-tier findings**: fix inline OR file tracking issue + cite #. Never auto-approve with unaddressed Risk+. (`feedback_review_before_automerge`)
- **Unaddressed load-bearing**: every load-bearing leftover (reviewer finding, spec deviation, future-wave dep sketched in prose) → tracking issue filed BEFORE merge. PR bodies are not durable. Universal rule, no PR-type exempt. (`feedback_unaddressed_load_bearing`)

## Worktree discipline

- **Agents always in worktrees** (`.claude/worktrees/agent-<id>/`). Main checkout is read-only (`git fetch`, `git log`, `git status`). Never push from primary.
- **Never `git clone` or `git worktree add` from a subagent** — the harness pre-creates the worktree at `.claude/worktrees/agent-<id>/`; subagent only `cd`s to it. Writing under `/tmp/regatta-<slug>/` leaves stray edits in main worktree with no pushable branch. (#188)
- **Per-merge cleanup**: `git worktree remove --force` after merge.
- **Force-twice clears locks**: `git worktree remove --force --force <path>` if lock persists.
- **Post-removal hygiene**: `golangci-lint cache clean` after worktree removal (cache holds stale per-file analysis refs). prose-dup may also hold stale refs; verify script `exclude-dirs` current. (`feedback_worktree_discipline`, `feedback_post_worktree_removal_hygiene`)

## Dispatch (parallel subagent waves)

- **Cap parallel implementers at 3-4** (shared API quota dies at 5+; heavy-context sessions cap at 2-3).
- **Dispatch prompt MUST pin**: migration N (never let implementer pick — duplicate-version panic per `feedback_migration_number_lock`), output path slug (never let plan subagent pick — produces dup files per `feedback_plan_subagent_dup_files`), exact brief text.
- **File-disjoint only** in parallel; sequence chained-output work.
- **Shared-primitive owner**: scan composition roots (`cmd/regatta/serve.go`, `internal/orchestrator/state/machine.go`, `Makefile`, `docs/engineer/specs/README.md`) before dispatch; name OWNER for each shared primitive. (`feedback_parallel_safety`, `feedback_conflict_anticipation`)
- **Pre-file shared followups** for cross-cutting items; pre-merge collision rebase.

## Cross-cutting design / research

- **Adopt proven OSS over reimplementation**. Cite version + commit-sha + license. Priority: UX > quality bar matching reference > ecosystem conventions > long-term repo benefit. (`feedback_research_design_principles`)

## Self-host filter (Phase context)

Every wedge filtered by: "does the sole internal operator need this to dispatch regatta-the-binary at this repo unattended?" Keep → in scope. Defer → Phase X with reopen-trigger (external customer ask OR 30-day-green). Single-tenant, single-operator, single-repo, CLI-only, deterministic CI, human-merge via GH branch protection. No RBAC, no billing, no htmx UI, no Sigstore, no blackboard. (See `docs/engineer/briefs/2026-06-01-self-host-first.md` §1.)

**Mechanical gate**: `scripts/check-phase-x-leak.sh` (in `make check`) walks `docs/engineer/specs/*.md`, reads YAML frontmatter `status:`, and fails closed when an active spec names a Phase-X token (`tenant_id`, `RBAC`, `Stripe`, `Sigstore`, `Rekor`, `blackboard`, `Temporal`) without `phase: x-forward-fit` opt-in. Skip statuses: `shipped`, `archived`, `superseded`, `skeleton-prefetch`. To declare intentional Phase-X awareness in an active spec, add `phase: x-forward-fit` (forward-fit seam) or `phase: x-prefetch` to the frontmatter.

## Branch protection state

`main` has `required_status_checks.strict: false` (turned off 2026-06-01 during autonomous batch-merge). Only DIRTY merge-state blocks merge — automerge fires as soon as PR's own CI passes. (`feedback_branch_protection_strict`)

## Pointers

- Autonomous-session boot prompt: `docs/engineer/autonomous-session-prompt.md`
- Dispatch templates: `docs/engineer/dispatch-templates/{implementer,reviewer,designer,triage}.md`
- Self-host brief: `docs/engineer/briefs/2026-06-01-self-host-first.md`
- Per-operator memory (citation-only; NEVER `Read` from agents): `~/.claude/projects/<project-hash>/memory/MEMORY.md`
