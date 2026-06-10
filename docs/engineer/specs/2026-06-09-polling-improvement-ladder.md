---
name: "Polling improvement ladder — ETag conditional GET → adaptive interval → webhook hybrid"
slug: 2026-06-09-polling-improvement-ladder
status: draft
phase: self-host-first
owner: tri@maydow.com
created: 2026-06-09
summary: "Five-rung roadmap for reducing GitHub-poll rate-limit consumption and end-to-end change-detection latency. Rungs 1 + 2 (ETag conditional GET; per-resource adaptive backoff) land in the self-host-first phase — zero new infra, reversible, addresses #1164 + the adaptive-interval observation from the regatta-operator skill. Rungs 3-5 (GH events API stream; smee.io/`gh webhook forward` hybrid (#1165); full webhook + Cloudflare tunnel) are Phase-X forward-fit with explicit reopen triggers — they impose ingress / multi-tenant complexity that the single-operator self-host filter rejects today. Closes #1164 (rung 1) and #1165 (rung 4, deferred)."
---

# Polling improvement ladder — Roadmap Spec

Status: draft
Date: 2026-06-09
Author: tri@maydow.com
Tracks: #1164 (rung 1, in-scope), #1165 (rung 4, Phase-X forward-fit)
Cross-ref: `internal/ghclient/client.go` (current gh-CLI seam, no HTTP client yet — rungs 1/3/5 require one), `internal/orchestrator/adaptersync/adaptersync.go:122-134` (MinPollInterval gate — closest sibling primitive for rung 2), `internal/orchestrator/adapter/githubissues/adapter.go:281` (per-adapter MinPoll wiring), `internal/orchestrator/prwatch/ghcli.go:61-71` (PR-list poll surface — second consumer of any rate-budget win), `internal/orchestrator/prwatch/prwatch.go:44-50` (poll tick budget rationale).

Memory rules in force: `feedback_default_simpler`, `feedback_decision_priority`, `feedback_root_cause`, `feedback_deletion_default`, `feedback_adversarial_review_every_step`, `feedback_unaddressed_load_bearing`, `feedback_validate_before_ship`, `feedback_no_signatures`.

Self-host filter (`CLAUDE.md` §"Self-host filter (Phase context)"): every rung is filtered by "does the sole internal operator need this to dispatch regatta-the-binary at this repo unattended?" Rungs 1 + 2 pass (rate-budget headroom + sub-poll latency on a quiet repo are operator-felt today). Rungs 3-5 carry tenant-shaped or ingress-shaped cost and are deferred per the filter; reopen triggers documented per rung in §5.

---

## §1 Problem

Regatta's polling loops drive every GitHub-derived state transition: `adaptersync` (issues → work items, `internal/orchestrator/adaptersync/adaptersync.go:122-134`), `prwatch` (PR head SHA + merge state, `internal/orchestrator/prwatch/ghcli.go:61-71`), and the github_issues adapter (`internal/orchestrator/adapter/githubissues/adapter.go:281`). Each loop runs on a fixed `PollInterval` configured per-resource. The current shape has two quantified costs:

1. **Latency floor = poll interval.** At default `PollInterval = 10s`, the perceived lag between a GH state change (PR comment posted, review submitted, check-run completed, label flipped) and regatta noticing it is uniformly distributed across `[0, 10s]` — mean ~5s, p99 ~10s. Operator perception during dispatch sessions is sluggishness on PR-review handoffs, which compounds across the agent → reviewer → merge cadence.

2. **Rate-budget waste on idle windows.** GitHub's authenticated REST budget is 5000 req/hr (~83 req/min). Each poll tick spends 1 req per endpoint per resource. For an idle repo (no new issues, no PR state change) the body is byte-identical to the prior tick, but the request still counts. Empirically across a 5-min idle window with one `adaptersync` + one `prwatch` consumer at 10s cadence: 2 × 30 = **60 requests**, ~95% returning identical bodies. Net useful information per request in that window: ~0.05. The conditional-GET path (ETag → 304) is documented by GitHub as **not counted against the rate budget** — converting 60 idle-window requests to ~3 informative + ~57 free 304s recovers ~57/60 ≈ **95%** of the consumed budget. The §2 goal targets ≥80% conservatively.

