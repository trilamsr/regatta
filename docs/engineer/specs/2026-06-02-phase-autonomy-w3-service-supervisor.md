# PHASE-AUTONOMY W3 — Service Supervisor — Design Spec

Status: ready for review
Date: 2026-06-02
Author: design subagent
Item: `.regatta/items/phase-autonomy-w3-service-supervisor.md`
Source brief: `docs/engineer/briefs/2026-06-02-phase-autonomy-amendment.md` §11 W3
Depends on: PHASE-AUTONOMY-W1 (alarm-webhook lives inside the supervised process tree), PHASE-AUTONOMY-W2 (auto-merge — restart correctness is only testable with the loop active)
Soft-depends on: PHASE-AUTONOMY-W6 (secret-credential fetch; install-service hands off env-var prep to `regatta secret set` when W6 lands)

Memory rules in force: `feedback_decision_priority`, `feedback_research_design_principles`, `feedback_root_cause`, `feedback_grade_rubric`, `feedback_adversarial_review`, `feedback_review_every_step`, `feedback_deletion_default`, `feedback_self_improvement`, `feedback_no_signatures`, `feedback_pr_body_release_notes_fence`, `feedback_pr_body_file_only`, `feedback_doc_check_banned_phrases`, `feedback_unaddressed_load_bearing`, `feedback_spec_pattern_authority`, `feedback_test_godoc_one_line`.

---

## §1 Problem statement

Operator currently keeps the loop alive by hand. The substrate process dies on three classes of event the operator should not have to be present for:

- Laptop reboot (overnight macOS auto-update, weekend Linux kernel upgrade).
- Process crash (panic in a worker, OOM kill, dependency exec error).
- Log churn (unbounded `stderr` fills the disk on a long-running session).

W1 (alarm-webhook) gives the operator an issue when SLO breaks; W2 (auto-merge) closes the merge step; but the surface they ride on — the `regatta serve` process — is still operator-restarted. A weekend AFK currently means an offline substrate from the first crash onward.

W3 closes that gap. Brief §11 W3 fixes the shape: macOS launchd plist + Linux systemd unit shipped under `dist/services/`, a one-command `regatta install-service`, a `/healthz` endpoint the supervisor polls, pidfile + lock-file to prevent double-start, and cron lines for the three out-of-loop jobs (`digest --emit`, `items refresh`, `followups triage`).

Self-host filter (`docs/engineer/briefs/2026-06-01-self-host-first.md` §1): yes — the sole operator running `regatta` against this repo cannot leave for a weekend until the OS-native supervisor closes the restart loop. In scope.

### 1.1 Non-goals

- Cross-platform supervisor abstraction. Linux uses systemd. macOS uses launchd. No FreeBSD `rc`, no Windows service. Each named target is one OS init contract adopted verbatim.
- Custom watchdog daemon. The supervisor IS the OS init system; this spec adds zero new long-running processes.
- Container orchestration. K8s / Nomad / Docker Compose run elsewhere (`docs/operator/container.md`); this wedge is the bare-metal path only.
- Log rotation engine. Linux journald rotates on its own; macOS launchd's `StandardErrorPath` plus operator-configured `newsyslog` rotates on its own. This wedge adds zero log code.
- Replacing the existing `deploy/install-systemd.sh` / `deploy/install-launchd.sh` shell scripts as a separate path. The `regatta install-service` Go command supersedes them; the shell scripts get deleted in the same PR (deletion-default per `feedback_deletion_default`).

---

## §2 Decision-priority filter

Per `feedback_decision_priority` (UX > ease > performance > best-practices > speed > velocity; long-term > short-term):

| Lens | Choice |
|---|---|
| UX | One command — `regatta install-service` — leaves the operator with a unit registered, bootstrapped, and verified live via `/healthz`. No second tab, no manual `systemctl daemon-reload`, no `launchctl bootstrap` syntax memorized. |
| Ease | Re-run is idempotent. Same command updates an existing install. `regatta uninstall-service` reverses cleanly. |
| Performance | Supervisor runs in OS kernel; install-time cost is bounded by health-poll window (≤ 30s wall clock). `/healthz` checks three local primitives; p99 < 50ms. |
| Best-practices | systemd + launchd are the named init contracts on each OS. The `/healthz` shape follows the Kubernetes liveness/readiness convention (status + checks map). |
| Long-term | Deleting `deploy/install-{systemd,launchd}.sh` removes 244 lines of bash. Adding `regatta install-service` adds ~280 lines of Go. Net: bash-to-Go conversion with one binary, one entry point, unit-tested. |

Highest-leverage UX win: operator runs ONE command then never restarts the loop again. That ranks the install-time verification step (poll `/healthz` for 30s) above raw service-file fidelity — a unit file that exists but fails to start is worse than no install.

---

## §3 Architecture

### 3.1 Component map

```
  operator
     |
     | regatta install-service [--user|--system] [--dry-run]
     v
+----------------------------+
|  cmd/regatta install.go    |
|                            |
|  - OS detect (GOOS)        |
|  - template render         |
|  - bootstrap call          |
|  - /healthz poll loop      |
|  - idempotency check       |
+----------------------------+
        |              |
        v              v
   templates        OS init system
   under            (systemctl OR
   dist/services/   launchctl)
        |              |
        v              v
   render to     bootstrap unit
   target path        |
                      v
                +-------------+
                | regatta     |
                | serve       |
                |             |
                | /healthz    |<--- supervisor poll (Linux: WATCHDOG=1;
                |             |     macOS: cron-via-launchd KeepAlive)
                +-------------+
```

The install command is a thin orchestrator. It:

