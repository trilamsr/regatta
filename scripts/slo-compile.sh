#!/usr/bin/env bash
# slo-compile.sh - compile every slo/*.yaml to Prom recording + alert
# rules under dashboards/prometheus/rules/ using the pinned Sloth binary.
#
# Idempotent + deterministic: same input YAML produces byte-equal output
# (spec §9 R3). The pin lives at tools/sloth/version; mismatch with the
# binary on PATH refuses to compile so version churn is loud.
#
# Auto-installs the pinned binary into ./tools/sloth/bin/sloth on the
# host's OS+arch if no compatible binary is on PATH. CI sandboxes that
# lack network egress can pre-stage the binary under that path and the
# script will reuse it.

set -euo pipefail

REPO_ROOT=$(cd "$(dirname "$0")/.." && pwd)
SLO_DIR="${REPO_ROOT}/slo"
OUT_DIR="${REPO_ROOT}/dashboards/prometheus/rules"
PIN_FILE="${REPO_ROOT}/tools/sloth/version"
CACHE_BIN="${REPO_ROOT}/tools/sloth/bin/sloth"
WINDOWS_DIR="${REPO_ROOT}/tools/sloth/windows"
DEFAULT_PERIOD="7d"

if [ ! -f "${PIN_FILE}" ]; then
  echo "slo-compile: missing version pin at ${PIN_FILE}" >&2
  exit 1
fi
PINNED=$(tr -d '[:space:]' < "${PIN_FILE}")

if [ ! -d "${SLO_DIR}" ] || ! ls "${SLO_DIR}"/*.yaml >/dev/null 2>&1; then
  echo "slo-compile: no OpenSLO YAMLs found under ${SLO_DIR}" >&2
  exit 1
fi

# Resolve a Sloth binary that matches the pin.
resolve_sloth() {
  if [ -x "${CACHE_BIN}" ]; then
    local got
    got=$("${CACHE_BIN}" version 2>&1 | head -1 | awk '{print $NF}')
    if [ "${got}" = "${PINNED}" ]; then
      echo "${CACHE_BIN}"
      return 0
    fi
  fi
  if command -v sloth >/dev/null 2>&1; then
    local got
    got=$(sloth version 2>&1 | head -1 | awk '{print $NF}')
    if [ "${got}" = "${PINNED}" ]; then
      command -v sloth
      return 0
    fi
    echo "slo-compile: sloth on PATH is ${got}, pin demands ${PINNED}" >&2
  fi
  return 1
}

# Download the pinned binary into the cache. Skipped when offline; the
# caller sees a clear "install sloth ${PINNED}" message instead.
download_sloth() {
  local os arch url
  os=$(uname -s | tr '[:upper:]' '[:lower:]')
  case "$(uname -m)" in
    x86_64|amd64) arch="amd64" ;;
    arm64|aarch64) arch="arm64" ;;
    *) echo "slo-compile: unsupported arch $(uname -m)" >&2; return 1 ;;
  esac
  url="https://github.com/slok/sloth/releases/download/${PINNED}/sloth-${os}-${arch}"
  mkdir -p "$(dirname "${CACHE_BIN}")"
  echo "slo-compile: downloading sloth ${PINNED} (${os}/${arch})"
  if ! curl -fsSL -o "${CACHE_BIN}" "${url}"; then
    echo "slo-compile: download failed; install sloth ${PINNED} manually" >&2
    return 1
  fi
  chmod +x "${CACHE_BIN}"
}

SLOTH_BIN=""
if ! SLOTH_BIN=$(resolve_sloth); then
  download_sloth || exit 1
  if ! SLOTH_BIN=$(resolve_sloth); then
    echo "slo-compile: downloaded binary did not match pin ${PINNED}" >&2
    exit 1
  fi
fi
echo "slo-compile: using ${SLOTH_BIN} (pin ${PINNED})"

mkdir -p "${OUT_DIR}"

# One input → one output. Deterministic: --no-color drops the only
# non-input-dependent byte in Sloth's output stream.
shopt -s nullglob
for src in "${SLO_DIR}"/*.yaml; do
  base=$(basename "${src}")
  dst="${OUT_DIR}/${base}"
  echo "slo-compile: ${src} -> ${dst}"
  "${SLOTH_BIN}" generate --no-color \
    --slo-period-windows-path "${WINDOWS_DIR}" \
    --default-slo-period "${DEFAULT_PERIOD}" \
    -i "${src}" -o "${dst}"
done

echo "slo-compile: done"
