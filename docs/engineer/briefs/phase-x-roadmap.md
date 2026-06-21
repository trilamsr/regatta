---
status: active
phase: x-forward-fit
---

# Phase-X roadmap — consolidated tracking surface

_Author: 2026-06-07 design session. Single tracking doc for every Phase-X-parked followup. Replaces 29 individual `[phase-x]`-labeled issues — those close with a back-reference here. Reopen triggers (§3) are the authoritative gate for moving work back into scope._

## 1. Purpose

29 open `[phase-x]` issues each carried its own reopen-trigger, source spec/PR cite, and approach sketch. Each was filed against the self-host filter (`docs/engineer/briefs/2026-06-01-self-host-first.md` §1 + §4): defer until an external paying customer asks. Tracking 29 tiny issues is overhead — closing them and folding the substance into one doc keeps the reopen-triggers searchable without inflating the issue tracker.

The §6 ledger preserves the issue-number → bucket map so closed-issue search still resolves.

## 2. Phase-X definition

Per `CLAUDE.md` "Self-host filter" + `docs/engineer/briefs/2026-06-01-self-host-first.md` §1: self-host = single-tenant, single-operator, single-repo, deterministic CI, human-merge via GitHub branch protection. Phase-X = work that fails the filter — not needed for the sole internal operator to dispatch `regatta`-the-binary against this repo unattended.

Token convention (informational, post-MAY-31 demote): Phase-X tokens include `tenant_id`, `RBAC`, `Stripe`, `Sigstore`, `Rekor`, `blackboard`, `Temporal`, `htmx`. Specs that intentionally explore these live under `docs/engineer/specs/phase-x/`; `make pre-push-check` greps for them as an operator-glance hint.

## 3. Per-bucket roadmap

### B1 — Multi-tenant / RBAC (W8)

**Issues:** #492, #218, #221.

W8 wedge: per-context tenant resolver replaces the `regatta.tenant_id=default` constant baked into the OTel resource (`internal/obs/otel/setup.go::buildResource`); CLI `regatta substrate retag-tenant --run-prefix=<X> --tenant=<Y>` re-tags + re-signs rows on single→multi cutover; substrate_policies primitive adds the `(tenant_id, kind, key, strategy, ttl)` table that overrides hardcoded `defaultReducer()` strategies. Specs: `docs/engineer/specs/phase-x/2026-06-01-unified-substrate-design.md` §S1+§12 F3/F7, `2026-06-02-observability-roadmap.md` §9 R8.

**Reopen trigger:** signed LOI from a customer requiring data isolation between ≥2 tenants on the same regatta instance. NOT lumalabs-internal multi-project (one operator = one tenant).

**Effort:** ~M (3-5 subagent-days).

### B2 — Multi-host / horizontal scaling (W9+)

**Issues:** #220, #614, #222, #219.

W9+ wedge: UUIDv7 (RFC 9562) replaces Crockford-base32 ULID minter once cross-host writers can collide on same-ms PKs; webhook DB-level uniqueness on `(alert_fingerprint, window)` with `INSERT ... ON CONFLICT` replaces in-process idempotency map; `substrate_blobs` content-addressed store + ref-counted mark-and-sweep GC (default `gc_grace_seconds=3600`) populates the forward-fit `substrate_events.blob_digest` column; per-kind TTL cron + `archive_audit_outbox` bounds growth (heartbeats fast, approval_events never, node_output per work_item).

**Reopen trigger:** webhook scaled to >1 replica in any deployment OR duplicate-issue observation in prod OR substrate_events row count exceeds 10M on a single-host deployment.

**Effort:** ~L (10-15 subagent-days; multi-host story is the largest single bucket).

### B3 — Billing / Stripe (W12)

**Issues:** #729, #588.

W12 wedge: Stripe metering / billing spec umbrella (`docs/engineer/specs/2026-06-01-w12-billing-design.md` — currently `status: skeleton-prefetch`). Spend-event payloads dual-emit `usd` (float64, legacy) + `usd_micro` (int64, canonical) per #570/#560 review; drop the float field after 90 days of clean canonical-int consumer reads.

