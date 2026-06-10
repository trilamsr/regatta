#!/usr/bin/env bash
# check-phase-x-leak.sh - mechanical enforcement of the self-host filter.
#
# CLAUDE.md §Self-host filter + docs/engineer/briefs/2026-06-01-self-host-first.md
# §1 + §4 say: defer Phase-X scope (multi-tenant `tenant_id`, RBAC, Stripe
# metered billing, Sigstore/Rekor attestation, blackboard CAS, Temporal
# workflow engine, htmx UI) until an external paying customer asks. Without a gate,
# the filter is prose-only and Phase-X scope creeps into active specs via
# forward-fit prose. This gate fails the build when an `active` spec names
# a Phase-X token outside an opt-in escape hatch.
#
# Scope: every *.md file in `docs/engineer/specs/` (overridable via
# SPECS_DIR for the test fixture).
#
# Opt-in escape hatches (the spec is intentionally aware of Phase-X):
#   - frontmatter `phase: x-forward-fit`
#   - frontmatter `phase: x-prefetch`
#   - frontmatter `status: skeleton-prefetch`
#
# Skipped statuses (Phase-X tokens are historical, not new scope):
#   - shipped
#   - archived
#   - superseded
#   - skeleton-prefetch (also an opt-in)
#   - phase-x-deferred (#1238 sweep)
#
# Scanned statuses:
#   - active
#   - missing/empty status (defaults to active per gen-specs-readme.sh)
#   - draft, backlog, anything else not in the skip set
#
# Fenced + inline-backtick spans are stripped before scanning, mirroring
# scripts/doc-check.sh — a spec that names a token in a literal/code span
# is meta-documenting it, not absorbing it as scope.
#
# Exit: 0 clean, 1 on first leak (lists every hit before exit).

set -uo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/.." && pwd)

: "${SPECS_DIR:=$REPO_ROOT/docs/engineer/specs}"

if [ ! -d "$SPECS_DIR" ]; then
  echo "check-phase-x-leak: SPECS_DIR not found: $SPECS_DIR (skipping)"
  exit 0
fi

# Phase-X tokens pinned to the self-host brief §1 + §4. Word-boundary regex
# (case-sensitive — `grep -E` default). `Temporal` matches only when capitalized
# (lowercase `temporal` is generic time-domain prose; `Temporal` is the Workflow
# engine product). Same heuristic for `Sigstore` / `Stripe` / `Rekor` — product
# names the brief itself spells with leading caps.
#
# `tenant_id` is matched lower_snake_case because that is the SQL column +
# attribute spelling the brief calls out; the prose word "tenant" alone is
# not in scope. `htmx` is matched lowercase because that is the brief's spelling
# ("No htmx UI needed" §1) and the upstream library's own casing.
phase_x_tokens=(
  '\btenant_id\b'
  '\bRBAC\b'
  '\bStripe\b'
  '\bSigstore\b'
  '\bRekor\b'
  '\bblackboard\b'
  '\bTemporal\b'
  '\bhtmx\b'
)
# Single regex for the grep pass.
tokens_union=$(IFS='|'; echo "${phase_x_tokens[*]}")

# strip_doc_spans <file> -> stdout — same shape as doc-check.sh.
strip_doc_spans() {
  perl -ne '
    BEGIN { $in_fence = 0 }
    if (/^```/) { $in_fence = !$in_fence; print "\n"; next }
    if ($in_fence) { print "\n"; next }
    s/`[^`]*`//g;
    print;
  ' "$1"
}

# parse_spec <path> -> "<status>|<phase>"
#   status defaults to "active"; phase defaults to "".
parse_spec() {
  python3 - "$1" <<'PY'
import sys
path = sys.argv[1]
with open(path, "r", encoding="utf-8", errors="replace") as fh:
    lines = fh.read().splitlines()

status = ""
phase = ""

if lines and lines[0].strip() == "---":
    end = None
    for i in range(1, len(lines)):
        if lines[i].strip() == "---":
            end = i
            break
    if end is not None:
        for raw in lines[1:end]:
            if ":" not in raw:
                continue
            k, _, v = raw.partition(":")
            k = k.strip().lower()
            v = v.strip()
            if len(v) >= 2 and v[0] == v[-1] and v[0] in ("'", '"'):
                v = v[1:-1]
            if k == "status" and not status:
                status = v.lower()
            elif k == "phase" and not phase:
                phase = v.lower()

if not status:
    status = "active"

print(f"{status}|{phase}")
PY
}

# Statuses that skip the gate. `phase-x-deferred` pairs with the #1238 sweep
# that moved active specs into docs/engineer/specs/phase-x/.
is_skipped_status() {
  case "$1" in
    shipped|archived|superseded|skeleton-prefetch|phase-x-deferred) return 0 ;;
    *) return 1 ;;
  esac
}

# Phase opt-in markers.
is_phase_optin() {
  case "$1" in
    x-forward-fit|x-prefetch) return 0 ;;
    *) return 1 ;;
  esac
}

scanned=0
leaks=0
leak_lines=""

while IFS= read -r -d '' specfile; do
  base=$(basename -- "$specfile")
  [ "$base" = "README.md" ] && continue

  meta=$(parse_spec "$specfile")
  status="${meta%|*}"
  phase="${meta##*|}"

  if is_skipped_status "$status"; then
    continue
  fi
  if is_phase_optin "$phase"; then
    continue
  fi

  scanned=$((scanned + 1))

  hits=$(strip_doc_spans "$specfile" | grep -nE -- "$tokens_union" || true)
  if [ -n "$hits" ]; then
    while IFS= read -r line; do
      [ -z "$line" ] && continue
      # Surface the FIRST matching token so the error message names it.
      token=$(printf '%s' "$line" | grep -oE -- "$tokens_union" | head -1)
      leak_lines="${leak_lines}${specfile}:${line%%:*}: Phase-X token \`${token}\` in active spec — mark \`phase: x-forward-fit\` (or \`x-prefetch\`) in frontmatter, flip \`status: skeleton-prefetch\`, or remove the token."$'\n'
      leaks=$((leaks + 1))
    done <<< "$hits"
  fi
done < <(find "$SPECS_DIR" -maxdepth 1 -type f -name '*.md' -print0)

if [ "$leaks" -gt 0 ]; then
  echo "check-phase-x-leak: $leaks Phase-X token leak(s) detected across $scanned active spec(s):"
  printf '%s' "$leak_lines" | sed 's/^/  - /'
  echo
  echo "Self-host filter (CLAUDE.md + docs/engineer/briefs/2026-06-01-self-host-first.md §1):"
  echo "  Phase-X scope (tenant_id, RBAC, Stripe, Sigstore, Rekor, blackboard, Temporal, htmx)"
  echo "  defers until an external paying customer asks. Active specs that touch these"
  echo "  tokens must declare intent via frontmatter \`phase: x-forward-fit\`."
  exit 1
fi

echo "check-phase-x-leak: $scanned active spec(s) scanned; no Phase-X token leaks"
exit 0
