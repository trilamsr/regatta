# reviewer-verdict/quality-sections.sh — enforce three review-quality
# sections on load-bearing PRs per #1062:
#   - `## A+ delta`
#   - `## Negative-space audit`
#   - `## Reviewer confidence`
#
# Each section must be present AND have non-empty body content (≥1
# non-blank line between the heading and the next `## ` heading / EOF).
#
# Escape hatches (per issue spec, mirror operator-escape pattern):
#   - `<!-- a-plus-not-applicable: <reason ≥4 chars> -->` skips A+ check.
#   - `<!-- negative-space-not-applicable: <reason ≥4 chars> -->` skips
#     negative-space check.
#
# Stderr tokens (machine-parseable): missing_a_plus_delta,
# missing_negative_space_audit, missing_reviewer_confidence.
#
# Closes #1062 c1+c2: review quality enforced mechanically, not just
# review presence. Pairs with INSUFFICIENT_EVIDENCE verdict accepted in
# verdict.sh (c3).

# rv_section_body_nonempty <heading-text> <body-file>
# Returns 0 when the heading exists AND has ≥1 non-blank line of body
# before the next `## ` heading or EOF. Returns 1 otherwise.
rv_section_body_nonempty() {
  local heading="$1"
  local body_file="$2"
  awk -v hdr="## $heading" '
    BEGIN { in_section = 0; nonblank = 0 }
    $0 == hdr { in_section = 1; next }
    in_section && /^## / { exit }
    in_section {
      line = $0
      sub(/^[[:space:]]+/, "", line)
      sub(/[[:space:]]+$/, "", line)
      if (length(line) > 0) { nonblank = 1 }
    }
    END { exit nonblank ? 0 : 1 }
  ' "$body_file"
}

# rv_has_escape <marker-name> <body-file>
# Returns 0 when body contains `<!-- <marker>: <reason ≥4 chars> -->`.
rv_has_escape() {
  local marker="$1"
  local body_file="$2"
  local reason
  reason=$(grep -oE "<!--[[:space:]]*${marker}:[[:space:]]*[^>]*-->" "$body_file" \
    | head -1 \
    | sed -E "s/^<!--[[:space:]]*${marker}:[[:space:]]*//; s/[[:space:]]*-->\$//" \
    | sed -E 's/[[:space:]]+$//')
  [ "${#reason}" -ge 4 ]
}

rv_check_quality_sections() {
  local missing=0

  if ! rv_section_body_nonempty "A+ delta" "$BODY_FILE"; then
    if ! rv_has_escape "a-plus-not-applicable" "$BODY_FILE"; then
      echo "check-reviewer-verdict: missing_a_plus_delta — load-bearing PR has no '## A+ delta' section with non-empty body." >&2
      echo "  Fix: add a paragraph naming the specific evidence that would close the B->A->A+ gap (e.g. 'live integration test against regatta serve')." >&2
      echo "  Or, when not applicable, add to PR body:" >&2
      echo "    <!-- a-plus-not-applicable: <reason ≥4 chars> -->" >&2
      missing=1
    fi
  fi

  if ! rv_section_body_nonempty "Negative-space audit" "$BODY_FILE"; then
    if ! rv_has_escape "negative-space-not-applicable" "$BODY_FILE"; then
      echo "check-reviewer-verdict: missing_negative_space_audit — load-bearing PR has no '## Negative-space audit' section with non-empty body." >&2
      echo "  Fix: list ≥3 bypass attempts considered + outcome for each (mitigated / accepted / filed-as-tracker)." >&2
      echo "  Or, when not applicable, add to PR body:" >&2
      echo "    <!-- negative-space-not-applicable: <reason ≥4 chars> -->" >&2
      missing=1
    fi
  fi

  if ! rv_section_body_nonempty "Reviewer confidence" "$BODY_FILE"; then
    echo "check-reviewer-verdict: missing_reviewer_confidence — load-bearing PR has no '## Reviewer confidence' section with non-empty body." >&2
    echo "  Fix: name the verdict (APPROVE / INSUFFICIENT_EVIDENCE / REVISE / BLOCK) and 1-line rationale." >&2
    echo "  INSUFFICIENT_EVIDENCE pairs with a 'Confidence-evidence-needed: <what would unblock>' line." >&2
    missing=1
  fi

  if [ "$missing" -ne 0 ]; then
    exit 1
  fi
}