**Reopen trigger:** external customer LOI requesting metered billing OR multi-tenant scope unlock (B1 ships first — billing without tenancy has no invoice recipient distinction).

**Effort:** ~L (10-15 subagent-days; W12 spec body itself is unwritten beyond skeleton).

### B4 — GDPR / compliance / crypto-shredding

**Issues:** #548, #606, #607.

Compliance pitch (SOC2, EU AI Act traceability) sells an append-only HMAC-signed hash-chained event log. `substrate_events` payloads carry PII (prompts, code with names/emails, Slack handles in approval tokens). GDPR Art.17 / CCPA right-to-erasure cannot delete a row from a hash-chained signed log without breaking downstream signatures — direct legal contradiction. Approach: per-subject/per-work-item DEK; erasure = destroy the key; ciphertext remains in chain. SOC2 / compliance spec to be authored at reopen trigger fire — no skeleton today. Two prerequisites at that time: legal precedent confirming key-destruction satisfies Art.17 (EDPB/DPA citation), and JWE/JOSE (RFC 7516) prior-art comparison before locking the bespoke `{v,kid,ct,aad}` envelope.

**Reopen trigger:** regulated/EU customer LOI requiring Art.17 compliance via crypto-shredding. Do NOT use the word "compliance" in customer-facing collateral until resolved.

**Effort:** ~L (legal review + crypto-eng design = 15-20 subagent-days plus external counsel cost).

### B5 — Schema evolution / substrate forward-fit

**Issues:** #217, #223.

`schema_version` v2 migration recipe — first real kind-payload bump: bump const for affected kind only → ship versioned canonicalization helper → run one release cycle with v1+v2 verifiers → writers may emit v2 only → operator runbook entry. Reducer-strategy re-fold tool: `regatta substrate refold --run=<X> --kind=<K> --from=lww --to=append` asserts old strategy on disk, writes new policy row, emits audit event. Specs: `docs/engineer/specs/phase-x/2026-06-01-unified-substrate-design.md` §5+§8+§12 F2/F10.

**Reopen trigger (F2):** first kind-payload schema change post-Wave 1 (no v2 exists yet). **Reopen trigger (F10):** B1 substrate_policies primitive ships (F10 depends on F7).

**Effort:** ~S (2-4 subagent-days each; both well-scoped).

### B6 — Adversarial-review / Phase-S deferrals

**Issues:** #324, #678, #679, #728.

`check-tdd` downgrade-to-warning rejected (violates `feedback_tdd_discipline`); lower-risk `[REFACTOR]` opt-out expansion deferred. Windows GitHub Actions runner matrix entry + `TestStatus_RendersOnWindows` smoke test for stdlib ANSI renderer. Mutation testing on `internal/triggers/greenclock.go` via `gremlins` / `go-mutesting` — requires Makefile target + per-package config + CI gate-vs-informational decision. S2-T2 adversarial L4 gate umbrella (`docs/engineer/specs/phase-x/2026-06-02-s2-t2-adversarial-l4-gate.md`) — `scheduler.l4Gate` rejects `mode=adversarial` verdicts.

**Reopen triggers (each falsifiable):**
- check-tdd: ≥5 PRs blocked-on-check-tdd in 7-day window where blocker was a refactor PR with zero new prod code.
- Windows: Windows-running operator files a bug reproducible on Windows but green on Linux OR Linux CI flake indicates an OS-specific failure reproducible only on non-Linux runners. (Environmental availability — GH Actions Windows runner quota — is necessary infrastructure but not by itself a trigger; need a real Windows-specific failure to investigate.)
- Mutation testing: greenclock regression bypasses `TestGreenClock_ConsecutiveStreakProperty` OR triggers misbehave under edge inputs (zero events, future-dated, DST rollover).
- L4 gate: child PRs implementing the L4 reviewer all merge AND 7 consecutive days green property-test rejection of adversarial verdicts.

