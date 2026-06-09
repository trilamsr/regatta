# Integration-test harness survey for load-bearing gates (2026-06-09)

Research input for #1064. Surveys the live-validation gap on regatta's CI gate stack, three OSS prior-art harnesses, the seam in `cmd/regatta/serve.go`, the CI cost envelope, and failure modes. No code, no spec.

## Problem

The 2026-06-08 dogfood batch shipped 4 load-bearing gates (`scripts/check-reviewer-verdict.sh`, `scripts/check-tdd.sh`, `internal/prwatch/prwatch.go::Sweep`, `internal/orchestrator/spawner/claude.go::defaultPromptBuilder`) under three validation surfaces: in-PR unit tests, an adversarial reviewer subagent, and post-merge observation on the next PR that exercises the gate. Only the third signal proves the gate fires on real input — and by then the gate is on `main`.

A recent example: the `os.DevNull` MCP regression behaved cleanly under unit tests (the stub spawner accepted `/dev/null`) but failed on the first real `claude` invocation because the upstream MCP client rejects the path. The test asserted shape, not behaviour against the real boundary; the bug shipped. Classic "fakes drift from reality" anti-pattern (Hyrum 2017, Google SRE Book chap. 17).

The closest existing surface is `internal/orchestrator/orchestrator_test.go::newHarness` (`:66-111`), which wires `adaptersync` + `state.DB` + `scheduler.New` + `spawner.Stub` and drives `PollOnce` / `ScheduleOnce`. This proves the orchestration plumbing works; it does **not** exercise `serve.go`'s boot composition (gate construction, listener, reaper, prwatcher), and it does not invoke the bash scripts under `scripts/check-*.sh`.

`cmd/regatta/serve.go::runServe` already exposes a `--tick-once` shortcut (`:382-388`) that runs one orchestrator tick then exits. That single flag is the under-used seam.

## Prior art

### nats-server (NATS messaging daemon)

- `v2.14.2`, Apache-2.0, https://github.com/nats-io/nats-server.
- Layout: `test/` directory at repo root, separate from per-package `*_test.go`. Each test (`test/cluster_test.go`, `test/leafnode_test.go`, `test/gateway_test.go`) boots a real `*Server` in-process via the public `RunServer(opts)` helper, binds `127.0.0.1` on an ephemeral port, drives the wire protocol against it. Configs under `test/configs/`.
- Reference value: the daemon is loaded as a Go library, not exec'd. Harness depends on `server.Options{}` (analogous to `serveFlags`). Cost amortised by `t.TempDir()` + OS-assigned port.

### Vitess (sharded MySQL orchestrator)

- `v24.0.1`, Apache-2.0, https://github.com/vitessio/vitess.
- Layout: `go/test/endtoend/` with one directory per scenario (`backup/`, `cluster/`, `onlineddl/`, `reparent/`, ~40 total). Each scenario boots real `vttablet` + `vtgate` + `etcd` + MySQL subprocesses via a shared `cluster.LocalProcessCluster` helper at `go/test/endtoend/cluster/`. Tests assert against a live MySQL endpoint.
- Reference value: shows the cost shape. CI shards e2e per scenario directory. A regatta equivalent would shard `gate-rush-merge` + `gate-tdd` + `gate-prompt-parity` so a single flaky scenario does not block the rest.

### k0s (single-binary Kubernetes distribution)

- `v1.35.4+k0s.0`, Apache-2.0 per https://github.com/k0sproject/k0s/blob/main/LICENSE (GitHub API returns `NOASSERTION` because the repo ships dual headers — see LICENSE content). https://github.com/k0sproject/k0s.
- Layout: `inttest/` (28+ scenarios — `basic/`, `airgap/`, `backup/`, `ap-*/`, `autopilot/`). Each scenario its own Go package; `inttest/Makefile.variables` parameterises image + topology. Tests invoke the real `k0s` binary via `exec.Command`.
- Reference value: closest match to regatta — single-binary daemon with heterogeneous gate stack. k0s treats the daemon as a black box. Same call shape applies because bash gate scripts cannot be linked as a Go library — they must be `exec`'d against synthesized fixtures.

