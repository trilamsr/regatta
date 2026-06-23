#!/usr/bin/env bash
# check-docker-env-parity.sh — fail-closed if a REGATTA_* env var
# declared in docker-compose*.yml is NOT read by prod Go code
# (R-MEGA-2 G2). Catches typo-class drift: an operator names a knob
# REGATTA_STAT_DB in compose, nothing in the daemon ever reads it, and
# the silent-no-op behaviour misleads every future operator.
#
# Direction: compose → code only. The reverse (every Go-side
# REGATTA_* must appear in compose) is too noisy — many env vars are
# opt-in feature flags the default compose intentionally omits.
#
# Scope: REGATTA_ prefix only. OTEL_/GH_/secret vars are intentionally
# compose-only (operator-facing) or code-only.
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
cd "$repo_root"

tmp_compose="$(mktemp)"
tmp_code="$(mktemp)"
trap 'rm -f "$tmp_compose" "$tmp_code"' EXIT

# REGATTA_* keys named in docker-compose*.yml top-level environment maps.
{ grep -hoE 'REGATTA_[A-Z0-9_]+' docker-compose*.yml 2>/dev/null || true; } | sort -u > "$tmp_compose"

# REGATTA_* keys read by Go code (os.Getenv / os.LookupEnv / envconfig tags).
# Test files excluded — they exercise the readers, not configure the daemon.
# The for-loop over present roots avoids set -e killing the script when
# a tree (e.g. internal/) is absent in a fixture under test.
declare -a roots=()
for root in cmd internal; do
    [[ -d "$root" ]] && roots+=("$root")
done
if (( ${#roots[@]} > 0 )); then
    { grep -rhoE 'REGATTA_[A-Z0-9_]+' \
        --include='*.go' \
        --exclude='*_test.go' \
        "${roots[@]}" 2>/dev/null || true; } | sort -u > "$tmp_code"
else
    : > "$tmp_code"
fi

# Allowlist: keys legitimately in compose but NOT consumed by Go code.
# REGATTA_UI: compose-only ${REGATTA_UI:-true} default for --ui flag;
# Go side reads the parsed bool flag, not the env var.
declare -a compose_only_allowlist=(
    REGATTA_UI
)

fail=0

while IFS= read -r key; do
    [[ -z "$key" ]] && continue
    if ! grep -qx "$key" "$tmp_code"; then
        skip=0
        for allow in "${compose_only_allowlist[@]}"; do
            if [[ "$key" == "$allow" ]]; then skip=1; break; fi
        done
        if (( skip == 0 )); then
            echo "drift: $key declared in docker-compose*.yml but not read by Go code (typo? renamed?)" >&2
            fail=1
        fi
    fi
done < "$tmp_compose"

exit "$fail"
