# Native deploy runbook

Reader: operator running Regatta directly on a Linux server or macOS
laptop — no Docker daemon involved.
Read time: 10 minutes.
Expires when: the `regatta install-service` command, the systemd unit
template, or the launchd plist template under `dist/services/` change.

## When to use this path

| Path | Use when |
|---|---|
| Native (this doc) | self-hosted on a Linux server you own end-to-end, OR a developer macOS laptop where Docker overhead is wasted on a single-process binary. |
| [`container.md`](container.md) | the host already runs other Docker workloads, OR you want Stage 1's pre-baked Claude Code + `gh` toolchain. |
| Kubernetes (deferred) | multi-tenant or fleet-managed; Stage 3 follow-up — not on the Phase-S path. |

Native is the lowest-overhead route: one binary, one supervisor, one
log stream (journald on Linux, `~/Library/Logs/regatta/stderr.log` on
macOS). The install verb is one command (`regatta install-service`),
but the *deploy journey* is 14 steps — clone, secrets, config, console
cap, branch protection, CI, install, status, smoke, watch — sequenced
below.

## Phase DEPLOY operator flow

| # | Step | Section |
|---|---|---|
| 1 | Clone / pull the repo | [Step 1 — Clone](#step-1--clone) |
| 2 | `.env` fill (`ANTHROPIC_API_KEY` + `GH_TOKEN`) | [Step 2 — `.env`](#step-2--env-fill) |
| 3 | `chmod 600 .env` | [Step 3 — secret mode](#step-3--secret-mode) |
| 4 | Edit `regatta.yaml` (spend cap + alarm-webhook) | [Step 4 — `regatta.yaml`](#step-4--regattayaml) |
| 5 | Anthropic console monthly cap | [Step 5 — console cap](#step-5--anthropic-console-monthly-cap) |
| 6 | Branch protection sanity (`regatta verify-repo-config`) | [Step 6 — branch protection](#step-6--branch-protection-sanity) |
| 7 | `make ci-check` on `main` | [Step 7 — `make ci-check`](#step-7--make-ci-check) |
| 8 | `regatta install-service` | [Step 8 — install](#step-8--install) |
| 9 | Verify launchd / systemd loaded | [Step 9 — verify supervisor](#step-9--verify-supervisor) |
| 10 | `regatta status` panels populate | [Step 10 — status panels](#step-10--regatta-status-panels) |
| 11 | Smoke-dispatch one trivial work_item | [Step 11 — smoke-dispatch](#step-11--smoke-dispatch) |
| 12 | Watch first PR end-to-end | [Step 12 — first-PR walk](#step-12--first-pr-walk) |
| 13 | Verify alarm-webhook trips | [Step 13 — alarm-webhook](#step-13--alarm-webhook-trip) |
| 14 | 24h unattended green criteria | [Step 14 — 24h stop criteria](#step-14--24h-stop-criteria) |

## Step 1 — Clone

```sh
git clone https://github.com/<you>/regatta && cd regatta
```

The daemon reads `regatta.yaml` from `--repo`'s working directory; the
checkout root *is* the deploy root. There is no global config path.

## Step 2 — `.env` fill

```sh
cp .env.example .env
$EDITOR .env  # fill ANTHROPIC_API_KEY + GH_TOKEN
```

The supervisor sources `.env` (default location `$HOME/.config/regatta/env`
for `--user`; `/etc/regatta/env` for `--system`). Move the populated
file into place or pass `--env-file ./.env` to `install-service` (Step
8).

**macOS pre-PR-#830**: the rendered plist injects only `PATH` + `HOME`
and ignores the env file. Confirm the fix has landed before trusting
`.env` injection on macOS:

```sh
git log --oneline --grep='#830' | head -1
```

If the grep returns empty, either rebase onto a post-#830 `main` or
export the two secrets via `launchctl setenv` as a stop-gap:

```sh
launchctl setenv ANTHROPIC_API_KEY "$(grep ANTHROPIC_API_KEY .env | cut -d= -f2-)"
launchctl setenv GH_TOKEN          "$(grep GH_TOKEN .env          | cut -d= -f2-)"
```

`launchctl setenv` persists across the next plist load but does *not*
survive a reboot; re-export on each boot until #830 merges, or pin the
repo to a post-#830 SHA.

## Step 3 — secret mode

```sh
chmod 600 .env
```

PR #830 surfaces a WARN at install time when the mode is looser than
`0600` but does not refuse the install. Fix the mode now; loose modes
leak the Anthropic key to every account on the host.

## Step 4 — `regatta.yaml`

Two fields are operator-set on every fresh install; the rest is
schema-default.

**4a. `safety.spend_cap_usd_per_day`** — soft per-day throttle. The
schema default is `200` (sized for a team); the recommended self-host
default is `20`. Raise only after you have seen a green-clock week at
20.

```yaml
safety:
  spend_cap_usd_per_day: 20  # soft cap; throttles brief.signed dispatch.
```

The cap is a *soft* throttle — it pauses dispatch but does not
bill-block. The hard ceiling is the Anthropic console monthly cap
(Step 5).

**4b. `alarm_webhook`** — local listener that files a GitHub issue on
cost-cap trip or other named alarms.

```yaml
alarm_webhook:
  listen_addr: 127.0.0.1:9101
  gh_repo: <owner>/<name>   # where alarm issues land; typically this repo
  gh_token_env: GH_TOKEN    # env var that holds the PAT
```

Validate before continuing:

```sh
regatta validate-config && echo ok
```

Exit 0 with `ok` printed means the YAML parsed and both required
sections are present.

## Step 5 — Anthropic console monthly cap

Set a monthly USD cap in the [Anthropic console](https://console.anthropic.com/settings/billing).
This is the only *hard* spend ceiling — the in-loop
`spend_cap_usd_per_day` is a soft cap that throttles dispatch but does
not bill-block. Pick a number you can afford to lose; cap-tripped
requests return a 4xx and the loop surfaces them through the
alarm-webhook (Step 13).

## Step 6 — branch protection sanity

```sh
regatta verify-repo-config
```

Expected output: one line per required check + `OK` on each
(`required_status_checks`, `required_pull_request_reviews`,
`enforce_admins`, `restrictions`). Any `MISMATCH` line means the
repository's branch protection drifted from the P2 canonical recipe —
fix the repo settings before installing; the loop relies on these
checks to gate automerge.

## Step 7 — `make ci-check`

```sh
make ci-check
```

Exit 0 confirms the tree is green before you hand it to a loop that
will rebase against `main` on every PR. Non-zero exit means *fix
locally first*; the loop will not push to a red tree but will burn
budget retrying.

## Step 8 — install

The supervisor wedge (PHASE-AUTONOMY-W3) ships `regatta install-service`
which detects the OS, renders the correct unit / plist, bootstraps the
OS init system, and polls `/healthz` until the loop is up.

```sh
# Default: per-user install (no sudo required on macOS).
regatta install-service --env-file ~/.config/regatta/env

# System install (Linux server; requires root).
sudo regatta install-service --system --env-file /etc/regatta/env

# Preview without mutating the filesystem.
regatta install-service --dry-run

# Re-run upgrades the unit when the rendered template changes.
regatta install-service --force
```

`--env-file` is required post-PR-#830 — it threads the secret file
into the rendered plist's `set -a; . <file>; set +a; exec` wrapper. On
Linux the same path goes into `EnvironmentFile=` in the systemd unit.

The command is idempotent:

- Identical rendered unit → `already installed` + exit 0.
- Differing unit + `--force` → a `.bak` snapshot is taken, then re-applied.
- Differing unit, no `--force` → refuses with a named error pointing
  at `--force` (protects hand-edits).

`regatta uninstall-service` reverses the install. Re-run on a clean
host is a no-op (`INFO: nothing to remove`). See [Rollback escape
hatches](#rollback-escape-hatches) for stuck-unit recovery.

### Linux specifics (systemd)

Prereqs:

- systemd >= 245 (`ProtectKernelLogs` lands here).
- `regatta` binary on `$PATH`.
- root access for `--system` mode (the unit lands at
  `/etc/systemd/system/regatta.service`).

```sh
sudo install -m 0755 -o root -g root ./regatta /usr/local/bin/regatta
sudo regatta install-service --system --env-file /etc/regatta/env
```

The install path:

1. Renders `dist/services/regatta.service.tmpl` substituting the
   resolved binary path, working directory, env-file, and user.
2. Validates with `systemd-analyze verify` (warns and falls back to a
   built-in text-schema check when `systemd-analyze` is not on `$PATH`).
3. Writes `/etc/systemd/system/regatta.service` atomically.
4. Installs the cron block under the operator's crontab (digest, items
   refresh, followups triage). Pass `--no-cron` to opt out.
5. Detects SELinux enforcing → emits an `audit2allow` hint (instruction
   only; install proceeds — generating the policy requires an AVC trace
   that only exists after a failed start).

### macOS specifics (LaunchAgent)

Prereqs:

- macOS 12+ (`launchctl bootstrap` semantics).
- `regatta` binary on `$PATH` (either brew prefix is fine — the install
  command resolves the absolute path from `os.Executable`).

### Env file

launchd has no native `EnvironmentFile=` equivalent, so the rendered
plist wraps `regatta serve` in `/bin/sh -lc 'set -a; . "<env-file>";
set +a; exec ...'` — every restart re-sources the file so rotating
`ANTHROPIC_API_KEY` is a single `chmod 600` edit, no plist re-render.

Default paths:

- `--user` install (default): `$HOME/.config/regatta/env`.
- `--system` install: `/etc/regatta/env`.
- `--env-file <path>` overrides both.

Bootstrap the file before the first install:

```sh
mkdir -p "$HOME/.config/regatta"
cat > "$HOME/.config/regatta/env" <<'EOF'
ANTHROPIC_API_KEY=sk-ant-...
GH_TOKEN=ghp_...
EOF
chmod 600 "$HOME/.config/regatta/env"
```

The installer WARNs (not fails) when the env-file is missing or its
mode is not `0600` so a first-time install on a fresh laptop is not
blocked — the operator may also set env via the launchd parent
environment. `KeepAlive` restarts the agent on `.` source failure, so
a missing file surfaces as a tight restart loop in `stderr.log`.

The install path:

1. Renders `dist/services/regatta.plist.tmpl` with binary path, working
   directory, log dir, env-file path, and PATH (ordered by the
   resolved binary's brew prefix — `/opt/homebrew/bin` first on Apple
   Silicon, `/usr/local/bin` first on Intel).
2. Validates with `plutil -lint` (warns + falls back to a built-in
   text-schema check when `plutil` is not on `$PATH`).
3. Writes `~/Library/LaunchAgents/com.regatta.serve.plist`.
4. Bootstraps the LaunchAgent under `gui/$UID`.

### Watchdog

The unit template ships `Type=notify` + `WatchdogSec=30`. The serve
binary's notify goroutine emits `WATCHDOG=1` every 10s (3x safety
factor); `STOPPING=1` is emitted on graceful shutdown so systemd
suppresses the watchdog-restart trigger.

### Log rotation (macOS)

macOS launchd does not rotate `StandardErrorPath` natively. Drop a
`newsyslog` config at `/etc/newsyslog.d/regatta.conf`:

```
# logfilename                                       [owner:group]    mode count size when  flags
/Users/<you>/Library/Logs/regatta/stdout.log        <you>:staff      644  7     5000 *     GZ
/Users/<you>/Library/Logs/regatta/stderr.log        <you>:staff      644  7     5000 *     GZ
```

## Step 9 — verify supervisor

```sh
# Linux
curl -fsS -H 'Accept: application/json' http://127.0.0.1:8080/healthz
journalctl -u regatta -f

# macOS
launchctl print "gui/$(id -u)/com.regatta.serve" | head -40
tail -F "$HOME/Library/Logs/regatta/stderr.log"
curl -fsS -H 'Accept: application/json' http://127.0.0.1:8080/healthz
```

The `/healthz` endpoint returns the W3 readiness envelope (`status` +
per-subsystem `checks`) as `application/json` regardless of the request
`Accept` header. 200 on `ok` or `degraded`; 503 only when the DB ping
fails AND no recent heartbeat.

## Step 10 — `regatta status` panels

```sh
regatta status            # live TUI, refreshes every 5s
regatta status --once     # one-shot snapshot for scripting
```

Five panels render:

| Panel | What it shows | Green-loop shape |
|---|---|---|
| Active subagents | count of in-flight Claude subprocess sessions | `0` when idle; `1-3` during brief processing |
| In-flight PRs | open PRs the loop is tracking | `0-2` typical; spikes during batch waves |
| Recent merges (24h) | merges the loop observed in the last 24h | starts at `0`, ticks up as PRs close |
| Today's cost | running USD against `spend_cap_usd_per_day` | `$0.00` on a fresh install; under-cap throughout the day |
| Green-clock | uninterrupted uptime since last supervisor restart | starts ticking 60s after `install-service` returns |

A panel reading `MISSING` means the substrate DB query returned no
rows. On a fresh install this is *expected* for cost + merges until
the first work_item lands. After Step 11 (smoke-dispatch), the cost
panel should leave `MISSING`; if it stays `MISSING` past the first
spawn, substrate-DB connectivity is broken — tail stderr.log.

## Step 11 — smoke-dispatch

Verify the loop actually picks up work with a minimal item.

```sh
# 1. Drop a trivial work item.
mkdir -p .regatta/items
cat > .regatta/items/SMOKE-1.md <<'EOF'
---
id: SMOKE-1
kind: refactor
title: smoke — rename a docs comment
status: planned
---

## Acceptance criteria

- [planned] c1: change one HTML comment in docs/operator/README.md
EOF

# 2. Confirm the adapter sees it.
sqlite3 .regatta/state.db \
  "SELECT id, status FROM work_items WHERE id='SMOKE-1'"
# Expected: 1 row, status=planned (before tick) → running (after tick).

# 3. Watch the loop pick it up.
regatta status --refresh 5s
# Expected within 30s: active-subagents panel flips 0 → 1.

# 4. Open the spawned PR.
gh pr list --state open --search "SMOKE-1" --json number,title

# 5. Confirm green-clock + recent-merges advance after merge.
regatta status --once
# Expected: recent-merges panel shows SMOKE-1; green-clock unchanged.
```

End-to-end SLO target: <10 minutes from item drop to merge on a
trivial refactor.

## Step 12 — first-PR walk

The first real PR fires five transitions, in order:

```
brief.signed → spawn → L4 verdict → automerge gate → human-merge
```

Watch each transition in the log stream:

```sh
# Linux
journalctl -u regatta -f | grep -E 'brief\.signed|spawn|L4|automerge|merged'

# macOS
tail -F ~/Library/Logs/regatta/stderr.log | grep -E 'brief\.signed|spawn|L4|automerge|merged'
```

What success looks like:

- `brief.signed` event with a non-empty `brief_id`.
- A child PR opens within 60s.
- An `L4 verdict` line with `pass`.
- An `automerge gate` line: `eligible` (CI green + reviewer approve).
- A `merged` line after you click the merge button in the GitHub UI.

**MVR-1-T4 caveat**: the GH-issue adapter (auto-consume of
loop-filed issues back into work_items) is **not yet shipped**.
Alarm-webhook *fires* and files a GH issue (Step 13), but the loop
does not yet auto-consume it back into work-item state. Until MVR-1-T4
lands you must hand-process those issues weekly.

## Step 13 — alarm-webhook trip

The alarm-webhook listens on `alarm_webhook.listen_addr` (default
`127.0.0.1:9101`) and files a GitHub issue on receipt. The fastest
verification hits the handler directly:

```sh
curl -fsS -X POST http://127.0.0.1:9101 \
  -H 'Content-Type: application/json' \
  -d '{"alerts":[{"labels":{"alertname":"smoke"}}]}'
```

Expected: one new GH issue in `alarm_webhook.gh_repo` within 30s,
title `regatta alarm: smoke`. Close the test issue once filed.

If you also want to exercise the cost-cap path end-to-end (slower —
five operator steps), set `safety.spend_cap_usd_per_day: 0.01` in
`regatta.yaml`, run `regatta reload-secrets`, drop a smoke item, then
restore the cap and run `regatta resume` to lift the throttle. The
curl recipe above is the default verification; the cost-cap variant is
optional belt-and-suspenders.

## Step 14 — 24h stop criteria

Stop watching when **all four** hold:

- [ ] `regatta status --once` green-clock panel shows ≥24h since the
      last restart event.
- [ ] Cost-today panel is under your `spend_cap_usd_per_day` setting.
- [ ] Zero `brief.rejected`, `brief.tombstoned`,
      `child.cascade_archived`, `child.dependency_archived`, or
      `alarmwebhook.disabled` events in journald / stderr.log in the
      last 24h.
- [ ] At least one PR closed end-to-end (merged via operator click,
      abandoned via approval-gate timeout, or L4-rejected). Confirms
      the brief→spawn→L4→merge path is exercised, not just polled.

If any criterion fails, do **not** stop. Capture the failing panel +
the matching log slice into a post-mortem at
`docs/engineer/post-mortems/`.

7d-watch criterion adds: GH-issue adapter (MVR-1-T4) must have shipped,
else continue hand-processing alarm-webhook issues weekly.

30d-watch criterion adds: at least one full automerge cycle (no human
merge click). Gated on Phase X relaxation, not Phase S.

## Common pitfalls

- `.env` was not gitignored prior to PR #826 — re-clone or `git rm
  --cached .env` if running off an older checkout.
- `GH_TOKEN` in your shell environment overrides `.env` on Linux
  (`EnvironmentFile=` is overridden by the parent); on macOS the plist
  wrapper runs `set -a; . ...; exec` *after* the parent env is
  captured, so `.env` wins. Pick one source.
- launchd on macOS does **not** inherit your interactive shell's
  `PATH`. Confirm `launchctl print gui/$UID/com.regatta.serve | grep
  PATH` shows the brew prefix your binary lives in.
- `regatta.yaml` lives at the repo root, **not** under
  `~/.config/regatta/`. The daemon reads it from `--repo`'s working
  directory.
- Anthropic console monthly cap is the *only* hard ceiling; the
  in-loop `spend_cap_usd_per_day` is a soft cap that throttles but does
  not bill-block.

## Troubleshooting

### Install times out at `/healthz` poll

The supervisor reports `installed but not yet healthy — check stderr
for boot progress` after a 30s window if `status` stays `degraded`.
Tail `stderr.log` (`~/Library/Logs/regatta/stderr.log` on macOS,
`journalctl -u regatta` on Linux) for the boot error. Common causes:

- Missing `ANTHROPIC_API_KEY` / `GH_TOKEN` in the env file *or* (on
  macOS pre-#830) the env file is correct but the plist ignores it —
  see Step 2.
- SQLite database path not writable (check `WorkingDir` permissions).
- Cold-start brief load slower than 30s — wait for the watchdog
  restart, or `launchctl kickstart -k gui/$UID/com.regatta.serve` on
  macOS / `sudo systemctl restart regatta` on Linux. Re-running
  `install-service` re-renders the unit and re-bootstraps launchd; it
  does **not** affect brief caching.

### `regatta` exits immediately, no log

Check the unit's effective env:

- Linux: `systemctl show regatta -p Environment` and
  `sudo cat /etc/regatta/env`.
- macOS: `launchctl print gui/$(id -u)/com.regatta.serve` and grep
  `environment`.

### Health probe returns connection refused

The HTTP listener defaults to `:8080`. Confirm:

- the `regatta serve --help` listener-flag list for current defaults
  (the listener flag was renamed during the operator-console v5.1
  refactor; verify the current name on your tree),
- nothing else is bound on 8080 (`sudo lsof -i :8080`),
- the unit was not started with the listener disabled.

## Rollback escape hatches

`regatta uninstall-service` is the happy-path verb (covered above).
Use the verbs below when the supervisor is wedged.

- **launchd unit hangs (macOS)**: `launchctl bootout
  gui/$(id -u)/com.regatta.serve` forcibly tears down a stuck agent
  before `uninstall-service` runs.
- **systemd unit hangs (Linux)**: `sudo systemctl kill -s KILL
  regatta.service` then `sudo systemctl reset-failed regatta.service`.
- **Stuck `/healthz` poll during install**: Ctrl-C is safe — the
  install is atomic at `writeAtomic`; re-run with `--force` to retry.
- **Wedged DB / state corruption**: `regatta uninstall-service` leaves
  `~/.local/share/regatta/` (Linux) and `~/Library/Application
  Support/regatta/` (macOS) intact by design. To reset, `rm -rf` the
  state dir between uninstall and reinstall. This is destructive —
  every prior work-item, brief, and merge record is gone.

## Related

- [`container.md`](container.md) — Docker path (Stage 1 runtime image).
- [`getting-started.md`](getting-started.md) — first 15-minute walkthrough.
- [`configure.md`](configure.md) — `regatta.yaml` schema.
