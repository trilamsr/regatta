# Wedge research dossiers

Research dossiers for **prospective features** that widen regatta's
moat versus single-session agent tools -- chiefly Claude Code.
Each dossier surveys prior art, proposes a minimal data model that
hooks into `contracts/schemas/`, and maps the feature to a
load-bearing pattern in the [Trap Catalog](../incidents.md).

Not commitments. Proposals that `docs/design.md` may pull in when
a Phase 2 or Phase 3 trigger fires (see §Programs). Adopt-when-needed
still rules -- nothing here ships until its trigger condition is
observed.

## Strategic frame

Regatta is the **control plane for AI labor** -- scheduling,
budgeting, audit, recovery for fleets of LLM workers. Single-session
tools (Claude Code, Codex, Cursor) own the *worker* layer; regatta
owns the *fleet* layer. Each wedge here deepens a primitive a
single-session tool cannot absorb without becoming a different
product:

- Cross-session, cross-operator durability
- DAG-shaped work with dependency staging across runs
- Pre-call budget enforcement with replay-grade attribution
- Human-in-the-loop checkpoints with append-only audit
- Reusable, PR-reviewable workflow definitions with typed inputs
- Concurrent-agent shared state with provenance

Worker-layer competition (smarter prompts, better planners, IDE
integration, code-review UI, chat-agent products) is out of scope --
the vendor or model lab will eat it.

### The Dynamic Workflows reality (2026-05-28)

