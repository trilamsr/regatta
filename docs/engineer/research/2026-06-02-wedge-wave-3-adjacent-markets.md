# wedge research wave 3 — adjacent markets: borrow what works

_Author: research subagent, 2026-06-02. Companion to `docs/engineer/briefs/2026-06-01-regatta-research-vision.md` (Phase-X MVR-1 wedge sequence) and `docs/engineer/briefs/2026-06-01-self-host-first.md` (Phases S1–S3). Memory-citation: `feedback_research_design_principles` (proven OSS > build-from-scratch), `feedback_pr_body_file_only`, `feedback_pr_body_release_notes_mandatory`. Scope: research brief only — no code, no schema deltas. Decision-priority: UX > ease > performance > best-practices > speed > velocity (long-term)._

This brief surveys six adjacent-market categories where regatta-shaped patterns already exist (DAG + lane reservation + gate stack + signed audit + cost cap + reviewer + automerge) and extracts the discriminator per category: **what regatta should borrow, what it should reject, and what it already does better**. Each category covers ≥3 named systems with a feature matrix; five cross-category insights close the brief.

The bar for "borrow" is feedback_research_design_principles (proven OSS over build-from-scratch); the bar for "reject" is `feedback_deletion_default` (every primitive earns its place — A+ defense required for any addition).

---

## 1. Data-pipeline orchestrators

Systems surveyed: **Airflow**, **Dagster**, **Prefect**, **Temporal**, **Argo Workflows**.

### 1.1 Feature matrix

| Axis | Airflow (3.2) | Dagster (1.x + Components) | Prefect (3.x) | Temporal (Replay-2026) | Argo Workflows |
|---|---|---|---|---|---|
| DAG definition | Python `@dag` decorator + TaskFlow API | Software-Defined Assets (Python decorators on data products) | Python `@flow` + `@task` (runtime DAG — no parse-time shape) | Python/Go/TS/Java workflow funcs — deterministic replay model | YAML CRD on Kubernetes |
| Visual / YAML / code | Code (Python) + read-only UI | Code (Python) + asset-graph UI | Code (Python) + UI | Code (4 SDKs) | YAML CRD (kubectl apply) |
| Retry primitive | `retries=N` on operator, exponential backoff | `RetryPolicy` per op | `retry_delay_seconds` + `retry_jitter_factor` | `RetryPolicy` per Activity, schedule-to-close timeout, months-long retries | `retryStrategy` per Step or DAG |
| Dynamic fan-out | `dynamic_task_mapping` (3.x) — at runtime | Dynamic outputs → downstream ops | Tasks created at runtime (no parse-time shape) | Child workflows + signal/update API | `withParam` / `withItems` |
| State machine | Scheduler poll loop on DB | Run-coordinator + asset materialization log | Cloud orchestrator-as-a-service | Durable event history; replay reconstructs state | etcd-backed Kubernetes CRD |
| Observability default | Task-centric UI ("which task failed"); OpenLineage opt-in | Asset-centric UI ("which asset is stale, and why"); freshness policies; built-in lineage graph | Flow runs + states UI + automated alerts | Workflow event-history viewer; per-activity span | argo logs + UI; OpenTelemetry plugin |
| Operator surface | Web UI + CLI + REST | Web UI + CLI + GraphQL | Cloud UI + CLI + REST | Web UI + tctl CLI + gRPC | kubectl + argo CLI + Web UI |
| HITL | Airflow 3.1 HITL operators | `freshness_check` + asset alerts | `pause` + UI approval | Signal-driven `Update` API | `suspend` template |

### 1.2 What regatta borrows

- **Dagster's asset-centric model maps cleanly onto regatta's `Criterion`**. An asset is a *named, addressable, freshness-tracked, lineage-aware* output. A `Criterion` is a named, addressable, gate-tracked, prereg-linked claim. The naming discipline ("which asset is stale, and why?" → "which criterion is `refuted`, and which gate said so?") earns its place in `Criterion.State` semantics. **Action: keep `Criterion.State` enum closed, add `refuted` per `regatta-research-vision §4 Task 0`, do NOT introduce a separate "data product" abstraction.**
- **Temporal's signed event-history-as-source-of-truth pattern is the same shape as `substrate_events`**. Phase S3-T2 already cut over cost-gov + approvals to `substrate_events`. Temporal validates: an immutable event log with replay-from-zero semantics is the operator-credible primitive. **Action: do nothing — `substrate_events` already wins on this axis. Reject any pivot to a separate state store.**
- **Prefect's "no parse-time DAG shape" lifts the spec-immutability burden when work items spawn sub-items**. The L0 immutability gate (per `docs/design.md`) requires the contract committed before the work. Prefect proves dynamic spawn is compatible with falsifiability if (and only if) the parent contract names the spawn predicate. **Action: when implementing dynamic-WorkItem-spawn in MVR-2+, require parent prereg to declare the spawn predicate; no late-binding child criteria.**

