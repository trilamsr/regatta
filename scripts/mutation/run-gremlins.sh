#!/usr/bin/env bash
# scripts/mutation/run-gremlins.sh — S2-T4 wave 1 mutation-testing runner.
#
# Spec: docs/engineer/specs/2026-06-02-s2-t4-mutation-testing.md (§3.2 allowlist,
# §4.2 per-package thresholds, §4.4 local-developer mode).
#
# Iterates the allowlist; invokes gremlins once per package with the package's
# efficacy floor. Per-package iteration is the only way to enforce per-package
# thresholds — gremlins' --threshold-efficacy is global.
#
# Modes:
#   default (no env)          — enforce per-package floors; exit non-zero on breach.
#   NO_THRESHOLD=1            — report only; exit 0 regardless. Developer mode.
#   ARTIFACT_DIR=<path>       — write per-package JSON reports. Workflow mode.
#   GREMLINS_WORKERS=<n>      — override worker count (default: nproc).
#   GREMLINS_PACKAGES=<list>  — override allowlist (space-separated); useful for
#                               on-demand workflow_dispatch one-package runs.
#
# Exit codes:
#   0   — all packages met their floor (or NO_THRESHOLD=1).
#   1   — at least one package below floor.
#   2   — gremlins binary missing.

set -euo pipefail

GREMLINS_BIN="${GREMLINS_BIN:-gremlins}"
if ! command -v "$GREMLINS_BIN" >/dev/null 2>&1; then
  echo "run-gremlins.sh: gremlins not on PATH; install via 'make mutation-test-install'" >&2
  exit 2
fi

# Per-package floors — spec §4.2 v1 column. Format: "pkg:floor".
# When extending, keep the dispatch-prompt boundary in mind:
#   wave 1 = wiring only; wave 2 = lift floors via new tests.
ALLOWLIST_DEFAULT=(
  "internal/cost/gate:80"
  "internal/cost/spend:75"
  "internal/cost/estimate:75"
  "internal/cost/reconcile:70"
  "internal/orchestrator/scheduler:70"
)

if [[ -n "${GREMLINS_PACKAGES:-}" ]]; then
  # Override allowlist; tokens are bare paths — apply the default floor (70).
  read -r -a override <<<"${GREMLINS_PACKAGES}"
  ALLOWLIST=()
  for pkg in "${override[@]}"; do
    ALLOWLIST+=("${pkg}:70")
  done
else
  ALLOWLIST=("${ALLOWLIST_DEFAULT[@]}")
fi

if [[ -n "${ARTIFACT_DIR:-}" ]]; then
  mkdir -p "${ARTIFACT_DIR}"
fi

# Default worker count: capped at 4. Higher parallelism races the
# go-test binary cache and produces TIMED-OUT mutants on hosts where
# `go test -race` is already CPU-bound (every mutation runs the full
# package test suite). Operators can override via GREMLINS_WORKERS.
if [[ -z "${GREMLINS_WORKERS:-}" ]]; then
  GREMLINS_WORKERS=4
fi

declare -a failures=()
declare -a summary=()
overall_status=0

for entry in "${ALLOWLIST[@]}"; do
  pkg="${entry%:*}"
  floor="${entry##*:}"
  pkg_slug="${pkg//\//_}"

  echo "==> ${pkg} (floor: ${floor}%)"

  args=(
    "unleash"
    "--workers" "${GREMLINS_WORKERS}"
    "./${pkg}/"
  )

  # Artifact mode writes JSON; workflow uploads as a CI artifact.
  if [[ -n "${ARTIFACT_DIR:-}" ]]; then
    args+=("--output" "${ARTIFACT_DIR}/${pkg_slug}.json")
  fi

  # Threshold enforcement: parse efficacy from output and gate in shell.
  # gremlins-v0.6.0 silently ignores --threshold-efficacy on the CLI
  # (only the YAML key triggers the gate), and the YAML key is global,
  # so per-package thresholds cannot be expressed in gremlins config at all.
  # The shell-side gate is the only path to per-package floors today.
  set +e
  output=$("${GREMLINS_BIN}" "${args[@]}" 2>&1)
  rc=$?
  set -e
  echo "${output}"

  if [[ "${rc}" -ne 0 ]]; then
    overall_status=1
    failures+=("${pkg} gremlins exited ${rc}")
    summary+=("${pkg}: gremlins error (exit ${rc}) floor=${floor}%")
    continue
  fi

  # Pull the efficacy line from gremlins' tail summary.
  efficacy=$(echo "${output}" | awk -F': ' '/^Test efficacy/ {gsub(/%/, "", $2); print $2}')

  summary+=("${pkg}: efficacy=${efficacy:-unknown}% floor=${floor}%")

  if [[ -z "${NO_THRESHOLD:-}" && -n "${efficacy}" ]]; then
    # awk does the float comparison; bash test only handles integers.
    breach=$(awk -v e="${efficacy}" -v f="${floor}" 'BEGIN { print (e + 0 < f + 0) ? 1 : 0 }')
    if [[ "${breach}" == "1" ]]; then
      overall_status=1
      failures+=("${pkg} efficacy=${efficacy}% below floor=${floor}%")
    fi
  fi
done

echo
echo "===== mutation-test summary ====="
printf '  %s\n' "${summary[@]}"

if [[ "${#failures[@]}" -gt 0 ]]; then
  echo
  echo "===== threshold breaches ====="
  printf '  %s\n' "${failures[@]}"
fi

if [[ -n "${NO_THRESHOLD:-}" ]]; then
  # Developer mode — exit 0 regardless.
  exit 0
fi

exit "${overall_status}"
