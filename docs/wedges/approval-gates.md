# Wedge: human-in-the-loop approval gates

Prospective. Not on the milestone path. See
[`README.md`](./README.md).

## Thesis

An `approval_gate` DAG node pauses execution, notifies a human
through a pluggable channel (Slack, email, webhook, CLI), and
resumes on approval, rejection, or "approve with edits." Every
transition appends to an immutable log; current state is the fold
over that log.

Maps to **Trap Catalog P2** (two-key approval on irreversible
actions, load-bearing) and **P3** (trusted instructions from
`main` only, load-bearing). Procurement gate for regulated buyers.

### Defensibility under Dynamic Workflows

Claude Code Dynamic Workflows can wait inside a session for a
human, but waits that survive a process restart, an operator
handoff, or a 30-day timer are out of scope for them by
construction. Regatta lives at that boundary on purpose. The
direct competitors are [HumanLayer](https://humanlayer.dev) (SDK
embedded into the agent) and [Cloudflare
Agents HITL](https://developers.cloudflare.com/agents/concepts/human-in-the-loop/)
(durable-object-backed); regatta differs by sitting *outside*
the agent, pausing the DAG, and feeding the journal that the
[conditional-DAG wedge](./conditional-dag.md) and the
[cost-governor wedge](./cost-governor.md) read.

## Prior art

| System | Pause primitive | Resume mechanism | Timeout | Audit |
|---|---|---|---|---|
| [Temporal](https://docs.temporal.io/develop/python/message-passing) | `@workflow.signal` handler + `workflow.wait_condition` | External client `signal(name, payload)` mutates state | `workflow.timeout` race against signal future | Event history is the audit log |
| [Airflow](https://airflow.apache.org/docs/apache-airflow/stable/core-concepts/dags.html) | `ExternalTaskSensor` / paused DAG state | UI click, REST `dagRuns/{id}/tasks/{id}/clear` | Sensor `timeout` + `poke_interval` | Task instance log + DAG run metadata |
| [Prefect 3](https://docs.prefect.io/v3/develop/pause-resume) | `pause_flow_run()` / `suspend_flow_run()` with `wait_for_input` | `resume_flow_run(run_input=…)` with Pydantic-validated payload | `timeout` param | Event log per run |
| [Argo Workflows](https://argo-workflows.readthedocs.io/en/latest/walk-through/suspending/) | `suspend: {}` template | `argo resume WORKFLOW` or `duration:` auto-resume | Built-in `duration` field | CRD status + controller events |
| [AWS Step Functions](https://docs.aws.amazon.com/step-functions/latest/dg/connect-to-resource.html) | Task with `.waitForTaskToken` resource ARN | External worker calls `SendTaskSuccess(token, output)` | `TimeoutSeconds` + `HeartbeatSeconds` with `SendTaskHeartbeat` reset | Execution history events `TaskSubmitted` / `TaskSucceeded` |
| [LangGraph](https://docs.langchain.com/oss/python/langgraph/interrupts) | `interrupt(payload)` inside a node; checkpointer persists state | Re-invoke with `Command(resume=value)` | Application-owned | Checkpoint history per `thread_id` |
| [GitHub Actions environments](https://docs.github.com/en/actions/managing-workflow-runs/reviewing-deployments) | Job referencing protected `environment:` | One of up to 6 required reviewers clicks Approve | Two timers: wait timer up to 43,200 min (30 days) *before* the run starts, and a separate approval-request expiry of 30 days. `prevent_self_review` flag. | Immutable deployment review event |
| [PagerDuty](https://support.pagerduty.com/main/docs/audit-trail-reporting) | Incident triggered, escalation timer running | `acknowledge` halts escalation; `resolve` closes | Ack timeout re-escalates up the policy | Per-object audit-trail API |
| [HumanLayer](https://humanlayer.dev) | SDK-level `require_approval()` wrapping a tool call | Sync (block thread) or async (resume via callback) | Per-call; SDK-owned | SDK records to its own backend; embedded into the agent |
| [Cloudflare Agents HITL](https://developers.cloudflare.com/agents/concepts/human-in-the-loop/) | Durable-object suspend on a tool call | Resume from Workers RPC | Durable-object lifetime | Workers analytics + custom |
| [Decagon AOPs](https://decagon.ai/blog/enterprise-conversational-ai-features) and [Sierra Ghostwriter](https://trust.sierra.ai/) | Sandbox-validate-then-queue-for-review | Operator review console | Configurable per AOP | Vendor-managed audit |
| [GitHub Copilot cloud agent CI gate](https://github.blog/changelog/2026-04-01-research-plan-and-code-with-copilot-cloud-agent/) | PR requires human approval before CI runs | PR review | Standard PR review window | GitHub PR timeline |
| [Trigger.dev `waitpoint`](https://trigger.dev/product/ai-agents) | Typed wait primitive in a TS workflow | Token-correlated callback | Per-waitpoint TTL | Run timeline |

## Patterns worth stealing

1. **Token-correlated callback** (Step Functions). A signed,
   opaque, single-use token in the notification payload is the
   only key that resumes the node. Decouples the resume channel
   (Slack button, email link, CLI) from the orchestrator.
2. **Heartbeat plus ultimate timeout** (Step Functions). Two
   clocks: `heartbeat_seconds` detects notifier-channel death;
   `timeout_seconds` is the absolute deadline.
3. **Typed input schema** (Prefect `wait_for_input`). Approval
   payloads validated against a JSON Schema declared on the
   node. Kills "approve with changes" ambiguity at the type
   level.
4. **N-of-M reviewer set with `prevent_self_review`** (GitHub
   Actions). Up to 6 reviewers, any single approval unblocks,
   the requester cannot approve their own run.
5. **Durable `wait_condition` over signal predicate** (Temporal).
   Do not expose `resume` as RPC. Expose typed signals
   (`Approve`, `Reject`, `RequestChanges`) and let the node
   `wait_condition` on a predicate. Idempotent handlers absorb
   duplicate clicks.
6. **Suspend with auto-resume duration** (Argo). Optional
   `duration` for low-stakes gates (ship to staging unless
   someone vetoes in 2h).
7. **Audit as immutable event log** (Temporal / PagerDuty).
   Approvals are events, never row updates.
8. **Escalation policy** (PagerDuty). If the primary approver
   doesn't acknowledge within N minutes, escalate to the next
   tier. Solves the offline-approver failure mode.
9. **Identity-broker the reviewer, not just the operator**
   ([Strata](https://www.strata.io/blog/agentic-identity/practicing-the-human-in-the-loop/)).
   Reviewer-set snapshot is half the story; OIDC / SAML
   federation of *reviewers* and on-call rotation feeds
   (PagerDuty schedule -> effective reviewer) is the other half.

## Failure modes

- **Approver offline >24h.** GHA hard-fails at 30 days; Argo
  auto-resumes if `duration` is set; PagerDuty escalates. Support
  all three under `on_timeout: fail | auto_approve | escalate
  (next_reviewer)`.
- **"Approve with changes."** LangGraph-style edit interrupt:
  reviewer returns a *patched payload*, not a boolean. Model as
  `decision ∈ {approve, reject, approve_with_edits}` where
  `edits` is a JSON patch validated against the node's output
  schema. Without a schema, only `approve` and `reject` are
  accepted.
- **Duplicate clicks / replay.** Temporal docs explicitly warn
  signals can dupe; handler is idempotent via the single-use
  token.
- **Approver authority drift.** Reviewer list is snapshotted at
  gate creation, not resolved at click time. Prevents privilege
  escalation between notify and resume.
- **Notifier delivery failure.** Slack 500, email bounce. The
  heartbeat clock surfaces it; without it the gate looks
  "waiting" while no human ever saw it.

## Proposed data model

Extend `work_items.state` with two new values:

```
pending → running → awaiting_approval → running → succeeded
                                     ↘ rejected   (terminal)
                                     ↘ timed_out  (terminal)
```

New `approvals` table:

| Field | Type | Notes |
|---|---|---|
| `id` | uuid pk | |
| `work_item_id` | fk `work_items` | the `approval_gate` node |
| `run_id` | fk `runs` | DAG-run correlation |
| `token` | text unique | opaque single-use |
| `reviewer_set` | jsonb | snapshot of allowed principals |
| `quorum` | int | N-of-M, default 1 |
| `payload_schema` | jsonb | optional JSON Schema for decision payload |
| `notify_channels` | jsonb | array of `{kind, target}` |
| `timeout_at` | timestamptz | absolute deadline |
| `heartbeat_at` | timestamptz nullable | last notifier liveness |
| `on_timeout` | enum | `fail` / `auto_approve` / `escalate` |
| `escalation_chain` | jsonb nullable | ordered reviewer tiers |

New `approval_events` (append-only audit log):

| Field | Type |
|---|---|
| `id` | uuid pk |
| `approval_id` | fk |
| `actor` | text (principal id or `system`) |
| `kind` | enum: `requested` / `notified` / `acknowledged` / `approved` / `rejected` / `approve_with_edits` / `escalated` / `timed_out` / `token_revoked` |
| `payload` | jsonb |
| `channel` | text (`slack` / `email` / `webhook` / `cli`) |
| `client_ip` | inet nullable |
| `created_at` | timestamptz |

Current state is `fold(events)`. Never a row mutation.

## Notifier abstraction

```go
type Notifier interface {
    Kind() string
    Notify(ctx context.Context, req ApprovalRequest) (DeliveryReceipt, error)
}

type InteractiveNotifier interface {
    Notifier
    CallbackRoute() (path string, handler http.Handler)
}

type ApprovalRequest struct {
    Token         string
    GateTitle     string
    ContextURL    string
    PayloadSchema *jsonschema.Schema
    Reviewers     []Principal
    DeadlineAt    time.Time
}
```

Notifier registration is config-driven; the orchestrator never
imports vendor SDKs. The callback validates the token, emits an
`approval_events` row, and the DAG runner picks it up via
`LISTEN`/`NOTIFY` (or polling) before transitioning the work
item. Same shape as Step Functions' `SendTaskSuccess`,
transport-agnostic.

## Security and RBAC

- **Reviewer set** is snapshotted at gate creation, resolved
  against an identity provider (OIDC `sub`, GitHub team, Slack
  `user_id`). Principal must appear in the set, or in an
  escalation tier if timeout fired.
- **Self-review prevention.** Gate carries `submitter_id`; if
  `prevent_self_review`, that ID is filtered from the effective
  reviewer set.
- **Token scoping.** Single-use, HMAC-signed, expires at
  `timeout_at`. Never logged in plain.
- **Decision authority on edits.** `approve_with_edits` requires
  `payload_schema` at gate-definition time. Without a schema,
  only `approve` / `reject` are accepted -- ambiguity dies at the
  type level, not in reviewer judgment.
- **Audit immutability.** `approval_events` is append-only;
  deletes are blocked at the database-role level.

## Regatta extension points

- New gate type `type: approval_gate` in `regatta.yaml:gates[]`.
- New gate handler in `internal/gates/` parallel to L3 / L4 / L5.
- Re-uses the existing HMAC signature mechanism for the token.
- Blocks PR advance unless the approval result is present and
  verified.

## Trigger metric (when to adopt)

- First procurement ask citing change-management or SOC2.
- OR first incident where an unapproved auto-merge required
  reversal.
- OR ≥1 customer running a workflow targeting `lane = prod`.

## Grade rubric

| Tier | Criterion |
|---|---|
| **B** | Approval gate type implemented, single-use token, append-only `approval_events`, CLI notifier only. |
| **A** | All B + Slack and webhook notifiers + heartbeat clock + `on_timeout: fail / auto_approve` + `prevent_self_review`. |
| **A+** | All A + escalation chains + JSON-Schema-validated `approve_with_edits` + reviewer-set snapshot + zero deletable audit rows (mutation-tested at the role layer). |

## References to existing repo state

- `docs/incidents.md` P2 and P3.
- `docs/design.md` §Programs MVP-2 (`RouteVerdicts`) — approval
  decisions feed the same deterministic routing function.
- `contracts/schemas/gate_result.schema.json` — approval verdicts
  return through the existing `GateResult` shape with a new
  `gate_kind`.
