# reviewer-verdict/token-extract.sh — extract bare Reviewer-recommendation
# and Reviewer-agent-id tokens from PR body. Strips ```-fenced blocks first
# so stale draft tokens cannot shadow the bare footer. Picks the LAST
# bare token so a stale REVISE preceding a fresh APPROVE does not win.
#
# REQUIRES: BODY_FILE  (rv_resolve_body)
# SETS:     RECOMMENDATION, REVIEWER_AGENT_ID, CONFIDENCE_EVIDENCE_NEEDED
# ORDER:    must run after rv_resolve_body. Guard fails fast if not.

rv_extract_tokens() {
  : "${BODY_FILE?rv_extract_tokens requires BODY_FILE — call rv_resolve_body first}"
  RECOMMENDATION=$(awk '
    /^```/ { in_fence = !in_fence; next }
    !in_fence { print }
  ' "$BODY_FILE" \
    | grep -iE '^[[:space:]]*Reviewer-recommendation:' \
    | tail -1 \
    | sed -E 's/^[[:space:]]*Reviewer-recommendation:[[:space:]]*//I' \
    | tr -d '[:space:]' \
    | tr '[:lower:]' '[:upper:]')

  REVIEWER_AGENT_ID=$(awk '
    /^```/ { in_fence = !in_fence; next }
    !in_fence { print }
  ' "$BODY_FILE" \
    | grep -iE '^[[:space:]]*Reviewer-agent-id:' \
    | tail -1 \
    | sed -E 's/^[[:space:]]*Reviewer-agent-id:[[:space:]]*//I' \
    | tr -d '[:space:]')

  CONFIDENCE_EVIDENCE_NEEDED=$(awk '
    /^```/ { in_fence = !in_fence; next }
    !in_fence { print }
  ' "$BODY_FILE" \
    | grep -iE '^[[:space:]]*Confidence-evidence-needed:' \
    | tail -1 \
    | sed -E 's/^[[:space:]]*Confidence-evidence-needed:[[:space:]]*//I' \
    | tr -d '[:space:]')
}
