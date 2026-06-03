#!/usr/bin/env bash
#
# Linux installer for the regatta systemd unit.
#
# Idempotent: re-running upgrades the unit + restarts the service.
# Requires root (writes /etc, /usr/local/bin, /var/lib).
#
# Layout:
#   /usr/local/bin/regatta              the binary (caller stages this)
#   /etc/systemd/system/regatta.service the unit
#   /etc/regatta/regatta.yaml           config
#   /etc/regatta/env                    secrets, 0600 root:regatta
#   /var/lib/regatta/                   state (sqlite + repo bind-target)
#   /var/log/regatta/                   logs (journald primary, this dir for crash dumps)

set -euo pipefail

REGATTA_USER="${REGATTA_USER:-regatta}"
REGATTA_GROUP="${REGATTA_GROUP:-regatta}"
REGATTA_HOME="${REGATTA_HOME:-/var/lib/regatta}"
REGATTA_LOG_DIR="${REGATTA_LOG_DIR:-/var/log/regatta}"
REGATTA_CONF_DIR="${REGATTA_CONF_DIR:-/etc/regatta}"
UNIT_DIR="${UNIT_DIR:-/etc/systemd/system}"

# Resolve repo root from the script's own location so the installer
# works whether invoked from the repo, a tarball extract, or /tmp.
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
SRC_UNIT="${SCRIPT_DIR}/systemd/regatta.service"

if [ "$(id -u)" -ne 0 ]; then
  echo "install-systemd.sh: must run as root (writes /etc + /var)" >&2
  exit 1
fi

if [ ! -f "${SRC_UNIT}" ]; then
  echo "install-systemd.sh: unit file not found at ${SRC_UNIT}" >&2
  exit 1
fi

if [ ! -x /usr/local/bin/regatta ]; then
  echo "install-systemd.sh: /usr/local/bin/regatta missing or non-exec — stage the binary first" >&2
  exit 1
fi

# System user. --system caps the UID below 1000 and skips the home-dir
# create; we hand-stage /var/lib/regatta below so ownership is explicit.
if ! getent group "${REGATTA_GROUP}" >/dev/null; then
  groupadd --system "${REGATTA_GROUP}"
fi
if ! getent passwd "${REGATTA_USER}" >/dev/null; then
  useradd --system --gid "${REGATTA_GROUP}" \
    --home-dir "${REGATTA_HOME}" --no-create-home \
    --shell /usr/sbin/nologin "${REGATTA_USER}"
fi

install -d -m 0750 -o "${REGATTA_USER}" -g "${REGATTA_GROUP}" "${REGATTA_HOME}"
install -d -m 0750 -o "${REGATTA_USER}" -g "${REGATTA_GROUP}" "${REGATTA_LOG_DIR}"
install -d -m 0755 -o root -g root "${REGATTA_CONF_DIR}"

# Config + env stubs only land when absent. Re-running install never
# clobbers operator secrets.
if [ ! -e "${REGATTA_CONF_DIR}/regatta.yaml" ]; then
  cat >"${REGATTA_CONF_DIR}/regatta.yaml" <<'YAML'
# regatta.yaml — operator-edited config.
# Schema reference: docs/operator/configure.md
listener:
  addr: ":8080"
  ui: true
YAML
  chown root:"${REGATTA_GROUP}" "${REGATTA_CONF_DIR}/regatta.yaml"
  chmod 0640 "${REGATTA_CONF_DIR}/regatta.yaml"
fi

if [ ! -e "${REGATTA_CONF_DIR}/env" ]; then
  cat >"${REGATTA_CONF_DIR}/env" <<'ENV'
# Secrets sourced by the systemd unit's EnvironmentFile=.
# Fill in real values, then `systemctl restart regatta`.
ANTHROPIC_API_KEY=
GH_TOKEN=
# REGATTA_BRIEF_HMAC_KEYS=kid:secret
ENV
  chown root:"${REGATTA_GROUP}" "${REGATTA_CONF_DIR}/env"
  chmod 0640 "${REGATTA_CONF_DIR}/env"
fi

install -m 0644 -o root -g root "${SRC_UNIT}" "${UNIT_DIR}/regatta.service"

systemctl daemon-reload
systemctl enable regatta.service
systemctl restart regatta.service

cat <<EOF

regatta systemd unit installed.

Next steps:
  1. Fill secrets:   sudoedit ${REGATTA_CONF_DIR}/env
  2. Edit config:    sudoedit ${REGATTA_CONF_DIR}/regatta.yaml
  3. Restart:        systemctl restart regatta
  4. Tail logs:      journalctl -u regatta -f
  5. Health probe:   curl -fsS http://127.0.0.1:8080/healthz

See docs/operator/native-deploy.md for log rotation + uninstall.
EOF