Adversarial framing: a single high-cadence consumer (`prwatch` at 5s) on a busy repo can exhaust budget in ~15 hours of continuous operation today; ETag converts that ceiling into an effectively-uncapped budget on the steady-state idle component. The webhook ladder rungs (3-5) attack the latency floor; the ETag + adaptive-interval rungs (1-2) attack the budget cost. Two orthogonal axes — both addressed by the same ladder.

---

## §2 Goals + non-goals

### Goals (rungs 1 + 2, self-host phase)

- **G1.** Cut rate-limit consumption ≥80% on idle windows (5-min window, no GH state change in scope). Measured via `X-RateLimit-Remaining` delta over the window pre/post rung 1.
- **G2.** Sub-second perceived latency on PR comment / review submitted (rungs 3-5 territory — deferred; G2 is the Phase-X reopen target, NOT a self-host-phase commitment). The self-host-phase G2 surrogate is "p99 perceived latency ≤ effective poll interval after rung 2 backoff is *suppressed* during the active window" — i.e. busy repo polls at floor cadence, idle repo backs off without operator config.
- **G3.** Zero public-ingress surface. The single-operator self-host filter rejects any rung that requires a publicly-reachable URL on the operator's machine. Outbound-only relays (rung 4) and full webhook ingress (rung 5) sit behind reopen triggers.

### Non-goals (explicit, with §5 reopen triggers)

- **NG1.** Full webhook receiver with multi-tenant routing (HMAC + replay store + per-tenant secret rotation). Out of scope under `feedback_default_simpler` + the self-host filter; reopen on external-customer ask.
- **NG2.** Sigstore-signed webhook delivery receipts. Phase-X enterprise wedge per `CLAUDE.md` self-host filter ("no Sigstore").
- **NG3.** Cross-repo coalesced polling (one HTTP call mirrors N repos via a fan-out service). Reopen when the operator drives >5 target repos simultaneously.
- **NG4.** Persisted ETag store. In-memory map is sufficient — restart re-warms in ≤1 poll tick; the storage primitive (sqlite + migration) is more expensive than the recovery cost it prevents (`feedback_default_simpler`).

---

## §3 Design — the five-rung ladder

Each rung is independently shippable; ordering reflects cost-of-implementation × operator-felt-benefit. Rungs 1 + 2 are in scope for the self-host phase; rungs 3-5 are documented for forward-fit and to keep `#1165`-class operator asks legible.

### Rung 1 — ETag conditional GET on GH list endpoints (self-host phase, in scope; closes #1164)

**Mechanism.** Wrap the (to-be-introduced) HTTP transport under `internal/ghclient/` with an ETag-aware round-tripper:

- On first response, capture `ETag` header into `map[urlKey]string` keyed by `(method, url, accept-header)`.
- On subsequent poll for the same key, send `If-None-Match: <etag>`.
- On `304 Not Modified`, return the cached parsed result + emit `ghclient.poll.not_modified` event. Per GitHub REST docs, `304` does NOT decrement `X-RateLimit-Remaining`.
- On `200`, replace cached ETag + cached parsed body; emit `ghclient.poll.miss`.
- On any non-200/304 (403 rate-limited, 5xx, network), emit `ghclient.poll.error`; do NOT invalidate the cache — the next successful 200 replaces it.

**Per-URL ETag store: in-memory only.** Restart re-warms in ≤1 tick per consumer. Persistence is a cost-without-benefit per `feedback_default_simpler`.

**Event vocabulary.** Three additions: `ghclient.poll.hit` (200 with payload replaced), `ghclient.poll.not_modified` (304), `ghclient.poll.error`. Wired via `internal/obs/` per `CLAUDE.md` event-vocabulary rules. The reviewer-verdict gate flags `internal/obs/` changes as load-bearing — the implementer PR will need independent reviewer dispatch.

**Acceptance.** §4.1.

**Cost.** ~250 LoC in `internal/ghclient/` (transport + cache + test) + ~20 LoC in `internal/obs/` (event constants). One implementer wave; reviewer-skip predicate per `feedback_review_proportional` does NOT apply (obs/ surface is load-bearing). Closes #1164.

### Rung 2 — Per-resource adaptive poll interval (self-host phase, in scope)

