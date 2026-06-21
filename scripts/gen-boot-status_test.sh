#!/usr/bin/env bash
# gen-boot-status_test.sh - assertions for scripts/gen-boot-status.sh.
#
# The generator queries `gh` for merged PRs and open priority issues. Tests
# stub `gh` via PATH (tmpdir mock binary that prints fixed JSON) so the suite
# is hermetic and offline-safe. Real repo state stays untouched.

set -uo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/.." && pwd)
GEN="$REPO_ROOT/scripts/gen-boot-status.sh"
FIXTURE="$SCRIPT_DIR/testdata/boot-status/prompt-fixture.md"

passes=0
fails=0
failed_names=()

ok()   { passes=$((passes + 1)); echo "ok   $1"; }
fail() { fails=$((fails + 1)); failed_names+=("$1"); echo "FAIL $1: $2"; }

# stub_gh — write a `gh` shim into $1 (a tmp bindir). The shim emits canned
# JSON for the two query shapes the generator uses, anything else exits 2.
stub_gh() {
  local bindir="$1"
  mkdir -p "$bindir"
  cat > "$bindir/gh" <<'STUB'
#!/usr/bin/env bash
# Minimal gh stub: distinguishes `pr list` vs `issue list` by first two args.
case "$1 $2" in
  "pr list")
    cat <<'JSON'
[{"number":737,"title":"[REFACTOR] split serve.go","mergedAt":"2026-06-04T01:08:09Z"},
 {"number":734,"title":"[REFACTOR] inline single-impl interfaces","mergedAt":"2026-06-04T00:54:12Z"},
 {"number":723,"title":"[CI] mechanical self-host filter gate","mergedAt":"2026-06-04T00:16:07Z"}]
JSON
    ;;
  "issue list")
    cat <<'JSON'
[{"number":739,"title":"[BLOCKER] Wave G state/ 3-way split","labels":[{"name":"followup"}]},
 {"number":738,"title":"[D-followup] rename serve_*.go → wire_*.go","labels":[{"name":"followup"}]}]
JSON
    ;;
  *)
    echo "gh stub: unsupported args: $*" >&2
    exit 2
    ;;
esac
STUB
  chmod +x "$bindir/gh"
}

setup_workdir() {
  local tmp
  tmp=$(mktemp -d)
  cp "$FIXTURE" "$tmp/prompt.md"
  mkdir -p "$tmp/bin"
  stub_gh "$tmp/bin"
  echo "$tmp"
}

# TestGenBootStatus_ReplacesShippedMarkers
case_replaces_shipped() {
  local name="replaces auto-shipped block content"
  local dir
  dir=$(setup_workdir)
  PATH="$dir/bin:$PATH" bash "$GEN" --prompt "$dir/prompt.md" >/dev/null 2>&1
  local content
  content=$(cat "$dir/prompt.md")
  local fail_reason=""
  echo "$content" | grep -qF 'stale content that should be replaced' && \
    fail_reason="${fail_reason}stale shipped content survived; "
  echo "$content" | grep -qF '#737' || fail_reason="${fail_reason}PR #737 missing; "
  echo "$content" | grep -qF '#734' || fail_reason="${fail_reason}PR #734 missing; "
  echo "$content" | grep -qF 'BEGIN auto-shipped' || fail_reason="${fail_reason}BEGIN marker dropped; "
  echo "$content" | grep -qF 'END auto-shipped' || fail_reason="${fail_reason}END marker dropped; "
  rm -rf "$dir"
  if [ -z "$fail_reason" ]; then ok "$name"; else fail "$name" "$fail_reason"; fi
}