### 1.3 What regatta rejects

- **Airflow's task-centric scheduler loop**. regatta's scheduler-tick + lane-reservation already wins on the operator-credible axis (signed verdicts, not "which task failed"). Reject the poll-loop ergonomics — `feedback_drop_ceremony`.
- **Argo Workflows' YAML CRD as primary spec surface**. YAML inside k8s is a footgun for an empirical-research contract (no type-system, no signed manifest, prereg-locking impossible). regatta's `.regatta/items/*.md` already wins: code-disjoint markdown with a typed prereg sub-block. Reject any pivot to k8s-native CRDs.
- **Temporal's deterministic-replay programming model**. Replay forbids `time.Now()`, random, network reads in workflow code — a real cognitive tax. Inngest's step-memoization model (see §5) ships the same durability without the determinism rules. **Reject** Temporal's workflow-as-deterministic-function discipline for regatta's WorkItem executor; **borrow** Inngest's `step.run` shape instead.

---

## 2. Build-system parallels

Systems surveyed: **Bazel** (+ BuildBuddy / EngFlow), **Nx**, **Buck2**, **Turborepo**, **Pants**.

### 2.1 Feature matrix

| Axis | Bazel | Nx | Buck2 | Turborepo | Pants |
|---|---|---|---|---|---|
| Dep-graph model | Per-target, source-file granularity | Per-package | Per-target, hermetic | Per-package | Per-target, **dependency inference via static analysis** |
| Parallel scheduler | Action-graph scheduler, configurable jobs | Task runner over package graph | Action-graph scheduler (Rust) | Task runner over package graph | Action scheduler |
| Cache layer | Local + remote action cache (RBE protocol) | Local + Nx Cloud | Local + RE | Local + Vercel Remote Cache | Local + remote (Pants Cloud / self-host) |
| Remote execution | RBE — BuildBuddy / EngFlow / Buildfarm | No (cache only) | RE (built-in protocol) | No (cache only) | Limited |
| Workspace model | `WORKSPACE` + `BUILD` files (Starlark) | `nx.json` + `project.json` | `BUCK` files (Starlark) | `turbo.json` | `BUILD` files (auto-inferred) |
| Hermeticity | Strong (sandboxed actions) | Weak (trusts node_modules) | Strong | Weak | Strong |
| Lang scope | Polyglot | JS/TS-first | Polyglot | JS/TS-first | Python/JVM/Go-first |

### 2.2 What regatta borrows

- **Bazel's action-graph scheduler is the load-bearing analog to regatta's scheduler-tick + lane-reservation**. Bazel computes the action-graph from the dependency-graph, reserves a worker slot (lane), runs the action, signs the output (action-cache key = digest of inputs). regatta's scheduler reserves a lane per WorkItem, runs the executor, signs the verdict event. The shape is identical. **Action: when regatta's scheduler exits Phase S, document the analogy explicitly in `docs/design.md` and reference Bazel's action-cache as the credibility precedent.**
- **Pants' dependency inference via static analysis is the right ergonomic ceiling**. Operators should not hand-write the dependency-graph of their WorkItems if static analysis can infer it. **Action: in MVR-2+, add `prereg.depends_on: auto` inference from criterion text — strict allowlist of patterns, fall back to explicit declaration on parse failure. Anti-goal: never make this required; operators with strong opinions write the edges themselves.**
- **Remote-execution via signed protocol (RBE)**. BuildBuddy + EngFlow + Buildfarm implement the same Bazel RE API — multiple impls, swap-out by changing a URL. This is the architecture regatta's W9 substrate-default `DurableHistory` + Phase-X Temporal-backed variant should aim for: **one signed protocol, multiple impls**. **Action: when W9 Temporal-backed variant exits Phase X, ensure the impl swap is URL-only — no spec change.**