Honourable mention: etcd `v3.6.12` (Apache-2.0, https://github.com/etcd-io/etcd) runs `tests/e2e/` against `etcdctl` subprocesses; identical shape to k0s. nats-server (library-mode) + Vitess (binary + helper-cluster) + k0s/etcd (exec-the-binary) span the design space.

## Seam

The fixture-injection seam is `regatta serve --tick-once` (`cmd/regatta/serve.go:382-388`). One full boot of the composition root runs (secrets cache, db open, spec adapter, spawner build, scheduler, orchestrator, reaper, prwatcher, listener, authz, alarm webhook), `o.Recover(ctx)` fires once, `runTickOnce` runs one tick, process returns. Harness would shell out via `exec.Command("./regatta", "serve", "--tick-once", "--db", tmpDB, "--spawner", "stub", "--repo-root", tmpFixture)`.

Three knobs already exist:

1. `--spawner=stub` — `spawner.New` returns a `*spawner.Stub` that records every `Spawn` request and returns a synthetic `Result` without exec'ing a child (`internal/orchestrator/spawner/spawner.go:97-160`). No real claude binary needed.
2. `--db <path>` — sqlite file under `t.TempDir()` gives fresh state.
3. Repo-root fixture — `t.TempDir()` populated with `.regatta/items/*.md` + `git init` suffices for the markdown adapter; github_issues skipped by default.

The bash gate stack (`scripts/check-reviewer-verdict.sh`, `scripts/check-tdd.sh`, `scripts/check-prompt-parity.sh`) is **not** wired through `serve --tick-once`. These read PR body via `gh pr view` and inspect the diff. A harness for them is simpler: a Go test synthesizes a fixture PR body + diff, writes both to `t.TempDir()`, `exec`s the script with `--body-file` and `--diff-file` flags. No daemon boot.

This bifurcation matters for #1064 c1-c4: the rush-merge gate does **not** need the daemon; the prwatch / reaper gates do. One harness for both over-couples cheap script tests to expensive daemon boots.

## Cost

`make check` clocks ~30-60s; `make ci-check` adds `stale-todo` at ~30s; `make check-go` is the long pole at 3-5 min. The c4 envelope ("≤30s combined") fits the script-only path; it is tight against any path that runs `regatta serve --tick-once`.

Per-test shape:

- `serve --tick-once` cold boot: dominated by `state.Open` (sqlite migrations + first write) + secrets cache load + scheduler/reaper construction. Empirically ~200-500ms on a clean tmpdir; `secrets.Cache.Load` can blow to seconds if it walks the keychain. Mitigation: a `--no-keyring` analogue or stub resolver — needs a flag.
- Bash gate script: pure text inspection, ~50-100ms per invocation when `gh pr view` is bypassed by `--body-file`.
- Net for 4 gates × 2 fixtures = 8 invocations. Script path: ~800ms. Daemon path: ~4s. Combined: well under 30s if daemon-boot tests stay under ~10 cases.

The trap: if the harness becomes attractive and grows to 50+ scenarios, daemon-boot balloons to 25s and starves the rest of `make check`. Bound explicitly in the spec.

## Failure modes

1. **Sqlite-lock contention under `t.Parallel()`**: `state.DB` is single-writer. Multiple `serve --tick-once` sharing a tmp DB serialise on `BEGIN IMMEDIATE`. Per-test `t.TempDir()` isolates — enforce.
2. **Port-bind clashes** (`cmd/regatta/serve.go:325-349`, default `:8080`): tests must pass `--addr=:0`. Hard-coded port collides with parallel runs and with a developer's running daemon.
3. **Fakes drift from reality**: stub spawner accepts any prompt; real claude does not. Harness proves orchestrator behaves correctly; cannot prove child behaves correctly. Pin scope to gates + orchestrator transitions. Subprocess-boundary tests (MCP path, prompt rendering) stay separate — a `claude-real-integration` job gated on opt-in.
4. **Ticker-timing flakiness**: `--tick-once` runs one tick. If a gate decision needs two ticks (reaper expires lock, scheduler reassigns), the harness misses it. `--tick-n N` or accept multi-tick gates need a different shape.
5. **CI determinism vs realism**: more substitution = further from production. Vitess runs real MySQL; k0s boots real k0s. Regatta's stub-everything maximises determinism over fidelity. Accept stub-everything for the first cut; nightly real-claude soak behind a label; never block PRs on it.
6. **Bash gate script untestability**: `scripts/check-*.sh` accept input only via `gh pr view`. Without `--body-file` / `--diff-file`, harness must monkey-patch `gh` via PATH — fragile. One-line flag addition unblocks.
7. **PR-body snapshot lag** (`feedback_pr_lint_body_snapshot_lag`): gates parse the body string. Trap: tests assert body strings the operator never sees because pr-lint snapshots at trigger time. Harness must not paper over that bug.

## Recommendation

Three deliverables, in priority order:

1. **Bash-gate fixture harness** (cheap, immediate, no daemon). Add `--body-file` + `--diff-file` to `scripts/check-reviewer-verdict.sh`, `scripts/check-tdd.sh`, `scripts/check-prompt-parity.sh`. Write Go tests under `scripts/checks_integration_test.go` that synthesize positive + negative fixtures and `exec` the scripts. Cost: ~1s in `make check`. Closes #1064 c1 + c3.
2. **`serve --tick-once` orchestrator harness** for prwatch.Sweep + reaper transitions. Reuse `newHarness` pattern but at `cmd/regatta/serve_gate_integration_test.go` level so the boot path is in scope. Requires `--addr=:0` and a `--no-keyring` mode. Cost: ~4s for 4 scenarios. Closes #1064 c2.
3. **Nightly real-claude soak** (out of scope for #1064, follow-up). One scheduled job per week drives a real `claude` binary against a synthetic repo, watches for MCP-path-style regressions, fails loud. Modelled on k0s `inttest/`. Closes the `os.DevNull` class of bug for real; cannot block PRs.

Adopt **k0s's binary-mode shape** (exec the daemon, treat as black box) for items 2 and 3, **nats-server's library-mode** as a fallback if `--tick-once` proves too slow. Bash-gate path needs no prior art — `bats`-style test in Go.

## Open questions

1. Does `serve.go` need a `--no-keyring` flag to make boot deterministic, or is `secrets.NewCache` fast enough on a clean env? Wants empirical timing under `t.TempDir()` before spec'ing.
2. Cap at N scenarios via a Go build tag, with a separate `make ci-integration` shard for the long tail? Vitess shards by directory; regatta could shard by file.
3. Location: `cmd/regatta/serve_gate_integration_test.go` (operator-facing) or `internal/orchestrator/integrationtest/` (subsystem-facing)? k0s picks top-level `inttest/`; nats-server keeps it in `test/`. Spec decision.
4. Uniform `--body-file` / `--diff-file` contract on bash gate scripts, or ad-hoc per script? Suggest uniform.
5. Nightly real-claude soak in scope or split? Recommend split — its own beast (real API quota, real worktree, real `gh`) that would balloon #1064.

## References

- Issue #1064: `gh issue view 1064`.
- `cmd/regatta/serve.go:382-388` — `--tick-once` seam (verified via `git ls-tree origin/main cmd/regatta/serve.go`).
- `internal/orchestrator/orchestrator_test.go:66-111` — existing `newHarness`.
- `internal/orchestrator/spawner/spawner.go:97-160` — `Stub` spawner.
- `Makefile.d/ci.mk:10` — `check` composition; `:25` — `ci-check`; `:34` — `pre-push-check`.
- `scripts/check-reviewer-verdict.sh`, `scripts/check-tdd.sh`, `scripts/check-prompt-parity.sh`.
- nats-server `v2.14.2` Apache-2.0 — https://github.com/nats-io/nats-server.
- Vitess `v24.0.1` Apache-2.0 — https://github.com/vitessio/vitess.
- k0s `v1.35.4+k0s.0` Apache-2.0 per https://github.com/k0sproject/k0s/blob/main/LICENSE.
- etcd `v3.6.12` Apache-2.0 — https://github.com/etcd-io/etcd.
- Hyrum's Law (2017), https://www.hyrumslaw.com.
- Google SRE Book chap. 17 "Testing for Reliability".
- `feedback_validate_before_ship`, `feedback_cite_origin_main_not_local`, `feedback_research_design_principles` (CLAUDE.md).
