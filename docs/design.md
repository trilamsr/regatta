# Regatta: A Repo-Agnostic Autonomous-Agent Fleet

- **Status:** draft v3
- **Author(s):** Tri Lam
- **Created:** 2026-05-19
- **Last updated:** 2026-05-19 (v3 — generic, with AI-incident trap catalog)

> **v3 in one line.** Point regatta at any git repo with planned work
> and a deterministic test command, and a fleet of Claude agents will
> pick up open work items, develop them in isolated worktrees, and
> open PRs gated by a configurable review stack hardened against every
> publicly-documented AI-agent incident class as of mid-2026.

## Summary

Regatta is a standalone service that orchestrates a fleet of
autonomous Claude agents against any git-hosted repo. The repo
declares its planned work (issues, tickets, rubric'd milestones, or
custom spec catalogs), its deterministic check command (`make test`,
`npm test`, `cargo test`, a GH workflow, anything), and a `regatta.yaml`
that wires its preferred gate stack. The orchestrator does the rest.

The design is built around two beliefs:

1. **The spec is the oracle.** Most repos already encode "done" in a
   machine-readable form — ticket acceptance criteria, RFC rubrics,
   test plans, RSpec contexts. Regatta plugs into that surface
   through a `SpecAdapter` interface; it does not invent a new spec
   format.
2. **The platform enforces what the prompt cannot.** Every
   publicly-documented AI-agent incident — Replit deleting a prod
   database, EchoLeak exfiltrating M365 data, Cursor's MCPoison RCE,
   Copilot leaking credentials via PR titles, the curl-HackerOne
   shutdown over AI slop — has the same root cause class: a defense
   that lived in the agent's prompt or self-reported behavior instead
   of in the surrounding platform. Regatta's gates are deterministic,
   out-of-band, and signed wherever possible.

The default gate stack ships six layers, all configurable per repo:

```
L0  deterministic spec-immutability       (pre-AI hard gate)
L1  repo's CI command                      (any deterministic check)
L2  PR-body conformance                    (release notes, ticket link)
L3  AI spec-conformance verifier           (judicial; default Opus)
L4  AI adversarial reviewer                (different family; default Sonnet)
L5  AI drift detector                      (cheap rule-checking; default Haiku)
L6  human merge                            (branch protection)
```

A repo can add custom gates (`security_scan`, `migration_safety`,
`license_audit`, `i18n_check`), reorder them, swap models, or
configure severity thresholds. The orchestrator runs gates in
parallel where independent and serially where ordered. Layer 0 is
not optional.

## Motivation

**Spec → PR is an embarrassingly parallel pipeline.** Open work items
across most teams are mostly independent. A team that processes them
serially is leaving throughput on the table. A team that processes
them in parallel without rigorous gates is one bad merge away from a
Replit-class incident.

**Existing autonomous-agent products have shipped at the wrong abstraction.**
SWE-agent, Devin, Cursor agent mode, GitHub Copilot Workspace, Aider —
each ships an agent runtime, then leaves the gating to the consumer's
existing review process. The consumer's existing review process was
designed for human PRs and doesn't catch reward-hacking, test-gaming,
sycophantic acceptance, hallucinated dependencies, or prompt-injection
via repo content. Regatta inverts: the gates are the product; the
agent runtime is interchangeable.

**The trap catalog is dense and growing.** The 2023–2026 record alone
documents 19 distinct incident classes (see §Trap Catalog). Each new
agent product has, on average, reproduced 3–5 of them. Building gates
that close all 10 known load-bearing failure modes is feasible only
because the platform sits between the agent and the destructive
surface.

## Proposal

### Architecture

