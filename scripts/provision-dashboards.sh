#!/usr/bin/env bash
# Upsert every dashboard JSON under docs/operator/dashboards/ to a Grafana
# instance via the HTTP API (POST /api/dashboards/db). Idempotent — re-runs
# overwrite via the documented `overwrite: true` payload flag.
#
# Operator UX (#523): expose the automated path the §3 observability doc
# claims exists. UX > velocity per CLAUDE.md decision priority.
#
# Required env:
#   GRAFANA_URL        Base URL of the Grafana instance (no trailing slash).
#   GRAFANA_API_TOKEN  Service-account token with dashboard write scope.
# Optional env:
#   DASHBOARDS_DIR     Directory to scan (default: docs/operator/dashboards).
#   GRAFANA_FOLDER_ID  Numeric folder id to upsert into (default: 0 = General).

set -euo pipefail

if [[ -z "${GRAFANA_URL:-}" ]]; then
	echo "provision-dashboards: GRAFANA_URL must be set (e.g. http://localhost:3000)" >&2
	exit 2
fi
if [[ -z "${GRAFANA_API_TOKEN:-}" ]]; then
	echo "provision-dashboards: GRAFANA_API_TOKEN must be set (service-account dashboard:write)" >&2
	exit 2
fi

DASHBOARDS_DIR="${DASHBOARDS_DIR:-docs/operator/dashboards}"
GRAFANA_FOLDER_ID="${GRAFANA_FOLDER_ID:-0}"

if [[ ! -d "$DASHBOARDS_DIR" ]]; then
	echo "provision-dashboards: dashboards dir not found: $DASHBOARDS_DIR" >&2
	exit 2
fi

# jq is the smallest dep that turns each dashboard JSON into the API envelope
# without shell-quoting hell. Pin a runnable jq invocation guard up-front so
# the operator sees a clear error instead of a payload that POSTs as text.
if ! command -v jq >/dev/null 2>&1; then
	echo "provision-dashboards: jq is required (brew install jq / apt install jq)" >&2
	exit 2
fi

api_url="${GRAFANA_URL%/}/api/dashboards/db"
shopt -s nullglob
files=("$DASHBOARDS_DIR"/*.json)
if (( ${#files[@]} == 0 )); then
	echo "provision-dashboards: no *.json under $DASHBOARDS_DIR" >&2
	exit 0
fi

rc=0
for f in "${files[@]}"; do
	# Grafana API expects {"dashboard": <obj>, "folderId": N, "overwrite": true}
	# Setting "id": null forces an insert-or-overwrite by uid; the per-file uid
	# already lives inside the dashboard JSON.
	payload=$(jq -c \
		--argjson folder "$GRAFANA_FOLDER_ID" \
		'{dashboard: (.|.id=null), folderId: $folder, overwrite: true}' \
		"$f")

	echo "provision-dashboards: upserting $(basename "$f")"
	http_code=$(curl --silent --show-error \
		--output /tmp/provision-dashboards.last \
		--write-out '%{http_code}' \
		-X POST "$api_url" \
		-H "Authorization: Bearer $GRAFANA_API_TOKEN" \
		-H 'Content-Type: application/json' \
		--data "$payload" || true)
	if [[ "$http_code" != "200" ]]; then
		echo "  FAIL ($http_code): $(cat /tmp/provision-dashboards.last)" >&2
		rc=1
		continue
	fi
done

exit "$rc"