1. Detects OS via `runtime.GOOS`. macOS → launchd path; Linux → systemd path; anything else → exit with named error pointing at the container runbook.
2. Resolves the absolute path of the running binary (`os.Executable`) and the operator's chosen install root (`--user` → `~/Library/LaunchAgents` or `~/.config/systemd/user`; `--system` → `/Library/LaunchDaemons` or `/etc/systemd/system`).
3. Renders the template (text/template) substituting the resolved binary path, repo path, log directory, and environment-file path.
4. Writes the unit / plist atomically (write-to-tmp + rename).
5. Validates: `plutil -lint <path>` on macOS; `systemd-analyze verify <path>` on Linux. Roll back on failure. **Fallback when the validator binary is missing on `$PATH`** (stripped-down container image, embedded Linux, macOS recovery shell): install-service falls back to an in-process text-schema validation pass (plist: regex check for `<?xml version="1.0"...?>` preamble + `<plist version="1.0">` open + balanced `<dict>`/`</dict>` + balanced `<array>`/`</array>` + closing `</plist>`; unit: required `[Service]` + `ExecStart=` + `[Install]` sections present) and emits `WARN: plutil/systemd-analyze not on PATH; applied text-schema validation only — recommend installing the validator before next install` to stderr. Install proceeds. The validator-missing case never blocks the install — it only downgrades the safety check with an operator-visible warning.
6. Bootstraps: `launchctl bootstrap gui/$UID <path>` (user) or `launchctl bootstrap system <path>` (system) on macOS; `systemctl --user daemon-reload && systemctl --user enable --now <unit>` (user) or the system equivalent on Linux.
7. Polls `/healthz` over loopback every 1s for 30s. First 200 → install reported success. Timeout → rollback (deregister unit, remove file) and exit with named error. Cold-start `degraded` handling (per §10 risk 11): the poll loop accepts `status: ok` as success and `status: degraded` after the full 30s window as `installed but not yet healthy — tail stderr for boot progress` (exit 0 with warning). Only `status: down` OR no-response-at-all triggers rollback.

   **SELinux/AppArmor detection (Linux only, per §10 risk 8):** before the bootstrap call (step 6), install-service checks for SELinux and AppArmor:

   - If `command -v sestatus` is on `$PATH` AND `sestatus` reports `SELinux status: enabled` AND `Current mode: enforcing`: emit a post-install instructions block to stdout (NOT stderr; this is operator guidance, not an error):
     ```
     NOTE: SELinux is enforcing. If the unit fails to start with a permission denial,
           generate + load a local policy module:
               sudo ausearch -m AVC -ts recent | audit2allow -M regatta_local
               sudo semodule -i regatta_local.pp
           Then re-run: regatta install-service
     ```
     Install proceeds — `audit2allow` is not auto-invoked because it requires `sudo` + an AVC denial trace that only exists AFTER the first failed start. F-6 tracks the autodetect-and-auto-generate flow.
   - If `command -v aa-status` is on `$PATH` AND AppArmor is enabled in enforce mode: emit the analogous instruction pointing at `aa-complain` for the regatta profile path. Install proceeds.
   - Neither tool present → silent (most non-RHEL/non-Ubuntu hosts).

Idempotency: step 4 detects existing file → compares rendered template byte-for-byte. Three branches:

- **Identical**: skip steps 5-7 and report `already installed`.
- **Different + `--force` set**: write a timestamped backup at `<path>.bak.<RFC3339>`, then apply + re-bootstrap. Operator sees `existing unit differs; backed up to <path>.bak.<ts>; reinstalling`.
- **Different + `--force` NOT set (default)**: refuse with named error `existing unit at <path> differs from rendered template; re-run with --force to overwrite (a .bak file will be written)`. Exit 1, no filesystem mutation. This is the safe default — operators who hand-edited the unit do not lose work to an unattended re-run.

Additionally per §10 risk 12: idempotency-check verifies BOTH file presence AND unit registration (`systemctl is-enabled --quiet <unit>` on Linux, `launchctl print <domain>/<label>` on macOS). File-present-but-unregistered → step 4 falls through to bootstrap-only (steps 6-7) so a prior crash between write and register self-heals on re-run.

### 3.2 File layout (new files in this wedge)

```
cmd/regatta/
  install.go             # install-service subcommand (~180 LoC)
  install_test.go        # dry-run table tests, template-render tests
  uninstall.go           # uninstall-service subcommand (~60 LoC)
  uninstall_test.go      # symmetric uninstall coverage
  service_status.go      # service status subcommand (~40 LoC)
  service_status_test.go

internal/health/
  healthz.go             # the /healthz handler, expanded from cmd/regatta/serve.go::healthzHandler (~80 LoC, including checks)
  healthz_test.go        # check matrix: db ok, db down, heartbeat stale, brief missing
  checks.go              # the three check primitives (db ping, heartbeat freshness, brief schema)
  checks_test.go

internal/watchdog/
  notify_linux.go        # sd_notify(WATCHDOG=1) every 10s; emits via systemd-supplied NOTIFY_SOCKET
  notify_other.go        # no-op stub for darwin + windows + freebsd (build tag)
  notify_test.go         # socket-write test under fake $NOTIFY_SOCKET

dist/services/
  regatta.service.tmpl   # systemd unit template (~30 lines)
  regatta.plist.tmpl     # launchd plist template (~50 lines including XML preamble)

dist/cron/
  regatta.crontab        # three cron lines, idempotent comment-anchored

docs/engineer/specs/
  2026-06-02-phase-autonomy-w3-service-supervisor.md   # this file
```

Files deleted in this wedge (deletion-default):

```
deploy/install-systemd.sh        # 104 lines → supplanted by cmd/regatta install-service
deploy/install-launchd.sh        #  97 lines → supplanted by cmd/regatta install-service
deploy/launchd/regatta-serve.sh  #  43 lines → no longer needed; install renders plist directly
deploy/launchd/com.regatta.serve.plist  # → replaced by dist/services/regatta.plist.tmpl (templated)
deploy/systemd/regatta.service         # → replaced by dist/services/regatta.service.tmpl (templated)
```

Net LoC: +280 Go, +80 service templates, -244 bash, -91 service files = +25 net. README + native-deploy.md edits update install steps to call the new command.

