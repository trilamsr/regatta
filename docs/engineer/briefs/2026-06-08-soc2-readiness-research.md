# SOC 2 readiness — research brief (TSC taxonomy + scope decisions)

_Author: design session, 2026-06-08. Companion to `docs/engineer/specs/2026-06-08-soc2-roadmap.md`. Phase-X gated per `docs/engineer/briefs/2026-06-01-self-host-first.md` §4 — research lands now, implementation deferred until first enterprise prospect ask._

## 1. Why this brief exists

Regatta today is single-operator, single-tenant, single-repo (`docs/engineer/briefs/2026-06-01-self-host-first.md` §1). No external customer holds data inside regatta; no contractual obligation to attest to controls. SOC 2 spend (audit firm $20-50k + readiness platform $7-15k/yr + 60-120 eng-days) is rejected at this phase per the self-host filter.

But the surface area we ship _today_ already covers a meaningful fraction of SOC 2's Security (Common Criteria) controls — append-only signed event log, HMAC chain audit, approval-gate primitives, secret routing, L4 reviewer-as-gate, branch protection. When the trigger fires (first enterprise ask + W8 multi-tenant landed), implementer should land slices without re-research. This brief captures the taxonomy + maps regatta primitives to it; the spec carries the slice roadmap.

## 2. Authoritative sources

Primary (AICPA, normative):

- AICPA, _2017 Trust Services Criteria for Security, Availability, Processing Integrity, Confidentiality, and Privacy (With Revised Points of Focus — 2022)_ — <https://www.aicpa-cima.com/resources/download/2017-trust-services-criteria-with-revised-points-of-focus-2022>. PDF, 554 KB, dated 2023-09-30. **The TSC document is the single normative source**; points-of-focus revised 2022 to align with COSO 2013 ERM updates.
- AICPA, _SOC Suite of Services_ landing — <https://www.aicpa-cima.com/topic/audit-assurance/audit-and-assurance-greater-than-soc-2>.
- AICPA Trust Services Principles superseded by the 2017 criteria; references to "Trust Services Principles" in older blog posts are stale terminology.

Secondary (vendor practitioner guides, useful for evidence patterns):

- Secureframe, _Trust Services Criteria_ — <https://secureframe.com/hub/soc-2/trust-services-criteria>.
- Secureframe, _SOC 2 Common Criteria_ — <https://secureframe.com/hub/soc-2/common-criteria>.
- Secureframe, _SOC 2 Controls List_ — <https://secureframe.com/hub/soc-2/controls>.
- Imperva, _SOC 2 Compliance overview_ — <https://www.imperva.com/learn/data-security/soc-2-compliance/>.

Sources NOT used (avoid blog-quality): Vendor sales pages with no AICPA cross-reference; Drata/Vanta marketing pages 404'd at fetch time and were dropped.

## 3. The Trust Services Criteria — taxonomy

SOC 2 attests against **five** Trust Services Categories. Only **Security** is mandatory; the other four are operator-elective and added when the service materially touches that surface. The 2017 TSC + COSO 2013 alignment means the Common Criteria embed COSO's 17 internal-control principles wholesale.

### 3.1 Security (mandatory) — the "Common Criteria" CC1-CC9

Nine subcategories. Every SOC 2 audit covers all nine.

| ID | Subcategory | Auditor question (paraphrased from AICPA 2017 TSC) |
|----|-------------|----------------------------------------------------|
| CC1 | Control environment | Does the organization demonstrate commitment to integrity, ethics, competence, accountability? |
| CC2 | Communication and information | Are policies, security obligations, and incident channels communicated internally + externally? |
| CC3 | Risk assessment | Is there a documented risk-assessment process? Does it consider fraud? |
| CC4 | Monitoring activities | Does the organization run ongoing AND separate evaluations of its controls? |
| CC5 | Control activities | Are technology, policy, and procedure controls selected and developed to mitigate risk? |
| CC6 | Logical and physical access | Identify + authenticate + authorize users; encrypt; restrict physical access; protect credentials. |
| CC7 | System operations | Detect, mitigate, respond to security events; vulnerability mgmt; anomaly monitoring. |
| CC8 | Change management | Authorize + design + develop + test + deploy + document infra and software changes. |
| CC9 | Risk mitigation | Identify, assess, mitigate business risks including sub-processor / vendor risk. |

### 3.2 Availability — A1 series (optional)

Adds when the service has uptime SLAs. Three control families:

- A1.1 — Capacity / performance planning.
- A1.2 — Environmental threat detection + mitigation.
- A1.3 — Backup, recovery, business-continuity testing.

Evidence shape: capacity-trend reports, backup-restore drill logs, BCP/DR runbooks with sign-offs.

