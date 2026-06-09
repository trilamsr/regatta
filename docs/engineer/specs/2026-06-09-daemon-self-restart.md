---
title: "Daemon self-restart on binary update (#1079)"
status: design
phase: self-host-s2
issue: 1079
summary: Add a binary-mtime watcher to `regatta serve` so the daemon notices when its own on-disk binary has been replaced, logs `serve.binary_updated_detected`, and (under opt-in flag `--auto-restart-on-update=<delay>`) cancels the parent ctx so the existing graceful-shutdown path drains and the supervisor (`docker compose` / `systemd` / `launchd`) relaunches with the new bytes. No new restart primitive — re-use the existing supervisor + existing `signal.NotifyContext` drain.
---

# Daemon self-restart on binary update — Spec

Memory rules in force: `feedback_default_simpler`, `feedback_no_signatures`, `feedback_cite_origin_main_not_local`, `feedback_research_design_principles`.

```release-notes
[DOCS] specs: daemon self-restart design (#1079)
```

## Problem

The 2026-06-08 dogfood session ran `regatta serve` for 17h45m. During that window the orchestrator merged 6 PRs that hardened its own behaviour. The on-disk binary was never reloaded — every fix landed in `main` but stayed dormant in the running daemon. The operator workaround was three rounds of manual `kill -TERM <pid>` + rebuild + restart in one session (#1079).

`regatta serve` has no mechanism today to notice that `os.Executable()` (or `/regatta` inside the container) has been replaced:

- `make build && cp ./regatta /usr/local/bin/regatta` does not affect the running daemon.
- `docker compose build regatta` leaves the existing container running the cached image until manual `docker compose up -d --build regatta`.
- The signal-handling surface is already crowded — `SIGINT`/`SIGTERM` drive graceful shutdown (`cmd/regatta/serve.go:164`), `SIGHUP` is claimed by both the secrets cache (`cmd/regatta/wire_secrets.go`) and the OPA policy reloader (`cmd/regatta/wire_authz.go:76`), and the operator-facing `regatta reload-secrets` CLI signals into the lockfile PID (`cmd/regatta/reload_secrets.go:70`). A new SIGHUP overload is contraindicated.

Net delta the spec must produce: with the daemon running and a fresh binary on disk, the running daemon exits cleanly within `detect_interval + drain_budget` seconds, and the supervisor that's already restarting the process on exit (`docker-compose.yml:52` `restart: unless-stopped`; systemd `Restart=always`; launchd `KeepAlive=true`) brings the new binary up. Operator never reaches for `kill`.

## Design

Adopt **Approach A — supervisor-driven exit-on-update** per the feasibility brief at `docs/engineer/research/2026-06-08-self-restart-on-binary-update.md` (recommendation §Approach comparison). Reject Approach B (in-process `syscall.Exec`) and Approach C (cloudflare/tableflip zero-downtime) as Phase-X forward-fit per `feedback_default_simpler`.

### Detection (#1079 c1)

A new `binwatcher` goroutine boots next to `secrets.Cache.Run` in `serve.go`. It owns its own `time.Ticker` at `--binwatch-interval` (default 60 s) and stores the boot-time mtime of `os.Executable()`. On each tick:

1. `os.Stat(exePath)` — error logs a `serve.binwatcher_stat_failed` WARN with the err message and continues (does NOT exit; transient ENOENT during atomic replace is normal).
2. Compare `info.ModTime()` against the stored baseline. Equal → continue.
3. Advance → emit `serve.binary_updated_detected` WARN with attrs `path`, `old_mtime`, `new_mtime`, `auto_restart_in` (the configured delay, or `"never"` when log-only). Update the stored baseline so subsequent ticks don't re-fire on the same change.

Default behaviour: log only. No ctx cancel. Operator who wants the observability without the restart gets it free.

### Drain + restart (#1079 c2)

New flag wired through `wire_flags.go`:

```
--auto-restart-on-update=<delay>   default: 0  (log-only)
--binwatch-interval=<duration>     default: 60s
```

When `--auto-restart-on-update` is non-zero, the detector arms a `time.AfterFunc(delay, stop)` where `stop` is the `signal.NotifyContext` cancel returned from `cmd/regatta/serve.go:164`. The delay buys the operator a window to abort (`kill -INT`) if the update was unintended.

After `stop()` fires:

- Parent ctx cancels → `Orchestrator.Run` exits its select loop (`internal/orchestrator/orchestrator.go:106`).
- Deferred `httpSrv.Shutdown(shutdownCtx)` drains `:8080` within `listenerShutdownBudget` (5 s, `cmd/regatta/serve.go:37`).
- Deferred cost-reconciler join waits up to `reconcilerShutdownBudget` (5 s).
- `runServe` returns `0`; `main` exits `0`.
- Supervisor sees a clean exit and relaunches: `restart: unless-stopped` for docker compose, `Restart=always` for systemd, `KeepAlive=true` for launchd. The new process picks up the new bytes.

No new drain logic. The existing graceful-shutdown contract carries the load.

### Docker path (#1079 c3)

Per the research brief §Recommendation, the c3 sidecar (`cmd/regatta-watcher`) is deferred to a follow-up issue. Rationale:

- Inside the regatta container, `/regatta` lives on the image's overlay FS. `docker compose build regatta` produces a new image but the running container's `/regatta` mtime does not change — the container holds the old layer. The mtime watcher fires only on bare-metal / installed-binary paths.
- `docker compose up -d --build regatta` rebuilds + replaces the container in one step. The new container boots with the new binary; the watcher never has to fire.
- The sidecar would poll `docker pull <image>` every 5 m + trigger `docker compose restart regatta` on digest change. That is a separate concern (registry-based deployment) and adds Docker socket access — a security surface this spec is unwilling to introduce without an external operator asking.

Operator-facing docs (`docs/operator/docker-compose.md`) document the recommended docker refresh loop: `docker compose up -d --build regatta`. The sidecar reopen-trigger is "operator runs `docker compose up -d --build` ≥10×/week for ≥2 weeks" — file as `binwatcher sidecar (#TBD-followup)` against #1079 follow-up.

### Detection — design decisions

**60 s mtime poll, not fsnotify**. Trade-off table in research brief §Detection mechanism. Mtime poll is cross-platform (darwin/linux/windows), trivial, dep-free. Self-host operator merges + rebuilds at most every few minutes; 60 s is invisible. fsnotify adds a dep + platform quirks (Windows `ReadDirectoryChangesW` rename-then-replace semantics) for latency the wedge does not need. Per `feedback_default_simpler`.

**Mtime, not hash**. Operator pushback hint in the task brief: "Detection should only fire when the binary HASH changes, not mtime." Counter: `make build` always advances mtime (the linker writes the file fresh) and `cp -p` preservation is not a realistic update path — operators rebuild, they don't preserve-mtime-copy. Hashing the binary on every tick is wasted I/O (~30-80 MB of read per minute). **Decision: mtime is the trigger; the spec acknowledges the false-positive class and rules it acceptable.** A follow-up issue may add a `--binwatch-strategy=hash` flag if container-redeploy false-positives become real (re-open trigger: ≥1 operator-filed "container restarted without binary change" against the watcher within 30 days). Sidecar-driven container path is covered by c3 deferral above; in-container path is also covered by the second adversarial point below.

**`os.Executable()` resolution**. On linux, this resolves via `/proc/self/exe` — a kernel-managed symlink that points at the binary's inode at the time of `exec()`. After atomic replace, `/proc/self/exe` still resolves to the new path string but the inode behind it changed. We read `info.ModTime()` of the path, not the symlink target, so the new mtime is observed. On darwin, resolution is argv0-based — the new file at the same path is observed. Document the linux behaviour in a code comment so the next reader does not "fix" it.

### Observability

One new event constant added to `internal/obs/events.go`, in the `serve.*` family alongside `EventAgentExited` (the closest existing taxonomic neighbour):

```
EventBinaryUpdatedDetected EventName = "serve.binary_updated_detected"
```

Add to `AllEventNames()`. The events_test.go schema test re-runs.

Attribute keys: re-use `KeyReason` for the "log-only" / "auto-restart-armed" disposition string. New keys avoided per `internal/obs/events.go` policy ("anything not listed lives under attrs.\* so dashboards do not break"). Path + mtimes ship as `attrs.path`, `attrs.old_mtime_ns`, `attrs.new_mtime_ns`, `attrs.auto_restart_in_ms`.

### OSS prior art surveyed

Cross-checked against well-known long-lived-daemon restart patterns. All four reject in-process re-exec for this self-host wedge:

| Daemon | Reload mechanism | Source | License | Why this spec does not adopt |
| --- | --- | --- | --- | --- |
| nginx 1.27 | `nginx -s reload` sends `SIGHUP` to the master; master forks new workers + drains old | <https://nginx.org/en/docs/control.html> tag `release-1.27.4`, <https://github.com/nginx/nginx/blob/release-1.27.4/LICENSE> (BSD-2-Clause) | SIGHUP-driven config reload; does not re-exec the master itself. Closest analog to the regatta SIGHUP overload we are explicitly avoiding. |
| PostgreSQL 17 | `pg_ctl reload` sends `SIGHUP` to the postmaster for config only; binary upgrade is `pg_upgrade` offline | <https://www.postgresql.org/docs/17/app-pg-ctl.html>, <https://github.com/postgres/postgres/blob/REL_17_STABLE/COPYRIGHT> (PostgreSQL License, BSD-style) | Confirms upstream practice: long-lived data-plane daemons do NOT in-process re-exec across binary upgrade; they cleanly stop and let the supervisor restart. |
| kubelet (Kubernetes 1.31) | Process restart on binary update is delegated to the host init system (systemd unit); kubelet itself does not watch its binary | <https://kubernetes.io/docs/setup/production-environment/tools/kubeadm/install-kubeadm/>, <https://github.com/kubernetes/kubernetes/blob/release-1.31/LICENSE> (Apache-2.0) | Exact pattern this spec adopts: detect → exit clean → supervisor relaunches. kubelet is the proof-of-life for Approach A at scale. |
| HashiCorp Vault 1.18 | `SIGHUP` reloads config + audit devices; binary upgrade is a clean stop + supervisor relaunch | <https://developer.hashicorp.com/vault/docs/commands/server#signals>, <https://github.com/hashicorp/vault/blob/v1.18.3/LICENSE> (BUSL-1.1) | Same shape as PostgreSQL: SIGHUP is config-only, not re-exec. |
| watchexec 2.2 / nodemon 3.1 | External file-watcher daemons that send signal + relaunch | <https://github.com/watchexec/watchexec/blob/v2.2.1/LICENSE> (Apache-2.0 / MIT), <https://github.com/remy/nodemon/blob/v3.1.7/LICENSE> (MIT) | Inverse pattern: external watcher manages a non-self-aware child. The regatta wedge wants the daemon to notice itself so a stock supervisor with no file-watching DSL works. nodemon-style is rejected; the watcher lives in-process. |

Reference for the rejected Approach C: <https://github.com/cloudflare/tableflip>, <https://github.com/cloudflare/tableflip/blob/v1.2.3/LICENSE> (BSD-3-Clause). Adds ~2k LOC for true zero-downtime FD handoff. Self-host single-operator has no client behind `:8080` that notices a 2 s blip; the dep-cost is not earned. Per `feedback_default_simpler`. **Implementer constraint**: the impl PR MUST NOT add `github.com/cloudflare/tableflip` to `go.mod`; the reviewer-verdict gate's deletion-default lens covers this and the spec pins it here so the implementer cannot rationalize the dep as "the spec rejected Approach C but didn't forbid the import".

### What gets smaller

This spec adds an event constant + a goroutine + two flags. Nothing gets smaller — pure addition. The A+ defence per `feedback_deletion_default`: the surface area increase is ~60 LoC across `cmd/regatta/serve.go` + `cmd/regatta/wire_flags.go` + a new test file; the deletion it earns downstream is the manual `kill -TERM + rebuild + cp` loop the operator ran three times in one 17h45m session (#1079 Symptom). The follow-up that re-opens the c3 sidecar is also a candidate to delete this spec's docker caveat — when an external operator triggers the reopen, the prose moves into the sidecar spec.

## Acceptance

`c1` — Detection (log-only default).

- [ ] `regatta serve` boots a binwatcher goroutine that stat's `os.Executable()` every `--binwatch-interval` (default 60 s).
- [ ] Stat error emits `serve.binwatcher_stat_failed` WARN + continues (does not exit).
- [ ] First mtime-advance emits `serve.binary_updated_detected` WARN with `path`, `old_mtime`, `new_mtime`, `auto_restart_in` attrs.
- [ ] Default behaviour (`--auto-restart-on-update=0`) is log-only — parent ctx is NOT cancelled.
- [ ] BOTH `EventBinaryUpdatedDetected` AND `EventBinwatcherStatFailed` are declared in `internal/obs/events.go` and registered in `AllEventNames()`; `events_test.go` schema test passes. (Reviewer a14732cccb3075e65 caught the missing `EventBinwatcherStatFailed` constant — every event emit MUST go through the obs vocabulary per the package header.)

`c2` — Drain + restart on flag.

- [ ] `--auto-restart-on-update=<delay>` (default 0) wired through `wire_flags.go`.
- [ ] On mtime-advance with non-zero delay, a `time.AfterFunc(delay, stop)` fires where `stop` is the `NotifyContext` cancel.
- [ ] Parent ctx cancel drives the existing graceful-shutdown path — `httpSrv.Shutdown` within `listenerShutdownBudget`, cost-reconciler join within `reconcilerShutdownBudget`, no new drain primitive.
- [ ] `runServe` returns `0`; `main` exits `0`. `state.db` closes cleanly (post-exit `state.db-wal` ≤ 1 KB).
- [ ] Operator can abort the armed restart with `kill -INT <pid>` before the delay elapses; ctx-cancel collapses both paths to the same exit-0 drain.

`c3` — Docker sidecar. **Deferred** to a follow-up issue per §Design > Docker path. Documented in `docs/operator/docker-compose.md` (operator runs `docker compose up -d --build regatta`). Reopen-trigger filed against #1079 follow-up.

`c4` — Regression tests.

- [ ] Unit `cmd/regatta/serve_binwatcher_test.go`:
  - RED then GREEN: `binwatcher.tick(now, stat{mtime: t1})` returns `Changed=true` when `t1 > seenAt`.
  - RED then GREEN: returns `Changed=false` when mtime unchanged.
  - RED then GREEN: stat error path emits WARN + continues; no goroutine leak under `goleak.VerifyNone` on ctx cancel mid-poll.
  - RED then GREEN: log-only mode emits the event but does NOT cancel parent ctx.
  - RED then GREEN: `--auto-restart-on-update=100ms` cancels parent ctx within `100ms + tick interval` of the mtime change (fake clock).
- [ ] Integration `cmd/regatta/serve_binwatcher_e2e_test.go` (`//go:build !windows`):
  - Boot daemon in `t.TempDir`, mutate binary mtime via `os.Chtimes`, assert process exits 0 within `delay + 5 s` of detection.
  - Post-exit assertion: `state.db-wal` ≤ 1 KB.
- [ ] Existing `serve_authz_reload_test.go::TestServe_AuthzPolicyHotReload_OnSIGHUP` still passes (proves we did not steal SIGHUP).
- [ ] Existing `reload_secrets_test.go::TestReloadSecrets_SendsSIGHUPToPidFromLockfile` still passes (same).
- [ ] First commit in PR is the RED test commit (TDD discipline; failing output pasted in PR body).

## Out of scope

- In-process re-exec (`syscall.Exec`). Documented under §Design > Approach comparison; reopen-trigger is "an operator deploys `regatta serve` without a supervisor (no compose, no systemd, no launchd) and asks for self-update". Bare-shell invocation today logs the cause and exits; the operator manually reruns. Acceptable for self-host.
- Zero-downtime FD handoff (cloudflare/tableflip). Reopen-trigger: an external client behind `:8080` reports observable downtime AND that client's SLO contractually rules out a 2 s blip.
- `cmd/regatta-watcher` docker sidecar (#1079 c3). Reopen-trigger above.
- Hash-based detection. Reopen-trigger above.
- `regatta self-update` CLI subcommand (operator-initiated SIGUSR1). Punt — the supervisor + watcher already eliminate the operator interaction this would automate.
- Container image-pull / registry-poll behaviour. Same surface as the sidecar deferral.
- Multi-binary deployments (`regatta` + `regatta-watcher` + `regatta-alarm-webhook` all updating in lockstep). The alarm-webhook and other binaries get the same watcher in a follow-up; this spec ships the daemon path only.

## Adversarial

Independent reviewer must hunt these before APPROVE.

1. **Re-exec mid-spawn → orphaned subagents.** The Approach A exit is clean from regatta's perspective, but in-flight `claude` child processes spawned for active worktrees survive the parent exit and get reparented to init (PID 1). On supervisor relaunch, the new regatta sees `state.db` rows in `running` with PIDs that are still alive but no longer its children. **Mitigation**: this is the existing crashed-agent recovery path (`internal/orchestrator/orchestrator.go::Recover` requeues `running` rows on boot). The supervisor-relaunched daemon reattaches via the same lock-then-reconcile pass. Document this explicitly so the implementer does not invent a new drain step. **Sharper risk**: if the new binary's `state.db` schema is incompatible with rows the orphaned children continue to write, recovery panics. **Pin**: the spec MUST NOT ship alongside a schema migration; the implementer PR MUST audit `git ls-tree origin/main internal/orchestrator/state/migrations/` at impl-PR-open time, cite the head SHA in the PR body, and confirm no migration landed between this spec's merge and the impl PR. Recent example: #1108 landed schema-impacting changes on 2026-06-08; the impl PR holds the restart-on-update merge until any in-flight migration is either landed or the schema is byte-equal pre/post.

2. **Container mtime false-positive.** Container redeploys never touch the running container's `/regatta` mtime (the image is layered FS), so this is a *false negative*, not false positive — the watcher is silent in-container. Reverse direction: if an operator bind-mounts `./regatta:/regatta` (development pattern, against the documented docker-compose deploy shape), every `make build` on the host triggers an exit. **Mitigation**: document the bind-mount caveat in `docs/operator/docker-compose.md`; the bind-mount path is the *intended* development loop, so the exit-on-rebuild is the desired behaviour — not a bug.

3. **mtime preserved by `cp -p` / `rsync -t`.** Operator who copies a new binary with mtime preservation (`cp -p`, `install -p`, `rsync --times`) leaves mtime unchanged and the watcher misses the update. **Mitigation**: documented in the brief; `make build` always advances mtime (linker writes fresh). Operators wanting to deploy preserved-mtime binaries must arm `--binwatch-strategy=hash` (deferred to follow-up).

4. **Ticker collision with `signal.NotifyContext`.** The `time.AfterFunc(delay, stop)` fires on the runtime's timer goroutine; `signal.NotifyContext`'s `stop` is documented safe to call from any goroutine. **Mitigation**: explicit unit test (`c4`) asserts the cancel path via fake-clock-fired AfterFunc.

5. **Detect-then-restart race**. Two near-simultaneous binary updates (operator runs `make build` twice within 60 s) only fire once because the baseline mtime is bumped after the first detection. **Mitigation**: this is intentional. The first detection arms the restart timer; subsequent updates ride the restart that's already coming. Document the semantic.

6. **`os.Executable()` returns the wrong path.** On linux, `/proc/self/exe` is a symlink to the file the kernel exec'd, not the path the operator updated (if the operator overwrote `/usr/local/bin/regatta` but PID was exec'd from `./regatta` in cwd, the watcher polls the cwd path). **Mitigation**: this matches the operator's mental model — the watched path is "the binary that produced this PID". If installation copied the binary to a system path, the operator runs from that path, and `os.Executable()` resolves to it. Pin the behaviour in a code comment.

7. **Supervisor not actually configured to restart.** Bare-shell `./regatta serve` with no supervisor exits 0 on detection and the daemon is gone. **Mitigation**: the WARN log explicitly says "if your supervisor is not configured to relaunch, this process will exit". `docs/operator/install.md` calls out the supervisor requirement. Acceptable failure mode: operator-visible.

8. **Reopen-trigger discipline (`feedback_default_simpler`).** Three deferrals (c3 sidecar, hash strategy, self-update CLI) each carry a written reopen-trigger that is mechanically observable (count of operator-filed issues / count of `docker compose up -d --build` invocations / external-client SLO). No "we'll see when it matters" handwave.

9. **Cite origin/main (`feedback_cite_origin_main_not_local`).** Every internal path cited above (`cmd/regatta/serve.go:164`, `internal/orchestrator/orchestrator.go:106`, `docker-compose.yml:52`, `internal/obs/events.go`, `docs/engineer/research/2026-06-08-self-restart-on-binary-update.md`, `docs/engineer/briefs/2026-06-01-self-host-first.md`) resolves at `git ls-tree -r origin/main --name-only | grep <path>` time of spec-write. OSS-prior-art table cites tagged-release LICENSE URLs, not GitHub topic chips. Implementer rerunning the commands gets the same results.

## Implementer brief

Per `docs/engineer/dispatch-templates/implementer.md` shape.

**Scope**: c1 + c2 + c4 only. c3 is filed as a follow-up tracker issue before this PR merges; do NOT ship the sidecar in the same PR.

**Touch list (estimated)**:

- `cmd/regatta/serve.go` — boot the binwatcher goroutine next to `secrets.Cache.Run`; thread the `stop` cancel from `signal.NotifyContext`.
- `cmd/regatta/wire_flags.go` — register `--auto-restart-on-update` (`time.Duration`, default `0`) and `--binwatch-interval` (`time.Duration`, default `60s`).
- `cmd/regatta/binwatcher.go` (NEW) — `BinWatcher` struct with `Tick(now time.Time, stat fs.FileInfo) Outcome` pure method + `Run(ctx, interval, delay, stop)` driver. Pure-method shape is the unit-test seam.
- `cmd/regatta/binwatcher_test.go` (NEW) — RED tests per `c4` unit list. Use `clockwork.FakeClock` or the existing `internal/testutil` fake-clock primitive — match whichever the surrounding tests use (verify before writing).
- `cmd/regatta/serve_binwatcher_e2e_test.go` (NEW) — integration test per `c4`. Guard with `//go:build !windows`.
- `internal/obs/events.go` — add BOTH `EventBinaryUpdatedDetected = "serve.binary_updated_detected"` AND `EventBinwatcherStatFailed = "serve.binwatcher_stat_failed"` to the constants block + `AllEventNames()`.
- `internal/obs/events_test.go` — picked up by the schema-reflection test automatically; no edit unless the test asserts a specific count.
- `docs/operator/docker-compose.md` — bind-mount caveat + `up -d --build` refresh loop.
- `docs/operator/container.md` — single-line note: the watcher fires on bare-metal / install-service paths, not in-container.

**Commit order (TDD)**:

1. RED test commit: `cmd/regatta/binwatcher_test.go` with the four unit cases failing. Paste failing output in PR body.
2. Impl commit: `cmd/regatta/binwatcher.go` to GREEN.
3. Wire commit: `cmd/regatta/serve.go` + `cmd/regatta/wire_flags.go` + event constant.
4. Integration test commit: e2e file.
5. Docs commit.

**Rules to honour** (cite slug in PR body):

- `feedback_default_simpler` — do not pre-build hash strategy / sidecar / SIGUSR1 path. The spec defers them with explicit reopen triggers; the implementer adds NOTHING beyond c1 + c2 + c4.
- `feedback_no_signatures` — no `Co-Authored-By`, no AI attribution in commits / PR body / code comments.
- `feedback_comments_discipline` — WHY not WHAT; the binwatcher loop body needs one comment explaining the linux `/proc/self/exe` resolution behaviour. Nothing else.
- `feedback_test_godoc_one_line` — every `TestX` godoc is one line, format `// TestX asserts O on I (#1079).`
- `feedback_keep_orchestrator_branch_name` — do not `git checkout -b <semantic>` inside the worktree. Push under the orchestrator-assigned `regatta/agent-<N>` ref.

**Reviewer dispatch**: load-bearing PR (touches `cmd/`, `internal/obs/events.go`). Reviewer subagent MUST run before APPROVE token lands in PR body. Reviewer focus: the 9 adversarial points above plus a fresh edge-case sweep against the cited line refs at `origin/main`.

**CI gates that will run**:

- `make check` (full target list) — comment-density on the new files (<5%), banned-phrase, doc-links (the OSS-prior-art table URLs are external, not intra-repo, so doc-link gate ignores them).
- `make check-no-bare-sleep` — the e2e test MUST poll via `testutil.Eventually` / `testutil.EventuallyT`, not `time.Sleep` inside `for`.
- `pr-lint-reviewer-verdict` — independent reviewer ID + APPROVE required.

## Reopen trigger

Reopen this spec (re-promote `status: design` to add a section) when ANY of the following is observed:

- An external operator deploys `regatta serve` without a supervisor and files a "self-update without supervisor" request → reopen Approach B section.
- A non-self-host client (gates `:8080`, web UI consumer) reports an observable downtime SLO incident attributable to the supervisor-restart blip → reopen Approach C section.
- Operator files ≥1 "container restarted without binary change" incident against the watcher within 30 days of v1 ship → reopen hash-strategy section.
- Operator runs `docker compose up -d --build regatta` ≥10×/week for ≥2 weeks → reopen sidecar section and spawn `cmd/regatta-watcher` implementer. **How the operator knows**: shell-history audit (`history | grep -c "docker compose.*--build regatta"` over a 7-day window). The threshold is operator self-rating, not an in-orchestrator metric — the operator action lives outside regatta. Acceptable proxy: the operator notices they are running the same one-liner often enough to be irritating.

Closing trigger: c1 + c2 + c4 ship, the watcher is observed firing once on a real operator-driven `make build && cp` cycle without manual `kill`, and the deferral tracker issues are filed. Move spec frontmatter to `status: shipped` only after the post-merge soak observation lands in a session retro.
