# Amendment — PHASE AUTONOMY (§11 insert)

**Date:** 2026-06-02
**Status:** amendment to `docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md` (squash into §11 after #433 merges; lands as new §11, prior §11 renumbers to §12, references shift to §13, rubric to §14).
**Depends on:** #433 (the unified next-horizon brief) — must merge first OR be squashed-in via amend-and-merge.

This insert adds a wedge phase **PHASE AUTONOMY** that sequences BEFORE MVR-1. Operator must be able to leave the substrate unattended against THIS repo before chasing an external customer; the next-horizon brief's §4 Phase S3 closes self-host operationally, but PHASE AUTONOMY closes the **self-running + self-improving** loop the boot prompt assumes. Seven small wedges (~980 LoC total) ordered into three landings.

---

## §11 PHASE AUTONOMY — operator-grade self-runner

**Gate to enter:** Phase S3 closed (per the next-horizon brief §4).

**Gate to exit (= MVR-1 entry):** 30-day-self-host-green per §2 Gate 1, AND substrate has filed + closed ≥3 self-improvement issues without operator-side template edits, AND zero unhandled `obs-alert` issues older than 24h.

**Decision-priority spine.** Operator IS the customer here (`feedback_decision_priority`). Every wedge below is scoped against the customer-benefit-first ordering: UX (unattended ≥ watching) → ease (one binary + one supervisor file) → performance → best-practices → speed → velocity.

**Adoption-first spine.** Per `feedback_research_design_principles`, every wedge cites ≥2 OSS candidates and prefers adopt over build.

### Phasing — three landings

**Landing 1 (closes obs → issue → merge loop):** W1 alarm-webhook + W2 auto-merge-on-gate-pass. Without W1 the operator can't see SLO breaches without polling; without W2 the operator is still the merge actor. Landing 1 lets the operator sleep through a green night.

**Landing 2 (bootstrap stability):** W3 service-supervisor + W6 secret-credential autonomic fetch. Without W3 the loop dies on reboot; without W6 the operator manually re-exports `ANTHROPIC_API_KEY` on every wake. Landing 2 lets the operator close the laptop for a weekend.

**Landing 3 (self-improvement + cost + identity):** W4 self-improvement detector + W5 cost-cap autonomic enforcement + W7 PR-merge L4-as-review identity. Landing 3 lets the substrate amend its own boot prompt + dispatch templates from observed failure patterns, halt itself before burning a daily cap, and merge without a second human-account approval.

LoC budget across the phase: ~980. Effort-band: ~10-14 days subagent-time at current 3-4 lane parallel pace.

### W1 — alarm-webhook

**Goal.** Prometheus AlertManager firing → `gh issue create --label autonomous --label obs-alert --label <severity>` with the alarm name, SLO breach, metric-snapshot link, dashboard URL, and reproduce command in the body.

**Prior art (≥2 OSS).**
- [AlertManager Webhook receiver](https://prometheus.io/docs/alerting/latest/configuration/#webhook_config) (Apache 2). Standard JSON payload shape; receiver writes `firing` array per group.
- [grafana/oncall](https://github.com/grafana/oncall) (AGPL-3) — webhook → ticket pattern in OSS form; reference for dedupe-by-alarm-name shape.
- [k8s/event-exporter](https://github.com/resmoio/kubernetes-event-exporter) (Apache 2) — event-to-sink adapter pattern; cited as the "small thing that does one thing" shape we match.

**Adopt vs build.** Adopt AlertManager's webhook payload shape verbatim. Build the issue-creating receiver (≈200 LoC) — no OSS today files an issue against a per-repo regatta substrate with the labels we need. Total: adopt = payload contract; build = the receiver binary.

**LoC estimate.** ~200 (Go), one file `cmd/regatta-alarm-webhook/main.go`, one config knob in `regatta.yaml: alarm_webhook.listen_addr`.

**Dependency.** None — W1 is the head of the chain. Lands first.

**Acceptance criteria.**
- c1: AlertManager-format POST creates an issue with `autonomous + obs-alert + <severity>` labels.
- c2: Dedup — second firing of the same alarm name comments on the open issue instead of opening a new one.
- c3: Body includes alarm name, SLO threshold, current metric value, Grafana URL, and a `regatta replay` reproduce command.
- c4: Receiver runs under the `regatta serve` process tree OR as a sidecar binary (`cmd/regatta-alarm-webhook`) — operator picks via systemd/launchd.

**B/A/A+ rubric (per `feedback_grade_rubric`).**

| Tier | Criteria |
|---|---|
| B (floor) | (a) c1+c2+c3 ship. (b) Single binary. (c) Release-notes fence in PR body. (d) No new runtime deps beyond `github.com/google/go-github`. |
| A (target) | B + (e) c4. (f) Span coverage via W6 OTel for each `/webhook` HTTP request. (g) Unit test against the literal AlertManager JSON sample from the upstream docs. (h) Adversarial reviewer subagent posts on the PR. |
| A+ (stretch) | A + (i) Property test: 100 random alarm payloads, none produce a duplicate issue when fed twice. (j) Receiver self-files a `self-improvement` issue when ≥3 alarms fire from the same `alertname` within 7 days (handoff to W4). (k) Replay-command embedded in body works end-to-end against the substrate fixture. |

---

### W2 — auto-merge-on-gate-pass

**Goal.** When a PR's required checks go green, L4 gate ADOPTs, cost-cap holds, and the adversarial reviewer has cleared, `regatta serve` calls `gh pr merge --squash --auto`. Operator is no longer the merge actor.

**Prior art (≥2 OSS).**
- [bors-ng](https://github.com/bors-ng/bors-ng) (Apache 2) — canonical "merge-when-green" bot. Reference for queueing + serialization shape; we don't take the binary (it's Elixir + per-repo daemon), we take the design.
- [Mergify](https://github.com/Mergify/mergify) (Apache 2, OSS core) — rule-engine for auto-merge. Reference for the per-label override pattern (`[needs-human-review]` blocks; `[auto-merge-ok]` accelerates).
- GitHub native `gh pr merge --auto` — adopted as the actual mutation; we don't reimplement squash-merge.

**Adopt vs build.** Adopt `gh pr merge --auto` as the leaf call. Build the policy engine that decides when to make the call (≈150 LoC inside `cmd/regatta` serve). Bors + Mergify are heavier than persona-A wants; the operator IS the persona, and they want one binary.

**LoC estimate.** ~150 for the policy engine, extends existing `cmd/regatta` serve subcommand (two config knobs: `ci.automerge_on_pass: true` + per-issue label `[auto-merge-ok]` override) + **~120 for c0** (intent/outbox merge wrapper + `awaiting_merge` recovery re-probe, in `internal/orchestrator`). c0 is the harder, load-bearing half — it is correctness, not policy.

**Dependency.** W1 lands first (an `obs-alert` issue may want auto-merge blocked while incident open — wire that interlock).

**Prerequisite (c0 — load-bearing, blocks c2).** W2 introduces the *first external, non-idempotent side-effect* in the loop (`gh pr merge`). Today the merge path is a DB status-flip with idempotent journal reconciliation (`spawner.go Complete` → `reconcile.go reconcileOne`); a real `gh pr merge` is a GitHub-side mutation that the substrate cannot undo or dedup. Two existing holes make naive wiring unsafe:
1. **`awaiting_merge` is excluded from crash recovery.** `Recover` and `Heartbeat` enumerate only `{spawning, running, pr_open, gates_running}` (`internal/orchestrator/orchestrator.go:209-214`, `:390`). An agent that crashes in `awaiting_merge` — exactly the state straddling the external merge call — is invisible to recovery: never re-probed, never requeued.
2. **No intent/outbox.** A crash between the external `gh pr merge` and the completion event re-drives the merge on the next tick. Idempotent branch names do not save us — post-merge the branch may be deleted, so the re-merge errors and recovery misreads work-already-in-`main` as a failure to escalate or rerun.

c0 closes both before c2 may ship:
- **(i)** Add `AgentAwaitingMerge` to the `nonTerminal` recovery set in `Recover` + `Heartbeat`; on recovery of an `awaiting_merge` agent, **re-probe GitHub PR state** (merged? open? branch gone?) rather than blind-flip the FSM.
- **(ii)** Intent/outbox around the merge: append `merge_intended` (carrying an idempotency key = head-SHA) **before** the `gh pr merge` call; make the call idempotent against that key (query PR `merged` state first, treat already-merged as success); append `merge_executed` **after**. Recovery reconciles dangling `merge_intended` by querying actual PR state, never by blind retry.
- **(iii)** Reuse the existing dedup primitive: the `merge_intended`/`merge_executed` pair rides the substrate `UNIQUE(run_id, written_by, nonce)` guard (`substrate/event.go`), nonce = head-SHA, same pattern token-spend already uses (`spend/writer.go nonceFor`). No new mechanism — generalize the one that works.
- Cross-ref open issues #273 (spawner reconciliation outbox on SIGKILL) and #219 (archive_audit_outbox); c0 is the general external-side-effect case those two narrow.

**Acceptance criteria.**
- c0: Intent/outbox + `awaiting_merge` recovery re-probe land and are tested (crash-injected E2E: kill between `merge_intended` and `merge_executed`, assert no double-merge, assert recovery reconciles from real PR state). **Blocks c2.**
- c1: Config `ci.automerge_on_pass: true` enables; default-off.
- c2: After PR closes-review + all required checks green + L4 ADOPT + cost-cap OK + adversarial reviewer cleared → `gh pr merge --squash --auto` fires (guarded by c0's idempotency path).
- c3: Label `[needs-human-review]` blocks (escape hatch).
- c4: Label `[auto-merge-ok]` bypasses the L4-ADOPT requirement for trivial doc PRs (per `feedback_review_proportional`).
- c5: Open `obs-alert` issue with severity `critical` blocks all auto-merges substrate-wide until closed.

**B/A/A+ rubric.**

| Tier | Criteria |
|---|---|
| B (floor) | (a) **c0** + c1+c2+c3 ship (c0 is non-negotiable: no external merge without the intent/outbox + `awaiting_merge` recovery). (b) Default-off; explicit opt-in. (c) Release-notes fence in PR body. |
| A (target) | B + (d) c4+c5. (e) Substrate event `auto_merge_fired` emitted with the gate-decision summary. (f) Adversarial reviewer subagent posts on the PR. (g) E2E test: spin a fake GH server (`gock` or similar), assert merge-call shape. |
| A+ (stretch) | A + (h) Per-`obs-alert` severity interlock (only `critical` halts; `warning` allows). (i) Replay harness shows the gate-decision deterministic across 100 random PR states. (j) Cost-cap reset action (W5) auto-unblocks queued merges atomically. |

---

### W3 — service-supervisor

**Goal.** Operator runs `regatta install-service`; loop survives reboots, crashes, and log churn. macOS launchd plist + Linux systemd unit shipped under `dist/services/`. `/healthz` endpoint + pidfile + lock-file. Cron lines for daily-digest + `make items` + `make followups`.

**Prior art (≥2 OSS).**
- [systemd](https://systemd.io/) — Restart=on-failure, WatchdogSec, EnvironmentFile, ExecReload. Adopted as Linux init contract.
- [launchd](https://developer.apple.com/library/archive/documentation/MacOSX/Conceptual/BPSystemStartup/Chapters/CreatingLaunchdJobs.html) — KeepAlive, RunAtLoad, StandardErrorPath. Adopted as macOS init contract.
- [grafana/agent](https://github.com/grafana/agent) (Apache 2) — reference for how an OSS Go daemon ships systemd + launchd files in one repo.
- [/healthz convention](https://github.com/kubernetes/kubernetes/blob/master/CHANGELOG/CHANGELOG-1.0.md) (Kubernetes) — adopted endpoint shape.

**Adopt vs build.** Adopt systemd + launchd verbatim (zero LoC there). Build: `regatta install-service` writes the right plist/unit, registers it, and verifies `/healthz` returns 200 within 30s. Build the `/healthz` endpoint in the serve binary. ~150 LoC + 2 service-file templates under `dist/services/`.

**LoC estimate.** ~150 + 2 service files (~30 lines each).

**Dependency.** W1+W2 land first (the alarm + auto-merge surfaces ride on the supervised process; restart correctness is testable only with the loop active).

**Acceptance criteria.**
- c1: `regatta install-service` on macOS writes the launchd plist + `launchctl bootstrap`s it.
- c2: Same command on Linux writes the systemd unit + `systemctl enable --now`s it.
- c3: `kill -9` on the regatta PID — supervisor restarts within 10s; `/healthz` returns 200.
- c4: Log rotation via OS-native facility (systemd journal on Linux, `newsyslog`/launchd `StandardErrorPath`-rotation on macOS). No log-rotator added.
- c5: Crontab lines for daily-digest + `make items` + `make followups` land under `dist/cron/regatta.crontab`.

**B/A/A+ rubric.**

| Tier | Criteria |
|---|---|
| B (floor) | (a) c1+c2+c3 ship. (b) `/healthz` ≤ 30 lines. (c) Release-notes fence in PR body. |
| A (target) | B + (d) c4+c5. (e) `regatta uninstall-service` exists + reverses cleanly. (f) Pidfile + lock-file prevent double-start. (g) Adversarial reviewer subagent posts. |
| A+ (stretch) | A + (h) Lock-file race tested with 50 concurrent `install-service` calls; only one wins. (i) Service file is signed via cosign as part of release. (j) `regatta install-service --dry-run` prints the unit/plist + exits without mutating. |

---

### W4 — self-improvement detector

**Goal.** A daemon-side analyzer reads substrate events for recurring failure patterns and files `[self-improvement]` issues with a root-cause hypothesis. Issue auto-picked by the regatta loop → subagent writes memory + boot-prompt + dispatch-template PR. Loop closes itself.

**Triggers.**
- Same gate-fail ≥3× in 7 days.
- Same banned-phrase token tripped ≥2× across distinct PRs.
- Same agent-failure-mode (e.g., "subagent failed make check after claiming clean") ≥3× in 7 days.
- Same load-bearing-leftover pattern in ≥2 PR bodies.

**Prior art (≥2 OSS).**
- [Sentry's issue-grouping fingerprint](https://docs.sentry.io/concepts/data-management/event-grouping/) (BSL) — algorithm shape for dedup-by-pattern; reference, not import.
- [grafana/oncall](https://github.com/grafana/oncall) (AGPL-3) — event aggregation pattern; reference.
- [hashicorp/go-set](https://github.com/hashicorp/go-set) (MPL-2) — adopted for the bounded-window count primitive.
- [substrate Bayesian alerts in nodejs/diagnostics](https://github.com/nodejs/diagnostics) — anomaly-detection pattern via rolling window.

**Adopt vs build.** Adopt: rolling-window count primitive (hashicorp/go-set) + Sentry's fingerprint shape (referenced, not imported). Build: the heuristic suite + issue body templating (~250 LoC). The heuristics are operator-specific to OUR substrate event vocabulary — no upstream OSS knows what `gate_fail kind=banned-phrase` means.

**LoC estimate.** ~250 (Go) + ~5 named heuristics in a YAML file at `internal/selfimprove/heuristics.yaml`.

**Dependency.** W1+W2+W3 land first (heuristics observe a live substrate; can't test against an empty one). W5 dependency soft — if a cost-pause fires, W4 should not file a self-improvement issue blaming agents.

**Acceptance criteria.**
- c1: Each of the four triggers fires a `self-improvement` issue with: detected pattern + 3+ source-event links + root-cause hypothesis + one suggested edit (boot prompt, memory file, or dispatch template).
- c2: Dedup: re-firing the same pattern within 7 days comments on the open issue, no new issue.
- c3: Heuristic suite lives in `internal/selfimprove/heuristics.yaml`; adding a 6th heuristic is a single-file PR + a single Go test.
- c4: Subagent picking the issue can resolve it by writing a `feedback_*.md` file + opening a PR; the loop closes.

**B/A/A+ rubric.**

| Tier | Criteria |
|---|---|
| B (floor) | (a) c1 ships. (b) ≥3 of the 4 triggers wired. (c) Release-notes fence. |
| A (target) | B + (d) c2+c3+c4. (e) Adversarial reviewer subagent posts. (f) Heuristics-coverage table in the PR body shows which substrate-event kinds each heuristic reads. |
| A+ (stretch) | A + (g) Each issue carries an estimated-time-saved number (operator hours/week if pattern eliminated). (h) Mutation test: each heuristic survives mutation of every other heuristic's threshold by ±50%. (i) Replay harness re-runs the substrate window deterministically and reproduces every filed issue. |

---

### W5 — cost-cap autonomic enforcement

**Goal.** Daily cap exceeded → `regatta pause-all` flag in substrate. `regatta serve` reads pause flag, halts dispatch. Auto-unpause at period boundary OR operator-manual `regatta resume-all`.

**Prior art (≥2 OSS).**
- [k8s/kueue](https://github.com/kubernetes-sigs/kueue) (Apache 2) — queue-pause semantics; reference for the pause-flag-as-state shape.
- [hashicorp/vault](https://github.com/hashicorp/vault) sealed/unsealed model — adopted as the pause/resume semantic (binary flag + observable status).
- [argoproj/argo-workflows](https://github.com/argoproj/argo-workflows) (Apache 2) — `argo suspend` precedent for the per-workflow pause command shape.

**Adopt vs build.** Adopt: vault's sealed/unsealed binary-flag UX shape; argo's `suspend`/`resume` command shape. Build: ~50 LoC wiring the existing `internal/cost/gate` cap-fired event to flip the substrate flag + a guard at scheduler tick that reads the flag before dispatching.

**LoC estimate.** ~50, extends `internal/cost/gate` + scheduler tick guard.

**Dependency.** W2 (auto-merge needs to know about pause), W4 (self-improvement should NOT file an issue blaming agents when the cause is a pause). Lands in Landing 3 alongside both.

**Acceptance criteria.**
- c1: Daily cap exceeded → substrate `regatta_pause_all=true` event.
- c2: Scheduler tick reads the flag at the top of every loop; dispatch halts within one tick.
- c3: Period boundary (UTC midnight by default; configurable) clears the flag automatically.
- c4: `regatta resume-all` clears it on operator command; emits substrate event with `actor=<operator-gh-handle>`.
- c5: `regatta status` shows pause state in plain text.

**B/A/A+ rubric.**

| Tier | Criteria |
|---|---|
| B (floor) | (a) c1+c2 ship. (b) Default-on once `cost.daily_cap_usd` set. (c) Release-notes fence. |
| A (target) | B + (d) c3+c4+c5. (e) Adversarial reviewer posts. (f) Substrate event schema for `regatta_pause_all` documented in `docs/engineer/specs/2026-06-01-unified-substrate-design.md`. |
| A+ (stretch) | A + (g) Per-DAG pause (some DAGs can keep running while others halt) via label-set. (h) Property test: 100 random scheduler ticks under random pause/resume sequences; assert no dispatch fires when paused. (i) W4 self-improvement detector wired to detect pause-cycling (≥3 daily-cap hits in 7 days = self-improvement issue suggesting cap raise). |

---

### W6 — secret-credential autonomic fetch

**Goal.** Supervisor unlocks credentials at boot via gpg-agent; no operator-side `export ANTHROPIC_API_KEY=...` per wake. Adopt `pass` for the three secrets: `ANTHROPIC_API_KEY` + `GH_TOKEN` + `REGATTA_BRIEF_HMAC_KEYS`. Fallback: env-var if `pass` not installed.

**Prior art (≥2 OSS).**
- [pass — the standard unix password manager](https://www.passwordstore.org/) (GPL-2) — GPG-backed, file-tree-shaped secret store. Zero vendor dependency.
- [gopasspw/gopass](https://github.com/gopasspw/gopass) (MIT) — Go-native `pass`-compatible store; allows tighter integration if shelling out to `pass` proves awkward.
- [systemd LoadCredential](https://www.freedesktop.org/software/systemd/man/systemd.exec.html#LoadCredential=) — alternative reference; rejected because operator wants the same UX on macOS where LoadCredential doesn't exist.
- [HashiCorp Vault](https://www.vaultproject.io/) — rejected for persona-A (one-binary operator) as too heavy.

**Adopt vs build.** Adopt `pass` (or gopass) as the secret store. Build: ~100 LoC supervisor shim that reads the three secrets at boot, exports as env vars to the regatta serve process, fails-closed with a clear error if `pass` isn't installed AND no fallback env vars set.

**LoC estimate.** ~100, lives in the W3 supervisor cmd tree.

**Dependency.** W3 (supervisor process owns the secret-fetch step). Lands in Landing 2 alongside W3.

**Acceptance criteria.**
- c1: `regatta install-service` checks for `pass` + prompts to initialize if missing.
- c2: Boot path: gpg-agent → `pass show regatta/anthropic_api_key` → env var → `regatta serve`.
- c3: Same shape for `GH_TOKEN` + `REGATTA_BRIEF_HMAC_KEYS`.
- c4: Fallback: if `pass` not installed, fall back to existing env-var read; emit substrate event `secret_source=env`.
- c5: gpg-agent unlock TTL configurable via `regatta.yaml: secrets.gpg_agent_ttl_seconds`.

**B/A/A+ rubric.**

| Tier | Criteria |
|---|---|
| B (floor) | (a) c1+c2 ship. (b) Env-var fallback. (c) Release-notes fence. |
| A (target) | B + (d) c3+c4. (e) Adversarial reviewer posts. (f) Rotation drill: `pass insert -e regatta/anthropic_api_key` + `regatta reload-secrets` rotates without restart. |
| A+ (stretch) | A + (g) gopass integration tested as a drop-in alt to `pass`. (h) Secret-presence diagnostic in `regatta status` — shows source (`pass` vs `env`) per secret without printing the value. (i) Failure mode: gpg-agent absent → clear error + recovery doc link, not a stack trace. |

---

### W7 — PR-merge L4-as-review identity

**Goal.** L4 gate's ADOPT verdict = an actual GitHub PR review with `APPROVED` state, so branch-protection's "≥1 review" count is satisfied. No bot account needed — operator's PAT signs the review.

**Prior art (≥2 OSS).**
- [github/safe-settings](https://github.com/github/safe-settings) (MIT) — pattern for "rules-as-config" against branch protection; reference for the review-count contract.
- [bors-ng](https://github.com/bors-ng/bors-ng) — uses the same `gh api repos/.../pulls/N/reviews` POST shape we adopt. Reference.
- GitHub REST API `POST /repos/{owner}/{repo}/pulls/{pull_number}/reviews` with `event: APPROVED` — adopted as the actual mutation.

**Adopt vs build.** Adopt the REST contract verbatim. Build: ~80 LoC inside `internal/gates/l4` that posts the review when ADOPT fires + carries the per-criterion citation in the review body (per the operator's a+ rubric criterion (k) in W7-Wave-1).

**LoC estimate.** ~80, extends `internal/gates/l4`.

**Dependency.** W2 (the auto-merge call counts the L4-as-review against the branch-protection requirement; sequencing matters). Lands in Landing 3.

**Acceptance criteria.**
- c1: L4 ADOPT verdict → `gh api repos/.../pulls/N/reviews` POST with `event=APPROVED`, body carries the per-criterion citation summary.
- c2: L4 REJECT verdict → POST with `event=REQUEST_CHANGES`, body carries the failed-criteria list.
- c3: Branch-protection "≥1 approving review" is satisfied by the L4 review; W2 auto-merge proceeds.
- c4: Operator-side PAT is the actor; no bot account introduced.
- c5: Review body is reproducible — same PR + same gate state = same review body verbatim.

**B/A/A+ rubric.**

| Tier | Criteria |
|---|---|
| B (floor) | (a) c1+c4 ship. (b) Release-notes fence. (c) Default-off; opt-in via `regatta.yaml: gates.l4_posts_review: true`. |
| A (target) | B + (d) c2+c3+c5. (e) Adversarial reviewer posts. (f) Replay harness shows review-body is byte-identical across two L4 runs against the same gate state. |
| A+ (stretch) | A + (g) When L4 changes verdict between runs, the prior review is dismissed via `PUT /pulls/N/reviews/{review_id}/dismissals` — no review accretion. (h) Per-criterion citations linked to substrate events (`/substrate/event/{id}` URL). (i) L4-as-review compatible with GitHub's CODEOWNERS file: when CODEOWNERS demands a specific reviewer, L4 fails-closed instead of self-approving. |

---

## §11 phasing summary table

| Landing | Wedges | LoC | What closes |
|---|---|---|---|
| 1 | W1 + W2 (incl. c0) | ~470 | obs → issue → merge loop, on a crash-safe external-merge primitive |
| 2 | W3 + W6 | ~250 | bootstrap stability (reboot + secret unlock) |
| 3 | W4 + W5 + W7 | ~380 | self-improvement + cost autonomic + identity |

Phase total: ~1100 LoC across 7 wedges (was ~980; +120 for W2 c0 intent/outbox — the cost of making the loop's one irreversible external action crash-safe before it ships). Effort-band: ~10-14 days subagent-time. Abandon-criterion: if Landing 2 takes longer than 6 days subagent-time, slim W3 to Linux-only (drop macOS launchd) and treat macOS supervisor as Phase X scope.

## §11 cites

- `feedback_decision_priority` — operator IS the customer during PHASE AUTONOMY.
- `feedback_research_design_principles` — adoption-first; every wedge ≥2 OSS candidates.
- `feedback_grade_rubric` — B/A/A+ per wedge; PR body posts scorecard verbatim.
- `feedback_design_iteration_local` — local-first iteration before MVR-1 external surface.
- `feedback_review_every_step` — adversarial reviewer subagent in every wedge's A-tier rubric.
- `feedback_pr_body_file_only` — `--body-file` in dispatch templates.
- `feedback_pr_body_release_notes_mandatory` — release-notes fence in every PR body.
- `docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md` §4 Phase S3 close (entry) + §2 Gate 1 (exit).
- `docs/engineer/autonomous-session-prompt.md` — PRIORITY rewrite when PHASE AUTONOMY opens.

## §11 self-score against B/A/A+

| Tier | Criteria |
|---|---|
| B (floor) | (a) 7 wedges scoped + LoC-budgeted. (b) Three landings ordered by dependency. (c) Adoption-first per wedge. (d) Release-notes fence in PR body. |
| A (target) | B + (e) Each wedge cites ≥2 OSS candidates. (f) Adopt-vs-build verdict per wedge. (g) Entry + exit gate measurable. (h) Abandon-criterion per landing OR per phase. |
| A+ (stretch) | A + (i) Each wedge carries B/A/A+ rubric. (j) Dependency graph explicit between wedges. (k) Customer-benefit-first ordering visible in landing sequence (UX → ease → performance). (l) LoC-per-landing summary table. |

**Self-scored tier:** A+ — every criterion met. The amendment delta vs the unified brief: PHASE AUTONOMY closes the "operator-still-watching" gap the brief leaves between Phase S3 close and MVR-1 entry.
