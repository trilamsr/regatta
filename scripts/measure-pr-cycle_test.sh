#!/usr/bin/env bash
# Smoke test for scripts/measure-pr-cycle.sh. Stubs `gh` on PATH to
# exercise --help, unknown-flag rejection, empty-window, and a fixture
# PR list with synthetic per-stage timestamps. No network.

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
target="$script_dir/measure-pr-cycle.sh"

failed=0
pass=0

assert_contains() {
  local name="$1" haystack="$2" needle="$3"
  if printf '%s\n' "$haystack" | grep -q -- "$needle"; then
    pass=$((pass + 1))
    echo "PASS $name"
  else
    failed=$((failed + 1))
    echo "FAIL $name: stdout missing '$needle'"
    echo "  output:"
    printf '%s\n' "$haystack" | sed 's/^/    /'
  fi
}

# Case 1 - --help prints usage and exits 0.
out=$("$target" --help 2>&1) && rc=0 || rc=$?
if [ "$rc" -ne 0 ]; then
  echo "FAIL help-exit-zero: got $rc"
  failed=$((failed + 1))
else
  pass=$((pass + 1))
  echo "PASS help-exit-zero"
fi
assert_contains "help-mentions-last"      "$out" "last"
assert_contains "help-mentions-stage"     "$out" "stage"

# Case 2 - unknown flag fails with exit 2.
out=$("$target" --bogus 2>&1) && rc=0 || rc=$?
if [ "$rc" -ne 2 ]; then
  echo "FAIL unknown-flag-exit-2: got $rc"
  failed=$((failed + 1))
else
  pass=$((pass + 1))
  echo "PASS unknown-flag-exit-2"
fi

# Case 3 - empty merged-PR list exits 0 with a "no merged PRs" sentinel.
stub_dir=$(mktemp -d -t measure-pr-cycle-stub.XXXXXX)
trap 'rm -rf "$stub_dir"' EXIT

cat > "$stub_dir/gh" <<'STUB'
#!/usr/bin/env bash
case "$1" in
  pr)
    shift
    if [ "${1:-}" = "list" ]; then
      echo "[]"
      exit 0
    fi
    ;;
  api)
    echo "[]"
    exit 0
    ;;
esac
echo "stub-gh: unexpected args: $*" >&2
exit 99
STUB
chmod +x "$stub_dir/gh"

out=$(PATH="$stub_dir:$PATH" "$target" --last 5 2>&1) && rc=0 || rc=$?
if [ "$rc" -ne 0 ]; then
  echo "FAIL empty-window-exit-zero: got $rc"
  failed=$((failed + 1))
  echo "  output:"
  printf '%s\n' "$out" | sed 's/^/    /'
else
  pass=$((pass + 1))
  echo "PASS empty-window-exit-zero"
fi
assert_contains "empty-window-sentinel" "$out" "no merged PRs"

# Case 4 - fixture PR with synthetic timestamps produces a table row
# AND aggregate summary lines. The script must:
#   (a) emit one row per PR with PR# + each stage lag
#   (b) emit a "median" line and a "p95" line in the aggregates block
cat > "$stub_dir/gh" <<'STUB'
#!/usr/bin/env bash
# pr list -> two merged PRs with commit history + reviewer body
# pr view -> per-PR body + commits + check runs
# api    -> per-PR check-runs listing for ci start/complete

case "$1" in
  pr)
    shift
    if [ "${1:-}" = "list" ]; then
      cat <<'JSON'
[
  {"number":101,"createdAt":"2026-06-01T10:00:00Z","mergedAt":"2026-06-01T10:30:00Z","title":"[FEAT] one","body":"x\nReviewer-agent-id: ax0123\nReviewer-recommendation: APPROVE\n"},
  {"number":102,"createdAt":"2026-06-02T10:00:00Z","mergedAt":"2026-06-02T11:00:00Z","title":"[FIX] two","body":"y\nReviewer-agent-id: ax0124\nReviewer-recommendation: APPROVE\n"}
]
JSON
      exit 0
    fi
    if [ "${1:-}" = "view" ]; then
      shift
      pr="$1"
      case "$pr" in
        101)
          cat <<'JSON'
{
  "number":101,
  "createdAt":"2026-06-01T10:00:00Z",
  "mergedAt":"2026-06-01T10:30:00Z",
  "title":"[FEAT] one",
  "body":"x\nReviewer-agent-id: ax0123\nReviewer-recommendation: APPROVE\n",
  "commits":[
    {"committedDate":"2026-06-01T10:02:00Z","oid":"sha101a"},
    {"committedDate":"2026-06-01T10:05:00Z","oid":"sha101b"}
  ],
  "reviews":[],
  "comments":[],
  "headRefOid":"sha101b"
}
JSON
          exit 0 ;;
        102)
          cat <<'JSON'
{
  "number":102,
  "createdAt":"2026-06-02T10:00:00Z",
  "mergedAt":"2026-06-02T11:00:00Z",
  "title":"[FIX] two",
  "body":"y\nReviewer-agent-id: ax0124\nReviewer-recommendation: APPROVE\n",
  "commits":[
    {"committedDate":"2026-06-02T10:03:00Z","oid":"sha102a"}
  ],
  "reviews":[],
  "comments":[],
  "headRefOid":"sha102a"
}
JSON
          exit 0 ;;
      esac
    fi
    ;;
  api)
    # gh api repos/.../commits/<sha>/check-runs?per_page=100
    # Return a check_runs array with started_at + completed_at.
    case "$*" in
      *sha101b*)
        cat <<'JSON'
{"check_runs":[{"name":"verify","started_at":"2026-06-01T10:06:00Z","completed_at":"2026-06-01T10:20:00Z","conclusion":"success"}]}
JSON
        exit 0 ;;
      *sha102a*)
        cat <<'JSON'
{"check_runs":[{"name":"verify","started_at":"2026-06-02T10:04:00Z","completed_at":"2026-06-02T10:50:00Z","conclusion":"success"}]}
JSON
        exit 0 ;;
    esac
    echo "{}"
    exit 0
    ;;
esac
echo "stub-gh: unexpected args: $*" >&2
exit 99
STUB
chmod +x "$stub_dir/gh"

out=$(PATH="$stub_dir:$PATH" "$target" --last 2 2>&1) && rc=0 || rc=$?
if [ "$rc" -ne 0 ]; then
  echo "FAIL fixture-exit-zero: got $rc"
  failed=$((failed + 1))
  echo "  output:"
  printf '%s\n' "$out" | sed 's/^/    /'
else
  pass=$((pass + 1))
  echo "PASS fixture-exit-zero"
fi
assert_contains "fixture-row-101"     "$out" "101"
assert_contains "fixture-row-102"     "$out" "102"
assert_contains "fixture-header-pr"   "$out" "PR"
assert_contains "fixture-aggregate-median" "$out" "median"
assert_contains "fixture-aggregate-p95"    "$out" "p95"

echo
echo "measure-pr-cycle smoke: $pass passed, $failed failed"
exit "$failed"