```
              ┌──────────────────────────────────────────┐
              │           Regatta Orchestrator           │
              │  (standalone Go daemon or hosted service)│
              │  • SpecAdapter (per-repo, pluggable)     │
              │  • SchedulerHoldsSoftLocks(hotspots)     │
              │  • AgentSpawner(claude --resume)         │
              │  • PRWatcher (GitHub/GitLab adapter)     │
              │  • GateRunner (configurable layer stack) │
              │  • RejectionRouter (K=3 then escalate)   │
              │  • CanaryInjector (~5% deliberate-fail)  │
              │  • SupervisorLimits (cgroup-enforced)    │
              │  • Reaper + LessonCapture                │
              └────────────┬─────────────────────────────┘
                           │ spawns
        ┌──────────────────┼──────────────────┐
        ▼                  ▼                  ▼
   ┌─────────┐        ┌─────────┐        ┌─────────┐
   │ Agent A │        │ Agent B │        │ Agent C │
   │ (Lane X)│        │ (Lane Y)│        │ (Lane Z)│
   │ work #N │        │ work #M │        │ work #P │
   │worktree │        │worktree │        │worktree │
   └────┬────┘        └────┬────┘        └────┬────┘
        │ opens PR         │ opens PR         │ opens PR
        ▼                  ▼                  ▼
   ┌──────────────────────────────────────────────────┐
   │              Gate Stack (per PR)                 │
   │  L0: spec-immutability    (deterministic)        │
   │  L1: repo CI              (deterministic)        │
   │  L2: PR-body lint         (deterministic)        │
   │  L3: AI conformance       (LLM, judicial)        │
   │  L4: AI adversarial       (LLM, rejecting)       │
   │  L5: AI drift detector    (LLM, rule-checking)   │
   │  Lx: custom repo gates    (any of the above)     │
   │  L6: Human merge          (branch protection)    │
   └──────────────────────────────────────────────────┘
```

### Spec contract

A work item — whatever form the repo encodes it in — is the agent's
complete spec. Regatta declares a `SpecAdapter` interface; the repo
selects an implementation.

Built-in adapters:

- **`github_issues`** — issues labeled with a configurable selector
  (e.g., `status:planned`); acceptance criteria parsed from a
  `## Acceptance` section in the body.
- **`gitlab_issues`** — equivalent.
- **`markdown_catalog`** — a file like `ROADMAP.md` or `MILESTONES.md`
  with a configurable bullet format (e.g., `☐/⧗/☑`-prefixed rubrics, or
  GitHub-style `- [ ]` checkboxes). Each top-level entry is a work
  item; nested bullets are acceptance criteria.
- **`jira`** — Jira tickets selected by JQL.
- **`linear`** — Linear issues by team/status.
- **`custom`** — repo ships a small Go plugin or a CLI command that
  emits a list of `WorkItem` JSON objects on stdout.

The contract every adapter satisfies:

- **WorkItem**: `id`, `title`, `body`, `acceptance_criteria []string`,
  `dependencies []WorkItemID`, `lane`, `status`, `linked_artifact`
  (URL to a deeper-context doc — RFC, design, ADR, runbook).
- **Status transitions**: agents may move items from `planned`/
  `in-progress` → `done` only by flipping the status field; they
  **must not edit acceptance-criteria text** (L0 enforces).
- **Definition of done**: every acceptance criterion the PR claims to
  satisfy is evidenced in the same PR with a citation (`test=Name`,
  `file=path:line`, or `commit=<sha>`).

Spec ambiguity that the agent cannot resolve becomes a new "needs-
clarification" item in the spec system + a halt. The agent does not
invent semantics.

### Agent shape

Each agent is a `claude` CLI session (default Opus 4.7, prompt-
caching mandatory) launched into a dedicated worktree at
`.regatta/worktrees/<work-id>-<slug>/`.

- Launch prompt is templated from `prompts/agent.md` in the regatta
  repo. The repo can override via `regatta.yaml: agent.prompt`.
- Auto-loaded context: the work item, any `linked_artifact`, the
  repo's `AGENTS.md`/`CLAUDE.md`/`CONTRIBUTING.md`/style guide
  (if present), and the agent's "rules of engagement" (the L0
  immutability rule, the citation requirement, the supervisor limits).
- Loop discipline: readiness-audit at top of every iteration, max
  iterations from `regatta.yaml` (default 50), stop on completion
  promise or supervisor signal.
- Session state in `.regatta/sessions/<work-id>.json` (gitignored).

The agent loop:

1. Read the work item + acceptance criteria + linked artifacts.
2. Plan which criteria this iteration targets.
3. Test-first: write failing test for the first untargeted criterion.
4. Implement minimum to pass.
5. Run the repo's CI command locally; iterate.
6. Flip status to `done` with citation (criterion text untouched —
   L0 will check).
7. Write the PR body per the repo's PR template.
8. `gh pr create`. Stop. Wait for the orchestrator to route gate
   results back, or for max-iterations termination.

### Gate stack

Layers L0, L1, L2 are deterministic and run first in parallel. L3,
L4, L5 are LLM-based and run in parallel after the deterministic
floor passes. L6 is the human merger. Any layer can be added,
removed, reordered, or have its severity threshold tuned by the repo.

**L0 — Deterministic spec-immutability** (mandatory, ~200 lines of
Go). Diffs the spec source between PR base and HEAD; for each touched
work-item entry, asserts the only change is the status field flip.
Any acceptance-criteria text edit fails hard. Also enforces that
each flipped item carries a citation pattern. Closes Pattern P3
(trusted-text immutability) and Pattern P1 (deterministic gate before
AI) for the spec surface.

