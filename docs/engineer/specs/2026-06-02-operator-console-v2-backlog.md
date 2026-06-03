---
status: backlog
companion_to: docs/engineer/specs/2026-06-02-operator-console-design.md
date: 2026-06-02
last_revised: 2026-06-03
purpose: explicit deferred-item registry with trigger conditions
---

# Regatta Operator Console — v2 backlog

Items intentionally deferred from v1. Each carries an explicit
**trigger condition** — when reality matches the trigger, lift the item
back into a v2 spec slice.

---

## A. Debug surface

### A1. Replay scrubber (60 fps @ 10k events)
- **Trigger:** operator post-mortems > 3 PR failures / week using raw
  event list and reports slowness.
- **Cost:** 2-3 wk.
- **Prereq:** content-addressed `causal_hash` permalink (already v1).

### A2. Trace viewer link template
- **Trigger:** operator runs Jaeger / Tempo / Honeycomb and asks for
  per-trace jump.
- **Cost:** 2-3 d (config string + URL allowlist + scheme allowlist).

### A3. Postmortem visual timeline (decision-tree viz)
- **Trigger:** raw timeline insufficient for operator.
- **Cost:** 1-1.5 wk.

### A4. Signed-handoff inspector
- **Trigger:** persona-D or persona-B lands.
- **Cost:** 3-5 d.

---

## B. Dashboards

### B1. Cost time-series (canvas + LTTB + rollups)
- **Trigger:** operator queries cost rollup > 5x / day OR audit spend
  > X / day where X needs review.
- **Cost:** 1.5-2 wk.

### B2. Fleet table virtualized
- **Trigger:** > 50 active PRs sustained AND inbox-only view causes
  confusion.
- **Cost:** 1-1.5 wk.

### B3. Top-N cost breakdown
- **Trigger:** cost-spike investigation > 2x / week.
- **Cost:** 3-5 d.

### B4. Budget-vs-plan gauge
- **Trigger:** sparkline insufficient.
- **Cost:** 2-3 d once B1 ships.

### B5. SLO panels in console
- **Trigger:** operator wants in-console primary SLO check.
- **Decision:** likely never — Grafana stays canonical.

---

## C. v2 hedges (multi-tenant / embed / mobile-first)

### C1. `tenantID` + `repo` context-threading
- **Trigger:** persona-B or multi-repo goal.
- **Cost:** 1-1.5 wk.

### C2. `/t/<tenant>/r/<repo>/` URL prefix
- **Trigger:** C1 fires.
- **Cost:** included in C1.

### C3. Dual auth-mode (bearer for vendor clients)
- **Trigger:** CLI client demand OR embed-mode partner OR mobile-PWA
  push OR persona-B per-user tokens.
- **Cost:** 3-5 d (regatta-self bearer already v1).

### C4. Embed mode (`?embed=1` chromeless + parent-origin allowlist)
- **Trigger:** vendor partner asks to iframe regatta.
- **Cost:** 1 wk incl. CSP `frame-ancestors` allowlist + Partitioned
  cookie or cookie-rename off `__Host-`.

### C5. Mobile PWA + push notifications
- **Trigger:** operator approves > 5 PRs / week from phone AND
  Slack-deep-link timing feels slow.
- **Cost:** 1.5-2 wk.

### C6. Loop detector
- **Trigger:** operator surfaces first real loop case (same tool-call
  signature N times → cost waste).
- **Cost:** 2-3 wk total — 1-2 wk substrate plumbing + 1 wk UI.
- **Hard prereq:** substrate emits new event kind w/ tool-call
  signature (partial — `tool_call.signature` ships in v1 §3.2).

### C7. "Since last visit" delta card
- **Trigger:** v1 absolute-counts inbox proves insufficient.
- **Cost:** 3-5 d.
- **Decision risk:** may add cognitive load vs absolute counts.

### C8. Persistent fleet-pulse strip
- **Trigger:** > 2 surfaces routinely need pulse summary.
- **Cost:** 3-5 d.

---

## D. Supply-chain ceremony

### D1. SLSA L2 → L3 provenance
- **Trigger:** public-domain deploy / persona-B / external paying
  customer / regulatory ask.
- **Cost:** 1-2 wk (hermetic CI build + non-falsifiable provenance +
  verification tooling).

