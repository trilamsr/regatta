---
status: phase-x-deferred
summary: "Design for taking regatta from single-target (one repo, one daemon, one config) to N-target (single operator dispatching regatta against N repos). Three options scored — A: one daemon per target (default-simpler, mostly works today); B: one daemon, N targets in one config (Phase-X); C: runtime `regatta target add` (deferred). Recommendation: A first. NOT multi-tenant (that is Phase-X W8 under a different trigger)."
deferred_on: 2026-06-10
---

# Multi-target-repo orchestration — single operator, N repos

_Author: design session, 2026-06-07. Brief: `docs/engineer/briefs/2026-06-01-self-host-first.md` §1 (self-host filter). Memory rule cites: `feedback_default_simpler`, `feedback_decision_priority`, `feedback_single_user_priority`, `feedback_no_signatures`, `feedback_audit_main_before_implementing`._

## 1. Problem

Today the self-host posture is single-tenant, single-operator, single-repo — and the runtime makes the third assumption hard-coded in two places:

1. `cmd/regatta/install_service.go` invokes `supervisor.Install` with no instance name. `internal/supervisor/supervisor.go:259` sets `p.Label = "com.regatta.serve"` (darwin) and `:275` sets `p.UnitName = "regatta.service"` (linux). The plist path `~/Library/LaunchAgents/com.regatta.serve.plist` and the systemd unit `~/.config/systemd/user/regatta.service` collide on a second install.
2. The OS WorkingDir / LogDir / EnvFile are derived from a hardcoded literal `regatta` namespace (e.g. `~/.local/share/regatta`, `~/.config/regatta/env`). A second target installed via the same `install-service` would overwrite the first's working directory + env file.

The runtime itself (the `serve` process) is already per-target-clean — `serveFlags.DBPath`, `RepoRoot`, `ItemsRoot`, `Addr` are all per-process flags with no shared globals (`cmd/regatta/wire_flags.go:13-31`). Worktrees live at `<repo>/.regatta/worktrees` (per-repo-root). `regatta.yaml` lives at `<repo>/regatta.yaml` (per-repo). The orchestrator config (`internal/orchestrator/orchestrator.go`) carries no package globals. Two `regatta serve` processes with disjoint `--db`, `--repo`, `--addr` already run side-by-side without code change.

The wedge is in the supervisor surface, not the daemon. Operator with N repos wanting "regatta everywhere" hits the install-service collision first.

Operator question: is regatta extensible to other codebases? Answer today: yes for `serve`, no for `install-service` without name namespacing.

## 2. Goal

Spec the path from single-target self-host to N-target single-operator orchestration. NOT multi-tenant (that is Phase-X W8 — driven by external-customer ask, not operator-with-N-repos). NOT billing / `RBAC`. NOT cross-target work-item routing.

Operator runs N daemons (Option A) OR one daemon supervising N targets (Option B) OR registers targets dynamically (Option C). This spec evaluates the three and recommends Option A as the simplest viable path, B as Phase-X if A's operator-ergonomics pain hits, C as deferred indefinitely.

## 3. Options

### Option A — one daemon per target

Each target repo gets its own service unit, its own DB, its own working directory, its own listener port, its own config file. Operator runs N services, manages N units, watches N processes.

```
~/Library/LaunchAgents/com.regatta.serve.<name>.plist     (darwin)
~/.config/systemd/user/regatta@<name>.service             (linux, instance form)
~/.local/share/regatta/<name>/                            (working dir per target)
~/.config/regatta/<name>/env                              (env file per target)
~/Library/Logs/regatta/<name>/                            (darwin log dir)
```

Each `regatta serve` invocation in the unit gets `--db <name>.db --repo <repo-path> --addr :<port>`. Worktrees stay at `<repo>/.regatta/worktrees` — already per-repo. The CLI surface gains ONE flag (`install-service --name <name>`); the orchestrator runtime gains zero changes.

Pros: simplest. Process isolation = blast radius bounded per target. No cross-target shared state, so no concurrency bugs to design around. Operator can disable / restart / debug one target without touching the others. Matches the self-host filter — single operator, single tenant, deterministic CI per target.

Cons: N processes = N idle baselines (Go runtime ~20MB each per `feedback_validate_before_ship` measurement methodology — bench before complaining). Operator manages N service units. Port allocation manual. No cross-target dashboards (each target has its own listener URL).

