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
# dispatch-templates}/*.md + CLAUDE.md REMOVED from auto-flag. Empirical:
# PR #1248 was [DOCS] but classified load-bearing → 80.7min cycle for
# 1-file doc change + 70 empty-commit snapshot refreshes on main. Solo
# operator may still spawn reviewer voluntarily; prod-path edits remain
# auto-flagged.

rv_classify_paths() {
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