# TestGenBootStatus_ReplacesPriorityMarkers
case_replaces_priority() {
  local name="replaces auto-priority block content"
  local dir
  dir=$(setup_workdir)
  PATH="$dir/bin:$PATH" bash "$GEN" --prompt "$dir/prompt.md" >/dev/null 2>&1
  local content
  content=$(cat "$dir/prompt.md")
  local fail_reason=""
  echo "$content" | grep -qF 'stale priority content' && \
    fail_reason="${fail_reason}stale priority content survived; "
  echo "$content" | grep -qF '#739' || fail_reason="${fail_reason}issue #739 missing; "
  echo "$content" | grep -qF '#738' || fail_reason="${fail_reason}issue #738 missing; "
  rm -rf "$dir"
  if [ -z "$fail_reason" ]; then ok "$name"; else fail "$name" "$fail_reason"; fi
}

# TestGenBootStatus_Idempotent
case_idempotent() {
  local name="second run produces byte-identical output"
  local dir
  dir=$(setup_workdir)
  PATH="$dir/bin:$PATH" bash "$GEN" --prompt "$dir/prompt.md" >/dev/null 2>&1
  local first
  first=$(cat "$dir/prompt.md")
  PATH="$dir/bin:$PATH" bash "$GEN" --prompt "$dir/prompt.md" >/dev/null 2>&1
  local second
  second=$(cat "$dir/prompt.md")
  rm -rf "$dir"
  if [ "$first" = "$second" ]; then ok "$name"; else fail "$name" "two runs diverged"; fi
}

# TestGenBootStatus_ErrorsOnMissingMarkers
case_missing_markers() {
  local name="exits non-zero when prompt lacks markers"
  local dir
  dir=$(mktemp -d)
  echo "no markers here" > "$dir/prompt.md"
  mkdir -p "$dir/bin"
  stub_gh "$dir/bin"
  PATH="$dir/bin:$PATH" bash "$GEN" --prompt "$dir/prompt.md" >/dev/null 2>&1
  local got=$?
  rm -rf "$dir"
  if [ "$got" -ne 0 ]; then ok "$name"; else fail "$name" "expected non-zero exit"; fi
}

# TestGenBootStatus_PreservesOutsideMarkers
case_preserves_outside() {
  local name="prose outside markers untouched"
  local dir
  dir=$(setup_workdir)
  PATH="$dir/bin:$PATH" bash "$GEN" --prompt "$dir/prompt.md" >/dev/null 2>&1
  local content
  content=$(cat "$dir/prompt.md")
  local fail_reason=""
  echo "$content" | grep -qF '# Autonomous Session Trigger Prompt' || \
    fail_reason="${fail_reason}H1 dropped; "
  echo "$content" | grep -qF 'Sample preamble.' || \
    fail_reason="${fail_reason}preamble dropped; "
  echo "$content" | grep -qF 'Trailing prose.' || \
    fail_reason="${fail_reason}trailer dropped; "
  rm -rf "$dir"
  if [ -z "$fail_reason" ]; then ok "$name"; else fail "$name" "$fail_reason"; fi
}

# TestGenBootStatus_EscapesHostileTitles asserts triple-backtick + leading-pipe
# titles render without breaking markdown structure downstream.
case_escapes_hostile_titles() {
  local name="escapes triple-backtick + leading-pipe in titles"
  local dir
  dir=$(mktemp -d)
  cp "$FIXTURE" "$dir/prompt.md"
  mkdir -p "$dir/bin"
  cat > "$dir/bin/gh" <<'STUB'
#!/usr/bin/env bash
case "$1 $2" in
  "pr list")
    cat <<'JSON'
[{"number":900,"title":"fix ```code``` block break","mergedAt":"2026-06-04T01:08:09Z"},
 {"number":901,"title":"|leading pipe corrupts table","mergedAt":"2026-06-04T01:08:09Z"}]
JSON
    ;;
  "issue list")
    echo "[]"
    ;;
  *) exit 2 ;;
