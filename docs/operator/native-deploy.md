# Native deploy runbook

Reader: operator running Regatta directly on a Linux server or macOS
laptop — no Docker daemon involved.
Read time: 5 minutes.
Expires when: the `regatta install-service` command, the systemd unit
template, or the launchd plist template under `dist/services/` change.

## When to use this path

| Path | Use when |
|---|---|
| Native (this doc) | self-hosted on a Linux server you own end-to-end, OR a developer macOS laptop where Docker overhead is wasted on a single-process binary. |
| [`container.md`](container.md) | the host already runs other Docker workloads, OR you want Stage 1's pre-baked Claude Code + `gh` toolchain. |
| Kubernetes (deferred) | multi-tenant or fleet-managed; Stage 3 follow-up — not on the Phase-S path. |

Native is the lowest-overhead route: one binary, one supervisor, journald
or `tail -F` for logs.

## One-command install

The supervisor wedge (PHASE-AUTONOMY-W3) ships `regatta install-service`
which detects the OS, renders the correct unit / plist, bootstraps the
OS init system, and polls `/healthz` until the loop is up — operator
runs one command, never `systemctl daemon-reload` or `launchctl
bootstrap` by hand:

```sh
# Default: per-user install (no sudo required on macOS).
regatta install-service

# System install (Linux server; requires root).
sudo regatta install-service --system

# Preview without mutating the filesystem.
regatta install-service --dry-run

# Re-run upgrades the unit when the rendered template changes.
regatta install-service --force
```

The command is idempotent:

- Identical rendered unit ⇒ `already installed` + exit 0.
- Differing unit + `--force` ⇒ a `.bak` snapshot is taken, then re-applied.
- Differing unit, no `--force` ⇒ refuses with a named error pointing at
  `--force` (protects hand-edits).

`regatta uninstall-service` reverses the install. Re-run on a clean host
is a no-op (`INFO: nothing to remove`).

## Linux install (systemd)

### Prereqs

- systemd >= 245 (`ProtectKernelLogs` lands here).
- `regatta` binary on `$PATH`.
- root access for `--system` mode (the unit lands at
  `/etc/systemd/system/regatta.service`).

### Install

```sh
sudo install -m 0755 -o root -g root ./regatta /usr/local/bin/regatta
sudo regatta install-service --system
```

The command:

1. Renders `dist/services/regatta.service.tmpl` substituting the
   resolved binary path, working directory, env-file, and user.
2. Validates the rendered unit with `systemd-analyze verify` (warns and
   falls back to a built-in text-schema check when `systemd-analyze` is
   not on `$PATH`).
3. Writes `/etc/systemd/system/regatta.service` atomically.
4. Installs the cron block under the operator's crontab (digest, items
   refresh, followups triage). Pass `--no-cron` to opt out.
5. Detects SELinux enforcing → emits an `audit2allow` hint (instruction
   only; install proceeds — generating the policy requires an AVC trace
   that only exists after a failed start).

### Verify

```sh
curl -fsS -H 'Accept: application/json' http://127.0.0.1:8080/healthz
journalctl -u regatta -f
```

The `/healthz` endpoint returns the W3 readiness envelope (`status` +
per-subsystem `checks`) as `application/json` regardless of the request
`Accept` header. 200 on ok or degraded; 503 only when the DB ping fails
AND no recent heartbeat.

### Watchdog

The unit template ships `Type=notify` + `WatchdogSec=30`. The serve
binary's notify goroutine emits `WATCHDOG=1` every 10s (3x safety
factor); `STOPPING=1` is emitted on graceful shutdown so systemd
suppresses the watchdog-restart trigger.

### Uninstall

```sh
sudo regatta uninstall-service --system
```

Reverses the install: unloads the unit (`systemctl disable --now`),
removes the unit file, strips the cron block. State directories are
preserved (`/var/lib/regatta`) so a re-install picks up the existing
SQLite database.

## macOS install (LaunchAgent)

### Prereqs

- macOS 12+ (`launchctl bootstrap` semantics).
- `regatta` binary on `$PATH` (either brew prefix is fine — the install
  command resolves the absolute path from `os.Executable`).

### Install

```sh
regatta install-service
```

The command:

1. Renders `dist/services/regatta.plist.tmpl` with binary path, working
   directory, log dir, and PATH (ordered by the resolved binary's brew
   prefix — `/opt/homebrew/bin` first on Apple Silicon,
   `/usr/local/bin` first on Intel).
2. Validates with `plutil -lint` (warns + falls back to a built-in
   text-schema check when `plutil` is not on `$PATH`).
3. Writes `~/Library/LaunchAgents/com.regatta.serve.plist`.
4. Bootstraps the LaunchAgent under `gui/$UID`.

### Verify

```sh
launchctl print "gui/$(id -u)/com.regatta.serve" | head -40
tail -F "$HOME/Library/Logs/regatta/stderr.log"
curl -fsS -H 'Accept: application/json' http://127.0.0.1:8080/healthz
```

### Log rotation

macOS launchd does not rotate `StandardErrorPath` natively. Drop a
`newsyslog` config at `/etc/newsyslog.d/regatta.conf`:

```
# logfilename                                       [owner:group]    mode count size when  flags
/Users/<you>/Library/Logs/regatta/stdout.log        <you>:staff      644  7     5000 *     GZ
/Users/<you>/Library/Logs/regatta/stderr.log        <you>:staff      644  7     5000 *     GZ
```

### Uninstall

```sh
regatta uninstall-service
```

## Troubleshooting

### Install times out at `/healthz` poll

The supervisor reports `installed but not yet healthy — check stderr
for boot progress` after a 30s window if `status` stays `degraded`.
Tail `stderr.log` (`~/Library/Logs/regatta/stderr.log` on macOS,
`journalctl -u regatta` on Linux) for the boot error. Common causes:

- Missing `ANTHROPIC_API_KEY` / `GH_TOKEN` in the env file.
- SQLite database path not writable (check `WorkingDir` permissions).
- Cold-start brief load slower than 30s — re-run `install-service` once
  the brief is cached.

### `regatta` exits immediately, no log

Check the unit's effective env:

- Linux: `systemctl show regatta -p Environment` and
  `sudo cat /etc/regatta/env`.
- macOS: `launchctl print gui/$(id -u)/com.regatta.serve` and grep
  `environment`.

### Health probe returns connection refused

The HTTP listener defaults to `:8080` and binds when `--ui=true`
(default). Confirm:

- the `regatta serve` flag list for the current listener defaults,
- nothing else is bound on 8080 (`sudo lsof -i :8080`),
- the unit has not been started with `--ui=false`.

## Related

- [`container.md`](container.md) — Docker path (Stage 1 runtime image).
- [`getting-started.md`](getting-started.md) — first 15-minute walkthrough.
- [`configure.md`](configure.md) — `regatta.yaml` schema.