**Mechanism.** Extend the `MinPollInterval` primitive at `internal/orchestrator/adaptersync/adaptersync.go:131` with a multiplicative-backoff `EffectivePollInterval` computed per-resource (per-adapter, per-watched-PR-set):

- Active state (default): `effective = configured PollInterval`.
- After N=3 consecutive empty polls (no new work items for adaptersync; no head-SHA / state change for prwatch), double the effective interval up to a ceiling of `8 × configured`.
- On any non-empty result, immediately reset to `configured` (the active-window snap-back, NOT a gradual decay — operator-felt latency wins under `feedback_decision_priority` UX > performance).

**Design strategy — adaptive backoff state-machine.** Per-resource counter `consecutiveEmptyPolls int` initialized 0; current backoff multiplier `mult int` initialized 1. On each poll completion:

- Empty result: `consecutiveEmptyPolls++`. When `consecutiveEmptyPolls >= 3`, double `mult` (1 → 2 → 4 → 8) up to the ceiling of 8×, then reset `consecutiveEmptyPolls = 0` so the next doubling window starts fresh. Emit `ghclient.poll.backoff resource=<key> mult=<n>` at the moment of doubling.
- Non-empty result: snap `mult = 1` AND `consecutiveEmptyPolls = 0` in one transition. Emit `ghclient.poll.snap_back resource=<key>` once per snap-back (not on every floor-cadence poll).
- Gate read: `EffectivePollInterval = configured × mult`. The doubling window is bounded — at ceiling, `mult` saturates at 8 and stops growing regardless of further empty polls.

**Implementation surface.** Extend `internal/orchestrator/adaptersync/` `MinPollInterval` gate at line ~131: introduce a per-`Syncer` struct field `backoff backoffState` (3 ints: `mult`, `consecutiveEmpty`, plus a `ceiling` set from `configured × 8` at construction). The gate at line 131 reads `s.backoff.effective(configured)` in place of bare `configured`. Method `backoffState.effective(configured time.Duration) time.Duration` returns the effective interval; the gate at `adaptersync.go:131` substitutes the return value directly in place of `configured`. Mirror the same `backoffState` struct in `internal/orchestrator/prwatch/` `Watcher` — per-resource, NOT a shared package-global. New events declared in `internal/obs/events.go`: `EventGhclientPollBackoff = "ghclient.poll.backoff"` and `EventGhclientPollSnapBack = "ghclient.poll.snap_back"` (registry append + event-kind table row per `CLAUDE.md` event-vocabulary rules).

**Scope is per-resource, not global.** A quiet adapter must not slow down a busy prwatch sweep. Counter lives in the `Syncer` / `Watcher` struct, not a process-global.

**Event vocabulary.** Two additions: `ghclient.poll.backoff` (multiplier doubled), `ghclient.poll.snap_back` (multiplier reset to 1 on first non-empty result). Wired via `internal/obs/` per `CLAUDE.md` event-vocabulary rules — same load-bearing-surface reviewer-dispatch requirement as Rung 1.

**Acceptance.** §4.2.

**Cost.** ~80 LoC + a `testutil.AssertStable`-driven test (per `CLAUDE.md` `check-no-bare-sleep` rule). Spec-pattern authority: implementer MUST NOT pick the backoff curve — re-spawn designer if doubling-up-to-8× is wrong for any consumer (`feedback_spec_pattern_authority`).

### Rung 3 — GH events API single stream (Phase-X forward-fit)

**Mechanism.** Single `/repos/:owner/:repo/events` poll replaces the N-endpoint fan-out (issues + PRs + check-runs + comments collapse into one stream). Per-poll cost: 1 request instead of N; tradeoff: response is unstructured event log, requires client-side fan-out demux.

**Why deferred.** Rung 1 already converts the N-endpoint fan-out to mostly-304s; rung 3's only net win is on a busy repo where rung 1's 304-ratio drops. The self-host repo (this one) has nowhere near the activity to make rung 3's complexity pay back — the events stream is denormalized, requires client-side state reconstruction, and the demux logic itself is ~500 LoC. Reopen trigger in §5.3.

### Rung 4 — smee.io / `gh webhook forward` hybrid (Phase-X forward-fit, customer-ask trigger; closes #1165)

