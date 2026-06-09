# Self-restart on binary update — feasibility + plan (#1079)

Status: research brief (informs implementation)
Date: 2026-06-08
Scope: Close the gap exposed by the 2026-06-08 dogfood session — `regatta serve` ran 17h45m against a stale binary while the orchestrator merged 6 PRs that fixed its own behaviour. Operator workaround was manual SIGTERM + rebuild + restart, repeated three times in one session.

## Problem statement

`regatta serve` is a long-lived daemon. Today it has no mechanism to notice that its on-disk binary (`os.Executable()`, or `/regatta` in container) has been replaced. Symptoms observed in #1079:

- Operator runs `make build && cp ./regatta /usr/local/bin/regatta` (or docker rebuild) → no effect on the running daemon.
- Operator must `kill -TERM <pid>` then restart by hand.
- In the docker path, even `docker compose build regatta` leaves the existing container running the cached image until manual `docker compose up -d --build regatta`.

Net: every fix the orchestrator produces lands in main but NOT in the running daemon. The daemon's behaviour is frozen at boot.

## Existing signal-handling surface

- `cmd/regatta/serve.go:164` — `signal.NotifyContext(ctx, SIGINT, SIGTERM)` drives graceful shutdown. `Orchestrator.Run` (`internal/orchestrator/orchestrator.go:106`) loops on `ctx.Done()`; defers cancel poll/tick/heart tickers cleanly.
- `cmd/regatta/wire_secrets.go` — `secrets.Cache.Run` claims `SIGHUP` for atomic credential re-resolve (uses its own `signal.Notify` channel, does NOT steal from the parent `NotifyContext`).
- `cmd/regatta/wire_authz.go:76` — OPA policy bundle reloader also listens on `SIGHUP` via its own channel.
- `cmd/regatta/reload_secrets.go:70` — operator-facing CLI `regatta reload-secrets` sends `SIGHUP` to the PID read from the lockfile.

Implication: `SIGHUP` is already overloaded (secrets + OPA bundle). A new signal (or a new flag-gated detector goroutine) is required for self-update.

## Approach comparison

### Approach A — operator-level supervisor restart (do nothing in-binary)

Rely on the supervisor that already restarts the process on exit: `docker-compose.yml:52` declares `restart: unless-stopped`; systemd unit files (`cmd/regatta/install_service.go`) typically declare `Restart=on-success`/`Restart=always`; launchd `KeepAlive=true`. To self-update, the daemon only has to `os.Exit(0)` when it notices its binary changed; the supervisor relaunches with the new bytes.

Pros:
- Zero new restart machinery in-process. The hard part (re-exec, FD inheritance, port handoff) is delegated to a primitive that's already proven (compose, systemd, launchd).
- Works in docker with no compose-file change — `restart: unless-stopped` already does it.
- Aligns with the self-host filter: single-tenant, single-operator, deterministic CI. No multi-tenant zero-downtime hot-swap requirement.
- Reuses existing graceful shutdown path (`ctx` cancel → `Orchestrator.Run` exits → deferred `httpSrv.Shutdown` + reconciler drain).

Cons:
- 1–3 s window where `:8080` is unbound between `os.Exit` and the supervisor's relaunch. Acceptable for self-host (operator is the only client).
- Direct `./regatta serve` invocations from a shell with no supervisor never auto-restart — operator sees exit 0 and must rerun. Mitigation: log the cause loudly + document the supervisor requirement.

### Approach B — in-process re-exec via `syscall.Exec`

After detecting binary update, call `syscall.Exec(os.Args[0], os.Args, os.Environ())` to replace the current process image with the new binary, preserving PID and inherited FDs.

Pros:
- No supervisor dependency. Works for bare `./regatta serve` runs.
- PID stable across upgrades → external monitoring (pidfile in `reload_secrets.go`) survives.

Cons:
- All in-process state must drain BEFORE the exec call: `state.db` writer goroutines, OPA bundle watcher, secrets `Cache.Run`, cost reconciler, alarm-webhook listener, OTel exporter (otherwise telemetry loses 30s of buffered metrics). Each subsystem owns its own defer in `runServe`; re-exec bypasses them all unless we manually invoke shutdown first.
- Open listener FD handoff is non-trivial. `httpSrv.ListenAndServe` is on a goroutine; `syscall.Exec` keeps the FD open across exec but the new process must `net.FileListener` to reattach — wiring is brittle.
- Worktree-held child processes (Claude Code spawns) survive the exec; if the new binary's lock-table schema differs, recovery panics. Requires every active agent to be drained first (operator-blocking, because some Claude runs are 30+ min).
- Windows: `syscall.Exec` is a no-op stub (returns ENOSYS). Cross-platform requires conditional compile + a Windows path that just `os.Exit`s anyway, eliminating the only advantage over A.

