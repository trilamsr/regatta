---
title: "SOC 2 readiness roadmap — Trust Services Criteria → regatta primitives"
status: skeleton-prefetch
phase: x-forward-fit
summary: "SOC 2 readiness roadmap. Five slices map TSC Common Criteria + Availability + Confidentiality to existing or planned regatta primitives. Phase-X gated; activates on first enterprise prospect ask OR `W8` multi-tenant launch OR $1M ARR OR 5+ paying customers. Companion brief at docs/engineer/briefs/2026-06-08-soc2-readiness-research.md carries the TSC taxonomy + scope decisions."
---

# SOC 2 readiness roadmap — design spec, v1

_Author: design session, 2026-06-08. Companion: `docs/engineer/briefs/2026-06-08-soc2-readiness-research.md` (TSC taxonomy + sources + scope decisions). Phase-X gated per `docs/engineer/briefs/2026-06-01-self-host-first.md` §4._

Sources of truth:

- AICPA, _2017 Trust Services Criteria with Revised Points of Focus — 2022_ — <https://www.aicpa-cima.com/resources/download/2017-trust-services-criteria-with-revised-points-of-focus-2022>.
- Companion brief §3 (TSC taxonomy) + §6 (scope decision) + §8 (existing primitives table).
- Self-host filter: `docs/engineer/briefs/2026-06-01-self-host-first.md` §1, §4.

Memory rules in force: `feedback_default_simpler` (5 slices, not 20), `feedback_research_design_principles` (reuse shipped primitives over reimpl), `feedback_deletion_default` (every slice answers what it deletes or reuses), `feedback_unaddressed_load_bearing` (every deferred control → tracker on activation), `feedback_audit_main_before_implementing` (pre-dispatch grep against `origin/main`).

---

## 1. Trigger conditions

Spec inert until **any** of the following fires:

1. First enterprise prospect explicitly requests SOC 2 in writing (sales email, RFP, MSA addendum).
2. `W8` multi-tenant authorization launches with ≥1 paying tenant (`docs/engineer/specs/2026-06-01-w8-opa-rbac-design.md`).
3. ARR crosses $1M annualized.
4. Paying-customer count crosses 5.

Earliest-of-four fires implementation. Slice 1 (CC6 logical access) dispatches that week. The companion brief §7 carries the rationale.

## 2. Scope decision (in / out)

In-scope Trust Services Categories — claimed in the SOC 2 report:

- **Security** (Common Criteria CC1-CC9). Mandatory.
- **Availability** (A1.1-A1.3). Low-tier — "best-effort daemon with documented restart behaviour", no public SLA until W12 billing ships customer contracts.
- **Confidentiality** (C1.1-C1.2). Regatta routes secrets + writes confidential audit payloads.

Out-of-scope categories — explicitly **not** claimed:

- **Processing Integrity** (PI1). LLM verdicts are advisory; every binding action passes through operator-merge or the `regatta-reviewer-bot` L4 identity. Reconsider only if `--unattended-merge` ships without a human gate.
- **Privacy** (P1-P8). Regatta processes no end-user PII today; operator is the sole data subject. Reconsider when end-user PII ingestion lands.

Rationale for each exclusion: companion brief §6.

## 3. Gap analysis — TSC → regatta primitive map

Status legend: `SHIPPED` on main; `SPECCED` design locked, Phase-X gated; `NEEDS-DESIGN` no spec yet; `N/A` does not apply at self-host phase.

### 3.1 Common Criteria (mandatory)