### Option B — one daemon, N targets in one config

`regatta.yaml` grows a `targets:` list. The daemon holds per-target schedulers, per-target DBs, per-target worktree roots in one process. One service unit, one listener (multiplexed paths e.g. `/targets/<name>/...`), one log file (interleaved or per-target sink).

```yaml
version: 1
targets:
  regatta:
    repo: { host: github, owner: trilamsr, name: regatta }
    db: regatta.db
    items_root: .
  proj-b:
    repo: { host: github, owner: trilamsr, name: proj-b }
    db: proj-b.db
    items_root: ../proj-b
```

Pros: one supervised unit. One log to tail. Centralized port. Easier "show me everything" view.

Cons:
- Composition root rewrite. `cmd/regatta/serve.go` today builds ONE scheduler / spawner / spend / authorizer / cost-cap-enforcer / approval-gate / brief-loader bundle from `serveFlags`. Going N-target means duplicating every primitive N times keyed by target-name, plus a routing layer in front of every CLI subcommand (`regatta approval list --target <name>`).
- Shared-primitive owner explosion. Today `cmd/regatta/serve.go` is the single shared anchor (`feedback_cascade_rebase_root_cause`); going N-target forces re-anchoring every per-target subsystem on a target-keyed map. Cascade-rebase risk.
- Worktrees collide if two targets pull the same repo path. Need a per-target worktree-root namespace.
- Failure-mode blast radius: a stuck adapter-poll loop in target X stalls the same goroutine pool used by Y. Today this is impossible because targets are separate processes.

### Option C — runtime `regatta target add <owner/repo>`

Most ambitious. Daemon exposes a CLI / HTTP control plane for dynamic target registration. New target = new DB created on the fly, new scheduler spun up, new worktree root provisioned. Operator never touches yaml.

Pros: cleanest operator UX (one command per new repo). Hot-add without service restart.

Cons: every Option B con plus a full control-plane API to design (auth, persistence of the target registry, recovery semantics on crash mid-add, removal flow). Substrate-events would need a `target_id` column propagated through every reader (echoes the W8 `tenant_id` work but on a different axis). At self-host scale (operator adds a target ~once a month) this is overbuilt.

## 4. Recommendation

**Option A.** Default-simpler (`feedback_default_simpler`). Mostly works today. Smallest surface change. Process isolation eliminates whole categories of concurrency bug. Single-operator + single-tenant + per-target single-repo posture preserved per `feedback_decision_priority` UX-first.

Option B graduates from Phase-X to active when ALL of:
1. Operator measures Option A pain ≥30 days running ≥3 targets (memory-baseline overhead becomes load-bearing OR multi-unit management drops on the floor).
2. A concrete operator-facing decision needs cross-target state (e.g. shared cost budget across targets — not in scope today).
3. No external-customer wedge has fired in the same window (else that work has higher priority).

Option C does not graduate without an explicit external customer ask (per brief §4).

## 5. Acceptance for Option A

A.1 `regatta install-service --name <name>` flag exists. Default: `<name>=""` ⇒ today's single-target behavior (`com.regatta.serve`, `regatta.service`, `~/.local/share/regatta`). With `--name regatta`: `com.regatta.serve.regatta`, `regatta@regatta.service` (systemd instance form), `~/.local/share/regatta/regatta/`, `~/.config/regatta/regatta/env`.

A.2 Two `regatta install-service --user --name a` and `--name b` runs coexist with no filesystem collision. `uninstall-service --name a` leaves `b` running.

A.3 Each instance points at its own `--repo`, `--db`, `--addr`, `--items-root` via the env file or unit `ExecStart` args.

A.4 `docs/operator/configure.md` gains a "Running regatta against multiple repos" section showing the two-install workflow end-to-end.

A.5 No change to `regatta serve` flag surface, scheduler, spawner, orchestrator state. The runtime stays per-process.

## 6. Required changes (scoped for A only)

Five file edits + one doc:

6.1 `internal/supervisor/supervisor.go` — add `Options.Name string`. Derive `p.Label = "com.regatta.serve" + suffixOf(name)` (darwin) and `p.UnitName = "regatta" + atSuffix(name) + ".service"` (linux: empty name ⇒ `regatta.service`; non-empty ⇒ `regatta@<name>.service`, the systemd `%i` instance form). Derive `p.WorkingDir`, `p.LogDir`, `p.EnvFile`, `p.ConfigPath` with the `<name>` segment when non-empty. Confirmed call sites today: `:259`, `:262-263`, `:266-267`, `:275`, `:277-279`, `:282-285`, `:287-288`.