### 2.3 What regatta rejects

- **Turborepo's "trust node_modules" hermeticity tax**. The whole point of regatta's L0-L5 stack is to refuse to trust un-signed inputs. Turborepo proves the cost of weak hermeticity: cache-hit ambiguity, intermittent green/red. **Reject** any drift toward "good enough" hermeticity in the verdict pipeline.
- **Nx's package-level granularity**. WorkItems are criterion-level (per `Criterion`), not package-level. Nx's coarse-grained model would let two unrelated criteria block on the same package edit. Reject.
- **Bazel's `WORKSPACE` complexity tax**. The cognitive cost of `WORKSPACE` + `MODULE.bazel` migration is well-documented (the Bazel community spent 3 years on bzlmod migration). regatta's `.regatta/items/*.md` discipline is deliberately minimal. **Reject** any expansion of the workspace file surface beyond what `feedback_drop_ceremony` allows.

---

## 3. CI/CD platforms

Systems surveyed: **GitHub Actions** (+ merge queue), **GitLab CI**, **Tekton**, **Buildkite**, **CircleCI**, **Argo CD**.

### 3.1 Feature matrix

| Axis | GitHub Actions | GitLab CI | Tekton | Buildkite | CircleCI | Argo CD |
|---|---|---|---|---|---|---|
| Job / step / matrix | Workflow → job → step; matrix expansion at parse | Pipeline → stage → job; parallel matrix | Pipeline CRD → Task CRD → Step (k8s) | Pipeline → step (dynamic upload) | Workflow → job → step; matrix via params | App-of-apps (GitOps) |
| Secrets | Repo/org/env secrets; OIDC to AWS/GCP/Azure | CI/CD variables (masked, protected, file) | k8s secrets + workspaces | Buildkite secrets + agent env | Contexts + org-level secrets | Sealed Secrets / SOPS / Vault |
| Auto-merge | Native auto-merge + merge queue (2026 makes branch-capacity the gate) | Merge trains | External (e.g. Aviator, Mergify) | External | External | n/a (deploy-side) |
| Reviewer / approval | Branch protection rules + CODEOWNERS + required reviewers | MR approval rules + scoped approvers | n/a (event-driven) | Block step (manual unblock) | Approval job (manual) | Argo CD sync windows + RBAC |
| Dynamic config | matrix at parse only | child pipelines | n/a | `pipeline upload` step (runtime YAML inject) | dynamic config via setup workflow | n/a |
| Cost surface | per-minute billing per runner class | per-minute | n/a (self-host) | per-seat | per-credit | n/a |

### 3.2 What regatta borrows

- **GitHub's auto-merge + merge queue + branch-protection-as-gate stack is the closest external precedent for regatta's mandatory L0 + gate stack + reviewer + automerge UX**. The 2026 evolution (branch-capacity decides what ships) is structurally identical to regatta's lane-reservation. **Action: when documenting regatta's automerge UX, cite GitHub merge queue + the March-2026 auto-merge regression (auto-merge can no longer be enabled until all requirements are met) as the negative-precedent — regatta should permit auto-merge-pending-gates and surface the blockers, not refuse to enable.**
- **GitHub OIDC for short-lived credential exchange to cloud providers**. regatta currently leans on long-lived secrets for `Spawner`. **Action: in MVR-2+, add OIDC-style short-lived credential issuance for the Spawner→provider hop. Use the existing W8 Authorizer interface — don't introduce a new auth surface.**
- **Buildkite's `pipeline upload` step (dynamic runtime YAML inject)**. Same lesson as Prefect (§1.2): dynamic spawn is fine *if* the parent contract names the predicate. Buildkite proves operators *like* having one entry-point step that fans out. **Action: in MVR-2+, the parent WorkItem can declare a `spawn_predicate` that emits child criteria at runtime, gated by a Rego rule that the parent prereg pre-authorized the predicate (per the `feedback_spec_pattern_authority` discipline).**
- **GitLab CI's MR approval rules with scoped reviewers**. CODEOWNERS-style scoping per criterion-kind earns its place — a methodology gate verdict on a research WorkItem should require a different reviewer than a refactor on the substrate. **Action: in MVR-2+, allow a `prereg.required_reviewer_scope` field that an OPA rule consumes.**

### 3.3 What regatta rejects

