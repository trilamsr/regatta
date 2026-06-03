# Native deploy runbook

Reader: operator running Regatta directly on a Linux server or macOS
laptop — no Docker daemon involved.
Read time: 8 minutes.
Expires when: the systemd unit, LaunchAgent plist, or installer scripts
under `deploy/` change.

## When to use this path

| Path | Use when |
|---|---|
| Native (this doc) | self-hosted on a Linux server you own end-to-end, OR a developer macOS laptop where Docker overhead is wasted on a single-process binary. |
| [`container.md`](container.md) | the host already runs other Docker workloads, OR you want Stage 1's pre-baked Claude Code + `gh` toolchain. |
| Kubernetes (deferred) | multi-tenant or fleet-managed; Stage 3 follow-up — not on the Phase-S path. |

Native is the lowest-overhead route: one binary, one supervisor, journald
or `tail -F` for logs. Containers add an extra fs layer + an ENTRYPOINT
indirection that buys nothing on a single-host install.

## Linux install (systemd)

### Prereqs

- systemd >= 245 (`ProtectKernelLogs` lands here).
- `regatta` binary built locally or downloaded from a release tarball.
- root access on the target host.

### Stage the binary

```sh
sudo install -m 0755 -o root -g root \
  ./regatta /usr/local/bin/regatta
/usr/local/bin/regatta version
```

### Run the installer

```sh
sudo deploy/install-systemd.sh
```

The script (see [`../../deploy/install-systemd.sh`](../../deploy/install-systemd.sh)):

1. Creates the `regatta` system user + group (UID < 1000, no shell).
2. Stages `/var/lib/regatta` (state) + `/var/log/regatta` (crash sink),
   both `0750 regatta:regatta`.
3. Writes a config stub at `/etc/regatta/regatta.yaml` (skipped if
   present so re-running never clobbers operator edits).
4. Writes a secrets stub at `/etc/regatta/env` (`0640 root:regatta`).
5. Copies the unit file to `/etc/systemd/system/regatta.service`.
6. `systemctl daemon-reload && enable && restart`.

### Fill secrets + restart

```sh
sudoedit /etc/regatta/env       # ANTHROPIC_API_KEY, GH_TOKEN
sudo systemctl restart regatta
sudo systemctl status regatta
```

### Verify health

```sh
curl -fsS http://127.0.0.1:8080/healthz   # expect: ok
journalctl -u regatta -f                  # live tail
```

The `/healthz` endpoint is owned by W7.0 — see
[`../../cmd/regatta/serve.go`](../../cmd/regatta/serve.go) line 643. It
issues zero DB queries and returns `200 OK` + body `ok`, so it is safe
to point an external uptime probe at it.

### Unit hardening (what's enforced)

| Directive | Why |
|---|---|
| `User=regatta`, `Group=regatta` | non-root execution; system UID. |
| `ProtectSystem=strict` | `/usr`, `/boot`, `/efi` mounted read-only. |
| `ProtectHome=true` | `/home`, `/root`, `/run/user` blanked. |
| `PrivateTmp=true` | private `/tmp` + `/var/tmp` namespace. |
| `NoNewPrivileges=true` | setuid binaries cannot elevate. |
| `ReadWritePaths=/var/lib/regatta /var/log/regatta` | only carved exceptions. |
| `MemoryHigh=2G` / `MemoryMax=4G` | cgroup soft + hard caps. |
| `LimitNOFILE=65536` | sqlite + HTTP fan-out file-descriptor budget. |
| `Restart=on-failure`, `RestartSec=5` | crash-loop with 5 s backoff. |

The unit ships `Type=simple` today; upgrading the binary to call
`sd_notify(READY=1)` (research brief §11, `coreos/go-systemd/v22/daemon`)
is filed as a load-bearing follow-up issue — switch to `Type=notify`
once that lands.

### Validate the unit file

On a Linux host with systemd:

```sh
systemd-analyze verify deploy/systemd/regatta.service
systemd-analyze security regatta.service
```

