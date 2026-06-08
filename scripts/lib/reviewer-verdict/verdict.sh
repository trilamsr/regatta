# reviewer-verdict/verdict.sh — decide pass/fail given RECOMMENDATION
# and REVIEWER_AGENT_ID. Handles operator-escape comment, allowlist shape,
# and self-tag mismatch vs PR_AUTHOR.

rv_decide_verdict() {
  case "$RECOMMENDATION" in
    APPROVE)
      # Token-present check (closes #999/#1001/#1002 self-tag bypass per
      # `feedback_no_self_tagged_approve`): a load-bearing APPROVE without
      # a named reviewer is a self-tagged approval — independent review
      # never happened.
      if [ -z "$REVIEWER_AGENT_ID" ]; then
        echo "check-reviewer-verdict: load-bearing PR has Reviewer-recommendation: APPROVE but is missing the Reviewer-agent-id token in body." >&2
        echo "  An APPROVE without a named reviewer is a self-tagged approval — independent review never happened." >&2
        echo "  Fix: dispatch an independent reviewer subagent per CLAUDE.md feedback_no_self_tagged_approve." >&2
        echo "  Add to PR body footer (bare, NOT in a code block):" >&2
        echo "    Reviewer-agent-id: <id>" >&2
        echo "    Reviewer-recommendation: APPROVE" >&2
        exit 1
      fi
      # Operator escape via `<!-- reviewer-skip-justified: <reason ≥4 chars> -->`
      # bypasses BOTH the allowlist check and the author-mismatch check
      # (rare, trivial doc/typo/dep-bump only). The token-present check
      # above always applies.
      JUSTIFICATION=$(grep -oE '<!--[[:space:]]*reviewer-skip-justified:[[:space:]]*[^>]*-->' "$BODY_FILE" \
        | head -1 \
        | sed -E 's/^<!--[[:space:]]*reviewer-skip-justified:[[:space:]]*//; s/[[:space:]]*-->$//' \
        | sed -E 's/[[:space:]]+$//')
      JUSTIFICATION_LEN=${#JUSTIFICATION}
      if [ "$JUSTIFICATION_LEN" -ge 4 ]; then
        exit 0
      fi
      # Independent-reviewer allowlist per `feedback_no_self_tagged_approve`.
      # Real reviewer agent IDs match one of two canonical shapes:
      #   (a) harness shape `^a[0-9a-f]{16}$` (17-char hex, e.g. `a6614259e2388c0ee`)
      #   (b) named-subagent shape `^(cavecrew|designer|triage|implementer|reviewer)-[a-z0-9-]+$`
      # Any other value is treated as a self-tag escape (#1036 used
      # `main-thread-adversarial-self`; #1037/#1038 used `self-tagged-defer`)
      # and rejected. Allowlist beats denylist: workers can invent new
      # escape strings, but cannot fake the canonical prefixes without
      # actually dispatching a real subagent.
      if ! echo "$REVIEWER_AGENT_ID" | grep -qE '^(a[0-9a-f]{16}|(cavecrew|designer|triage|implementer|reviewer)-[a-z0-9-]+)$'; then
        echo "check-reviewer-verdict: Reviewer-agent-id '$REVIEWER_AGENT_ID' does not match independent-reviewer allowlist on a load-bearing PR." >&2
        echo "  Allowed shapes: harness ID '^a[0-9a-f]{16}\$' (e.g. 'a6614259e2388c0ee') OR named subagent '^(cavecrew|designer|triage|implementer|reviewer)-<slug>\$'." >&2
        echo "  Self-tag escapes (e.g. 'main-thread-adversarial-self', 'self-tagged-defer') rejected per CLAUDE.md 'No self-tagged Reviewer-recommendation: APPROVE'." >&2
        echo "  Fix: dispatch independent reviewer subagent in fresh slot; paste its agent ID into the PR body footer." >&2
        echo "  Operator escape (rare, trivial doc/typo/dep-bump only):" >&2
        echo "    <!-- reviewer-skip-justified: <reason ≥4 chars> -->" >&2
        exit 1
      fi
      # Self-tag mismatch check: when caller passes --pr-author, the bare
      # `Reviewer-agent-id:` value MUST differ from the author login.
      if [ -n "$PR_AUTHOR" ] && [ "$REVIEWER_AGENT_ID" = "$PR_AUTHOR" ]; then
        echo "check-reviewer-verdict: self-tagged APPROVE rejected — Reviewer-agent-id ($REVIEWER_AGENT_ID) equals PR author ($PR_AUTHOR)." >&2
        echo "  Fix: dispatch an independent reviewer subagent in a fresh slot per CLAUDE.md 'TDD + review'." >&2
        echo "  Update PR body footer with the reviewer subagent id:" >&2
        echo "    Reviewer-agent-id: <independent-reviewer-id>  # MUST differ from $PR_AUTHOR" >&2
        echo "    Reviewer-recommendation: APPROVE" >&2
        echo "  Operator escape (rare, trivial doc/typo/dep-bump only):" >&2
        echo "    <!-- reviewer-skip-justified: <reason ≥4 chars> -->" >&2
        exit 1
      fi
      exit 0
      ;;
    REVISE|BLOCK)
      echo "check-reviewer-verdict: Reviewer-recommendation is $RECOMMENDATION on a load-bearing PR." >&2
      echo "  Fix: address the findings, then update the body to Reviewer-recommendation: APPROVE." >&2
      exit 2
      ;;
    '')
      echo "check-reviewer-verdict: load-bearing PR is missing Reviewer-recommendation token in body." >&2
      echo "  Fix: dispatch an independent reviewer subagent per CLAUDE.md 'TDD + review'." >&2
      echo "  Add to PR body footer (bare, NOT in a code block):" >&2
      echo "    Reviewer-agent-id: <id>" >&2
      echo "    Reviewer-recommendation: APPROVE|REVISE|BLOCK" >&2
      exit 1
      ;;
    *)
      echo "check-reviewer-verdict: unrecognized Reviewer-recommendation value: $RECOMMENDATION (expected APPROVE / REVISE / BLOCK)." >&2
      exit 1
      ;;
  esac
}