esac
STUB
  chmod +x "$dir/bin/gh"
  PATH="$dir/bin:$PATH" bash "$GEN" --prompt "$dir/prompt.md" >/dev/null 2>&1
  local content
  content=$(cat "$dir/prompt.md")
  local fail_reason=""
  echo "$content" | grep -qF '```code```' && \
    fail_reason="${fail_reason}triple-backtick survived unescaped; "
  echo "$content" | grep -qF '#900' || fail_reason="${fail_reason}PR #900 missing; "
  echo "$content" | grep -qF '#901' || fail_reason="${fail_reason}PR #901 missing; "
  # Bullet for #901 must escape leading pipe: "- #901 \|..."
  echo "$content" | grep -qFe '- #901 \|' || \
    fail_reason="${fail_reason}leading pipe not escaped; "
  rm -rf "$dir"
  if [ -z "$fail_reason" ]; then ok "$name"; else fail "$name" "$fail_reason"; fi
}

# TestGenBootStatus_FailsWhenGhFails asserts gh non-zero exit propagates
# (instead of being silently swallowed into a "[]" empty payload).
case_fails_when_gh_fails() {
  local name="propagates gh failure rather than silently writing empty block"
  local dir
  dir=$(mktemp -d)
  cp "$FIXTURE" "$dir/prompt.md"
  mkdir -p "$dir/bin"
  cat > "$dir/bin/gh" <<'STUB'
#!/usr/bin/env bash
echo "gh: auth required" >&2
exit 4
STUB
  chmod +x "$dir/bin/gh"
  PATH="$dir/bin:$PATH" bash "$GEN" --prompt "$dir/prompt.md" >/dev/null 2>&1
  local got=$?
  rm -rf "$dir"
  if [ "$got" -ne 0 ]; then ok "$name"; else fail "$name" "expected non-zero exit on gh failure"; fi
}

# TestGenBootStatus_ExcludeLabelDropsMatchingIssues asserts --exclude-label
# post-filters the priority block — used to suppress parking labels (phase-x)
# without polluting the unfiltered gh query shape.
case_exclude_label() {
  local name="--exclude-label drops issues carrying that label"
  local dir
  dir=$(mktemp -d)
  cp "$FIXTURE" "$dir/prompt.md"
  mkdir -p "$dir/bin"
  cat > "$dir/bin/gh" <<'STUB'
#!/usr/bin/env bash
case "$1 $2" in
  "pr list")
    echo "[]"
    ;;
  "issue list")
    cat <<'JSON'
[{"number":810,"title":"keep me","labels":[{"name":"followup"}]},
 {"number":811,"title":"drop me","labels":[{"name":"phase-x"}]},
 {"number":812,"title":"also drop","labels":[{"name":"followup"},{"name":"phase-x"}]}]
JSON
    ;;
  *) exit 2 ;;
esac
STUB
  chmod +x "$dir/bin/gh"
  PATH="$dir/bin:$PATH" bash "$GEN" --prompt "$dir/prompt.md" --exclude-label phase-x >/dev/null 2>&1
  local content
  content=$(cat "$dir/prompt.md")
  local fail_reason=""
  echo "$content" | grep -qF '#810' || fail_reason="${fail_reason}#810 (no phase-x) missing; "
  echo "$content" | grep -qF '#811' && fail_reason="${fail_reason}#811 (phase-x) survived; "
  echo "$content" | grep -qF '#812' && fail_reason="${fail_reason}#812 (phase-x+followup) survived; "
  rm -rf "$dir"
  if [ -z "$fail_reason" ]; then ok "$name"; else fail "$name" "$fail_reason"; fi
}

case_replaces_shipped
case_replaces_priority
case_idempotent
case_missing_markers
case_preserves_outside
case_escapes_hostile_titles
case_fails_when_gh_fails
case_exclude_label

echo
if [ "$fails" -gt 0 ]; then
  echo "gen-boot-status_test: $fails failed, $passes passed"
  for n in "${failed_names[@]}"; do echo "  - $n"; done
  exit 1
fi
echo "gen-boot-status_test: all $passes case(s) passed"