### Approach C — uber-go/tableflip (graceful zero-downtime restart)

Pull in [`github.com/cloudflare/tableflip`](https://github.com/cloudflare/tableflip) (or the more-maintained `github.com/rcrowley/goagain`). The library forks a child, hands off listening FDs, drains the parent, swaps in the child as the new PID-1 worker. Zero connection drop.

Pros:
- True zero-downtime — relevant for a hot-path proxy serving thousands of QPS.

Cons:
- Adds a dep (~2k LOC); tableflip last release was 2024-02 (one year stale at time of writing).
- Solves a problem regatta does NOT have. Self-host single-operator: a 2 s `:8080` blip is invisible. We'd be paying tableflip's complexity tax for a non-feature.
- Conflicts with `feedback_default_simpler`: don't pre-build for hypothetical drift; three similar lines beat a premature abstraction. There's no second client of zero-downtime restart in regatta.

## Recommendation

**Adopt Approach A (supervisor-driven exit-on-update) for v1.** Ship a small `binwatcher` goroutine in `cmd/regatta/serve.go` that polls `os.Executable()` mtime every 60 s and, on change, cancels the parent context with a logged reason. The supervisor (`docker compose`, `systemd`, `launchd`) handles relaunch. Re-use the existing graceful-shutdown path.

Defer Approach B (`syscall.Exec`) until a non-supervised deployment is requested by a real operator. Defer Approach C (tableflip) indefinitely — Phase-X forward-fit, not self-host wedge.

Mapping to issue #1079 acceptance criteria:

- **c1 (binary mtime watch, log-only default)** — Approach A nucleus. ~30 LoC: a goroutine spawned next to `secrets.Cache.Run`, owns its own ticker, no signal collision.
- **c2 (`--auto-restart-on-update=<delay>`)** — Wired through `wire_flags.go`; default `0` = log-only (c1 behaviour). Non-zero value triggers `stop()` after the delay. Drain is whatever the existing SIGINT path already does — no new drain logic.
- **c3 (sidecar `regatta-watcher` for docker image pull)** — Out-of-scope for v1. Docker's `restart: unless-stopped` reuses the cached image; image pull is a separate concern. Punt to a follow-up issue with reopen-trigger "operator runs `docker compose up -d --build` ≥10×/week for ≥2 weeks". For now, document in `docs/operator/docker-compose.md` that the operator runs `docker compose up -d --build regatta` to refresh; the new binary inside the rebuilt image then triggers c1+c2 if mtime advanced relative to the volume-mounted bind path. Reality check: in containers, `os.Executable()` resolves to `/regatta` inside the container's overlay FS, and a rebuild replaces the entire FS layer → the running container's `/regatta` mtime does NOT change; the operator must `docker compose up -d --build` anyway. So c1 is a **bare-metal/install-service** feature; docker self-update needs the c3 sidecar OR operator `--build`. Document this distinction explicitly.

## Detection mechanism

**Picked: 60 s mtime poll** (`os.Stat(os.Executable()).ModTime()`).

| Mechanism | Pros | Cons | Verdict |
| --- | --- | --- | --- |
| 60 s mtime poll | Cross-platform (darwin/linux/windows); trivial; no deps | 1–60 s detection latency | **Pick.** Self-host operator merges + rebuilds every few minutes at fastest; 60 s is invisible. |
| inotify / fsnotify (`github.com/fsnotify/fsnotify`) | Sub-second latency | New dep; linux/darwin only (windows uses ReadDirectoryChangesW with quirks); requires watching the parent dir, not the file (rename-then-replace is the standard binary-update pattern). | Reject — extra dep + platform quirks for latency we don't need. |
| Operator-sent SIGHUP-2 (`SIGUSR1`) | Zero polling cost; explicit operator intent | Requires operator to remember the signal after every build; defeats the "walk away" UX target in #1079. | Reject as primary; keep as a backup escape hatch (`regatta self-update --send-signal`). |

Edge cases handled in the poll:
- Binary deleted then atomically replaced (`mv tmp $exe`): inode changes, mtime advances → detected.
- Binary truncated mid-write: `os.Executable()` continues to resolve via `/proc/self/exe` (linux) or argv-resolved path (darwin); poll uses argv-resolved path which sees the new mtime. Document linux `/proc/self/exe` behaviour in a code comment.
- Binary mtime equal but content different (mtime preserved by `cp -p`): not detected. Acceptable — `make build` always touches mtime.

Emission:
- `slog.WarnContext(ctx, "serve.binary_updated_detected", "path", exe, "old_mtime", t0, "new_mtime", t1, "auto_restart_in", delay)`.
- New event kind `binary_updated` written via the existing audit/events sink (constant lives next to `serve.shutdown` family).

## Test plan

Unit (in `cmd/regatta/serve_binwatcher_test.go`):
1. RED test: assert `BinWatcher.tick(now, fakeStat{mtime: t1})` returns `Changed=true` when `t1 > seenAt`.
2. RED test: assert `BinWatcher.tick(...)` returns `Changed=false` when mtime unchanged.
3. RED test: assert no goroutine leak under `goleak.VerifyNone` when ctx cancels mid-poll.
4. RED test: assert log-only mode emits the `binary_updated_detected` slog record + `binary_updated` event but does NOT cancel the parent ctx.
5. RED test: assert `--auto-restart-on-update=100ms` cancels parent ctx within `100ms + tick interval` of the mtime change (fake clock).

Integration (in `cmd/regatta/serve_binwatcher_e2e_test.go`, `//go:build !windows`):
6. Boot `regatta serve --tick-once=false --auto-restart-on-update=1s` in a t.TempDir, copy the test binary to `$tmp/regatta`, point `os.Executable()` via env override (or argv0 substitution), wait 70 s, mutate the file with `os.Chtimes(...)` to a future time, assert process exits 0 within 5 s of detection.
7. Assert state.db is closed cleanly (no `-wal` file remaining > 1 KB) after the auto-restart exit. Re-uses existing `defer db.Close()` path.

Regression (existing suites):
8. `serve_authz_reload_test.go::TestServe_AuthzPolicyHotReload_OnSIGHUP` must still pass — proves we did not steal SIGHUP from the OPA reloader.
9. `reload_secrets_test.go::TestReloadSecrets_SendsSIGHUPToPidFromLockfile` must still pass — proves we did not steal SIGHUP from the secrets reloader.

Docker validation (manual, document in `docs/operator/docker-compose.md`):
10. `docker compose up -d --build regatta` while the existing container is running; assert the container exits 0 within 60 s of the build completing AND the rebuilt container picks up the new binary. Capture as a script in `scripts/manual/test-docker-self-restart.sh` for repeatability.

## Estimated effort

- c1 (mtime watch + log-only): ~30 LoC implementation + ~80 LoC unit tests. **0.5 day.**
- c2 (`--auto-restart-on-update` flag + ctx-cancel wiring): ~20 LoC + ~40 LoC tests. Touches `wire_flags.go`. **0.5 day.**
- c4 (regression coverage): integration e2e test is the bulk. **0.5 day.**
- c3 (sidecar watcher): defer per recommendation above. If operator pulls it forward, ~40 LoC Go (`cmd/regatta-watcher/main.go`) + compose `profiles: [watcher]` change. **0.5 day.**
- Doc updates (`docs/operator/docker-compose.md`, `docs/operator/container.md`): **0.25 day.**

**Total v1 (c1 + c2 + c4 + docs): ~1.75 days single-implementer.** Sidecar c3 is opt-in follow-up.

## Open questions for implementer

- Should the new event kind go in the existing audit log or a new `serve_events` table? Likely the existing audit log (it already carries `serve.shutdown` lifecycle events) — confirm with audit-table owner before landing.
- Does `os.Executable()` resolve correctly when `regatta` is installed via `install_service.go` on darwin (launchd plist path)? Sanity test on a real darwin install before merging.

## References

- Issue #1079 — Symptom + acceptance criteria.
- `cmd/regatta/serve.go:164` — current SIGINT/SIGTERM `NotifyContext` wiring.
- `internal/orchestrator/orchestrator.go:106` — `Orchestrator.Run` ctx-cancel exit path.
- `cmd/regatta/wire_secrets.go`, `cmd/regatta/wire_authz.go`, `cmd/regatta/reload_secrets.go` — existing SIGHUP claimants (shows why a new signal is contraindicated).
- `docker-compose.yml:52` — `restart: unless-stopped` (Approach A enabler).
- `feedback_default_simpler` — rejection rationale for tableflip / Approach C.
- `docs/engineer/briefs/2026-06-01-self-host-first.md` §1 — self-host filter that scopes us to Approach A for v1.
