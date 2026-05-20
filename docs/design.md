# Regatta: A Repo-Agnostic Autonomous-Agent Fleet

- **Status:** draft v3.1 (A+ revision pass)
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

The whole design rests on these. Every other choice traces back here.

- **The spec is the oracle.** Most repos already encode "done" in a
  machine-readable form — ticket acceptance criteria, RFC rubrics,
  test plans. Regatta plugs into that surface through `SpecAdapter`
  ([§Spec contract](#spec-contract)); it does not invent a new spec
  format.
- **The platform enforces what the prompt cannot.** Every public
  AI-agent incident — Replit deleting a prod database, EchoLeak
  exfiltrating M365 data, Cursor's MCPoison RCE, Copilot leaking
  credentials via PR titles, curl shutting down its bounty program
  over AI slop — has the same root cause: a defense that lived in the
  agent's prompt or self-report instead of in the surrounding
  platform. Regatta's gates are deterministic, out-of-band, and
  signed wherever possible. The Trap Catalog
  ([§Trap Catalog](#trap-catalog)) maps 19 incidents to 10 enforcement
  points.

### Default gate stack

Six layers, all configurable per repo. L0 is mandatory.

| Layer | Kind | Default model | Purpose |
|---|---|---|---|
| **L0** spec-immutability | deterministic | — | Block any PR that mutates criterion text or flips status without citation. |
| **L1** repo CI | deterministic | — | Run the repo's own check (`make test`, `npm test`, etc.). |
| **L2** PR-body conformance | deterministic | — | Validate citation block, ticket link. |
| **L3** spec-conformance | AI judicial | Opus 4.7 | For each flipped criterion, verify diff evidences the criterion. |
| **L4** adversarial reviewer | AI adversarial | Sonnet 4.6 | Prompted to reject; reviews against repo principles/style/budgets. |
| **L5** drift detector | AI rule-check | Haiku 4.5 | Tracking docs / changelogs / consumer-test pattern. |
| **L6** human merge | human | — | Maintainer approves on branch protection. |

Repos add custom gates (`security_scan`, `migration_safety`,
`license_audit`, `i18n_check`), reorder, swap models, tune thresholds
via `regatta.yaml`. Deterministic gates run first in parallel; AI
gates run after the deterministic floor passes. Default-deny on any
blocking finding.

### Companion artifacts in this repo

- `docs/incidents.md` — 19 AI-agent incidents with primary sources;
  full prose of patterns P1–P10.
- `schemas/spec_adapter.go` — normative Go interface for `SpecAdapter`.
- `schemas/gate_result.schema.json` — JSON Schema for the structured
  payload every gate emits.
- `schemas/work_item.schema.json` — JSON Schema for `WorkItem`.
- `schemas/regatta.v1.cue` — CUE schema for `regatta.yaml`.
- `gates/l0/testdata/` — fixture corpus for the L0 gate (pass/fail/
  edge cases).
- `gates/canary/testdata/` — canary archetype corpus.

## Motivation

**Spec → PR is embarrassingly parallel.** Open work items are mostly
independent. A team that processes them serially leaves throughput on
the table. A team that processes them in parallel *without rigorous
gates* is one bad merge away from a Replit-class incident.

**Existing agent products ship at the wrong abstraction.** SWE-agent,
Devin, Cursor agent mode, Copilot Workspace, Aider — each ships an
agent runtime, then leaves gating to the consumer's existing review
process. That process was designed for human PRs and doesn't catch
reward-hacking, vacuous tests, sycophantic acceptance, hallucinated
dependencies, or prompt-injection via repo content. Regatta inverts:
**the gates are the product**; the agent runtime is interchangeable.
(See [§Alternatives](#alternatives) for the honest read on
runtime-agnosticism today.)

**The incident catalog is dense and growing.** 2023–2026 documents 19
distinct incident classes ([incidents.md](incidents.md)). Building
gates that close all 10 known load-bearing failure modes is feasible
only because the platform sits between the agent and the destructive
surface.

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

```
            ┌─────────────────────────────────────────────┐
            │  Signed trust root: target repo main + tag  │
            │  (regatta.yaml, prompts/, AGENTS.md)        │
            └────────────────────┬────────────────────────┘
                                 │ pinned by SHA at start
            ┌────────────────────▼────────────────────────┐
            │  Orchestrator host (regatta.db, OIDC mint)  │
            │  — assumed honest-but-fallible              │
            │  — actions countersigned to audit sink      │
            └─────────┬────────────────────┬──────────────┘
                      │                    │
         agent ctx (untrusted)    gate ctx (low-trust)
                      │                    │
   ┌──────────────────▼──┐   ┌─────────────▼──────────────┐
   │ Agent worktree       │   │ Gate runner                │
   │ (no main credentials)│   │ (model API, no repo write) │
   └──────────────────────┘   └────────────────────────────┘
```

### Threats explicitly defended

The 10 patterns in §Trap Catalog. The most load-bearing five (each
preventing 3+ documented incidents): P1 (deterministic gate before AI
gate on destructive ops), P3 (trusted text from `main` only), P5
(out-of-band supervisor), P6 (verified grounding), P8 (spend +
iteration brakes).

### Threats explicitly *not* defended (in v3.1)

- **Compromised model provider** producing systematically-biased gate
  output. Mitigation deferred to v4 cross-vendor adapter.
- **Side-channels in PR comments or CI logs**. Egress-content scanning
  for known secret formats is on the v3.1 roadmap but not yet
  implemented.
- **Malicious maintainer**. Two-key approval on `regatta.yaml` edits
  (CODEOWNERS requires ≥2 reviewers) is the only mitigation; collusion
  is not modeled.

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

The 10 platform-enforcement patterns that close the documented AI-agent
incident classes. Each pattern is wired into the architecture below;
none is advisory. Full prose, primary-source citations, and the
cross-pattern map live in [`incidents.md`](incidents.md).

| ID | Pattern | Prevents (incidents from [incidents.md](incidents.md)) |
|---|---|---|
| **P1** | Deterministic gate before AI gate on destructive ops | Replit (#1), PocketOS (#2), slopsquatting (#11), Lovable RLS (#12) |
| **P2** | Two-key approval on irreversible actions | Replit (#1), PocketOS (#2), Copilot RCE (#10) |
| **P3** | Trusted instructions from `main` only; all other text is data | Comment-and-Control (#8), EchoLeak (#6), Cursor MCPoison/CurXecute/Rules-File (#9), Copilot RCE (#10) |
| **P4** | Least-privilege, ephemeral, environment-scoped credentials | PocketOS (#2), Amazon Q (#7), Comment-and-Control (#8) |
| **P5** | Out-of-band supervisor for limits and kill-switches | Sakana (#3), o3 shutdown (#4), Cursor runaway (#19) |
| **P6** | Verified grounding for any outward-facing claim | Air Canada (#13), MyCity (#14), Mata (#17), slopsquatting (#11), curl (#16) |
| **P7** | Schema-level scope constraints, not prompt-level | Chevy $1 (#15), MyCity (#14), Air Canada (#13) |
| **P8** | Spend / iteration brakes with mandatory re-approval | Cursor runaway (#19), Sakana (#3), GTG-1002 (#18) |
| **P9** | Sensitive context segregation | Opus 4 blackmail (#5), EchoLeak (#6) |
| **P10** | Invisible-glyph normalization + signed prompt artifacts | Cursor Rules-File-Backdoor (#9), Amazon Q (#7), MCPoison (#9) |

P1, P3, P5, P6, P8 each prevent 3+ documented incidents and are the
highest-leverage rules.

### How patterns map to layers

| Layer | Patterns enforced |
|---|---|
| L0 spec-immutability | P1, P3, P10 (rubric body) |
| L1 repo CI | P1 (license, govulncheck), P6 (test grounding) |
| L2 PR-body | P6 (citation block) |
| L3 spec-conformance | P6 (per-criterion grounding) |
| L4 adversarial | P3, P6, P7, P9 (all read against signed `main` text) |
| L5 drift | P3 (consumer-test contract from `main`) |
| L6 human merge | P2 |
| Orchestrator | P4, P5, P8, P10 |

## Proposal

### Architecture

```
              ┌──────────────────────────────────────────┐
              │           Regatta Orchestrator           │
              │  (standalone Go daemon)                  │
              │  • SpecAdapter (per-repo, pluggable)     │
              │  • Scheduler — sorted-lock acquisition   │
              │  • AgentSpawner (claude --resume)        │
              │  • PRWatcher (GitHub/GitLab adapter)     │
              │  • GateRunner — parallel L0–Lx           │
              │  • RejectionRouter (K=3 then escalate)   │
              │  • CanaryInjector (~5%)                  │
              │  • SupervisorLimits (cgroup-enforced)    │
              │  • Reaper + LessonCapture                │
              └────────────┬─────────────────────────────┘
                           │ spawns
                ┌──────────┼──────────┐
                ▼          ▼          ▼
              Agent A   Agent B   Agent C
              Lane X    Lane Y    Lane Z
              work N    work M    work P
                │          │          │
                └──── PRs ─┴────── PRs┘
                           │
                ┌──────────▼──────────────────┐
                │   Gate Stack (see Summary)   │
                └─────────────────────────────┘
```

### Spec contract

A work item is the agent's complete spec. Regatta declares a
`SpecAdapter` interface
([`schemas/spec_adapter.go`](../schemas/spec_adapter.go) — normative);
the repo selects an implementation.

Built-in adapters: `github_issues`, `gitlab_issues`,
`markdown_catalog`, `jira`, `linear`, `custom` (shells out to a binary
on PATH via a versioned JSON-over-stdio protocol).

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

Long-running Go daemon. The single recommended deployment for v3.1 is
a self-hosted daemon watching one or more repos via VCS APIs — fastest
to iterate, lowest blast radius. GitHub Actions integration ships as
v3.2; hosted multi-tenant service is v4.

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

Authored, validated by the CUE schema
[`schemas/regatta.v1.cue`](../schemas/regatta.v1.cue), versioned with
a migration tool (`regatta migrate-config --from 1 --to 2`).

```yaml
version: 1
repo: { host: github, owner: example, name: myproject, default_branch: main }
spec_adapter:
  type: github_issues
  selector: 'label:planned'
  acceptance_section: '## Acceptance'
ci: { command: 'npm test && npm run lint', timeout_minutes: 30 }
pr_template: { citation_section_required: true }
gates:
  - { id: spec_conformance, type: ai, model: claude-opus-4-7,    severity_block: ['fail'] }
  - { id: adversarial,      type: ai, model: claude-sonnet-4-6,  severity_block: ['critical', '2*high'],
      rigorous_label: 'regatta:rigorous-review' }
  - { id: drift,            type: ai, model: claude-haiku-4-5,   severity_block: ['drift'] }
  - { id: license_audit,    type: deterministic, command: 'npx license-checker --failOn GPL' }
lanes:
  - { id: server, paths: ['src/server/**'], max_concurrency: 1 }
  - { id: client, paths: ['src/web/**'],    max_concurrency: 1 }
hotspots: [CHANGELOG.md, package.json, pnpm-lock.yaml, README.md]
safety:
  destructive_ops_deny: ['git push --force', 'rm -rf /', 'npm publish', 'DROP TABLE']
  agent_creds_scope: dev_only
  iteration_cap: 50              # P8 — hard cap on agent loop
  spend_cap_usd: 50              # P8 — per-task ceiling
  spend_cap_usd_per_day: 200     # P8 — org-wide ceiling
  canary_rate: 0.05              # ~1 in 20 PRs is a canary
context:
  trusted_doc_paths: [PRINCIPLES.md, STYLE.md, docs/architecture.md]
  agent_guidance_path: AGENTS.md
  agent_guidance_codeowners_check: true
telemetry:
  digest_path: docs/regatta-digest.md
  state_db_path: ./.regatta/regatta.db
  audit_sink: s3://acme-audit/regatta/?object-lock=COMPLIANCE
```

The `severity_block: ['critical', '2*high']` mini-DSL: any `critical`
finding OR ≥2 `high` findings block the PR. Future operators: `&`, `|`,
and `count*severity` are the only forms; the validator rejects others.

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
```

Expected output of `validate-spec`:
```
✓ adapter: github_issues — 12 items, 0 cycles, 3 ready
✓ acceptance criteria: 47 parsed, 47 NFC-clean, 0 invisible-glyph hits
✓ dependency graph: 12 nodes, 8 edges, DAG verified
ready to spawn: [#101, #104, #109]
```

### Day 2 — calibrate the gates

```sh
$ regatta gate-calibrate --pr 95,87,79              # 3 already-merged PRs
$ regatta gate-calibrate --canary-corpus            # all 8 archetypes
```

`gate-calibrate` runs L0–L5 against the chosen PRs and emits a
per-gate confusion matrix. The 8 canary archetypes (see
[`gates/canary/testdata/README.md`](../gates/canary/testdata/README.md))
have known-expected verdicts; a gate that fails its calibration is not
safe to enable yet.

Tune `gates[*].severity_block` until calibration is clean. Common
calibration outputs:

- L4 over-cautious: ≥3 false-rejects on clean PRs → raise threshold to
  `['critical']` only.
- L5 noisy: false-drift on auto-generated files → add path filters.

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

It opens PR #259 with a citation block in the body. The orchestrator
runs gates and posts five comments:

```jsonc
// L0 spec-immutability
{"gate_id":"l0","verdict":"pass","blocking":false,
 "findings":[],
 "telemetry":{"duration_ms":34}}

// L1 repo CI
{"gate_id":"l1","verdict":"pass","blocking":false,
 "telemetry":{"duration_ms":118000}}

// L3 spec-conformance (Opus 4.7)
{"gate_id":"l3","verdict":"pass","blocking":false,
 "findings":[
   {"id":"c1","severity":"info","claim":"Test 'request-id retries preserve header' newly added at test/integration/request-id.test.js:42; assert checks X-Request-ID on retries.","evidence":{"path":"test/integration/request-id.test.js","line_start":42,"line_end":71}},
   {"id":"c2","severity":"info","claim":"Same test satisfies criterion 2.","evidence":{"path":"test/integration/request-id.test.js","line_start":42}},
   {"id":"c3","severity":"info","claim":"CHANGELOG.md:14 entry under [Unreleased] / Fixed.","evidence":{"path":"CHANGELOG.md","line_start":14}}
 ],
 "telemetry":{"tokens_input":4280,"tokens_cached":3200,"tokens_output":410,"cost_usd":0.062,"model":"claude-opus-4-7"}}

// L4 adversarial (Sonnet 4.6)
{"gate_id":"l4","verdict":"pass","blocking":false,
 "findings":[
   {"id":"f1","severity":"low","claim":"Test mocks the upstream with nock; consider a request-level interceptor for parity with prod axios config.","remediation":"Optional follow-up."}
 ],
 "telemetry":{"tokens_input":5100,"tokens_cached":4600,"tokens_output":280,"cost_usd":0.041,"model":"claude-sonnet-4-6"}}

// L5 drift (Haiku 4.5)
{"gate_id":"l5","verdict":"pass","blocking":false,
 "telemetry":{"tokens_input":2200,"tokens_output":90,"cost_usd":0.004,"model":"claude-haiku-4-5"}}
```

Total gate spend: **~$0.11**. Plus agent spend across 4 iterations:
**~$0.95**. PR-total cost: **~$1.06**. Maintainer reviews L0–L5 in
~3 min, clicks Merge. Reaper tears down the worktree, archives session
state, kicks the scheduler. Next item on lane `server` spawns.

## Test harness

The doc separates audit-tier by gate kind. **Deterministic gates** are
auditable: their fixture corpus is the contract. **AI gates** are
statistically auditable only: live-replay + drift alerting.

### Deterministic-tier audit

L0, L1, L2, L6, and custom deterministic gates ship with a fixture
corpus. The L0 corpus at
[`gates/l0/testdata/`](../gates/l0/testdata/) is the v1 contract.
Targets: 200 fixtures by v1.0, broken pass/fail/edge. CI runs the full
corpus on every commit; expected verdict is checked into the repo.

### Statistical-tier audit

L3, L4, L5, and AI custom gates:

- **Golden corpus.** A set of historical PRs (≥30) with maintainer-
  recorded verdicts. Weekly replay job re-runs each AI gate against
  the golden corpus; ≥95% verdict-agreement gate required to ship a
  new model version or prompt revision.
- **VCR fixtures.** Unit tests record canonical model responses for a
  set of synthetic inputs; CI replays them deterministically.
- **Canary catch-rate.** See `gates/canary/testdata/`. The 8 archetypes
  produce known-bad PRs; failure to be caught at the expected layer
  alerts.

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

10-engineer team, Node.js repo, 15 PRs/week average, mid-2026 list
prices, mandatory prompt caching:

| Component | Per PR | Per week | Per month |
|---|---|---|---|
| Agent (Opus 4.7, ~4 iterations, cached) | $0.95 | $14.25 | $61.75 |
| L3 (Opus 4.7) | $0.06 | $0.90 | $3.90 |
| L4 (Sonnet 4.6) | $0.04 | $0.60 | $2.60 |
| L5 (Haiku 4.5) | $0.004 | $0.06 | $0.26 |
| Custom gates (avg) | $0.05 | $0.75 | $3.25 |
| **Total** | **~$1.10** | **~$16.55** | **~$71.75** |

Comparison (mid-2026 list pricing, *unverified*):

- **Devin** — ACU-priced; 1 ACU ≈ $2.25; typical PR = 2–5 ACU =
  $4.50–$11.25/PR.
- **Cursor Business** — $40/seat/month flat; agent usage tiered.
- **Copilot Business + Coding Agent** — $39/seat/month; agent usage on
  consumption pricing.
- **CodeRabbit Pro** — $24/dev/month.

Caveat: numbers compare different things. Devin's price includes the
agent + review + merge; Regatta's includes orchestration + gates but
the agent is a `claude` API session billed separately. Apples-to-apples
on a fully-loaded 10-eng team running 15 PRs/week:

- **Regatta self-hosted** + Anthropic API + 10× Claude Code seats:
  ~$70/PR-equivalent (incl. agent compute).
- **Devin Team** (~$500/mo) + 10× CodeRabbit ($240/mo): ~$50/PR at 15
  PRs/week.
- **Copilot Business** (~$390/mo) + 10× Greptile ($200/mo) + ad-hoc
  agent: ~$40/PR.

Regatta's positioning: **not the cheapest; the only one with a
deterministic immutability gate, a published trap catalog, and a
tamper-evident audit log out-of-band.** See [§Alternatives](#alternatives).

## Failure modes (on-call runbook)

Each row: detection signal → first command → second command →
escalation.

| Failure | Detect | First | Second | Escalate |
|---|---|---|---|---|
| Agent can't pass CI after K iters | session.iteration_count ≥ cap | `regatta logs --work-id N` | `regatta diff --work-id N` | open draft PR + flag human |
| AI reviewer rejects K=3 times | gate_runs.rejection_count ≥ 3 | `regatta gate-history --pr N` | `regatta agent-prompt --pr N` | label `needs-human` |
| Agent gaslights L3 (vacuous test) | L4 finding + canary catch-rate dip | `regatta gate-comment l4 --pr N` | `regatta canary-report --since 1d` | revise L4 prompt |
| Agent edits acceptance criterion | **L0 fails hard** | (none — auto-blocked) | `regatta gate-comment l0 --pr N` | review agent log; if intentional, file FOLLOWUP |
| L4 over-cautious — rejects everything | net-helpfulness <50% rolling 10 | `regatta net-helpfulness l4` | bump severity threshold | swap model |
| Hallucinated package install | L1 fails (lockfile mismatch) + P1 deny | `regatta diff package.json --pr N` | check `pip index versions <pkg>` | block, comment |
| Coverage gaming | L4 finding ("vacuous test"); mutation-test gate if enabled | `regatta gate-comment l4 --pr N` | manual triage | revise L4 prompt |
| L4 prompt-injected via diff | `injection_suspected: true` in L4 result | `regatta gate-result l4 --pr N --raw` | human review of diff | rotate signing key |
| Recursive lesson taint | CODEOWNERS blocks merge | (none — branch-protected) | review lesson PR | maintainer rejects |
| Spec ambiguous / fixture missing | agent halts with "needs-clarification" | `regatta logs --work-id N` | open clarification item | route to maintainer |
| Token budget burned | telemetry.cost_usd ≥ cap | `regatta status --over-budget` | `regatta halt --work-id N` | summarize, alert |
| Two agents collide on hotspot | scheduler lock contention >15min | `regatta locks` | identify loser; reschedule | none — auto-recovers |
| Cross-item build break | L5 consumer-test gate fails | `regatta gate-comment l5 --pr N` | block downstream | upstream contract fix |
| 737-MAX rubber-stamp drift | canary catch-rate <80% | `regatta canary-report` | `regatta canary-recent` | pause merges; review |
| Spec mutated on `main` mid-flight | L0 re-run at merge time fails | `regatta gate-rerun l0 --pr N` | rebase + agent re-evaluates | usually agent halts cleanly |
| Orchestrator broken | heartbeat dead-man-switch | `regatta health` | `systemctl status regatta` | restart; running agents reaped on next start |
| Long autonomous trajectory (GTG-1002 pattern) | anomaly detector on tool-call sequence length | `regatta agent-trace --work-id N` | pause | require fresh approval |
| Force-push or destructive git op | P1 deny + P2 two-key | (none — auto-rejected) | `regatta audit --since 1h` | log + audit sink entry |
| API outage (Anthropic / GitHub) | gate-run timeouts spike | `regatta health --externals` | `regatta serve --pause` | wait for vendor; resume |
| Token expiry (OIDC mint) | gate runs 401 | `regatta token-status` | `regatta token-refresh` | check OIDC provider |
| Webhook drop | adapter event lag | `regatta adapter-lag` | `regatta serve --force-poll` | check webhook subscription |
| Clock skew (orchestrator vs git host) | gate sig verify fails on PR comments | `chronyc tracking` | sync host clock | wait |
| Duplicate PR race | adapter returns same item twice | `regatta locks` | mark one PR `duplicate` | dedupe in adapter |

## Stop conditions (measurable)

Abandon a deployment if **any** of these triggers:

- Canary human-catch-rate <60% over rolling 20-canary window, ≥4 weeks
  post-Phase-3.
- Maintainer review-burden median time-on-PR has not dropped vs.
  pre-Regatta baseline by week 6 (measured by `gh pr view --json
  closedAt,createdAt` aggregations).
- Agent completion rate (PR merged without escalation) <60% across
  rolling 10-PR window for ≥2 consecutive windows.
- Cost per completed item exceeds `safety.spend_cap_usd` consistently
  for 3+ items in a row — gates are too noisy or agent is thrashing.

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

**(e) Swap Claude for another runtime.** *Honest concession.* v3.1
ships Claude only. The cross-vendor adapter (Codex, Gemini, open-
source) is v4 scope and the single most load-bearing v4 deliverable.
Until it ships, treat "runtime-agnostic" as a thesis, not a feature.
Pairs with the next point.

**(f) "Different family" claim at L4.** Sonnet 4.6 and Opus 4.7 are
the same Anthropic Claude 4.x family — they are not different families
in the cross-vendor sense. They are *different checkpoints*, which
reduces some same-prompt response correlation but does *not* close
same-family judge bias. The genuinely cross-family L4 (`gates.adversarial.model:
gpt-5-something`) lands with v4. Until then, treat the bias mitigation
as partial; the published "5–15% same-family bias" figure is a
range often cited in the LLM-judge literature but should be read as
*directional* until benchmarked on Regatta's own canary corpus.

**(g) Vendor the gate stack into each repo.** Rejected — keeps
Regatta independently versionable; lets one deployment serve many
repos; avoids forking gate logic per consumer.

## Competitor positioning

| | Regatta v3.1 | Devin 2.0 | Cursor 3 + BugBot | Copilot Workspace + Coding Agent | Claude Code Agent Teams | OpenHands | CodeRabbit | Gitar |
|---|---|---|---|---|---|---|---|---|
| **Pricing** | Self-host + API at cost | $500/mo + ACUs | $40/seat/mo + usage | $39/seat/mo + usage | API at cost | OSS + API | $24/dev/mo | (stealth, $9M) |
| **Gate transparency** | open, configurable | proprietary | proprietary | proprietary | open via Code | open | semi-open | semi-open |
| **Runtime-agnostic** | thesis (v4) | no | no | no | Claude only | yes | model-agnostic | yes |
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
4. **Multi-repo coordination.** v3.1 = one deployment per repo.
   Monorepo cross-package dependencies handled by single-repo
   `dependencies` graph; cross-repo deferred to v4.
5. **Cross-vendor L4.** When (not if) v4 ships, default L4 swaps to a
   genuinely different family. The benchmark plan: re-run the canary
   corpus on `(Opus, Sonnet)` vs `(Opus, GPT-5)` and publish the
   delta.

## Versioning

`regatta.yaml` carries `version: 1`. The CUE schema is at
`schemas/regatta.v1.cue`. Breaking changes bump major; a migration
tool (`regatta migrate-config --from 1 --to 2`) ships before any v2
release.

`GateResult` has `schema_version: 1`. Same migration discipline.

`SpecAdapter` Go interface uses Go's interface-evolution rules:
breaking changes bump the `regatta` module's major version (semver).

## References

- `incidents.md` — 19 AI-agent incidents + full prose for P1–P10.
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

## v2 → v3.1 changelog (compact)

- **Repo-agnostic.** Generic `SpecAdapter` interface; no
  consumer-repo references anywhere in the doc.
- **Schemas published.** `SpecAdapter` (Go), `GateResult` (JSON),
  `WorkItem` (JSON), `regatta.yaml` (CUE). Normative.
- **Threat model + tamper-evident audit added.**
- **L0 specified normatively** with a fixture-corpus contract.
- **Trap Catalog promoted** to top-level; full prose lives in
  `incidents.md`.
- **Day-1→Day-30 runbook + worked example added.**
- **Costed reference workload added.**
- **Competitor table added** with honest concessions on runtime-
  agnosticism and same-family bias.
- **Failure modes rewritten as on-call runbook** with first/second/
  escalate columns.
- **Stop conditions made measurable** (numeric thresholds + windows).
- **Concurrency state machine specified** (sorted lock acquisition,
  heartbeat lease, idempotency key, crash-recovery scan).
