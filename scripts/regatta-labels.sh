#!/usr/bin/env bash
# regatta-labels.sh — apply the canonical 25-label set from
# docs/engineer/briefs/label-sop.md to any repo.
#
# Usage:
#   scripts/regatta-labels.sh apply           # create/update labels; never deletes
#   scripts/regatta-labels.sh apply --prune   # also delete labels not in the SOP set
#   scripts/regatta-labels.sh check           # fail non-zero if repo diverges from SOP
#
# Requires: gh CLI authenticated to the target repo. Operates on the
# current repo per `gh repo view`; pass `--repo owner/name` to gh via
# GH_REPO env if scripting across repos.

set -uo pipefail

MODE="${1:-check}"
PRUNE=0
for arg in "$@"; do
  if [ "$arg" = "--prune" ]; then PRUNE=1; fi
done

# Canonical label set. Format: NAME|COLOR|DESCRIPTION
LABELS=(
  # kind (8)
  "kind:bug|D93F0B|Defect: behavior deviates from spec or doc"
  "kind:feat|0E8A16|New capability"
  "kind:chore|8B949E|Tooling, dependency bump, repo housekeeping"
  "kind:docs|0075CA|Documentation-only change"
  "kind:refactor|8B949E|Internal restructure; no behavior change"
  "kind:test|1D76DB|Test-only addition or fix"
  "kind:wedge|586069|Umbrella tracking issue holding N slices"
  "kind:slice|586069|One implementation slice under an umbrella"
  # severity (4)
  "severity:critical|B60205|Production outage, security exploit, data loss"
  "severity:high|D93F0B|Real defect, no workaround, must fix before next release"
  "severity:medium|B07D00|Real defect with workaround, or latent risk in load-bearing surface"
  "severity:low|0E8A16|Cosmetic, edge case, deferred fine-tuning"
  # priority (3)
  "priority:p0|D93F0B|This-sprint critical-path; blocks other work"
  "priority:p1|B07D00|Next-sprint candidate; ships when p0 clears"
  "priority:p2|8B949E|Backlog; picked at operator discretion"
  # state (4)
  "state:blocked|D93F0B|Cannot start until a named prerequisite closes"
  "state:followup|586069|Deferred during PR review; reopen-trigger in body"
  "state:parking|8B949E|Forward-fit deferral; reopen-trigger required"
  "state:in-review|586069|Has an open PR or is under reviewer-subagent pass"
  # scope (4)
  "scope:security|5319E7|Auth, secrets, sandbox, supply chain, injection"
  "scope:ci|0052CC|CI gates, GitHub Actions, lint, test infrastructure"
  "scope:perf|006B75|Latency, throughput, memory, cost — change must show measurement"
  "scope:ux|006B75|Operator-facing surface — change must include the operator's eyeball"
  # special (2)
  "good-first-issue|7057FF|Self-contained, well-scoped, safe for a new contributor"
  "duplicate|424A53|Closing as duplicate of another tracked issue"
)

canonical_names() {
  for entry in "${LABELS[@]}"; do
    printf '%s\n' "${entry%%|*}"
  done | sort
}

repo_label_names() {
  gh label list --limit 200 --json name --jq '.[].name' | sort
}

apply_labels() {
  local missing=0
  local updated=0
  local failed=0
  local existing
  existing=$(gh label list --limit 200 --json name --jq '.[].name' || echo "")
  for entry in "${LABELS[@]}"; do
    IFS='|' read -r name color desc <<<"$entry"
    if printf '%s\n' "$existing" | grep -Fxq "$name"; then
      if ! gh label edit "$name" --color "$color" --description "$desc" >/dev/null 2>&1; then
        echo "edit FAILED: $name" >&2
        failed=$((failed + 1))
      else
        updated=$((updated + 1))
      fi
    else
      if ! gh label create "$name" --color "$color" --description "$desc" >/dev/null 2>&1; then
        echo "create FAILED: $name" >&2
        failed=$((failed + 1))
      else
        missing=$((missing + 1))
      fi
    fi
  done
  echo "apply: created=$missing updated=$updated failed=$failed"

  if [ "$PRUNE" -eq 1 ]; then
    local removed=0
    local extra
    extra=$(comm -23 <(repo_label_names) <(canonical_names))
    while IFS= read -r name; do
      [ -z "$name" ] && continue
      if gh label delete "$name" --yes >/dev/null 2>&1; then
        removed=$((removed + 1))
      else
        echo "delete FAILED: $name" >&2
        failed=$((failed + 1))
      fi
    done <<<"$extra"
    echo "prune: removed=$removed"
  fi

  if [ "$failed" -gt 0 ]; then
    echo "apply: $failed operation(s) failed; check output above" >&2
    return 1
  fi
}

check_labels() {
  local missing extra
  missing=$(comm -23 <(canonical_names) <(repo_label_names))
  extra=$(comm -13 <(canonical_names) <(repo_label_names))
  local rc=0
  if [ -n "$missing" ]; then
    echo "MISSING from repo (canonical set):" >&2
    while IFS= read -r line; do printf '  %s\n' "$line" >&2; done <<<"$missing"
    rc=1
  fi
  if [ -n "$extra" ]; then
    echo "EXTRA in repo (not in canonical SOP):" >&2
    while IFS= read -r line; do printf '  %s\n' "$line" >&2; done <<<"$extra"
    rc=1
  fi
  if [ "$rc" -eq 0 ]; then
    echo "label set matches SOP"
  fi
  exit "$rc"
}

case "$MODE" in
  apply) apply_labels ;;
  check) check_labels ;;
  *) echo "usage: $0 [apply|check] [--prune]" >&2; exit 2 ;;
esac
