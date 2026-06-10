# reviewer-verdict/token-extract.sh — extract bare Reviewer-recommendation
# and Reviewer-agent-id tokens from PR body. Strips ```-fenced blocks first
# so stale draft tokens cannot shadow the bare footer. Picks the LAST
# bare token so a stale REVISE preceding a fresh APPROVE does not win.

rv_extract_tokens() {
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
}