6.2 `internal/supervisor/templates/regatta.plist.tmpl` — already templates `{{.Label}}` + `{{.WorkingDir}}`; no edit needed. `{{.WorkingDir}}/repo` line needs review — see §6.6.

6.3 `internal/supervisor/templates/regatta.service.tmpl` — confirm systemd-instance-form `%i` template substitution works for `WorkingDirectory=` and `ExecStart=` if the operator uses `--name`. If template hardcodes paths, switch to template fields or document the systemd `regatta@.service` pattern.

6.4 `cmd/regatta/install_service.go` — add `--name <string>` flag, thread to `supervisor.Options.Name`. Validate: charset `[a-z0-9-]{1,32}` (reject path metacharacters per `sanitizeShellPath` defense-in-depth).

6.5 `cmd/regatta/uninstall_service.go` — accept `--name` symmetrically; uninstall the matching unit only.

6.6 `internal/supervisor/templates/regatta.plist.tmpl` line `exec "{{.BinaryPath}}" serve --repo "{{.WorkingDir}}/repo"` hardcodes `<workingDir>/repo` as the operator-managed clone path. With Option A this stays per-target (each `<name>` has its own `<workingDir>/repo`). No edit, but `docs/operator/configure.md` must say: "you clone the target repo to `<workingDir>/repo` before `launchctl load`".

6.7 `docs/operator/configure.md` — new "Multiple targets" section. Documents the `--name` flag, the two-install workflow, the per-target env file layout, and the rule "no two targets share a `--repo` path or `--db` path".

No changes to: `regatta.yaml` schema, `cmd/regatta/serve.go`, `cmd/regatta/wire_flags.go`, `internal/orchestrator/*`, `contracts/schemas/regatta.v1.cue`. Verified per §1 audit.

## 7. Out of scope

7.1 Multi-tenant `tenant_id` propagation — Phase-X W8, external-customer trigger (`docs/engineer/briefs/2026-06-01-self-host-first.md` §4). DIFFERENT axis: tenant = customer-of-regatta, target = repo-of-operator. One operator with N targets is still ONE tenant.

7.2 Billing / `Stripe` metering — Phase-X W12, no per-target cost-allocation surface needed today.

7.3 Cross-target work-item routing — operator does not need work_item X in target A to depend on work_item Y in target B at self-host scale. Single-target scheduler is fine.

7.4 Centralized cross-target dashboard — operator opens N tabs at N listener URLs. Option B prereq, defer.

7.5 Option B + C designs — bodies of options above are aspirational; concrete impl spec waits for the Option-A pain trigger.

7.6 `regatta serve --target <name>` flag — runtime is already per-process; the supervisor unit injects `--db` / `--repo` per target. No new serve-level flag needed.

7.7 New regatta.yaml `targets:` block — Option B / C only. Today's per-repo yaml stays the only convention.

## 8. Test scaffold (Option A)

8.1 `TestInstallService_NameFlag_DarwinLabel` — `--name foo` on darwin produces plist path `~/Library/LaunchAgents/com.regatta.serve.foo.plist` + Label `com.regatta.serve.foo`. Empty name preserves today's `com.regatta.serve.plist`.

8.2 `TestInstallService_NameFlag_LinuxUnit` — `--name foo` on linux produces unit path `~/.config/systemd/user/regatta@foo.service`. Empty name preserves today's `regatta.service`.

8.3 `TestInstallService_NameFlag_DirectoriesPerName` — `--name a` + `--name b` produce disjoint `WorkingDir`, `LogDir`, `EnvFile`, `ConfigPath` strings. No path overlap.

8.4 `TestInstallService_NameFlag_Validation` — `--name "foo/bar"` rejected (path metacharacter). `--name "Foo"` rejected (uppercase). `--name ""` accepted (back-compat default). `--name "a"` through `--name "a"*32` accepted; `*33` rejected.

8.5 `TestSupervisorOptions_NameZeroValueIsBackCompat` — unit test on `supervisor.Install` with `Options.Name = ""` produces today's exact paths + labels. Pins back-compat.

