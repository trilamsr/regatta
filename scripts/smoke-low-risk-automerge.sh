#!/usr/bin/env bash
# smoke-low-risk-automerge.sh — pre-flight check operators run before
# flipping --auto-merge=true + low_risk_automerge.enabled=true (MAY-270
# §1, live PRFetcher smoke harness).
#
# Default (re-runnable, no creds): exercises the lowrisk classifier
# directly against a synthetic doc-only PR + synthetic load-bearing PR.
# Asserts (eligible=true) + (eligible=false). Always re-runnable.
#
# Live mode (opt-in): set LOWRISK_SMOKE_LIVE_PR_DOC + LOWRISK_SMOKE_LIVE_PR_LOAD_BEARING
# to real PR numbers in this repo to additionally exercise the real
# `gh pr view --json files,additions,deletions,createdAt` shell-out
# through lowrisk.NewGhFetcher(). Requires gh on PATH + valid auth.
#
# Exit:
#   0 — all assertions pass.
#   1 — assertion failure (verdict didn't match expectation).
#   2 — wiring failure (go missing, build failed, gh missing in live mode).

set -uo pipefail

ROOT=${ROOT:-.}
cd "$ROOT" || { echo "smoke: cd $ROOT failed" >&2; exit 2; }

if ! command -v go >/dev/null 2>&1; then
  echo "smoke: go not on PATH" >&2
  exit 2
fi

# Run stub-mode unconditionally — the re-runnable, creds-free path.
if ! go run ./cmd/smoke-lowrisk-automerge; then
  echo "smoke: stub-mode FAILED" >&2
  exit 1
fi

# Live mode is opt-in: skip silently when env vars unset.
doc_pr=${LOWRISK_SMOKE_LIVE_PR_DOC:-0}
lb_pr=${LOWRISK_SMOKE_LIVE_PR_LOAD_BEARING:-0}
if [ "$doc_pr" = "0" ] && [ "$lb_pr" = "0" ]; then
  echo "smoke: live-mode SKIPPED (set LOWRISK_SMOKE_LIVE_PR_DOC / LOWRISK_SMOKE_LIVE_PR_LOAD_BEARING to exercise NewGhFetcher)"
  exit 0
fi

if ! command -v gh >/dev/null 2>&1; then
  echo "smoke: gh not on PATH (live mode requires gh auth)" >&2
  exit 2
fi

if ! go run ./cmd/smoke-lowrisk-automerge \
    --pr-doc-only="$doc_pr" \
    --pr-load-bearing="$lb_pr"; then
  echo "smoke: live-mode FAILED" >&2
  exit 1
fi
