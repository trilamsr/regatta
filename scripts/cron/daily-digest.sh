#!/usr/bin/env bash
# daily-digest.sh - cron wrapper for `regatta digest`.
#
# Cron line (operator local crontab): 0 9 * * * scripts/cron/daily-digest.sh
#
# The script renders yesterday's UTC day (cron at 09:00 local picks up
# the prior 24h cleanly), commits the file on a fresh branch, and opens
# a [DOCS]-prefixed PR via the standard workflow. PR opening is the
# operator's hand-off — auto-merge is intentionally NOT enabled here so
# a human eyeballs the rendered numbers before the file lands on main.
#
# Environment expectations:
#   - $REPO_ROOT defaults to the git toplevel of pwd.
#   - $DIGEST_PROM_URL / $OTEL_EXPORTER_OTLP_METRICS_ENDPOINT /
#     $OTEL_METRICS_PROMETHEUS_PORT — at least one must be set for the
#     digest to fetch real numbers. None set → backend-down banner.
#   - gh CLI authenticated against the origin repo.

set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel)}"
cd "$REPO_ROOT"

# Yesterday in UTC — Linux uses `date -d`, macOS uses `date -v`.
if date -u -d "yesterday" +%F >/dev/null 2>&1; then
  date_yesterday="$(date -u -d 'yesterday' +%F)"
else
  date_yesterday="$(date -u -v-1d +%F)"
fi

echo "daily-digest: rendering $date_yesterday"
go run ./cmd/regatta digest --date "$date_yesterday" --root .

digest_path="docs/digests/${date_yesterday}.md"
if [ ! -s "$digest_path" ]; then
  echo "daily-digest: $digest_path missing or empty after render" >&2
  exit 1
fi

branch="docs/daily-digest-${date_yesterday}"
git fetch origin main --quiet || true
git checkout -B "$branch" origin/main
cp "$digest_path" "/tmp/digest-${date_yesterday}.md"
mkdir -p "$(dirname "$digest_path")"
cp "/tmp/digest-${date_yesterday}.md" "$digest_path"
git add "$digest_path"

if git diff --cached --quiet; then
  echo "daily-digest: no changes to commit"
  exit 0
fi

git commit -m "[DOCS] daily digest for ${date_yesterday}"
git push -u origin "$branch"

body_file="$(mktemp)"
cat > "$body_file" <<EOF
Daily digest rendered by \`scripts/cron/daily-digest.sh\` per spec §6.2.

\`\`\`release-notes
[DOCS] daily-digest: render ${date_yesterday} via regatta digest
\`\`\`
EOF

gh pr create \
  --title "[DOCS] daily digest ${date_yesterday}" \
  --body-file "$body_file" \
  --base main \
  --head "$branch"

rm -f "$body_file"
