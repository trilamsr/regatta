# Regatta: A Repo-Agnostic Autonomous-Agent Fleet

- **Status:** Draft design, pre-implementation
- **Author(s):** Tri Lam
- **Created:** 2026-05-19
- **Last updated:** 2026-05-19

> Point Regatta at any git repo with planned work and a deterministic
> test command, and a fleet of Claude agents picks up open work items,
> develops them in isolated worktrees, and opens PRs gated by a
> configurable review stack hardened against every publicly-documented
> AI-agent incident class as of mid-2026.

## Summary

### Operator TL;DR (60-second read)

**What it is.** A single Go daemon you self-host. You point it at a
git repo plus a machine-readable work source (GitHub issues, Jira,
Linear, `RFC.md`, custom adapter) plus a deterministic CI command.
Regatta spawns one Claude agent per ready work item in an isolated
worktree, runs a configurable gate stack (L0-L6) on every PR push,
and routes feedback to the agent. **You always merge.** Regatta
refuses to.

**What it explicitly is not.** Not hosted SaaS. Not multi-repo
(today). Not multi-vendor (today: Claude only; cross-vendor adapter
behind a measured trigger). Not an unstructured-spec discoverer
(your repo must encode "done" somewhere). Not a merge button (L6 is
mandatory human review).

**What ships today.** Two commands you can run right now:
`regatta l0 <diff>` (deterministic spec-immutability verdict) and
`regatta verify-repo-config -owner -repo` (audit a GitHub repo
against the P2 canonical recipe). Everything else in this doc is
the contract; the binary follows.

Regatta is a standalone service that orchestrates autonomous Claude
agents against any git-hosted repo. The repo declares its planned work
(GitHub issues, Jira, Linear, an `RFC.md`, custom adapter, etc.), its
deterministic check command, and a `regatta.yaml`. The orchestrator
spawns one agent per ready work item, watches the resulting PR, runs a
configurable gate stack, and routes feedback back to the agent.

### Two beliefs

Every other choice traces to these.

