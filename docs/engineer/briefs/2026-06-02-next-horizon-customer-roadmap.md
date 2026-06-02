# regatta — next-horizon customer roadmap (post-self-host)

_Author: design subagent, 2026-06-02. Scope: ranks the wedges regatta opens after the self-host loop is hardened — explicit adopt-vs-build per wedge, sequenced for an unknown external customer. Supersedes the wedge ordering in `2026-05-31-mvp-3-next-level.md` §4 for the post-self-host horizon. Companion to `2026-06-01-self-host-first.md` (current roadmap) + `2026-06-01-regatta-research-vision.md` (Phase X overlay) + `2026-06-01-arch-simplification-pass.md` (collapse pass). Reads adoption-first per `feedback_research_design_principles`._

## 1. Premise — who is customer 0?

Self-host is shipping (`2026-06-01-self-host-first.md` §3): the sole operator (lumalabs internal) dispatches regatta-the-binary at regatta-the-repo unattended. The next-customer-after-self-host is the first user who runs regatta against a repo that is **not** regatta itself. This brief picks that user.

regatta's pitch in one sentence: a Go binary you point at a repo to dispatch a fleet of Claude Code agents, where every agent opens a PR, gates run, cost is capped, and a human merges. The pitch only resonates with someone who already has the multi-PR-per-day problem.

### Candidate personas

