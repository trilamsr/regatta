#!/usr/bin/env bash
# Test harness for check-alert-severity-label.sh (R-MEGA-3 LIVE-3).
set -euo pipefail

SCRIPT="$(cd "$(dirname "$0")" && pwd)/check-alert-severity-label.sh"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

# Fixture 1: missing severity → expect fail.
cat > "$TMP/bad.yaml" <<'EOF'
groups:
- name: substrate-bad
  rules:
  - alert: BadAlert
    expr: vector(1)
    labels:
      category: integrity
      sloth_severity: page
    annotations:
      summary: bad
EOF
if "$SCRIPT" "$TMP" >/dev/null 2>&1; then
  echo "FAIL: expected gate to flag missing severity"
  exit 1
fi

# Fixture 2: severity present → expect pass.
cat > "$TMP/good.yaml" <<'EOF'
groups:
- name: substrate-good
  rules:
  - alert: GoodAlert
    expr: vector(1)
    labels:
      category: integrity
      severity: page
      sloth_severity: page
    annotations:
      summary: good
EOF
rm "$TMP/bad.yaml"
if ! "$SCRIPT" "$TMP" >/dev/null 2>&1; then
  echo "FAIL: expected gate to pass with severity present"
  exit 1
fi

# Fixture 3: ticket-severity (not page) → no severity required.
cat > "$TMP/ticket.yaml" <<'EOF'
groups:
- name: substrate-ticket
  rules:
  - alert: TicketAlert
    expr: vector(1)
    labels:
      category: integrity
      sloth_severity: ticket
    annotations:
      summary: ticket
EOF
rm "$TMP/good.yaml"
if ! "$SCRIPT" "$TMP" >/dev/null 2>&1; then
  echo "FAIL: expected gate to pass on ticket-only alerts"
  exit 1
fi

echo "OK"