**Effort:** ~S each (2-3 subagent-days).

### B7 — Wave-trigger-gated wiring

**Issues:** #140, #306, #828, #731.

CHECK constraint on `approval_events.kind` enum after Wave 5 e2e shakedown locks the final list (`requested,notified,decided,approved,rejected,timed_out,escalated,token_consumed,token_revoked`). `Templates.RegisterFunc` extension for `/runs/{run_id}` handlers when W7 Wave 2 lands — range helpers + sort funcs the W7 Wave 1 `formatTime`/`formatBytes`/`formatTokens`/`formatUSDMicros`/`truncate`/`humanizeDuration`/`safeURL`/`csrfToken` whitelist doesn't cover. `planner.go` / `planner_v2.go` / `planner_stub.go` pick-one audit before kill — sequence after Substrate Wave 1 cutover refactors planner call-sites (per `feedback_cascade_rebase_root_cause`). `orchestrator.script_plan.enforce` default flip `false`→`true` after shadow-mode burn-in.

**Reopen triggers:**
- #140: Wave 5 e2e ships.
- #306: W7 Wave 2 `/runs/{run_id}` handlers (T8) ship.
- #828: Substrate Wave 1 cutover lands.
- #731: 30 consecutive days of green shadow-mode rejections OR deterministic regression suite covering the L0-L6 false-positive surface.

**Effort:** ~XS-S each.

### B8 — Self-improve / LLM / replay

**Issues:** #620, #621, #630.

`regatta self-improve mute <pattern>` (suppress noisy observation type) + `regatta self-improve replay <run-id>` (re-run LLM analysis on captured run for debugging). Production Anthropic wiring behind `//go:build llm_anthropic` build tag using official SDK with prompt caching; env: `ANTHROPIC_API_KEY`, model id; stub remains default. Replay-latency P95 baseline bench/test for fixed-size sample chain — cannot land until Phase-X Replay impl exists.

**Reopen triggers:**
- mute/replay CLI: self-improve emits ≥10 false-positive observations per week OR a regression in the analyzer requires replay capability.
- Anthropic wiring: decision to enable self-improve in production OR external customer asks for LLM-backed analysis.
- Replay P95 baseline: Phase-X Replay impl PR opens (chained).

**Effort:** ~S each.

### B9 — Operational extensions

**Issues:** #615, #617, #551, #727.