`systemd-analyze` is not available on macOS, so unit changes must round-trip
through a Linux VM (or the CI runner) before landing.

### Log rotation

`journalctl -u regatta` is the primary log surface — rotation is handled
by `systemd-journald` per `/etc/systemd/journald.conf`. Defaults
(10% of /var/log, 4 GiB cap) are sane for most operators. To override:

```ini
# /etc/systemd/journald.conf.d/regatta.conf
[Journal]
SystemMaxUse=2G
SystemMaxFileSize=128M
MaxRetentionSec=30day
```

Then `sudo systemctl restart systemd-journald`.

The unit also writes crash dumps to `/var/log/regatta/` — rotate with
logrotate if that directory grows:

```
# /etc/logrotate.d/regatta
/var/log/regatta/*.log {
  weekly
  rotate 8
  compress
  delaycompress
  missingok
  notifempty
  copytruncate
}
```

### Uninstall

```sh
sudo systemctl disable --now regatta
sudo rm /etc/systemd/system/regatta.service
sudo systemctl daemon-reload
sudo rm -rf /etc/regatta /var/log/regatta
# Keep /var/lib/regatta if you want to preserve sqlite state across re-install.
sudo userdel regatta
sudo groupdel regatta
```

## macOS install (LaunchAgent)

### Prereqs

- macOS 12+ (`launchctl bootstrap` semantics).
- `regatta` binary built locally (`go build ./cmd/regatta`) or installed
  via release tarball.
- A working tree clone (the agent runs `regatta serve --repo $REGATTA_REPO`).
- `claude` CLI on `PATH` — `npm install -g @anthropic-ai/claude-code@2.1.161`.

The agent is **per-user** (`~/Library/LaunchAgents/`), not a system-wide
LaunchDaemon. Tokens live in the user's keychain; running as root would
defeat that threat model.

### Stage the binary

```sh
sudo install -m 0755 -o root -g wheel \
  $(go env GOPATH)/bin/regatta /usr/local/bin/regatta
/usr/local/bin/regatta version
```

### Stash secrets in Keychain

```sh
security add-generic-password -a "$USER" -s regatta/anthropic_api_key -w
security add-generic-password -a "$USER" -s regatta/gh_token -w
```

The `-w` flag prompts for the secret value on stdin; the secret never
hits shell history or process args.

### macOS keychain wrapper

The shipped plist invokes `/usr/local/bin/regatta` directly — it does
not know about the keychain. The repo ships a wrapper at
[`../../deploy/launchd/regatta-serve.sh`](../../deploy/launchd/regatta-serve.sh)
that reads the two keychain entries (`regatta/anthropic_api_key`,
`regatta/gh_token`) and exports them before `exec regatta "$@"`.

Stage it and point the plist at it:

```sh
sudo install -m 0755 -o root -g wheel \
  deploy/launchd/regatta-serve.sh /usr/local/bin/regatta-serve

sed -i '' 's|/usr/local/bin/regatta|/usr/local/bin/regatta-serve|' \
  "$HOME/Library/LaunchAgents/com.regatta.serve.plist"

launchctl bootout "gui/$(id -u)/com.regatta.serve"
launchctl bootstrap "gui/$(id -u)" \
  "$HOME/Library/LaunchAgents/com.regatta.serve.plist"
```

The wrapper fails fast with a named-entry error if either keychain item
is missing — silent empty-credential crash-loop is a footgun the wrapper
removes.

### Run the installer

```sh
REGATTA_REPO="$HOME/code/regatta" deploy/install-launchd.sh
```

The script (see [`../../deploy/install-launchd.sh`](../../deploy/install-launchd.sh)):

1. Templates `REGATTA_REPO_PATH`, `REGATTA_LOG_DIR`, `REGATTA_HOME` into
   the plist (launchd does not substitute shell vars inside `<string>`).
2. Runs `plutil -lint` on the rendered plist before installing.
3. `launchctl bootout` any existing instance, then `bootstrap` + `kickstart`.