### 3.3 Confidentiality — C1 series (optional)

Adds when the service handles non-PII confidential data (intellectual property, financial data, customer secrets). Two control families:

- C1.1 — Identify + maintain confidential information.
- C1.2 — Dispose of confidential information per policy + contract.

Evidence shape: data-classification matrix, retention/disposal logs, encryption-at-rest + in-transit attestations.

### 3.4 Processing Integrity — PI1 series (optional)

Adds when the service performs transactional processing customers depend on (payments, e-commerce, financial). Five control families:

- PI1.1 — Definition of processing-integrity objectives.
- PI1.2 — System inputs are complete + accurate.
- PI1.3 — Processing is timely + accurate.
- PI1.4 — Outputs are complete + accurate + distributed correctly.
- PI1.5 — Inputs/outputs stored and retained per policy.

### 3.5 Privacy — P1 through P8 (optional)

Adds when the service collects, uses, retains, or discloses PII. Eight control families aligned to AICPA's Generally Accepted Privacy Principles:

- P1 — Notice (privacy policy disclosed).
- P2 — Choice + consent.
- P3 — Collection (only what is needed).
- P4 — Use, retention, disposal.
- P5 — Access (subject-access requests).
- P6 — Disclosure to third parties.
- P7 — Quality of personal data.
- P8 — Monitoring + enforcement.

## 4. Type I vs Type II

- **Type I** — point-in-time attestation. Auditor confirms controls are _designed_ properly as of a single date. Typical engagement length: 6-10 weeks. Used as a first SOC 2 to "have a report" while gathering Type II evidence.
- **Type II** — operating-effectiveness window. Auditor samples evidence across a 3-12 month observation window (most common: 6 or 12 months). Used as the steady-state attestation enterprises will actually accept.

Engagement-cost asymmetry: Type I ≈ 1× audit fee; Type II ≈ 1.2-1.5× audit fee but **requires** a clean evidence-collection process running across the whole window (no retroactive backfill). For regatta this means: evidence-collection automation MUST land before the Type II window opens, not as the audit nears.

## 5. Audit-firm landscape (informational; no commitment)

Common SOC 2 attestation providers seen in startup ecosystem (alphabetical, not endorsed):

- A-LIGN, AssuranceLab, Insight Assurance, Johanson Group, Prescient Assurance, Schellman.
- Readiness platforms (NOT attestors) sit upstream: Drata, Secureframe, Vanta, Thoropass, Sprinto. They automate evidence collection but do not issue the SOC 2 letter.

Selection criteria belong to the trigger-time decision (cost, industry fit, AICPA peer-review record). This brief makes no recommendation.

## 6. Scope decision — what regatta will / won't claim

| TSC | Claim? | Justification |
|-----|--------|---------------|
| Security (CC1-CC9) | IN | Mandatory. Regatta dispatches code + handles credentials + writes to GitHub on operator's behalf — the surface is "security" by definition. |
| Availability (A1) | IN, low tier | Operator-merge flow tolerates regatta downtime — agent dispatch resumes at next tick. Claim availability at "best-effort daemon, restart-tolerant, no public SLA" until W12 multi-tenant adds paying-customer SLAs. |
| Confidentiality (C1) | IN | Regatta routes secrets (Anthropic API keys, GitHub tokens, HMAC keys via `internal/secrets/`) and writes audit events with confidential payload data. Confidentiality scope is structural, not opt-in. |
| Processing Integrity (PI1) | OUT initially, RECONSIDER on first enterprise ask | Regatta does not run customer transactions; it dispatches LLM-generated PRs. Operator-merge gate means humans (or `L4-as-review-identity` bot) make the binding decision. Without customer-binding transactional output, PI1 is over-scope. If a future buyer treats agent verdicts as authoritative outputs, reconsider. |
| Privacy (P1-P8) | OUT | Regatta processes no PII today. Customer issue text + PR diffs are operator's own repository contents; the operator is the only data subject. If multi-tenant W8 lands and customers grant regatta access to their own repos, the customer remains the data controller and regatta remains processor — P1-P8 may still defer to the customer's existing privacy program. Reconsider only when regatta ingests end-user PII directly. |

In-scope TSC = **Security + Availability + Confidentiality**. The spec roadmap (`2026-06-08-soc2-roadmap.md`) covers these three.

## 7. Trigger conditions (when to start)

Land the spec now; activate implementation only when **any** of:

1. First enterprise prospect explicitly asks "do you have SOC 2?" in writing (sales email, RFP line item).
2. W8 multi-tenant launches with ≥1 paying tenant (`docs/engineer/specs/2026-06-01-w8-opa-rbac-design.md`).
3. ARR crosses $1M annualized.
4. Paying-customer count crosses 5.