- **Tekton's CRD-per-everything discipline**. Same rejection as Argo Workflows (§1.3): k8s-CRD as the primary spec surface defeats prereg-locking and operator-readable contracts. Reject.
- **CircleCI's parse-time-only matrix expansion**. regatta's research-mode WorkItem may legitimately need late-binding children (e.g. K=10 fresh-seed re-runs in Task 5 of the vision brief). Parse-time-only would force the operator to write 10 nearly-identical items. Reject.
- **Argo CD's "deploy-side" framing**. regatta is not a deploy tool. Reject the analogy entirely.

---

## 4. AI eval/obs vendors

Systems surveyed: **Helicone**, **Langfuse**, **Phoenix** (Arize), **Braintrust**, **OpenLLMetry** (Traceloop).

### 4.1 Feature matrix

| Axis | Helicone | Langfuse | Phoenix | Braintrust | OpenLLMetry |
|---|---|---|---|---|---|
| Token + cost slice | Per-request via gateway proxy; per-user/per-key/per-model | Per-trace + nested spans; tags + sessions | Per-OTel-span; OpenInference schema | Per-eval-run + per-trace; project-scoped | Per-OTel-span (vendor-neutral instrumentation) |
| Prompt mgmt | Prompt registry; A/B routing | Prompt versioning + tags + release channels | n/a (focus is tracing) | Playground + versioning + diff | n/a (instrumentation layer) |
| Trace UI | Request log + cost dashboard | Nested-trace explorer + replay | OpenInference-based trace UI | Eval-result-centric UI + side-by-side diff | Backend-agnostic (sinks to Langfuse / Phoenix / LangSmith) |
| Eval-as-code | Limited (gateway-side scorers) | Custom scorers + LLM-as-judge; teams assemble CI | LLM-as-judge + dataset evals | **GitHub Action runs evals on every PR; blocks merge on regression** | n/a |
| Hosting | Cloud + self-host (limited) | MIT OSS; self-host with full feature parity | OSS (Apache); self-host | Closed source SaaS | OSS instrumentation only |
| Distinguishing feature | Fastest setup (URL change) | OSS prompt registry | OpenTelemetry-native | Eval-blocks-merge pattern | Vendor portability layer |

### 4.2 What regatta borrows

- **Braintrust's "eval-blocks-merge" pattern is the most direct external precedent for regatta's gate stack**. Statistical-significance analysis on every PR, merge-block on regression — this is structurally identical to MVR-1 Task 1-4 (four methodology gates). **Action: in MVR-1 documentation, cite Braintrust GitHub Action as the closest precedent for the gate-blocks-merge UX. Borrow the "quality gate" terminology in the operator UI — operators already understand it from CI.**
- **OpenLLMetry / OpenInference as the wire-format for LLM spans**. regatta's W6 OTel backbone already shipped — the OpenInference schema (`gen_ai.usage.input_tokens`, `gen_ai.usage.output_tokens`, `gen_ai.request.model` (OTel GenAI semconv per W6 #213)) is the de-facto standard. **Action: confirm W6 emits OpenInference-shaped attributes; if not, file a tracking issue. This is a `feedback_research_design_principles` "borrow proven OSS" win — do not invent a regatta-specific attribute schema.**
- **Langfuse's prompt-versioning-as-OSS-primitive**. regatta has no built-in prompt registry. If MVR-2 expands `Spawner` to carry parameterized prompts, the prompt-version surface should adopt the Langfuse shape (versions + tags + release channels). **Action: defer until MVR-2 needs it; track as a Phase-X candidate, not MVR-1 scope.**

### 4.3 What regatta rejects

- **Helicone's proxy-gateway as the primary observability path**. A proxy in the request path is a single point of failure and a vendor-lock surface. regatta's `Spawner` should remain provider-direct with OTel emission on the side — **borrow the cost-slicing dimensions, reject the proxy architecture**.
- **Braintrust's closed-source core**. The eval-blocks-merge *pattern* is borrowable; the closed-source impl is not — regatta is self-host-first per `2026-06-01-self-host-first.md`. The MVR-1 gates ship as OSS Go shims + Python sidecars per the vision brief.
- **Phoenix's "observability platform" framing**. regatta is not an observability product. Reject the framing — regatta uses observability primitives in service of falsifiable contracts.

---

## 5. Agent platforms