### Verify health

```sh
launchctl print "gui/$(id -u)/com.regatta.serve" | head -40
tail -F "$HOME/Library/Logs/regatta/stdout.log"
curl -fsS http://127.0.0.1:8080/healthz
```

### Log rotation

macOS ships `newsyslog` (cron-driven) for arbitrary log files. Drop a
config at `/etc/newsyslog.d/regatta.conf`:

```
# logfilename                                       [owner:group]    mode count size when  flags
/Users/<you>/Library/Logs/regatta/stdout.log        <you>:staff      644  7     5000 *     GZ
/Users/<you>/Library/Logs/regatta/stderr.log        <you>:staff      644  7     5000 *     GZ
```

Replace `<you>` with `whoami`. Force a rotation pass with
`sudo newsyslog -nvv` (dry-run) then `sudo newsyslog -F`.

### Uninstall

```sh
launchctl bootout "gui/$(id -u)/com.regatta.serve"
rm "$HOME/Library/LaunchAgents/com.regatta.serve.plist"
security delete-generic-password -a "$USER" -s regatta/anthropic_api_key
security delete-generic-password -a "$USER" -s regatta/gh_token
rm -rf "$HOME/Library/Logs/regatta"
```

## Troubleshooting

### `regatta` exits immediately, no log

Check the unit's effective env:

- Linux: `systemctl show regatta -p Environment` and
  `sudo cat /etc/regatta/env`. A missing `ANTHROPIC_API_KEY` makes the
  L4 adapter fail-fast on first plan.
- macOS: `launchctl print gui/$(id -u)/com.regatta.serve` and grep
  `environment`; if you wired the keychain wrapper, run it standalone
  (`/usr/local/bin/regatta-serve --help`) to confirm `security` returns
  a value.

### `claude: command not found` from the spawned planner

The agent inherits `PATH` from the plist (or unit). On macOS, the plist
lists `/usr/local/bin:/usr/bin:/bin:/opt/homebrew/bin` — if `claude` is
installed elsewhere (e.g. an `nvm`-managed prefix), either symlink it
into `/usr/local/bin` or append the nvm bin to the plist's `PATH`
entry and re-run `install-launchd.sh`.

### `GH_TOKEN` rejected ("Bad credentials")

Tokens issued via `gh auth token` are short-lived if the GitHub org
enforces SSO. Re-mint with `gh auth refresh -h github.com -s repo,workflow`
and re-stash via `security add-generic-password -U …` (the `-U` flag
overwrites the existing keychain entry).

### Network outage recovery

systemd's `Restart=on-failure` + `RestartSec=5` will re-attempt the
process every 5 s on crash. The unit also depends on
`network-online.target`, so a transient interface flap during boot
delays start but does not fail. On macOS the LaunchAgent uses
`KeepAlive.NetworkState=true` — launchd re-launches when the network
returns. If neither relaunches after a multi-hour outage:

```sh
# Linux
sudo systemctl status regatta              # confirm Restart counter
sudo journalctl -u regatta --since "1h ago"

# macOS
launchctl print "gui/$(id -u)/com.regatta.serve" | grep -E "state|last exit"
```

### Health probe returns connection refused

The HTTP listener defaults to `:8080` and binds when `--ui=true`
(default). Confirm:

- the `regatta serve` flag list in
  [`../../cmd/regatta/serve.go`](../../cmd/regatta/serve.go) for the
  current listener defaults,
- nothing else is bound on 8080 (`sudo lsof -i :8080`),
- the unit has not been started with `--ui=false`.

## Related

- [`container.md`](container.md) — Docker path (Stage 1 runtime image).
- [`getting-started.md`](getting-started.md) — first 15-minute walkthrough.
- [`configure.md`](configure.md) — `regatta.yaml` schema.
- [`../engineer/research/2026-06-02-container-stages.md`](../engineer/research/2026-06-02-container-stages.md) — research brief (§11 systemd, §12 launchd).
