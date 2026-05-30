#!/usr/bin/env bash
# Workflow-shape gate. GitHub treats SKIPPED required checks as
# satisfied, so an aggregator without if: always() + needs.*.result
# checks bypasses branch protection silently. Also asserts release.yml
# is tag-triggered with provenance + CHANGELOG flip.

set -euo pipefail

dir="${CI_WORKFLOWS_DIR:-.github/workflows}"

fail() {
  echo "check-ci-shape: $*" >&2
  exit 1
}

gates="$dir/gates.yml"
release="$dir/release.yml"

if [ ! -f "$gates" ]; then
  fail "missing $gates aggregator. Branch protection cannot rely on a multi-job CI without one."
fi

if ! grep -q 'if: always()' "$gates"; then
  fail "$gates aggregator must declare 'if: always()' on the aggregate job. GitHub short-circuits needs:* to SKIPPED on failure, and branch protection treats SKIPPED required checks as satisfied."
fi

if ! grep -Eq 'needs\.[a-zA-Z0-9_-]+\.result' "$gates"; then
  fail "$gates aggregator must explicitly check needs.<job>.result. Without per-job result inspection, the aggregator passes even when a sub-job failed."
fi

if [ ! -f "$release" ]; then
  fail "missing $release workflow. Tag-triggered release with provenance + CHANGELOG flip is required by spec section 5."
fi

if ! grep -Eq '^[[:space:]]*tags:' "$release"; then
  fail "$release must be tag-triggered (on.push.tags). Manual workflow_dispatch alone bypasses signed-tag provenance chain."
fi

if ! grep -qi 'attest-build-provenance\|provenance' "$release"; then
  fail "$release must generate SLSA build provenance attestation. The auditor surface (docs/auditor/reproducibility.md) cites this as the artifact-integrity proof."
fi

if ! grep -qi 'changelog' "$release"; then
  fail "$release must flip the changelog Unreleased section to a versioned section. Without this, CHANGELOG.md and the published release diverge."
fi

echo "check-ci-shape: ok"
