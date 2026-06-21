# reviewer-verdict/body-source.sh — resolve PR body to BODY_FILE.
# Mutex --pr / --body-file; --pr fetches via gh into a tempfile.
# Caller is responsible for the EXIT trap when this allocates.
#
# REQUIRES: PR_NUM BODY_FILE  (set by rv_parse_args)
# SETS:     BODY_FILE         (resolved + validated)
# ORDER:    must run after rv_parse_args. Guard fails fast if not.

rv_resolve_body() {
  : "${PR_NUM?rv_resolve_body requires PR_NUM — call rv_parse_args first}"
  : "${BODY_FILE?rv_resolve_body requires BODY_FILE — call rv_parse_args first}"
  if [ -n "$PR_NUM" ] && [ -n "$BODY_FILE" ]; then
    echo "check-reviewer-verdict: --pr and --body-file are mutually exclusive" >&2
    exit 3
  fi

  if [ -n "$PR_NUM" ]; then
    BODY_FILE=$(mktemp)
    trap 'rm -f "$BODY_FILE"' EXIT
    if ! gh pr view "$PR_NUM" --json body --jq '.body' > "$BODY_FILE"; then
      echo "check-reviewer-verdict: gh pr view $PR_NUM failed" >&2
      exit 3
    fi
  fi

  if [ -z "$BODY_FILE" ] || [ ! -f "$BODY_FILE" ]; then
    echo "check-reviewer-verdict: no body source (use --pr or --body-file)" >&2
    exit 3
  fi
}
