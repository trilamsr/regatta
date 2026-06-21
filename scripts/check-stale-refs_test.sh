#!/usr/bin/env bash
# check-stale-refs_test.sh - assertions for scripts/check-stale-refs.sh.
#
# Each case builds a throwaway git repo, simulates a PR diff (base→head),
# and asserts the gate exit + stderr token.

set -uo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
GATE="$SCRIPT_DIR/check-stale-refs.sh"

PASS=0
FAIL=0
failed=()

pass() { echo "ok   $1"; PASS=$((PASS + 1)); }
fail() { echo "FAIL $1"; FAIL=$((FAIL + 1)); failed+=("$1"); }

# Stand up a repo with a base commit, branch, and head commit. The PR
# diff between base and head is what the gate scans. Pass deleted file
# names + remaining ref file content + expected exit.
run_case() {
  local name="$1" deleted_files="$2" remaining_content="$3" body="$4" want_exit="$5"
  local tmp; tmp=$(mktemp -d)
  local got_exit out
  out=$(
    cd "$tmp" || exit 99
    git init -q -b main . 2>/dev/null
    git config user.email t@t.t
    git config user.name t
    # Base: create files to be deleted.
    for f in $deleted_files; do
      mkdir -p "$(dirname "$f")"
      echo "original content of $f" > "$f"
    done
    # Reference file mirrors the optional remaining content (e.g. spec citing the deleted gate).
    if [ -n "$remaining_content" ]; then
      mkdir -p docs
      printf '%s\n' "$remaining_content" > docs/notes.md
    fi
    git add .
    git commit -q -m base 2>/dev/null
    base_sha=$(git rev-parse HEAD)
    git checkout -q -b work
    # Head: delete the files.
    for f in $deleted_files; do
      rm -f "$f"
    done
    git add -A
    if [ -n "$deleted_files" ]; then
      git commit -q -m delete 2>/dev/null
    fi
    BODY="$body" bash "$GATE" --base "$base_sha" --head HEAD 2>&1
  )
  got_exit=$?
  rm -rf "$tmp"

  if [ "$got_exit" -eq "$want_exit" ]; then
    pass "$name (exit=$got_exit)"
  else
    fail "$name: want exit=$want_exit got=$got_exit"
    echo "$out" | sed 's/^/     | /'
  fi
}

# Case 1: deletion + no remaining refs → pass.
run_case "deletion w/o stale refs passes" "old-gate.sh" "" "" 0

# Case 2: deletion + stale ref in another file → fail.
run_case "deletion w/ stale ref fails" "old-gate.sh" "Run \`bash old-gate.sh\` before push." "" 1

# Case 3: escape hatch in PR body bypasses the gate.
run_case "stale-refs-justified bypasses" "old-gate.sh" "Mentions old-gate.sh historically." \
  $'## Summary\n<!-- stale-refs-justified: shipped spec historical accuracy -->\n```release-notes\n[CHORE] x\n```' 0

# Case 4: no deletions in PR → pass.
run_case "no deletions passes" "" "" "" 0

# Case 7 (MAY-269): whitespace-only escape must NOT bypass — gate fails.
run_case "whitespace-only justified rejected" "old-gate.sh" "Run \`bash old-gate.sh\` before push." \
  $'## Summary\n<!-- stale-refs-justified:     -->\n```release-notes\n[CHORE] x\n```' 1

# Case 8 (MAY-269): empty justification must NOT bypass — gate fails.
run_case "empty justified rejected" "old-gate.sh" "Run \`bash old-gate.sh\` before push." \
  $'## Summary\n<!-- stale-refs-justified: -->\n```release-notes\n[CHORE] x\n```' 1

# Like run_case but also seeds a tracked file that SURVIVES into head, plus a
# reference file citing that surviving file's name. Exercises the stem-match
# branch: deleting a suffixed variant (kept_file + suffix) must not flag the
# live refs to kept_file it shares a stem with.
run_case_keep() {
  local name="$1" deleted_files="$2" kept_file="$3" ref_content="$4" want_exit="$5"
  local tmp; tmp=$(mktemp -d)
  local got_exit out
  out=$(
    cd "$tmp" || exit 99
    git init -q -b main . 2>/dev/null
    git config user.email t@t.t
    git config user.name t
    for f in $deleted_files; do
      mkdir -p "$(dirname "$f")"
      echo "original content of $f" > "$f"
    done
    mkdir -p "$(dirname "$kept_file")"
    echo "kept content of $kept_file" > "$kept_file"
    mkdir -p docs
    printf '%s\n' "$ref_content" > docs/notes.md
    git add .
    git commit -q -m base 2>/dev/null
    base_sha=$(git rev-parse HEAD)
    git checkout -q -b work
    for f in $deleted_files; do
      rm -f "$f"
    done
    git add -A
    git commit -q -m delete 2>/dev/null
    bash "$GATE" --base "$base_sha" --head HEAD 2>&1
  )
  got_exit=$?
  rm -rf "$tmp"

  if [ "$got_exit" -eq "$want_exit" ]; then
    pass "$name (exit=$got_exit)"
  else
    fail "$name: want exit=$want_exit got=$got_exit"
    echo "$out" | sed 's/^/     | /'
  fi
}

# Case 5 (MAY-260): deleting a suffixed variant whose stem is a still-tracked
# file must NOT flag the live refs to that file → pass.
run_case_keep "suffixed-variant deletion spares live stem ref" \
  "foo.yaml.bak" "foo.yaml" "See foo.yaml for config." 0

# Case 6 (MAY-260): the legit stem-match still fires — deleting foo.sh whose
# stem `foo` is NOT a tracked file, with a remaining bare `foo` ref → fail.
run_case_keep "non-tracked stem still flags" \
  "foo.sh" "bar.txt" "Invoke foo to run the gate." 1

if [ "$FAIL" -gt 0 ]; then
  echo
  echo "check-stale-refs_test: $FAIL failed, $PASS passed"
  for n in "${failed[@]}"; do echo "  - $n"; done
  exit 1
fi
echo
echo "check-stale-refs_test: all $PASS case(s) passed"
