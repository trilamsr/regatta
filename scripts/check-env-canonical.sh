#!/usr/bin/env bash
# check-env-canonical.sh — fail-closed if prod Go code reads a
# non-canonical env-var name when a canonical alias exists
# (R-MEGA-2 G3). Closes the rename-drift class where docker-compose
# pins REGATTA_STATE_DB and a sibling Go path still reads the legacy
# REGATTA_DB, so two code paths disagree on which database to open.
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
cd "$repo_root"

# alias map — `<legacy>=<canonical>`. A grep hit on the LHS in prod code
# fails the gate; the RHS is the expected replacement.
declare -a aliases=(
    "REGATTA_DB=REGATTA_STATE_DB"
)

# Per-call-site allowlist: a line carrying `// canonical-env-skip: <reason>`
# is an intentional legacy read (e.g. back-compat fallback). The gate
# greps for the legacy name AND requires the same line lack the skip
# marker to count as drift.
fail=0
for entry in "${aliases[@]}"; do
    legacy="${entry%%=*}"
    canonical="${entry#*=}"
    hits=$(grep -rln --include='*.go' --exclude='*_test.go' \
        -E "\"${legacy}\"" cmd/ internal/ 2>/dev/null || true)
    if [[ -z "$hits" ]]; then
        continue
    fi
    while IFS= read -r path; do
        [[ -z "$path" ]] && continue
        # A hit is a drift unless every occurrence of the legacy literal
        # in this file is accompanied by the skip marker on the same line.
        offending=$(grep -nE "\"${legacy}\"" "$path" | grep -v 'canonical-env-skip:' || true)
        if [[ -n "$offending" ]]; then
            echo "drift: prod code reads legacy env var ${legacy}; use ${canonical} or annotate //canonical-env-skip: <reason>:" >&2
            echo "$offending" | sed "s|^|  $path:|" >&2
            fail=1
        fi
    done <<< "$hits"
done

exit "$fail"