### D2. Sigstore signing
- **Trigger:** D1 fires.
- **Cost:** 2-3 d once D1 in flight.

### D3. CycloneDX + SPDX dual-emit SBOM
- **Trigger:** D1 fires OR regulator asks.
- **Cost:** 2-3 d.

### D4. npm supply-chain hardening
- **Trigger:** any contributor outside operator OR > 10 npm deps.
- **Cost:** 3-5 d + ongoing CI maintenance.

### D5. Ban local-built `web/build/` commits
- **Trigger:** D1 fires.
- **Cost:** 1 d.

### D6. Sigstore Rekor transparency-log anchor
- **Trigger:** public-verifiability needed (persona-D research-
  credibility OR persona-B SOC-2-style external attestation).
- **Cost:** 2-3 d (mint regatta-self sigstore keypair via cosign +
  `hashedrekord` typed-entry signer + verify CLI extension).
- **Why deferred from v1:** Rekor v1 doesn't accept opaque hashes;
  requires `hashedrekord` typed entry + signing keypair. v1
  S3-bucket-object-lock + versioning alone provides write-once tamper-
  evidence. Sigstore is defense-in-depth + public-verifiability — not a
  v1 acceptance criterion.

---

## E. v1-nice-to-haves (deferred for ship velocity)

### E1. OpenAPI codegen (`go struct → openapi.json → ts types`)
- **Trigger:** hand-written `types.ts` drift causes > 2 bugs.
- **Cost:** 1-1.5 wk.

### E2. Per-surface store namespaces + sibling-import lint
- **Trigger:** more than one surface owner OR store collision.
- **Cost:** 1-2 d.

### E3. Bearer mode for CLI clients
- **Trigger:** `regatta cli` actions need to talk to console API.
- **Cost:** included in C3.

### E4. WCAG AAA (vs AA)
- **Trigger:** persona-D / accessibility-mandate.
- **Decision:** AA = industry floor; AAA reserved for kill/override
  contrast pairs only.

### E5. i18n / l10n
- **Trigger:** non-English operator OR persona-B non-English team.
- **Cost:** 1-1.5 wk.

### E6. Cost rollup tables
- **Trigger:** cost queries on raw substrate cross 100 ms p95.
- **Cost:** 1-1.5 wk.

### E7. UI request OTel tracing
- **Trigger:** failure crosses UI → API → scheduler → adapter
  without end-to-end trace.
- **Cost:** 3-5 d.

### E8. Audit history full-text search
- **Trigger:** audit table > 10k rows AND operator wants historical
  lookup.
- **Cost:** 3-5 d (sqlite FTS5).

### E9. Persistent rate-limit table
- **Trigger:** rate-limit needs to survive process restart.
- **Cost:** 1-2 d.

### E10. Persistent idempotency dedupe (vs 5-min memory window)
- **Trigger:** mobile flaky double-submit observed past 5-min window
  OR longer replay guarantee.
- **Cost:** 1-2 d.

---

## F. Operator-experience polish (post-v1)

### F1. Custom dashboards (operator-defined widget grid)
- **Trigger:** persona-B only.
- **Decision:** YAGNI; saved filters in URL handle it.

### F2. Saved searches / saved filters with names
- **Trigger:** operator re-types same filter > 3x / week.
- **Cost:** 2-3 d.

### F3. Notification preferences UI
- **Trigger:** > 1 notification channel (Slack + email + push).
- **Cost:** 3-5 d.

### F4. Per-operator preferences
- **Trigger:** multi-user lands.
- **Cost:** 2-3 d.

### F5. Bulk operations (multi-select rows → approve all / kill all)
- **Trigger:** operator routinely triages > 10 stuck rows at once.
- **Cost:** 3-5 d.

### F6. CSV / JSON export (inbox / audit / cost)
- **Trigger:** offline analysis needed (partial — `/self-actions`
  ships export in v1).
- **Cost:** 2-3 d.

---

## Triage rules

1. **One item per PR.** Don't bundle backlog lifts.
2. **Re-validate trigger** before lifting — confirm load-bearing.
3. **Re-estimate** costs at lift time.
4. **Reference companion** v1 spec as baseline.
5. **Update this backlog**: mark `[SHIPPED]` with merge SHA + PR #;
   don't delete (keep prior-art trail).