`GH_TOKEN` SIGHUP / file-watcher hot-reload — current `internal/webhook` reads once at startup, rotation requires process restart. GPL-2 NOTICE + source-offer for `regatta-with-pass` container variant (`pass` Linux password-store backend in PR #592). Generalize intent/outbox primitive (`intent(key) -> effect(idempotent) -> executed(key)` riding substrate nonce) beyond W2 merge — approval-notify is currently class C (unsafe ordering, unguarded); first real Slack/email notifier will double-send on retry. Cost-governor design spec umbrella (`docs/engineer/specs/phase-x/2026-06-01-cost-governor-design.md`) — T1-T5 PRs all SHIPPED per #796; remaining IN-SCOPE work is #796 P2-1 soak script + #796 P3-1 schema-pin (closes #277), bucket holds these.

**Reopen triggers:**
- GH_TOKEN hot-reload: operator performs emergency token rotation OR scheduled-rotation policy adopted.
- GPL-2 pass: decision to ship `regatta-with-pass` image OR user requests pass-backed deployment.
- Intent/outbox generalize: first real approval notifier (Slack/email) wired — currently `approval.NewStubNotifier`. OR W2 c0 lands (extract from W2 impl rather than parallel-build).
- Cost-governor umbrella: 7 consecutive days green substrate emission of `budget_reconciled` events on `main` (pending #796 P2-1 soak script + observation window).

**Effort:** ~S-M each.

## 4. Cross-bucket dependencies

- B3 (billing) depends on B1 (multi-tenant) — billing without tenancy has no invoice recipient distinction.
- B4 (GDPR crypto-shredding) depends on B1 (per-tenant DEK key registry shares the tenant identifier scope).
- B5 F10 re-fold (`#223`) depends on B1 F7 substrate_policies (`#221`).
- B2 F4 TTL (`#219`) depends on B1 F7 substrate_policies (`#221`).
- B6 mutation-testing tooling-add (`#679`) blocks any future mutation-testing reopen across the repo.
- B7 #828 planner pick-one waits on Substrate Wave 1 cutover (already in flight per main-branch state).
- B8 #630 replay-latency baseline waits on B8 #621 Anthropic wiring + Phase-X Replay impl.
- B9 #551 intent/outbox extracts from W2 c0 impl (`docs/engineer/briefs/2026-06-02-phase-autonomy-amendment.md` §W2).

## 5. Activation order (first-trip estimate)

Reopen-trigger likelihood, descending:

1. **B7** wave-trigger-gated — internal triggers (Substrate Wave 1, W7 Wave 2, Wave 5) fire on the self-host loop; mechanical, not gated on external buyer.
2. **B5** schema evolution — first kind-payload bump post-Wave 1 likely lands within Phase-S timeline.
3. **B6** adversarial-review / Phase-S — false-positive measurements accumulate from normal self-host operation.
4. **B9** operational extensions — GH_TOKEN rotation or Slack/email notifier wiring driven by routine ops.
5. **B8** self-improve / LLM — gated on decision to enable self-improve in production.
6. **B2** multi-host — gated on substrate row count or webhook replica scaling; both lag self-host phase.
7. **B1** multi-tenant / RBAC — gated on external customer LOI requiring ≥2 tenants.
8. **B3** billing — gated on B1 (per §4 dep).
9. **B4** GDPR / compliance — gated on regulated/EU customer LOI + external legal counsel cost.

## 6. Issue-to-doc migration ledger

| # | Title fragment | Bucket |
|---|---|---|
| 828 | planner.go pick-one audit | B7 |
| 731 | script_plan.enforce default flip | B7 |
| 729 | W12 Stripe billing umbrella | B3 |
| 728 | S2-T2 adversarial L4 gate umbrella | B6 |
| 727 | cost-governor design umbrella | B9 |
| 679 | mutation testing on greenclock.go | B6 |
| 678 | Windows CI runner matrix | B6 |
| 630 | T4 replay-latency P95 baseline | B8 |
| 621 | production Anthropic LLM wiring | B8 |
| 620 | self-improve mute/replay CLI verbs | B8 |
| 617 | GPL-2 container variant tracking | B9 |
| 615 | GH_TOKEN rotation hot-reload | B9 |
| 614 | cross-host idempotency | B2 |
| 607 | JWE/JOSE prior-art comparison | B4 |
| 606 | GDPR Art.17 crypto-shredding precedent | B4 |
| 588 | dual-emit USD float field deprecation | B3 |
| 551 | intent/outbox primitive generalization | B9 |
| 548 | GDPR/CCPA erasure crypto-shredding | B4 |
| 492 | tenant_id propagation (W8 RBAC) | B1 |
| 324 | check-tdd downgrade rejected | B6 |
| 306 | Templates.RegisterFunc Wave 2 extension | B7 |
| 223 | F10 reducer-strategy re-fold tooling | B5 |
| 222 | F8 substrate_blobs CAS + ref-counted GC | B2 |
| 221 | F7 substrate_policies primitive (W8) | B1 |
| 220 | F6 UUIDv7 vs ULID post-W9 multi-host | B2 |
| 219 | F4 per-kind TTL + archive_audit_outbox | B2 |
| 218 | F3 tenant_id retag helper | B1 |
| 217 | F2 schema_version v2 migration recipe | B5 |
| 140 | approval_events.kind CHECK Wave 5 | B7 |