| TSC ID | Control intent | Regatta primitive | Status |
|--------|----------------|-------------------|--------|
| CC1.1-CC1.5 | Control environment — integrity, ethics, accountability | `CLAUDE.md` repo rules + `docs/engineer/dispatch-templates/{implementer,reviewer,designer,triage}.md` + per-PR adversarial-review pass | SHIPPED (process); NEEDS-DESIGN (formal policy doc set for auditor) |
| CC2.1-CC2.3 | Communication of policies + incident channels | Repo-resident rules (CLAUDE.md); auto-emitted alarm-webhook (`internal/alarmwebhook/`); GitHub Issues as incident channel | SHIPPED (mechanisms); NEEDS-DESIGN (external-facing policy publication) |
| CC3.1-CC3.4 | Risk assessment + fraud consideration | Per-wave reviewer subagent (adversarial pass); `feedback_adversarial_review_every_step` rule | SHIPPED (per-PR); NEEDS-DESIGN (annual risk-assessment artifact) |
| CC4.1-CC4.2 | Monitoring activities — ongoing + separate evaluations | OBS Wave-B substrate-health dashboards (`docs/engineer/specs/2026-06-02-obs-wave-b-substrate-health.md`); HMAC chain-break sweeper; cost-cap monitor | SHIPPED |
| CC5.1-CC5.3 | Control activities — tech, policy, procedure | CUE config validation (`internal/config/validate/`); `make check` gates; banned-phrase + scorecard scripts | SHIPPED |
| CC6.1 | Logical access — authn, authz, account lifecycle | `internal/secrets/` (operator credentials); approval-token HMAC (`internal/canon/approvaltoken/`); single-operator today | PARTIAL — multi-operator authn = Phase-X `W8` |
| CC6.2 | Provision + de-provision access | N/A self-host (single operator) | N/A → Phase-X `W8` |
| CC6.3 | Authorize role-based access | N/A self-host; planned `W8` authorizer | SPECCED (Phase-X) |
| CC6.6 | Restrict logical access — boundary protection | `regatta serve` listens only on localhost; cookie-HMAC on approval surface | SHIPPED |
| CC6.7 | Restrict information transmission, storage, removal | Audit chain ciphertext-stable signing (`cmd/regatta/audit.go`); crypto-shredding design | SHIPPED (in-place chain); SPECCED (shredding) |
| CC6.8 | Detect + prevent unauthorized software | L0/L4 gates (`internal/gates/l0/`, `internal/gates/l4/`); `make check` CI gates; CUE plan-script gate (`docs/engineer/specs/2026-06-02-mvr-3-t5-script-plan-cue-gate.md`) | SHIPPED |
| CC7.1 | Detect security events — vulnerability mgmt | `make check` includes `govulncheck`; dependabot wiring; security gates (`internal/gates/security/`) | SHIPPED |
| CC7.2 | Monitor system components for anomalies | OBS Wave-B HMAC chain-break sweeper + event-rate stall alarm | SHIPPED |
| CC7.3 | Evaluate incidents + take action | Operator alarm-webhook (`internal/alarmwebhook/`); substrate event timeline; no formal IR runbook today | NEEDS-DESIGN (IR runbook + escalation matrix) |
| CC7.4 | Recovery from identified incidents | `regatta serve` restart-tolerant via substrate replay; per-spec `Phase-S3-T3 HMAC key-rotation drill` | SHIPPED (restart); SHIPPED (key-rotation drill spec) |
| CC7.5 | Identify + remediate vulnerabilities | `make check` `govulncheck` + `lint` + `vet`; dependabot PRs through same L4 gate | SHIPPED |
| CC8.1 | Authorize + design + develop + test + deploy changes | TDD-first discipline (CLAUDE.md); L4 reviewer gate; branch protection; squash-merge with PR linkage; `make pre-push-check`; `regatta merge` audit event | SHIPPED |
| CC9.1 | Identify + mitigate business disruptions | Cost-cap autonomic enforcement (`docs/engineer/specs/2026-06-02-phase-autonomy-w5-cost-cap-autonomic-enforcement.md`); supervisor restart (`docs/engineer/specs/2026-06-02-phase-autonomy-w3-service-supervisor.md`) | SHIPPED |
| CC9.2 | Vendor + sub-processor risk management | Today: GitHub, Anthropic. Documented in `docs/auditor/`?; no formal sub-processor list | NEEDS-DESIGN (sub-processor inventory + DPA tracker) |

### 3.2 Availability (A1)

| TSC ID | Control intent | Regatta primitive | Status |
|--------|----------------|-------------------|--------|
| A1.1 | Capacity / performance planning | Cost governor pre-call caps + token estimation (`docs/engineer/specs/2026-06-01-cost-governor-design.md`); SLO compile test (`make check` → `slo-compile-test`) | SHIPPED |
| A1.2 | Environmental threat mitigation | Single-operator workstation = N/A for hosted infra; reconsider at hosted deployment | N/A self-host |
| A1.3 | Backup + recovery + BC/DR drills | Substrate event log = source of truth, sqlite file backup is operator-owned; replay-diff harness (`docs/engineer/specs/2026-06-01-w9-replay-diff-harness-design.md`); HMAC key-rotation drill | SHIPPED (replay); NEEDS-DESIGN (backup-restore drill cadence + evidence) |

