# reviewer-verdict/category-skip.sh — extract release-notes category prefix
# and auto-skip [CHORE]/[DOCS]/[CI]/[NONE]/[CHANGE] unless the path classifier
# already flagged the PR as load-bearing.
# Also short-circuits when --load-bearing not set after path classification.

rv_category_skip() {
  CATEGORY=$(awk '
    /^```release-notes/ { in_block = 1; next }
    in_block && /^```/ { exit }
    in_block { print; exit }
  ' "$BODY_FILE" | grep -oE '^\[[A-Z]+\]' | head -1)

  if [ "$LOAD_BEARING_BY_PATH" -ne 1 ]; then
    case "$CATEGORY" in
      '[CHORE]'|'[DOCS]'|'[CI]'|'[NONE]'|'[CHANGE]')
        exit 0
        ;;
    esac
  fi

  if [ "$LOAD_BEARING" -ne 1 ]; then
    exit 0
  fi
}
