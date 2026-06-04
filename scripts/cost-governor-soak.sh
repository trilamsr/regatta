#!/usr/bin/env bash
# cost-governor-soak.sh — 7-day emission soak for `budget_reconciled`
# substrate events. Walks the trailing 7×24h buckets ending at "now" and
# fails closed on the FIRST bucket missing at least one row. A bucket
# gap means the cost reconciler has been silent for ≥1 day — operator
# must inspect (cron stalled, Anthropic Cost API outage exceeded
# retry budget, substrate write path broken).
#
# Why: spec #727 requires a 30-day-green emission trail before the
# cost-governor wedge graduates. This is the bucket-level enforcement
# the operator runs in cron.
#
# Usage: REGATTA_DB=/path/to/regatta.db scripts/cost-governor-soak.sh
# Defaults: REGATTA_DB=regatta.db (matches `regatta serve` default).
# Exit: 0 on full 7-day coverage; 1 on first bucket gap (with date on
#       stderr); 2 on DB-access errors.

set -uo pipefail

: "${REGATTA_DB:=regatta.db}"

if [ ! -f "$REGATTA_DB" ]; then
  echo "cost-governor-soak: DB not found at REGATTA_DB=$REGATTA_DB" >&2
  exit 2
fi

if ! command -v sqlite3 >/dev/null 2>&1; then
  echo "cost-governor-soak: sqlite3 not in PATH" >&2
  exit 2
fi

# SOAK_NOW_MS env override pins the "now" timestamp to the test driver's
# clock — single source for both fixture INSERTs and bucket reads,
# defends against an NTP midnight-UTC jump landing between them.
if [ -n "${SOAK_NOW_MS:-}" ]; then
  now_ms=$SOAK_NOW_MS
else
  now_ms=$(( $(date +%s) * 1000 ))
fi
day_ms=$(( 24 * 3600 * 1000 ))

gaps=0
for i in $(seq 0 6); do
  hi=$(( now_ms - i * day_ms ))
  lo=$(( hi - day_ms ))
  printf -v query "SELECT COUNT(*) FROM substrate_events WHERE kind='budget_reconciled' AND written_at > %d AND written_at <= %d;" "$lo" "$hi"
  count=$(sqlite3 "$REGATTA_DB" "$query" 2>/dev/null)
  if [ -z "$count" ]; then
    echo "cost-governor-soak: query failed for bucket ending $(date -u -r $(( hi / 1000 )) +%Y-%m-%dT%H:%MZ)" >&2
    exit 2
  fi
  if [ "$count" -eq 0 ]; then
    bucket_date=$(date -u -r $(( hi / 1000 )) +%Y-%m-%d)
    echo "cost-governor-soak: missing budget_reconciled rows in 24h bucket ending $bucket_date (window: $lo..$hi ms)" >&2
    gaps=$(( gaps + 1 ))
  fi
done

if [ "$gaps" -gt 0 ]; then
  echo "cost-governor-soak: $gaps/7 bucket(s) empty — reconciler emission stalled; inspect cron + Anthropic Cost API health" >&2
  exit 1
fi

echo "cost-governor-soak: 7/7 24h buckets carry budget_reconciled rows"
exit 0