**Mechanism.** Outbound-only webhook relay (`smee.io`, `gh webhook forward`, Cloudflare Tunnel) forwards GitHub webhooks to `127.0.0.1`-bound regatta listener. On valid event, regatta enqueues a **targeted GET** for the affected resource (which benefits from rung 1's ETag cache → 304 if unchanged, 200 if the webhook caught a real edge). The webhook payload is informational only — never authoritative. Lost events are not fatal: the rung-1/rung-2 poll loop still catches up on the next tick.

**Why this is the right hybrid shape.** Push notifies, regatta still pulls detail. No public ingress (the relay is outbound-only from the operator's machine). HMAC validation (`X-Hub-Signature-256`) still required on the inbound side from the relay — defense-in-depth against a compromised relay. Dedup window 30s on `(event_id)` per #1165.

**Why deferred under self-host filter.** Adds: a listener on `127.0.0.1`, HMAC validation primitive, dedup store, relay-setup runbook, and a second secret (`WEBHOOK_SECRET`) to the operator's `.env`. The self-host operator's pain (10s lag floor) is real but not dispatch-blocking; the relay-ops burden is non-trivial. Reopen trigger in §5.4.

### Rung 5 — Full webhook + Cloudflare Tunnel + HMAC + replay store (Phase-X enterprise-ask trigger)

**Mechanism.** Direct GitHub → public-URL ingress with Cloudflare Tunnel termination, full HMAC validation, persisted replay store for at-least-once delivery semantics, and per-tenant secret rotation.

**Why deferred.** This is the multi-tenant SaaS shape — orthogonal to the self-host filter. Reopen only when (a) regatta is deployed by an enterprise customer with an SRE team to own the ingress, or (b) the self-host operator base grows past the point where outbound relays (rung 4) are insufficient. Reopen trigger in §5.5.

---

## §4 Acceptance per rung

### §4.1 Rung 1 acceptance (closes #1164)

- **A1.1.** Unit test: with a stubbed transport, a first `GET /repos/:owner/:repo/issues` returns 200 + `ETag: "abc"`; the second call to the same URL via the ETag-wrapped client MUST send `If-None-Match: "abc"`. Assert on the recorded request header.
- **A1.2.** Unit test: when the stubbed transport returns 304 to the second call, the ETag wrapper MUST return the parsed result cached from the first call (byte-identical struct) and emit `ghclient.poll.not_modified` once.
- **A1.3.** Integration / soak: over a 5-minute idle window against a real test repo, `X-RateLimit-Remaining` MUST drop by no more than 20% of the rung-0 baseline drop for the same window. Measured via a recorded gauge — NOT a self-reported log line per `feedback_validate_before_ship`.
- **A1.4.** TDD discipline: failing test commit lands FIRST; PR body shows the RED output (`feedback_tdd_discipline`).

### §4.2 Rung 2 acceptance

- **A2.1.** Unit test: with N=3 configured, after 3 consecutive empty `adapter.List` returns, the next `Sync` call MUST short-circuit per the doubled `EffectivePollInterval` even when `pollStartedAt - lastPoll` exceeds the *configured* `MinPollInterval` (i.e. the gate at `adaptersync.go:131` now reads the effective, not the static, budget).
- **A2.2.** Unit test: the FIRST non-empty `adapter.List` after backoff MUST snap the effective interval back to configured — assert via `testutil.AssertStable` on the next 3 ticks running at floor cadence.
- **A2.3.** Empty-poll counter is per-resource, not global. Test: two `Syncer` instances on different adapters; one going quiet MUST NOT slow down the other.
- **A2.4.** No `time.Sleep` inside `for` blocks in the new test file — uses `testutil.Eventually` / `AssertStable` per `CLAUDE.md` `check-no-bare-sleep`.

### §4.3 Rungs 3-5 acceptance

Deferred. Each carries the implicit "design spec lands before implementation" gate per `feedback_spec_pattern_authority` — i.e. before any work, a follow-up design spec re-validates the rung against the then-current self-host filter. Reopen-trigger satisfaction (§5) is the entry condition.

---

## §5 Out of scope per rung; reopen triggers

### §5.1 Rung 1 — none. In scope, ships in the self-host phase.

### §5.2 Rung 2 — none. In scope, ships immediately after rung 1.

### §5.3 Rung 3 reopen trigger (GH events API)

Reopen when **any** of:

- Rung 1's measured 304-ratio drops below 60% in steady state (i.e. the repo gets busy enough that conditional GETs stop helping).
- A second high-cadence consumer is added (a third polling loop beyond `adaptersync` + `prwatch`) and would push the N-endpoint fan-out toward the 5000/hr budget under sustained load.
- The operator dispatches against ≥3 target repos simultaneously and the cross-repo budget pressure becomes the binding constraint.

### §5.4 Rung 4 reopen trigger (smee.io / `gh webhook forward` hybrid; closes #1165 once triggered)

Reopen when **any** of:

- External-customer ask: a non-operator user runs regatta against their own repo and reports the 10s lag floor as a dispatch-blocker (i.e. the latency becomes load-bearing for someone other than the sole internal operator).
- The operator's review-handoff cadence becomes the bottleneck on agent throughput — measured by a sustained gap of ≥30 minutes between PR-ready-for-review and reviewer subagent dispatch attributable to poll latency, NOT operator availability. Surfaces via existing event vocabulary (`prwatch.*` timestamps vs `reviewer.dispatched`).
- Rung 2's adaptive backoff over-tunes — busy-window polls hit budget pressure while the idle-window backoff is at ceiling. Indicates the polling shape is the wrong primitive; webhook hybrid becomes net-cheaper.

### §5.5 Rung 5 reopen trigger (full webhook + Cloudflare Tunnel)

Reopen only when **all** of:

- Regatta is deployed in a multi-tenant configuration (multiple operators, multiple tenants), per the Phase-X tenant_id rollout (gated by `scripts/check-phase-x-leak.sh` today).
- An SRE team — not the regatta operator — owns the ingress, secret rotation, and replay-store operations.
- HMAC-signed webhook delivery is a customer security requirement, not a regatta-internal preference.

Per the self-host filter in `CLAUDE.md`: "No RBAC, no billing, no htmx UI, no Sigstore, no blackboard." Rung 5 is the polling-loop analog of these — rejected today, surfaced as forward-fit so future operators see the design history.

---

## §6 References

- **Issues**: #1164 (rung 1, in-scope, closed-by this spec's rung-1 implementer PR), #1165 (rung 4, deferred under §5.4 reopen trigger).
- **Existing surface**:
  - `internal/ghclient/client.go:7-35` — current `Client` interface; today a gh-CLI subprocess (`GHCLIClient`, line 78-92). Rungs 1 + 3 + 5 require introducing an HTTP transport seam alongside this — additive, not a replacement (`feedback_deletion_default` answer: rung 1 deletes the per-poll-budget tax, not LoC).
  - `internal/orchestrator/adaptersync/adaptersync.go:122-134` — `MinPollInterval` gate; closest sibling primitive for rung 2's adaptive backoff. Rung 2 extends this in place.
  - `internal/orchestrator/adapter/githubissues/adapter.go:281` — per-adapter `MinPoll` wiring via `Capabilities()`. Rung 2 surfaces an `AdaptiveBackoff` capability in the same shape.
  - `internal/orchestrator/prwatch/ghcli.go:61-71` — `prwatch` PR-list query surface. Second consumer of any rung-1 budget win; co-equal beneficiary.
  - `internal/orchestrator/prwatch/prwatch.go:44-50` — poll tick rationale ("12 ticks × 5s ≈ 1 minute — rides out a `gh pr list` blip"). Rung 2's backoff must not interfere with the 12-tick disagreement-tolerance budget.
- **Memory rules**: `feedback_default_simpler` (rejects rungs 3-5 in self-host phase), `feedback_decision_priority` (UX > performance: rung 2's active-window snap-back over gradual decay), self-host filter from `CLAUDE.md` (rejects rung-5-class public-ingress wedges absent enterprise reopen trigger).
- **Plan-master**: this spec is the artifact #1164 + #1165 cluster onto. Per `feedback_audit_main_before_implementing`, before dispatching the rung-1 implementer, confirm no in-flight PR already introduces an HTTP transport under `internal/ghclient/` (current main has gh-CLI subprocess only — confirmed via `internal/ghclient/client.go` read 2026-06-09).
