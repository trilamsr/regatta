#!/usr/bin/env bash
#
# macOS installer for the regatta LaunchAgent.
#
# Per-user agent — runs at login, restarts on crash, throttled at 30s.
# Tokens NEVER live in the plist; the wrapper reads them from the
# user's keychain at exec time.
#
# Layout:
#   /usr/local/bin/regatta                            binary (caller stages this)
#   ~/Library/LaunchAgents/com.regatta.serve.plist    agent definition
#   ~/Library/Logs/regatta/{stdout,stderr}.log        log sinks
#   ~/code/regatta (or REGATTA_REPO)                  working tree

set -euo pipefail

REGATTA_REPO="${REGATTA_REPO:-${HOME}/code/regatta}"
REGATTA_LOG_DIR="${REGATTA_LOG_DIR:-${HOME}/Library/Logs/regatta}"
PLIST_NAME="com.regatta.serve"
PLIST_TARGET="${HOME}/Library/LaunchAgents/${PLIST_NAME}.plist"

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
SRC_PLIST="${SCRIPT_DIR}/launchd/${PLIST_NAME}.plist"

if [ "$(id -u)" -eq 0 ]; then
  echo "install-launchd.sh: run as your normal user, not root — this is a per-user LaunchAgent" >&2
  exit 1
fi

if [ ! -f "${SRC_PLIST}" ]; then
  echo "install-launchd.sh: plist template not found at ${SRC_PLIST}" >&2
  exit 1
fi

if [ ! -x /usr/local/bin/regatta ]; then
  echo "install-launchd.sh: /usr/local/bin/regatta missing or non-exec — stage the binary first" >&2
  exit 1
fi

if [ ! -d "${REGATTA_REPO}" ]; then
  echo "install-launchd.sh: REGATTA_REPO=${REGATTA_REPO} does not exist; set REGATTA_REPO=/path/to/repo and re-run" >&2
  exit 1
fi

mkdir -p "${REGATTA_LOG_DIR}"
mkdir -p "$(dirname "${PLIST_TARGET}")"

# Templating: launchd does not substitute $HOME / shell vars inside
# <string>; the installer rewrites the three placeholders in-place.
# Done via two-phase write so the target is atomic if sed errors.
TMP_PLIST="$(mktemp -t regatta-plist)"
trap 'rm -f "${TMP_PLIST}"' EXIT

sed \
  -e "s|REGATTA_REPO_PATH|${REGATTA_REPO}|g" \
  -e "s|REGATTA_LOG_DIR|${REGATTA_LOG_DIR}|g" \
  -e "s|REGATTA_HOME|${HOME}|g" \
  "${SRC_PLIST}" >"${TMP_PLIST}"

# Validate before installing — a malformed plist would make launchctl
# silently ignore the agent.
if ! plutil -lint "${TMP_PLIST}" >/dev/null; then
  plutil -lint "${TMP_PLIST}"
  echo "install-launchd.sh: rendered plist failed plutil -lint" >&2
  exit 1
fi

mv "${TMP_PLIST}" "${PLIST_TARGET}"
trap - EXIT

# bootout-then-bootstrap on re-install so the new plist content is
# loaded; launchctl load is silently a no-op for an already-loaded
# agent on modern macOS.
if launchctl print "gui/$(id -u)/${PLIST_NAME}" >/dev/null 2>&1; then
  launchctl bootout "gui/$(id -u)/${PLIST_NAME}" || true
fi
launchctl bootstrap "gui/$(id -u)" "${PLIST_TARGET}"
launchctl enable "gui/$(id -u)/${PLIST_NAME}"
launchctl kickstart -k "gui/$(id -u)/${PLIST_NAME}"

cat <<EOF

regatta LaunchAgent installed.

Next steps:
  1. Stash secrets in Keychain (one-time):
       security add-generic-password -a "\$USER" -s regatta/anthropic_api_key -w
       security add-generic-password -a "\$USER" -s regatta/gh_token         -w
  2. Wrap regatta to read keychain at exec (see docs/operator/native-deploy.md
     §"macOS keychain wrapper" — the plist invokes /usr/local/bin/regatta
     directly today; replace with the wrapper to inject tokens).
  3. Tail logs:   tail -F ${REGATTA_LOG_DIR}/stdout.log
  4. Health:      curl -fsS http://127.0.0.1:8080/healthz
  5. Status:      launchctl print gui/\$(id -u)/${PLIST_NAME}

Uninstall: launchctl bootout gui/\$(id -u)/${PLIST_NAME} && rm ${PLIST_TARGET}
EOF
