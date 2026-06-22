# reviewer-verdict/path-classifier.sh — flag PR as load-bearing when any
# changed path hits agent-rule / CI-gate / operator-UX / event-vocabulary /
# skill surfaces. Sets LOAD_BEARING_BY_PATH (0/1) and upgrades LOAD_BEARING
# when matched.
#
# Closes #985 #986 #991 (retro audit 2026-06-08).
# Closes #1133 (audit 2026-06-09): internal/web/ + internal/obs/ added.
# Closes #1189 + #1190 (audit 2026-06-10): .claude/skills/* added because
# skill files encode operator-authority surfaces.
# Closes #1264 (N1, audit 2026-06-10): docs/engineer/{specs,briefs,
# dispatch-templates}/*.md + CLAUDE.md REMOVED from auto-flag. Solo
# doc PRs auto-skip per feedback_review_proportional; operator may
# spawn reviewer voluntarily.
# MAY-71 (audit 2026-06-20): internal/cost/* (money — daily cap +
# pre-call cost gate gate spawns), internal/canon/* (replay-determinism
# + approval-token HMAC security) and contracts/* (agent-authority
# prompts; sibling of contracts/schemas already in the workflow list)
# added — same #1133 gap class: silent [CHORE]/[DOCS] changes there
# break operator-decision / safety / money / event-vocab surfaces.
# MAY-266 (audit 2026-06-21): internal/{ghclient,gates,orchestrator,
# supervisor,ghidempotency,secrets,sandbox}/* + cmd/* added — the
# classifier omitted 8 of the 11 internal/* prefixes + cmd/ that
# CLAUDE.md §Reviewer-verdict gate lists as load-bearing, allowing
# [CHANGE]/[CHORE]/[DOCS] PRs touching those surfaces to auto-skip
# the reviewer-verdict gate. Drift caught by run_case_claude_md_prefix_parity.
#
# REQUIRES: PATHS_FILE LOAD_BEARING  (set by rv_parse_args)
# SETS:     LOAD_BEARING_BY_PATH, LOAD_BEARING (upgraded on match)
# ORDER:    must run after rv_parse_args. Guard fails fast if not.

rv_classify_paths() {
  : "${PATHS_FILE?rv_classify_paths requires PATHS_FILE — call rv_parse_args first}"
  : "${LOAD_BEARING?rv_classify_paths requires LOAD_BEARING — call rv_parse_args first}"
  LOAD_BEARING_BY_PATH=0
  if [ -z "$PATHS_FILE" ]; then
    return 0
  fi
  if [ ! -f "$PATHS_FILE" ]; then
    echo "check-reviewer-verdict: --changed-paths-file $PATHS_FILE not found" >&2
    exit 3
  fi
  while IFS= read -r changed_path; do
    [ -z "$changed_path" ] && continue
    case "$changed_path" in
      Makefile|Makefile.d/*|.github/workflows/*)
        LOAD_BEARING_BY_PATH=1
        break
        ;;
      scripts/check-*.sh)
        LOAD_BEARING_BY_PATH=1
        break
        ;;
      internal/web/*|internal/obs/*)
        LOAD_BEARING_BY_PATH=1
        break
        ;;
      internal/cost/*|internal/canon/*|contracts/*)
        LOAD_BEARING_BY_PATH=1
        break
        ;;
      internal/ghclient/*|internal/gates/*|internal/orchestrator/*|internal/supervisor/*)
        LOAD_BEARING_BY_PATH=1
        break
        ;;
      internal/ghidempotency/*|internal/secrets/*|internal/sandbox/*|cmd/*)
        LOAD_BEARING_BY_PATH=1
        break
        ;;
      .claude/skills/*)
        LOAD_BEARING_BY_PATH=1
        break
        ;;
    esac
  done < "$PATHS_FILE"
  if [ "$LOAD_BEARING_BY_PATH" -eq 1 ]; then
    LOAD_BEARING=1
  fi
}