### 3.3 Confidentiality (C1)

| TSC ID | Control intent | Regatta primitive | Status |
|--------|----------------|-------------------|--------|
| C1.1 | Identify + maintain confidential information | Secret canonical key set (`internal/secrets/secrets.go::CanonicalKeys`); CUE-modeled secret routing; redaction wrapper (`secrets.Value`) | SHIPPED |
| C1.2 | Dispose of confidential information per policy | Crypto-shredding design (`docs/engineer/specs/2026-06-02-crypto-shredding-design.md`); outbox primitive for downstream disposal signalling | SPECCED (Phase-X) |

## 4. Roadmap slices

Five slices, each = independent PR sequence. Default-simpler: pick the simplest viable per slice; reject controls that already pass through the audit chain or substrate sweepers. Each slice is dispatchable as one designer subagent + one implementer wave; cap parallel implementers at 3-4 per the dispatch rules.

### 4.1 Slice 1 — CC6 logical access (authn, authz, MFA, secret rotation)

**Trigger to dispatch**: trigger fired (§1) AND `W8` multi-tenant authorization is merged.

**Scope**:

1. Document the existing single-operator → multi-operator path as the SOC 2 access-provisioning runbook. Cite `W8` OPA authorizer (`internal/authz/`).
2. Add operator-facing `regatta access list` CLI surfacing principals + last-login event from the substrate.
3. Add quarterly-access-review prompt — substrate event `kind: access_review` written when operator confirms the principal list. Auditor reads the event stream as evidence.
4. Document secret-rotation cadence per canonical key (`internal/secrets/secrets.go::CanonicalKeys`); land `regatta secret rotate <key>` CLI that emits `secret_rotated` substrate events.
5. Reject: MFA implementation. Defer to upstream IdP (Okta / Auth0 / Keycloak via `W8` followup). Regatta does not own identity; documenting the IdP boundary IS the control.

**Reuses**: `internal/secrets/`, `internal/authz/` (post-`W8`), substrate event log. **What gets smaller**: zero new primitive — the slice is documentation + thin CLI on top of shipped state.

**Auditor-reject (most adversarial finding)**: "You cannot evidence quarterly access reviews when the operator is the sole reviewer of their own access." → Counter-control: the access-review substrate event MUST be signed by a second operator's approval-token; until a second principal exists, claim N/A on CC6.2 and document the constraint in the system description. **Weakest link**: single-operator phase cannot satisfy separation-of-duties on the access review itself; only `W8`-with-≥2-operators unblocks an A+ posture on this control.

### 4.2 Slice 2 — CC7 system operations (incident response, vulnerability mgmt)

**Trigger to dispatch**: trigger fired (§1). Independent of `W8`.

**Scope**:

1. Author `docs/runbooks/incident-response.md` — escalation matrix (operator → on-call → public-disclosure decision), severity rubric, communication template. Cite `internal/alarmwebhook/` as the inbound channel.
2. Add `incident` event kind to the substrate (reducer = LWW per `incident_id`). Auditor reads the timeline (declared → mitigated → root-caused → closed) as evidence.
3. Add `regatta incident {open,update,close}` CLI; payload carries severity, blast-radius, services-affected.
4. Re-classify existing alarm-webhook traffic as `incident` events when severity ≥ medium. Lower severity stays as a counter-only metric.
5. Reject: 24/7 SOC tooling, PagerDuty integration, SIEM forwarder. Defer to operator's choice via outbox primitive (`docs/engineer/specs/2026-06-02-external-effect-outbox-primitive.md`).

**Reuses**: substrate event log, alarm-webhook, outbox primitive. **What gets smaller**: alarm-webhook outputs converge with substrate events — one timeline replaces two.

**Auditor-reject**: "Your incident log is one operator's CLI — there is no time-stamping authority guaranteeing the events were not written after the fact." → Counter-control: the HMAC chain (`cmd/regatta/audit.go`) covers `incident` events the same way it covers `gate_verdict` events; chain-break detection (OBS Wave-B sweeper) gives tamper-evidence. **Weakest link**: HMAC key custody is single-operator — same custodian who writes the events also holds the signing key. Mitigate with W10 supply-chain signing (`docs/engineer/specs/2026-06-01-w10-sigstore-design.md`) once that wedge activates; that adds a third-party transparency log via the `Rekor` public-good service so the operator cannot back-date events. Until W10 activates, the SOC 2 system description MUST disclose this limitation.

