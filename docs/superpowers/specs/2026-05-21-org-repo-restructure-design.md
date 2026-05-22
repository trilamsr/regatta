# Regatta repo restructure — design spec

Reader: internal engineer + (future) security auditor under NDA.
Read time: 20 minutes.
Status: Draft, pending implementation plan.
Scope: this Regatta repo (closed-source product). Not OSS, not an umbrella org repo.
Expires when: Wave 3 ships OR Section 5 activation trigger fires (second human, first customer).

This spec encodes the operating principles, tree-shape principles, code
principles, doc principles, governance principles, and migration plan
that the restructure must satisfy. It is principles-first, not
literal-tree-first: any layout that satisfies the principles is
acceptable.

The five preceding section headers (1 Operating principles, 2 Tree-shape,
3 Code, 4 Docs, 5 Governance) define WHAT good looks like; Section 6 is
the HOW for moving the current tree there in three waves.

## 1. Operating principles

Six rules drive every concrete move in later sections. Each derives
from existing repo `PRINCIPLES.md` / `STYLE.md` / `AGENTS.md`; this
section consolidates and adapts them to closed-source posture.

1. **Audience layering.** Customer-operator > internal-engineer >
   security-auditor (under NDA). Root README answers "what / how to
   run" in <60 seconds. `docs/` answers "is this safe / how is it
   built." `internal/` and `CONTRIBUTING.md` answer "where do I add
   code." `CONTRIBUTING.md` is reframed for internal-engineer
   onboarding, not external-contributor outreach.

