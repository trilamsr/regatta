# Regatta — Agent Operating Rules

Single source of truth for any agent (main session, subagent, CI) operating in this repo. Subagents do NOT inherit `~/.claude/projects/.../memory/` — rules below MUST be self-sufficient. Memory dir stays per-operator (personal preferences only); load-bearing rules live here.

When a rule cites `feedback_<slug>`, the slug names a per-operator memory file kept only for citation traceability. The rule body is fully captured here — do NOT `Read` the slug file.

## Decision priority (apply to every choice)

UX > ease > performance > best-practices > speed > velocity. Long-term > short-term. NEVER ask user — spawn review subagent + decide via these rules. Tool-checkable facts: verify, never ask. (`feedback_decision_priority`, `feedback_verify_before_asking`)

**Default simpler**: Pick the simplest viable option. Don't pre-build lint scripts / abstractions / tier systems for hypothetical drift. Anti-pattern: "what if 5 PRs/month land outside the list" — wait for the 5 PRs, not the lint. Three similar lines beat a premature abstraction. (`feedback_default_simpler`)

- **Recognize session end**: "address N items" is a hint, not a contract. When non-phase-x non-soak-gated open issues drop to ≤2, report "actionable surface exhausted" + offer phase-x triage / wedge triage / stop. Don't fabricate items, don't re-touch swept files, don't build lint scripts for hypothetical drift. (`feedback_recognize_session_end`)

## Token economy (subagent-injection rules)

