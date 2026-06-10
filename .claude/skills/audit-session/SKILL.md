---
name: audit-session
description: End-of-session audit + handoff for any agent operating in the regatta repo. Use when the user says "audit session", "end session", "wrap up", "before we stop", "what did we miss", "before signing off", or any phrasing that asks Claude to validate the session's work before exit. Runs 9 phases (PR audit, reviewer-comment audit, issue audit, doc audit, code audit, worktree cleanup, learning + memory, cost + budget, NEXT-SESSION HANDOFF) and writes a single consolidated handoff file the next session reads to pick up exactly where this one left off. Default = silent pass per phase; ONE operator hand-back at end. Auto-file ONLY mechanically-derivable trackers (parity, self-tag, REVISE-slip). Phase 7 cross-refs the learn-from-mistakes skill — surfaces unsaved learnings if pushback/rollback events fired without that skill activating. Per `feedback_operator_minimal_input`.
---

# audit-session

End-of-session validator + handoff. Catches what slipped, codifies what was learned, writes the next session's pickup file.

## Activation

**Explicit.** "audit session" / "end session" / "wrap up" / "before we stop" / "before signing off" / "what did we miss" / `/audit-session`.

**Implicit.** Self-offer (don't start) when: "I'm done" / "back tomorrow" / ≥30min idle + ≥1 PR open / harness session-end signal.

## Inputs

- `$SESSION_START` — ISO. Default 8h ago.
- `$GIT_AUTHOR` — git `user.email`.
- `$HANDOFF_DIR` — `.claude/session-handoffs/`. Created if missing. Override via `SESSION_HANDOFF_DIR` env var. Next-session BOOT step 6 reads from the same path; both sides must use the same `$HANDOFF_DIR` resolution. Path is gitignored (operator-local handoffs).

## Default behavior

- 9 phases sequential.
- Silent per phase when clean; one line + action on finding.
- After phase 9: ONE consolidated hand-back (≤30 lines).
- Auto-file ONLY mechanically-derivable trackers. Never auto-close, auto-merge, auto-edit CLAUDE.md.

## Phase 1: PR audit

```bash
gh pr list --author "@me" --search "updated:>=$SESSION_START" \
  --json number,state,mergedAt,mergeStateStatus,statusCheckRollup,body -L 50 \
  > "$HANDOFF_DIR/prs.json"
```

Rules:
- `mergedAt==null AND state==OPEN` → log + offer operator merge. NOT auto-merge.
- `state==MERGED AND body~/Reviewer-recommendation: (REVISE|BLOCK)/` → file `[SESSION-AUDIT][post-merge] PR#<N>`.
- `mergeStateStatus IN (BLOCKED,DIRTY,UNSTABLE)` → file `[SESSION-AUDIT][automerge-stall] PR#<N>`.
- `Reviewer-agent-id:` matches PR author login → file `[SESSION-AUDIT][self-approve-leak]` per `feedback_no_self_tagged_approve`.
- **Self-approve-after-amend (binding gate, per `feedback_no_self_approve_after_edits`).** The canonical verdict set is `clear-to-merge` | `block-on-findings` | `re-spawn-design`. Verdict strings appear in TWO surfaces — (a) inline review/comment bodies on GitHub, AND (b) this session's transcript when reviewers were spawned as inline Agent tool calls whose output never reached GitHub. **Both surfaces must be scanned** — defaulting to (a) alone silently passes reviewer-runs that never posted.

  Per-PR algorithm:

  ```bash
  events=()  # each: <iso-timestamp>\t<verdict>\t<agent-id-or-source>

  # Source (a) — GitHub review/comment bodies, timestamped by submittedAt/createdAt.
  jq -r '
    (.reviews // [])[] | select(.body | test("block-on-findings|clear-to-merge")) |
      "\(.submittedAt)\t\((.body | capture("(?<v>block-on-findings|clear-to-merge)").v))\tgh-review-\(.author.login)",
    (.comments // [])[] | select(.body | test("block-on-findings|clear-to-merge")) |
      "\(.createdAt)\t\((.body | capture("(?<v>block-on-findings|clear-to-merge)").v))\tgh-comment-\(.author.login)"
  ' "$HANDOFF_DIR/pr-$N.json" >> "$HANDOFF_DIR/pr-$N-events.tsv"

  # Source (b) — session transcript. Resolution: line-order ≈ wall-clock order, so
  # synthesize a monotonic timestamp from `mergedAt - (line_count - line_number) seconds`
  # which preserves ordering across the merged event list (a-events stay at their real
  # ISO time; b-events sort into the same chronological slot relative to mergedAt).
  if [ -n "${CLAUDE_TRANSCRIPT:-}" ] && [ -r "$CLAUDE_TRANSCRIPT" ]; then
    total=$(wc -l < "$CLAUDE_TRANSCRIPT")
    merged_epoch=$(date -j -f '%Y-%m-%dT%H:%M:%SZ' "$(jq -r .mergedAt "$HANDOFF_DIR/pr-$N.json")" '+%s')
    grep -nE "PR\s*#?$N\b" "$CLAUDE_TRANSCRIPT" |
      grep -E 'block-on-findings|clear-to-merge' |
      awk -F: -v t="$total" -v m="$merged_epoch" '{
        ts = m - (t - $1);
        printf "%s\t%s\ttranscript-line-%d\n", strftime("%Y-%m-%dT%H:%M:%SZ", ts), ($0 ~ /block-on-findings/ ? "block-on-findings" : "clear-to-merge"), $1
      }' >> "$HANDOFF_DIR/pr-$N-events.tsv"
  else
    # Fallback: $CLAUDE_TRANSCRIPT unset or unreadable → main thread surfaces
    # the gap interactively in the consolidated hand-back instead of auto-filing
    # an issue. Phase 7 cross-ref still records "transcript unavailable" so the
    # operator knows the GH-only path is the only signal.
    echo "transcript_unavailable" >> "$HANDOFF_DIR/pr-$N-events.tsv"
  fi

  # Walk forward. For each block-on-findings event, require a later clear-to-merge
  # from a distinct agent-id (cavecrew-reviewer-* OR a[0-9a-f]{16}) before mergedAt.
  sort "$HANDOFF_DIR/pr-$N-events.tsv" |
    awk -F'\t' 'BEGIN{block=""} $2=="block-on-findings"{block=$3; next}
                $2=="clear-to-merge" && block!="" && $3!=block {block=""}
                END{if(block!="") exit 1}'
  ```

  Exit 1 → file `[SESSION-AUDIT][self-approve-after-amend] PR#<N>`. "transcript_unavailable" sentinel → main thread surfaces gap in hand-back, never auto-files (per Hard Nos `NO auto-file on uncertain detection`). (Does NOT detect `re-spawn-design` slips — those rebuild the change wholesale and bypass the simple later-clear-to-merge heuristic; flag as future-lever in Phase A2.)

## Phase 2: Reviewer-comment audit

ONE cached `gh pr view N --json comments,reviews` per PR (per `feedback_pr_body_cache_per_phase`).

```bash
for n in $(jq -r '.[].number' "$HANDOFF_DIR/prs.json"); do
  gh pr view "$n" --json comments,reviews > "$HANDOFF_DIR/pr-$n-comments.json"
done
```

- Grep `HIGH|CRITICAL|MED|🔴|🟡` in review bodies. Cross-ref open `[REVIEWER #<N>]` issues. Missing → file per `feedback_reviewer_findings_to_issues`.
- Merged PR with `Reviewer-recommendation: REVISE` token unaddressed → `[BYPASS-AUDIT]` issue.

## Phase 3: Issue audit

```bash
gh issue list --author "@me" --search "created:>=$SESSION_START" -L 30 \
  --json number,title,state,labels,body > "$HANDOFF_DIR/issues.json"
```

Decision:
- Body references PR# now MERGED + title lacks `(partial)` → close-candidate. List for operator.
- Duplicate titles → flag.
- Missing `autonomous` label on orchestrator-consumable items → suggest add.
- `depends on #N` for OPEN N → orphan log.

## Phase 4: Doc audit

```bash
git log --since "$SESSION_START" --author "$GIT_AUTHOR" -- CLAUDE.md \
  | grep -oE 'feedback_[a-z_]+' | sort -u > "$HANDOFF_DIR/new-rules.txt"

while read rule; do
  grep -l "$rule" docs/engineer/dispatch-templates/*.md > /dev/null \
    || echo "rule $rule missing from dispatch templates"
done < "$HANDOFF_DIR/new-rules.txt"

bash scripts/check-prompt-parity.sh
```

MEMORY.md sync: diff new `feedback_*` slugs vs MEMORY.md index → write missing index lines via Edit. Memory dir is operator-personal; per `feedback_no_memory_reread` do not `cat` slug files.

Pointers freshness: `git diff --stat origin/main -- docs/engineer/`; ≥3 doc moves → flag `docs/engineer/pointers.md`.

## Phase 5: Code audit (signal mining)

Scrape session transcript for: `TODO|FIXME|smell|dead|simplif|refactor|over-broad|bloat|unused`. Cross-ref this-session-filed issues + open `followup` issues.

Unfiled → operator hand-back list. **Do NOT auto-file** (noise).

Deletion debt per `feedback_deletion_default`:
```bash
# Refresh origin/main first — local ref can lag the remote if no fetch
# fired this session, undercounting pure-add commits merged after the
# last fetch. Read-only; safe in primary checkout.
git fetch origin main --quiet

# --merges filter misses squash-and-merge (the common GH path), which
# lands as a single non-merge commit on origin/main. Use --first-parent
# against origin/main to enumerate squash-merged commits and --no-merges
# to drop true merge commits if any exist; --shortstat surfaces ins/del.
git log --since "$SESSION_START" --author "$GIT_AUTHOR" \
  --first-parent --no-merges --shortstat origin/main \
  | awk '/insertions/ && !/deletion/ {print}'
```
≥3 pure-add commits on origin/main → `[DELETION-DEBT]` audit issue.

## Phase 6: Worktree + branch cleanup

```bash
git worktree list --porcelain | awk '/^worktree/ {print $2}' | while read wt; do
  br=$(git -C "$wt" rev-parse --abbrev-ref HEAD 2>/dev/null)
  [ "$br" = "main" ] && continue
  merged=$(gh pr list --head "$br" --state merged --json mergedAt --jq '.[0].mergedAt')
  [ -n "$merged" ] && echo "worktree $wt: branch $br merged; safe remove"
done

git branch --merged main | grep -v '^\* main' | grep -v 'main$' | head -20

docker ps --filter "name=regatta" --format '{{.Names}} {{.Status}}'
```

Decision: hand back commands; NO auto-remove. Per `feedback_post_worktree_removal_hygiene` suggest `golangci-lint cache clean` after operator removes worktrees. Per `feedback_primary_checkout_always_on_main` flag if primary not on main.

## Phase 7: Learning + memory

**Scope:** universal patterns + operator-personal `feedback_*` rules. **NOT** worker-side prompt fixes — those belong to Phase A2 (file `[AUTONOMY-LEVER][prompt]` issue against `internal/orchestrator/spawner/claude.go::defaultPromptBuilder`). Phase 7 writes memory + CLAUDE.md candidates; Phase A2 writes implementer/reviewer/designer prompt-template updates.

- **Twice-burned scan.** Transcript grep `same|again|twice|retry|second time|broken again`. Cluster by root cause. ≥2 + no `feedback_*` entry → new candidate.
- **Repeated operator directive.** ≥2 user turns with same phrasing → queue codification PR per `feedback_meta_codify_repeat_directives`.
- **Trap projection.** Trap operator hit ≥2 → propose worker-side fix at gate / prompt / knowledge boundary per `feedback_trap_projection`.

**Cross-ref `learn-from-mistakes` skill (binding).** Scan transcript for friction triggers — user pushback (`no`, `don't`, `stop`, `revert`, `undo that`), in-session rollback, test failing twice for related reasons, rediscovered ruled-out answer. For each trigger, verify `learn-from-mistakes` skill activated (look for `/learn` invocation, AGENTS.md edit, `` write, `.claude/notes/<topic>.md` write in session). If trigger fired WITHOUT skill activation → surface the unsaved learning candidate in the consolidated hand-back so the operator can decide whether to capture or drop. Do NOT auto-invoke the skill (per Hard Nos); only surface the gap. Capture-in-the-moment remains preferred (fresher context); this cross-ref is the safety net.

Write: `feedback_*.md` stubs under `~/.claude/projects/<hash>/memory/`; MEMORY.md index lines via Edit; CLAUDE.md candidate list into handoff file (NOT auto-edit).

## Phase 8: Cost + budget audit

```bash
gh api rate_limit --jq '.resources.core | {remaining,reset}'
# context-mode ctx_stats if available
# subagent dispatch count from session log
```

Log-only unless rate-remaining <500 OR explicit budget cap hit. One-line summary into handoff: `N PRs merged, M issues, K subagents, GH remaining Y`.

## Phase 9: Next-session handoff

THE LOAD-BEARING PHASE. Writes `$HANDOFF_DIR/<ISO>-session-handoff.md`. Next session reads this BEFORE doing anything else.

Schema:

```yaml
---
session_id: <uuid>
session_start: <ISO>
session_end: <ISO>
operator: $GIT_AUTHOR
exit_reason: clean | interrupt | bottleneck | budget-cap
next_session_first_action: <one-line>
---

## Open PRs at exit
- #<N> <title> — <state> <mergeStateStatus> — <one-line why-open>

## Open issues filed this session
- #<N> <title> — <surface> [<labels>]

## Open bottleneck (if any)
<paragraph: bottleneck-resolution loop attempt count + last error + the next thing to try>

## Active worktrees + branches
- <worktree-path> (<branch>) — <status, last commit sha>

## Container / live-system state
- <docker stack state, regatta PID, mounted dirs, kill-switch command>

## Pending operator decisions
1. <decision the next session should NOT make autonomously>

## Memory deltas
- new `feedback_<slug>` files written
- CLAUDE.md candidates for next codification round
- dispatch-template parity drift items

## Roadmap delta vs `docs/engineer/autonomous-session-prompt.md`
- phases progressed: <list>
- new findings that move the roadmap: <list>
- if any operator decision changes priority order, note it here

## Next-session quick-start (paste verbatim)
```bash
cd <primary-checkout>
git fetch origin main && git pull --ff-only
cat .claude/session-handoffs/<this-file>
```

## Top 3 things that went wrong + how I'd avoid them
1. <lesson> — <prevention>
```

Update `docs/engineer/autonomous-session-prompt.md` BOOT section to add `cat .claude/session-handoffs/$(ls -t .claude/session-handoffs/ | head -1)` after step 1 (pull main). regatta-operator skill pre-flight gains the same line.

## Phase A1: autonomy-prompt audit (MANDATORY when session touched roadmap)

Re-read `docs/engineer/autonomous-session-prompt.md`. Compare against the session's actual evidence. Find:

- **Phases marked COMPLETE that the session saw drift back open.** File `[ROADMAP-DRIFT]` issue + propose status flip.
- **Phases marked IN-FLIGHT that didn't move this session.** Log only.
- **Phases marked DEFERRED whose reopen-trigger fired** (external customer ask, 30-day-green, etc). Propose status flip.
- **Operator priority reorder** signals in user turns ("do X first now", "skip Y") → write reorder candidate.

Update `autonomous-session-prompt.md` PRIORITY block via Edit when reorder is operator-confirmed. Otherwise list candidates in handoff.

## Phase A2: more-autonomy lever scan (MANDATORY when session hit any operator-bottleneck)

Scan the session for places where the operator was a bottleneck — i.e. an action the orchestrator could have done by itself if it had X capability. Categorise:

- **Boot-time precondition gap** — failure mode that should have refused-at-boot per `feedback_loud_at_boot`. File `[AUTONOMY-LEVER]` issue with proposed gate.
- **Reviewer prompt gap** — finding type a five-lens reviewer should have caught. File issue with proposed prompt addition.
- **Implementer prompt gap** — trap an implementer should have avoided. File issue with proposed `internal/orchestrator/spawner/claude.go::defaultPromptBuilder` addition per `feedback_trap_projection`.
- **Self-improve detector gap** — observed pattern that the autonomy loop's #832 R6-R11 detector should have caught but didn't. File against `#832`.
- **CI gate gap** — drift that a `scripts/check-*.sh` script should mechanically catch.
- **Operator-decision gap** — decision the operator made that could be encoded as a rule (per `feedback_decision_priority` defaults).

For each finding write an `[AUTONOMY-LEVER]` issue with:
- Surface (CORE / ORCH / OPS / AGENT)
- Smallest implementer brief (file:line + 1-line fix)
- Estimated operator-touch reduction (1 / 2 / 3 / ∞ per session)

These directly populate `docs/engineer/autonomous-session-prompt.md` PRIORITY list re-rank in Phase A1.

## Hand-off summary (consolidated operator output)

After all 9 phases + A1/A2 run, emit ONE consolidated hand-back block:

```
audit-session: <N PRs merged, M issues, K subagents this session>

OPEN (operator action):
- merge: PR#<N> CLEAN+APPROVE, waiting on you
- decide: <one-line decision>

FILED (auto):
- [POST-MERGE-AUDIT] x N
- [REVIEWER #PR] x N
- [PROMPT-PARITY-DRIFT] x N
- [AUTONOMY-LEVER] x N

CANDIDATES (next-session promote):
- CLAUDE.md: <slug>
- feedback_: <slug>
- autonomous-session-prompt reorder: <one-line>

HANDOFF: .claude/session-handoffs/<file>.md
NEXT-SESSION FIRST ACTION: <from frontmatter>
```

## Hard nos

- NO auto-merge any PR.
- NO auto-close any issue.
- NO auto-edit CLAUDE.md.
- NO `git gc --prune=now` or reflog-destructive cleanup.
- NO container stop / regatta kill.
- NO auto-invoke other skills.
- NO unbounded CI poll per `feedback_bounded_ci_poll` (CLAUDE.md rule landed via this PR; pattern landed via #1186).
- NO auto-file on uncertain detection (e.g. Phase 1 transcript-unavailable sentinel → surface gap in hand-back, never auto-file).

## A+ rubric (mandatory per `feedback_grade_rubric`)

| Tier | Criterion | Notes |
|---|---|---|
| B | Defect named + root cause | Session-end forgotten leftover; repeatedly hit |
| B | Failing test OR TDD-not-applicable | Skill is operator process; TDD-not-applicable |
| B | `make check` green | doc-check + skill markdown well-formed |
| A | Smallest fix | 9 + 2 phases each ≤30 LOC; could split if growing |
| A | Operator-helpful failure mode | Single-block hand-back; explicit no-auto-merge |
| A | Cross-cuts | Cites `feedback_no_memory_reread`, `feedback_pr_body_cache_per_phase`, `feedback_no_self_tagged_approve`, `feedback_operator_minimal_input`, `feedback_meta_codify_repeat_directives`, `feedback_trap_projection`, `feedback_double_fail_root_cause`, `feedback_deletion_default`, `feedback_bounded_ci_poll`, `feedback_loud_at_boot` |
| A | Adversarial pass | Pending — five-lens reviewer |
| A+ | Eliminates a class of failures | Every session-end forgotten leftover gets mechanical sweep |
| A+ | Generalises a primitive | `$HANDOFF_DIR` schema + next-session-read pattern reusable |
| A+ | Carries forward | regatta-operator + autonomous-session-prompt gain handoff-read line; future skills inherit the handoff schema |
