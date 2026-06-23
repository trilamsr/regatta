#!/usr/bin/env bash
# Fail-closed gate: every alert rule under dashboards/prometheus/rules/
# with sloth_severity == "page" MUST also carry a `severity:` label
# (AlertManager routes on `severity`, not `sloth_severity`) — R-MEGA-3 LIVE-3.
set -euo pipefail

RULES_DIR="${1:-dashboards/prometheus/rules}"
fail=0

for f in "$RULES_DIR"/*.yaml; do
  [[ -e "$f" ]] || continue
  # awk walks each labels: block; flags page alerts without `severity:`.
  awk '
    /^[[:space:]]*labels:[[:space:]]*$/ {
      in_labels = 1
      has_severity = 0
      has_sloth_page = 0
      block_start = NR
      next
    }
    in_labels {
      if ($0 ~ /^[[:space:]]+severity:[[:space:]]/) has_severity = 1
      if ($0 ~ /^[[:space:]]+sloth_severity:[[:space:]]*page[[:space:]]*$/) has_sloth_page = 1
      # labels block ends at the next non-indented key (typically annotations:).
      if ($0 ~ /^[[:space:]]*[a-zA-Z_]+:[[:space:]]*$/ && $0 !~ /labels:/) {
        if (has_sloth_page && !has_severity) {
          printf "%s:%d: page alert missing `severity:` label\n", FILENAME, block_start
          exit_code = 1
        }
        in_labels = 0
      }
    }
    END { exit exit_code }
  ' "$f" || fail=1
done

if [[ $fail -ne 0 ]]; then
  echo "ERROR: prometheus rules missing severity label on page alerts" >&2
  echo "Fix: add `severity: page` (or `severity: critical`) to the labels block." >&2
  exit 1
fi