Earliest of those four = trigger fires. Implementation slice 1 (CC6 logical-access) dispatches that week.

## 8. What's already in regatta's favour

The following primitives reduce SOC 2 readiness effort substantially when the trigger fires:

| Primitive | Status | Maps to |
|-----------|--------|---------|
| Append-only signed event log (`internal/orchestrator/state/substrate/`) + HMAC chain (`cmd/regatta/audit.go`) | SHIPPED | CC7.2 anomaly detection, CC7.3 incident logging, A1.3 backup integrity, CC4.1 monitoring evidence. |
| Approval gates (`internal/canon/approvaltoken/`, `internal/gates/approval/`) | SHIPPED | CC6.1 logical access, CC8.1 change authorization. |
| Secret routing (`internal/secrets/` chain + keychain/pass + canonical key names) | SHIPPED | CC6.1 credential protection, CC6.7 secrets-at-rest. |
| Branch protection + `L4-as-review-identity` (`docs/engineer/specs/2026-06-02-phase-autonomy-w7-l4-as-review-identity.md`) | SHIPPED | CC8.1 change-management approval; PR-review separation-of-duties evidence. |
| Cost caps + budget reconcile (`docs/engineer/specs/2026-06-01-cost-governor-design.md`) | SHIPPED | CC4.1 monitoring of operational metrics; CC7 anomaly trigger (spend spike). |
| Cryptographic erasure design (`docs/engineer/specs/2026-06-02-crypto-shredding-design.md`) | SPECCED, Phase-X gated | C1.2 disposal of confidential information; later reusable for P4 retention. |
| Outbox primitive (`docs/engineer/specs/2026-06-02-external-effect-outbox-primitive.md`) | SPECCED | CC2.3 communication of incidents to external parties; future SIEM export. |

The headline gap is **evidence-collection automation**: today the substrate writes audit events but there is no SOC-2-shaped query interface, no operator-facing "show me every change to access-control config in the past 90 days" report. The spec carries that slice.

## 9. What this brief explicitly does NOT decide

Engagement-time decisions, deferred to trigger:

- Audit firm selection.
- Customer-signed DPA (Data Processing Agreement) templates.
- Sub-processor list publication (DPA Annex II equivalent).
- Type I vs Type II first-engagement choice.
- Legal review of policy documents.
- Readiness-platform selection (Drata / Vanta / Secureframe / none).
- Bug-bounty program activation.
- Penetration-test vendor selection.

These are not engineering decisions and should not pre-commit.

## 10. Adversarial pass on this brief

What a hostile reader would object to:

1. **"You're under-scoping privacy."** Counter: regatta-today processes only operator's own repo contents. Operator is sole data subject. Reconsider when multi-tenant W8 + customer-end-user-data ingestion lands.
2. **"PI1 belongs in scope because LLM verdicts ARE outputs."** Counter: every binding action passes through operator-merge or `regatta-reviewer-bot` L4 verdict. LLM output is advisory; the merge button is the authority. Reconsider if `--unattended-merge` flag ships without human gate.
3. **"You can't claim availability with no SLA."** Counter: AICPA TSC permits a defined availability commitment to be _stated by the entity_; a "best-effort with documented restart behaviour" disclosure is acceptable for the report. Re-examine at first paid SLA contract.
4. **"Why land design before trigger?"** Counter: cost of locked design = ~2 days of subagent time. Cost of re-research at trigger = ~5 days under sales pressure + risk of designing under-spec. Pre-fetch is net positive. (Same logic as Phase-X spec skeletons across the repo.)

## 11. Cross-references

- Roadmap spec: `docs/engineer/specs/2026-06-08-soc2-roadmap.md`.
- Self-host filter: `docs/engineer/briefs/2026-06-01-self-host-first.md` §1, §4.
- Audit-chain audit subcommand: `cmd/regatta/audit.go`.
- Substrate event log: `internal/orchestrator/state/substrate/event.go`.
- Secret routing: `internal/secrets/secrets.go`.
- Approval token canonical surface: `internal/canon/approvaltoken/token.go`.
- W7 L4-as-review: `docs/engineer/specs/2026-06-02-phase-autonomy-w7-l4-as-review-identity.md`.
- W8 RBAC + multi-tenant: `docs/engineer/specs/2026-06-01-w8-opa-rbac-design.md`.
- W10 supply-chain signing: `docs/engineer/specs/2026-06-01-w10-sigstore-design.md`.
- W12 billing: `docs/engineer/specs/2026-06-01-w12-billing-design.md`.
- Crypto-shredding: `docs/engineer/specs/2026-06-02-crypto-shredding-design.md`.
- Outbox primitive: `docs/engineer/specs/2026-06-02-external-effect-outbox-primitive.md`.