Anthropic shipped [Claude Code Dynamic
Workflows](https://code.claude.com/docs/en/workflows) on
2026-05-28: a JS orchestration script Claude writes that runs up
to 1,000 subagents (16 concurrent) with intermediate state held
outside the model context. The [watch item](#watch-item) at the
bottom of this README is now in motion, not hypothetical. Restate
the line: Claude Code Dynamic Workflows ship the *in-session*
fleet primitive. Regatta owns *cross-session, cross-operator,
cross-tenant* durability with deterministic replay and append-only
audit. **The boundary is the session, not the agent count.**

Each dossier below has been re-examined under this lens; the
"defensibility" notes call out where Dynamic Workflows compresses
the wedge.

## Index

Read the meta-dossier first -- it changes how the others' data
models land.

| Dossier | One-liner | Trap fit | Wave |
|---|---|---|---|
| [unified-substrate.md](./unified-substrate.md) | **Read first.** Collapses the five wedges' tables into three primitives (`events`, `policies`, `blobs`) + one `Decider` interface + one plan envelope, without losing features | n/a (meta) | adopt before the second wedge lands |
| [cost-governor.md](./cost-governor.md) | Per-DAG / per-operator USD + token caps with pre-call deny and post-hoc Anthropic Usage API reconciliation | **P8** (load-bearing) | MVP-2 W1 |
| [approval-gates.md](./approval-gates.md) | DAG node type that pauses, notifies, resumes with single-use token and append-only audit log | **P2 + P3** (both load-bearing) | MVP-2 W1 |
| [plan-as-code.md](./plan-as-code.md) | `.regatta/plans/*.yaml` declarative DAGs, CUE-validated, round-trips with runtime planner output | P3, P10 | MVP-2 W2 |
| [conditional-dag.md](./conditional-dag.md) | CEL-predicated edges with journaled output snapshots; enables triage / remediation trees while staying deterministic | **P1** (load-bearing) | MVP-2 W2 |
| [blackboard.md](./blackboard.md) | Typed facts table with reducers and content-addressed blobs for inter-subagent communication | P6, P9 | MVP-3 |

## Wedge ranking matrix

| Wedge | Operator pain | Operator UX | Single-session can't do | Build cost | Load-bearing trap |
|---|---|---|---|---|---|
| Cost governor | high | high (visible spend caps) | high (cross-session attribution) | low | yes (P8) |
| Approval gates | high | high (procurement gate) | high (durable wait beyond session) | low | yes (P2, P3) |
| Plan-as-code | medium | medium (PR review) | low after 2026-05-28 | low | no |
| Conditional DAG | high | medium | high (deterministic replay) | medium | yes (P1) |
| Blackboard | medium | low (operator-invisible) | high (cross-session facts) | medium-high | no |
| Replay + diff | medium-high | high | high (journaled mutations) | high | no |
| Multi-repo fleet | niche-high | medium | high | high | no |
| Determinism harness | medium (regulated only) | low | high | medium | no (P10 adjacent) |
| Generalised resource model | medium | low | high | low | yes (P5) |

UX column added per the project's decision-priority guidance
(UX first, then ease of use, then best practices). Wedges that
land in operator-visible surfaces ship first; operator-invisible
primitives wait for a paying-customer pull.

## Recently surfaced -- not yet dossiered

Candidates worth a dossier next pass. Each one became sharper
after the Dynamic Workflows release:

- **Cross-operator / cross-DAG tenant attribution and billing.**
  Tenant-axis on every spend event so agencies can bill fleets to
  end customers (Mavvrik + Vantage pattern).
- **Approver identity broker.** OIDC / SAML federation of
  reviewers with quorum-by-group and on-call rotation feeds
  (PagerDuty schedule → effective reviewer). Strict superset of
  the approval-gates dossier.
- **Air-gapped / sovereign deployment topology.** SQLite-only
  mode, no telemetry egress, invoice-CSV reconciliation in place
  of the Anthropic Usage API cron. Procurement-unblocking for
  FedRAMP / EU sovereign customers.
- **Verifier / adversarial-review node.** First-class DAG node
  type whose output feeds the conditional-DAG predicate. Composes
  with replay and budgets; differentiator vs Dynamic Workflows'
  in-session "agents review each other."
- **Replay-with-mutation (diff-by-rerun).** Replays with altered
  inputs against journaled snapshots, generating a behavior diff.
  The only wedge that beats Dynamic Workflows' "rerun" claim head
  on.
- **MCP server as the integration surface.** Expose DAG-launch,
  approval, and budget APIs as an MCP server. Inverts the
  absorption risk: Claude Code (or any client) becomes a regatta
  *client*, not just a worker.

## Defensibility

Why these wedges don't evaporate the next time a worker tool
ships:

- **Network effects via Trap Catalog.** Each operator's
  P-incident sighting is feedback into the shared catalog at
  `docs/incidents.md`. Cross-operator aggregation is the
  flywheel; absorbing it requires owning *somebody else's*
  incident corpus, which the vendors don't.
- **Data flywheel via replay corpus.** A journaled DAG run trains
  which budget caps work, which conditional predicates fire
  false, which approvers respond in time. Operator-private;
  cross-operator aggregate is the moat.
- **Cross-vendor stance.** Anthropic optimises for single-vendor
  fleets. Regatta's MCP + multi-vendor routing (OpenAI, Bedrock,
  Vertex) is structurally incompatible with their product
  economics. They will not build it.
- **Session boundary.** Anything that needs to survive a process
  restart, an operator handoff, or a tenant boundary cannot live
  inside a single agent session by construction. Regatta lives
  there on purpose.

## Buyer

The primary buyer is the platform-engineering + FinOps + compliance
triad inside a company already running agent fleets. Mirrors the
2026 State of FinOps survey priorities. Pricing and positioning
follow.

## Adoption blockers we address explicitly

- **SOC 2 narrative.** Append-only event log → CC7.2 evidence;
  HMAC-signed audit rows → CC6.1; reviewer-set snapshot → CC6.3
  access management.
- **EU AI Act §49 traceability.** The DAG journal *is* the
  trajectory log the Act expects from 2026-08-02 onward
  (high-risk systems, 7% global revenue penalty). The
  conditional-DAG and blackboard dossiers carry this mapping.
- **CI integration vs replacement.** Regatta is meta-CI that
  GitHub Actions or Buildkite can call, not a replacement.
- **Air-gapped / data residency.** Per the deployment-topology
  candidate above; pin DB region; refuse cross-region writes by
  config; document every outbound call (today: the Anthropic
  Usage API cron).
- **Procurement collateral.** A `docs/procurement/` placeholder
  with SOC 2 control mapping, DPA template, and a data-residency
  statement is a prerequisite for the approval-gates dossier's
  trigger metric ("first procurement ask") to be meetable.

## Validation checklist (before promoting any wedge into a milestone)

A dossier graduates into `docs/design.md` Phase 2 / Phase 3 only
when **every** box is checked:

- [ ] Maps to at least one Trap Catalog pattern (P1–P13)
- [ ] Does not require upstream model-vendor roadmap changes
- [ ] Hooks into existing extension points without breaking
      `contracts/schemas/`
- [ ] Has independent PR-reviewable test fixtures
- [ ] Grade rubric: B / A / A+ criteria distinct and
      tool-checkable (per the project's grade-rubric guidance).
      When a rubric box says "returns typed sentinel X", state
      explicitly whether the sentinel is returned **bare**
      (`return ErrX`) or **wrapped** (`fmt.Errorf("ctx: %w", ErrX)`)
      and give the chosen reason — bare is simpler; wrap adds
      operator-readable log context. Both forms satisfy `errors.Is`,
      so the choice is intent-driven and reviewers verify the
      implementation matches the stated form.
- [ ] Trigger metric defined (when does adoption begin?)
- [ ] An adversarial review subagent has hunted edge cases

## Anti-wedges (do not pursue)

Listed so we do not re-litigate them:

- IDE integration -- Claude Code, Cursor, JetBrains will own this.
- Smarter prompts or planners -- model-vendor territory.
- Code review UI -- GitHub owns it.
- RAG / memory as a *core* feature -- too many incumbents; only
  acceptable as one read mode of the blackboard.
- Agentic UI builder / chat-agent product -- Sierra, Decagon,
  Lovable own this.

## Watch item

A single-session agent tool shipping durable cross-session work
queues with DAG dependencies and native budget governance absorbs
the bottom of our stack. The 2026-05-28 Claude Code Dynamic
Workflows release pushed on the in-session axis; the cross-session
axis is still open. Skim the Claude Code changelog quarterly and
flag overlap on this index.
