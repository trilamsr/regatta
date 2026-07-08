# Engineering CHANGELOG

Rolling reverse-chronological log of shipped design decisions. Replaces the retained-forever spec-file model — once a spec ships (`status: shipped` / cites `closes #N` / marked superseded), its "why" collapses into one line here and the source file is deleted. Reach for `git log --follow` at the killed path to recover full historical text.

Scope: `docs/engineer/specs/` + `docs/engineer/briefs/`. Phase-X exploratory specs live in `git tag archive/phase-x-*`, not here.

## 2026-06-09

- Multi-phase WorkItem (#1083 #832): work items advance through explicit phases with per-phase gate + rebase semantics instead of monolithic pending→running→done.
- Pause/resume CLI for in-flight agents (#1078): operator can pause a spawned agent mid-tick and resume on a signed callback; state persists across daemon restart.
- Scheduler file-scope collision check (#1065): scheduler refuses to dispatch two items whose declared `file_scope` intersects; belt on top of shared-primitive owner rule.
- Daemon self-restart on binary update: supervisor watches its own inode; on binary swap, re-execs cleanly without losing in-flight ticks.

## 2026-06-08

- Cross-repo roadmap auto-discovery + zero-touch bootstrap: `regatta init` scans org for `.regatta/` bundles and stitches them into a unified board without manual wiring.
- `docs/engineer/dispatch-templates/*.md` modular split (#985): monolithic dispatch template split per-role (implementer, reviewer, designer, triage) so worker prompts only carry the relevant slice.
- External-Platform Spec Adapters: spec adapter tier discipline generalized beyond GitHub Issues — Linear, Jira, YouTrack land on the same `SpecAdapter` interface.
- Provider-halt gate: pre-call gate refuses to spawn when the target LLM provider is degraded per health probe; prevents cost burn on known outages.
- SOC 2 readiness roadmap: TSC taxonomy, in-scope boundary, control-implementation sequence.
- Svelte console UX design: dashboard visual overhaul; anchors dashboard + mission-control aesthetic thread.
- W4.5 self-improve detector rules R6-R11: six new self-improvement detectors covering flake-rate, cost-drift, prompt-parity, worktree-leak, reviewer-verdict-void, agent-liveness classes.

## 2026-06-07

- Autotuner closed-loop write-back (#1005 #988): autotuner writes measured latency/error back into schedule multipliers each tick; anchored by a bounded convergence test.
- Scheduler ↔ adaptersync MinPoll unification (#847 #888): single MinPoll knob replaces the two divergent poll clocks; drift eliminated at composition root.

## 2026-06-04

- BudgetReconciledPayload float-field deprecation (#709): sequenced cutover from `float64` cost fields to fixed-point `int64` cents; two-release deprecation with byte-equal replay parity.
- MVR-1-T4 github_issues spec adapter impl (#850): first tier-B adapter under the SpecAdapter interface; consumed by cross-repo roadmap discovery.
- Operator Console Phase-S UI roadmap: S1→S2→S3 sequence for the operator dashboard alongside the substrate cutover.
- Scheduler `filter.Apply` monomorphization bench (#753): ns/op + B/op + allocs/op captured across the 3 instantiation sites with stripped `cmd/regatta` binary-size baseline for the monomorphization cost debate.

## 2026-06-03

- Scheduler filter-helper consolidation (#251 #698): the divergent per-scope filter helpers collapse to a single `filter.Apply[Scope]` seam.
- MVR-2 T2 W8 multi-tenant `tenant_id` routing skeleton: routes carry tenant scope end-to-end; single-tenant self-host mode default.
- MVR-2 T4 P3.8 LLM-gateway adapter skeleton: pluggable LLM provider behind adapter interface; foundation for BYOM.
- MVR-3-T1 Sigstore skeleton: cosign behind signer adapter; skeleton pre-fetch tier.
- MVR-3-T2 Stripe metering skeleton: usage-based billing adapter surface.
- MVR-3-T3 Blackboard skeleton: sqlite-CAS + fact subscription primitive.

## 2026-06-02

- Approval-gate per-JTI persistence (#195 / #332): reaper revocation branch reaches the event log because token-mint rows persist per JTI; closes the silent-drop where a revoked token left no audit trace.
- MVR-1 T1 W7 Wave-1 htmx UI MVP: first operator web surface; htmx-only, no SPA.
- Phase OBS-D operator surface (D-T1 + D-T2 + D-T3): dashboard event-vocabulary + slow-poll widget stack.
- Orchestrator PR Watcher (#15 #520 #527): first-class PR-status watch loop feeding scheduler + reaper.
- PHASE AUTONOMY §11 W2 c2 merge-execute: `gh pr merge` wired on top of the c0 approval-token substrate.
- PHASE-AUTONOMY W3 service supervisor: process supervisor + tick heartbeat.
- PHASE-AUTONOMY W4 self-improvement detector: R1-R5 detectors + evidence-log pipeline.
- PHASE-AUTONOMY W6 secret-credential autonomic fetch: OS keyring + rotation-aware credential resolver.
- S1-T1 `regatta.yaml` for this repo + `.regatta/items/` bootstrap: first self-host bootstrap; regatta dispatches regatta.
- S1-T3 boot-prompt → work_item brief converter: converts the human boot prompt into a serialized brief the scheduler can dispatch.
- S1-T5 self-host smoke test: Phase-S1 acceptance gate — full loop against this repo, green.
- S2-T1 W9 Replay+Diff Harness, Substrate-Default DurableHistory impl: substrate v2 replay-parity harness lands as default.
- S2-T3 GitHub [followup] issues → work_item briefs: triage adapter routes labelled issues into the scheduler.
- S2-T4 mutation testing on cost-governor + scheduler: mutation-verify surface expands to the two most cost-sensitive packages.
- S3-T1 W8 OPA slim (policy hot-reload single-tenant): OPA policy hot-reload without a restart on single-tenant deployments.
- S3-T2 substrate Phase B+C cutover (approvals only): approvals migrate to substrate v2; other subsystems deferred.
- S3-T3 HMAC key-rotation drill + operator recovery doc: rehearsed rotation with reproducible operator playbook.
- S3-T4 crash-recovery property test: property harness asserts fold-equivalence across arbitrary crash points.

## 2026-06-01

- MVP-4 W10 Sigstore cosign + Rekor transparency log (design): supply-chain provenance for regatta-authored artifacts.
- MVP-4 W11 Blackboard (design): typed facts + reducers + CAS blobs as the substrate for agent coordination.
- MVP-3 W7 Operator Web UI (design v2): full operator dashboard scope; supersedes v1.
- MVP-3 W7 Wave-7.2 admin pages (T8 + T9): DAG list + run detail admin pages.
- MVP-3 W8 OPA RBAC + multi-tenant + policies primitive: RBAC design with a first-class `policies` primitive; deferred Phase-X in self-host filter.
- Research-Mode Extension: autonomous empirical AI/CS research on existing regatta primitives.

## 2026-05-31 and earlier (briefs)

- Dashboard redesign — mission-control aesthetic (#1215 #1217, 2026-06-10): full visual language reset for the operator dashboard.
- Regatta UX Audit — Phase-S self-host loop (2026-06-10): audit against the sole internal operator's dispatch loop; source of many Phase-S wedges.
- Adapter terminal-status recognition + log-level downgrade (2026-06-09): terminal statuses no longer emit WARN spam.
- ETag conditional GET impl brief (#1164, 2026-06-09): Rung-1 polling optimization.
- smee.io / `gh webhook forward` webhook hybrid impl (#1165, 2026-06-09): Rung-4 polling optimization.
- SOC 2 readiness research (2026-06-08): TSC taxonomy + scope decisions feeding the design spec.
- Autotuner closed-loop design brief (2026-06-04): design predecessor to the shipped spec above.
- Roadmap reorder — self-improvement + meta-improvement promotion (2026-06-04): promoted W4/W4.5 ahead of external wedges.
- OBS-C T2 PR-watch collector wedge (#633, 2026-06-03): observability collector for the PR watcher.
- regatta next-horizon roadmap (post-self-host, 2026-06-02): post-Phase-S wedge sequence.
- PHASE AUTONOMY §11 amendment (2026-06-02): §11 insert covering supervisor + reaper cadence.
- regatta architecture simplification pass (collapse-before-extend, 2026-06-01): repo-wide simplification pass; motivated the god-file splits.
- regatta research-mode vision brief (Phase X candidate, 2026-06-01): research-mode direction set aside behind Phase-X trigger.
- regatta self-host-first roadmap reorder (2026-06-01): the canonical Phase-S filter this doc's scope now enforces.
- regatta next-level design brief (post MVP-2, 2026-05-31): post-MVP-2 vision; supersedes MVP-3 admin roadmap.
- Phase-X roadmap consolidated tracking surface (#277): consolidated the pre-filter phase-x tracker.
