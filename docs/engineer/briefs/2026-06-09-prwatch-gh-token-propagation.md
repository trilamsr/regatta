---
name: prwatch-gh-token-propagation
slug: 2026-06-09-prwatch-gh-token-propagation
status: draft
phase: self-host-first
owner: tri@maydow.com
created: 2026-06-09
---

# prwatch GH_TOKEN propagation — container `gh auth` failure storm

_Author: design session, 2026-06-09. Closes GH #1178. Source: operator pain observed during regatta-operator skill session 4 against the regatta repo._

## 1. Observed

In a single 23-second observation window during regatta-operator skill session 4 (regatta repo, container-mode boot), the structured log emitted **210 `prwatch.list_failed` WARN events**, every one carrying:

```
err="prwatch: gh pr list --head regatta/agent-N: exit status 4"
```

`gh` exit status 4 is the documented `auth required` code (https://cli.github.com/manual/gh_help_exit_codes). The pattern is one failure per scheduled `Watcher.Sweep` tick per spawned agent: with `N` running agents the per-tick failure count scales linearly, so a 30-agent fleet sweeping at the default cadence saturates the log channel inside half a minute. Every spawned agent stays `running` in state forever — the reaper depends on prwatch correlating a PR head to the agent, and prwatch cannot answer the question without a working `gh` auth context.

The operator's host shell can authenticate fine: `GH_TOKEN=<token> gh pr list --head regatta/agent-1` on the host returns immediately. The same command issued by the regatta container against the same repo with the same `.env` file fails with exit status 4. So the token exists, it works, and the failure is strictly a container-boundary propagation issue.

## 2. Possible root causes (ranked by likelihood)

### (a) GH_TOKEN env not propagated into the gh subprocess [MOST LIKELY]

prwatch is **in-process** with the orchestrator (`cmd/regatta/wire_prwatch.go::startPRWatcher` calls `prwatch.New(...)` directly; the watcher then shells `gh` via `internal/orchestrator/prwatch/ghcli.go::defaultExec`, which uses `exec.CommandContext(ctx, "gh", args...).Output()`). `exec.Cmd.Env == nil` means **the child inherits the parent process env unchanged** — there is no scrub at this seam. Confirmed by reading `child_env.go::scrubChildEnv`: the only scrub path in the spawner package strips `ANTHROPIC_API_KEY` + `ANTHROPIC_AUTH_TOKEN` from spawned **claude CLI** children (controlled by `REGATTA_SPAWNER_STRIP_API_KEY`), and is not invoked anywhere on the prwatch code path. `GH_TOKEN` therefore survives the in-process spawn IF it ever reaches the regatta process at all.

The remaining question is whether the operator's `.env` actually reaches the regatta container's environment. `docker-compose.yml` (lines 53-55) wires `.env` via `env_file: [- path: .env, required: false]`, then re-declares a narrow `environment:` block (lines 56-75) that names only `OTEL_*`, `REGATTA_HMAC_KEY`, and `REGATTA_SPAWNER_STRIP_API_KEY` — `GH_TOKEN` is never listed there. Compose **does** merge `env_file` keys into the container env, but the `required: false` flag silently passes when `.env` is absent, and any operator running `docker compose up` from a different cwd (e.g. a fresh worktree) gets a no-op `.env` load with no diagnostic. Net effect: it is mechanically possible for the regatta container to boot with no `GH_TOKEN` at all, and the failure surfaces 30 seconds later as a `gh` exit-4 storm rather than a boot-time refusal.

### (b) gh CLI inside container prefers `~/.config/gh/hosts.yml` over `GH_TOKEN`

gh's documented precedence (https://cli.github.com/manual/gh_auth_login, https://cli.github.com/manual/gh_auth_status) is: `GITHUB_TOKEN` or `GH_TOKEN` env wins over `hosts.yml`. The distroless container image used for the regatta service has no shell, no `gh auth login` ever ran, and no `~/.config/gh/` is mounted — so there is no `hosts.yml` to lose to. If `GH_TOKEN` is set in the container env, gh uses it. This rules out the precedence theory once (a) is fixed; until (a) is fixed, (b) is moot because there is no token to prefer.

### (c) GH_TOKEN scope mismatch [UNLIKELY, self-host]

Operator's PAT could in principle have access to the operator's fork but not the upstream repo prwatch is querying. For self-host (single operator, single repo, repo is `trilamsr/regatta`) the same token that pushes worker branches is the same token prwatch needs — scope mismatch is not credible. Reopen only if (a) + (b) both come up clean.

## 3. Fix proposals (in increasing scope)

### Smallest — declare `GH_TOKEN` in the compose `environment:` block

In `docker-compose.yml` under the `regatta:` service, add an explicit pass-through line alongside the existing `REGATTA_HMAC_KEY` entry:

```yaml
environment:
  GH_TOKEN: ${GH_TOKEN:?GH_TOKEN must be set in .env or shell env}
```

The `:?` form fails the `docker compose up` loudly when the operator forgot the token, instead of silently shipping a container that cannot talk to GitHub. `.env` continues to be the carrier (no plaintext token in compose), but the propagation contract is now explicit at the compose layer rather than implicitly riding on `env_file:` merge semantics. Roughly 1 LoC, no Go changes, no test impact. This alone resolves #1178 for the steady-state operator boot.

### Medium — boot-time `gh auth` probe analogous to `preflightUIBoot`

Add `preflightGHAuth(ctx)` to `cmd/regatta/wire_prwatch.go`, fired before `prwatch.New(...)` returns. The probe shells `gh auth status` (exit 0 == authed) or, more accurately for prwatch's actual workload, `gh pr list --repo <TARGET_REPO> -L 1 --json number`. Either form returns non-zero on missing/invalid auth and the orchestrator refuses to boot with a single loud line naming the env-var the operator needs to set. Trade: one extra GH API call per boot (negligible vs. the 210-per-23s WARN flood). Mirrors the existing `preflightUIBoot` HMAC pattern that catches the analogous misconfig at the loud-at-boot moment instead of as a render-time lie.

### Larger — extract a typed `GHClient` interface in `internal/ghclient/`

Replace the `exec.CommandContext("gh", ...)` shell-out in `prwatch/ghcli.go` (and the parallel callsites in `rejectionrouter.GHLabeler` etc.) with a `GHClient` interface that takes an explicit token at construction and dispatches via the `github.com/google/go-github/v6X/github` SDK or net/http. Drops the dependency on the gh CLI binary being installed in the container image, drops the dependency on gh's env-precedence rules, and makes prwatch retry semantics testable without an `exec.Cmd` stub. Cost: net-new package, schema-drift insurance the `ghJSONFields` constant currently provides at the CLI seam moves into typed-Go code, ~300 LoC + integration tests.

## 4. Recommendation

Ship the smallest fix first — add `GH_TOKEN:` to the compose `environment:` block with the `:?` required-flag form. This is one line, zero Go changes, no test surface, and mechanically resolves the observed #1178 symptom by closing the propagation gap. **Then** add the medium boot-probe so the next operator who misconfigures their `.env` sees a loud refusal at second 0 instead of a 210-WARN flood at second 23. **Defer** the `GHClient` SDK extract until rate-limit issue #1164 forces the move: that issue already needs retry/backoff/quota visibility the gh CLI shell-out cannot give us, so the extract earns its keep once. Until then, the CLI seam is fine — three similar `exec.CommandContext` lines beat a premature abstraction (`feedback_default_simpler`). Decision priority: this is a UX-killing operator pain (container looks broken on first boot, 210 logs to read before the actual signal), not a velocity question, so the smallest fix lands first and ships today (`feedback_decision_priority`).

## 5. Citations

- GH #1178 — prwatch.list_failed × 210 with `gh pr list --head ... : exit status 4`
- GH #1164 — prwatch rate-limit / retry issue (reopen-trigger for the larger fix)
- `feedback_default_simpler` — pick simplest viable; don't pre-build for hypothetical drift (smallest fix first, defer GHClient extract)
- `feedback_decision_priority` — UX > ease > performance > best-practices > speed > velocity (operator pain dictates fix order)