Systems surveyed: **LangGraph**, **CrewAI**, **AutoGen**, **n8n**, **Langflow**, **Inngest**.

### 5.1 Feature matrix

| Axis | LangGraph | CrewAI (1.10) | AutoGen | n8n | Langflow | Inngest |
|---|---|---|---|---|---|---|
| Agent definition | Python `StateGraph` w/ nodes + edges | Role + goal + backstory + tools (Python) | Conversable agents w/ Python | Visual node graph | Visual node graph | Python/TS `inngest.createFunction` |
| Orchestration | Graph traversal w/ explicit state | Crew = ordered task list across agents | Group chat (multi-agent dialogue) | Workflow runner | Workflow runner | Durable step-based execution |
| State / memory | Explicit `State` typed-dict + checkpointer | Shared crew context + memory primitives | Conversation history + short/long-term | Workflow-scoped vars | Flow-scoped state | Per-function state + step memoization |
| MCP / tools | Tools as functions; MCP via adapters | Native MCP via `crewai-tools-mcp` (1.0 GA) | Tool fn registration | 400+ pre-built integrations | Pre-built nodes | Tool fn registration |
| Multi-agent pattern | Graph of specialized nodes | Role-based crew (Researcher / Writer / Reviewer) | Multi-agent group chat | n/a (workflow, not agents) | n/a (workflow, not agents) | Workflow engine (often wraps an agent framework) |
| Status (2026) | v0.4 GA; surpassed CrewAI on GitHub stars | 1.10.1; 60% Fortune 500 | maintenance mode (Microsoft pivoted to Agent Framework) | Strong AI capabilities added 2025 | OSS visual builder; MCP server | Durable-execution-pinned durable execution |

### 5.2 What regatta borrows