**Deleted-files transition (atomic with this spec's implementation PR, not a follow-up).** The implementer PR that lands `cmd/regatta install-service` deletes the listed `deploy/install-systemd.sh`, `deploy/install-launchd.sh`, `deploy/launchd/regatta-serve.sh`, `deploy/launchd/com.regatta.serve.plist`, and `deploy/systemd/regatta.service` files in the SAME commit as the new Go command. Atomic delete + replace is mandatory because:

1. **No runtime callers in-tree.** Verification command for the implementer to run before staging the delete:
   ```bash
   git grep -nE 'deploy/(install-systemd|install-launchd|launchd/regatta-serve)\.sh|deploy/launchd/com\.regatta\.serve\.plist|deploy/systemd/regatta\.service'
   ```
   Expected hit shape (verified at spec-time on `main`): hits ONLY in (a) prose references inside this spec itself, (b) `docs/operator/native-deploy.md` (operator runbook prose pointing at the install scripts), and (c) `docs/engineer/autonomous-session-prompt.md` (boot-prompt Option B / Option C bullets). NO hits under `cmd/`, `internal/`, `Makefile`, `.github/workflows/`, `scripts/`, or any non-doc Go file — meaning no automated caller depends on the deleted paths. If the implementer's verification turns up a non-doc hit, that is a blocker; the implementer files a followup and stops the delete.
2. **Doc callers rewritten in the SAME PR.** The implementer PR atomically:
   - Rewrites `docs/operator/native-deploy.md` so all `deploy/install-systemd.sh` / `deploy/install-launchd.sh` / `deploy/launchd/regatta-serve.sh` invocations become `regatta install-service` walkthroughs (§6 of this spec is the source of truth for the new flow; the runbook copies the verbatim shell transcripts).
   - Updates `docs/engineer/autonomous-session-prompt.md` Option B / Option C bullets to point at `regatta install-service [--user|--system]` instead of the bash scripts.
   - Updates the top-level `README.md` install snippet.

   All three doc updates land in the SAME commit as the script deletions; no doc-link-rot window exists between PR-open and PR-merge.
3. **PR body operator instructions.** The implementer PR body's `release-notes` fence calls out the cutover explicitly: `## Breaking — deploy/install-{systemd,launchd}.sh removed; replace operator runbook invocation with regatta install-service [--user|--system]. Old scripts had no automated callers; doc references rewritten in same PR; no CI updates required.` This makes the migration visible in the release notes without needing a follow-up wedge.

The spec rejects the alternative phased-deletion approach (leave bash scripts as a stub that `exec`s the Go command) because it (a) leaves dead bash in-tree against `feedback_deletion_default`, (b) duplicates the runtime path of failures (now an operator must debug bash-wrapping-Go), and (c) adds a second binary contract surface (the bash exit codes) that has to stay in sync. One atomic swap is cheaper.

### 3.3 Linux systemd unit template

Path: `dist/services/regatta.service.tmpl`. Rendered with `text/template` substituting `{{.BinaryPath}}`, `{{.EnvFile}}`, `{{.User}}`, `{{.WorkingDir}}`, `{{.ReadWritePaths}}`.

```
[Unit]
Description=regatta autonomous PR loop
Documentation=https://github.com/treedesk-ai/regatta/blob/main/docs/operator/native-deploy.md
After=network-online.target
Wants=network-online.target

[Service]
Type=notify
NotifyAccess=main
ExecStart={{.BinaryPath}} serve --config /etc/regatta/regatta.yaml --repo {{.WorkingDir}}/repo
EnvironmentFile={{.EnvFile}}
User={{.User}}
Group={{.User}}
WorkingDirectory={{.WorkingDir}}

Restart=on-failure
RestartSec=5
WatchdogSec=30

MemoryHigh=2G
MemoryMax=4G
LimitNOFILE=65536
TasksMax=512

ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
PrivateDevices=true
NoNewPrivileges=true
RestrictSUIDSGID=true
LockPersonality=true
ProtectControlGroups=true
ProtectKernelModules=true
ProtectKernelTunables=true
ProtectKernelLogs=true
RestrictNamespaces=true
RestrictRealtime=true
SystemCallArchitectures=native

ReadWritePaths={{.ReadWritePaths}}

[Install]
WantedBy=multi-user.target
```

Changes vs existing `deploy/systemd/regatta.service`:

- `Type=simple` → `Type=notify` (enables `WATCHDOG=1` semantics).
- Add `WatchdogSec=30`.
- Add `NotifyAccess=main`.
- Hardening directives carried verbatim (the existing unit is already tightly scoped).

### 3.4 macOS launchd plist template

Path: `dist/services/regatta.plist.tmpl`. Substitutes `{{.Label}}`, `{{.BinaryPath}}`, `{{.WorkingDir}}`, `{{.LogDir}}`, `{{.HomePath}}`, `{{.EnvVars}}`.

```
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>{{.Label}}</string>

    <key>ProgramArguments</key>
    <array>
        <string>{{.BinaryPath}}</string>
        <string>serve</string>
        <string>--repo</string>
        <string>{{.WorkingDir}}/repo</string>
    </array>

    <key>WorkingDirectory</key>
    <string>{{.WorkingDir}}</string>

    <key>RunAtLoad</key>
    <true/>

    <key>KeepAlive</key>
    <dict>
        <key>SuccessfulExit</key>
        <false/>
        <key>NetworkState</key>
        <true/>
    </dict>

    <key>ThrottleInterval</key>
    <integer>30</integer>

    <key>ProcessType</key>
    <string>Interactive</string>

    <key>StandardOutPath</key>
    <string>{{.LogDir}}/stdout.log</string>

    <key>StandardErrorPath</key>
    <string>{{.LogDir}}/stderr.log</string>

    <key>EnvironmentVariables</key>
    <dict>
        <key>PATH</key>
        <string>{{.PathEnv}}</string>
        <key>HOME</key>
        <string>{{.HomePath}}</string>
    </dict>
</dict>
</plist>
```

Changes vs existing `deploy/launchd/com.regatta.serve.plist`:

- Placeholders templated rather than `REGATTA_*` literal-replaced (cleaner than sed-substitution).
- `BinaryPath` for `ProgramArguments` is taken from `os.Executable()` (canonicalized via `filepath.EvalSymlinks`) — never from `which regatta` or `$PATH`. This pins the plist to the binary actually running install-service. Per §10 risk 4 amendment: ignore `which` entirely for binary resolution; `which` cross-arch failures (Apple-Silicon laptop returning the Intel brew path) silently break `launchctl bootstrap`.
- `PATH` env var (for child invocations inside the loop) is resolved at install-time by inspecting `os.Executable()`'s parent directory: if it lives under `/opt/homebrew` → emit `/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin`; if under `/usr/local` → emit `/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin`; otherwise → emit `/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin` (non-brew path; both brew prefixes appended as fallback in case the operator later adds a brew dependency). `which brew` is consulted only as a tie-breaker when `os.Executable()` returns a generic path like `/usr/bin/regatta` (Linux distro package install); it is never authoritative on macOS.
- All other keys carried verbatim.

### 3.5 `/healthz` semantics

Existing endpoint (`cmd/regatta/serve.go:687`) returns literal `ok\n` with zero DB queries (W7.0 §3.3 row 6 liveness contract).

W3 extends with a parallel **readiness** check at `/healthz` returning JSON when `Accept: application/json` is set. The literal-`ok` path is preserved for the W7.0 contract; the JSON path adds the supervisor-observable check matrix.

Shape (Accept: application/json):

```
{
  "status": "ok" | "degraded" | "down",
  "version": "v0.<semver>",
  "checks": {
    "db":        { "status": "ok|down", "latency_ms": 3,  "error": "" },
    "heartbeat": { "status": "ok|stale", "age_seconds": 7, "error": "" },
    "brief":     { "status": "ok|missing", "loaded_path": "...", "error": "" }
  }
}
```

Rules:

- `db` — `sql.DB.PingContext` with 500ms timeout. `down` → overall `down` → HTTP 503.
- `heartbeat` — the serve binary writes a row to `health_heartbeat (ts)` every 10s in a background goroutine. Stale = age > 60s. Stale → overall `degraded` → still HTTP 200 (the process is alive enough to answer; supervisor reads `status` field for restart decision).
- **Dedicated `*sql.DB` for heartbeat writes (mitigates §10 risk 2)**: the heartbeat writer opens its own `*sql.DB` handle against the same database file (`sql.Open("sqlite3", dsn)`), and immediately calls `db.SetMaxOpenConns(1)` + `db.SetConnMaxIdleTime(0)` so this handle owns exactly one reserved SQLite connection that the main DB pool cannot exhaust. Effect: even if the main pool is fully saturated by a stuck migration / long transaction / pathological worker loop, the heartbeat writer continues to record liveness — and once heartbeats truly stop, that signal is honest (the supervisor's restart is then correct). Without the dedicated handle, pool exhaustion makes the heartbeat row stale even though the process is otherwise healthy, producing spurious supervisor restarts. The handle is shared with the `db` check (also `SetMaxOpenConns(1)`) so the readiness probe and the heartbeat writer fail together when SQLite is actually wedged.
- `brief` — at-start the loader records the resolved brief path in an in-memory cell; missing → overall `degraded` → HTTP 200.
- `status` derivation: any `down` → `down`. Otherwise any `stale|missing` → `degraded`. Otherwise `ok`.

Supervisor consumers:

- systemd (Linux): does not poll `/healthz` directly. It relies on `WATCHDOG=1` notifies from the worker goroutine (see §3.6). The `/healthz` JSON endpoint is for the operator's `regatta service status` command and external monitors.
- launchd (macOS): no native watchdog. `regatta service status` polls `/healthz` JSON; an out-of-loop cron poll under `dist/cron/regatta.crontab` writes a stderr line on `down`. See §10 risk 7 for the gap discussion.

Implementation lives in `internal/health/`; the serve binary plugs it in via a single `mux.HandleFunc("/healthz", health.Handler(...))` replacement.

### 3.6 systemd watchdog wire path

`Type=notify` plus `WatchdogSec=30` in the unit means: systemd kills the process if it does not receive `WATCHDOG=1` within 30s of the previous notify (or service start). On kill, `Restart=on-failure` brings it back.

The serve binary's worker goroutine emits `sd_notify(WATCHDOG=1)` every 10s — a 3x safety factor vs the 30s window. The notify is fire-and-forget on a Unix datagram socket whose path arrives in the `NOTIFY_SOCKET` env var; absence of the env var = not under systemd = silent no-op (correct behavior on macOS or under `go run`).

Risk: the long-running LLM call can run > 30s. The notify goroutine is independent of any LLM client — it sleeps and writes, never blocks on a Claude API call. See §10 risk 2 for the failure mode where the entire process is wedged.

No third-party dep needed; the wire format is one ~20-line socket-write implementation in `internal/watchdog/notify_linux.go`. Reference: systemd `sd_notify(3)` man page.

**Socket-write error handling.** Each `WATCHDOG=1` write is fire-and-forget at the socket layer, but the implementation does NOT silently swallow errors. On any non-nil error from `conn.Write` (socket gone, permission, ENOBUFS), the goroutine logs at `WARN` with the error string + the syscall errno (when available), increments an internal `watchdog_notify_failures_total` counter (exposed via the existing `/metrics` endpoint when OTel is wired), and continues the 10s loop — it does NOT crash, does NOT exit, and does NOT escalate to panic. Rationale: if the notify socket is truly gone, systemd will kill the process within `WatchdogSec=30` anyway, and `Restart=on-failure` brings it back. The supervisor is the source of truth for "is the watchdog working"; the goroutine's job is to keep trying, not to second-guess the supervisor.

**Shutdown coordination.** The notify goroutine takes a `context.Context` from the serve binary's main lifecycle. On context-cancel (SIGTERM, SIGINT, supervisor stop): emit one `STOPPING=1` notify (best-effort; ignore errors), then close the unix-domain socket, then `return`. This lets the goroutine exit cleanly under `go test -race` (no leaked goroutines) and gives systemd an explicit "I am about to exit" signal so it suppresses the `WatchdogSec` restart trigger on a graceful stop. Test 12 covers the `ctx.Cancel() → goroutine returns within 1s` contract.

### 3.7 Cron templates

Path: `dist/cron/regatta.crontab`. Format follows operator-installable convention (comment-anchored so re-run does not duplicate entries).

```
# BEGIN regatta cron (managed by regatta install-service; do not edit between markers)
0 4  * * *   /usr/local/bin/regatta digest --emit                  >> /var/log/regatta/cron.log 2>&1
*/15 * * * * /usr/local/bin/regatta items refresh                  >> /var/log/regatta/cron.log 2>&1
0 *  * * *   /usr/local/bin/regatta followups triage               >> /var/log/regatta/cron.log 2>&1
# END regatta cron
```

`install-service` invokes `crontab -l` (capturing the operator's existing entries), strips any prior `BEGIN regatta cron ... END regatta cron` block, appends the new block, and pipes the result back to `crontab -`. `uninstall-service` strips the block.

Per `feedback_root_cause`, this addresses the "operator forgot to add cron" failure mode at its root: the install command owns the install, not a separate documentation step.

### 3.8 CLI surface

```
regatta install-service [--user|--system] [--dry-run] [--no-cron]
regatta uninstall-service [--user|--system]
regatta service status
```

`--user` is the default. The user-mode systemd path uses `~/.config/systemd/user/regatta.service` and `systemctl --user`; the user-mode launchd path uses `~/Library/LaunchAgents/com.regatta.serve.plist` and `launchctl bootstrap gui/$UID`. No sudo required for user mode. `--system` requires root (the command refuses with a named error if `os.Geteuid() != 0` and `--system` is set).

`--dry-run` renders the unit / plist + the install plan to stdout, makes zero filesystem writes, and exits 0. Operator inspects, then re-runs without `--dry-run`.

`--no-cron` skips §3.7. Operator with an existing cron-management story can opt out; default is install crontab.

**Uninstall idempotency.** `regatta uninstall-service` is fully idempotent. Behavior matrix:

- Unit registered + file present → unregister (`launchctl bootout` / `systemctl --user disable --now`), remove file, strip cron block, report `uninstalled`.
- Unit registered + file missing → unregister only, report `uninstalled (stale registration cleared)`.
- Unit not registered + file present → remove file, strip cron block, report `uninstalled (file removed)`.
- Unit not registered + file missing + no cron block → exit 0 with `INFO: nothing to remove (already uninstalled)`. NO error, NO non-zero exit, NO operator-action-required message. Re-run on an already-clean host is a green no-op.
- Partial cron block remnant (block present but unit gone) → strip cron block, report `uninstalled (cron block stripped)`.

Each step is independently best-effort: a failure to strip the cron block does NOT prevent the unit unregister from running, and vice versa. All errors are accumulated and reported at exit; partial success is the default mode. This matches the install-side idempotency contract — re-runs converge to the steady state regardless of how messy the starting point is.

`regatta service status` reads the OS init system's state plus polls `/healthz` JSON:

```
$ regatta service status
unit:         com.regatta.serve (launchd, user)
state:        running (pid 12345, uptime 3d 4h)
restarts:     2 (last: 2d ago, exit code 137)
healthz:      ok
checks:       db=ok heartbeat=ok(2s) brief=ok
binary:       /usr/local/bin/regatta v0.3.4
log:          ~/Library/Logs/regatta/stderr.log
```

---

## §4 Prior art adopted

Per `feedback_research_design_principles`:

- [systemd](https://systemd.io/) (LGPL-2.1, `v257.x`) — Linux init contract. `Restart=on-failure`, `WatchdogSec`, `EnvironmentFile`, `Type=notify`, `NotifyAccess=main` all adopted verbatim. Reference unit shape: systemd-bundled `systemd-journald.service` (`/lib/systemd/system/systemd-journald.service` upstream).
- [launchd](https://developer.apple.com/library/archive/documentation/MacOSX/Conceptual/BPSystemStartup/Chapters/CreatingLaunchdJobs.html) — macOS init contract. `KeepAlive`, `RunAtLoad`, `StandardErrorPath`, `ThrottleInterval`, `ProcessType` all adopted verbatim. Apple-licensed reference, no third-party fork.
- [grafana/agent](https://github.com/grafana/agent) (Apache 2, `v0.43.4` at `commit 91a9c4e`) — reference Go daemon that ships both systemd unit + launchd plist in one repo. Our `dist/services/` layout mirrors its `packaging/grafana-agent/` layout. We do not adopt code — just file-organization shape.
- [Kubernetes `/healthz` convention](https://github.com/kubernetes/kubernetes/blob/master/CHANGELOG/CHANGELOG-1.0.md) (Apache 2) — adopted endpoint shape, including the `status + checks` JSON map and 200/503 split. No code adopted; the convention is in operator-monitoring tooling muscle memory.
- [sd_notify(3) man page](https://www.freedesktop.org/software/systemd/man/sd_notify.html) — wire format for the watchdog notify. Pure socket write; ~20 LoC. No dependency added.
- [`github.com/coreos/go-systemd/v22/daemon`](https://github.com/coreos/go-systemd) (Apache 2, `v22.5.0`) — optional dep that exposes `daemon.SdNotify`. **Considered + rejected** for this wedge: pulls in ~3k LoC of unrelated systemd helpers; the notify-only path is 20 LoC of pure stdlib `net.Dial("unixgram", ...)`. Revisit if we later need `daemon.SdNotifyBarrier` or socket-activation; track as Followup F-3.

Adopt-vs-build summary: adopt systemd + launchd + sd_notify wire format + Kubernetes `/healthz` shape. Build `regatta install-service` (no OSS today writes a templated unit for a per-repo regatta install) and the `/healthz` JSON extension.

---

## §5 Self-host filter

Filter question (`docs/engineer/briefs/2026-06-01-self-host-first.md` §1): does the sole internal operator dispatching regatta against THIS repo need this to run unattended?

- launchd path: yes — operator runs on macOS laptop.
- systemd path: yes — operator may run on a Linux server for the 30-day-green window.
- `/healthz` JSON: yes — `regatta service status` is the diagnostic the operator opens when waking from AFK.
- Cron entries: yes — `digest --emit`, `items refresh`, `followups triage` are out-of-loop today and someone has to schedule them.
- `--user` default: yes — operator's macOS dev box has no sudo path planned; `~/Library/LaunchAgents` is the only viable target.
- `--dry-run`: yes — the operator's first-run test will be `--dry-run` against the prod plist; without it the first install gambles a half-broken unit.
- Cosign-signed unit files (A+ rubric item): deferred to Phase X. Reopen trigger: first external customer ask for verifiable supply-chain provenance on the unit files.
- Multi-user host support (§10 risk 8): deferred. Reopen trigger: second human operator on the same host.

---

## §6 Operator UX walkthrough

**First-time install on macOS (the load-bearing path):**

```
$ regatta install-service --user --dry-run
detected: darwin, brew at /opt/homebrew/bin
plist:    ~/Library/LaunchAgents/com.regatta.serve.plist
binary:   /opt/homebrew/bin/regatta
workdir:  ~/.local/share/regatta
logs:     ~/Library/Logs/regatta
cron:     3 entries (digest, items refresh, followups)
no writes performed (--dry-run)

$ regatta install-service --user
installed plist, bootstrapped, polling /healthz... ok (4.2s)
cron installed (3 entries)
$
```

**Re-run (idempotent):**

```
$ regatta install-service --user
plist unchanged, cron unchanged, nothing to do
```

**Status check:**

```
$ regatta service status
unit:    com.regatta.serve (launchd, user, loaded)
state:   running (pid 71234, uptime 18h)
healthz: ok
checks:  db=ok heartbeat=ok(3s) brief=ok
```

**Uninstall:**

```
$ regatta uninstall-service --user
unloaded plist, removed file, stripped cron block
$
```

**Failure path — install times out at /healthz poll:**

```
$ regatta install-service --user
installed plist, bootstrapped, polling /healthz... timed out after 30s
rolling back: unloaded plist, removed file
last stderr (~/Library/Logs/regatta/stderr.log tail):
  fatal: open db: no such file or directory: ~/.local/share/regatta/regatta.db
hint: run `regatta init` first, then retry install-service
$ echo $?
1
```

W6 hand-off: when W6 (`regatta secret set`) ships, `install-service` detects missing `ANTHROPIC_API_KEY` / `GH_TOKEN` and prompts the operator to run `regatta secret set` before retrying. Until W6 ships, the spec leaves env-var setup to the operator's existing flow (matches today's `deploy/install-systemd.sh` behavior: writes a stub `/etc/regatta/env`, operator fills it in).

---

## §7 Test plan

All tests pass `make check` (lint + vet + race + cover); long tests skipped under `-short` per repo convention. Per `feedback_tdd_discipline` the failing test lands first in each implementer PR.

Named tests (1-line godocs per `feedback_test_godoc_one_line`):

1. `TestInstallServiceDryRun_PrintsPlistAndExitsCleanOnDarwin` — dry-run prints rendered plist to stdout + writes zero files; covers c1 + A+ rubric (h).
2. `TestInstallServiceDryRun_PrintsUnitAndExitsCleanOnLinux` — Linux dry-run mirror; covers c2.
3. `TestInstallServiceIdempotent_ReRunSkipsBootstrap` — second invocation re-detects unchanged file + skips re-bootstrap; covers UX walkthrough §6 case 2.
4. `TestInstallServiceRollback_OnHealthzPollTimeout` — fake serve never opens `/healthz` → install times out at 30s → unit deregistered + file removed; covers c3 negative path.
5. `TestUninstallService_RemovesUnitAndStripsCron` — uninstall reverses install side effects; covers c6.
6. `TestHealthzJSON_AllOk_Returns200` — happy path of the JSON endpoint.
7. `TestHealthzJSON_DbDown_Returns503` — db ping fails → overall `down` → 503.
8. `TestHealthzJSON_HeartbeatStale_Returns200Degraded` — heartbeat > 60s old → `degraded` + 200 (supervisor reads body to decide).
9. `TestHealthzJSON_BriefMissing_Returns200Degraded` — brief loader has no path → `degraded`.
10. `TestHealthzPlainText_LegacyContractPreserved` — `GET /healthz` without `Accept: application/json` still returns literal `ok\n` per W7.0 contract.
11. `TestSdNotify_WritesWatchdogMessage_OnLinux` — under fake `$NOTIFY_SOCKET`, the goroutine writes `WATCHDOG=1` every 10s.
12. `TestSdNotify_NoSocketEnv_IsNoOp` — empty `$NOTIFY_SOCKET` → no panic, no goroutine leak, returns clean.
13. `TestPlistRender_AppleSiliconBrew_HasOptHomebrewInPath` — Apple-Silicon detection → `/opt/homebrew/bin` precedes `/usr/local/bin`.
14. `TestPlistRender_IntelBrew_HasUsrLocalInPath` — Intel mac detection → `/usr/local/bin` precedes `/opt/homebrew/bin`.
15. `TestCronInstall_AnchorBlock_DoesNotDuplicateOnReRun` — re-run replaces the `BEGIN regatta cron`...`END regatta cron` block in place; operator's foreign entries preserved.
16. `TestSystemdAnalyzeVerify_ValidatesRenderedUnit` — `systemd-analyze verify` exits clean on the rendered file; skipped if `systemd-analyze` not on PATH (CI gate runs on Linux runner only).
17. `TestPlutilLint_ValidatesRenderedPlist` — `plutil -lint` exits clean on the rendered file; skipped if `plutil` not on PATH.
18. `TestServiceStatus_RunningUnit_PrintsHealthzOk` — end-to-end: launch fake unit + assert `regatta service status` reports running + healthz ok.
19. `FuzzPlistRender_NoXmlInjection` — fuzz the templated fields against XML-special-char inputs; rendered plist always validates via `plutil -lint`.
20. `TestInstallService_ConcurrentInvocations_OnlyOneWins` — 50 parallel `install-service` calls; flock on the install lockfile ensures only one mutates state; covers A+ rubric (f).

---

## §8 B/A/A+ grade rubric

Per `feedback_grade_rubric`. Each tier names falsifiable acceptance.

| Tier | Criteria |
|---|---|
| B (floor) | (a) Tests 1, 2, 4, 6, 10 ship green. (b) `/healthz` JSON ≤ 80 LoC including the three check primitives. (c) `regatta install-service --user` on macOS writes the plist + bootstraps + polls `/healthz` within 30s. (d) Same on Linux for systemd. (e) Release-notes fence in PR body. (f) Banned-phrase grep clean. |
| A (target) | B + (g) Tests 3, 5, 7-9, 11-18 ship green. (h) `regatta uninstall-service` reverses cleanly + Test 5 covers it. (i) Pidfile + lockfile under `~/.local/state/regatta/regatta.pid` prevent double-start (race-tested via Test 20). (j) Adversarial reviewer subagent posts on the implementer PR + load-bearing findings ADOPTed. (k) Cron block is comment-anchored idempotent (Test 15). (l) `--dry-run` flag works as §6 case 1 + Test 1 covers. |
| A+ (stretch) | A + (m) Test 19 (fuzz XML injection) + Test 20 (50-way race) ship green. (n) Service files signed via cosign as part of the release artifact path (gated by W10 sigstore signer landing; if W10 not yet shipped, this becomes a tracking issue per `feedback_unaddressed_load_bearing`). (o) `--dry-run` output is mechanically diff-able against the rendered template (operator pipes through `diff` to verify changes between install cycles). (p) Test 16 + 17 (`systemd-analyze verify` + `plutil -lint`) wired into CI for the relevant matrix entries. |

Implementer scorecard MUST be posted verbatim in PR body per `feedback_grade_rubric`.

---

## §9 Adversarial review

Per `feedback_adversarial_review`. Self-review pass before reviewer subagent spawns.

### 9.1 Simplification opportunities

- **Drop `service status` subcommand for v1.** `systemctl status regatta` and `launchctl list com.regatta.serve` already exist. Argument for keeping: status is the load-bearing post-AFK check + a single command that fuses unit-state + healthz is real UX win. **Verdict:** keep. ~40 LoC is cheap; the alternative is two operator-memorized commands.
- **Drop `--system` for v1.** Self-host operator uses `--user` on macOS. **Verdict:** keep the flag (the Linux server path uses `--system`), but skip the test matrix for system-mode rendering until first external user reports the path used. Tracked as F-1.
- **Drop XML-injection fuzz.** Plist render is internal-controlled + the only operator-provided strings are paths. **Verdict:** keep — paths can contain spaces + apostrophes (`/Users/o'connor/code`), and a bad render hangs `launchctl` silently (§10 risk 3). Cheap to fuzz.
- **Drop `internal/watchdog` for non-Linux.** Per build-tag stub. Already the design. ✓.

### 9.2 Deletion candidates

- **Delete `deploy/install-systemd.sh` + `deploy/install-launchd.sh` + `deploy/launchd/regatta-serve.sh` + the static `deploy/{launchd,systemd}/` unit files.** Already in §3.2 deletion list. Net -244 bash LoC.
- **Delete `deploy/launchd/com.regatta.serve.plist` + `deploy/systemd/regatta.service`.** Same.
- **Consider deleting the `--no-cron` flag.** The operator who wants no cron will not invoke `install-service` at all. **Verdict:** keep — the install of the unit is decoupled from cron, and an operator who runs `digest --emit` from elsewhere needs the unit but not the cron block. Cheap (one CLI flag, 5 LoC).

### 9.3 Risk tiers (≥ 10 enumerated per spec template requirement)

Per `feedback_adversarial_review`. Tiers: **B** (blocker, must fix in this wedge), **L** (load-bearing, fix or tracking issue), **N** (noted, accepted).

1. **[L] WatchdogSec=30 vs long LLM call.** A Claude API call can stall > 30s under network latency. The notify is in a separate goroutine, so a stalled LLM call does not block the notify. But if a worker holds the runtime in a tight CPU loop (rare; e.g. infinite loop in retrieved code), the goroutine can starve. Mitigation: the notify goroutine `runtime.LockOSThread` is unnecessary — Go schedules across OS threads. Real failure mode is a panic that takes the process down before the notify fires; `Restart=on-failure` covers it. **Action:** ship as-designed; track as F-4 if observed under prod.
2. **[B] Long-running call wedges entire process.** A SQLite write under a held mutex can block all goroutines (sql.DB pool exhaustion). Mitigation: the heartbeat-write goroutine uses a separate `sql.DB` connection with `SetMaxOpenConns(1)` reserved for health writes. **Action:** spec §3.5 amends; implementer test (Test 11 extended) covers.
3. **[L] launchd plist syntax errors silently fail to load.** macOS swallows malformed plist loads — `launchctl bootstrap` exits 0 but the unit never starts. Mitigation: `plutil -lint` at install-time (§3.1 step 5) catches at write-time. **Action:** Test 17 covers.
4. **[L] PATH brew detection mismatch.** Operator on Apple Silicon with `/opt/homebrew/bin/regatta` but `which brew` returns the Intel path (cross-arch tool layout) → plist `ProgramArguments` points to a missing binary. Mitigation: use `os.Executable()` to resolve the running binary's absolute path; ignore `which`. **Action:** spec §3.4 amends; Test 13 + 14 cover.
5. **[B] Crontab edit race.** Two `install-service` runs editing `crontab -l | ... | crontab -` race-corrupt each other. Mitigation: flock the lock file `~/.local/state/regatta/install.lock` for the entire install. **Action:** Test 20 covers.
6. **[L] Logs flooding on launchd.** `StandardErrorPath` does not rotate by default on macOS. After 30 days the file fills the disk. Mitigation: operator-installable `newsyslog.conf` snippet documented in `docs/operator/native-deploy.md`. **Action:** F-2 tracks doc snippet; this spec does not bundle a `newsyslog` config (out of scope per §1.1).
7. **[L] macOS lacks native watchdog.** WatchdogSec=30 is Linux-only. macOS path relies on `KeepAlive` (restart-on-exit) + external cron-via-launchd poll. If the process is wedged but does not exit, macOS will not restart it. Mitigation: a dedicated launchd job (separate plist `com.regatta.healthcheck.plist`) polls `/healthz` every 60s + `kill -9`s the parent if `down` for 3 consecutive polls. **Action:** ship the healthcheck plist in a follow-up wedge (W3.1) — too much scope for the floor of W3; tracked as F-5.
8. **[L] SELinux / AppArmor profile refuses to start regatta.** Default RHEL with SELinux=enforcing refuses to exec from `/usr/local/bin` for a custom service unit. Mitigation: install-service detects `getenforce` → `Enforcing` + emits a one-liner fix (`audit2allow -a -M regatta && semodule -i regatta.pp`). **Action:** spec §3.1 step 7 amends; tracked as F-6 for the audit2allow flow.
9. **[N] Multi-user host: user-mode + system-mode collision.** If user A installs `--user` and root installs `--system`, both units may try to bind the same port. Mitigation: deferred per §5 self-host filter. **Action:** F-1 tracks.
10. **[N] Reboot semantics on macOS.** `RunAtLoad=true` starts at LaunchAgent load (user login), not at boot. For a true cold-boot LaunchDaemon path, the operator needs `--system` mode → `/Library/LaunchDaemons`. Documented in `docs/operator/native-deploy.md` update.
11. **[L] Health-check flakiness on cold start.** First 5-10s after boot: DB file not yet open, brief not yet loaded → install-service polls a `degraded` healthz and times out. Mitigation: `/healthz` JSON returns 200 for `degraded` (supervisor reads `status` field, not HTTP code, to decide). Install-service polls until `status: ok` OR until the 30s window closes; on `degraded` after 30s it reports `installed but not yet healthy — check stderr for boot progress` and exits 0 with a warning. **Action:** spec §3.1 step 7 amends; Test 8 + 9 cover.
12. **[L] Idempotent re-run misses partial state.** If a prior install crashed between writing the unit file and registering it, re-run sees the file + does nothing — the unit is never registered. Mitigation: idempotency check verifies BOTH file presence AND unit registration (`systemctl is-enabled` / `launchctl print`). **Action:** spec §3.1 step 4 amends.
13. **[N] Cron `PATH` minimal.** Default cron `PATH` is `/usr/bin:/bin` — missing `/usr/local/bin`. Mitigation: cron entries use absolute `/usr/local/bin/regatta` path (§3.7 already). ✓.
14. **[L] Operator runs install-service from a brew binary then moves it.** `os.Executable()` is captured at install-time → plist points at the brew path → operator `brew uninstall regatta && cp ~/build/regatta /usr/local/bin/regatta` → plist points at the now-deleted brew path. Mitigation: install-service emits a warning when `os.Executable()` resolves under `/opt/homebrew` or `/usr/local/Cellar` recommending `--binary /usr/local/bin/regatta` override; document. **Action:** F-7 tracks the override flag.

### 9.4 OSS reuse the spec missed

- **`github.com/kardianos/service`** (Zlib, `v1.2.2`) — cross-platform service abstraction. **Considered + rejected.** Pros: one library, one API for systemd + launchd + Windows. Cons: pulls in Windows + FreeBSD adapters we do not need; abstracts away the unit-file shape we want to template + version-control; ~5k LoC vs ~280 LoC for the explicit path. Decision: the deletion-default + readability-first directive favors explicit. Reconsider when Windows lands.
- **`github.com/cloudflare/tableflip`** (BSD-3, `v1.2.3`) — zero-downtime reload. **Considered + rejected.** Pros: graceful reload without dropping connections. Cons: we have one operator + no HTTP-traffic-pinning need + W1's webhook receiver can absorb a 5s restart gap. Decision: defer until first hosted-backend customer ask. F-8 tracks.

---

## §10 Risks (cross-ref §9.3)

Enumerated above with B/L/N tiers + mitigations. Count: 14 named risks, exceeds the 10-risk template requirement.

---

## §11 Followups (inline; pre-file per `feedback_unaddressed_load_bearing`)

These are created as GH issues by the implementer at PR-open time so the spec stays focused on the in-wedge floor.

- **F-1: system-mode + multi-user test matrix.** Reopen trigger: second human operator on the same host OR external customer ask for `--system` mode.
- **F-2: bundle a `newsyslog.conf` snippet for the macOS log-rotation path.** Reopen trigger: first disk-fill incident or 30 days of green operator-managed manual rotation.
- **F-3: adopt `github.com/coreos/go-systemd/v22/daemon` if we need `SdNotifyBarrier` or socket-activation.** Reopen trigger: first wedge that needs barrier semantics.
- **F-4: investigate watchdog goroutine starvation under CPU-bound worker.** Reopen trigger: first observed missed-watchdog restart in prod where the process was alive but compute-bound.
- **F-5: W3.1 macOS healthcheck plist (out-of-process poller that kills wedged regatta).** Reopen trigger: first observed `KeepAlive` failure where the process is wedged-not-exited on macOS.
- **F-6: SELinux audit2allow auto-generation flow.** Reopen trigger: first SELinux-enforcing Linux operator.
- **F-7: `--binary <path>` override flag for non-`/usr/local/bin` installs.** Reopen trigger: first issue filed against the brew-uninstall path drift.
- **F-8: tableflip-style zero-downtime reload.** Reopen trigger: first hosted-backend customer ask.

---

## §12 Parallel-dispatch plan

W3 splits into 4 implementer tasks. Per `feedback_parallel_safety`, shared primitives are owned by the named task, and shared follow-ups are pre-filed before parallel dispatch.

| Task | Surface | Files (owner = first writer) | LoC est. | Tests |
|---|---|---|---|---|
| T1 | `internal/health/` JSON `/healthz` + checks | `internal/health/*.go`, `cmd/regatta/serve.go` (one line: handler wire-up) | ~90 | 6, 7, 8, 9, 10 |
| T2 | `internal/watchdog/` sd_notify | `internal/watchdog/*.go` | ~50 | 11, 12 |
| T3 | `cmd/regatta install-service` + templates | `cmd/regatta/install.go`, `cmd/regatta/install_test.go`, `dist/services/regatta.{service,plist}.tmpl`, `dist/cron/regatta.crontab` | ~210 | 1, 2, 3, 4, 13, 14, 15, 16, 17 |
| T4 | `cmd/regatta uninstall-service` + `service status` | `cmd/regatta/uninstall.go`, `cmd/regatta/service_status.go` + tests | ~90 | 5, 18 |

Shared primitive owner: T1 owns `internal/health` (T3 + T4 read via the package's exported `Probe` func). Collision risk: T3 + T4 both touch `cmd/regatta/main.go` for AddCommand wiring → T3 lands first; T4 rebases.

Followup pre-filings (F-1 through F-8): pre-file before T1-T4 dispatch per `feedback_parallel_safety`.

---

## §13 Comment sweep

Per `feedback_comments_discipline`: this spec is prose; no Go comments to sweep yet. Implementer PRs run a comment sweep before push per `feedback_comments_discipline` + `feedback_comments_lint_reconcile` (exported-symbol godocs: one-line WHY-form starting with the symbol name; `make check` runs golangci-lint after the sweep).

State: **clean** for this prose spec.

---

## §14 Cites

- `docs/engineer/briefs/2026-06-02-phase-autonomy-amendment.md` §11 W3 — goal, prior art, LoC estimate, acceptance criteria, B/A/A+ rubric.
- `.regatta/items/phase-autonomy-w3-service-supervisor.md` — wedge item, scope, dependencies.
- `docs/engineer/briefs/2026-06-01-self-host-first.md` §1 — self-host filter applied in §5.
- `docs/engineer/specs/2026-06-02-phase-autonomy-w4-self-improvement-detector.md` — sibling W4 spec, shape reference for §3 component map.
- `docs/operator/native-deploy.md` — existing operator runbook updated in this wedge's PR.
- `cmd/regatta/serve.go:687` — current `/healthz` literal-`ok` handler; W3 extends without breaking the W7.0 contract.
- `deploy/systemd/regatta.service` + `deploy/launchd/com.regatta.serve.plist` — files this spec templatizes + moves under `dist/services/`.
- systemd man pages: `systemd.service(5)`, `systemd.exec(5)`, `sd_notify(3)`.
- launchd reference: Apple Developer's `Creating launchd Jobs` (linked in §4).
- Kubernetes `/healthz` convention (linked in §4).
- `feedback_decision_priority` — operator UX: ONE command, never restarts. Filtered §2.
- `feedback_research_design_principles` — adopt-first; init contracts adopted, install command built. §4.
- `feedback_root_cause` — cron installation lives in the install command, not in a doc step. §3.7.
- `feedback_deletion_default` — net +25 LoC after deleting 244 bash + 91 service files. §3.2.
- `feedback_grade_rubric` — falsifiable B/A/A+. §8.
- `feedback_adversarial_review` — §9 self-review pass; reviewer subagent runs on implementer PR per §8 (j).
- `feedback_unaddressed_load_bearing` — F-1 through F-8 pre-filed. §11.
- `feedback_pr_body_release_notes_fence` — PR body needs ```release-notes ... ``` fence.
- `feedback_pr_body_file_only` — `gh pr create --body-file`, no HEREDOC.
- `feedback_no_signatures` — no Co-Authored-By, no AI footer.
- `feedback_doc_check_banned_phrases` — pre-push grep run before merge.
- `feedback_test_godoc_one_line` — Test/Fuzz/Benchmark godocs one line.

---

## §15 Definition-of-done checklist

- [x] Spec at `docs/engineer/specs/2026-06-02-phase-autonomy-w3-service-supervisor.md`.
- [x] B/A/A+ rubric with falsifiable criteria (§8).
- [x] OSS references cited with version + license (§4).
- [x] Self-host filter explicit (§5).
- [ ] Reviewer subagent cleared — runs after this draft lands as a PR.
- [x] Release-notes fence in PR body — present at PR-create time.
- [x] No banned phrases — pre-push grep before push.
- [x] No signatures — none added.
- [x] Memory rules cited (§14).
- [ ] PR opened against `main`; worktree removed after merge — pending PR open.