- **Dispatch brief only**: implementer subagents receive per-task brief (spec §12 style), NOT full spec doc. Main thread keeps full spec for cross-cutting Qs. (`feedback_dispatch_brief_only`)
- **gh minimal fields**: every `gh pr list/view/issue list` MUST pass explicit `--json` allowlist (default: `number,state,mergeStateStatus,statusCheckRollup,isDraft,headRefName`) + `-L 20`. Never bare `--json`. (`feedback_gh_minimal_fields`)
- **No memory re-read**: never `cat`/`Read` files under `~/.claude/projects/<hash>/memory/`. Reference by slug only. Exception: editing the memory file itself. (`feedback_no_memory_reread`)
- **Worker-prompt parity gate**: every `feedback_*` slug listed under `docs/engineer/dispatch-templates/implementer.md` `## Anchored rules (worker-prompt parity)` MUST be cited in `internal/orchestrator/spawner/claude.go::defaultPromptBuilder`. `scripts/check-prompt-parity.sh` enforces; dispatch template is authoritative. Escape hatch: append ` <!-- prompt-parity-skip: <reason> -->` to a bullet kept reviewer-only. Closes session retro 2026-06-04 Impact 3 (#901).
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
- **`make check`** — authoritative target list lives at `Makefile.d/ci.mk::check`; run `make help` for descriptions. Single source of truth. `check-tbd` fails closed on bare `TBD` placeholders under `docs/engineer/{specs,plans,briefs}/` — wrap in backticks for meta-mentions, cite `#NNN` on the same line, wrap in `<!-- TODO(#NNN) -->`, or move to a `release-notes` fence. `check-comment-density` fails when a NEW prod `.go` file in the PR diff exceeds 5% comment density (`// ` line count / total LOC); pre-existing files warn-only at 10%. Operator escape: `<!-- comment-density-justified: <reason> -->` in PR body. `check-no-bare-sleep` fails closed on `time.Sleep` lexically nested inside a `for` block in any `*_test.go` — migrate to `testutil.Eventually` / `testutil.EventuallyT` / `testutil.AssertStable`, or annotate `// allow-sleep: <reason>` for legitimately non-polling waits.
- **`make ci-check`** = `check stale-todo`.
- **Banned-phrase gate** (`scripts/doc-check.sh`): rejects `blazing[- ]fast`, `production[- ]grade`, `world[- ]class`, `seamless`, `cutting[- ]edge`, `state[- ]of[- ]the[- ]art`, and 5 more (11 total). Wrap literal token mentions in backticks. Reword hits to falsifiable claims (version pin, benchmark, named reference). (`feedback_ci_gates`)
- **Banned-phrase doc-check + check-tdd opt-outs**: tag spec-only PRs with `[DOCS]`/`[CI]`/`[CHORE]` release-notes prefix to skip check-tdd. (`feedback_ci_gates`)
- **PR body hygiene**: `gh pr create`/`gh pr edit` MUST use `--body-file <path>` (HEREDOC escapes backticks + silently breaks release-notes fence). Pre-push grep for triple-fence ` ```release-notes ` block presence. (`feedback_pr_body_hygiene`)
- **Windows path tests**: when test assertions compare path strings, canonicalize BOTH sides the same way production code does — or platform-branch the test inputs. 8.3 short-names + `/etc`-literal paths break Windows CI. (`feedback_windows_path_tests`)
- **pr-lint body-snapshot lag**: `pr-lint` workflow snapshots PR body at the triggering commit's event payload. Reruns use the STORED payload, not live body. Body-edit alone doesn't refresh. After fixing release-notes errors in body, push an empty commit (`git commit --allow-empty -m "chore: refresh pr-lint snapshot" && git push`) to force a fresh trigger event. (`feedback_pr_lint_body_snapshot_lag`)
- **Byte-equal-refactor pin**: any refactor whose correctness story is "target/gate/route set is byte-equal pre/post" MUST ship a mechanical drift gate (template: `scripts/check-prompt-parity.sh`). PR-body claim alone is rejected — drift surfaces in the next sibling PR, not in this one. Adversarial review hunts this pattern. Closes #985.

## TDD + review

- **Failing test FIRST**, capture failing output in PR body, then impl, then green. Order matters; commit log must show the failing test landed first. (`feedback_tdd_discipline`)
- **Independent reviewer subagent** on every load-bearing PR (security, concurrency, schema, public API). Adversarial framing: hunt edge cases, don't auto-approve. Every load-bearing artifact gets an adversarial pass — not just PRs. Design briefs, specs, prompt templates, operator-facing decisions. Builder spawns inline reviewer OR explicitly skips with cited `feedback_review_proportional` predicate. (`feedback_adversarial_review`, `feedback_adversarial_review_every_step`)
- **Reviewer-verdict gate** (`scripts/check-reviewer-verdict.sh`, wired into pr-lint workflow): load-bearing PRs (paths under `internal/{ghclient,gates,orchestrator,supervisor,ghidempotency,secrets,sandbox}/`, `cmd/`, `contracts/schemas/`, PLUS agent-rule + CI-gate surfaces `CLAUDE.md`, `Makefile`, `Makefile.d/*`, `.github/workflows/*`, `docs/engineer/dispatch-templates/*`, `scripts/check-*.sh`) MUST carry `Reviewer-recommendation: APPROVE` in the PR body footer (bare, not in a code block). Missing → fail. `REVISE`/`BLOCK` → fail. `[CHORE]`/`[DOCS]`/`[CI]`/`[NONE]`/`[CHANGE]` release-notes auto-skips — EXCEPT when path classifier flags an agent-rule / CI-gate surface (those surfaces are themselves the category being reviewed). Closes session retro 2026-06-04 Impact 1 (#899) + retro 2026-06-08 (#986).
- **PR not done until merged**: `mergedAt != null` AND `state = MERGED`. Automerge enabling + 'CLEAN' status + 'approved' DO NOT count. Post-approval CI flake (verify, test-macos, govulncheck) regularly fails after automerge fires, leaving PR in OPEN/BLOCKED limbo. Watch via `gh pr view N --json state,mergedAt,mergeStateStatus,statusCheckRollup` until terminal. (`feedback_watch_pr_until_merged`)
- **Reviewer findings → tracking issues**: every CRITICAL/HIGH/MED adversarial-review finding NOT addressed inline → file tracking issue BEFORE marking PR merge-ready. PR bodies are not durable; reviewer comments evaporate. Issue title: `[REVIEWER #PR] <severity> <category>: <claim-summary>`. (`feedback_reviewer_findings_to_issues`)
- **A+ rubric (operator self-grade, no CI gate)** — feat-PR bodies MAY include a B/A/A+ rubric scorecard for the operator's own benefit: spec authors draft tier criteria, implementer self-rates, independent reviewer re-scores. Format is unenforced — no token shape, no CI check. Self-host phase: solo operator + solo reviewer = no vibes-grader to catch. Reopen-trigger: external contributor lands OR audit need surfaces. (`feedback_grade_rubric`)
- **Reviewer measures vs A+ rubric** — solo implementers ship at B-tier by default; independent review pulls up to A. (`feedback_agent_pr_review`)
- **Skip reviewer when proportional**: dep bumps with CI green + <20 LoC + no API change, PR-body-edit-only, trivial doc strips. Auto-skip when `git diff --name-only origin/main...HEAD | grep -vE '^(docs/|\.github/|scripts/|.*\.md$)'` returns empty. (`feedback_review_proportional`)
- **Spec pattern authority**: implementer deviation → re-spawn design subagent. NEVER let implementer pick pattern. (`feedback_spec_pattern_authority`)
- **Risk-tier findings**: fix inline OR file tracking issue + cite #. Never auto-approve with unaddressed Risk+. (`feedback_review_before_automerge`)
- **Unaddressed load-bearing**: every load-bearing leftover (reviewer finding, spec deviation, future-wave dep sketched in prose) → tracking issue filed BEFORE merge. PR bodies are not durable. Universal rule, no PR-type exempt. (`feedback_unaddressed_load_bearing`)
- **Audit before dispatch**: before dispatching an implementer for a plan-master task, verify the work isn't already on main: `git ls-tree -r origin/main --name-only | grep <expected-path>` OR `git log --oneline origin/main | grep '(#<task-issue>)'`. Plan-master issues may document already-shipped work; dispatching wastes subagent invocations. (`feedback_audit_main_before_implementing`)
- **Test-coverage audit per wave**: end every parallel-dispatch wave with explicit test-coverage audit BEFORE next wave. Audit unit / integration / E2E + TDD-order-verification (`git log --reverse <branch>` shows RED commit first) + RED-output-in-PR-body + mock-vs-real ratio. Catches subagent over-claims + integration gaps unit tests don't see. Gap → tracker issue before next wave. (`feedback_test_coverage_audit_per_wave`)
- **Trap projection across loop closure**: when a recurring trap (scorecard rejection, comment-density fail, missing fence, banned-phrase hit) trips the operator ≥2 times in a session, project whether autonomous workers will hit the same trap post-loop-closure. If yes, fix BOTH operator-side AND worker-side BEFORE loop closure. Three boundaries — pick by root cause: (1) gate enforcement (`scripts/check-*.sh` too strict), (2) prompt authorship (`internal/orchestrator/spawner/claude.go::defaultPromptBuilder` doesn't teach the rule), (3) operator knowledge (CLAUDE.md / dispatch templates drift from spec). Anti-pattern: patching per-PR symptoms instead of root-causing at one boundary. (`feedback_trap_projection`)
- **Validate empirically + verify subagent output**: Before recommending CI/perf/memory changes OR dispatching action on subagent findings, run a local measurement (`/usr/bin/time -l go test -race ...` resolves debates in 1min) AND spot-check 2-3 file:line refs from investigator/reviewer reports. Subagent output is a LEAD, not GROUND TRUTH. (`feedback_validate_before_ship`, `feedback_subagent_output_verify`)
- **Verify-CANCELLED + orphan test = goroutine leak**: when GitHub Actions verify job is CANCELLED at full job timeout (e.g. 15min) and the log shows `ok` for all packages followed by silent minutes then `##[error]The operation was canceled.` and `Terminate orphan process: pid (N) (sometest.test)`, root cause is a goroutine inside one test blocking on a channel that is closed only in a `defer` that never fires because the test func never returns. Fix with `select { case <-ch: case <-time.After(deadline): }`. (`feedback_ci_timeout_orphan_test_goroutine`)

## Worktree discipline

- **Agents always in worktrees** (`.claude/worktrees/agent-<id>/`). Main checkout is read-only (`git fetch`, `git log`, `git status`). Never push from primary.
- **Never `git clone` or `git worktree add` from a subagent** — the harness pre-creates the worktree at `.claude/worktrees/agent-<id>/`; subagent only `cd`s to it. Writing under `/tmp/regatta-<slug>/` leaves stray edits in main worktree with no pushable branch. (#188)
- **Per-merge cleanup**: `git worktree remove --force` after merge.
- **Force-twice clears locks**: `git worktree remove --force --force <path>` if lock persists.
- **Post-removal hygiene**: `golangci-lint cache clean` after worktree removal (cache holds stale per-file analysis refs). prose-dup may also hold stale refs; verify script `exclude-dirs` current. (`feedback_worktree_discipline`, `feedback_post_worktree_removal_hygiene`)
- **Rebase conflict resolution**: During `git rebase origin/main`, `--theirs` = PR commit being replayed (counterintuitive vs merge); `--ours` = main. Wrong choice silently drops PR work (lost #772). Sibling-stack PRs (branched off another in-flight feature, no merge-base with main) MUST use `git rebase --onto origin/main <sibling-base> <pr-branch>` — plain `rebase origin/main` replays the sibling's commits as massive add/add conflicts. Verify merge-base first. Snippet: `git checkout --theirs <file>` → `git add <file>` → `git rebase --continue`. (`feedback_rebase_theirs_vs_ours`, `feedback_rebase_onto_for_sibling_stacks`)
- **Primary checkout always on `main`**: primary checkout (`/Users/treedesk/Desktop/Projects/regatta` analog) MUST always be on `main`. Feature branches in primary block subagent worktrees from grabbing the same branch — git refuses checkout. If primary drifts onto a feature branch, immediately `git checkout main && git pull --ff-only origin main`. (`feedback_primary_checkout_always_on_main`)

## Dispatch (parallel subagent waves)

- **Cap parallel implementers at 3-4** (shared API quota dies at 5+; heavy-context sessions cap at 2-3).
- **Dispatch prompt MUST pin**: migration N (never let implementer pick — duplicate-version panic), output path slug (never let plan subagent pick — produces dup files), exact brief text. (`feedback_migration_number_lock`, `feedback_plan_subagent_dup_files`)
- **File-disjoint only** in parallel; sequence chained-output work.
- **Shared-primitive owner**: scan composition roots (`cmd/regatta/serve.go`, `internal/orchestrator/state/machine.go`, `Makefile`) before dispatch; name OWNER for each shared primitive. `docs/engineer/specs/README.md` was a shared anchor until it was untracked + gitignored; auto-regenerated locally via `make specs-index`. (`feedback_parallel_safety`, `feedback_conflict_anticipation`)
- **Pre-file shared followups** for cross-cutting items; pre-merge collision rebase.
- **Cascade-rebase = design defect**: when ≥3 PRs go DIRTY simultaneously on shared-anchor changes, treat as design defect, not "normal merge math". Investigate the shared anchor (god-file, large composition root) — fix structurally (split files per #737 pattern) rather than absorbing rebase churn N times. (`feedback_cascade_rebase_root_cause`)
- **Free-headroom backfill**: when parallel-implementer cap has open slots AND critical-path subagents are running, do NOT idle. Backfill with safe, file-disjoint, easy issues from open followups (`gh issue list --state open --label followup OR --label trivial -L 20`). Candidates must be file-disjoint w/ active scopes, doc/scripts/single-file-bounded, not trigger-gated (skip Phase-X tracked-only), <30 min effort. Skip silently when all open followups are trigger-gated. (`feedback_free_headroom_backfill`)

## Cross-cutting design / research

- **Adopt proven OSS over reimplementation**. Cite version + commit-sha + license. Priority: UX > quality bar matching reference > ecosystem conventions > long-term repo benefit. (`feedback_research_design_principles`)

## Self-host filter (Phase context)

Every wedge filtered by: "does the sole internal operator need this to dispatch regatta-the-binary at this repo unattended?" Keep → in scope. Defer → Phase X with reopen-trigger (external customer ask OR 30-day-green). Single-tenant, single-operator, single-repo, CLI-only, deterministic CI, human-merge via GH branch protection. No RBAC, no billing, no htmx UI, no Sigstore, no blackboard. (See `docs/engineer/briefs/2026-06-01-self-host-first.md` §1.)

**Mechanical gate**: `scripts/check-phase-x-leak.sh` (in `make check`) walks `docs/engineer/specs/*.md`, reads YAML frontmatter `status:`, and fails closed when an active spec names a Phase-X token (`tenant_id`, `RBAC`, `Stripe`, `Sigstore`, `Rekor`, `blackboard`, `Temporal`) without `phase: x-forward-fit` opt-in. Skip statuses: `shipped`, `archived`, `superseded`, `skeleton-prefetch`. To declare intentional Phase-X awareness in an active spec, add `phase: x-forward-fit` (forward-fit seam) or `phase: x-prefetch` to the frontmatter.

## Branch protection state

`main` has `required_status_checks.strict: false` (turned off 2026-06-01 during autonomous batch-merge). Only DIRTY merge-state blocks merge — automerge fires as soon as PR's own CI passes. (`feedback_branch_protection_strict`)

## Pointers

See `docs/engineer/pointers.md` for the canonical cross-ref index.
