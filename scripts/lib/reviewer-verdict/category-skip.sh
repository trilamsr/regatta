# reviewer-verdict/category-skip.sh — extract release-notes category prefix
# and auto-skip [CHORE]/[DOCS]/[CI]/[NONE]/[CHANGE] unless the path classifier
# already flagged the PR as load-bearing.
# Also short-circuits when --load-bearing not set after path classification.
#
# REQUIRES: BODY_FILE (rv_resolve_body), LOAD_BEARING_BY_PATH (rv_classify_paths),
#           LOAD_BEARING (rv_parse_args, possibly upgraded by rv_classify_paths)
# SETS:     CATEGORY
# ORDER:    must run after rv_resolve_body AND rv_classify_paths. Guard
#           fails fast if not — LOAD_BEARING_BY_PATH is set ONLY by
#           rv_classify_paths, so a missing guard would silently mis-skip.

rv_category_skip() {
  : "${BODY_FILE?rv_category_skip requires BODY_FILE — call rv_resolve_body first}"
  : "${LOAD_BEARING_BY_PATH?rv_category_skip requires LOAD_BEARING_BY_PATH — call rv_classify_paths first}"
  : "${LOAD_BEARING?rv_category_skip requires LOAD_BEARING — call rv_parse_args first}"
  CATEGORY=$(awk '
    /^```release-notes/ { in_block = 1; next }
    in_block && /^```/ { exit }
    in_block { print; exit }
  ' "$BODY_FILE" | grep -oEi '^\[[A-Z]+\]' | head -1 | tr '[:lower:]' '[:upper:]')

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