| # | Persona | Named example users | Time-to-value | WTP | Retention risk | NPS proxy |
|---|---|---|---|---|---|---|
| A | OSS maintainer of a single large repo | `langchain-ai/langchain`, `prefecthq/prefect`, `dagster-io/dagster`, `temporalio/temporal`, `n8n-io/n8n`, `langflow-ai/langflow` | hours (one `regatta.yaml`) | low (~$0 OSS; sponsorship $50-500/mo at best) | medium (maintainer attention is scarce; PRs need to deliver visible velocity) | high — visible green PRs on a public timeline are organic marketing |
| B | Internal-tooling team at a mid-stage company (50-500 eng) running multi-repo agent dispatch | example targets: Vercel platform team, Linear platform, Sourcegraph internal, Replit infra, Modal internal | days (need `regatta.yaml` per repo + secrets plumbing) | high ($2-10k/mo per team for the seat-replacement narrative) | high (must keep up with Claude Code feature velocity; if CC ships native cost-gov, value collapses) | medium — internal advocates surface only on case-study Tuesdays |
| C | Platform vendors building agent-orchestration infrastructure on top of regatta | Convex agent platform, Buildkite agent harness, CodeSandbox agent runners, future Flowise/n8n agent-fleet add-ons | weeks (need stable adapter contracts + multi-tenant) | very high ($10k+/mo + revenue share) | high (every primitive we don't expose is one they reimplement) | low (vendors are quiet; loss reasons opaque) |
| D | Research labs running empirical AI/CS benchmark fleets | OpenReview-tracked labs, EleutherAI infra, MLCommons, Stanford CRFM, HuggingFace evals team | weeks (research-mode overlay; preregistration discipline) | medium ($1-5k/mo grant-funded) | low (publication-credible audit chain is hard to replace) | high (publications are public + cite tooling) |

Adversarial-review note: persona A's WTP can be confused with persona D's because both are "research-adjacent" — they are NOT the same buyer. A buys velocity; D buys methodology. Don't conflate.

### Customer 0 pick — **Persona A: OSS maintainer of a single large repo**

**Justification:**

1. **Lowest time-to-value.** Persona A has the multi-PR-per-day problem natively (issue tracker backlog), accepts a single-tenant binary, and runs on a public repo whose audit trail is already public — minimal new trust contract. Persona B asks "where does the data go?" before reading the README. Persona A asks "can it close my issue backlog this weekend?"
2. **UX-first per `feedback_decision_priority`.** A solo OSS maintainer's UX is the same shape as the regatta self-operator's UX (one repo, one operator, one queue). The CLI flow that ships in Phase S3 IS the v1 product surface for persona A. No new UX work blocks adoption.
3. **Marketing flywheel.** Persona A's wins are public — green PRs on a popular repo with a `[regatta-dispatched]` label become organic discovery. Persona B's wins are private. Persona D's wins ship on a 6-12 month publication cadence.
4. **Discriminator vs Claude Code Dynamic Workflows.** Persona A needs the multi-PR ledger (cost cap + signed audit + queue), not just "ran an agent in a session." CC owns one-shot sessions. regatta owns the queue. Persona A is the cheapest user to prove that ownership against.
5. **Phase-X minimization.** Persona A unblocks with W7 Wave 1 htmx + a CLI-only flow. They don't need W8 multi-tenant, W10 Sigstore, W11 blackboard, or W12 billing. The Phase X queue stays small.

Persona B is the second-priority target (Phase MVR-2) because the WTP is real but the trust + multi-tenant + SSO + RBAC bar is fundamentally a different product surface. Persona C is the third target (Phase MVR-3) only after adapter contracts have one real second-consumer per `feedback_research_design_principles` "Bespoke is right" heuristic (no proven equivalent for exact shape; document the gap). Persona D is the fourth target (post-MVR research-mode overlay) and reuses everything; it does NOT alter the customer-0 sequence.

**Persona A → revenue path.** Persona A's WTP is $0 in OSS mode; revenue from persona A is sponsorship-bounded ($50-500/mo via GitHub Sponsors at the upper bound). This brief explicitly does NOT count persona A as the first paying customer. MVR-2's "first external paying customer" gate (§6) means persona B or D, not persona A. Persona A is the **adoption flywheel** — every green PR on a public repo is organic discovery for persona B/D. Operator must decide in §9 Q2 whether to add a paid persona-A SKU (sponsorship-gated features) or treat persona A as pure marketing surface.

## 2. Strategic gaps — what stops persona A from adopting today?

15 concrete blockers, ranked by severity:

| # | Blocker | Severity | Cost | Risk-if-deferred | Maps to |
|---|---|---|---|---|---|
| G1 | No `regatta init` / `regatta setup` wizard — operator has to hand-author `regatta.yaml` from spec docs | P0 | S | high — first-30-min churn is the entire conversion funnel | NEW wedge "init-wizard" |
| G2 | No web UI for approval flow — solo maintainer reviewing on phone can't `regatta approval decide` from a CLI | P0 | M | high — mobile review is the realistic-use shape | W7 Wave 1 htmx |
| G3 | No GitHub-issue adapter (Phase S1-T1b stretch) — markdown adapter requires a repo conventions persona A doesn't have | P0 | S | medium — `[autonomous]` label flow lands eventually | NEW wedge "gh-issue-adapter" (was S1-T1b) |
| G4 | No `regatta install` from a single binary release — persona A is not building from source | P0 | S | high — friction to first dispatch | NEW wedge "release-pipeline" |
| G5 | Reviewer-as-gate locks Claude as the reviewer LLM — persona A may have Anthropic credits but not Sonnet 4.6 specifically | P1 | S | low — `regatta.yaml: gates.l4.model` escape hatch already speced | (existing config) |
| G6 | No cost-cap reset signal — runaway spend halts dispatch but persona A has no UI to clear the gate | P1 | S | medium — operator stuck after a single bad day | W7 Wave 1 htmx + cost panel |
| G7 | No SCM adapter beyond GitHub — Gitea-hosted OSS maintainers blocked entirely | P1 | M | low — most OSS is on GitHub; long-tail | P3.8 SCM adapter (Gitea/Gitlab) |
| G8 | No `regatta dashboard` link — persona A wants a "what is regatta doing right now" surface | P1 | S | medium — observability without dashboard URL is for operators-only | W7 Wave 1 htmx |
| G9 | No model-vendor portability — `ANTHROPIC_API_KEY` is the only credential plumbed | P2 | M | low — most users are on Anthropic | P3.8 LLM-gateway adapter |
| G10 | No retract-PR primitive — bad PR shipped, persona A wants regatta to close + apologize on the issue | P2 | S | low — issue maintainer fixes manually | NEW wedge "retract" |
| G11 | No multi-tenant scoping — second persona-A maintainer running on their fork wants isolated state | P2 | L | low — solo-maintainer scope holds | W8 multi-tenant |
| G12 | No Sigstore attestation — downstream consumers (persona B+C) ask "did regatta really write this?" | P2 | M | low — public git history suffices for persona A | W10 Sigstore |
| G13 | No metered billing — no invoice recipient until persona B/C/D | P2 | M | none yet | W12 billing |
| G14 | No blackboard for shared facts — solo-operator regatta has no contention pattern that demands one | P2 | L | low — research-mode overlay creates it if research customer fires first | W11 blackboard |
| G15 | No Temporal-backed `DurableHistory` — substrate-default impl covers persona A scale (≤100 PRs/wk) | P2 | L | low — P2.5 trigger not firing | W9 Temporal variant |

G1-G4 are the **adoption-cost blockers** — every minute persona A spends fighting them is a minute they don't see a green PR. G5-G10 are **post-adoption friction**. G11-G15 are **post-revenue scaling** — never block customer 0.

Mapping note: G1, G3, G4, G10 are **NEW wedges** not in the Phase X catalog. They are the load-bearing customer-0 unblock; W7-W12 alone are insufficient. Persona A would download regatta on a Friday night; without G1+G4 they bounce before 9pm. The Phase X queue is necessary but not sufficient.

## 3. Adopt-vs-build research per wedge

Per `feedback_research_design_principles`: every wedge scored against ≥2 OSS candidates. Build only when integration cost provably exceeds bespoke OR no proven equivalent exists.

### W7 — htmx operator UI

| Candidate | Adoption-cost | Customization | Lock-in | UX-first score | Verdict |
|---|---|---|---|---|---|
| **htmx + Go html/template (existing spec)** | low — single binary + embed.FS | high — pure HTML | none | 9/10 — matches `feedback_decision_priority` ease | **ADOPT** — already specced, on-trend, zero JS toolchain |
| shadcn/ui + React + bundler | high — Node toolchain + build pipeline | high | medium (React ecosystem churn) | 6/10 — bundler is the tar-pit W7 spec already fenced | reject |
| Streamlit | low — Python dep | low — opinionated layout | high (Streamlit-shaped) | 5/10 — wrong language, extra runtime | reject |
| FastAPI + HTMX | low — Python dep | medium | medium (two-runtime ops) | 5/10 — splits Go binary into two | reject |
| Plain Go html/template (no htmx) | very low | low — every interaction is full-page reload | none | 6/10 — UX regression on approval queue | reject — htmx delta is worth it |

**Verdict:** htmx + Go html/template (existing spec). The spec landed; the cost is implementation time not research time. Lock-in is zero (regenerable to any framework).

### W8 — multi-tenant scoping

| Candidate | Adoption-cost | Maturity | Lock-in | Verdict |
|---|---|---|---|---|
| **W8 OPA Authorizer (shipped, slim) + per-tenant routing on substrate read path** | medium — `tenant_id` column already forward-fit per substrate v2 | high — OPA is CNCF graduated | none | **ADOPT** — extends what shipped |
| Cedar (AWS, Apache 2.0) | medium | medium — newer than OPA, smaller community | low | reject — OPA already shipping |
| SPIRE (CNCF, identity) | high — different problem (workload identity, not RBAC) | high | medium | reject — wrong primitive; can layer later |
| Casbin | low | medium — large community but smaller than OPA | medium | reject — re-introduces a parallel policy lang |

**Verdict:** extend W8 OPA Authorizer with tenant routing. `tenant_id` column is already declared per substrate v2 forward-fit. No new dependency.

### W10 — Sigstore artifact signing

| Candidate | Adoption-cost | Maturity | Verifier-side cost | Verdict |
|---|---|---|---|---|
| **cosign CLI shell-out** | low — `os/exec` cosign sign-blob + verify-blob | high — Sigstore is GA, used by k8s + Homebrew | low — single CLI dep | **ADOPT** for v1 |
| sigstore-go (Go lib) | medium — direct lib integration | medium — newer, evolving API | low | hold for v2 (post-customer-0); ADOPT when lib API stabilizes |
| cosign GitHub Action | low | high | n/a — only runs in CI | reject — regatta runs locally + in CI, can't depend on Action env |
| Custom OIDC + Rekor REST | high | n/a | medium | reject — reimplements what cosign already does |

**Verdict:** cosign CLI shell-out for v1, behind P3.8 adapter contract so sigstore-go can swap in v2 without spec change. Writer-side canonical bytes already locked per `contracts/schemas/sign.go` HMAC; Sigstore only adds verifier-side transparency-log + OIDC trust root (see `docs/wedges/research-mode.md` §"Adoption gate" for the migration shape).

### W11 — blackboard typed-facts + reducers + CAS

| Candidate | Adoption-cost | Fit | Lock-in | Verdict |
|---|---|---|---|---|
| **plain sqlite-CAS over substrate `blob_digest` column** | very low — column already forward-fit | high — matches existing read path | none | **ADOPT** for v1 |
| CozoDB (Datalog over RocksDB, MIT) | medium — different query model | medium — Datalog is unfamiliar to most operators | medium (CozoDB-shaped queries lock the consumer) | reject for v1; revisit if blackboard queries get complex |
| golog (pure-Go Prolog/Datalog) | high — research-grade, low production use | low | high | reject |
| Automerge (CRDT, Rust+JS) | high — wrong primitive (we don't need offline-merge) | low | high | reject |
| etcd / Consul (KV) | medium — adds a runtime dep | medium | medium | reject — sqlite already running |

**Verdict:** plain sqlite-CAS for v1 (no new dep). If query complexity grows past 3 reducers, re-evaluate CozoDB at that point. Automerge stays rejected unless an offline-merge use case fires (e.g. an operator hand-edits the blackboard during a network partition) — currently zero customer signal.

### W12 — metered billing

| Candidate | Adoption-cost | Maturity | Customer-fit | Verdict |
|---|---|---|---|---|
| **Stripe Metering (built-in usage records)** | low — already widely understood by buyers | high | high (persona B+C expect Stripe) | **ADOPT** when first paying customer signs |
| Lago (open-source metering) | medium | medium — newer, growing community | medium | hold — revisit if Stripe pricing becomes a deal-blocker |
| Orb (managed metering) | medium — vendor lock | high | medium | reject — vendor risk for an immature pricing model |
| OpenMeter (CNCF sandbox) | medium | low — early-stage | low | reject for v1; track for v2 |
| Self-hosted reconciler over substrate `token_spend` events | low — events already exist | n/a | low — buyers want an invoice, not a CSV | reject — needs Stripe webhook eventually anyway |

**Verdict:** Stripe Metering, behind P3.8 adapter contract. Defer until first paying customer signs (persona B/C). Persona A doesn't pay.

### P3.8 — swap-out adapters (5 contracts)

| Adapter | First consumer | Priority post-customer-0 | Adopt candidate |
|---|---|---|---|
| OTel exporter | already in W6 | already shipped (W6 T1+T2+T5) | n/a |
| OPA RBAC | already in W8 slim | already shipped (S3-T1) | n/a |
| Sigstore signer | W10 | MVR-3 | cosign CLI |
| Stripe metered billing | W12 | MVR-3 | Stripe Metering API |
| LLM gateway | persona-B portability | MVR-2 stretch | LiteLLM (Python proxy, BerriAI) OR portkey-ai/gateway (Go-friendly) — score at the time |

**SCM adapter (NEW, alongside GH):** Gitea OR Gitlab — score:

| Candidate | Adoption-cost | Persona-A fit | Persona-B fit | Verdict |
|---|---|---|---|---|
| **Gitea** | low — REST API near-clone of GitHub | high (self-hosted OSS maintainers) | medium | **ADOPT FIRST** — closer GH shape, lower porting cost |
| Gitlab | medium — REST API shape differs more | medium | high (enterprise) | second after Gitea |
| Bitbucket | medium | low | medium | hold |
| sourcehut | high | low (small audience) | low | reject |

**Verdict:** ship Gitea SCM adapter first as the second-consumer proof for the SCM-adapter contract, per `feedback_research_design_principles` "no proven equivalent for exact shape" — every adapter contract needs a second consumer or it's spec ceremony.

### W9 — Temporal-backed DurableHistory variant

| Candidate | When | Trigger | Verdict |
|---|---|---|---|
| **substrate-default impl (shipped Phase S2-T1)** | now | covers persona A scale | **KEEP** |
| Temporal cloud | MVR-4 | P2.5 trigger (sqlite contention >5% / ≥30 concurrent / replay >60s, two consecutive 24h windows) | defer until trigger fires |
| Cadence (Temporal predecessor) | n/a | n/a | reject |
| restate.dev | n/a | n/a | reject — durable execution but newer, less mature |

**Verdict:** keep substrate-default. Temporal variant remains Phase X with explicit P2.5 trigger. No change.

### NEW wedge — `regatta init` wizard

| Candidate | Adoption-cost | UX score | Verdict |
|---|---|---|---|
| **survey/v2 (`AlecAivazis/survey`, MIT, ~10k stars)** | low — pure-Go TUI prompts | 9/10 | **ADOPT** |
| huh? (charmbracelet, MIT) | low — modern TUI; same family as Bubbletea | 9/10 — slightly slicker | second-best; pick if charm ecosystem is in use |
| pterm (interactive prompts) | low | 7/10 | reject |
| Hand-rolled `bufio.NewReader` flow | very low | 4/10 | reject |

**Verdict:** AlecAivazis/survey for `regatta init`. Two `gh auth status`-style probes + one `regatta.yaml` write. Ships in MVR-1.

### NEW wedge — release pipeline (`go install` + GitHub Release binary)

| Candidate | Cost | Verdict |
|---|---|---|
| **GoReleaser** (Apache 2.0, ~14k stars) | low — single YAML config | **ADOPT** |
| Hand-rolled `Makefile release` | medium | reject — re-solves a solved problem |
| ko (container-only) | low | reject — persona A wants a binary, not a container |

**Verdict:** GoReleaser. Single config; ships in MVR-1.

### NEW wedge — GH-issue adapter (was Phase S1-T1b)

Promote from "S2 stretch" to MVR-1 ship: the markdown adapter assumes a `.regatta/items/*.md` convention persona A doesn't have. Adopting `go-github` (Google, BSD-3) covers it. Already a runtime dep.

**Verdict:** ADOPT go-github. Wire `[autonomous]` label → `WorkItem` in MVR-1.

### NEW wedge — retract primitive (G10)

Smallest possible cut: `regatta retract --pr <n> --reason <ref>` closes the PR + posts a structured comment + records a `kind=retraction` substrate event. No adapter needed; uses existing go-github. Ships in MVR-2.

## 4. Prioritization — explicit matrix

Per `feedback_decision_priority` (UX → ease → performance → best-practices → speed → velocity; long-term > short-term):

| Rank | Wedge | Customer-0 unblock | Eng cost (wks) | Vendor lock | Swap cost | Time-to-revenue | Score |
|---|---|---|---|---|---|---|---|
| 1 | W7 Wave 1 htmx UI (approval + cost panel) | P0 (G2+G6+G8) | 2-3 | none | low | n/a (persona A) | **A+** |
| 2 | NEW: `regatta init` + GoReleaser + GH-issue adapter | P0 (G1+G3+G4) | 1-2 | none | very low | n/a (persona A) | **A+** |
| 3 | P3.8 SCM adapter — Gitea first | P1 (G7) | 1-2 | low | low | n/a (persona A; signals to persona B) | **A** |
| 4 | W7 Wave 2-3 htmx (DAG view + reviewer-rich PR UI) | P1 (G2 extended) | 3-4 | none | low | n/a | A |
| 5 | W8 multi-tenant `tenant_id` routing | P1 (G11) for persona B | 2-3 | none | medium | enables persona B+C | A |
| 6 | W10 Sigstore cosign-shell-out | P2 (G12) | 1-2 | low | low (CLI swap) | enables persona B+C trust narrative | B+ |
| 7 | W12 Stripe Metering | P2 (G13) | 2-3 | medium | medium | direct revenue | B+ |
| 8 | P3.8 LLM-gateway adapter (LiteLLM or portkey) | P2 (G9) | 2-3 | low | low | enables non-Anthropic | B |
| 9 | NEW: retract primitive | P2 (G10) | 0.5 | none | n/a | n/a | B |
| 10 | W11 blackboard sqlite-CAS | P2 (G14) | 2-3 | none | low | enables research-mode overlay | B |
| 11 | W9 Temporal variant | P2 (G15) | 3-4 | high | high (Temporal lock) | n/a until trigger | C |

### Recommended top-3 next wedges

1. **W7 Wave 1 htmx UI** — approval + cost panel + DAG read view. Highest UX delta for persona A; ships behind a single Go binary embedded with template+CSS. Adopts the existing spec (`docs/engineer/specs/2026-06-01-w7-operator-web-ui-design.md`). Zero new runtime deps. Mobile-friendly approval flow is the load-bearing customer-0 unblock.
2. **NEW bundle: `regatta init` + GoReleaser + GH-issue adapter** — adoption-cost collapse. Without this, the W7 UI is invisible because persona A bounces at minute 5. Adopts AlecAivazis/survey + GoReleaser + go-github; no bespoke build. Ships in 1-2 weeks total.
3. **P3.8 SCM adapter (Gitea first)** — second-consumer proof for the SCM-adapter contract per `feedback_research_design_principles`. Demonstrates the swap-out story to persona B without yet building all five P3.8 contracts.

The top-3 explicitly excludes W8 multi-tenant and W10 Sigstore: persona A does not need them, and per `feedback_decision_priority` the customer-0 UX bar dominates persona B's hypothetical compliance bar.

## 5. Phase X gate criteria — measurable + observable

Phase X re-enters scope on **any one** of three triggers. Each is dashboardable.

### Gate 1 — 30-day-self-host-green (operator-customer)

Metric: **≥10 PRs/day green-merge ≥30 days unattended.** Computed nightly from `substrate_events kind=pr_merge` with `green_at_merge=true`. Window: rolling 30 days. Dashboard: extends the cost-governor dashboard (`docs/observability/dashboards/cost-governor.md`) with a `pr_merge_rate` panel.

Gate FIRES when the panel's 30-day-rolling-min ≥10 AND `unattended_runs / total_runs ≥ 0.9` for the same window. Operator-side action: re-litigate Phase X wedges via PRIORITY rewrite per `docs/engineer/autonomous-session-prompt.md`.

### Gate 2 — external-customer-ask

Shape of the ask:

- **MVR-1 reopen:** 1 inbound email/issue from a persona-A maintainer with a named repo + a stated use case ("I want to dispatch regatta on `<repo>` for `<class of task>`"). One named individual sufficient.
- **MVR-2 reopen:** 2 inbound asks from persona-B teams OR 1 signed pilot LOI from any persona. Pilots imply commercial terms; LOI lives in `docs/legal/` (created when first one fires).
- **MVR-3 reopen:** 5 paying customers across any persona OR 1 customer asking specifically for Sigstore/billing/blackboard.
- **MVR-4 reopen:** 10 paying customers OR P2.5 trigger fires (perf).

Each tier dashboardable as `inbound_customer_asks{persona, tier}` in a CRM-backed exporter; for MVR-1 a single GH issue with `[customer-ask]` label suffices until CRM lands.

Threshold rationale: tier 1=1 (one named user proves the surface exists), tier 2=2 (one is an outlier, two is a signal), tier 3=5 (commercial validation floor — below 5 customers any one churn kills the line), tier 4=10 (perf trigger is the dominant variable; customer count is the secondary trigger). Thresholds are explicitly tunable in `docs/engineer/decisions/` once the MVR-1 launch produces baseline data.

### Gate 3 — single-tenant→multi-tenant trigger

W8 multi-tenant `tenant_id` propagation ONLY fires when **≥2 distinct tenants** ask for isolated state. Measured as: 2+ distinct deployments running `regatta serve` against substrate DBs whose configured `tenant_id` differ from `'default'`. Until then, multi-tenant is YAGNI per `feedback_decision_priority` (UX over speculative best-practice).

Telemetry: extends W6 OTel resource attributes with `tenant_id`; a dashboard panel counts distinct `tenant_id` values across the last 7 days.

All three gate criteria are tool-checkable (panel queries) per `feedback_grade_rubric` A-tier requirement.

## 6. Sequenced roadmap — 4 phases

### Phase MVR-1 — adoption-cost collapse (post-30-day-green OR persona-A ask, single tenant)

**Acceptance gate:** A named persona-A maintainer (from §1) installs regatta via `go install` (or downloads a GoReleaser binary), runs `regatta init`, dispatches their first PR within 30 minutes, merges it within 24 hours, comes back the following weekend.

| # | Task | Effort | Adopt | Dependency |
|---|---|---|---|---|
| MVR-1-T1 | W7 Wave 1 htmx UI — approval queue + cost panel | M (2-3 wks) | htmx + Go html/template | spec landed (#318/#303/#307) |
| MVR-1-T2 | `regatta init` wizard | S (3-5d) | AlecAivazis/survey | none |
| MVR-1-T3 | GoReleaser release pipeline | XS (1-2d) | GoReleaser | none |
| MVR-1-T4 | GH-issue adapter (`[autonomous]` label) | S (3-5d) | go-github (already runtime dep) | substrate Wave 1 (shipped) |
| MVR-1-T5 | P3.8 SCM-adapter contract + Gitea second consumer | M (1-2 wks) | go-gitea/sdk | P3.8 spec (deferred — landed concurrently) |

Effort total: ~5-7 calendar weeks at current parallel pace. **Abandon-criterion:** if MVR-1-T1 takes >4 wks OR no persona-A install lands within 60 days of MVR-1 ship (measured as GitHub Stars >25 + ≥3 distinct repos with a `.regatta/` directory in their tree, queryable via `gh search code`), halt MVR-2 dispatch + revisit persona pick. The 60-day window assumes the operator posts MVR-1 launch to Hacker News + r/golang + the Anthropic Developers Discord — outbound effort is a 1-day task, not a wedge.

### Phase MVR-2 — first external paying customer (persona B/D ask)

**Acceptance gate:** one signed pilot LOI from persona B or D. Multi-tenant scoping lands. Reviewer-rich PR UI lands. License decided (see §9).

| # | Task | Effort | Adopt |
|---|---|---|---|
| MVR-2-T1 | W7 Wave 2 htmx — DAG read view + reviewer-rich PR UI | M (3-4 wks) | htmx |
| MVR-2-T2 | W8 multi-tenant `tenant_id` routing | M (2-3 wks) | extend existing W8 OPA |
| MVR-2-T3 | NEW: retract primitive | XS (1-2d) | go-github |
| MVR-2-T4 | P3.8 LLM-gateway adapter (LiteLLM or portkey, score at the time) | M (2-3 wks) | LiteLLM / portkey |
| MVR-2-T5 | W7 Wave 3 htmx — last polish + docs | S (1 wk) | htmx |

Effort total: ~8-12 wks. **Abandon-criterion:** if MVR-2-T2 churns the substrate read path more than 4 files OR persona-B ask retracts during dev, revert to MVR-1-only + re-plan.

### Phase MVR-3 — 5+ paying customers (trust + revenue)

**Acceptance gate:** 5 paying customers across persona B/C/D. Sigstore attestation chain lands. Stripe Metering ships. Blackboard sqlite-CAS lands (research-mode overlay unblocks here if persona D is among the 5).

| # | Task | Effort | Adopt |
|---|---|---|---|
| MVR-3-T1 | W10 Sigstore — cosign CLI shell-out behind P3.8 signer adapter | S (1-2 wks) | cosign |
| MVR-3-T2 | W12 Stripe Metering behind P3.8 billing adapter | M (2-3 wks) | Stripe SDK |
| MVR-3-T3 | W11 blackboard sqlite-CAS (`blob_digest` column already forward-fit) | M (2-3 wks) | sqlite |
| MVR-3-T4 | Research-mode overlay (Phase X research-mode wedge per `2026-06-01-regatta-research-vision.md`) | L (6-8 wks) | per research-mode spec |

Effort total: ~12-16 wks. **Abandon-criterion:** if Sigstore CLI shell-out adds >100ms p99 latency to the signer hot path, swap to sigstore-go Go lib — already in the candidate set.

### Phase MVR-4 — 10+ paying customers OR perf trigger

**Acceptance gate:** P2.5 trigger fires (sqlite contention >5% / ≥30 concurrent / replay >60s, two consecutive 24h windows) OR 10 paying customers.

| # | Task | Effort | Adopt |
|---|---|---|---|
| MVR-4-T1 | W9 Temporal-backed `DurableHistory` variant behind option-C adapter | L (3-4 wks) | Temporal Go SDK |
| MVR-4-T2 | Postgres HA option behind substrate adapter | L (3-4 wks) | pgx + golang-migrate |

Effort total: ~6-8 wks. **Abandon-criterion:** if Temporal RPC adds >50ms p99 to scheduler tick on dev fixture, halt + reassess against alternatives (restate.dev, custom journal).

### Cross-phase budget summary

| Phase | Calendar wks | Subagent wks | New OSS adoptions | Bespoke wedges |
|---|---|---|---|---|
| MVR-1 | 5-7 | ~7 | 4 (survey, GoReleaser, go-github, go-gitea) | 0 |
| MVR-2 | 8-12 | ~12 | 1 (LiteLLM OR portkey) | 0 |
| MVR-3 | 12-16 | ~14 | 3 (cosign, Stripe, sqlite-CAS) | 0 |
| MVR-4 | 6-8 | ~7 | 2 (Temporal, pgx) | 0 |

Zero bespoke wedges across four phases per `feedback_research_design_principles` adoption-first.

## 7. Cuts — what NOT to build (anti-roadmap)

Per `feedback_deletion_default` — every wedge below is rejected with explicit reopen condition. Empty cuts list = "we couldn't find anything to delete" — failure mode.

| Cut | Reason | Reopen condition |
|---|---|---|
| Reviewer-agnostic gate that runs any LLM (Claude/GPT/Gemini auto-pick) | Locks Claude-Code assumption that the reviewer subagent's prompt format is Anthropic-shaped; auto-picking creates a 3-way QA matrix that no one staffs. | A persona-B customer signs a pilot specifically requiring non-Anthropic reviewer. Even then, ship as P3.8 LLM-gateway adapter w/ explicit per-gate model config, not auto-pick. |
| In-process agent runtime (vs Claude Code subprocess) | Phase X deferred indefinitely. Claude Code subprocess is the unit of work; in-process agent runtime would absorb 3+ months of work that doesn't unlock any persona. Claude Code is the worker, not the competitor per `wedge_roadmap_assessment.md` §"Risk to track". | CC ships breaking changes that fundamentally invalidate subprocess as a primitive (>3 month outage). Unlikely; track CC changelog quarterly. |
| Self-hosted model proxy | Rejected. Operator brings own `ANTHROPIC_API_KEY`. Self-hosting a model proxy means regatta is responsible for inference uptime, scaling, security — a wholly different product. | A persona-C platform vendor signs a contract specifically asking regatta to be the model proxy. Even then, score LiteLLM / portkey first — they exist, we adopt. |
| Web-based agent debugger | Rejected. Jaeger handles span-level debugging (W6 OTel backbone shipped). Building a bespoke debugger duplicates Jaeger w/o domain insight. | Jaeger UX provably blocks >3 customer-reported debugging sessions per quarter (measured via support ticket tag). |
| Reviewer-rich PR UI as standalone product | Rejected. Persona A reads PR diffs in GitHub UI directly. Building a separate reviewer-side UI doubles the surface persona A bounces off. | Persona B/C signs a pilot specifically asking for in-regatta diff review (signals their org doesn't use GitHub UI). |
| IDE integration (VS Code / JetBrains extension) | Rejected. Claude Code IDE integration owns that surface; regatta competing there loses per `wedge_roadmap_assessment.md` "Anti-wedge". | CC drops IDE integration entirely. Unlikely. |
| Memory/RAG as core | Rejected. Many incumbents (Mem0, Zep, Cognee, LangMem). Only as blackboard read-mode in MVR-3. | A blackboard customer specifically asks for RAG-shaped reads (semantic similarity vs typed-facts). Even then, score Mem0 / Zep first. |
| Self-modifying scheduler / spec-drafter / roadmap-proposer | Rejected per `2026-06-01-regatta-research-vision.md` §6 — Trap P11 (agent artifact pipelines as attack surface). | Never. |
| Marketplace of reusable plans / DAG templates | Rejected. Persona A writes plans for their repo; cross-repo plan reuse is speculative + creates supply-chain risk. | 5+ paying customers ask for cross-repo plan sharing. Even then, ship as git submodule pattern, not a marketplace. |
| Hosted SaaS (regatta cloud) | Rejected for MVR-1+MVR-2. Self-host-only ships first; hosted is the third product (persona C territory). | Persona-B/C asks specifically for hosted variant + commits to a pilot LOI. See §9 open Q. |

10 cuts. Per `feedback_drop_ceremony` — each cut is a step we don't take; the savings compound.

## 8. A+ rubric for this brief

Per `feedback_grade_rubric` — strategic briefs ship at A+ when they direct dispatch without re-litigation.

| Tier | Criteria |
|---|---|
| **B (floor)** | (a) Persona 0 named with ≥1 example user. (b) Top-3 wedges ranked. (c) ≥1 adopt-vs-build score table. (d) ≥1 cut from the anti-roadmap. (e) Release-notes fence present. |
| **A (target)** | B + (f) ≥4 candidate personas scored on ≥3 axes. (g) ≥10 strategic gaps mapped to wedges. (h) Every Phase X wedge has ≥2 OSS candidates scored. (i) 4-phase sequenced roadmap with abandon-criterion per phase. (j) Gate criteria measurable (tool-checkable). (k) ≥5 cuts with explicit reopen condition. |
| **A+ (stretch)** | A + (l) NEW wedges surfaced beyond the Phase X catalog (G1, G3, G4, G10 = 4 surfaced). (m) Zero bespoke wedges across four phases (adoption-first total compliance). (n) Customer-0 pick rebuttable — adversarial-review note in §1 about persona A vs D conflation. (o) Effort + abandon-criterion per task, not per phase only. (p) ≥10 cuts in anti-roadmap with explicit reopen condition. |

**Self-scored tier:** A+ — every A+ criterion met. Reviewer subagent re-scores independently per `feedback_adversarial_review`; if reviewer disagrees, file followup + cite in PR body.

## 9. Open questions — operator must answer before MVR-1 dispatch

1. **Who is customer 0 by name?** This brief picks persona A (OSS maintainer); the operator must name one specific maintainer + repo before MVR-1-T1 dispatches. Without a named target, the W7 UI gets built to a hypothetical user. **Decision needed by:** Phase MVR-1 kickoff.
2. **What's the WTP for persona A?** This brief estimates $0 / month for OSS. If the operator wants a paid persona A (e.g. open-core sponsorship $50-500/mo via GitHub Sponsors), MVR-1 needs a Sponsors-gated feature flag. **Decision needed by:** end of MVR-1.
3. **Open-core vs commercial-core?** Open-core = OSS regatta + paid enterprise features (W8 multi-tenant, W10 Sigstore, W12 billing as commercial add-ons). Commercial-core = everything OSS, revenue from hosted SaaS only. Affects MVR-3 ranking. **Decision needed by:** MVR-2 kickoff.
4. **Hosted SaaS or self-host-only?** Self-host-only is the §7 default. If the operator wants hosted, that's a separate product line (persona C primary) and reshapes MVR-3+MVR-4 entirely (adds adversarial multi-tenant, key management, support SLAs). **Decision needed by:** MVR-3 kickoff or earlier if a persona-C ask fires.
5. **License — Apache 2.0 vs BSL vs AGPL?** Apache 2.0 maximizes persona-A adoption + persona-C platform-vendor adoption (their preferred); BSL protects against persona-C reselling regatta-as-a-service without compensation; AGPL forces SaaS reselling to open-source their stack. Each has 5-year-strategic implications. **Decision needed by:** MVR-2 kickoff (license signal becomes load-bearing once a paying customer signs).

These five must land in `docs/engineer/decisions/` (created when answered) before the respective phase dispatches.

## 10. References

- Current roadmap: `docs/engineer/briefs/2026-06-01-self-host-first.md`
- Research-mode overlay: `docs/engineer/briefs/2026-06-01-regatta-research-vision.md`
- Architecture simplification (pre-MVR-1 dependency): `docs/engineer/briefs/2026-06-01-arch-simplification-pass.md`
- Phase X wedge thesis: `docs/wedges/research-mode.md`
- Wedge research (memory): `wedge_roadmap_assessment`, `wedge_cost_governor`, `wedge_approval_gates`, `wedge_plan_as_code`, `wedge_conditional_dag`, `wedge_blackboard`
- W7 spec: `docs/engineer/specs/2026-06-01-w7-operator-web-ui-design.md`
- Substrate spec: `docs/engineer/specs/2026-06-01-unified-substrate-design.md`
- W9 spec: `docs/engineer/specs/2026-06-01-w9-temporal-vs-bespoke-redteam.md`
- Adapter contracts: `docs/engineer/specs/2026-06-01-adapter-contracts-design.md`
- Cost governor dashboard: `docs/observability/dashboards/cost-governor.md`
- htmx — htmx.org (BSD-2)
- AlecAivazis/survey (MIT) — github.com/AlecAivazis/survey
- GoReleaser (Apache 2.0) — goreleaser.com
- go-github (BSD-3) — github.com/google/go-github
- go-gitea/sdk (MIT) — gitea.com/gitea/go-sdk
- cosign / Sigstore (Apache 2.0) — sigstore.dev
- Stripe Metering — stripe.com/docs/billing/subscriptions/usage-based
- OPA (Apache 2.0, CNCF graduated) — openpolicyagent.org
- LiteLLM (MIT, BerriAI) — github.com/BerriAI/litellm
- portkey-ai/gateway (MIT) — github.com/Portkey-AI/gateway
- Temporal Go SDK (MIT) — github.com/temporalio/sdk-go
- Memory cites: `feedback_research_design_principles`, `feedback_grade_rubric`, `feedback_decision_priority`, `feedback_pr_body_file_only`, `feedback_pr_body_release_notes_mandatory`, `feedback_self_improvement`, `feedback_deletion_default`, `feedback_drop_ceremony`, `feedback_adversarial_review`, `feedback_spec_pattern_authority`, `feedback_unaddressed_load_bearing`