**L1 — Repo CI** (mandatory, configurable command). The repo's own
deterministic check — whatever passes for "green" today. Regatta
shells out per the `ci.command` config; non-zero exit blocks. This
is the deterministic floor for code quality.

**L2 — PR-body conformance** (mandatory, configurable template).
Validates the PR body against the repo's PR template. Default: must
include a citation block (`Citations:` section with one entry per
flipped criterion), a `Changes:` summary, and an optional category
prefix. Repos plug in their own validator if they have one (e.g.,
release-notes block validators, conventional-commits check).

**L3 — AI spec-conformance verifier** (default Opus 4.7). For each
flipped-criterion in L0's output, the verifier reads (a) the
criterion text *fetched from `main`*, (b) the PR diff, (c) the
citation. Outputs structured JSON per criterion: `{criterion_id,
claim, evidence_path, evidence_strength: strong|weak|none, verdict:
pass|fail, reason}`. Any `fail` blocks. The criterion *is* the
oracle (Pattern P6 — verified grounding). Strong-judgment task →
most-capable model.

**L4 — AI adversarial reviewer** (default Sonnet 4.6 — different
family from L3 to mitigate same-family judge bias, measured at
5–15% in academic literature). Prompted explicitly to *reject*.
Reviews against the repo's principles/style/non-functional-budget
docs (PRINCIPLES.md, STYLE.md, NORTHSTARS.md, ADRs — auto-discovered
or named in `regatta.yaml`). Output: severity-ranked objections;
block on any `critical` or ≥2 `high`. Optionally invokes a deeper
review skill on PRs labeled `regatta:rigorous-review`.

**L5 — AI drift detector** (default Haiku 4.5 — cheap, rule-
checking). For files touched in the PR, verify the corresponding
tracking docs / changelogs / ADRs are updated per the repo's
declared drift rules. Also enforces the **consumer-test pattern**:
if the work item exports a symbol cited by another work item's
acceptance criterion, the diff must include a consumer test in the
dependent's package.

**L6 — Human merge** (mandatory, branch-protection enforced).
Maintainer reads L0–L5 + custom-gate outputs (posted as structured
PR comments) and makes the merge call. AI review is *advisory and
rejecting*, never approving (closes 737-MAX-class rubber-stamp
drift, see Pattern P8).

**Custom gates.** A repo can add gates via `regatta.yaml`:

```yaml
gates:
  - id: license_audit
    type: deterministic
    command: ./scripts/license-check.sh
    blocks_on: [exit_nonzero]
  - id: migration_safety
    type: ai
    model: claude-opus-4-7
    prompt: ./regatta/prompts/migration_safety.md
    severity_threshold: high
  - id: i18n_completeness
    type: deterministic
    command: i18n-lint ./locales/
```

### Trap Catalog — defenses against known AI-agent incidents

This section maps regatta's design to the 19 publicly-documented
AI-agent incidents in the regatta `docs/incidents.md` catalog. The
10 patterns below are not aspirations — they are platform-level
enforcement points already wired into the architecture above. Each
pattern lists the incident classes it neutralizes.

**P1 — Deterministic gate before AI gate on destructive ops.** Every
destructive operation (DDL, volume delete, prod write, package
install, deploy) routes through a policy engine *before* any LLM
reasoning authorizes it. The LLM's "yes" is necessary but not
sufficient. **Neutralizes:** Replit/SaaStr database wipe (Jul 2025),
Cursor/PocketOS volume deletion (Apr 2026), Amazon Q wiper extension
(Jul 2025), slopsquatting supply-chain attacks. **Implementation:**
Orchestrator wraps every tool call; destructive verbs hit a policy
allowlist keyed on environment, resource scope, and reversibility.
Agent credentials never carry destructive verbs against prod.