### 4.3 Slice 3 — CC8 change management (PR review gates — extend, do not rebuild)

**Trigger to dispatch**: trigger fired (§1). Partial scope already SHIPPED via L4 + branch protection + `make pre-push-check`.

**Scope**:

1. Document the existing change-management pipeline as the SOC 2 CC8.1 narrative. One markdown doc at `docs/auditor/change-management.md`. Cite: TDD-first discipline (CLAUDE.md), `make check` gate list, L4 reviewer-bot identity (`docs/engineer/specs/2026-06-02-phase-autonomy-w7-l4-as-review-identity.md`), branch-protection state (`feedback_branch_protection_strict`), squash-merge with PR linkage.
2. Add `regatta change-evidence --window <duration>` CLI emitting per-PR rows: PR#, merge-SHA, reviewer-identity, approval-token-event-id, gates-passed. Output is the slice-4 evidence-collection consumer.
3. Add `change_classification` substrate event written at `gh pr create` time tagging the release-notes prefix (`[FEAT]/[FIX]/[CHORE]/...`). Auditor reads classification distribution as control-coverage evidence.
4. Reject: a separate change-advisory-board workflow. The L4 reviewer + operator-merge is the CAB equivalent and is already mandatory.

**Reuses**: existing L4 gate, branch protection, substrate event log. **What gets smaller**: zero new gate — slice 3 is the audit narrative + read-side CLI.

**Auditor-reject**: "Operator can approve their own PR — separation of duties fails." → Counter-control: L4 reviewer-bot (`docs/engineer/specs/2026-06-02-phase-autonomy-w7-l4-as-review-identity.md`) runs under a dedicated service-account identity; the W7 spec already pre-handles GitHub's self-approval 422. Document this in the system description. **Weakest link**: same operator owns both the reviewer-bot service-account credentials AND the human merge button. Add a `regatta reviewer-identity attest` CLI emitting a daily substrate event proving the reviewer-bot token has not been swapped to the operator's PAT; until enterprise customers add per-tenant service-accounts, the operator-trust assumption is documented, not eliminated.

### 4.4 Slice 4 — audit-evidence-collection automation

**Trigger to dispatch**: trigger fired (§1). Highest leverage — converts shipped primitives into auditor-shaped query output.

**Scope**:

1. Add `regatta evidence` subcommand tree on top of `cmd/regatta/audit.go` patterns. Subcommands:
   - `regatta evidence access-review --since <date>` — every `access_review` event + access changes since date.
   - `regatta evidence incidents --since <date>` — every `incident` event with severity + duration.
   - `regatta evidence changes --since <date>` — every PR merge + classification (slice 3).
   - `regatta evidence backups --since <date>` — every backup-completed event (slice 5).
2. Each subcommand returns JSON + table format mirroring `audit verify`; auditor consumes JSON, operator consumes table.
3. Each subcommand verifies the HMAC chain over the returned event set and refuses to emit on chain-broken (reuses `cmd/regatta/audit.go::loadAuditVerifyKeyring`).
4. Add `docs/auditor/evidence-mapping.md` — table mapping each TSC criterion to the `regatta evidence` invocation that produces evidence for that criterion. Auditor reads the table, runs the commands, gets the JSON. No manual evidence collection.
5. Reject: a hosted "compliance dashboard" UI. CLI + JSON suffices; readiness platforms (Drata / Vanta / Secureframe) consume JSON directly via their evidence-upload APIs.

**Reuses**: `cmd/regatta/audit.go` patterns, substrate event log, HMAC chain verification. **What gets smaller**: replaces ad-hoc auditor-evidence-collection scripts with one structured surface.

**Auditor-reject**: "Your evidence is only as trustworthy as your HMAC chain — and your chain is only as trustworthy as the key custody." → Counter-control: same answer as slice 2 + W10 supply-chain signing wedge. **Weakest link**: same single-key-custodian limitation as slice 2. Cross-document with the slice 2 disclosure to avoid two contradictory mitigation claims.

### 4.5 Slice 5 — Type II operating-effectiveness window (90-day vs 12-month)

**Trigger to dispatch**: trigger fired (§1) AND slices 1-4 are MERGED.

**Scope**:

1. Decide window length at engagement time: 3-month bridge (gets a first letter fast) vs 6-month (most enterprise prospects accept) vs 12-month (annual cadence default). Default recommendation: 6-month first letter, then 12-month annual.
2. Pre-window readiness checklist — at trigger-fire time, run `regatta evidence` over the previous 90 days. Gaps that produce zero events (e.g., zero `access_review` events) are pre-window red flags. Operator addresses the gap before window opens.
3. Mid-window monitoring — weekly `regatta evidence` invocation gated by the OBS Wave-B substrate-health dashboard. Any control with zero evidence in the past 30 days alarms via the existing alarm-webhook.
4. Add `regatta evidence summary --window <start>..<end>` — one-shot rollup the operator hands to the auditor at engagement open. Cites which events satisfied which TSC criteria.
5. Backup-restore drill cadence — schedule `regatta restore --drill` per quarter, write `backup_drill_completed` substrate event. Mandatory evidence for A1.3.
6. Reject: any pre-engagement "policy templates pack" purchase. The repo-resident dispatch templates + CLAUDE.md ARE the policies; auditor reads them in the repo.

**Reuses**: slices 1-4 entirely. **What gets smaller**: turns the per-quarter manual evidence audit into one CLI invocation.

**Auditor-reject**: "Six months is fine for the first letter, but enterprise procurement will ask for the 12-month Type II letter on renewal — your shipped evidence-collection cadence must survive a year unattended." → Counter-control: cost-cap autonomic enforcement (W5) + supervisor (W3) + alarm-webhook = unattended-tolerant by construction. **Weakest link**: a year-long unattended window will hit a regatta version bump + schema migration; the SchemaSkew column in `audit verify` becomes an audit observation. Mitigate by pinning regatta version at engagement open AND landing one substrate-schema-migration drill before window-2.

## 5. Evidence-collection design

The audit chain (`cmd/regatta/audit.go` + `internal/orchestrator/state/substrate/`) is the single source of truth for SOC 2 evidence. Slice 4 wraps it; slices 1-3 produce events; slice 5 consumes the rollup.

### 5.1 Event-kind additions

Five new substrate event kinds — one per slice. Reducer = LWW unless noted:

| Kind | Slice | Reducer | Payload (sketch) |
|------|-------|---------|------------------|
| `access_review` | 1 | LWW per (reviewer_id, reviewed_at) | principals[], confirmed_by, decisions[] |
| `secret_rotated` | 1 | append | key_name, old_keyid, new_keyid, rotation_reason |
| `incident` | 2 | LWW per incident_id | severity, services_affected, declared_at, mitigated_at, root_cause |
| `change_classification` | 3 | LWW per pr_id | pr_id, release_notes_prefix, merge_sha, reviewer_identity |
| `backup_drill_completed` | 5 | append | drill_id, restored_from_at, restored_to_at, integrity_check_passed |

Every kind rides the same HMAC chain `cmd/regatta/audit.go::loadAuditVerifyKeyring` validates. Same backup discipline. Same chain-break-sweeper coverage. Zero new cryptographic primitive.

### 5.2 Auditor-facing surface (`regatta evidence`)

Slice 4 lands the `regatta evidence` subcommand tree. Each subcommand:

1. Reads the relevant substrate event set via `substrate.Fold`.
2. Runs the existing HMAC verifier (`substrate.Verify`) per row.
3. Emits JSON (default) or table format mirroring `audit verify`.
4. Exits non-zero on any `chain-broken` row OR missing HMAC key.

Auditor workflow: clone repo, set HMAC key, run `regatta evidence access-review --since 2026-01-01 --format json > access-review.json`. Upload to readiness platform. Repeat per evidence-mapping row.

### 5.3 Read-mostly, no new write paths

Slices 1, 2, 3, 5 add **one** write path each (the new event kind). Slice 4 adds **zero** new write paths — it is pure read. Total cryptographic-primitive additions across the roadmap: zero. Every event rides the chain that audit-verify already proves intact.

## 6. Adversarial pass — overall

Aggregate "what would an auditor reject" findings, deduped from per-slice sections:

