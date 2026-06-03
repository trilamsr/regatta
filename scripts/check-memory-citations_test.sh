#!/usr/bin/env bash
# check-memory-citations_test.sh - fixtures for check-memory-citations.sh.
#
# Asserts (a) every `feedback_*` slug cited from CLAUDE.md, the boot prompt,
# and the four dispatch templates resolves to a real file under MEMORY_DIR
# (or its archive/ subdir) → exit 0; (b) a fake citation pointing at an
# absent slug → exit 1. MEMORY_DIR is parameterized so CI can point at a
# fixture tree instead of the operator's real memory dir.

set -uo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/.." && pwd)
CHECK="$REPO_ROOT/scripts/check-memory-citations.sh"

passes=0
fails=0
failed_names=()

run_case() {
  # run_case <case-name> <expected-exit> <mem-dir> <extra-doc-file-or-empty>
  local name="$1" want_exit="$2" mem_dir="$3" extra_doc="$4"
  local got_exit out
  out=$(MEMORY_DIR="$mem_dir" EXTRA_DOC="$extra_doc" bash "$CHECK" 2>&1)
  got_exit=$?
  if [ "$got_exit" -eq "$want_exit" ]; then
    passes=$((passes + 1))
    echo "ok   $name (exit=$got_exit)"
  else
    fails=$((fails + 1))
    failed_names+=("$name")
    echo "FAIL $name: want exit=$want_exit got=$got_exit"
    printf '%s\n' "$out" | sed 's/^/  | /'
  fi
}

# Build a clean fixture memory dir mirroring every live slug cited in the
# repo doc surfaces. The script enumerates citations from CLAUDE.md +
# autonomous-session-prompt.md + the four dispatch templates, then resolves
# each against MEMORY_DIR. We populate the fixture with the union.
build_fixture_mem() {
  local dir="$1"
  mkdir -p "$dir/archive"
  local slugs
  slugs=$(grep -hoE 'feedback_[a-z0-9_]+' \
    "$REPO_ROOT/CLAUDE.md" \
    "$REPO_ROOT/docs/engineer/autonomous-session-prompt.md" \
    "$REPO_ROOT/docs/engineer/dispatch-templates/"*.md \
    2>/dev/null | sort -u)
  while IFS= read -r slug; do
    [ -n "$slug" ] && touch "$dir/$slug.md"
  done <<< "$slugs"
  # Plant an archived one too — the gate must accept archive/ resolution.
  touch "$dir/archive/feedback_gate_relaxation_phase_s.md"
}

# Case A: all cited slugs resolve → exit 0.
tmp_ok=$(mktemp -d)
build_fixture_mem "$tmp_ok"
run_case "all-cited-slugs-resolve" 0 "$tmp_ok" ""
rm -rf "$tmp_ok"

# Case B: planted citation to a non-existent slug → exit 1.
tmp_bad=$(mktemp -d)
build_fixture_mem "$tmp_bad"
bad_doc=$(mktemp)
printf 'See feedback_this_slug_does_not_exist for context.\n' > "$bad_doc"
run_case "broken-slug-detected" 1 "$tmp_bad" "$bad_doc"
rm -rf "$tmp_bad" "$bad_doc"

echo "---"
echo "passes=$passes fails=$fails"
if [ "$fails" -ne 0 ]; then
  echo "failed: ${failed_names[*]}"
  exit 1
fi
exit 0