8.6 Integration (manual, no CI): operator runs `install-service --user --name regatta` + `install-service --user --name proj-b` against two clone roots. `launchctl list | grep com.regatta.serve` (darwin) shows TWO labels. Both `serve` processes idle without filesystem collision.

## 9. Migration / rollout

9.1 Land §6.1 → §6.5 + §8.1 → §8.5 in ONE PR. Doc in §6.7 ships with the impl PR. File-disjoint within `internal/supervisor/` + `cmd/regatta/install_service.go` + `cmd/regatta/uninstall_service.go`.

9.2 Back-compat: `Options.Name == ""` ⇒ today's paths exactly. Existing single-target installs untouched. Zero operator action required to keep one-target setup.

9.3 No flag deprecation; this is a pure addition with zero-value preserving prior behavior.

9.4 Operator workflow for adopting Option A on an existing single-target install:
- Today's install becomes `--name regatta` (rename + reinstall) OR stays nameless (no migration needed) OR retires (uninstall + new named install).
- Adding a second target: `regatta install-service --user --name proj-b`, clone target into `~/.local/share/regatta/proj-b/repo`, populate `~/.config/regatta/proj-b/env`, `launchctl load` (darwin) or `systemctl --user enable --now regatta@proj-b.service` (linux).

## 10. Risk + adversarial pass

- **Risk**: operator passes `--name` containing a shell metacharacter that breaks the launchd `/bin/sh -lc` wrapper. **Mitigate**: §6.4 charset whitelist `[a-z0-9-]{1,32}` rejected at flag-parse time; reuses the `sanitizeEnvFile` / `sanitizeShellPath` defense (`internal/supervisor/supervisor.go:319-324`).
- **Risk**: operator runs two installs that point `--repo` at the same path. Worktree dir `<repo>/.regatta/worktrees` collides; two daemons fight over the same SQLite file. **Mitigate**: §8.6 manual integration test documents the rule; runtime detection is out of scope (per `feedback_default_simpler` — wait for a real operator hit before lint).
- **Risk**: scope creep — operator asks for cross-target cost ceiling next. **Mitigate**: §7.5 + §4 graduation criteria — Option B unlocks if-and-only-if A-pain triggers.
- **Risk**: systemd instance-form `%i` template substitution does not propagate into the `regatta.service.tmpl` fields we render. **Mitigate**: §6.3 verify-then-design — if `%i` is not viable, fall back to per-name unit name (`regatta-<name>.service`) rather than instance form. Either path satisfies acceptance.
- **Risk**: Linux systemd user services need `loginctl enable-linger <user>` for non-interactive starts; today's single-target install assumes the operator already ran this. Per-target instance form does not change this. Doc note in §6.7.
- **Risk** (Option B trigger ambiguity): operator might claim "pain" prematurely. **Mitigate**: §4 requires ≥3 active targets AND ≥30-day measurement window AND no external-customer wedge in flight — three tripwires before reopening B.

## 11. A+ rubric

Solo-operator self-host phase per CLAUDE.md `feedback_grade_rubric` — operator self-grade, no CI gate. Format unenforced. Tier criteria:

- (B1) Back-compat zero-action default — `Options.Name == ""` produces identical paths to today (`TestSupervisorOptions_NameZeroValueIsBackCompat`).
- (B2) Two `--name` installs coexist on disk without collision (`TestInstallService_NameFlag_DirectoriesPerName`).
- (A1) Charset validation catches shell-metacharacter injection at flag-parse before any filesystem write (`TestInstallService_NameFlag_Validation`).
- (A2) Operator-facing `docs/operator/configure.md` "Multiple targets" section explains the workflow end-to-end without grepping source.
- (A+1) Zero changes to `cmd/regatta/serve.go` / `wire_flags.go` / `internal/orchestrator/` — confirms the daemon runtime was already per-target-clean.
- (A+2) Trigger criteria for Option B documented in §4 so the next operator decision is auditable, not vibes.

## 12. Out-of-band followups (file as separate issues if approved)

- F1: lint gate `scripts/check-install-service-name-charset.sh` if charset validation regresses post-impl (defer until trip).
- F2: Option B detailed design — only on §4 graduation criteria. Spec stub already covers the option shape.
- F3: Cross-target listener-port allocator — operator picks ports today; centralize only on multi-target-dashboard ask.
- F4: `regatta doctor --name <name>` companion — operator preflight per target. Depends on the doctor spec (separate work).

```release-notes
docs: spec multi-target-repo orchestration paths
```
