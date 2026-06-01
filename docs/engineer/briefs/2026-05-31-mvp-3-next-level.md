# regatta — next-level design brief (post MVP-2)

_Author: design subagent, 2026-05-31. Source-of-truth: `memory/wedge_roadmap_assessment.md` + W1/W2/W3 wedge dossiers + autonomous-session prompt._

## 1. Where regatta IS, post-MVP-2 (day after Wave 5 lands)

- Planner-as-DAG (MVP-1) + L0 spec-immutability + lane caps + crash recovery — _shipped_.
- Approval gates (HITL): HMAC token, escalation tiers, reaper, slog audit, CLI `regatta approve` — `wedge_approval_gates` complete (P2+P3).
- Cost governor: per-DAG/operator USD+token caps, soft (80%) downgrade Sonnet→Haiku, hard pause, Anthropic Usage-API reconciliation — `wedge_cost_governor` (P8).
- Plan-as-code: `.regatta/plans/*.yaml`, CUE-validated, planner emits / executor consumes, signed for protected lanes — `wedge_plan_as_code` (P3+P10).
- Conditional DAG: CEL-predicated edges over journaled outputs, mandatory `default_next`, deadlock-rejecting validator — `wedge_conditional_dag` (P1).
- Structured-slog observability (#101/#113) + resource model (typed lock generalization, P5) — supporting backbone.

## 2. Gap between MVP-2 and "production-credible control plane"

- **Operator-grade observability**: slog is print-debugging — no OTel traces, no GenAI semconv, no dashboards. Pilot ops can't answer "why did DAG-X run 4× expected cost?" without reading log files.
- **No durable-execution substrate**: bespoke sqlite scheduler + reaper handles retries, but no signal-replay, no workflow-versioning, no cross-host failover. Single-host SPOF.
- **No RBAC / multi-tenant story**: today every operator can approve any gate, mutate any plan, read any audit row. No tenant scoping, no policy-as-code.
- **No secrets posture**: Anthropic/MCP keys live as env vars; no rotation, no scoped credentials per DAG-run, no break-glass audit.
- **No web UI**: CLI-only. Approver experience for #114 = email/Slack link → copy token → paste in terminal. Loses to GHA reviewer-set UX for non-eng approvers.
- **No supply-chain trust**: signed plans exist (P10) but no Sigstore/in-toto attestation chain proving _which model_ produced _which plan_ with _which inputs_. Regulated buyers can't onboard.
- **No billing/usage export**: cost governor records spend internally; no per-tenant invoice CSV, no Stripe metered-billing webhook, no chargeback report.
- **No replay/diff**: deferred to MVP-3. Today a failed DAG-run is forensically unreproducible without manual journal spelunking.

## 3. Next-level vision

**Tagline: regatta = the SOC-2-credible flight deck for autonomous developer agents.** Control-plane-for-AI-labor framing _holds_ but sharpens: the moat is not "we orchestrate LLMs" (Temporal+Inngest can) — it's "we ship the **operator surface** (UI + audit + budget + approvals + provenance) that regulated engineering orgs need to greenlight unattended AI labor in production repos." MVP-3 closes the observability+RBAC+UI gaps so a Fortune-500 platform team can run a pilot. MVP-4 closes the durability+billing+supply-chain gaps so they can deploy multi-tenant. GA = swap-out architecture: Temporal optional (single-binary still works), OPA optional (built-in RBAC ships defaults), Helicone/Portkey optional (native LLM gateway). Every seam exposes a proven OSS adapter contract so adopters bring their own backend.

## 4. Ranked wedge sequence for MVP-3 + MVP-4

### W6 — OTel + GenAI semconv observability backbone  _(MVP-3, rank #1)_
- **Elevator**: every span, event, and LLM call exports OTel with GenAI semconv attributes — drop-in to Honeycomb/Grafana/Datadog/Tempo.
- **Trap fit**: enables P8 (cost telemetry feedback loop) + cross-cutting on P1/P2/P3 audit verification.
- **Prior art adopted**: OpenTelemetry Go SDK + GenAI semantic conventions (`gen_ai.system`, `gen_ai.request.model`, `gen_ai.usage.input_tokens`); slog→OTel bridge via `otelslog`. Rejected bespoke — OTel is the only viable adopt (already industry default, GenAI semconv ratified 2025).
- **Why now**: pilot ops cannot debug without traces; cost governor reconciliation needs canonical token counts; approval-gate forensic chain (#80) needs durable external sink.
- **UX-first**: operator runs `regatta serve --otlp-endpoint=...` once; every gate decision, every spawn, every LLM call shows up in their existing observability stack with cross-DAG trace correlation. No new dashboards to learn.
- **Reference bar**: matches LangSmith/Helicone/Langfuse trace richness; exceeds because regatta correlates LLM spans with approval-gate + scheduler-tick spans in one trace tree.
- **Dependencies**: #117 (--log-format flag, follow-up to #101); journal table from conditional-DAG wedge.
- **Slot**: MVP-3.
- **Effort**: medium — ~5 file-disjoint tasks (SDK bootstrap, slog bridge, gate spans, spawner spans, scheduler spans).

### W7 — Operator web UI (approvals + cost + DAG view)  _(MVP-3, rank #2)_
- **Elevator**: minimal embedded web UI on `regatta serve` — approve gates, view DAG run, see cost burn, no terminal required.
- **Trap fit**: P2 (HITL ergonomics) + P8 (operator visibility for budget).
- **Prior art adopted**: Temporal Web UI pattern (single binary serves HTML+API); embed via Go `embed.FS`; htmx + Tailwind for zero-bundler dev; reuse approval HMAC token as URL-bound auth. Rejected SPA framework (React/Vue) — operator-pilot users need plain-HTML degrade.
- **Why now**: CLI-only loses every non-eng approver; demo bar for enterprise pilot.
- **UX-first**: approver clicks Slack link → web page shows DAG context + diff + cost-so-far + Approve/Reject buttons → done. No regatta install required for reviewers.
- **Reference bar**: matches Argo Workflows UI navigability; exceeds GHA environment-approval by surfacing live cost burn and reviewer-set rationale.
- **Dependencies**: W6 (traces feed live DAG view); approval-gates state ops.
- **Slot**: MVP-3.
- **Effort**: large — ~8 file-disjoint tasks (server, auth, DAG render, approval flow, cost panel, audit log view, e2e tests, embedded asset pipeline).

### W8 — OPA-backed RBAC + multi-tenant scoping  _(MVP-3, rank #3)_
- **Elevator**: every API call evaluated against a Rego policy; tenants get isolated DAG-runs, plans, audit rows.
- **Trap fit**: P3 (trusted instructions only from authorized principals) + P9 (sensitive-context segregation).
- **Prior art adopted**: Open Policy Agent embedded as Go library (`opa.rego.New`); Postgres Row-Level-Security pattern for storage isolation; Cedar considered + rejected (AWS-flavored, smaller ecosystem). Built-in Rego defaults ship — OPA-server optional for orgs with central policy.
- **Why now**: blocker for any multi-team pilot; today's "every operator can decide every gate" violates least-privilege.
- **UX-first**: operator drops `regatta.yaml: rbac.policy: file://./policy.rego`; existing single-tenant deployments keep working with built-in `allow-all` default. Tenant scoping via `--tenant-id` flag + DB filter; no schema migration for single-tenant users.
- **Reference bar**: matches Kubernetes RBAC + OPA Gatekeeper composability; exceeds because the same policy controls approval-gate authorization _and_ plan-mutation authorization _and_ cost-budget assignment.
- **Dependencies**: W6 (policy decisions emit OTel events); approval-gates reviewer-set already takes a list.
- **Slot**: MVP-3.
- **Effort**: medium — ~6 file-disjoint tasks (policy engine, tenant-scoped queries, migration, default policy, CLI auth, e2e).

### W9 — Replay + diff harness  _(MVP-3, rank #4)_
- **Elevator**: `regatta replay <run_id> --from=<node>` re-executes a DAG run from journal, diffs new outputs vs original.
- **Trap fit**: P1 (deterministic gate replay) + P10 (signed-artifact verification).
- **Prior art adopted**: Temporal `tctl workflow show` + replay; Snakemake checkpoint re-evaluation. Built on the conditional-DAG journal we already have. No bespoke history format.
- **Why now**: regression detection for planner drift; required to debug "why did this DAG cost 2× last week's identical inputs?"; closes deferred MVP-3 #2.
- **UX-first**: operator hits a failure → `regatta replay 7f3a --from=plan_step --pin-model claude-4.7` → side-by-side diff in web UI (W7) or stdout.
- **Reference bar**: matches Temporal's replay determinism guarantee; exceeds because the diff is _semantic_ (acceptance-criteria delta) not just output-byte delta.
- **Dependencies**: conditional-DAG journal (MVP-2 W2), W6 traces, optional W7 for visual diff.
- **Slot**: MVP-3.
- **Effort**: medium — ~4 file-disjoint tasks (replay engine, pin loader, semantic differ, CLI).

### W10 — Sigstore / in-toto plan-and-artifact provenance  _(MVP-4, rank #5)_
- **Elevator**: every plan + every produced commit + every approval decision gets a signed in-toto attestation; verifiable supply-chain bill of materials.
- **Trap fit**: P10 (signed prompt-as-artifact) + extends P3 + P6.
- **Prior art adopted**: Sigstore cosign for keyless signing; in-toto SLSA-3 attestation schema; reuse existing HMAC for short-lived tokens, Sigstore for durable signatures. Rejected bespoke PKI (re-litigated in industry, lost).
- **Why now**: regulated buyers (banks, healthcare) need SLSA-3 provenance to onboard; differentiates from CC fundamentally.
- **UX-first**: operator runs `regatta verify <run_id>` → green check + chain of attestations: "plan signed by ops-team, model claude-4.7-hash-X produced these tokens, approval decided by alice@co" — one command, full chain.
- **Reference bar**: matches SLSA-3 build provenance bar (GitHub Artifact Attestations, sigstore/cosign); exceeds because the attested artifact is the _agent decision lineage_, not just the binary.
- **Dependencies**: W6 (events as attestation inputs); plan-as-code signing (already partial in W2).
- **Slot**: MVP-4.
- **Effort**: medium — ~5 file-disjoint tasks (cosign integration, in-toto serializer, verify command, key-discovery, e2e).

### W11 — Blackboard / shared agent state  _(MVP-4, rank #6)_
- **Elevator**: typed-facts table + reducers + CAS blobs for inter-subagent state without prompt bloat — per `wedge_blackboard`.
- **Trap fit**: P6 + P9.
- **Prior art adopted**: LangGraph reducer-per-channel + Bazel CAS + ETS ACLs — already enumerated in dossier.
- **Why now**: unlocks large-fleet refactor demo (the multi-repo flagship); without it, parallel subagents serialize on handoff.json.
- **UX-first**: subagent calls `fact.get("schema.user_table")` instead of re-deriving from prompt; orchestrator rejects out-of-scope writes by manifest. Operator never sees the blackboard — it's the agent-facing API.
- **Reference bar**: matches LangGraph + extends with content-addressed blobs (Bazel-grade) + HMAC provenance (regatta-specific moat).
- **Dependencies**: W6 (fact-write OTel events); W8 (RBAC for fact-key ACLs).
- **Slot**: MVP-4.
- **Effort**: large — ~7 file-disjoint tasks (schema, reducer engine, CAS store, ACL enforce, read API, GC, e2e).

### W12 — Metered billing + tenant usage export  _(MVP-4, rank #7)_
- **Elevator**: per-tenant USD + token rollups exported as Stripe metered-billing events or CSV; reconciles against Anthropic Usage API.
- **Trap fit**: extends P8 to commercial layer.
- **Prior art adopted**: Stripe metered billing API; Anthropic Usage API; OpenMeter (CNCF metered-billing OSS) as adapter pattern. Rejected bespoke invoicing.
- **Why now**: pilot → paid customer conversion needs invoiceable usage; multi-tenant deployments need chargeback.
- **UX-first**: operator config `regatta.yaml: billing.export: stripe://...` → monthly invoices generated automatically; tenants see real-time burn in W7 UI.
- **Reference bar**: matches OpenMeter; exceeds because rollups are _DAG-attributed_ not just timestamp-bucketed.
- **Dependencies**: W6, W8, cost governor.
- **Slot**: MVP-4.
- **Effort**: medium — ~4 file-disjoint tasks (rollup view, Stripe adapter, CSV export, reconciliation cron).

## 5. Cross-wedge architectural threads

- **Typed-output schema is load-bearing**: conditional-DAG predicates, blackboard reducers, replay-diff, and OTel GenAI attributes ALL require per-node declared output contracts. Lock this schema in W6 design, not later.
- **Observability backbone is the spine**: W6 underpins W7 (UI reads spans), W9 (replay reads journal-as-spans), W10 (attestations reference span IDs), W12 (usage rollups aggregate spans). Build it first.
- **Adapter contracts for swap-out**: every external dep (OTel exporter, OPA backend, Sigstore signer, Stripe billing, Helicone/Portkey gateway) ships as an interface with default in-binary impl + adapter for hosted service. Avoid lock-in. Matches `feedback_research_design_principles` "swap option" rule.
- **HMAC stays internal, Sigstore goes external**: HMAC (Wave 1) for short-lived intra-DAG tokens; Sigstore (W10) for durable cross-boundary attestation. Don't conflate.
- **Multi-tenant cuts every query**: W8 RBAC introduces `tenant_id` to every read path. Plan the migration in W6's design phase or every later wedge re-litigates the column.

## 6. Adversarial red-team

- **OTel-first may be premature**: if no pilot customer needs traces yet, W6 burns 2 weeks for hypothetical value. _Answer in §4_: the cost-governor reconciliation cron already needs canonical token counts; W6 is not hypothetical, it's the cost-spine.
- **Web UI is a tar-pit**: a "minimal" Temporal-style UI took Temporal 4 years to reach decent UX. We can't match in 2 weeks. _Answer_: scope MVP-3 W7 to **approval flow + DAG read-only view + cost panel** — explicitly NOT a workflow-authoring UI. Plan-authoring stays in YAML.
- **OPA may be overkill for single-tenant pilots**: most early adopters run one tenant. _Answer_: ship built-in `allow-all` default; OPA off-by-default; multi-tenant a flag-gated migration. Matches UX-first rule.
- **Claude Code might ship native cost+approval next quarter**: would obsolete W6+W7 differentiation. _Answer_: regatta's moat is the _operator surface_ — UI + audit + RBAC — which CC cannot ship without becoming a different product (per roadmap §Risk-to-track). Watch CC changelog quarterly; if cost-governor lands, accelerate W7+W10 (UI+provenance) which CC can't replicate without taking on multi-tenant operator-surface scope.
- **Temporal / Inngest could absorb the bottom**: if a buyer says "just use Temporal," our scheduler value disappears. _Answer_: W6+W9 design must include a "Temporal-backend adapter" path so we adopt rather than compete with the durable-execution layer. Regatta sits one abstraction _above_ Temporal — AI-labor-specific gates, cost, plans, approvals — not the workflow engine itself. OPEN QUESTION: should W9 (replay) actually be implemented _on_ Temporal from day-1 instead of bespoke? Recommend spawning a Temporal-vs-bespoke design subagent before W9 spec freeze.
- **In-toto/SLSA may not move the needle for non-regulated buyers**: W10 is high-cost / narrow-fit. _Answer_: defer to MVP-4 (not MVP-3) and time it to the first regulated-pilot ask. If no regulated buyer asks by GA, drop to flagship demo.
- **Blackboard schema lock-in risk**: get the reducer contract wrong → every subagent rewrites. _Answer_: ship `lww|set-union|append|write-once` per `wedge_blackboard.md` row 80; reducer is row-level not table-level. Adversarial reviewer must hunt schema-drift on this spec.
- **Multi-tenant column migration is destructive**: adding `tenant_id` to every table mid-life = breaking change. _Answer_: introduce `tenant_id NULLABLE DEFAULT 'default'` in W8; no-op for single-tenant. Backfill cron migrates legacy rows. Adversarial reviewer must verify single-tenant deployments are non-disrupted by W8.

## 7. Decision-priority self-check (top-3 only)

- **W6 OTel observability**: UX win (operators get traces in existing stack, zero new dashboards) ✓; reference quality (matches LangSmith/Helicone, exceeds via cross-system trace tree) ✓; best-practices (GenAI semconv = canonical) ✓; long-term (every later wedge consumes this spine) ✓. **All four priorities aligned — confirmed rank #1.**
- **W7 Web UI**: UX win (non-eng approvers unlocked) ✓; reference quality (Temporal-UI bar — _achievable only if scope is read-only_, otherwise we lose) ⚠; best-practices (htmx + embed.FS = boring + correct) ✓; long-term (operator surface = primary moat) ✓. **Confirmed rank #2 — scope discipline is the risk; flag for design subagent.**
- **W8 OPA RBAC**: UX neutral for single-tenant (default off) ✓; reference quality (OPA = the canonical adopt) ✓; best-practices (Rego + Postgres RLS = textbook) ✓; long-term (every later wedge requires tenant scoping anyway) ✓. **Confirmed rank #3. No velocity-vs-UX tradeoff — ship.**
- **Velocity wedge that lost**: bespoke "approvals-UI lite" via Slack-bot-only would ship faster than W7, but loses operator visibility for non-Slack orgs (UX fail) and loses to GHA reviewer-set in bake-off (reference-quality fail). Downranked, not pursued.

## 8. Recommended next-session bootstrap (kicks off W6 — OTel observability)

```
Continue regatta development autonomously. TARGET: MVP-3 Wave 1 — OTel + GenAI semconv observability backbone (next-level wedge #1).

BOOT
1. cd /Users/treedesk/Desktop/Projects/regatta && git fetch && git pull --ff-only main
2. make check && make cleanup-branches
3. gh pr list --state open  (sweep merged Wave 5 approval-gates leftovers)
4. Read MEMORY.md + AGENTS.md (auto-loaded). Read /tmp/regatta-next-level-design-brief.md §3-§5.

PRIORITY (top-down, skip if blocked)
1. W6 OTel observability — see brief §4. Spawn DESIGN subagent first:
   prompt: "You are the design subagent for regatta's OTel observability backbone (next-level wedge #1, per /tmp/regatta-next-level-design-brief.md §4). Write spec at docs/superpowers/specs/2026-06-01-mvp3-otel-observability.md. Adopt OpenTelemetry Go SDK + GenAI semantic conventions (gen_ai.* attributes); slog→OTel via otelslog bridge. Cite proven OSS over build per feedback_research_design_principles. Spec MUST include: (a) Prior art adopted section, (b) typed output-schema contract per work_item.kind (load-bearing for blackboard + replay + conditional-DAG — see brief §5 thread #1), (c) span hierarchy across scheduler-tick → spawner → LLM-call → gate-decide → reaper, (d) GenAI semconv attribute mapping for token usage events, (e) OTLP exporter config in regatta.yaml + default-off built-in stdout exporter for dev, (f) grade rubric B/A/A+ per feedback_grade_rubric. NO new tracing primitive — adopt OTel verbatim. Schema must be additive (no breaking change to existing slog consumers)."
2. Spawn ADVERSARIAL REVIEWER on spec: hunt edge cases (sampler-config trap, OTLP backpressure, sensitive-payload-in-span hazard, tenant_id propagation w/ W8 future-fit, slog→OTel double-export risk, replay-time span replay correctness). NEVER auto-approve. Per feedback_adversarial_review.
3. Spawn PLAN subagent → wave-able plan (target 5 file-disjoint tasks: SDK bootstrap, slog bridge, scheduler spans, spawner/gate spans, e2e).
4. Spawn PARALLEL IMPLEMENTER subagents per task. Verify each subagent's "make check clean" claim myself before push (per feedback_subagent_verification).
5. Spawn ADVERSARIAL REVIEWER per wave. Fix. Merge.

WORKFLOW per item — see autonomous-session-prompt.md unchanged.

RULES (memory-bound) — see autonomous-session-prompt.md unchanged. Plus:
- This wedge is the spine for W7/W8/W9/W10/W12 (per brief §5 thread #2). Schema decisions made here are load-bearing for every later wedge. Any deviation requires re-spawning design subagent per feedback_spec_pattern_authority.
- File [followup] issues for any deferred sub-decisions (tenant_id propagation, replay-span correctness) per feedback_unaddressed_load_bearing.

STOP CRITERIA
- W6 Wave 1 (SDK + slog bridge + scheduler spans) merged + Wave 2 (spawner + gate spans + e2e) dispatched
- OR 3 critical PRs shipped this session
- OR genuinely irreversible step required

Begin BOOT. After boot, spawn the W6 design subagent above.
```

---

**Open questions surfaced** (per §6 red-team):
- Should W9 (replay) adopt Temporal as the durable-execution substrate instead of bespoke? Spawn a Temporal-vs-bespoke design subagent before W9 spec freeze.
- Web UI scope: confirm "approval + read-only DAG + cost panel" boundary with operator-pilot before W7 design starts.

_File: `/tmp/regatta-next-level-design-brief.md` — 197 lines._
