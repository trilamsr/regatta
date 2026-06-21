#!/usr/bin/env bash
# check-release-notes-local_test.sh - assertions for scripts/check-release-notes-local.sh.
#
# Covers MAY-100 (fence + [CATEGORY] presence in an intended PR body) and
# MAY-73 (Reviewer-recommendation: token misplaced into a COMMIT MESSAGE
# instead of the PR body -> warn pre-push, before CI wastes a cycle).

set -uo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
GATE="$SCRIPT_DIR/check-release-notes-local.sh"

PASS=0
FAIL=0
failed=()

pass() { echo "ok   $1"; PASS=$((PASS + 1)); }
fail() { echo "FAIL $1"; FAIL=$((FAIL + 1)); failed+=("$1"); }

tmproot=$(mktemp -d)
trap 'rm -rf "$tmproot"' EXIT

bodyfile_ok="$tmproot/body_ok.md"
cat >"$bodyfile_ok" <<'EOF'
Some PR description.

```release-notes
[FIX] something got fixed
```
EOF

bodyfile_nofence="$tmproot/body_nofence.md"
cat >"$bodyfile_nofence" <<'EOF'
Some PR description with no release-notes fence at all.
EOF

bodyfile_nocat="$tmproot/body_nocat.md"
cat >"$bodyfile_nocat" <<'EOF'
Description.

```release-notes
no category line here
```
EOF

bash "$GATE" --body-file "$bodyfile_ok" >/dev/null 2>&1 \
  && pass "body with fence + [CATEGORY] passes" \
  || fail "body with fence + [CATEGORY] passes"

bash "$GATE" --body-file "$bodyfile_nofence" >/dev/null 2>&1 \
  && fail "body missing fence is rejected" \
  || pass "body missing fence is rejected"

bash "$GATE" --body-file "$bodyfile_nocat" >/dev/null 2>&1 \
  && fail "body fence missing [CATEGORY] is rejected" \
  || pass "body fence missing [CATEGORY] is rejected"

repo="$tmproot/repo"
mkdir -p "$repo"
(
  cd "$repo"
  git init -q
  git config user.email t@example.com
  git config user.name t
  git config commit.gpgsign false
  echo a >a; git add a; git commit -qm "chore: base"
  base=$(git rev-parse HEAD)
  echo b >b; git add b
  git commit -qm "fix: a clean commit"
  echo c >c; git add c
  # Misplaced token: belongs in PR body, not a commit message.
  git commit -qm "fix: sneaky commit

Reviewer-recommendation: APPROVE"
  echo "$base" >"$tmproot/base_sha"
)
base_sha=$(cat "$tmproot/base_sha")

# Misplaced token in range -> warn (non-zero exit so a hook can surface it).
out=$( cd "$repo" && bash "$GATE" --scan-commits "$base_sha..HEAD" 2>&1 )
rc=$?
if [ "$rc" -ne 0 ] && printf '%s' "$out" | grep -qi 'Reviewer-recommendation'; then
  pass "misplaced Reviewer-recommendation in commit msg is flagged"
else
  fail "misplaced Reviewer-recommendation in commit msg is flagged (rc=$rc out=$out)"
fi

# Clean range (no token): add a child of the tainted commit, then scan only
# from that child to HEAD so the tainted commit is excluded -> exit 0.
( cd "$repo" && echo d >d && git add d && git commit -qm "fix: another clean commit" )
clean_base=$( cd "$repo" && git rev-parse 'HEAD~1' )
out3=$( cd "$repo" && bash "$GATE" --scan-commits "${clean_base}..HEAD" 2>&1 )
rc3=$?
if [ "$rc3" -eq 0 ]; then
  pass "clean commit range passes scan"
else
  fail "clean commit range passes scan (rc=$rc3 out=$out3)"
fi

echo
echo "PASS=$PASS FAIL=$FAIL"
if [ "$FAIL" -ne 0 ]; then
  printf 'failed: %s\n' "${failed[@]}"
  exit 1
fi
