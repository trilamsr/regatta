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
  agent incident — Replit's prod-DB wipe, EchoLeak, Cursor MCPoison
  RCE, Copilot credential leak via PR title, the curl bounty
  shutdown over AI slop — shares a root cause: a defense that lived
  in the agent's prompt or self-report instead of in the platform.
  Regatta's gates are deterministic, out-of-band, and signed wherever
  possible. The [Trap Catalog](#trap-catalog) maps 19 incidents to
  10 enforcement points.

The default gate stack is six layers; the [§Gate stack](#gate-stack-normative)
section is the normative source. Repos add custom gates, reorder,
swap models, and tune thresholds via `regatta.yaml`. Deterministic
gates run first in parallel; AI gates run after the deterministic
floor passes. Default-deny on any blocking finding. L0 is mandatory.

### Companion artifacts

`docs/incidents.md` (19 incidents + full P1–P10 prose);
`schemas/spec_adapter.go` (Go interface, normative);
`schemas/{gate_result,work_item}.schema.json` (JSON Schemas);
`schemas/regatta.v1.cue` (config schema); `gates/l0/testdata/` (L0
fixture corpus contract); `gates/canary/testdata/` (canary archetype
corpus).

## Motivation

**Spec → PR is embarrassingly parallel** — open work items are mostly
independent. A team processing them serially leaves throughput on the
table; a team processing them in parallel *without rigorous gates* is
one bad merge from a Replit-class incident.

**Existing agent products ship at the wrong abstraction.** SWE-agent,
Devin, Cursor agent mode, Copilot Workspace, Aider — each ships an
agent runtime then leaves gating to the consumer's existing review
process. That process was designed for human PRs; it doesn't catch
reward-hacking, vacuous tests, sycophantic acceptance, hallucinated
dependencies, or prompt-injection via repo content. Regatta inverts:
**the gates are the product**; the agent runtime is interchangeable
(see [§Alternatives](#alternatives) for the honest read).

**The incident catalog is dense and growing.** 2023–2026 documents 19
distinct incident classes. Closing the 10 known load-bearing failure
modes is feasible only because the platform sits between the agent
and the destructive surface.

## Threat Model

The threat model is the floor every other section sits on. If you
disagree with any assumption here, the design downstream is wrong for
your environment.

### Assets

- **`regatta.db`** (orchestrator state) — agent lifecycle, gate
  verdicts, token spend, canary results. Contains short-lived OIDC
  tokens at rest.
- **`regatta.yaml`** — safety policy, gate config, allowlists. Editing
  it relaxes every other defense.
- **`prompts/*.md`** — gate-prompt artifacts. Editing them changes
  what L3/L4/L5 ask the model.
- **Agent worktrees** — transient; cleaned on reap.
- **Target repo `main`** — the only trust root for instructions.
- **Audit log** — append-only, written to an out-of-band sink.

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

The signed trust root (target repo `main` at a pinned tag SHA — holds
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
  bypasses every rule below — `regatta verify-repo-config` enforces this
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
| **P3** | Trusted instructions from `main` only; all other text is data — including config / MCP discovery / deeplinks / ingested documents | Comment-and-Control (#8), EchoLeak (#6), MCPoison/CurXecute/Rules-File (#9), Copilot RCE (#10), MCP-Git (#20), TrustFall (#21), DXT calendar (#24), ClaudeBleed (#25), claude-cli deeplink (#26), Cowork (#29) |
| **P4** | Least-privilege, ephemeral, environment-scoped credentials | PocketOS (#2), Amazon Q (#7), Comment-and-Control (#8), DXT (#24), ClaudeBleed (#25) |
| **P5** | Out-of-band supervisor for limits and kill-switches | Sakana (#3), o3 shutdown (#4), Cursor runaway (#19), TrustFall (#21), Antigravity (#28) |
| **P6** | Verified grounding for any outward-facing claim | Air Canada (#13), MyCity (#14), Mata (#17), slopsquatting (#11), curl (#16) |
| **P7** | Schema-level scope constraints, not prompt-level | Chevy $1 (#15), MyCity (#14), Air Canada (#13), Antigravity workspace-root (#28) |
| **P8** | Spend / iteration brakes with mandatory re-approval | Cursor runaway (#19), Sakana (#3), GTG-1002 (#18) |
| **P9** | Sensitive context segregation | Opus 4 blackmail (#5), EchoLeak (#6), Cowork PII (#29) |
| **P10** | Invisible-glyph normalization + signed prompt artifacts | Rules-File-Backdoor (#9), Amazon Q (#7), MCPoison (#9), TrustFall (#21), ClaudeBleed (#25), claude-cli (#26), Cowork (#29) |
| **P11** | Agent-artifact release pipelines are themselves attack surface — content audit + build attestation + runner-memory isolation + age gate | Amazon Q wiper (#7), Claude Code source leak (#23), Mini Shai-Hulud (#30) |
| **P12** | Inbound vulnerability signals default-escalate — two-key on the decision to ignore | curl slop (#16), Cowork (#29), Lovable BOLA Apr-26 (#31) |
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
the gate runner (parallel L0–Lx), the rejection router (K=3 then
escalate), the canary injector (~5%), supervisor limits (cgroup-
enforced), and the reaper + lesson capture. It spawns one Claude
agent per ready work item into a lane-isolated worktree; each agent
opens a PR; the gate stack runs on every push.

### Spec contract

A work item is the agent's complete spec. Regatta declares a
`SpecAdapter` interface
([`schemas/spec_adapter.go`](../schemas/spec_adapter.go) — normative);
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
but cannot return the prior body text — Linear's GraphQL `IssueHistory`
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

- `acceptance_criteria[].text` — **immutable post-publication**. L0
  enforces byte-equality under UTF-8 NFC normalization and
  invisible-glyph stripping.
- `acceptance_criteria[].state` — flipped by the agent on completion.
  The flip must arrive with a `citation` field matching
  `^(test|file|commit)=\S+$`.
- `dependencies` — DAG; orchestrator only spawns agents whose chain is
  satisfied. Cycles fail-fast at adapter `List()`.
- `source` — immutable pointer L0 uses for verification (SHA-pinned
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
6. Flip `state` to `done` with citation (text untouched — L0 checks).
7. Write the PR body per the repo's PR template.
8. `gh pr create`. Stop. Wait for gate routing or supervisor signal.

### Gate stack (normative)

**L0 — spec-immutability** (deterministic, mandatory). Diffs the spec
source between PR base and HEAD; for each touched work-item entry,
asserts the only change is the `state` field flip. Acceptance-criteria
text edits fail hard. Full normative behavior — merge-base choice,
UTF-8 NFC, rename handling, invisible-glyph stripping, merge-time
re-run — is specified in
[`gates/l0/testdata/README.md`](../gates/l0/testdata/README.md). The
fixture corpus there is the contract; L0 implementations pass if and
only if they pass every fixture.

**L1 — repo CI** (deterministic, mandatory, configurable command).
Whatever passes for "green" in the repo today. Regatta shells out per
`ci.command`; non-zero exit blocks.

**L2 — PR-body conformance** (deterministic, mandatory, configurable
template). Validates the citation block, optional ticket link, optional
release-notes section.

**L3 — spec-conformance verifier** (AI judicial; default Opus 4.7).
For each flipped criterion in L0's output: read criterion text
**fetched from `main` SHA** (not PR branch — P3), the PR diff, the
citation. Emits a `GateResult` per
[`schemas/gate_result.schema.json`](../schemas/gate_result.schema.json)
with `gate_kind: ai_judicial`. Any per-criterion `verdict: fail`
blocks. Strong-judgment task → most-capable model. Does not evaluate
code quality (L4's job).

**L4 — adversarial reviewer** (AI adversarial; default Sonnet 4.6).
Prompted to *reject*. Reads repo principles/style/non-functional
budgets (paths in `context.trusted_doc_paths`, all fetched from signed
`main`). Output: severity-ranked findings; blocks on `critical` or
≥2 `high`. Optionally invokes a deeper review skill on PRs labeled
`regatta:rigorous-review`. (See [§Alternatives](#alternatives) for the
honest read on Sonnet-vs-Opus as a "different family.")

**L5 — drift detector** (AI rule-check; default Haiku 4.5). For files
touched: verify the corresponding tracking docs / changelogs / ADRs
are updated per repo rules. Enforces the **consumer-test contract**:
symbols cited by downstream work items must have a `*_consumer_test.*`
in the consumer's package before the upstream PR merges.

**L6 — human merge** (mandatory, branch-protection enforced). The
maintainer reads L0–L5 + custom-gate `GateResult` comments and makes
the merge call. AI review is **advisory and rejecting**, never
approving.

**Custom gates.** Repos add gates of either kind via `regatta.yaml`.
Example shapes: `license_audit` (deterministic), `migration_safety`
(AI), `i18n_completeness` (deterministic).

### Orchestrator shape

Long-running Go daemon. The recommended deployment is a self-hosted
daemon watching one or more repos via VCS APIs — fastest to iterate,
lowest blast radius. GitHub Actions integration and a hosted
multi-tenant service are deferred.

Responsibilities:

1. **SpecWatcher** — polls or webhook-subscribes the configured
   `SpecAdapter`; builds a work queue of items whose dependency chain
   is satisfied.
2. **Scheduler** — caps concurrency per lane (default 1); holds soft
   locks on hotspot files declared in `regatta.yaml`. **Lock
   acquisition is sorted by lock-name** to prevent deadlock; locks
   carry a heartbeat lease (15 min default, refreshed every 60 s)
   that auto-releases on agent SIGKILL.
3. **AgentSpawner** — creates worktree; spawns `claude` with the
   templated prompt; captures pid + session-id atomically into sqlite
   (one transaction reserves the lane and records the pid).
4. **PRWatcher** — runs gates in parallel against the HEAD SHA. Each
   gate run is keyed by `(pr_sha, gate_id)` for idempotency: a re-run
   against the same SHA returns the same `run_id` and skips
   recomputation.
5. **RejectionRouter** — rejecting gate `GateResult` newer than the
   agent's last commit wakes the agent. K=3 rejections → "needs
   human", agent unspawned (P8).
6. **CanaryInjector** — Bernoulli(p=`safety.canary_rate`) on each
   spawn; archetype patches from `gates/canary/testdata/catalog.ndjson`.
   See [§Test harness](#test-harness).
7. **SupervisorLimits** — wall-clock, disk, network, iteration, spend
   enforced in the parent process via cgroups (Linux) or rlimits
   (macOS). The agent cannot read or modify these (P5).
8. **Reaper** — on merge: tear down worktree, release locks, archive
   session state, kick scheduler. On close-without-merge: same plus
   `needs-rework` label.
9. **LessonCapture** — at agent EOL, if friction signals fired,
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

## Day 1 → Day 30 Runbook

**Pre-implementation banner.** Regatta is not shipped. Commands below
describe the v1 release surface. The schemas + this runbook are the
contract; the binary follows.

### Day 1 — install and validate

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
`verify-repo-config` is mandatory before the first `regatta serve` —
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

### Day 2 — calibrate the gates

```sh
$ regatta gate-calibrate --pr 95,87,79              # 3 already-merged PRs
$ regatta gate-calibrate --canary-corpus            # all 8 archetypes
```

`gate-calibrate` runs L0–L5 against the chosen PRs and emits a
per-gate confusion matrix. The 8 canary archetypes (see
[`gates/canary/testdata/README.md`](../gates/canary/testdata/README.md))
have known-expected verdicts. Tune `gates[*].severity_block` until
calibration is clean (typical fixes: raise L4 threshold to `critical`
only when over-cautious; add path filters when L5 false-drifts on
auto-generated files).

### Day 3 — single pilot PR (human-spawned)

```sh
$ regatta pilot --work-item 101 --no-orchestrator --interactive
```

Spawns one agent in `.regatta/worktrees/work-101-<slug>/`; you watch
it live. Once the PR opens:

```sh
$ regatta gate-run --pr 256                          # runs L0–L5; posts comments
```

If gates accept and a synthetic-bad variant rejects, you're ready for
Day 7.

### Day 7 — orchestrator on, one lane

```sh
$ regatta serve --lane server --max-concurrency 1
```

Watches one lane only; spawns one agent at a time. Runs ~5–10 items
in the first week. Monitor:

```sh
$ regatta status                                     # one-line per agent + last gate
$ regatta digest --since 1w                          # markdown digest
$ regatta canary-report                              # catch-rate + recent canary PRs
```

### Day 30 — all lanes, concurrency 1 each

```sh
$ regatta serve                                      # all lanes from regatta.yaml
```

Promotion criteria for raising any lane to concurrency 2:

- ≥20 PRs merged in the lane via fleet
- Canary human-catch-rate ≥85% over rolling 20-canary window
- Net-helpfulness ≥70% (see [§Test harness](#test-harness))

## Worked Example (simulated)

A Node.js backend repo `acme/api`. Work item #117:

> **#117 — Add request-ID propagation to upstream HTTP calls.** Currently `axios` calls strip the `X-Request-ID` header on outbound retries.
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
[`gates/l0/testdata/`](../gates/l0/testdata/) is the v1 contract
(target: 200 fixtures by v1.0). CI runs the full corpus on every
commit.

**Statistical-tier.** L3/L4/L5 + AI custom gates audit via three
mechanisms: a **golden corpus** of ≥30 historical PRs with
maintainer-recorded verdicts, replayed weekly with ≥95% agreement
required to ship a new prompt or model; **VCR fixtures** for
deterministic unit-test replay; and the **canary catch-rate** from
`gates/canary/testdata/` — 18 known-bad archetypes that must each be
caught at the expected layer. The 30-PR golden floor is adequate for
gross-regression detection only (Wilson 95% CI on 28/30 = [78.7%,
98.2%]); target 200 PRs over 6 months, with a CUSUM-style drift
detector on the weekly agreement series.

Two findings from the LLM-judge literature shape canary design.
**Style/length bias is ~10× larger than same-family bias** (0.76–0.92
across four vendor families on RewardBench 2; same-family bias is
~9% per Preference Leakage 2026), so canary archetypes must exercise
verbosity, formatting tricks, and citation-faking — not just
correctness defects. **The Opus 4 agentic-misalignment study shows
a 55.1% real-context vs 6.5% eval-context behavior gap** — judges
behave very differently when they detect eval scaffolding, so canary
PRs must be indistinguishable from real PRs (no test-only repos, no
distinctive author bots, no eval-shaped prompts in agent context).

### Metrics with precise definitions

- **Gate-catch-rate** = (PRs where ≥1 blocking gate fires) / (PRs
  total). Alert threshold: <99% over rolling 50 PRs. Low value means
  gates are letting everything through.
- **Human-catch-rate** = (canary PRs L6 rejected) / (canary PRs that
  reached L6). Alert threshold: <80% over rolling 20 canaries. Low
  value is the 737-MAX rubber-stamp signal — humans trust the gates
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
| **Total** | **~$1.15** | **~$17.93** | **~$77.66** |

Two cost-risk landmines on top of the table. **Opus 4.7 ships a new
tokenizer** producing up to 35% more tokens per text than its
predecessor — real bills run ~1.2× the table until prompt sizes are
re-tuned. **Prompt-caching is load-bearing**: Claude Code's March 2026
cache regression dropped hit rate to 4–17% and inflated cost 10–20×;
Regatta must halt the fleet if rolling `cache_hit_rate < 30%`. The
minimum cacheable block on current Opus / Haiku is 4,096 tokens — L5
prompts under that silently bypass caching with no error.

For comparison (mid-2026 list pricing, *unverified*, fully-loaded
on a 10-eng team at 15 PRs/week): Devin Team + CodeRabbit ~$50/PR;
Copilot Business + Greptile + ad-hoc agent ~$40/PR; Regatta self-host
+ Anthropic API + Claude Code seats ~$70/PR-equivalent. Regatta's
positioning is **not the cheapest** — it's the only one with a
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
| L4 over-cautious — rejects everything | net-helpfulness <50% rolling 10 | `regatta net-helpfulness l4` | bump severity threshold | swap model |
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

## Stop conditions (measurable)

Abandon a deployment if **any** triggers: canary human-catch-rate <60%
over rolling 20-canary window for ≥4 weeks post-Phase-3; maintainer
review-burden median time-on-PR has not dropped vs. pre-Regatta
baseline by week 6 (measured via `gh pr view --json
closedAt,createdAt`); agent completion rate <60% across rolling 10-PR
window for ≥2 consecutive windows; or cost per completed item exceeds
`safety.spend_cap_usd` consistently for 3+ items in a row.

## Alternatives

**(a) No orchestrator, single-shot agent.** Accepted as Day 1 mode in
the runbook; rejected as steady state.

**(b) Humans curate an agent-ready subset.** Rejected — the spec is
the oracle. Curation moves the bottleneck.

**(c) One big agent that does everything.** Rejected — no parallelism;
self-review is the rubber-stamp failure mode (P8 + 737-MAX class).

**(d) Reviewer ensemble (3 agents, majority).** Deferred — single
adversarial + mixed family closes same-family bias at 1× cost. Revisit
if false-pass rate justifies 3×.

**(e) Swap Claude for another runtime.** *Honest concession.* Ships
Claude only today. The cross-vendor adapter (Codex, Gemini,
open-source) is the single most load-bearing deferred deliverable.
Until it ships, treat "runtime-agnostic" as a thesis, not a feature.
Pairs with the next point.

**(f) "Different family" claim at L4.** Sonnet 4.6 and Opus 4.7 are
sibling Claude-4.x checkpoints — same vendor, same generation, different
post-training. The published proxy measurement closest to this pair is
GPT-4o judging GPT-4-turbo output, which inflates win-rate by **8.9%**
(Li et al., "Preference Leakage," ICLR 2026, Table 2). The "same-model"
case (Opus judging Opus) inflates by 23.6% in the same study, and
self-preference per Panickssery et al. (NeurIPS 2024) is 0.71–0.91 on
summarization pairs for strong models. Sonnet→Opus is therefore
meaningfully better than Opus→Opus — by roughly the same factor (2–3×)
that family-bias is smaller than self-preference bias — but is *not*
equivalent to cross-vendor judging, which the same study measures at
2.8% (different series within a family) and which cross-vendor work
(Cohere PoLL, arXiv 2404.18796) measures at variance reductions of ~64%
over a single large judge.

| Configuration | Win-rate inflation | Source |
|---|---|---|
| Opus judges Opus | ~20–50% | Panickssery '24, Wataoka '24 |
| Sonnet 4.6 judges Opus 4.7 (proxy: GPT-4o↔GPT-4-turbo) | ~9% | Preference Leakage '26 |
| GPT-5 judges Opus (cross-vendor proxy) | ~3% | Preference Leakage '26 |
| Three-judge cross-vendor jury | smallest variance (σ=2.2 vs 6.1) | PoLL '24 |

This design captures ~60% of the bias-reduction available; a
cross-vendor judge closes the remaining ~6 percentage points.
Style/length bias (0.76–0.92 across four vendor families per
RewardBench 2) and prompt-injection attack surface (JudgeDeceiver:
89% ASR on open-source judges) are *larger* design risks than the
residual same-family delta. Ship with Sonnet 4.6 today; the
L4-vs-cross-vendor comparison is deferred behind a documented
canary-corpus delta as the acceptance criterion — a family-stratified
catch-rate ratio `< 0.85` triggers cross-vendor L4.

**(g) Vendor the gate stack into each repo.** Rejected — keeps
Regatta independently versionable; lets one deployment serve many
repos; avoids forking gate logic per consumer.

## Competitor positioning

| | Regatta | Devin 2.0 | Cursor 3 + BugBot | Copilot Workspace + Coding Agent | Claude Code Agent Teams | OpenHands | CodeRabbit | Gitar |
|---|---|---|---|---|---|---|---|---|
| **Pricing** | Self-host + API at cost | $500/mo + ACUs | $40/seat/mo + usage | $39/seat/mo + usage | API at cost | OSS + API | $24/dev/mo | (stealth, $9M) |
| **Gate transparency** | open, configurable | proprietary | proprietary | proprietary | open via Code | open | semi-open | semi-open |
| **Runtime-agnostic** | thesis (deferred) | no | no | no | Claude only | yes | model-agnostic | yes |
| **Published incident defenses** | yes (10-pattern Trap Catalog) | partial | partial | partial | partial | no | partial | partial |
| **Deterministic spec-immutability gate** | **yes (L0)** | no | no | no | no | no | no | no |
| **Tamper-evident audit log out-of-band** | yes | no | no | no | no | no | no | no |
| **Canary-PR injection** | yes (8 archetypes ship) | no | partial (BugBot self-test) | no | no | no | no | no |
| **SWE-bench-style benchmark** | not yet | yes | yes | yes | yes | yes | no | no |

The defensible differentiators: **(1) deterministic L0**, **(2)
out-of-band audit**, **(3) published canary archetype corpus**. None
are reproducible in a sprint; (1) and (3) are publishable as
standalone OSS primitives.

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

## References

- `incidents.md` — AI-agent incident catalog + full prose for the Trap Catalog patterns.
- `schemas/spec_adapter.go` — normative Go interface.
- `schemas/gate_result.schema.json` — normative gate output schema.
- `schemas/work_item.schema.json` — normative work item schema.
- `schemas/regatta.v1.cue` — CUE schema for `regatta.yaml`.
- `gates/l0/testdata/` — L0 fixture corpus contract.
- `gates/canary/testdata/` — canary archetype corpus contract.
- Anthropic, *Claude 4 System Card* (2025).
- Palisade Research, *Shutdown Resistance in Reasoning Models* (2025).
- arXiv:2509.10540 — EchoLeak technical writeup.
- AI Incident Database (incidentdatabase.ai).
- OWASP Top 10 for LLM Applications (LLM01, LLM05).

