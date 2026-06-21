#!/usr/bin/env bash
# regen-go-shards.sh - regenerate scripts/go-shards/shard-{1..4}.txt and
# scripts/race-pkgs.txt by greedy bin-pack over per-package historical
# test time from a recent main verify-go run.
#
# When to run:
# - check-go-shard-coverage fires with "Missing from shards" (new package).
# - One shard wall-clock drifts >2x another (rebalance after big test moves).
# - Race-pkg classification needs refresh (new sync/chan in prod code).
#
# Usage:
#   ./scripts/regen-go-shards.sh                # uses latest main CI run
#   RUN_ID=12345 ./scripts/regen-go-shards.sh   # specific run
#
# Requires: gh CLI authenticated, python3.
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/.." && pwd)
cd "$REPO_ROOT"

if [ -z "${RUN_ID:-}" ]; then
  RUN_ID=$(gh run list --workflow=CI --branch=main --status=success \
    --json databaseId -L 1 --jq '.[0].databaseId')
fi
echo "regen-go-shards: using run $RUN_ID"

JOB_ID=$(gh run view "$RUN_ID" --json jobs \
  --jq '.jobs[] | select(.name == "verify-go") | .databaseId')
LOG=$(mktemp)
trap 'rm -f "$LOG"' EXIT

gh run view "$RUN_ID" --log --job="$JOB_ID" 2>&1 \
  | grep -E "ok\s+github.com" > "$LOG"

# Race classification: scan prod-only go files for concurrency primitives.
RACE_LIST=$(mktemp)
trap 'rm -f "$LOG" "$RACE_LIST"' EXIT
for p in $(go list ./...); do
  dir=$(go list -f '{{.Dir}}' "$p")
  has=0
  for f in "$dir"/*.go; do
    [ -f "$f" ] || continue
    case "$f" in *_test.go) continue;; esac
    if grep -qE 'sync\.(Mutex|RWMutex|Once|WaitGroup|Map|Pool|Cond|Locker)|make\(chan|<-chan|chan<-| chan |^chan |go func|^[[:space:]]+go [a-zA-Z_]|atomic\.(Add|Store|Load|Swap|CompareAnd|Bool|Int|Uint|Pointer)' "$f" 2>/dev/null; then
      has=1
      break
    fi
  done
  [ "$has" = "1" ] && echo "$p" >> "$RACE_LIST"
done
sort -u "$RACE_LIST" -o "$RACE_LIST"

python3 - "$LOG" "$RACE_LIST" <<'PY'
import sys
log_path, race_path = sys.argv[1], sys.argv[2]
times = {}
with open(log_path) as f:
    for line in f:
        for tok in line.split():
            if tok.startswith("github.com/"):
                idx = line.split().index(tok)
                parts = line.split()
                if idx + 1 < len(parts):
                    t = parts[idx + 1].rstrip("s")
                    try:
                        times[tok] = float(t)
                    except ValueError:
                        pass
                break

import subprocess
all_pkgs = subprocess.check_output(["go", "list", "./..."], text=True).strip().split("\n")
for p in all_pkgs:
    if p not in times:
        times[p] = 1.0  # no-test-files default

ordered = sorted(times.items(), key=lambda kv: -kv[1])
shards = [[], [], [], []]
totals = [0.0, 0.0, 0.0, 0.0]
for p, t in ordered:
    i = totals.index(min(totals))
    shards[i].append(p)
    totals[i] += t

for i, s in enumerate(shards, start=1):
    path = f"scripts/go-shards/shard-{i}.txt"
    with open(path, "w") as f:
        f.write(f"# verify-go-{i}: {len(s)} packages, ~{totals[i-1]:.0f}s historical CPU-time.\n")
        f.write("# Greedy bin-packed by last-known per-pkg test times to balance wall-clock.\n")
        f.write("# Regenerate via scripts/regen-go-shards.sh (see docs/engineer/pointers.md).\n\n")
        for p in sorted(s):
            f.write(p + "\n")
    print(f"wrote {path} ({len(s)} pkgs, {totals[i-1]:.0f}s)")

with open("scripts/race-pkgs.txt", "r") as f:
    header = []
    for line in f:
        if line.startswith("#") or line.strip() == "":
            header.append(line)
        else:
            break

with open(race_path) as f:
    race_pkgs = sorted(set(l.strip() for l in f if l.strip()))
with open("scripts/race-pkgs.txt", "w") as f:
    f.writelines(header)
    for p in race_pkgs:
        f.write(p + "\n")
print(f"wrote scripts/race-pkgs.txt ({len(race_pkgs)} pkgs)")
PY

bash scripts/check-go-shard-coverage.sh
echo "regen-go-shards: done"