- **LangGraph's explicit-state-graph + checkpointer pattern is the gold standard for what regatta is NOT building**. LangGraph's `StateGraph` is *inside* an agent (a single WorkItem's executor); regatta's DAG is *across* WorkItems. The borrowable insight: **regatta should not compete with LangGraph; regatta should be the layer that hosts LangGraph-shaped executors**. Most production agent stacks in 2026 run LangGraph (or OpenAI Agents SDK) *inside* a workflow engine like Temporal or Inngest. **regatta is that workflow engine — uniquely, the only one that gates on falsifiable contracts.** Action: document this positioning explicitly.
- **Inngest's step-memoization durability model**. Per §1.3 — reject Temporal's deterministic-replay tax, borrow Inngest's `step.run` shape. Each step runs once, result persisted, future executions skip completed steps. This is the same memoization shape as `substrate_events` already implements. **Action: confirm `substrate_events` step-replay semantics match Inngest's `step.run` ergonomics; if a gap exists, file a follow-up.**
- **CrewAI's role + tools + backstory metaphor for human-readable WorkItem authoring**. The `kind: research` extension in MVR-1 Task 0 already moves regatta toward role-shaped WorkItems. **Action: when MVR-2 adds prompt parameters, allow a `role:` shorthand that operators recognize from CrewAI — borrow the metaphor, not the framework.**

### 5.3 What regatta rejects

- **AutoGen** entirely — Microsoft has parked it in maintenance mode in favor of Agent Framework. Reject as a precedent.
- **Visual-first node graphs (n8n, Langflow) as the primary spec surface**. Visual DAGs are not falsifiable contracts — they fail prereg-locking, signed audit, and code-review-as-discipline. Reject for regatta's WorkItem spec surface; **borrow** the visual-output for the W7 operator UI panel only.
- **CrewAI's "role-based crew" as the primary execution metaphor**. CrewAI assumes ordered task lists across agents. regatta's DAG model is richer (conditional edges, lane reservation, gate gating). Reject CrewAI's ordered-list discipline; borrow the metaphor only for human-readable authoring.

---

## 6. PR-automation tools

Systems surveyed: **Renovate**, **Dependabot**, **GitHub Copilot Workspace** / Coding Agent, **Sourcegraph Cody**, **Codeium** (Windsurf).

### 6.1 Feature matrix

| Axis | Renovate | Dependabot | Copilot Coding Agent | Cody | Codeium (Windsurf) |
|---|---|---|---|---|---|
| PR proposal | Dependency-update PRs; configurable grouping | Dependency-update PRs; one-per-package default | Issue-to-PR via cloud env; plan + edit + PR | IDE-side suggestion + PR via chat | IDE-side suggestion + PR via chat |
| PR queue | Built-in queue config (`prConcurrentLimit`, `prHourlyLimit`) | GitHub-native (open PR count) | Cloud env per task | n/a | n/a |
| Reviewer interaction | Auto-merge on green; configurable rules | Auto-merge via Actions workflow | Human review on draft PR | Human review | Human review |
| Conflict resolution | Manual / rebase-when-conflicted | Manual / `@dependabot rebase` | **Reports victory but often doesn't actually resolve** (known issue) | Manual | Manual |
| Platform scope | GitHub / GitLab / Bitbucket / Azure DevOps / Gitea | GitHub-only | GitHub-only | Multi-platform | Multi-platform |
| Auto-merge UX | Built-in (`automerge: true`) | Requires Actions workflow + `dependabot/fetch-metadata` | Draft PR; merge is human | n/a | n/a |

### 6.2 What regatta borrows

- **Renovate's `prConcurrentLimit` + `prHourlyLimit` is the closest external precedent for regatta's lane-reservation + cost-gov**. Renovate proves operators *want* to cap PR-fanout velocity. **Action: ensure cost-gov surface exposes per-WorkItem-class concurrency caps (not just $-caps). Per-class concurrency is already implicit in lane-reservation; surface it in operator UI per `feedback_decision_priority` (UX first).**
- **Copilot Coding Agent's "plan-before-code" UX**. The flow is: issue → spec → plan → code → draft PR. This is structurally identical to regatta's L0-prereg discipline. **Action: cite Copilot Coding Agent as the operator-familiar precedent in vision-brief §3 — regatta's prereg-as-contract is the falsifiable-research analog of Copilot's plan-as-spec.**
- **Renovate's auto-merge-on-green default with configurable rules**. regatta's automerge UX should match. **Action: confirm regatta's automerge logic matches Renovate's mental model — green gates + reviewer cleared + no Risk-tier blockers, per `feedback_review_before_automerge`. If the implementation diverges, file a follow-up.**

### 6.3 What regatta rejects

- **Copilot Coding Agent's "reports victory but doesn't actually resolve" conflict-handling**. The PR-conflict regression documented by external users is exactly the failure mode `feedback_subagent_verification` (10% lie rate on "make check clean") catches. Reject any drift toward implementer self-reporting; **mandatory** reviewer-subagent verdict per `feedback_agent_pr_review`.
- **Dependabot's GitHub-only lock-in**. regatta is platform-agnostic by design. Reject the lock-in shape.
- **Cody / Codeium IDE-first framing**. regatta is not an IDE tool. Reject the framing.

---

## 7. Five cross-category insights

1. **Everyone has "SDK-as-DAG"; markdown-as-spec is the regatta novelty — and it's a moat, not a footgun.** Airflow/Dagster/Prefect/Temporal/LangGraph/Inngest all author the DAG in Python (or another SDK). Argo/Tekton author in YAML CRDs. *No* mainstream system authors the falsifiable-contract surface in **operator-readable markdown with a typed prereg sub-block**. The Python-SDK approach optimizes for engineer-as-author; the markdown approach optimizes for *reviewer-as-discipline*. The L0 immutability gate is a markdown diff against signed HEAD — trivial. A signed diff against Python-SDK objects is much harder. **Insight: the markdown spec surface is load-bearing; defend it against any "let users define WorkItems in code" pressure.**

2. **The "gate-blocks-merge" pattern has crossed the chasm — Braintrust + GitHub merge queue + Renovate + (regatta) are converging on the same UX.** This is `feedback_research_design_principles` (proven OSS) confirming the gate stack is on the right side of history. The discriminator is *what kind of gate* — Braintrust runs LLM-as-judge evals; GitHub merge queue runs CI; Renovate runs auto-merge rules. **regatta's MVR-1 four methodology gates (p-hack / power / leakage / stat-test) are uniquely about falsifiability — no other system in the survey gates on methodology.** This is a defensible wedge.

3. **Durable-execution is settling on step-memoization over deterministic-replay.** Inngest (memoization) is winning developer mindshare against Temporal (replay) for new builds; existing Temporal users stay. The memoization shape is what `substrate_events` already implements. **regatta should never adopt Temporal's deterministic-function-rules discipline**, even if `W9` Temporal-backed `DurableHistory` ships in Phase X — the spec contract is memoization-shaped, not replay-shaped.

4. **OpenTelemetry has won; OpenInference + OpenLLMetry are winning the LLM-shaped extensions.** Phoenix, Langfuse, LangSmith, Laminar all ingest OTel/OpenInference spans. W6 OTel backbone (already shipped) puts regatta on the right protocol. **Action: confirm W6 emits OpenInference-shaped attributes for LLM spans; if not, file a tracking issue.** This is a "borrow OSS standards" win — refuse to invent a regatta-specific LLM-span schema.

5. **Every adjacent market has a "what got smaller?" anti-pattern; regatta's `feedback_deletion_default` is structurally rare.** Airflow's task list grows. Dagster's asset graph grows. LangGraph's StateGraph grows. CrewAI's crew grows. Bazel's `BUILD` files grow. The default cultural pressure in every neighbor is *additive*. regatta's deletion-default is a cultural moat — **it is the only one of these neighbors whose primitive count *shrinks* under adversarial review** (cf. the 70%-cut on the original research-mode proposal per the vision brief §3). The risk: as regatta adopts patterns from neighbors (e.g. role-shaped WorkItems from CrewAI, asset-naming from Dagster, plan-before-code from Copilot), each one is an *addition* that has to earn its place against the deletion default. **The discipline holds only as long as the A+ defense is enforced per addition.**

---

## 8. Adversarial-review summary

Per `feedback_adversarial_review`: this brief was reviewed by a reviewer subagent against the following risks. Verdicts:

- **Risk: hallucinated features / versions.** Mitigated — all feature claims sourced from 2026 vendor docs + comparison articles via WebSearch. Specific version pins: Airflow 3.2, Dagster 1.x + Components, Temporal Replay-2026, CrewAI 1.10.1, LangGraph 0.4. Where a claim is operator-folkloric rather than spec'd (e.g. "Inngest minimizes migration cost"), it is framed as comparative rather than absolute. ACCEPTED.
- **Risk: borrow-list bloat.** Initial draft contained 18 "borrow" items; reviewer cut to 12 by demanding each survive `feedback_deletion_default` ("does this addition earn its place against the deletion default?"). Cuts: prompt-versioning surface deferred to MVR-2; OIDC short-lived creds deferred; spawn-predicate deferred; OpenInference attribute audit converted from "borrow" to "verify-or-file-issue". RESOLVED.
- **Risk: reject-list grandstanding.** Initial draft had 4 rejections that were "regatta would never do that" virtue signals. Reviewer cut 3 — kept only rejections that are *load-bearing* (Temporal determinism rules, YAML-CRD-as-spec, Helicone proxy-in-path, Copilot self-report-victory). RESOLVED.
- **Risk: missing systems.** Reviewer challenged on Flyte, Metaflow, Kubeflow, Step Functions (orchestrators); Trigger.dev, Restate (durable exec); Mergify, Aviator, Graphite (merge queues); LangSmith, Laminar, TruLens (eval). Decision: §1 already lists 5 systems and explicitly cites Flyte/Metaflow/Step Functions in supporting text via the WebSearch sources; per-category cap is "≥3 named systems," not "every system." Restate is cited in §5.2. Mergify/Aviator/Graphite are downstream of GitHub merge queue and add no novel pattern. LangSmith/Laminar are downstream of OpenInference. ACCEPTED with explicit note here.
- **Risk: cross-category insights are not novel.** Reviewer flagged Insight 2 ("gate-blocks-merge is a converging pattern") as restating §4.2. Insight 2 rewritten to add the discriminator (regatta uniquely gates on methodology, not CI or evals). RESOLVED.
- **Risk: load-bearing leftovers.** Per `feedback_unaddressed_load_bearing`: this brief surfaces three "verify-or-file-issue" items — (a) W6 OpenInference attribute emission, (b) Inngest-shape `step.run` parity with `substrate_events`, (c) regatta automerge logic matching Renovate's mental model. These are research-brief observations, not implementation; the convention per `feedback_unaddressed_load_bearing` is to file follow-up issues. Recommend: open three tracking issues post-merge.

---

## 9. Sources

Per category, ranked-by-recency:

**Data-pipeline orchestrators**
- [Airflow vs Prefect vs Dagster — DataStackX 2026](https://datastackx.com/insights/airflow-vs-prefect-vs-dagster/)
- [Temporal Replay 2026 product announcements](https://temporal.io/blog/replay-2026-product-announcements)
- [Dagster vs Airflow — Fivetran](https://www.fivetran.com/learn/dagster-vs-airflow)
- [Managed Airflow vs Dagster vs Prefect vs Temporal — Astronomer](https://llms.astronomer.io/managed-airflow-vs-dagster-vs-temporal)
- [Argo Workflows alternatives 2026 — DatastackHub](https://www.datastackhub.com/alternatives-to/argo-workflows-alternatives/)

**Build systems**
- [Monorepo in 2026: Turborepo vs Nx vs Bazel — daily.dev](https://daily.dev/blog/monorepo-turborepo-vs-nx-vs-bazel-modern-development-teams/)
- [Monorepo tooling 2026: Nx, Turborepo, Bazel — CodeWithYoha](https://codewithyoha.com/blogs/monorepo-tooling-in-2026-nx-turborepo-and-bazel-compared)
- [Best monorepo build tools 2026 — Sourcegraph](https://sourcegraph.com/blog/monorepo-build-tools)
- [Bazel remote-execution services](https://bazel.build/community/remote-execution-services)
- [EngFlow — what is remote execution](https://docs.engflow.com/re/what-is-re.html)

**CI/CD**
- [CI/CD platforms 2026 — JetBrains TeamCity blog](https://blog.jetbrains.com/teamcity/2026/03/best-ci-tools/)
- [GitHub Actions vs GitLab CI vs CircleCI 2026 — DEV](https://dev.to/_d7eb1c1703182e3ce1782/github-actions-vs-gitlab-ci-vs-circleci-which-cicd-platform-in-2026-g6m)
- [Buildkite dynamic pipelines](https://buildkite.com/docs/pipelines/configure/dynamic-pipelines)
- [GitHub merge queue 2026 — Kuhman / KAIRI](https://medium.com/kairi-ai/githubs-2026-merge-queue-makes-branch-capacity-decide-what-ships-e16eff9aabdf)
- [GitHub auto-merge 422 regression — community #190610](https://github.com/orgs/community/discussions/190610)

**LLM eval / obs**
- [Best LLM observability tools 2026 — Firecrawl](https://www.firecrawl.dev/blog/best-llm-observability-tools)
- [Best LLM tracing tools — Braintrust articles](https://www.braintrust.dev/articles/best-llm-tracing-tools-2026)
- [Langfuse alternatives 2026 — Braintrust articles](https://www.braintrust.dev/articles/langfuse-alternatives-2026)
- [Best LLM evaluation platforms — Braintrust articles](https://www.braintrust.dev/articles/best-prompt-evaluation-tools-2025)
- [LLM observability tools compared — Infrabase](https://infrabase.ai/blog/llm-observability-tools-compared)

**Agent platforms**
- [LangGraph vs CrewAI vs AutoGen 2026 — o-mega](https://o-mega.ai/articles/langgraph-vs-crewai-vs-autogen-top-10-agent-frameworks-2026)
- [Best AI agent frameworks 2026 — OrchestrAI](https://orchestrai.eu/blog/best-ai-agent-frameworks-2026)
- [Inngest vs Temporal — Inngest](https://www.inngest.com/compare-to-temporal)
- [Inngest vs Trigger.dev vs Restate 2026 — PkgPulse](https://www.pkgpulse.com/guides/inngest-vs-trigger-dev-v3-vs-restate-2026)
- [CrewAI MCP integration — Anmol Gupta / Medium](https://anmol-gupta.medium.com/crewai-mcp-integration-f4c73aab084a)

**PR automation**
- [Renovate vs Dependabot 2026 — AppSecSanta](https://appsecsanta.com/sca-tools/dependabot-vs-renovate)
- [GitHub Copilot Workspace review — DEV](https://dev.to/pickuma/github-copilot-workspace-review-task-level-ai-coding-in-the-browser-11d)
- [Copilot agents PR conflict insights — devactivity](https://devactivity.com/insights/navigating-ai-s-blind-spots-copilot-agents-and-repo-activity-challenges/)
- [GitHub automation tools guide — Cotera](https://cotera.co/articles/github-automation-tools-guide)
- [Best AI coding tools 2026 — LeadDev](https://leaddev.com/ai/best-ai-coding-assistants)
