# reviewer-verdict/path-classifier.sh — flag PR as load-bearing when any
# changed path hits agent-rule / CI-gate / load-bearing-doc surfaces.
# Sets LOAD_BEARING_BY_PATH (0/1) and upgrades LOAD_BEARING when matched.
# Closes #985 #986 #991 (retro audit 2026-06-08).

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
      CLAUDE.md|Makefile|Makefile.d/*|.github/workflows/*|docs/engineer/dispatch-templates/*)
        LOAD_BEARING_BY_PATH=1
        break
        ;;
      scripts/check-*.sh)
        LOAD_BEARING_BY_PATH=1
        break
        ;;
      docs/engineer/specs/*.md|docs/engineer/briefs/*.md)
        LOAD_BEARING_BY_PATH=1
        break
        ;;
    esac
  done < "$PATHS_FILE"
  if [ "$LOAD_BEARING_BY_PATH" -eq 1 ]; then
    LOAD_BEARING=1
  fi
}