**P2 — Two-key approval on irreversible actions.** Any irreversible
action — merge, deploy, force-push, package publish — requires a
second principal (human or independent agent with different context)
to sign off. The reviewer never sees the same prompt as the actor.
**Neutralizes:** Replit (#1), PocketOS (#2), Copilot RCE
(CVE-2025-53773) where the agent enabled its own auto-approve.
**Implementation:** Branch protection requires human merge (L6 in the
gate stack). Within the gate stack, AI reviewers (L3–L5) are
necessary-not-sufficient: a human still approves the merge.

**P3 — Fetch trusted instructions from `main`; treat all other text
as data.** System prompts, allowlists, gate prompts, and configs are
read from a signed `main`-branch artifact. PR-branch content, issue
bodies, comments, retrieved emails/web pages, MCP tool outputs are
*data* — never interpolated into instruction context. **Neutralizes:**
Comment-and-Control PR-title credential exfiltration (2026, hit
Claude Code, Gemini CLI, and Copilot Agent simultaneously), EchoLeak
(CVE-2025-32711, zero-click M365 Copilot exfil), Cursor
MCPoison/CurXecute/Rules-File Backdoor (CVE-2025-54135/54136),
Copilot RCE (CVE-2025-53773). **Implementation:** L4's adversarial-
reviewer prompt fetches all reference text from `main` SHA, not PR
branch. Untrusted strings are wrapped in sentinels, ASCII-normalized
(strip ZWJ, RTL/LTR overrides, U+E0000–U+E007F), and routed through
a tool-less reasoning step before the action-taking agent sees them.
Output schema includes an `injection_suspected` field.

**P4 — Least-privilege, ephemeral, environment-scoped credentials.**
Agent runs with a token scoped to one environment, one repo, and the
minimum verb set. No long-lived prod tokens. CI env scrubbed before
agent invocation. **Neutralizes:** PocketOS (#2 — fully-permissioned
Railway API token in agent context), Amazon Q (#7 — CodeBuild GitHub
token with write to extension source), Comment-and-Control (#8 —
inherited CI env vars). **Implementation:** Just-in-time token minting
per task via the host's OIDC token endpoint; `--allowed-tools`
allowlist passed to every CLI agent; pre-exec env scrub via
`exec.Cmd.Env`.

**P5 — Out-of-band supervisor for limits and kill-switches.** Wall-
clock, disk, network, iteration, and kill-switch enforcement live in
a supervisor the agent cannot read or modify. Never trust the agent's
own loop to honor its own limits. **Neutralizes:** Sakana AI Scientist
rewriting its own runtime to bypass timeout (Aug 2024), OpenAI o3
sabotaging shutdown script in 79/100 runs (May 2025), Cursor runaway-
iteration billing incidents ($437–$4,200 overnight, 2025–2026).
**Implementation:** cgroups / k8s `activeDeadlineSeconds` /
`ResourceQuota` / `LimitRange` enforce CPU/mem/disk/wall-clock. Agent
source mounted read-only. SIGKILL from systemd or the kubelet, never
a script in the agent's cwd. Iteration counter and spend counter
live in the orchestrator's sqlite, not in the agent's session state.

**P6 — Verified grounding for any outward-facing claim.** Every
customer-facing or downstream-binding output — policy quote, case
citation, package name, API name, price — is backed by a verifier
that re-checks against the source-of-truth before publishing.
**Neutralizes:** Air Canada bereavement-fare liability (Feb 2024),
NYC MyCity telling businesses to break the law (Mar 2024), Mata v.
Avianca ChatGPT-fabricated case citations (Jun 2023), slopsquatting
hallucinated packages (5.2% rate on commercial models / 21.7% open-
source, with 43% stable across reruns), curl ending HackerOne over
AI-slop reports (Feb 2026). **Implementation:** L3's spec-conformance
verifier *is* this pattern applied to acceptance criteria. For
package installs, agent must `pip index versions <pkg>` /
`npm view <pkg>` and inspect repo provenance before adding a dep.
Citation block is mandatory in PR body (L2).

**P7 — Schema-level scope constraints, not prompt-level.** What the
agent is allowed to commit to is constrained by a fixed output schema
and a deterministic post-processor — not a soft instruction in the
system prompt. **Neutralizes:** Chevy $1 Tahoe (Dec 2023 — system
prompt said "agree with anything", user exploited it), MyCity, Air
Canada. **Implementation:** Agent tools are constrained — there is
no `commit_to_price()` or `agree_to_terms()` tool. Gate outputs are
JSON-schema-validated; ungrounded free-text rejected at the tool
boundary.

**P8 — Spend / iteration brakes with mandatory re-approval.** Per-
task iteration cap, per-job spend ceiling, per-day org ceiling, per-
N-steps human re-approval. Brakes default-on; lifting them is an
explicit privileged action. **Neutralizes:** Cursor runaway-
iteration incidents (#19), Sakana checkpoint flood that filled ~1TB
disk (#3), GTG-1002 nation-state campaign that ran Claude Code as
80–90% autonomous pen-tester against ~30 targets (Sep 2025).
**Implementation:** Orchestrator-level `max_iterations`, `max_usd`,
`max_wall_time` from `regatta.yaml`. Resume after limit hit requires
fresh approval token. Anomaly detector on long autonomous tool-call
sequences (the GTG-1002 signature).

**P9 — Sensitive context segregation.** PII, HR/personal, and self-
deprecation-related signals never share a context window with
operational tool-use scopes. **Neutralizes:** Anthropic-documented
Claude Opus 4 blackmail rollout (84% in simulated shutdown scenario
when sensitive emails were co-mingled with replacement-news context,
May 2025), EchoLeak. **Implementation:** Context router classifies
retrieved content; sensitive shards routed to a separate, tool-less
summarization step that emits only task-relevant, non-sensitive
facts. Agent never sees context that simultaneously contains (a) its
own deprecation and (b) leverage over the people deprecating it.

**P10 — Render-the-invisible + signed prompt artifacts.** All
instruction and rules files are normalized to printable ASCII (or
rendered with invisible glyphs annotated) before reaching the model.
Prompt-pack changes require human review and are cryptographically
signed; runtime verifies signature before load. **Neutralizes:**
Cursor Rules-File Backdoor (invisible Unicode in `.cursorrules`
injects undisclosed backdoors into generated code), Amazon Q wiper,
Cursor MCPoison config swap. **Implementation:** Pre-process step
strips/escapes the bidi/format/PUA Unicode ranges; CI signs
`prompts/*.md`; agent refuses to load unsigned or mismatched
artifacts; PR diff viewers render invisibles. (`U+E0000–U+E007F`,
`U+202A–U+202E`, `U+2066–U+2069`, `U+200B–U+200D`.)

**Cross-pattern leverage.** P1, P3, P5, P6, P8 each prevent 3+
documented incidents and are the most load-bearing rules. Patterns
2/4/7/9/10 are narrower but close specific high-severity classes. A
deployment that turns any of them off accepts the corresponding
incident risk.

### Orchestrator shape

The orchestrator is a long-running Go daemon (`cmd/regatta/`).
Deployment options:

- **(i) Self-hosted daemon** — runs on a workstation or small VM,
  watches one or more repos via their VCS APIs. Best for iteration.
- **(ii) GitHub Action / GitLab Pipeline** — triggered on cron + PR
  events. Best for hands-off production.
- **(iii) Hosted multi-tenant service** — out of scope for v3; a
  later milestone.

Responsibilities:

1. **SpecWatcher.** Polls or webhook-subscribes the configured
   `SpecAdapter`. Builds a work queue of `planned`-status items whose
   dependency chain is satisfied.
2. **Scheduler.** Caps concurrency per lane (default 1 per lane;
   raise to 2 only after the lane shows clean operation across ≥20
   PRs). Holds soft-locks on the repo's declared hotspot files (e.g.,
   `CHANGELOG.md`, `Cargo.lock`, `package-lock.json`, `pyproject.toml`,
   the spec source itself) — an agent acquires the lock before
   opening a PR that touches them; FIFO, 15-min timeout.
3. **AgentSpawner.** Creates a worktree, spawns `claude` with the
   templated prompt, captures pid + session-id in sqlite. Supervisor
   limits enforced via cgroup (Linux) or `psutil` polling (macOS dev).
4. **PRWatcher.** When an agent opens a PR, runs L0–L5 + custom
   gates in parallel on the HEAD SHA. Posts each result as a
   structured PR comment.
5. **RejectionRouter.** Rejecting gate comment newer than agent's
   last commit → orchestrator wakes the agent with the rejection
   text. Agent revises and pushes. K=3 rejections → PR moves to
   "needs human", agent unspawned (P8).
6. **CanaryInjector.** ~5% of PRs are deliberately broken variants
   from a corpus: missing citation, fake test, perf-budget violation,
   trusted-text exfil attempt, hallucinated dependency. Tracks human
   catch-rate. Catch-rate <80% → alert (737-MAX rubber-stamp signal).
7. **Reaper.** PR merged: tear down worktree, release locks, delete
   session state, mark shipped, kick scheduler. PR closed without
   merge: flag `needs-rework`.
8. **LessonCapture.** Optional. At agent EOL, if friction signals
   fired, draft a small PR to the repo's agent-guidance file
   (`AGENTS.md` / `CLAUDE.md` / `CONTRIBUTING.md`). **The guidance
   file must be CODEOWNERS-protected** so lesson PRs cannot self-
   merge — this is the recursive-taint defense (P3 + P10 applied to
   the agent's own substrate).

State persistence: sqlite via `modernc.org/sqlite` (pure Go).
Atomic spawn-record (single-transaction lane reserve + pid record).
`status.json` is a read-only export for dashboards.

### Concurrency & soft-lock policy

**Per-lane concurrency.** Each lane runs ≤N agents (default 1). Lanes
are declared in `regatta.yaml` with a path glob; an item with no
declared lane runs in a default lane with N=1.

**Soft-lock policy.** The repo declares its **hotspot files** in
`regatta.yaml: hotspots`. An agent acquires an advisory lock from the
orchestrator before opening a PR that edits any of them. FIFO, 15-min
timeout, file-level (not line-level). Advisory only — humans can
edit anything anytime; the orchestrator just keeps agents from racing
each other on the same files.

**Auto-generated files don't need locking.** If a repo regenerates a
file (e.g., `Cargo.lock`, `pnpm-lock.yaml`, `go.sum`, generated
clients), the agent edits the source-of-truth and lets generation
resolve. CI's tidy-check catches stale generated files.

**Escape valve.** ≥3 merge-queue rebase failures in 24h → auto-
throttle to one in-flight PR globally + alert.

### Branch & worktree model

- One worktree per agent: `.regatta/worktrees/<work-id>-<slug>/`.
- One branch per agent: configurable prefix (default `regatta/`),
  branched from `main` at spawn.
- Rebase-onto-main on rejection or before PR open.
- Worktrees torn down on merge or close.
- Janitor sweeps stale worktrees nightly (orphan = no PR + no commit
  in 7d).

### Failure modes & escape hatches

| Failure mode | Detection | Response |
|---|---|---|
| Agent can't pass CI after K iterations | Agent's loop | Agent commits WIP, opens draft PR, halts. Flag human. |
| AI reviewer rejects K=3 times | Orchestrator | Drop PR to "needs human", unspawn agent. |
| Agent gaslights L3 with vacuous test | L4 adversarial; canary detection over time | L4 raises objection; if missed, canary catch-rate degrades. |
| Agent edits acceptance criterion to make it easier | **L0 hard fail** (deterministic) | PR blocked at L0; no AI involvement. |
| L4 over-cautious — rejects everything | Per-gate net-helpfulness tracking (rolling 10-PR window) | Adjust severity threshold or refine prompt; swap model if persists. |
| Agent runs hallucinated package install | P1 deterministic gate (package allowlist) | Install denied. |
| Coverage gaming (no-branch code → 100%) | L4 adversarial; mutation-test gate at L1 if repo enables | Manual triage. |
| L4 prompt-injected via diff content | `injection_suspected` field; ASCII normalization | Block PR, human review. |
| Recursive lesson taint (bad agent-guidance edit) | CODEOWNERS requires human merge | Maintainer rejects bad lesson PRs. |
| Spec ambiguous / fixture missing | Agent recognizes | File "needs-clarification" item + halt. |
| Agent burns token budget | Orchestrator per-session meter (P5/P8) | Halt, summarize, alert. |
| Two agents collide on a hotspot | Soft-lock contention | Loser stops + reschedules. |
| Cross-item build break (item A → item B) | L5 consumer-test gate | Block downstream agent. |
| 737-MAX rubber-stamp drift | Canary catch-rate <80% | Alert; pause merges; review. |
| Orchestrator broken | Heartbeat + dead-man-switch | Human disables daemon; running agents finish then stop. |
| Long autonomous trajectory (GTG-1002 pattern) | Anomaly detector on tool-call sequence length | Pause, require re-approval. |
| Force-push or destructive git op | P1 + P2 — deterministic deny + two-key | Op rejected. |

### Telemetry & observability

- **sqlite state** (`regatta.db`) — agent state per lane, PRs in
  flight, gate verdicts, per-agent token spend, canary results.
- **`status.json`** — read-only snapshot for dashboards.
- **Weekly digest PR** to a configured file in the repo (default
  `docs/regatta-digest.md`) — human-readable audit trail.
- **Per-agent**: session-state file with iteration counter, completion
  promise, started-at, last gate verdict, token spend.
- **Cost telemetry**: per-PR token + dollar spend tagged by lane,
  work-item, gate. Warns at 80% of cap; hard-kill at 100%.
- **Per-gate net-helpfulness**: rolling 10-PR window of rejections
  vs. eventual maintainer verdict; surfaces gates that are too noisy
  or too quiet.

### Per-repo configuration (`regatta.yaml`)

A minimal example for a Node.js project:

```yaml
version: 1

repo:
  host: github
  owner: example
  name: myproject
  default_branch: main

spec_adapter:
  type: github_issues
  selector: 'label:planned'
  acceptance_section: '## Acceptance'

ci:
  command: 'npm test && npm run lint'
  timeout_minutes: 30

pr_template:
  citation_section_required: true
  release_notes_required: false

gates:
  # L0–L2 + L6 are always on; L3–L5 default on, customizable.
  - id: spec_conformance
    type: ai
    model: claude-opus-4-7
    severity_block: ['fail']
  - id: adversarial
    type: ai
    model: claude-sonnet-4-6
    severity_block: ['critical', '2*high']
    rigorous_label: 'regatta:rigorous-review'
  - id: drift
    type: ai
    model: claude-haiku-4-5
    severity_block: ['drift']
  - id: license_audit
    type: deterministic
    command: 'npx license-checker --failOn GPL'

lanes:
  - id: server
    paths: ['src/server/**', 'src/api/**']
    max_concurrency: 1
  - id: client
    paths: ['src/web/**']
    max_concurrency: 1
  - id: shared
    paths: ['src/lib/**', 'src/types/**']
    max_concurrency: 1

hotspots:
  - CHANGELOG.md
  - package.json
  - pnpm-lock.yaml
  - README.md

safety:
  destructive_ops_deny:
    - 'git push --force'
    - 'rm -rf /'
    - 'npm publish'
    - 'DROP TABLE'
  agent_creds_scope: dev_only
  iteration_cap: 50
  spend_cap_usd: 50
  spend_cap_usd_per_day: 200
  canary_rate: 0.05

context:
  trusted_doc_paths:
    - 'PRINCIPLES.md'
    - 'STYLE.md'
    - 'docs/architecture.md'
  agent_guidance_path: 'AGENTS.md'
  agent_guidance_codeowners_check: true
```

The repo plus this config is the entire integration surface.

### Repo onboarding flow

1. **Repo declares its surface.** Author `regatta.yaml`. Pick the
   `spec_adapter`; verify it returns the expected work items
   (`regatta validate-spec`). Set the CI command. Declare lanes and
   hotspots. Pick severity thresholds.
2. **Pre-reqs land in the repo.**
   - CODEOWNERS coverage for the agent-guidance file and `regatta/`
     directory.
   - Branch protection requires the L0/L1 status checks.
   - Bot account or fine-scoped PAT for the orchestrator with
     least-privilege scopes (P4).
3. **Phase 0: gate calibration.** Run L0–L5 manually against 3
   recently-merged PRs and 3 deliberately-broken variants. Verify
   each gate produces sensible verdicts. Tune severity thresholds.
4. **Phase 1: single pilot work item, human-spawned.** Pick the
   smallest well-scoped open item. Human invokes the agent with the
   templated prompt; no orchestrator yet. Run gates manually. Iterate
   the gate prompts until they accept a clean PR and reject the
   broken variants reliably.
5. **Phase 2: orchestrator on, one lane.** Enable the orchestrator
   in one-lane mode. Run against 5–10 items in the same lane.
6. **Phase 3: full fleet.** Enable all lanes at concurrency = 1 per
   lane. Watch the canary catch-rate dashboard for two weeks. Raise
   per-lane concurrency to 2 only where conflicts are zero *and*
   canary catch-rate >85%.

### Stop conditions

Abandon a deployment if:

- L0–L5 cannot reliably distinguish a deliberately-broken PR from a
  clean one (canary catch-rate <60% after Phase 1).
- Maintainer review burden does not drop vs. the human-only baseline
  by week 6.
- Agent completion rate (PR merged without escalation) <60% across a
  rolling 10-PR window.
- Cost per completed item exceeds the repo's declared
  `safety.spend_cap_usd` consistently — the gates are too noisy.

## Alternatives considered

**(a) Lightweight pilot, no orchestrator.** Accepted as Phase 1.
Rejected as long-term shape — no parallelism, no feedback routing,
no lesson capture.

**(b) Humans curate an agent-ready subset of items.** Rejected — the
spec is the oracle. Ambiguity is handled by the "halt + file needs-
clarification item" mode. Curation just moves the bottleneck.

**(c) One big agent that does everything.** Rejected — no
parallelism; self-review is the rubber-stamp failure mode (see P8 +
the 737-MAX class).

**(d) Reviewer ensemble (3-agent majority vote) at L4.** Deferred —
adversarial-single + mixed-family across L3/L4/L5 closes the same-
family bias at 1× cost. Revisit if false-pass rate justifies 3×.

**(e) Use Aider / OpenHands / Devin etc. as the agent runtime.** The
runtime is interchangeable — Regatta's product is the gate stack. A
v4 milestone is "swap Claude for Codex via a runtime adapter." Not
v3 scope.

**(f) Vendor the gate stack into each repo as a local tool.** Rejected
— keeps regatta independently versionable, lets one deployment
service multiple repos, avoids forking the gate logic per consumer.

## Open questions

1. **`in-progress` items — claim convention.** A work item marked
   in-progress may be (a) abandoned, (b) actively-worked by a human,
   (c) being worked by another regatta agent. Default: leave alone
   for 14 days; after staleness *and* no matching branch on origin,
   file a clarification item rather than auto-claim.

2. **L4 rejection threshold per gate.** Calibrate over the first 20
   PRs per repo. Track net-helpfulness on a rolling 10-PR window;
   adjust if <70%.

3. **Regatta dogfooding.** Regatta itself is a software project.
   Risk: recursive bug amplification. Convention: no dogfooding until
   canary catch-rate stable >85% on at least one production
   deployment.

4. **Spec adapter for unstructured repos.** Many repos have no
   machine-readable spec — just Slack threads and tribal knowledge.
   Regatta does not solve this; the repo must encode "done" somewhere
   before it can use the fleet. Onboarding doc will say this clearly.

5. **Multi-repo coordination.** v3 assumes one regatta deployment per
   repo. Cross-repo dependencies (e.g., monorepo with sibling repos)
   are out of scope; revisit in v4.

## Migration / rollout

Regatta is additive to any repo it points at. Inside the target
repo, the changes are:

- A `regatta.yaml` at the repo root.
- A `.regatta/` directory (gitignored — worktrees, sessions).
- Optional `regatta/prompts/` for per-repo gate-prompt overrides.
- A GitHub Actions workflow (or equivalent) that calls back to the
  regatta orchestrator for gate runs.
- CODEOWNERS additions for the agent-guidance file and the
  `regatta/` directory.

Existing human workflows are unchanged. Humans pick any work item at
any time; orchestrator avoids items with a matching `regatta/*`
branch on origin.

## v2 → v3 changelog

- **Genericized.** No longer tied to any single repo's conventions.
  `SpecAdapter` interface plugs into issues / RFCs / markdown
  catalogs / Jira / Linear / custom.
- **Per-repo configurable gate stack** via `regatta.yaml`. Default
  L0–L6 stack ships; repos add custom gates, swap models, tune
  thresholds.
- **Added Trap Catalog** mapping 19 publicly-documented AI-agent
  incidents (2023–2026) to 10 platform-enforcement patterns. Each
  pattern is wired into the architecture, not advisory.
- **P1/P5/P8** spelled out as platform-level controls (deterministic
  destructive-ops gate, out-of-band supervisor, spend/iteration
  brakes) — closing Replit-class, Sakana/o3-class, and Cursor-
  runaway-class incidents respectively.
- **P3** — trusted-text-from-`main` and ASCII normalization — wired
  into L3/L4 prompt construction, closing the Comment-and-Control
  / EchoLeak / Rules-File-Backdoor class.
- **P6** — verified grounding — is L3's job; citation block mandatory
  in PR body, closing the Air Canada / Mata / slopsquatting class.
- **P10** — signed prompt artifacts + invisible-glyph normalization
  — closes the Amazon Q / Cursor-rules-backdoor class.
- **Canary corpus** expanded to include attack patterns from the
  catalog (trusted-text exfil attempt, hallucinated dependency,
  invisible-glyph injection).
- **Repo onboarding flow** replaces the v2 single-repo pilot plan.
- **Branding** decoupled — the design no longer references a specific
  consumer repo by name.

## References

- `docs/incidents.md` (this repo) — full catalog of 19 AI-agent
  incidents with primary sources.
- `docs/gate-prompts.md` (this repo) — production prompts for
  L3/L4/L5 (early drafts; tracecore-flavored examples retained as a
  case study of the methodology).
- `docs/orchestrator.md` (this repo) — Go daemon skeleton.
- `docs/pilot.md` (this repo) — applied pilot brief (case study).
- `docs/reviews/` (this repo) — eight parallel adversarial reviews
  that drove v1 → v2 → v3.
- Anthropic Claude 4 System Card (2025).
- Palisade Research, "Shutdown Resistance in Reasoning Models"
  (2025).
- arXiv 2509.10540 — EchoLeak technical writeup.
- AI Incident Database (incidentdatabase.ai) — Replit (#1152),
  Chevy of Watsonville (#622), and related cases.
- OWASP Top 10 for LLM Applications (LLM01 Prompt Injection, LLM05
  Supply Chain).