2. **Boundaries match gate-stack vocabulary.** Directory names use the
   product nouns: *adapters, gates, programs, orchestrator, audit,
   config, canary, tenant, prompts*. No invented categories. No
   grab-bag packages (`util`, `helpers`, `common`, `models`).
   (PRINCIPLES #8 names earn their slot.)

3. **Contracts colocate.** JSON Schema, CUE, exported Go interfaces,
   signed prompts, and wire-protocol docs are all contracts with
   operators or plugin authors. They live under one `contracts/`
   surface. Operator audit answer to "what does Regatta promise?" is
   one directory.

4. **Plugin seams = reference implementation + protocol doc.** Each
   extension point ships a wire-protocol doc plus one runnable
   reference impl that serves as falsifying consumer. Empty
   plugin dirs are ceremony without a consumer (AGENTS.md load-bearing
   lesson #4).

5. **PR purity.** Governs *change content*, NOT *PR atomicity*.
   Every change inside a PR is one of: feature, enhancement,
   root-cause bugfix, refactor, doc, chore. Workarounds are
   discouraged. A workaround is acceptable only when root cause is
   identified and demonstrably unfixable (upstream dep, platform
   limit, etc.); workaround changes declare the unfixable root cause
   inline in the commit body or PR description.

   Bundled multi-purpose PRs are allowed at solo scale (Section 5
   v4) — each change-set inside the bundle still satisfies this
   content rule. The rule is "no workaround masquerading as a fix,"
   not "one PR one purpose."

   Enforcement: PR template carries a Root-Cause / Workaround
   field. The pr-lint workflow (Wave 1 deliverable; see §6 Wave 1
   automation) validates the field is non-empty when the
   release-notes category is `[BUGFIX]`. Until that workflow lands,
   this rule is advisory.

6. **Comments answer WHY, not WHAT.** Default is no comment. One line
   where possible. Add a comment only when the WHY is non-obvious
   (hidden constraint, invariant, surprising behavior, workaround
   pin). Names + tests document WHAT (PRINCIPLES #7).

Closed-source posture: every adoption decision filters through
"does this help a paying operator + an auditor under NDA?" — not
"does this help open-source community visibility." OSS-flavored
ceremony (Code of Conduct, public bug-bounty channels, OpenSSF
Scorecard badge, public ROADMAP) is dropped or deferred to named
activation triggers.

### External best practices adopted

- Conventional Commits + DCO sign-off (drives auto-generated
  CHANGELOG).
- Keep-a-Changelog shape at `CHANGELOG.md` (SemVer per PRINCIPLES
  #11).
- ADRs (Michael-Nygard template) at `docs/rfcs/NNNN-title.md`,
  append-only, one decision per file.
- SLSA-aware build provenance and tamper-evident audit log
  (mechanisms, not OSS badging).

### External best practices rejected (with reason)

- `pkg/` separate directory (PRINCIPLES #6 — `internal/` first;
  contracts in `contracts/`).
- Monorepo tooling (Bazel/Nx/Lerna) — single Go module, no polyglot
  subprojects. Re-evaluate at Phase 3 P3.5.
- OpenSSF Scorecard badge — repo is private; badge is ceremony
  without consumer.
- Public Code of Conduct — solo maintainer until second human joins.

## 2. Tree-shape principles

Not a literal tree spec. Rules any acceptable layout must satisfy.

### P1. One noun, one home

Every concept named in `docs/design.md` (adapter, gate, program,
orchestrator, audit, config, canary, tenant, prompt) maps to exactly
one directory. No concept lives in two places. No directory hosts two
concepts.

Falsifier: `grep -r '<noun>' --include='*.go' | dirname-uniq` returns
one path per noun.

### P2. Layout mirrors gate-stack vocabulary

Directory names equal product nouns. No grab-bag names. If a new dir
name doesn't appear in `STYLE.md` or `docs/design.md`, it is an
invented category — reject or document first.

### P3. Contracts colocate; implementations scatter

Anything operators read, implement against, or sign against (schemas,
exported Go interfaces, prompts, wire-protocol docs) lives in ONE
contracts surface. Implementations (Go packages that satisfy
contracts) live wherever domain grouping puts them.

### P4. Internal-first, promotion by decision

Default visibility is `internal/`. Promotion to operator-facing
surface (contracts, exported Go API, plugin protocol) requires an
ADR. Promotion is a one-way ratchet; demoting later is a breaking
change. Closed-source intensifier: every promotion is a support
burden plus a competitive surface.

### P5. Plugin seam = reference impl + protocol doc, colocated

Each extension point ships a wire-protocol doc (in `contracts/wire/`)
and one reference impl that compiles and runs against that doc as a
falsifying consumer. Empty extension-point dirs forbidden.

### P6. Test fixtures consolidate; impls scatter

Fixture corpora live under one root. Currently fragmented across
`gates/l0/testdata`, `gates/canary/testdata`, `gates/security/testdata`,
`internal/program/testdata` — pick one root.

Trade-off accepted: Go convention puts `testdata/` adjacent to the
package. Overridden here because corpora ARE the contract per
`docs/design.md` ("fixture-corpus contracts are normative"); they
outrank package locality.

### P7. Audience layering at root

Top-level files split into three bands:

- Customer-operator: README, quickstart entry points.
- Engineering-internal: contributor onboarding, style, principles,
  agent brief, changelog, commit discipline.
- Auditor: security posture, third-party notices, license,
  threat-model entry.

Each band is one read-order. Cross-references between bands stay
one-directional (operator → engineer → auditor depth).

### P8. Depth ceiling

Max 3 dirs deep under `internal/`. Deeper = sign the package is doing
too much; split horizontally instead of vertically.

### P9. New directory needs falsifying consumer

Before creating any new dir: name the test, build target, lint rule,
or workflow that would notice if the dir were empty. No consumer is
ceremony (AGENTS.md load-bearing lesson #4).

### P10. Renames pay their cost up-front

Restructure happens in one wave per migration boundary, not drip.
Bundle moves into one PR per wave with one CHANGELOG entry. Closed-
source posture: no external import paths to break, so cost is
internal-only (CI updates, doc links, AGENTS.md anchors).

## 3. Code principles

### Decision priority (read top-to-bottom on every code call)

When two rules collide, higher wins:

1. **Customer impact.** Does this make the operator's life easier or
   harder? Optimize the gaps the operator lives in: CLI output, error
   messages, config surface, PR comments, audit-log queries, upgrade
   path, cost transparency.
2. **Trust under fire.** Will this misbehave on a repo we don't own?
   Deterministic before AI; two-key on irreversible; fail-closed on
   ambiguity. (PRINCIPLES #1.)
3. **Reproducibility.** Same input → same output a year out.
   (PRINCIPLES #12.)
4. **Correctness over cleverness.**
5. **Readability for next reader.**
6. **Standardization.** Match existing patterns; new pattern requires
   ADR.
7. **Extensibility.** Contracts stable; impls swappable.
8. **Reuse past rule-of-three.** Two = leave. Three = consider
   extract.
9. **Performance.** Last; profile before optimizing.

### Customer-impact surface (priority 1 expanded)

The customer-operator never opens a `.go` file. Their experience
surface is these gaps; code shape rules below follow from them:

- `regatta init` output: adapter-tier and degraded-mode warnings
  clear; refuses-to-proceed line unambiguous.
- `regatta validate-config` errors: cite the CUE rule and the yaml
  line; suggest the fix.
- `GateResult` PR comments: L6 reviewer parses verdict + rationale +
  cited evidence in <5 seconds. No wall-of-text.
- Audit-log query: compliance grep yields answers; field names
  structured and stable.
- `regatta.yaml` schema: smallest viable surface; defaults are the
  norm; advanced knobs gated behind clearly-named sections.
- Failure modes: every halt produces a single-line headline plus a
  runbook URL. No silent halt.
- Upgrade path: `regatta migrate-config --from N --to N+1` works
  without manual hand-edit.
- First-run latency: <60s from binary download to validated config.
- Cost transparency: spend cap honored; audit-log line per model
  call; cost summary at PR close.

Rule that follows: every customer-visible string (error, log line,
PR-comment template, CLI help text) is reviewed for clarity. Strings
ARE the product for the operator.

### Standardization rules (priority 6)

- **S1.** One mechanism per concept (PRINCIPLES #3).
- **S2.** Mirror existing patterns before inventing.
- **S3.** Naming is part of the standard: verb-noun for functions;
  lowercase-single-word packages; `Test_<func>` or
  `Test<Type>_<method>`; fixture files `<case>.json` with sibling
  `<case>.expected.json`.
- **S4.** Public-facing artifacts have a fixed template (RFC, PR
  body, `GateResult`, handoff, program_brief, release-notes block).

### Extensibility rules (priority 7)

- **E1.** Plugin equals stable wire protocol plus reference impl.
- **E2.** Versioned schemas; evolution via version bump plus
  migration tool. WE eat the migration cost, not the customer.
- **E3.** Hook points are explicit, not implicit. No reflection-based
  discovery. No "drop a binary in this magic path."
- **E4.** Defaults swappable but real. Visible in code and auditable.

### Reuse rules (priority 8)

- **R1.** Rule of three.
- **R2.** Cross-package reuse goes through contracts.
- **R3.** Reuse means SAME concept, not similar shape.
- **R4.** Reusable code lives behind a tight interface.
- **R5.** Helpers stay near the caller until promoted.

### C-rules (code shape, unchanged from PRINCIPLES + STYLE, restated for cohesion)

- **C1.** Function = one job; name says it.
- **C2.** Errors are part of the API; typed sentinels; document
  exported errors.
- **C3.** Contracts tested at the boundary against the fixture
  corpus, not the impl's internals.
- **C4.** No `util/` `helpers/` `common/` packages.
- **C5.** Comments WHY, not WHAT; one line where possible.
- **C6.** Doc-comments on exported symbols only when non-obvious
  beyond signature.
- **C7.** Size caps: function <50 LOC body; file <400 LOC; package
  <1500 LOC; cyclomatic complexity ≤15.
- **C8.** Concurrency explicit; ctx plumbed; goroutine ownership
  documented.
- **C9.** Determinism testable: injected `Clock`, `rand.Source`;
  sorted-key serialization; sorted filesystem walks.
- **C10.** Public surface follows the deprecation cycle (PRINCIPLES
  #11).
- **C11.** Tests first-class; no mocking contracts; integration
  tests with `//go:build integration`; e2e at `cmd/regatta`.
- **C12.** Structured logging plus audit-aware second channel; never
  log secrets, full prompt bodies, or customer source lines.
- **C13.** Shallow imports; stdlib first; each direct dep justified.
- **C14.** Irreversible action double-gated: deterministic
  precondition plus two-key or human-in-loop.
- **C15.** Closed-source: no decorative public surfaces. No
  `// Example_*` for godoc; no README badges.
- **C16.** No deferred debt in commits: every `TODO|FIXME|XXX` cites
  an issue or has a PR up within 7 days. Otherwise fix it in the
  commit. (Cross-link with D16.)

## 4. Documentation principles

Apply to every doc Regatta ships.

### D1. Every doc has one named reader

Top of doc states "Reader: <role>. Read time: <minutes>." If three
audiences fit, the doc is two docs.

### D2. Doc earns slot via falsifying consumer

Name the workflow, link-check, build, or human ritual that would
notice if the doc were deleted. No consumer is ceremony.

### D3. Source of truth, not parallel restatement

Each fact lives in exactly one doc. Other docs link, not restate.
When the fact is in code or config, prose cites file:line.
(PRINCIPLES #5.)

### D4. Falsifiable claims only

Every assertion is testable, dated, or labeled aspirational. Banned:
marketing language ("battle-tested", "production-grade",
"industry-leading"). Doc-check gate enforces.

### D5. Length is a cost

- README <800 lines.
- Each operator guide <300 lines.
- ADR 1-2 pages (Michael-Nygard shape).
- AGENTS.md ≤200 lines (already enforced).
- Per-package doc only when API non-obvious from code; <50 lines.

### D6. Read order over reference order

Customer-operator docs read top-to-bottom in <60s for first decision.
Reference material lives below or in subdirs. Auditor docs invert:
pinned ToC, deep-link friendly.

### D7. Code blocks are the contract; prose is the gloss

Minimal `regatta.yaml` example beats prose describing the schema.
Operator's `examples/minimal/regatta.yaml` is the canonical example;
docs link rather than embedding stale copies.

### D8. ADRs append-only, one decision per file

`docs/rfcs/NNNN-title.md`. Status: proposed | accepted | superseded.
Once accepted, never edit. Superseding = new file links back.
Numbering monotonic, never reused.

### D9. Customer-facing docs versioned with the product

`docs/operator/` ships in lockstep with the binary. Breaking config
change = doc update in same PR or PR fails. Aspirational features
labeled with target milestone.

### D10. Doc is not a substitute for code

Hierarchy: code > test > config > lint > workflow > prose. Prose only
when none of the above carries the rule. (PRINCIPLES #5.)

### D11. Closed-source posture = audit-friendly, not SEO-friendly

No external linkbait headlines. No think-pieces. Customer-operator
pitch lives in README quickstart; everything else aims at the
auditor under NDA.

### D12. Every doc states its expiration trigger

Top-of-doc single line ("Expires when…"). Doc-check warns when
trigger fires. (PRINCIPLES #15.)

### D13. Cross-doc links are typed

Link prefix shows reader the target's nature: `→ contract:` for
normative schema/interface/prompt; `→ code:` for impl reference
(file:line); `→ runbook:` for operator action; `→ adr:` for decision
record; `→ research:` for unedited investigation.

### D14. Doc-set has a single index

`INDEX.md` points at every doc. New doc not in INDEX = doc-check
fails. `AGENTS.md` remains the agent-internal index.

### D15. Customer onboarding is the load-bearing doc

If only one doc were maintained, it would be operator quickstart.
Monthly CI runs the quickstart end-to-end against a fresh container.

### D16. No deferred debt

If during a change you spot something fixable, fix it in the same PR.
No `// TODO`, no follow-up backlog, no "later." Scope of "fixable":
wrong name → rename; dead comment → delete; untested error path →
add test; doc out of sync → update; lint exception → resolve;
missing ADR for a decision just made → write it; duplicated logic
crossing rule-of-three → extract.

Scope of acceptable defer: change > 50 LOC breaking PR atomicity
(file an issue + cite in PR); schema/contract change (ADR first);
blocked on customer/operator action (label + ADR with trigger).

CI scan enforces: every `TODO|FIXME|XXX` cites an issue or has a PR
up within 7 days. (Cross-link with C16.)

## 5. Governance principles (solo + velocity edition)

Closed-source + single-contributor. Filter every governance item:

1. Does it hinder velocity? (Manual fields, prompts, waits,
   ceremony.)
2. Is it fully automated and seamless? (Hook fires silently, lint
   corrects in-place, CI generates the artifact.)

If hinders AND not seamless → drop, with named activation trigger.

### Distilled solo-velocity governance

**Kept (zero or near-zero friction):**

- Gate stack runs on every push (CI; invisible).
- Trunk-based; branches named `<type>/<short-slug>`; max lifetime 5
  days as advice not policy.
- Branch protection: no force-push to `main`, no skip-hooks
  (server-enforced; passive).
- Forward-only compliance (passive).
- CODEOWNERS file present as scaffold; no required-review.

**Kept conditional on automation existing (build automation as part
of restructure):**

- Conventional Commits: commit-msg hook validates + suggests fix.
- DCO sign-off: prepare-commit-msg hook auto-appends.
- Release-notes block in PR: pr-lint already validates.
- CHANGELOG: auto-generated at release from Conventional Commits.
- No-deferred-debt: CI lint scans `TODO|FIXME|XXX` and fails if any
  line >7 days old without an issue link.

**Kept rare-and-cheap (manual but rare):**

- ADR for: `internal/` → `contracts/` promotion; schema version
  bump; default model/gate/audit-sink change; post-incident rule
  change.

**Dropped at solo scale (named activation triggers):**

| Dropped | Activation trigger |
|---|---|
| One PR one purpose | Second human OR PR volume > comfortable self-review |
| Mandatory PR reviewer | Second human |
| Two-key on irreversibles | Second maintainer + `contracts/` PR rate >1/mo |
| Issue-link on every PR | Issue volume >1/wk from non-self |
| Scheduled release cadence | First paying customer |
| Code of Conduct | Third human |
| Public CONTRIBUTING.md framing | First external PR |
| Per-process ADR | Multi-team |
| Atomic-commits as rule | Second human |

Each = one-line ADR plus branch-protection flip when trigger fires.

### What stays load-bearing for solo

1. Gate stack on every push.
2. Trunk-based + short branches.
3. Conventional Commits + DCO (commits inside PR).
4. PR body: 1-line What + release-notes block.
5. Branch protection: no force-push, no skip-hooks, gates required.
6. ADR for `contracts/` promotion + schema version + default change
   + post-incident.
7. CHANGELOG-driven releases.
8. Forward-only compliance.
9. No-deferred-debt (D16/C16).

### Agent-authored commits

Treated identically to human commits. Same Conventional Commits +
DCO + gate stack discipline. No fast lane. (AGENTS.md load-bearing.)

### Incident response (G13)

When an incident happens, file: post-mortem at
`docs/engineer/post-mortems/YYYY-MM-DD-<slug>.md`; ADR if response
includes a rule change; fixture added to relevant gate corpus so the
next attempt fails the gate. Post-mortems internal-only.

## 6. Migration plan — three waves

Each wave = one PR, green CI, mergeable on its own. Rollback = revert
the PR. No partial state across waves. Adopt-when-needed applies: a
wave whose justification doesn't hold mid-flight gets cut.

### Wave 1 — Foundation

Tree moves + contracts surface + baseline docs + baseline automation.
Everything pure-mechanical or doc-only.

**Tree moves (P1+P2+P6):**

- `schemas/` → `contracts/schemas/` (JSON + CUE) +
  `contracts/go/` (Go interfaces) + `contracts/prompts/` (new home
  for `planner.md`, `security_gate.md`, etc.).
- `gates/{l0,canary,security}/testdata/` →
  `testdata/gates/{l0,canary,security}/`.
- `internal/program/testdata/handoffs/` → `testdata/programs/handoffs/`.
- `internal/l0` → `internal/gates/l0`.
- `internal/securitygate` → `internal/gates/security`.
- `internal/validateconfig` + `internal/verifyrepo` →
  `internal/config` (single package).
- `internal/program` → `internal/programs`.
- Delete top-level `gates/` once impls + testdata moved.
- Placeholder READMEs in earned-but-empty dirs (`internal/cli`,
  `internal/audit`, `internal/tenant`, `internal/canary`,
  `internal/modelclient`) — one paragraph plus activation trigger
  per P9.

**Contracts surface (P3):**

- `contracts/wire/spec_adapter_jsonio.md` extracted from
  `docs/design.md` §Spec contract.
- `contracts/wire/custom_gate_jsonio.md` extracted from
  `docs/design.md` §Custom gates.
- `contracts/README.md` — index + versioning policy + deprecation
  cycle.
- `docs/rfcs/` dir + `0000-template.md` (Michael-Nygard).
- `docs/rfcs/0001-contracts-surface.md` — records the promotion.
- `docs/design.md` updated to link `contracts/wire/` rather than
  restate (D3 dedupe).

**Top-level docs scaffold:**

- `CHANGELOG.md` (Keep-a-Changelog; `## Unreleased`).
- `LICENSE` (proprietary).
- `NOTICES.md` (third-party scan via `go-licenses`).
- `ARCHITECTURE.md` (1-page mental model + tree + read order).
- `CONTRIBUTING.md` (slim internal-eng onboarding).
- `SECURITY.md` (procurement-doc shape: data flow + escalation).
- `INDEX.md` (D14 — points at all docs).

**Automation:**

- `.githooks/commit-msg` (Conventional Commits validator).
- `.githooks/prepare-commit-msg` (DCO auto-append).
- `scripts/changelog-gen.sh` (or `git-cliff` config).
- `.github/workflows/stale-todo.yml` (TODO age scanner).
- `.github/workflows/doc-check.yml` (link-check + banned-phrase +
  typed-link-prefix).
- `.github/workflows/commit-lint.yml` (PR-level Conventional
  Commits check).
- `.github/workflows/pr-lint.yml` extended: when release-notes
  category is `[BUGFIX]`, require non-empty Root-Cause line in PR
  body (closes the §1 #5 PR-purity rule gap; rule is advisory
  until this workflow lands).
- `.github/PULL_REQUEST_TEMPLATE.md` minimal rewrite (1-line What +
  Root-Cause field + release-notes block).
- `Makefile`: `check` target extended with commit-lint advisory +
  changelog dry-run; new `install-hooks` / `uninstall-hooks`
  targets.

**Falsifier:** post-merge grep for
`internal/l0|internal/securitygate|schemas/` returns zero stale
references. `contracts/wire/spec_adapter_jsonio.md` referenced by a
real test asserting doc-vs-Go-interface coverage. doc-check passes.
`make check` green. `go build ./... && go test ./...` green.

**Rollback:** revert single PR; tree restored.

### Wave 2 — Customer surface

Operator-, auditor-, engineer-facing docs + runnable examples.
Implements Section 3 priority 1 (customer impact).

**Examples (P5/P9 falsifying consumers for the binary itself):**

- `examples/minimal/regatta.yaml` + README — smallest viable config.
- `examples/full/regatta.yaml` — every option exercised.
- `examples/target-repo/` — toy repo pinned SHA, used by e2e smoke.
- `.github/workflows/examples-validate.yml` runs
  `regatta validate-config` against each example on PR + monthly
  (D15 falsifier).

**Operator docs (`docs/operator/`):**

- `quickstart.md` — <60s binary → validated config; CI runs steps
  end-to-end.
- `install.md`, `configure.md`, `upgrade.md`.
- `day1.md`, `day7.md`, `day30.md` (hoisted from `docs/design.md`
  §Day 1 → Day 30 Runbook; design.md links rather than restates
  per D3).

**Auditor docs (`docs/auditor/`):**

- `threat-model.md` (extracted from `docs/design.md` §Threat Model;
  D3 dedupe).
- `audit-log.md`, `reproducibility.md`, `data-flow.md` — stubs with
  activation triggers when impls land.

**Engineer docs (`docs/engineer/`):**

- `how-to-add-a-gate.md`.
- `how-to-add-an-adapter.md`.
- `release-runbook.md`.
- `post-mortems/.gitkeep` (G13 surface ready when first incident).
- Cross-linked from `CONTRIBUTING.md`.

**AGENTS.md topic-index** updated to point at new locations.

**Falsifier:** quickstart runs end-to-end in CI; each doc cited from
another doc or workflow (D2); every customer-visible string in
CLI/error/log reviewed for clarity (priority 1 implementation).

**Rollback:** revert single PR; docs gone, no code impact.

### Wave 3 — Governance + closeout

Branch protection + workflow tightening + final dedupe.

**Branch protection on `main`:**

- `required_status_checks`: L0–L5 aggregator + `make check` +
  commit-lint + doc-check + stale-todo + examples-validate.
- `required_approving_review_count: 0` (solo posture).
- `enforce_admins: true`.
- No force-push, no admin merge, no skip-hooks.

**Workflow aggregator hardened:**

- `.github/workflows/gates.yml` aggregator uses `if: always()` +
  explicit `needs.*.result` checks (load-bearing lesson — no silent
  SKIPPED bypass).
- `.github/workflows/release.yml` — tag-triggered: signed tag,
  provenance attestation, CHANGELOG section flip, customer-release-
  notes derivation.

**Final dedupe pass (D3 + D16):**

- Grep prose across `docs/`; collapse duplicates to single source +
  citation.
- Any directory empty post-Wave-2 = either earned with stub impl +
  test OR deleted (D16 no-deferred-debt applied to restructure
  itself).
- Update `STYLE.md` to reflect new tree + automation.
- Update `INDEX.md` with final doc set.

**Tag `v0.2.0`:** CHANGELOG `Unreleased` → `0.2.0`; signed tag;
release-notes generated.

**Falsifier:** open test PR with intentional violations across each
new gate (force-push attempt, broken doc link, stale TODO, bad
commit subject, missing release-notes block). Each blocks correctly.
Audit-log entry verified for each block.

**Rollback:** branch-protection rollback = repo-admin removes
required checks (audit-logged). Doc/script rollback = revert PR.

### Adopt-when-needed — NOT a wave, deferred

- `plugins/adapters/example-jsonio/` + `plugins/gates/example-license/`
  + `make plugins-validate` — land when first customer asks OR a
  Phase 2/3 milestone hits.
- `internal/audit/` impl, `internal/tenant/` impl, cross-vendor
  `ModelClient` — per `docs/design.md` phasing.
- Two-key automation, public CONTRIBUTING framing, scheduled
  release cadence, Code of Conduct — per Section 5 activation
  triggers.

### Wave dependency

```
Wave 1 (Foundation) ─► Wave 2 (Customer surface) ─► Wave 3 (Gov + closeout)
```

Strict sequence. Each PR green + mergeable independently.

### Risk + rollback summary

| Risk | Mitigation | Rollback |
|---|---|---|
| Wave 1 import-path break | `go build ./... && go test ./...` pre-merge; sequential commits per move inside PR | revert PR |
| Wave 1 doc-link rot | doc-check workflow runs in same PR | revert PR |
| Wave 2 quickstart drift from binary | e2e smoke runs quickstart steps in CI | revert PR |
| Wave 3 branch-protection lockout | sandbox-branch test first; flip on main last | repo admin removes required checks |
| Wave 3 hooks block commits unexpectedly | `make install-hooks` opt-in; `make uninstall-hooks` available | operator unblocks self |

## Out-of-scope

- New product features (this is structural only).
- New gates beyond what `docs/design.md` already describes.
- Multi-tenancy implementation (Phase 3 trigger).
- Cross-vendor `ModelClient` (Phase 3 trigger).

## References

- `docs/design.md` — full product design.
- `docs/incidents.md` — AI-agent incident catalog.
- `PRINCIPLES.md` — why behind the rules.
- `STYLE.md` — current contributor conventions.
- `AGENTS.md` — agent / contributor onboarding brief.

[golang-standards/project-layout](https://github.com/golang-standards/project-layout) ·
[go.dev module layout](https://go.dev/doc/modules/layout) ·
[Alex Edwards — 11 tips structuring Go](https://www.alexedwards.net/blog/11-tips-for-structuring-your-go-projects) ·
[GitHub repo best practices](https://docs.github.com/en/repositories/creating-and-managing-repositories/best-practices-for-repositories) ·
[OpenSSF SLSA](https://openssf.org/projects/slsa/) ·
[SLSA v1.2 spec](https://slsa.dev/) ·
[adr.github.io — AD practices](https://adr.github.io/ad-practices/) ·
[Joel Parker Henderson — ADR examples](https://github.com/joelparkerhenderson/architecture-decision-record)