- **The spec is the oracle.** Repos already encode "done" in
  machine-readable form (acceptance criteria, RFC rubrics, test
  plans). Regatta plugs into that surface via `SpecAdapter`
  ([§Spec contract](#spec-contract)); it does not invent a new format.
- **The platform enforces what the prompt cannot.** Every public AI-
  agent incident -- Replit's prod-DB wipe, EchoLeak, Cursor MCPoison
  RCE, Copilot credential leak via PR title, the curl bounty
  shutdown over AI slop -- shares a root cause: a defense that lived
  in the agent's prompt or self-report instead of in the platform.
  Regatta's gates are deterministic, out-of-band, and signed wherever
  possible. The [Trap Catalog](#trap-catalog) maps 19 incidents to
  10 enforcement points.

The default gate stack is six layers; the [§Gate stack](#gate-stack-normative)
section is the normative source. Repos add custom gates, reorder,
swap models, and tune thresholds via `regatta.yaml`. Deterministic
gates run first in parallel; AI gates run after the deterministic
floor passes. Default-deny on any blocking finding. L0 is mandatory.

**Multi-feature work** (RFC-class decomposition into a DAG of
related PRs) is handled by an opt-in program layer
([§Programs](#programs-multi-feature-decomposition)) that decomposes
a parent `WorkItem{kind: program}` into child WorkItems and adds
a signed handoff schema + one optional security custom gate. The
program layer does **not** introduce a parallel agent runtime,
validator subsystem, or LLM verdict-routing agent; program progression
is a deterministic Go function over signed `GateResult`s. The
layer ships as MVP-1 → MVP-3 with a measured kill switch: if
program decomposition does not yield ≥5 pp net-helpfulness gain
over single-agent on the canary corpus, the program concept is
abandoned and the doc reverts to base Regatta.

### Companion artifacts

`docs/incidents.md` (19 incidents + full P1-P10 prose);
`schemas/spec_adapter.go` (Go interface, normative);
`schemas/{gate_result,work_item}.schema.json` (JSON Schemas);
`schemas/regatta.v1.cue` (config schema); `testdata/gates/l0/` (L0
fixture corpus contract); `testdata/gates/canary/` (canary archetype
corpus). Program-layer additions arrive in MVP-1 → MVP-3:
`schemas/handoff.schema.json` (signed inter-feature handoff,
normative), `gates/security/` (the only new gate kind).

## Motivation

**Spec → PR is embarrassingly parallel** -- open work items are mostly
independent. A team processing them serially leaves throughput on the
table; a team processing them in parallel *without rigorous gates* is
one bad merge from a Replit-class incident.

**Existing agent products ship at the wrong abstraction.** SWE-agent,
Devin, Cursor agent mode, Copilot Workspace, Aider -- each ships an
agent runtime then leaves gating to the consumer's existing review
process. That process was designed for human PRs; it doesn't catch
reward-hacking, vacuous tests, sycophantic acceptance, hallucinated
dependencies, or prompt-injection via repo content. Regatta inverts:
**the gates are the product**; the agent runtime is interchangeable
(see [§Alternatives](#alternatives) for the honest read).

**The incident catalog is dense and growing.** 2023-2026 documents 19
distinct incident classes. Closing the 10 known load-bearing failure
modes is feasible only because the platform sits between the agent
and the destructive surface.

## Threat Model

The threat model is the floor every other section sits on. If you
disagree with any assumption here, the design downstream is wrong for
your environment.

### Adversary stance (normative)

Every defense in this doc is built against three explicit adversary
assumptions; each section below names which adversary it defends
against:

1. **The agent is compromised.** Worker sessions are assumed
   capable of producing any output text within their tool
   surface, including outputs that lie about exit codes, fabricate
   citations, embed invisible glyphs, or attempt to mutate the
   spec. Defenses MUST not depend on the agent's good faith.
2. **All sources are attacker-controlled text.** Issue bodies,
   RFC files, MCP-tool outputs, linked artifacts, validator
   subagent text, prior gate output, and `AGENTS.md`-class
   guidance are DATA, never INSTRUCTIONS. The only trusted
   instructions are `prompts/*.md` files signed and pinned at the
   orchestrator boundary, loaded from `main@SHA`.
3. **The planner is honest-but-coerceable.** The planner agent
   itself is not adversarial, but its inputs are. A poisoned RFC
   linked from a parent `WorkItem` can steer the planner's
   `program_brief.json` into a malicious DAG. Defense: every
   `linked_artifact` is L0-normalized + glyph-stripped + scanned
   for imperative second-person instructions to the planner
   BEFORE the planner sees it.

These three assumptions are the audit framework. A defense that
fails under any of them is a defect.

### Assets

- **`regatta.db`** (orchestrator state) -- agent lifecycle, gate
  verdicts, token spend, canary results. Contains short-lived OIDC
  tokens at rest.
- **`regatta.yaml`** -- safety policy, gate config, allowlists. Editing
  it relaxes every other defense.
- **`prompts/*.md`** -- gate-prompt artifacts. Editing them changes
  what L3/L4/L5 ask the model.
- **Agent worktrees** -- transient; cleaned on reap.
- **Target repo `main`** -- the only trust root for instructions.
- **Audit log** -- append-only, written to an out-of-band sink.

### Principals

| Principal | Trust level | Capabilities |
|---|---|---|
| Maintainer | trusted | L6 merge, edit `regatta.yaml` via PR + CODEOWNERS |
| PR author (human) | semi-trusted | Open PR, push to feature branch |
| PR author (regatta agent) | low-trust | Open PR, push to `regatta/*` branch, no write to `main` |
| Repo external | untrusted | Read public state, open issues |
| Orchestrator host | high-trust *but* threat-modeled | Holds tokens, runs gates |
| Compromised orchestrator | adversary | Treated as untrusted; defense via signed audit log |

### Trust boundaries

The signed trust root (target repo `main` at a pinned tag SHA -- holds
`regatta.yaml`, `prompts/`, agent-guidance) flows down to the
orchestrator host (assumed honest-but-fallible, holds `regatta.db` and
the OIDC mint), which spawns isolated agent worktrees (untrusted, no
`main` credentials) and gate runners (low-trust, model API only, no
repo write). Every action is countersigned to the out-of-band audit
sink.

### Threats explicitly defended

The 13 patterns in §Trap Catalog. The most load-bearing six (each
preventing 3+ documented incidents): P1 (deterministic gate before AI
gate on destructive ops), P3 (trusted text from `main` only), P5
(out-of-band supervisor), P6 (verified grounding), P8 (spend +
iteration brakes), P10 (invisible-glyph normalization + signed prompts).

### Threats explicitly *not* defended

- **Compromised model provider** producing systematically-biased gate
  output. Mitigation deferred to the cross-vendor adapter.
- **Side-channels in PR comments or CI logs**. Egress-content scanning
  for known secret formats is on the roadmap but not yet
  implemented.
- **Malicious maintainer**. Two-key approval on `regatta.yaml` edits
  (CODEOWNERS routes to two disjoint teams + `required_approving_review_count: 2`
  + `enforce_admins: true` + `require_last_push_approval: true`) is the
  only mitigation; collusion is not modeled. Note that without
  `enforce_admins: true` (off by default on GitHub) any admin silently
  bypasses every rule below -- `regatta verify-repo-config` enforces this
  toggle pre-flight.

### Tamper-evident audit

Every gate result is HMAC-signed against an orchestrator key that
rotates per restart (see `schemas/gate_result.schema.json:signature`).
Gate results are written to an append-only out-of-band sink (S3
object-lock or transparency log per `regatta.yaml: telemetry.audit_sink`),
*not* solely to `regatta.db` or to PR comments. This closes the
compromised-orchestrator scenario: an attacker who edits `regatta.db`
cannot forge the audit log without also possessing the sink-write
credential, which lives outside the orchestrator host.

## Trap Catalog

The 13 platform-enforcement patterns that close the documented AI-agent
incident classes. Each pattern is wired into the architecture below;
none is advisory. Full prose, primary-source citations, and the
cross-pattern map live in [`incidents.md`](incidents.md).

| ID | Pattern | Prevents (incidents from [incidents.md](incidents.md)) |
|---|---|---|
| **P1** | Deterministic gate before AI gate on destructive ops | Replit (#1), PocketOS (#2), slopsquatting (#11), Lovable RLS (#12), Antigravity (#28), Kiro (#27, disputed) |
| **P2** | Two-key approval on irreversible actions | Replit (#1), PocketOS (#2), Copilot RCE (#10), Kiro (#27, disputed) |
| **P3** | Trusted instructions from `main` only; all other text is data -- including config / MCP discovery / deeplinks / ingested documents | Comment-and-Control (#8), EchoLeak (#6), MCPoison/CurXecute/Rules-File (#9), Copilot RCE (#10), MCP-Git (#20), TrustFall (#21), DXT calendar (#24), ClaudeBleed (#25), claude-cli deeplink (#26), Cowork (#29) |
| **P4** | Least-privilege, ephemeral, environment-scoped credentials | PocketOS (#2), Amazon Q (#7), Comment-and-Control (#8), DXT (#24), ClaudeBleed (#25) |
| **P5** | Out-of-band supervisor for limits and kill-switches | Sakana (#3), o3 shutdown (#4), Cursor runaway (#19), TrustFall (#21), Antigravity (#28) |
| **P6** | Verified grounding for any outward-facing claim | Air Canada (#13), MyCity (#14), Mata (#17), slopsquatting (#11), curl (#16) |
| **P7** | Schema-level scope constraints, not prompt-level | Chevy $1 (#15), MyCity (#14), Air Canada (#13), Antigravity workspace-root (#28) |
| **P8** | Spend / iteration brakes with mandatory re-approval | Cursor runaway (#19), Sakana (#3), GTG-1002 (#18) |
| **P9** | Sensitive context segregation | Opus 4 blackmail (#5), EchoLeak (#6), Cowork PII (#29) |
| **P10** | Invisible-glyph normalization + signed prompt artifacts | Rules-File-Backdoor (#9), Amazon Q (#7), MCPoison (#9), TrustFall (#21), ClaudeBleed (#25), claude-cli (#26), Cowork (#29) |
| **P11** | Agent-artifact release pipelines are themselves attack surface -- content audit + build attestation + runner-memory isolation + age gate | Amazon Q wiper (#7), Claude Code source leak (#23), Mini Shai-Hulud (#30) |
| **P12** | Inbound vulnerability signals default-escalate -- two-key on the decision to ignore | curl slop (#16), Cowork (#29), Lovable BOLA Apr-26 (#31) |
| **P13** | Judge-LLM lineage isolation + read-only metric channel | Reward-hacking corpus (#32) |

P1, P3, P5, P6, P8, P10 each prevent 3+ documented incidents and are
the highest-leverage rules. P11 (release-pipeline) prevents 3, P12
(vuln-intake) prevents 3, P13 (judge-isolation) is forward-looking
from the 2026 reward-hacking corpus and the Preference Leakage ICLR
2026 finding.

### How patterns map to layers

| Layer | Patterns enforced |
|---|---|
| L0 spec-immutability | P1, P3, P10 (rubric body) |
| L1 repo CI | P1 (license, govulncheck), P6 (test grounding), P11 (artifact-content audit + build attestation on agent commits) |
| L2 PR-body | P6 (citation block) |
| L3 spec-conformance | P6 (per-criterion grounding), P13 (judge runs in sibling sandbox) |
| L4 adversarial | P3, P6, P7, P9 (all read against signed `main` text), P13 (cross-family escalation on family-stratified catch-rate <0.85) |
| L5 drift | P3 (consumer-test contract from `main`) |
| L6 human merge | P2 |
| Orchestrator | P4, P5, P8, P10, P11 (release-pipeline checks on Regatta's own binaries), P13 (immutable audit channel) |
| Intake (vuln reports) | P12 (default-escalate; two-key on Won't-Fix) |

## Proposal

### Architecture

A standalone Go daemon (the **Regatta orchestrator**) holds the
`SpecAdapter`, the scheduler with sorted-lock acquisition, the agent
spawner (`claude --resume`), the PR watcher (GitHub/GitLab adapter),
the gate runner (parallel L0-Lx), the rejection router (K=3 then
escalate), the canary injector (~5%), supervisor limits (cgroup-
enforced), and the reaper + lesson capture. It spawns one Claude
agent per ready work item into a lane-isolated worktree; each agent
opens a PR; the gate stack runs on every push.

### Spec contract

A work item is the agent's complete spec. Regatta declares a
`SpecAdapter` interface
([`schemas/spec_adapter.go`](../schemas/spec_adapter.go) -- normative);
the repo selects an implementation.

Built-in adapters: `github_issues`, `gitlab_issues`,
`markdown_catalog`, `jira`, `linear`, `custom` (shells out to a binary
on PATH via a versioned JSON-over-stdio protocol).

Adapters split into two tiers based on what the platform's API actually
exposes. **First-class adapters** (`github_issues`, `markdown_catalog`)
support immutable-snapshot retrieval (ETag for GitHub, commit SHA for
markdown), audited edit history (GitHub's `userContentEdits.diff` walk
gives best-effort prior-text reconstruction with `ErrSourceUnverifiable`
fallback), and atomic state transitions. **Degraded-mode adapters**
(`gitlab_issues`, `jira`, `linear`) signal *that* a description
changed (via `updatedAt`, `version`, or `IssueHistory.updatedDescription`)
but cannot return the prior body text -- Linear's GraphQL `IssueHistory`
exposes `fromTitle`/`toTitle` and many other paired fields, but
verified live (2026-05-20) it does *not* expose `fromDescription`/`toDescription`,
only a Boolean `updatedDescription` flag plus a `descriptionUpdatedBy` list
and an opaque `changes` JSONObject. On these adapters L0 falls back to
"detect mutation, halt agent, file clarification item" rather than
"verify byte-equality." `regatta init` prints the selected adapter's
tier and refuses to advance without explicit acknowledgement when the
tier is degraded.

`WorkItem` shape: see
[`schemas/work_item.schema.json`](../schemas/work_item.schema.json).
Load-bearing fields:

- `acceptance_criteria[].text` -- **immutable post-publication**. L0
  enforces byte-equality under UTF-8 NFC normalization and
  invisible-glyph stripping.
- `acceptance_criteria[].state` -- flipped by the agent on completion.
  The flip must arrive with a `citation` field matching
  `^(test|file|commit)=\S+$`.
- `dependencies` -- DAG; orchestrator only spawns agents whose chain is
  satisfied. Cycles fail-fast at adapter `List()`.
- `source` -- immutable pointer L0 uses for verification (SHA-pinned
  for file adapters, ETag for API adapters).

Spec ambiguity the agent cannot resolve becomes a new
"needs-clarification" item in the spec source plus a halt. The agent
does not invent semantics.

### Agent shape

Each agent is a `claude` CLI session (default Opus 4.7,
prompt-caching mandatory) launched into a dedicated worktree at
`.regatta/worktrees/<work-id>-<slug>/`.

- Launch prompt templated from `prompts/agent.md` (signed,
  SHA-pinned); the repo can override.
- Auto-loaded context: the work item, its `linked_artifact`, the
  repo's agent-guidance file (`AGENTS.md` / `CLAUDE.md` /
  `CONTRIBUTING.md`), the immutability rules.
- Loop discipline: readiness-audit each iteration; max iterations from
  `regatta.yaml: safety.iteration_cap` (default 50).
- Session state in `.regatta/sessions/<work-id>.json` (gitignored).

The loop:

1. Read item + criteria + linked artifacts.
2. Plan which criteria this iteration targets.
3. Test-first: failing test for the first untargeted criterion.
4. Implement minimum to pass.
5. Run the repo's CI command; iterate.
6. Flip `state` to `done` with citation (text untouched -- L0 checks).
7. Write the PR body per the repo's PR template.
8. `gh pr create`. Stop. Wait for gate routing or supervisor signal.

### Gate stack (normative)

**L0 -- spec-immutability** (deterministic, mandatory). Diffs the spec
source between PR base and HEAD; for each touched work-item entry,
asserts the only change is the `state` field flip. Acceptance-criteria
text edits fail hard. Full normative behavior -- merge-base choice,
UTF-8 NFC, rename handling, invisible-glyph stripping, merge-time
re-run -- is specified in
[`testdata/gates/l0/README.md`](../testdata/gates/l0/README.md). The
fixture corpus there is the contract; L0 implementations pass if and
only if they pass every fixture.

**L1 -- repo CI** (deterministic, mandatory, configurable command).
Whatever passes for "green" in the repo today. Regatta shells out per
`ci.command`; non-zero exit blocks.

**L2 -- PR-body conformance** (deterministic, mandatory, configurable
template). Validates the citation block, optional ticket link, optional
release-notes section.

**L3 -- spec-conformance verifier** (AI judicial; default Opus 4.7).
For each flipped criterion in L0's output: read criterion text
**fetched from `main` SHA** (not PR branch -- P3), the PR diff, the
citation. Emits a `GateResult` per
[`schemas/gate_result.schema.json`](../schemas/gate_result.schema.json)
with `gate_kind: ai_judicial`. Any per-criterion `verdict: fail`
blocks. Strong-judgment task → most-capable model. Does not evaluate
code quality (L4's job).

**L4 -- adversarial reviewer** (AI adversarial; default Sonnet 4.6).
Prompted to *reject*. Reads repo principles/style/non-functional
budgets (paths in `context.trusted_doc_paths`, all fetched from signed
`main`). Output: severity-ranked findings; blocks on `critical` or
≥2 `high`. Optionally invokes a deeper review skill on PRs labeled
`regatta:rigorous-review`. (See [§Alternatives](#alternatives) for the
honest read on Sonnet-vs-Opus as a "different family.")

**L5 -- drift detector** (AI rule-check; default Haiku 4.5). For files
touched: verify the corresponding tracking docs / changelogs / ADRs
are updated per repo rules. Enforces the **consumer-test contract**:
symbols cited by downstream work items must have a `*_consumer_test.*`
in the consumer's package before the upstream PR merges.

**L6 -- human merge** (mandatory, branch-protection enforced). The
maintainer reads L0-L5 + custom-gate `GateResult` comments and makes
the merge call. AI review is **advisory and rejecting**, never
approving.

**Custom gates.** Repos add gates of either kind via `regatta.yaml`.
Example shapes: `license_audit` (deterministic), `migration_safety`
(AI), `i18n_completeness` (deterministic).

### Orchestrator shape

Long-running Go daemon. The recommended deployment is a self-hosted
daemon watching one or more repos via VCS APIs -- fastest to iterate,
lowest blast radius. GitHub Actions integration and a hosted
multi-tenant service are deferred.

Responsibilities:

1. **SpecWatcher** -- polls or webhook-subscribes the configured
   `SpecAdapter`; builds a work queue of items whose dependency chain
   is satisfied.
2. **Scheduler** -- caps concurrency per lane (default 1); holds soft
   locks on hotspot files declared in `regatta.yaml`. **Lock
   acquisition is sorted by lock-name** to prevent deadlock; locks
   carry a heartbeat lease (15 min default, refreshed every 60 s)
   that auto-releases on agent SIGKILL.
3. **AgentSpawner** -- creates worktree; spawns `claude` with the
   templated prompt; captures pid + session-id atomically into sqlite
   (one transaction reserves the lane and records the pid).
4. **PRWatcher** -- runs gates in parallel against the HEAD SHA. Each
   gate run is keyed by `(pr_sha, gate_id)` for idempotency: a re-run
   against the same SHA returns the same `run_id` and skips
   recomputation.
5. **RejectionRouter** -- rejecting gate `GateResult` newer than the
   agent's last commit wakes the agent. K=3 rejections → "needs
   human", agent unspawned (P8).
6. **CanaryInjector** -- Bernoulli(p=`safety.canary_rate`) on each
   spawn; archetype patches from `testdata/gates/canary/catalog.ndjson`.
   See [§Test harness](#test-harness).
7. **SupervisorLimits** -- wall-clock, disk, network, iteration, spend
   enforced in the parent process via cgroups (Linux) or rlimits
   (macOS). The agent cannot read or modify these (P5).
8. **Reaper** -- on merge: tear down worktree, release locks, archive
   session state, kick scheduler. On close-without-merge: same plus
   `needs-rework` label.
9. **LessonCapture** -- at agent EOL, if friction signals fired,
   drafts a small PR to the repo's agent-guidance file. **The file
   must be CODEOWNERS-protected.** Recursive-taint defense.

### State, persistence, recovery

`regatta.db` is sqlite via `modernc.org/sqlite` (pure Go). The state
machine per agent:

```
 pending ──spawn──▶ spawning ──ack──▶ running ──pr_opened──▶ pr_open
                                                                │
                       ┌────────────────────────────────────────┘
                       ▼
                 gates_running ─all_pass─▶ awaiting_merge ──merged──▶ done
                       │                            │
                       └──any_fail──▶ gates_failed  └─closed──▶ withdrawn
                                          │
                                   ┌──────┴──────┐
                                   ▼             ▼
                                  revise      escalate (K=3)
```

Recovery on orchestrator restart:

1. Read all agents in `spawning|running|pr_open|gates_running`.
2. For each, query its pid; if dead, transition to `crashed` and
   re-queue.
3. For each open PR, re-fetch HEAD SHA; if the recorded SHA matches,
   restore in-flight gate runs from `regatta.db`; else discard stale
   runs and re-evaluate.
4. Release any locks whose heartbeat is older than `15 min × 2`.

This is the explicit crash-recovery contract; without it, a SIGKILL
during spawn leaks both the worktree and the lock.

### Concurrency & soft-lock policy

Per-lane concurrency cap (default 1). Cross-lane parallelism limited
only by token budget and maintainer review bandwidth.

**Sorted-lock acquisition.** When an agent's PR touches multiple
hotspot files, locks are acquired in lexicographic order. This is
sufficient to prevent deadlock under FIFO scheduling.

**Heartbeat lease.** Lock holders renew every 60 s; the scheduler
forcibly releases stale locks (heartbeat > 30 min old) on the next
scheduling tick.

**Escape valve.** ≥3 merge-queue rebase failures in 24 h → auto-
throttle to 1 in-flight PR globally + alert.

### Per-repo configuration (`regatta.yaml`)

Full schema in [`schemas/regatta.v1.cue`](../schemas/regatta.v1.cue);
migration via `regatta migrate-config --from 1 --to 2`. Minimal
example:

```yaml
version: 1
repo: { host: github, owner: example, name: myproject }
spec_adapter: { type: github_issues, selector: 'label:planned' }
ci: { command: 'npm test && npm run lint' }
gates:
  - { id: spec_conformance, type: ai, model: claude-opus-4-7,   severity_block: ['fail'] }
  - { id: adversarial,      type: ai, model: claude-sonnet-4-6, severity_block: ['critical', '2*high'] }
  - { id: drift,            type: ai, model: claude-haiku-4-5,  severity_block: ['drift'] }
lanes:
  - { id: server, paths: ['src/server/**'], max_concurrency: 1 }
hotspots: [CHANGELOG.md, package.json, README.md]
safety: { iteration_cap: 50, spend_cap_usd: 50, canary_rate: 0.05 }
context: { agent_guidance_path: AGENTS.md, agent_guidance_codeowners_check: true }
telemetry: { audit_sink: 's3://acme-audit/regatta/?object-lock=COMPLIANCE' }
```

The `severity_block: ['critical', '2*high']` mini-DSL: `critical` OR
≥2 `high` blocks. Only `&`, `|`, and `count*severity` operators
permitted; the validator rejects others.

## Programs (multi-feature decomposition)

A **Program** is a parent `WorkItem` marked `kind: program` that
decomposes into a DAG of child `WorkItem`s. Each child runs through
the existing single-agent flow (worktree, L0-L6, human merge). The
program layer adds a planner front end, a tamper-evident handoff
between siblings, and one new custom gate; **it does not introduce a
parallel agent runtime, validator subsystem, or LLM-driven decision
agent.**

### Why programs

Single agents hit context-window and attention-degradation limits on
multi-day work (Factory's published Programs data: 14% of multi-agent
runs >24 h; the longest 16 days). SWE-bench-Verified 2026 shows
~6pp improvement from decomposition under the same model (Opus 4.5:
45.9% → 51.8% with better scaffolding). The program layer is
Regatta's bid for that 6pp, gated on canary-corpus measurement at
MVP-3.

### What programs are NOT

The program layer is **not** Factory Programs. Specifically:

- **No new agent roles.** No "validator agent process," no
  "decision agent," no "user-testing agent." Program PRs traverse
  the existing L0-L6 gate stack. Decomposition is a planner step,
  not an agent topology.
- **No LLM verdict routing.** Program progression is a
  deterministic Go function over signed `GateResult`s (P1: a
  deterministic gate sits before any AI gate on destructive ops).
  The LLM may *explain* the decision into the audit log; it never
  *makes* the decision.
- **No program-immutability gate ("L0M") at v1.** L0's fixture
  corpus is at **15 / 200** target. Extending L0 to cover program
  artifacts before L0 itself ships green at corpus is the
  "police what you don't have" failure (PRINCIPLES #4). L0M is
  deferred until the L0 corpus contract is met.
- **No bespoke `ModelProvider` abstraction at the program layer.**
  Even the base agent is honestly Claude-only today; cross-vendor
  is the deferred deliverable in §Alternatives (e). The program
  layer reuses whatever cross-vendor story the base layer ships.
- **No new validator process tree.** Where programs need extra
  scrutiny, the existing L3/L4/L5 receive `handoff.json` as
  additional grounding; one **new custom gate** (security) joins
  the stack via the same `gates:` config block any repo already
  uses to add gates.
- **No Postgres / Temporal / Vault dependency at v1.** Reuse the
  base sqlite, reuse the base audit sink, reuse the existing
  spawner. Adopt OSS plumbing (§Alternatives (h)) only when the
  base implementations measurably hurt.

### Architecture

```
WorkItem{kind: program} ─► Planner (one-shot Opus 4.7 call)
                              │
                              ▼ emits program_brief.json (DAG of child WorkItems)
                          Program publish PR
                              │ CODEOWNERS-gated; human merge to `main` (P3)
                              ▼
                          Existing orchestrator
                              │ consumes children like any DAG-ordered work
                              ▼
                          One worker per child (existing AgentSpawner)
                              │ implement → commit → handoff.json (signed)
                              ▼
                          Next worker reads sibling handoff.json
                              │ L0 byte-equality of handoff payload (P3, P10)
                              ▼
                          Child PR → L0-L5 + security custom gate → L6 human
                              │
                              ▼
                          Deterministic Go decision (RouteVerdicts)
                              ├── all pass + heartbeat anchors verified → advance
                              ├── any fail → inject fix-feature → re-spawn worker
                              └── depth exceeded / unverifiable / out-of-scope
                                       uncovered → halt to human
```

The program planner runs **exactly once** per program (one-shot
`program_brief.json` emission, then exit). It is not a long-lived agent
session; there is no planner state to recover from on crash. Program
state lives in `regatta.db` as it does for any `WorkItem`.

### Planner contract

The planner is a Claude (or any configured base-layer agent)
invocation with a templated prompt (`prompts/planner.md`, signed +
SHA-pinned). Input:

- The parent `WorkItem` (`kind: program`)
- The parent's `linked_artifact` (RFC, design doc, MILESTONES.md)
- `AGENTS.md` / `CLAUDE.md`
- The existing L0 immutability rules

Output: `program_brief.json`, written to a `program/<id>` branch and
opened as a **program-publish PR to `main`**. The PR is L0-gated,
CODEOWNERS-gated, and requires a human merge (P3 -- until artifacts
land on `main`, no worker reads them). The planner cannot mutate
`program_brief.json` once merged; re-planning requires a new program
with a `parent_program` link, costing the iteration budget.

Coverage invariant (every child `fulfills` an acceptance criterion
of the parent, and no criterion is double-claimed) is checked
**inline in the planner publish step** -- not as a separate gate.

### Handoff schema

`schemas/handoff.schema.json` (new). The only program-specific
inter-agent artifact. Written by a worker on PR-open; read by the
next worker in the DAG; verified by L0 on every push (the existing
fixture-corpus mechanism -- no L0M needed).

Required fields:

- `feature_id` -- child WorkItem ID
- `base_sha`, `head_sha` -- git anchors
- `commit_shas` -- must be reachable from `head_sha`, not from `base_sha`
- `criteria_addressed` -- assertion IDs ⊆ parent's acceptance criteria
- `commands_run` -- `{cmd, exit_code, stdout_sha}` tuples; the
  orchestrator independently re-runs and HMAC-signs the canonical
  tuple. Worker's claim is *advisory*; mismatch with the
  orchestrator's re-run is a hard fail and audit-sink escalation
  (the "self-report isn't ground truth" lesson -- incident #1).
- `falsifications` -- list of `(hypothesis, mutation_kind, target_invariant,
  citation, canary_archetype_killed, outcome)` tuples. Each must
  carry a citation matching `^(test|file|commit)=\S+$` (same
  discipline as criterion citations); plain counts are not
  accepted. Mutation-kind enum: `null | empty | race | inject |
  overflow | auth_bypass | path_traversal | ...`. At least one
  falsification per claimed `trap_pattern` (P3/P6/P7/P9/P10/...).
- `signature` -- HMAC over canonical JCS serialization, using the
  same orchestrator key that signs `GateResult`. No new key.

### Decision logic (deterministic Go, not LLM)

The program's progression past each child is a Go function in
`internal/program/route.go`:

```go
func RouteVerdicts(child Child, kr KeyRing) Decision {
    for _, r := range child.GateResults {
        if err := Verify(r, kr); err != nil {
            return Decision{Halt: "unverifiable verdict"}
        }
        if r.GateStarted == nil || r.GateFinished == nil {
            return Decision{Halt: "missing heartbeat anchor"}
        }
    }
    if AnyFail(child) {
        return Decision{Iterate, CollectFixFeatures(child)}
    }
    if AnyOutOfScopeUnclaimed(child) {
        return Decision{Halt: "uncovered out-of-scope"}
    }
    if OverDepthCap(child) {
        return Decision{Halt: "depth cap"}
    }
    return Decision{Advance: true}
}
```

Three properties this gives us that an LLM decision agent cannot:

1. **No prompt-injection surface.** The decision logic has no
   prompt. The program-internal `synthesis.json` files cannot
   influence the decision other than via their signed verdicts.
2. **Heartbeat-anchored silence detection.** Missing
   `gate_finished` (audit silent-bypass -- Crit) becomes `halt`,
   not silent advance. Pairs with `regatta audit --reconcile`
   (existing dead-man's-switch, §Failure modes).
3. **No "validator-of-validators" recursion.** P1 applied one
   layer up: deterministic gate before AI gate.

`AnyOutOfScopeUnclaimed` checks the existing `responsibility[]` /
`out_of_scope[]` fields on the gate results (extension to the
existing `GateResult` schema -- additive, no new gate kind).

### Depth caps (typed, enforced in Go spawner)

Program iteration is bounded by a typed cap, enforced in the parent
spawner process (P5 -- out-of-band of the LLM):

- `program_depth.functional ≤ 2` (default; rework iterations for
  pure functional fixes)
- `program_depth.security   ≤ 5` (default; legitimate CVE
  remediation often needs 3-5 rounds: patch → test reveals
  incomplete → widen → regress → clean)
- `program_depth.user_testing ≤ 3` (default)

Per-axis caps prevent one failure class from consuming the budget
of another. Each axis-overshoot requires P2 two-key human approval
to continue; enforcement is in `internal/program/route.go`, not in
the planner prompt.

### Security custom gate (the only new gate)

A new optional gate kind ships in `gates/security/`, added to a
repo's `regatta.yaml` like any other gate:

```yaml
gates:
  - { id: spec_conformance, type: ai, model: claude-opus-4-7,   severity_block: ['fail'] }
  - { id: adversarial,      type: ai, model: claude-sonnet-4-6, severity_block: ['critical', '2*high'] }
  - { id: drift,            type: ai, model: claude-haiku-4-5,  severity_block: ['drift'] }
  - { id: security,         type: hybrid,                       severity_block: ['critical', '2*high'],
      determinism_floor: { gitleaks: true, osv_scanner: true },
      ai_model: claude-opus-4-7,
      ai_mode: adversarial,
      lineage_isolation: cross_family }
```

Determinism floor (no LLM): `gitleaks`, `osv-scanner` (Google) and
optionally `semgrep`, `syft`. Each is shelled out (single binary,
JSON output, version-pinned in `safety.tool_versions`). Failure
short-circuits to a fix-feature; no LLM spend.

AI phase: Opus-class threat-modeling subagent producing a verdict
in the existing `GateResult` shape. Adversarial-default: must emit
falsifications matching the handoff-schema falsification fields.
Lineage isolation is `cross_family` against the worker's binding
(P13). Output is a normal `GateResult` (`gate_kind: ai_adversarial`
already valid); no new schema.

The security AI prompt targets **Regatta's Trap Catalog (P1-P13)**,
not OWASP generic -- commercial code-review tools cover OWASP
already; the program layer's incremental value is incident-pattern
coverage. Skipping commercial AI judges (CodeRabbit/Greptile/Snyk)
also keeps P13: their judge model is opaque and may be same-family
as the worker.

### Subagent / cache discipline

If the planner spawns research subagents (read-only investigation
during planning), they:

- Inherit the planner's binding (cache reuse, lineage consistency).
- Run with no Write/Edit tool (P4 -- read-only ephemeral).
- Cache prefix = `main`-signed text only; untrusted diff or
  external page content lives *after* the cache breakpoint and
  never participates in cache-key formation. Without this,
  prompt-injection in feature-1's diff persists through cached
  prefix into feature-2's subagent (Crit, prevented in-design).

Anthropic cache minimums (verified 2026-05-20):
[1024 tokens Haiku, 2048 Sonnet/Opus](https://platform.claude.com/docs/en/about-claude/pricing).
`regatta validate-config` refuses cache spans under floor.

### Fallback discipline (cross-family only)

When a binding returns `indeterminate` (refusal, structured-output
parse failure, timeout), the orchestrator reroutes *once* to a
fallback binding. P13 is preserved by requiring the fallback to be
**at least as strong** and **cross-family** for adversarial-class
verdicts: `Opus → GPT-5` or `Opus → Sonnet` (never `Opus → Haiku`).
Second indeterminate → halt; any indeterminate where
`injection_suspected: true` → halt immediately.

### State (sqlite, three tables)

```
programs(id, parent_work_item_id, planner_session_id, status,
         depth_functional, depth_security, depth_user_testing,
         created_at)
program_artifacts(program_id, kind, sha, signature, published_at)
program_verdicts(program_id, child_work_item_id, gate_id, verdict,
                 falsifications_json, signature, recorded_at,
                 gate_started_at, gate_finished_at)
```

Everything else (per-child status, handoffs) lives in existing
`work_items` / `gate_results` tables. **No Postgres at v1.** When
the state machine outgrows sqlite, adopt Temporal (§Alternatives
(h)); not before.

### Product capabilities per stage

What an operator can actually do at the end of each stage. Each
row is cumulative -- Stage N implies all capabilities of Stages
earlier than N.

| Stage | Operator can… | Operator cannot… |
|---|---|---|
| **Today (pre-MVP)** | Run `regatta l0 <diff>` on a unified diff and get a deterministic spec-immutability verdict. Run `regatta verify-repo-config -owner -repo` and audit branch protection against the P2 recipe. Print version. | Spawn an agent. Run a gate stack. Decompose a program. Verify a handoff. |
| **MVP-1 (Planner-as-DAG)** | Author a `WorkItem{kind: program}` JSON file. Run `regatta program plan` to one-shot decompose it into a signed, coverage-checked, DAG-validated `program_brief.json`. Audit the planner's output by file (every criterion ID is claimed by exactly one feature; cycles rejected; HMAC verifiable). | Spawn child workers. Open a program-publish PR. Persist program state. Run any security gate beyond L0-L5. |
| **MVP-2 (Tamper-evident handoff)** | Run `regatta program verify-handoff <path>` and get a structural + HMAC verdict on any handoff JSON. Use `internal/program.RouteVerdicts` to make deterministic Go-only advance/iterate/halt decisions over signed gate results. Detect missing heartbeat anchors (silent audit-bypass). Catch worker re-run mismatches (`ReRunMismatch`) on independently-rerun commands. | Run a real worker → handoff → orchestrator round-trip in CI. Inject fix-features back into a live spec adapter. |
| **MVP-3 (Security gate)** | Wire `gates/security` into `regatta.yaml gates:` row. Get a hybrid floor (`gitleaks` + `osv-scanner` + optional `semgrep`/`syft`) plus an AI threat-model against the Trap Catalog. Refuse advance on `injection_suspected: true` or any blocking finding. Enforce cross-family validator-vs-worker lineage at config load. | Run the user-testing gate (deferred). Cross-vendor route through LiteLLM (deferred). Hosted multi-tenant (deferred). |
| **P2.1 (sqlite state)** | Run >1 concurrent program. Survive orchestrator restart with all in-flight programs recoverable from `regatta.db`. Query program/feature/verdict state via SQL. | Survive cross-region failover. Multi-tenant isolation. |
| **P2.2 (publish PR machinery)** | Run `regatta program publish <plan>` and have it open a CODEOWNERS-gated PR to `main` carrying the signed `program_brief.json`. Branch protection refuses merge without 2 distinct human approvals. Workers only spawn after the publish PR merges. | Auto-merge program-publish PRs (forbidden by design; P3). |
| **P2.3 (typed depth caps)** | Configure per-axis caps in `regatta.yaml safety.program` (functional ≤ 2, security ≤ 5, user_testing ≤ 3). Caps enforced in the Go spawner -- the LLM planner cannot raise them. Each axis-overshoot demands a P2 two-key approval. | Allow an LLM to grant itself more iterations. |
| **P2.4 (audit reconciler)** | Run `regatta audit --reconcile` and detect any `gate_started` row without matching `gate_finished` past 30 min × 2. CUSUM-style drift detector on positive count delta. | Silently advance when a validator never wrote its verdict (now hard-halts). |
| **P2.5 (Temporal)** | Sustain ≥30 concurrent programs. Use Temporal workflow history as the second tamper-evident timeline alongside the audit sink. 3-day human approval pauses survive across restart. | Run without Postgres. |
| **P2.6 (Vector audit transport)** | Ship 100 MB+/day of signed audit events to S3 object-lock with backpressure handling and disk-buffered durability. | Sustain ≥1 GB/day without further tuning. |
| **P2.7 (Vault dynamic secrets)** | Issue per-spawn short-lived provider credentials. Long-lived provider keys never present in worker processes. Revoke a spawn's credentials in <60 s via `regatta halt`. | Run without HashiCorp Vault (the abstraction sits at the credential boundary). |
| **P2.8 (LiteLLM proxy for validators)** | Configure a non-Claude binding (GPT-5 / Gemini 2.5 Pro / etc.) for any *validator* role while workers stay on Anthropic direct via `claude --resume`. Family-stratified canary catch-rate live. | Route worker traffic through LiteLLM (cache fidelity loss). |
| **P3.1 (user-testing gate)** | Add a Playwright-CLI-backed user-testing custom gate to `regatta.yaml gates:`. Browser-flow validators produce per-assertion verdicts with screenshot evidence. | Run computer-use-only browser drivers (locks to Anthropic, kills agent-agnostic story). |
| **P3.2 (ModelProvider abstraction)** | Configure planner / worker / validator bindings against any of: anthropic, openai, google-gemini, bedrock, vertex, ollama, openrouter, litellm. Capability floor enforced per role at config load. | Route validator and worker through the same model family (`lineage_isolation: cross_family` default-denies). |
| **P3.3 (cross-vendor L4 benchmark)** | Run the canary corpus through (Opus, Sonnet) vs (Opus, GPT-5) vs (Opus, Gemini) and publish the family-stratified catch-rate delta. | Claim cross-vendor parity without published numbers. |
| **P3.4 (L0M)** | Extend L0 byte-equality to program artifacts (`program.md`, `program_brief.json`, `validation-state.json`, library files). Catch invisible-glyph injection into LLM-authored artifacts. | Ship L0M before the base L0 fixture corpus hits 200 fixtures (currently 15). |
| **P3.5 (hosted multi-tenant)** | Onboard a customer with a `regatta deploy --hosted` flow. Per-tenant key isolation. SLA-backed audit sink. | Run on-prem only. |
| **P3.6 (Postgres backend)** | Sustain program concurrency past sqlite's single-writer ceiling. Use `regatta migrate --to postgres` in-place. | Roll back from Postgres to sqlite (one-way migration). |
| **P3.7 (lineage map strict mode)** | Reject any role mapping that violates the published lineage map at config load. Operators see family-distance per (worker, validator) pair in `regatta validate-config`. | Run with `cross_series` lineage without `--accept-degraded-lineage` and an audit-sink note. |

The MVP-3 acceptance gate (next subsection) decides whether Phase
2 ever begins.

### Phasing (MVP → measure → full product)

The program layer ships in **three MVP milestones to a measurable
go/no-go gate**, then enters **eight conditional adoption phases**
that are triggered only by observed pain (PRINCIPLES #4 --
adopt-when-needed, never on speculation). The base L0-L6 gate
stack remains intact through every milestone.

#### Phase 1 -- MVP (program layer earns the right to exist)

| ID | Title | Scope | LOC | New schemas | Acceptance |
|---|---|---|---|---|---|
| **MVP-1** | Planner-as-DAG | `WorkItem{kind: program}` → planner one-shot → `program_brief.json` → existing orchestrator consumes a DAG of children. Anthropic-direct ModelClient. Coverage + DAG validators. HMAC-signed plan. CLI: `regatta program plan`. | ~600 | `program_brief.schema.json` | One program produces ≥3 child PRs through unmodified L0-L6 |
| **MVP-2** | Tamper-evident handoff | `handoff.json` signed + schema'd + falsification-citation discipline. `RouteVerdicts` deterministic Go decision function. CLI: `regatta program verify-handoff`. Reuses existing L0 fixture infrastructure (no L0M). | ~600 | `handoff.schema.json` | +6 handoff fixtures pass/fail corpus; deterministic decision function covers 7 routing cases |
| **MVP-3** | Security custom gate | `gitleaks` + `osv-scanner` deterministic floor + Trap-Catalog AI threat-modeler. Wired via `regatta.yaml gates:` row. Cross-family lineage enforced at config-load. 3 new canary archetypes (planner-glyph, fabricated-handoff, fallback-coercion). | ~500 | none (extends `GateResult`) | Net-helpfulness ≥+5 pp over single-agent on canary corpus |

**MVP-3 gate (go/no-go).** Measure on the extended canary corpus.
SWE-bench's published 2026 decomposition delta is ~6 pp (Opus 4.5:
45.9% → 51.8% with better scaffolding); we set the bar one point
below as our acceptance threshold.

- **If MVP-3 misses the bar:** kill the program concept, document
  the empirical result, ship base Regatta. PRINCIPLES #4 -- burden
  of proof on the proposer.
- **If MVP-3 clears the bar:** enter Phase 2.

#### Phase 2 -- adopt-when-needed (OSS plumbing as pain appears)

Each phase has a documented **trigger metric**. Adoption begins
only after the trigger fires for two consecutive measurement
windows. No speculative adoption.

| ID | Adopt | Trigger metric | Acceptance |
|---|---|---|---|
| **P2.1** | Program state in sqlite (3 tables per §State) | First program concurrency >1 | All in-flight programs recoverable across orchestrator restart |
| **P2.2** | Program-publish PR machinery + CODEOWNERS gate | First non-toy program (≥5 features) | Publish PR refuses to merge without 2 distinct human approvals matching `verify-repo-config` recipe |
| **P2.3** | Per-axis depth caps enforced in Go spawner | First fix-feature loop >3 rounds in production | `program_depth.{functional,security,user_testing}` enforced before LLM ever sees a verdict |
| **P2.4** | Audit reconciler (`regatta audit --reconcile`) | First missing-`gate_finished` heartbeat observed | Silent-bypass = halt, not advance. CUSUM detector live. |
| **P2.5** | Temporal OSS migration | sqlite contention >5 % of program writes OR ≥30 concurrent programs | Program state machine + crash recovery off the daemon; existing 3-table model fits Temporal workflow history |
| **P2.6** | Vector audit transport | Audit S3 backpressure events ≥1/week OR audit volume >100 MB/day | HMAC stays in Regatta; Vector handles batching + retry + disk-buffered durability |
| **P2.7** | Vault dynamic secrets | First enterprise procurement ask OR per-spawn token rotation ≥10×/day | Long-lived provider keys never present in worker process; revocation lag <60 s |
| **P2.8** | LiteLLM proxy for validators (workers stay `claude --resume`) | Second paying customer with a non-Claude validator binding | Cross-family validator/worker pair shipped end-to-end on canary corpus |

#### Phase 3 -- full product

Phase 3 work ships only after Phase 2 reveals which pieces are
demanded; designing them in advance is YAGNI. Likely candidates if
demand materializes:

| ID | Title | Conditional on |
|---|---|---|
| **P3.1** | User-testing custom gate via Playwright CLI | Customer with browser-served product asks. Microsoft's 2026 recommendation is CLI over MCP for coding agents (4× fewer tokens/session). |
| **P3.2** | ModelProvider abstraction (cross-vendor) | Second paying customer + Phase 2 P2.8 in production |
| **P3.3** | Cross-vendor L4 benchmark + family-stratified catch-rate | Phase 3 P3.2 lands |
| **P3.4** | L0M (program-immutability extension) | L0's base fixture corpus reaches 200-fixture target (currently 15) AND first program-artifact tampering attempt observed |
| **P3.5** | Hosted multi-tenant service | Self-host customer count ≥10 AND ≥3 ask for managed |
| **P3.6** | Postgres backend swap | Phase 2 P2.5 lands AND program concurrency justifies it |
| **P3.7** | Lineage map + `regatta validate-config` strict mode | Second model family adopted AND Preference Leakage measurement on canary corpus |

Phases 2 and 3 do **not** form a critical-path Gantt; they are
parallel demand streams. Pick by the trigger that fired, not by
the order they appear here.

#### What ships before Phase 1 begins (already done at this PR)

- §Programs design doc (this section) and §Alternatives (h)
  adopt-when-needed table.
- `schemas/handoff.schema.json` + `schemas/program_brief.schema.json`
  + `schemas/sign.go` (shared HMAC, no new key per artifact type).
- `internal/program/` package: `route.go` (deterministic verdict
  router), `handoff.go` (load/validate/coverage/re-run-mismatch),
  `planner.go` (one-shot pipeline + Validate + Sign), and
  `provider_anthropic.go` (single ModelClient implementation).
- `internal/securitygate/gate.go` skeleton (floor wired, AI stub
  pointing at unwritten `prompts/security_gate.md`).
- CLI: `regatta program plan` and `regatta program verify-handoff`.
- Fixtures: 6 handoff fixtures (3 pass, 3 fail) + 3 canary
  archetypes.

Test discipline: 62 tests pass across 5 packages. New code
ships with table-driven contract tests against the fixture corpus.

### What's load-bearing (cannot be cut without losing the thesis)

1. **Worker isolation per feature** (own session, fresh context).
   Already true in single-agent flow.
2. **Tamper-evident handoff** (signed, schema-validated). The
   recursive-taint defense at program scope.
3. **Human approval on program spawn** (L6-equivalent via branch
   protection on the program-publish PR). Inherits from base
   thesis (§Gate stack).
4. **Reuse base audit sink + signing key.** No fork of the
   tamper-evident pipeline.
5. **Depth enforced in Go spawner**, not LLM planner.

Everything else -- typed verdicts, falsification-quality lint,
security custom gate, planner subagents -- is configurable per repo
and removable without breaking the thesis.

## Day 1 → Day 30 Runbook

**Pre-implementation banner.** Regatta is not shipped. Commands below
describe the v1 release surface. The schemas + this runbook are the
contract; the binary follows.

### Day 1 -- install and validate

```sh
$ brew install trilamsr/regatta/regatta             # or `go install …`
$ cd ~/code/myproject
$ regatta init                                       # writes regatta.yaml skeleton
$ $EDITOR regatta.yaml                               # fill in adapter, ci.command, lanes
$ regatta validate-config                            # CUE-validates regatta.yaml
$ regatta validate-spec --dry-run                    # connects to adapter, lists items
$ regatta verify-repo-config                         # audits branch protection + CODEOWNERS
```

Expected: a parsed-items count, NFC + invisible-glyph cleanliness
report, DAG verification, and the list of ready-to-spawn item IDs.
`verify-repo-config` is mandatory before the first `regatta serve` --
several silent-bypass classes (admins-bypass-by-default,
CODEOWNERS-pattern-matches-nothing, SKIPPED-required-check satisfies
branch protection, `required_approving_review_count: 0` is a no-op,
CODEOWNERS file >3 MB is silently ignored, missing
`require_last_push_approval` enables Mercari-class PR hijacking) are
not surfaced by GitHub itself and would otherwise defeat L6 in
production. The audit checks the P2 canonical recipe
(`required_approving_review_count: 2`, `require_code_owner_reviews`,
`require_last_push_approval`, `dismiss_stale_reviews`,
`enforce_admins: true`, with Regatta-critical paths routed to two
disjoint CODEOWNERS teams) and refuses to start if any check fails
unless `--accept-degraded` is passed and the gap is logged to the
audit sink.

### Day 2 -- calibrate the gates

```sh
$ regatta gate-calibrate --pr 95,87,79              # 3 already-merged PRs
$ regatta gate-calibrate --canary-corpus            # all 8 archetypes
```

`gate-calibrate` runs L0-L5 against the chosen PRs and emits a
per-gate confusion matrix. The 8 canary archetypes (see
[`testdata/gates/canary/README.md`](../testdata/gates/canary/README.md))
have known-expected verdicts. Tune `gates[*].severity_block` until
calibration is clean (typical fixes: raise L4 threshold to `critical`
only when over-cautious; add path filters when L5 false-drifts on
auto-generated files).

### Day 3 -- single pilot PR (human-spawned)

```sh
$ regatta pilot --work-item 101 --no-orchestrator --interactive
```

Spawns one agent in `.regatta/worktrees/work-101-<slug>/`; you watch
it live. Once the PR opens:

```sh
$ regatta gate-run --pr 256                          # runs L0-L5; posts comments
```

If gates accept and a synthetic-bad variant rejects, you're ready for
Day 7.

### Day 7 -- orchestrator on, one lane

```sh
$ regatta serve --lane server --max-concurrency 1
```

Watches one lane only; spawns one agent at a time. Runs ~5-10 items
in the first week. Monitor:

```sh
$ regatta status                                     # one-line per agent + last gate
$ regatta digest --since 1w                          # markdown digest
$ regatta canary-report                              # catch-rate + recent canary PRs
```

### Day 30 -- all lanes, concurrency 1 each

```sh
$ regatta serve                                      # all lanes from regatta.yaml
```

Promotion criteria for raising any lane to concurrency 2:

- ≥20 PRs merged in the lane via fleet
- Canary human-catch-rate ≥85% over rolling 20-canary window
- Net-helpfulness ≥70% (see [§Test harness](#test-harness))

## Worked Example (simulated)

A Node.js backend repo `acme/api`. Work item #117:

> **#117 -- Add request-ID propagation to upstream HTTP calls.** Currently `axios` calls strip the `X-Request-ID` header on outbound retries.
>
> ## Acceptance
> - [ ] Outbound `axios` calls preserve incoming `X-Request-ID` on all retries.
> - [ ] A new integration test fails before the fix and passes after.
> - [ ] `CHANGELOG.md` entry under `[Unreleased] / Fixed`.

The orchestrator picks #117 (no dependencies, lane `server`). Spawns
agent into `.regatta/worktrees/work-117-request-id/`. The agent reads
the item, writes a failing test (`test/integration/request-id.test.js`),
fixes `src/server/http-client.js`, runs `npm test`, updates
`CHANGELOG.md`, and flips the three criteria with citations:

- `state: done, citation: test=request-id retries preserve header`
- `state: done, citation: test=request-id retries preserve header`
- `state: done, citation: file=CHANGELOG.md:14`

It opens PR #259 with a citation block. The orchestrator runs gates and
posts five `GateResult` comments (full schema:
[`gate_result.schema.json`](../schemas/gate_result.schema.json)):

```jsonc
L0  pass   findings=[]                              duration=34ms
L1  pass   findings=[]                              duration=118s
L3  pass   3× info findings, each citing test/file  $0.140  Opus 4.7
L4  pass   1× low finding (mock-vs-interceptor)     $0.110  Sonnet 4.6
L5  pass   findings=[]                              $0.014  Haiku 4.5
```

Gate spend **~$0.27**, agent spend over 4 iterations **~$0.88**,
PR-total **~$1.15**. Maintainer reads the comments in ~3 min, clicks
Merge. Reaper tears down the worktree and kicks the scheduler.

## Test harness

The doc separates audit-tier by gate kind. **Deterministic gates** are
auditable: their fixture corpus is the contract. **AI gates** are
statistically auditable only: live-replay + drift alerting.

**Deterministic-tier.** L0/L1/L2/L6 + custom deterministic gates ship
with a fixture corpus. The L0 corpus at
[`testdata/gates/l0/`](../testdata/gates/l0/) is the v1 contract
(target: 200 fixtures by v1.0). CI runs the full corpus on every
commit.

**Statistical-tier.** L3/L4/L5 + AI custom gates audit via three
mechanisms: a **golden corpus** of ≥30 historical PRs with
maintainer-recorded verdicts, replayed weekly with ≥95% agreement
required to ship a new prompt or model; **VCR fixtures** for
deterministic unit-test replay; and the **canary catch-rate** from
`testdata/gates/canary/` -- 18 known-bad archetypes that must each be
caught at the expected layer. The 30-PR golden floor is adequate for
gross-regression detection only (Wilson 95% CI on 28/30 = [78.7%,
98.2%]); target 200 PRs over 6 months, with a CUSUM-style drift
detector on the weekly agreement series.

Two findings from the LLM-judge literature shape canary design.
**Style/length bias is ~10× larger than same-family bias** (0.76-0.92
across four vendor families on RewardBench 2; same-family bias is
~9% per Preference Leakage 2026), so canary archetypes must exercise
verbosity, formatting tricks, and citation-faking -- not just
correctness defects. **The Opus 4 agentic-misalignment study shows
a 55.1% real-context vs 6.5% eval-context behavior gap** -- judges
behave very differently when they detect eval scaffolding, so canary
PRs must be indistinguishable from real PRs (no test-only repos, no
distinctive author bots, no eval-shaped prompts in agent context).

### Metrics with precise definitions

- **Gate-catch-rate** = (PRs where ≥1 blocking gate fires) / (PRs
  total). Alert threshold: <99% over rolling 50 PRs. Low value means
  gates are letting everything through.
- **Human-catch-rate** = (canary PRs L6 rejected) / (canary PRs that
  reached L6). Alert threshold: <80% over rolling 20 canaries. Low
  value is the 737-MAX rubber-stamp signal -- humans trust the gates
  too much.
- **Net-helpfulness per AI gate** = (gate rejections that maintainer
  also rejected at L6) / (total gate rejections). Computed on a
  rolling 10-PR window per gate. Ground truth: the maintainer's
  merge decision, *not* downstream bug rate (too slow, too noisy).
  Action at threshold: `<70%` → raise severity threshold by one
  level; `<50%` → swap model or revise prompt.

## Costed reference workload (simulated)

10-engineer team, Node.js repo, 15 PRs/week average, 2026-05-20 list
prices (Opus 4.7 $5 in / $25 out, Sonnet 4.6 $3 / $15, Haiku 4.5 $1 / $5
per MTok; 5m cache write 1.25×, read 0.1×), mandatory prompt caching:

| Component | Per PR | Per week | Per month |
|---|---|---|---|
| Agent (Opus 4.7, ~4 iterations, cached) | $0.88 | $13.20 | $57.20 |
| L3 (Opus 4.7) | $0.14 | $2.10 | $9.10 |
| L4 (Sonnet 4.6) | $0.11 | $1.65 | $7.15 |
| L5 (Haiku 4.5) | $0.014 | $0.21 | $0.91 |
| Custom gates (avg) | $0.05 | $0.75 | $3.25 |
| **Total (×1.2 tokenizer)** | **~$1.38** | **~$21.52** | **~$93.19** |

(Bottom row applies the +1.2× Opus 4.7 tokenizer multiplier verified
2026-05-20 -- see landmine note below. The unmultiplied subtotal is
~$1.15/PR; the bill is ~$1.38/PR.)

### Worked example: 4-feature program at MVP-3

A program-decomposed PR set with one planner one-shot + four child PRs
each going through the L0-L6 stack + the security custom gate (issue
#47):

| Component | Per program |
|---|---|
| Planner one-shot (Opus 4.7) | ~$0.12 |
| 4 × child (L0-L5, cached agent + gates) at $1.15 each | ~$4.60 |
| 4 × security custom gate (gitleaks + osv floor + Opus AI phase) at ~$0.20 each | ~$0.80 |
| 4 × handoff verify (orchestrator-side re-run) at ~$0.02 each | ~$0.08 |
| **Subtotal** | **~$5.60** |
| **× 1.2 Opus 4.7 tokenizer** | **~$6.72** |

A 10-feature program runs ~$15-17 fully-loaded; a 16-day Slack-class
program (40 features, 5-round security iteration cap) projects
~$220-280 at healthy cache hit-rate. The MVP-3 acceptance bar (≥+5pp
net-helpfulness over single-agent baseline on the canary corpus)
must clear at this cost envelope or the program layer is killed per
\`§Stop conditions\`.

Two cost-risk landmines: **Opus 4.7 ships a new tokenizer** producing
up to 35% more tokens per text than its predecessor (verified
2026-05-20; the +1.2× multiplier is the empirical mean, not the
worst case). **Prompt-caching is load-bearing**: Claude Code's
March 2026 cache regression dropped hit rate to 4-17% and inflated
cost 10-20×; Regatta must halt the fleet if rolling
\`cache_hit_rate < 30%\`. The minimum cacheable block on current
Opus / Haiku is 4,096 tokens -- L5 prompts under that silently
bypass caching with no error.

For comparison (mid-2026 list pricing, *unverified*, fully-loaded
on a 10-eng team at 15 PRs/week): Devin Team + CodeRabbit ~$50/PR;
Copilot Business + Greptile + ad-hoc agent ~$40/PR; Regatta self-host
+ Anthropic API + Claude Code seats ~$70/PR-equivalent. Regatta's
positioning is **not the cheapest** -- it's the only one with a
deterministic immutability gate, a published trap catalog, and a
tamper-evident audit log out-of-band.

## Failure modes (on-call runbook)

Each row: detection signal → first command → second command →
escalation.

| Failure | Detect | First | Second | Escalate |
|---|---|---|---|---|
| Agent can't pass CI after K iters | session.iteration_count ≥ cap | `regatta logs --work-id N` | `regatta diff --work-id N` | draft PR + flag human |
| AI reviewer rejects K=3 times | gate_runs.rejection_count ≥ 3 | `regatta gate-history --pr N` | `regatta agent-prompt --pr N` | label `needs-human` |
| Agent gaslights L3 (vacuous test) | L4 finding + canary catch dip | `regatta gate-comment l4 --pr N` | `regatta canary-report --since 1d` | revise L4 prompt |
| Agent edits acceptance criterion | **L0 fails hard** | (auto-blocked) | `regatta gate-comment l0 --pr N` | review agent log |
| L4 over-cautious -- rejects everything | net-helpfulness <50% rolling 10 | `regatta net-helpfulness l4` | bump severity threshold | swap model |
| Hallucinated package install | L1 lockfile mismatch + P1 deny | `regatta diff package.json --pr N` | `pip index versions <pkg>` | block, comment |
| L4 prompt-injected via diff | `injection_suspected: true` in L4 | `regatta gate-result l4 --pr N --raw` | human review of diff | rotate signing key |
| Recursive lesson taint | CODEOWNERS blocks merge | (branch-protected) | review lesson PR | maintainer rejects |
| Token budget burned | telemetry.cost_usd ≥ cap | `regatta status --over-budget` | `regatta halt --work-id N` | summarize, alert |
| Prompt-cache hit rate collapsed | rolling cache_hit_rate <30% | `regatta cache-stats` | `regatta serve --pause` | halt fleet; inspect prompt structure / TTL |
| 737-MAX rubber-stamp drift | canary human-catch-rate <80% | `regatta canary-report` | `regatta canary-recent` | pause merges; review |
| Spec mutated on `main` mid-flight | L0 merge-time re-run fails | `regatta gate-rerun l0 --pr N` | rebase + re-evaluate | usually agent halts cleanly |
| Long autonomous trajectory (GTG-1002) | tool-call sequence anomaly | `regatta agent-trace --work-id N` | pause | require fresh approval |
| Force-push or destructive git op | P1 deny + P2 two-key | (auto-rejected) | `regatta audit --since 1h` | audit sink entry |
| Orchestrator broken / API outage | heartbeat dead-man / gate timeouts | `regatta health` | `regatta serve --pause` | restart; running agents reaped on next start |
| Validator-binary supply-chain (Shai-Hulud) | SLSA L3+ provenance mismatch on Regatta release artifact | `regatta version --provenance` | refuse to launch on mismatch | rotate release keys |
| Audit-sink unavailable | S3 PUT 5xx ≥30 s OR ≥3 consecutive backpressure events | `regatta audit --sink-status` | **gate fails CLOSED** (advance blocked) | escalate to operator; do NOT advance on absence |
| Secrets in audit log | denylist regex hits in gate-result payload pre-sign | `regatta audit --scan-secrets --since 1h` | rotate exposed credential | block release of affected audit window |
| Cache-poisoning across work items | cache hit on prefix whose signed-prompt-pack SHA differs from current | `regatta cache-stats --by-pack` | flush cache scope | halt; review prompt-pack history |
| Canary corpus leaked into model training | family-stratified catch-rate drops ≥10pp without prompt change | `regatta canary-report --family-strata` | rotate canary archetypes from private holdout | freeze published catalog; refresh |
| DAG composition laundering | per-feature L4 pass but whole-`program_brief` L4 fails | `regatta gate-rerun l4 --program <id>` | reject brief; require re-plan | escalate to human |
| Bot-account CODEOWNERS bypass | `verify-repo-config` enumerates bot satisfiers | (auto-blocked at config-load) | `regatta verify-repo-config --enumerate-bots` | remove bot from CODEOWNERS |
| Heartbeat-anchor forgery (worker self-attests) | anchor signature key_id == worker session key | (auto-rejected at route-time) | `regatta gate-rerun --pr N` from sibling sandbox | rotate per-spawn keys |

## Stop conditions (measurable)

Abandon a deployment if **any** triggers: canary human-catch-rate <60%
over rolling 20-canary window for ≥4 weeks post-Phase-3; maintainer
review-burden median time-on-PR has not dropped vs. pre-Regatta
baseline by week 6 (measured via `gh pr view --json
closedAt,createdAt`); agent completion rate <60% across rolling 10-PR
window for ≥2 consecutive windows; or cost per completed item exceeds
`safety.spend_cap_usd` consistently for 3+ items in a row.

**Program-layer stop conditions** (additional, see §Programs):

- **MVP-3 acceptance bar miss.** Net-helpfulness improvement of
  program-decomposed flow over single-agent baseline on the canary
  corpus is < +5 pp after 20 rolling programs. → Kill the program
  concept; remove `Programs` section from this doc; ship base
  Regatta.
- **Handoff fabrication rate ≥ 1%.** Worker `handoff.json`
  `commands_run.exit_code` mismatch with the orchestrator's
  independent CI re-run (incident #1 pattern) over rolling 100
  handoffs. → Halt all programs; audit-sink escalation; planner
  prompt review.
- **Missing `gate_finished` heartbeat ≥ 1.** Any silent-bypass
  detected by `regatta audit --reconcile` is a hard failure of the
  P5 dead-man's-switch. → Halt all programs; investigate
  orchestrator integrity.
- **Cross-family fallback collapse.** Indeterminate verdict on the
  primary binding routes to a same-family fallback because no
  cross-family fallback is configured. → Refuse to advance; require
  operator to declare cross-family fallback in `regatta.yaml` before
  resuming.
- **Program-publish PR auto-merge.** Any program-publish PR merges
  to `main` without ≥2 human approvals matching the CODEOWNERS
  recipe enforced by `verify-repo-config`. → Halt the deployment;
  this is a P3 (trusted-instructions-from-main) violation.
- **Audit sink unavailable ≥ `T_sink_timeout` (default 30 s).** Any
  gate whose signed result cannot be persisted to the immutable
  sink halts the work item. Absence is never advance. The HMAC key
  history is externalized to the same trust boundary as the sink
  credential, not co-located with `regatta.db`.
- **Worker session without heartbeat trio.** Every spawned agent
  MUST emit signed `worker_started`, `worker_alive` (every 60 s),
  `worker_exited` records to the audit sink. A session that ends
  without `worker_exited` past 30 min × 2 → halt; treat as
  silent-bypass.
- **TOCTOU between L0 verdict and worker read.** Workers read spec
  artifacts from a content-addressed (hash-pinned) snapshot of
  `main@SHA`, not from working-tree files. Hash is pinned in the
  worker invocation; mismatch at read time → halt.
- **DAG composition fail despite per-feature pass.** L4 on the
  whole `program_brief.json` fails after all per-feature L4 passes
  → halt; require human review of the brief. Coverage invariant
  did not catch composition-safety.
- **Bot-account CODEOWNERS bypass detected.** `verify-repo-config`
  enumerates every account whose approval satisfies a CODEOWNERS
  rule; any bot or service account present → refuse to start until
  removed.

## SLOs (operator-visible operations)

p50 / p95 latency budgets per operator-visible operation. The
`alarm` column is what `regatta gate-calibrate` (issue #54) fails
on. Numbers are projection from the cost-section assumptions
(cached agent, Opus 4.7, mid-2026 prices); production data will
calibrate.

| Operation | p50 | p95 | Alarm |
|---|---|---|---|
| L0 verdict (deterministic) | <1s | <5s | >10s |
| L1 CI gate (repo's own command) | <30s | <2m | >5m |
| L3 spec-conformance (Opus 4.7, cached) | <2m | <5m | >10m |
| L4 adversarial (Sonnet 4.6, cached) | <2m | <5m | >10m |
| L5 drift (Haiku 4.5) | <30s | <90s | >3m |
| Security custom gate (floor + AI) | <90s | <4m | >8m |
| Planner one-shot (Opus 4.7) | <30s | <2m | >5m |
| Handoff verify (HMAC + schema) | <100ms | <500ms | >2s |
| Program-publish PR open | <10s | <30s | >2m |
| Worker spawn -> first commit | <2m | <5m | >10m |

Wire-up: `regatta gate-calibrate --fail-over=5m` (issue #54) walks
each row, runs the operation against a canary fixture, and fails
the run if any row's observed p95 exceeds the alarm threshold.
Calibration tooling sits at issue #54; the SLO contract is the
table above.

Mission-class workloads (16-day Slack-class programs) explicitly
relax the program-publish SLO to <10m p95 because they are
multi-day workflows; the per-gate SLOs stay tight regardless of
program duration.

## Alternatives

**(a) No orchestrator, single-shot agent.** Accepted as Day 1 mode in
the runbook; rejected as steady state.

**(b) Humans curate an agent-ready subset.** Rejected -- the spec is
the oracle. Curation moves the bottleneck.

**(c) One big agent that does everything.** Rejected -- no parallelism;
self-review is the rubber-stamp failure mode (P8 + 737-MAX class).

**(d) Reviewer ensemble (3 agents, majority).** Deferred -- single
adversarial + mixed family closes same-family bias at 1× cost. Revisit
if false-pass rate justifies 3×.

**(e) Swap Claude for another runtime.** *Honest concession.* Ships
Claude only today. The cross-vendor adapter (Codex, Gemini,
open-source) is the single most load-bearing deferred deliverable.
Until it ships, treat "runtime-agnostic" as a thesis, not a feature.
Pairs with the next point.

**(f) "Different family" claim at L4.** Sonnet 4.6 and Opus 4.7 are
sibling Claude-4.x checkpoints -- same vendor, same generation, different
post-training. The published proxy measurement closest to this pair is
GPT-4o judging GPT-4-turbo output, which inflates win-rate by **8.9%**
(Li et al., "Preference Leakage," ICLR 2026, Table 2). The "same-model"
case (Opus judging Opus) inflates by 23.6% in the same study, and
self-preference per Panickssery et al. (NeurIPS 2024) is 0.71-0.91 on
summarization pairs for strong models. Sonnet→Opus is therefore
meaningfully better than Opus→Opus -- by roughly the same factor (2-3×)
that family-bias is smaller than self-preference bias -- but is *not*
equivalent to cross-vendor judging, which the same study measures at
2.8% (different series within a family) and which cross-vendor work
(Cohere PoLL, arXiv 2404.18796) measures at variance reductions of ~64%
over a single large judge.

| Configuration | Win-rate inflation | Source |
|---|---|---|
| Opus judges Opus | ~20-50% | Panickssery '24, Wataoka '24 |
| Sonnet 4.6 judges Opus 4.7 (proxy: GPT-4o↔GPT-4-turbo) | ~9% | Preference Leakage '26 |
| GPT-5 judges Opus (cross-vendor proxy) | ~3% | Preference Leakage '26 |
| Three-judge cross-vendor jury | smallest variance (σ=2.2 vs 6.1) | PoLL '24 |

This design captures ~60% of the bias-reduction available; a
cross-vendor judge closes the remaining ~6 percentage points.
Style/length bias (0.76-0.92 across four vendor families per
RewardBench 2) and prompt-injection attack surface (JudgeDeceiver:
89% ASR on open-source judges) are *larger* design risks than the
residual same-family delta. Ship with Sonnet 4.6 today; the
L4-vs-cross-vendor comparison is deferred behind a documented
canary-corpus delta as the acceptance criterion -- a family-stratified
catch-rate ratio `< 0.85` triggers cross-vendor L4.

**(g) Vendor the gate stack into each repo.** Rejected -- keeps
Regatta independently versionable; lets one deployment serve many
repos; avoids forking gate logic per consumer.

**(h) Adopt-When-Needed OSS plumbing (program-layer trigger).**
Hand-rolled state machine, audit transport, credential plumbing,
multi-provider routing, and browser automation are explicitly
deferred to OSS adoption -- each gated on a measured trigger
out of the program-layer MVP-3 phase (see §Programs). Component
mapping:

| Trigger | Adopt | Notes |
|---|---|---|
| State machine outgrows sqlite (≥30 concurrent programs, or a documented schema-rewrite forcing function) | **Temporal OSS** 1.25+ via Go SDK | Real cost: 3 services + Postgres + metrics + workers; ~$480-610/mo small deploy; 37 tables; 145 SQL/3-activity workflow. Requires SRE capability. Defer hard until base sqlite hurts. |
| Audit volume >100 MB/day OR S3 backpressure under load | **Vector** (Rust, single binary, S3 object-lock sink, OTLP input, disk-buffered durability) | Keep HMAC signing in Regatta; Vector handles transport only. |
| Customer asks enterprise procurement story | **HashiCorp Vault OSS** + dynamic secrets engine | Maps to P4. Defer until first regulated-customer ask. |
| Second paying customer asks non-Claude provider | **LiteLLM** proxy (Python sidecar) for **validators only**; workers stay `claude --resume` to preserve `cache_control` placement | LiteLLM strips Anthropic cache annotations on some routes -- verified failure 2025. Validators tolerate; workers don't. |
| Customer asks browser-based validation | **Playwright CLI** (not Playwright MCP -- Microsoft recommends CLI for coding agents as of 2026, 4× fewer tokens/session) | Persistent profile auth in `ms-playwright/mcp-{channel}-{workspace-hash}`. |
| MVP-3 net-helpfulness < +5pp vs single-agent baseline on canary corpus | **None -- kill the program concept** | PRINCIPLES #4 acceptance criterion. Document the empirical result; ship base Regatta. |

Rejected adoptions:

- **LangGraph / CrewAI / OpenAI Agents SDK / Anthropic Agent SDK
  as the worker runtime.** Polyglot tax (Python) and embedded SDK
  pattern drag the agent runtime into Regatta's address space,
  losing cgroup-enforced supervisor limits (P5) and the
  per-binary-swap agent-agnostic story. Keep CLI shellout.
- **CodeRabbit / Greptile / Snyk as the security gate AI phase.**
  Their judge models are opaque (P13 violation surface); they
  target OWASP-generic, not Regatta's Trap Catalog (P1-P13).
  Use their underlying OSS tools (gitleaks, semgrep) directly;
  keep the AI phase in-house.

## Competitor positioning

| | Regatta | Devin 2.0 | Cursor 3 + BugBot | Copilot Workspace + Coding Agent | Claude Code Agent Teams | OpenHands | CodeRabbit | Gitar |
|---|---|---|---|---|---|---|---|---|
| **Pricing** | Self-host + API at cost | $500/mo + ACUs | $40/seat/mo + usage | $39/seat/mo + usage | API at cost | OSS + API | $24/dev/mo | (stealth, $9M) |
| **Gate transparency** | open, configurable | proprietary | proprietary | proprietary | open via Code | open | semi-open | semi-open |
| **Runtime-agnostic** | thesis (deferred) | no | no | no | Claude only | yes | model-agnostic | yes |
| **Published incident defenses** | yes (13-pattern Trap Catalog) | partial | partial | partial | partial | no | partial | partial |
| **Deterministic spec-immutability gate** | **yes (L0)** | no | no | no | no | no | no | no |
| **Tamper-evident audit log out-of-band** | yes | no | no | no | no | no | no | no |
| **Canary-PR injection** | yes (8 archetypes ship) | no | partial (BugBot self-test) | no | no | no | no | no |
| **SWE-bench-style benchmark** | not yet | yes | yes | yes | yes | yes | no | no |
| **Cryptographically reproducible PR verdicts** (`regatta verify-pr <url>` offline, six months later) | **yes** (HMAC over canonical JSON; signed audit) | no | no | no | no | no | no | no |
| **Fixture-corpus-as-contract** (customer-authored counterexample is binding spec) | **yes** (`testdata/gates/l0/`, `testdata/program/handoffs/`) | no | no | no | no | no | no | no |
| **Worker re-run mismatch** (orchestrator-independent CI re-run defeats incident-#1 by construction) | **yes** (`Handoff.ReRunMismatch`) | no | no | no | no | no | no | no |
| **Deterministic Go progression** (no LLM router, structurally no prompt-injection on the routing path) | **yes** (`RouteVerdicts` is a Go switch) | LLM router | LLM router | LLM router | LLM router | LLM router | n/a | n/a |
| **Per-axis depth caps as enforced integers** (functional ≤ 2 / security ≤ 5 / user_testing ≤ 3, audit-visible) | **yes** | prose | prose | prose | prose | prose | prose | prose |
| **Heartbeat-anchored silent-bypass detection** (missing `gate_finished` = halt) | **yes** (`regatta audit --reconcile` + CUSUM) | no | no | no | no | no | no | no |

The nine defensible differentiators, in publishability order: **(1)
deterministic L0**, **(2) out-of-band audit**, **(3) published canary
archetype corpus**, **(4) cryptographically reproducible PR
verdicts**, **(5) fixture-corpus-as-contract**, **(6) worker re-run
mismatch**, **(7) deterministic Go progression**, **(8) per-axis
depth caps as integers**, **(9) heartbeat-anchored silent-bypass**.
(1), (3), (4), and (5) are publishable as standalone OSS primitives
even if the rest of Regatta never ships -- they raise the floor for
every other agent vendor by their mere existence.

### Wedge evidence (code or fixture)

Each row above asserts Regatta has a wedge a named competitor does
not. Below: the code path or fixture in this repository that proves
the wedge ships, not just exists in prose.

| # | Wedge | Evidence |
|---|---|---|
| 1 | Deterministic spec-immutability gate (L0) | `internal/l0/gate.go::Check`; corpus `testdata/gates/l0/{pass,fail,edge}/` |
| 2 | Tamper-evident audit log out-of-band | `schemas/sign.go::Sign` / `Verify`; HMAC chokepoint test `schemas/sign_test.go::TestVerifyDetectsTamper` |
| 3 | Published canary archetype corpus | `testdata/gates/canary/program_archetypes.ndjson` (3 archetypes); base corpus catalog in `testdata/gates/canary/` |
| 4 | Cryptographically reproducible PR verdicts | `schemas/gate_result.go::SignatureBlock`; round-trip lockstep `schemas/gate_result_test.go::TestGateResultSchemaLockstep`; HMAC verify `schemas/sign.go::Verify` |
| 5 | Fixture-corpus-as-contract | `internal/l0/fixture_test.go::TestL0Fixtures`; program-handoff corpus `internal/program/corpus_test.go::TestHandoffCorpus` |
| 6 | Worker re-run mismatch (defeats incident #1) | `internal/program/handoff.go::Handoff.ReRunMismatch`; covered by `internal/program/handoff_test.go::TestReRunMismatch_*` (match, exit-code mismatch, length mismatch) |
| 7 | Deterministic Go progression | `internal/program/route.go::RouteVerdicts` (no LLM call site in the package); 13 routing tests in `internal/program/route_test.go::TestRouteVerdicts` |
| 8 | Per-axis depth caps as enforced integers | `internal/program/route.go::Depth.Over`; defaults `DefaultDepthCap`; tests `route_test.go::TestRouteVerdicts/depth_*` |
| 9 | Heartbeat-anchored silent-bypass detection | `internal/program/route.go::RouteVerdicts` heartbeat check (`v.StartedAt == nil \|\| v.FinishedAt == nil`); test `route_test.go::TestRouteVerdicts/missing_heartbeat_halts` |

Issue #53 tracks the date-stamped competitor confirmation
research (`research/13-wedge-confirmation.md`); until that lands,
the right-hand columns of the main table are author-asserted, not
peer-reviewed.

## Open questions

1. **In-progress claim convention.** A work item marked
   `in_progress` may be (a) abandoned, (b) human-worked,
   (c) regatta-worked. Default: leave alone for 14 days; after
   staleness *and* no matching `regatta/*` branch on origin, file a
   clarification item rather than auto-claim.
2. **Regatta dogfooding.** Risk: recursive bug amplification.
   Convention: no dogfooding until canary catch-rate stable >85% on a
   non-Regatta production deployment.
3. **Unstructured-repo path.** Many repos have no machine-readable
   spec. Regatta does not solve this; the repo must encode "done"
   somewhere before it can use the fleet.
4. **Multi-repo coordination.** One deployment per repo. Monorepo
   cross-package dependencies handled by single-repo `dependencies`
   graph; cross-repo deferred.
5. **Cross-vendor L4 benchmark.** When the cross-vendor adapter
   lands, re-run the canary corpus on `(Opus, Sonnet)` vs `(Opus,
   GPT-5)` and publish the same-family-vs-cross-family delta.
6. **Program decomposition empirical floor.** Published 2026
   SWE-bench-Verified gain from decomposition is ~6 pp under the
   same model (Opus 4.5: 45.9% → 51.8% with better scaffolding).
   The MVP-3 acceptance bar is set at ≥5 pp on Regatta's canary
   corpus; an unanswered question is whether that translates from
   SWE-bench to Regatta's incident-pattern (P1-P13) corpus, or
   whether Regatta's benchmark is more sensitive (smaller gain
   suffices) or less (larger gain required) than SWE-bench.
7. **Falsification-quality enforcement.** The handoff schema
   requires per-falsification citation and mutation-kind enum;
   Meta's mutation-testing + LLM work (FSE 2025) and MetaQA
   (ACM 2025) are the closest prior art. Open: does the canary
   archetype corpus need its own mutation operators for the
   `canary_archetype_killed` field to be meaningful, and can the
   archetype corpus carry mutation pairs the way mutation-testing
   benchmarks do?
8. **Decision-agent vs deterministic Go function.** Resolved in
   §Programs in favor of deterministic Go (P1 applied one layer
   up). Open: do we need an *advisory* LLM rationale generator
   (writes to audit log, never to verdict) for human reviewers?
   Cost is ~$0.10/decision; value is forensic clarity. Defer
   until L6 reviewers ask.

## References

- `incidents.md` -- AI-agent incident catalog + full prose for the Trap Catalog patterns.
- `schemas/spec_adapter.go` -- normative Go interface.
- `schemas/gate_result.schema.json` -- normative gate output schema.
- `schemas/work_item.schema.json` -- normative work item schema.
- `schemas/regatta.v1.cue` -- CUE schema for `regatta.yaml`.
- `testdata/gates/l0/` -- L0 fixture corpus contract.
- `testdata/gates/canary/` -- canary archetype corpus contract.
- Anthropic, *Claude 4 System Card* (2025).
- Palisade Research, *Shutdown Resistance in Reasoning Models* (2025).
- arXiv:2509.10540 -- EchoLeak technical writeup.
- AI Incident Database (incidentdatabase.ai).
- OWASP Top 10 for LLM Applications (LLM01, LLM05).

