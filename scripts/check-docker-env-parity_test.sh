#!/usr/bin/env bash
# Tests for scripts/check-docker-env-parity.sh — exercises a synthetic
# compose + Go-source tree under a TMP CWD so the real repo never
# matters (R-MEGA-2 G2).
set -euo pipefail

script_path="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/check-docker-env-parity.sh"
if [[ ! -x "$script_path" ]]; then
    echo "FAIL: $script_path missing or not executable" >&2
    exit 1
fi

run_case() {
    local name="$1" compose="$2" gocode="$3" expect_rc="$4"
    local tmp; tmp="$(mktemp -d)"
    trap 'rm -rf "$tmp"' RETURN
    (
        cd "$tmp"
        git init -q
        printf '%s' "$compose" > docker-compose.yml
        mkdir -p cmd/regatta
        printf '%s' "$gocode" > cmd/regatta/main.go
        set +e
        "$script_path" >/dev/null 2>&1
        actual=$?
        set -e
        if [[ "$actual" != "$expect_rc" ]]; then
            echo "FAIL [$name]: expected rc=$expect_rc got rc=$actual" >&2
            exit 1
        fi
    )
    rm -rf "$tmp"
    trap - RETURN
}

# Case 1: every compose key is read by Go → pass (rc=0).
run_case "all-consumed" \
    'services:
  regatta:
    environment:
      REGATTA_FOO: x
      REGATTA_BAR: y
' \
    'package main
func main() { _ = "REGATTA_FOO"; _ = "REGATTA_BAR" }' \
    0

# Case 2: compose declares a key never read by Go → fail (rc=1).
run_case "compose-typo" \
    'services:
  regatta:
    environment:
      REGATTA_TYPO: x
' \
    'package main
func main() { _ = "REGATTA_FOO" }' \
    1

# Case 3: REGATTA_UI in compose but not Go is explicitly allowed.
run_case "ui-allowlist" \
    'services:
  regatta:
    environment:
      REGATTA_UI: "true"
' \
    'package main' \
    0

# Case 4: Go-only key is fine (drift direction is compose → code).
run_case "go-only-ok" \
    'services:
  regatta:
    environment: {}
' \
    'package main
func main() { _ = "REGATTA_OPTIN_FEATURE" }' \
    0

echo "PASS: scripts/check-docker-env-parity_test.sh"
