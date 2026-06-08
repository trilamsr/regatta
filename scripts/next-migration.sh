#!/usr/bin/env bash
# next-migration.sh — print the next free SQLite migration number,
# zero-padded to 4 digits. Dispatch prompts call this via `make next-migration`
# so the implementer never picks the number (avoids duplicate-version panic).
set -uo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/.." && pwd)

: "${MIGRATIONS_DIR:=$REPO_ROOT/internal/orchestrator/state/migrations}"

nmax=$(find "$MIGRATIONS_DIR" -maxdepth 1 -type f -name '[0-9][0-9][0-9][0-9]_*.sql' \
  | sed -E 's|.*/([0-9]{4})_.*|\1|' \
  | sort -n \
  | tail -1)

if [ -z "$nmax" ]; then
  printf '0001\n'
  exit 0
fi

printf '%04d\n' $(( 10#$nmax + 1 ))