1. **Audit-trail integrity**: HMAC chain satisfies CC7.2 (tamper-evidence) but single-key-custodian limits the assurance. W10 supply-chain signing (with `Rekor` transparency log) lifts the bound; document until W10 activates.
2. **Access controls**: single-operator phase is structurally N/A for separation-of-duties; `W8` multi-tenant lifts. Until then, system description discloses the constraint per CC6.2.
3. **Vendor management** (CC9.2): GitHub, Anthropic, OS keyring/pass (per `internal/secrets/`). Sub-processor list lands as `docs/auditor/sub-processors.md` at slice 1 dispatch time. DPAs are engagement-time legal work — out of engineering scope.
4. **Change management** (CC8.1): L4 + branch-protection + `make pre-push-check` is sufficient for an A-tier control posture; A+ requires the slice 3 reviewer-identity-attest CLI to close the operator-self-trust gap.
5. **Incident response** (CC7.3): operator notices via alarm-webhook today; runbook (slice 2) closes the documentation gap; the substrate `incident` event closes the timeline-of-record gap.
6. **Crypto-shredding** (C1.2): SPECCED via `docs/engineer/specs/2026-06-02-crypto-shredding-design.md`; trigger to land before first regulated-customer pilot. Independent of SOC 2 trigger, but C1.2 evidence won't be claimable until crypto-shred lands.

## 7. Default-simpler defenses (per-slice why-not-more)

Each slice rejects scope inflation. Aggregated rejections + why:

- Reject MFA implementation: defer to upstream IdP. (Slice 1)
- Reject SIEM forwarder, PagerDuty, 24/7 SOC: operator's choice via outbox. (Slice 2)
- Reject separate CAB workflow: L4 + operator-merge IS the CAB. (Slice 3)
- Reject hosted compliance UI: CLI + JSON consumed by readiness platforms. (Slice 4)
- Reject pre-engagement policy templates: repo-resident CLAUDE.md + dispatch templates ARE the policies. (Slice 5)

Total LOC growth: ≤2000 (five new event kinds + one CLI subcommand tree + four runbook docs). Total deletion: zero — the slices are net-additive but every addition reuses an existing primitive (HMAC chain, substrate, alarm-webhook).

## 8. Out of scope (engagement-time decisions)

Explicitly NOT decided by this spec:

- Third-party audit firm selection (companion brief §5).
- Customer DPA signature workflow + legal review.
- Type I vs Type II first-letter choice (slice 5 §1 captures the default recommendation; engagement may override).
- Readiness-platform purchase (Drata / Vanta / Secureframe / none — slice 4 CLI works with all).
- Bug-bounty program activation.
- Penetration-test vendor selection.
- Customer-facing trust-portal hosting.

These are not engineering decisions. The roadmap does not pre-commit them.

## 9. Closes / tracks

- Companion brief: `docs/engineer/briefs/2026-06-08-soc2-readiness-research.md`.
- Self-host filter: `docs/engineer/briefs/2026-06-01-self-host-first.md` (§1, §4).
- `W8` multi-tenant authorization (Phase-X; soft trigger): `docs/engineer/specs/2026-06-01-w8-opa-rbac-design.md`; tracker `#492`, `#221`.
- `W10` supply-chain signing (Phase-X; mitigation for slices 2, 4): `docs/engineer/specs/2026-06-01-w10-sigstore-design.md`; tracker `#617`.
- `W12` metered billing (Phase-X; A1 SLA enablement): `docs/engineer/specs/2026-06-01-w12-billing-design.md`; tracker `#729`.
- Crypto-shredding (Phase-X; C1.2 enablement): `docs/engineer/specs/2026-06-02-crypto-shredding-design.md`; tracker `#548`, `#606`, `#607`.
- GDPR Article 17 (Phase-X; intersects P4 if P-series re-enters scope): tracker `#606`.
- Outbox primitive (slices 2, 4 dependency): `docs/engineer/specs/2026-06-02-external-effect-outbox-primitive.md`; tracker `#551`.
- Audit chain (slice 4 substrate): `cmd/regatta/audit.go`.
- Approval gates (slice 1 substrate): `internal/canon/approvaltoken/`.
- Secret routing (slice 1 substrate): `internal/secrets/secrets.go`.
- L4 reviewer identity (slice 3 substrate): `docs/engineer/specs/2026-06-02-phase-autonomy-w7-l4-as-review-identity.md`.
- HMAC key-rotation drill (slice 1 dependency): `docs/engineer/specs/2026-06-02-s3-t3-key-rotation-drill.md`.
- Replay-diff harness (A1.3 substrate): `docs/engineer/specs/2026-06-01-w9-replay-diff-harness-design.md`.

At trigger-fire time, the implementer dispatching slice 1 MUST file tracker issues for each `NEEDS-DESIGN` row in §3 that the slice does not absorb. Mirrors `feedback_unaddressed_load_bearing`.
