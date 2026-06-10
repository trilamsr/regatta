---
name: "ETag conditional GET — implementer brief for polling-improvement-ladder Rung 1"
slug: 2026-06-09-etag-conditional-get-impl
status: draft
phase: self-host-first
owner: trilamsr@gmail.com
created: 2026-06-09
summary: "Implementer-ready brief for Rung 1 of the polling-improvement-ladder spec (`docs/engineer/specs/2026-06-09-polling-improvement-ladder.md`). Introduces an ETag-aware `http.RoundTripper` under `internal/ghclient/` plus three new `internal/obs/events.go` event kinds (`EventGhclientPollHit`, `EventGhclientPollMiss`, `EventGhclientPollNotModified`). Targets ≥80% rate-budget reduction on a 5-min idle window via 304-Not-Modified short-circuit. Closes #1164."
---

# ETag conditional GET — Implementer Brief (Rung 1)

Tracks: #1164.
Sibling spec: `docs/engineer/specs/2026-06-09-polling-improvement-ladder.md` (Rung 1, §3 + §4.1).
Cross-ref: `internal/ghclient/client.go` (current gh-CLI seam — no `http.Client` exists yet; this brief introduces it), `internal/obs/events.go` (event-kind registry; load-bearing surface per `CLAUDE.md` reviewer-verdict gate).

Memory rules in force: `feedback_default_simpler`, `feedback_tdd_discipline`, `feedback_validate_before_ship`, `feedback_adversarial_review_every_step`, `feedback_no_signatures`, `feedback_spec_pattern_authority`, `feedback_subagent_cicheck_compress`.

---

## Problem

Every regatta polling loop (`adaptersync`, `prwatch`, the github_issues spec adapter) today drives state transitions through the `ghclient.Client` interface, currently backed only by a `gh` CLI subprocess (`internal/ghclient/client.go`). Idle-window polls return byte-identical bodies but still consume one GitHub REST request each — empirical 5-min idle window: 60 requests, ~95% redundant. The sibling polling-improvement-ladder spec §1 quantifies the cost; Rung 1 attacks it by routing GitHub list/get reads through an ETag-aware `http.RoundTripper` so the conditional-GET path returns `304 Not Modified` without decrementing the rate budget.

The implementer surface is purely additive: a new HTTP transport + an in-memory `(method, url) → etag` map + three new `obs/` event kinds. No existing call site changes shape; the gh-CLI client remains the alarm-path fallback. Rung 2 (adaptive backoff) lands next and reuses the same event vocabulary — keep it stable.

---

## Design

### HTTP transport wrap pattern

Introduce `internal/ghclient/etag_transport.go` exporting:

```go
// ETagTransport wraps a base http.RoundTripper, attaching If-None-Match
// from a per-(method,url) in-memory cache and short-circuiting 304s into
// the last successful parsed body. Concurrency-safe via sync.RWMutex.
type ETagTransport struct {
    Base  http.RoundTripper // nil → http.DefaultTransport
    clock func() time.Time   // injectable for tests
    mu    sync.RWMutex
    cache map[cacheKey]cacheEntry
    obs   ObsSink           // emits poll.hit / poll.miss / poll.not_modified
}

type cacheKey struct {
    Method, URL, Accept string
}

type cacheEntry struct {
    ETag string
    Body []byte // raw response body, cloned at insert
    Hdr  http.Header
}
```

`RoundTrip` flow (single round-tripper, no recursion):

1. Look up `cacheKey{req.Method, req.URL.String(), req.Header.Get("Accept")}` under `RLock`.
2. If hit, set `req.Header.Set("If-None-Match", entry.ETag)`.
3. Call `t.base().RoundTrip(req)`.
4. On `304`: drop the live body, fabricate `*http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(entry.Body)), Header: entry.Hdr.Clone()}`, emit `EventGhclientPollNotModified`. Return the synthetic 200 so call-site decoders are unchanged.
5. On `200`: read the body into a buffer (`io.ReadAll` with 8 MiB cap — GitHub list responses do not exceed this), capture `ETag` header, replace the cache entry under `Lock`, emit `EventGhclientPollMiss` if the cache entry was missing or `EventGhclientPollHit` if an entry existed but the ETag changed. Hand the buffered body back to the caller as `io.NopCloser(bytes.NewReader(buf))`.
6. On any other status (403, 5xx, network err): emit no event from this layer (Rung 2 owns `poll.backoff`); do NOT invalidate the cached entry — the next successful 200 replaces it. Return the response/err untouched.

### `(method, url) → etag` in-memory map

Keyed by `cacheKey{Method, URL, Accept}`. `Accept` is part of the key because GitHub varies representation by `Accept: application/vnd.github+json` vs `application/vnd.github.raw`. In-memory only — restart re-warms in ≤1 poll tick per consumer, and the persistence cost (sqlite + migration) is rejected by the sibling spec §2 NG4 under `feedback_default_simpler`.

Bound the map at 1024 entries with FIFO eviction on insert when full. The bound is defensive — production wiring covers <20 distinct URLs across all consumers, but a misconfigured caller iterating `?page=N` for unbounded `N` must not OOM the process.

### `internal/obs/events.go` additions

Three new event kinds appended to the registry (load-bearing under `CLAUDE.md` reviewer-verdict gate — implementer PR requires independent reviewer dispatch):

```go
const (
    EventGhclientPollHit          EventKind = "ghclient.poll.hit"
    EventGhclientPollMiss         EventKind = "ghclient.poll.miss"
    EventGhclientPollNotModified  EventKind = "ghclient.poll.not_modified"
)
```

Plus the event-kind table row per the obs/ vocabulary discipline (`CLAUDE.md` §"Worker-prompt parity gate" cites `internal/obs/` as load-bearing). The three kinds share schema `{resource, status_code, etag_present, elapsed_ms}`.

### `X-RateLimit-Remaining` gauge

Surface the most recently observed `X-RateLimit-Remaining` header value as an integer gauge in `internal/obs/`. Update on every successful 200 OR 304 response that carries the header. The gauge is the load-bearing measurement for Acceptance §A3 below — a self-reported event count is not sufficient per `feedback_validate_before_ship`.

---

## Acceptance

### A1 — `If-None-Match` propagation (failing test FIRST, per `feedback_tdd_discipline`)

Test `TestETagTransport_SendsIfNoneMatchOnSecondCall` in `internal/ghclient/etag_transport_test.go`. With a `httptest.NewServer` stub that records every inbound `If-None-Match` header and returns `ETag: "abc"` on the first call:

- First `GET /repos/o/r/issues` → no `If-None-Match` header sent; response captures `ETag: "abc"`.
- Second `GET /repos/o/r/issues` via the same transport → request MUST carry `If-None-Match: "abc"`. Assert on the recorded request header at the stub.

Failing-test commit lands first; PR body shows the RED output (compile-fail or assertion-fail — either is acceptable RED per `feedback_tdd_discipline`).

### A2 — 304 returns cached parsed value byte-identically

Test `TestETagTransport_304ReturnsCachedBody`. The stub returns `200 + body=B1 + ETag="abc"` on call 1, then `304` on call 2. The transport MUST return a synthetic 200 response on call 2 whose body bytes equal `B1` exactly. Decode both responses through the production `ghclient` decoder and assert struct equality via `reflect.DeepEqual` — not just `bytes.Equal` — so a future decoder regression also fails this test.

Also assert exactly one `EventGhclientPollNotModified` event was emitted for call 2 and exactly one `EventGhclientPollMiss` for call 1 (use a fake `ObsSink` that records emissions).

### A3 — `X-RateLimit-Remaining` gauge over 5-min idle window

Integration / soak test `TestETagTransport_RateLimitGauge_5MinIdle` (build-tag `integration`, opt-in). Requires `GH_TOKEN` + a test repo. Procedure:

1. Read baseline `X-RateLimit-Remaining` from a single `GET /repos/:o/:r/issues` warmup call.
2. Drive 30 polls at 10s cadence (5 minutes) through the ETag-wrapped transport against the idle test repo.
3. Read end-of-window `X-RateLimit-Remaining`.
4. Assert delta ≤ 6 (i.e. ≥80% of polls returned 304 and did not decrement the budget — sibling spec §2 G1).

This is the load-bearing measurement that proves Rung 1 hit its target. The gauge value MUST be observed via the production `internal/obs/` surface — NOT a test-only counter — per `feedback_validate_before_ship`.

### A4 — Concurrency safety

Test `TestETagTransport_ConcurrentRoundTrips_NoRace` under `-race`. 100 goroutines × 100 round-trips each against the in-memory stub. No data race; cache entries remain consistent under contention.

### A5 — CI gates clean

- `make pre-push-check` passes from the worktree root.
- `bash scripts/doc-check.sh` passes (this brief inclusive).
- `bash scripts/check-tdd.sh` is satisfied: the failing-test commit lands before impl per `feedback_tdd_discipline`.
- `bash scripts/check-reviewer-verdict.sh` — the implementer PR touches `internal/obs/events.go` (load-bearing surface), so independent reviewer dispatch is mandatory. NO self-tagged APPROVE per `feedback_no_self_tagged_approve`.

---

## Out of scope

- **OS1.** Persisted ETag store (sqlite-backed). Rejected by the sibling spec §2 NG4 under `feedback_default_simpler` — in-memory cache re-warms in ≤1 poll tick.
- **OS2.** Rung 2 adaptive-backoff state machine (`backoffState`, `EventGhclientPollBackoff`, `EventGhclientPollSnapBack`). Lands in a sibling implementer brief — keep this PR file-disjoint.
- **OS3.** Rung 3 GH-events-API single stream. Phase-X forward-fit per sibling spec §3 / §5.3.
- **OS4.** Webhook ingress of any shape (rungs 4 + 5). Tracked by `2026-06-09-smee-webhook-hybrid-impl.md` (Rung 4, Phase-X) and sibling spec §5.5 (Rung 5).
- **OS5.** Rewriting the gh-CLI fallback path. The alarm-path and `github_issues` adapter continue to use `GHCLIClient` unchanged; the new `http.RoundTripper` plugs in only at the (to-be-introduced) `http.Client`-backed `ghclient.Client` implementation.
- **OS6.** Changing call-site request shapes (URLs, query params, `Accept` headers). Pure transport-layer wrap.

---

## Adversarial

Independent reviewer pending — will be dispatched on the implementer PR per `feedback_adversarial_review_every_step` + `CLAUDE.md` reviewer-verdict gate. The implementer PR touches `internal/obs/events.go` (load-bearing event vocabulary) and `internal/ghclient/` (load-bearing under the verdict gate path classifier), so the gate refuses to auto-skip and a `Reviewer-agent-id:` + `Reviewer-recommendation: APPROVE` token pair is mandatory in the PR body footer.

Pre-dispatch adversarial hypotheses for the reviewer to hunt:

1. **Cache poisoning across `Accept` headers.** If the cache key omits `Accept`, a `application/vnd.github.raw` response could be returned to a `application/vnd.github+json` consumer. Mitigation: `Accept` is part of `cacheKey`. Reviewer: confirm via test that two different `Accept` values on the same URL store separate entries.
2. **Body read draining vs caller `io.Reader` contract.** The transport reads the body to buffer it; a careless implementation returns the drained body to the caller. Mitigation: replace `resp.Body` with `io.NopCloser(bytes.NewReader(buf))`. Reviewer: assert callers can read the response body byte-equally to what the stub sent.
3. **304 with absent prior cache entry.** Should never happen (GitHub only sends 304 after we sent `If-None-Match`) but defensive: if the cache entry was evicted between request-issue and response-receive, treat 304 as a transport error — emit no synthetic 200, return the 304 to the caller. Reviewer: stub a 304 with no prior cache entry; assert the call site sees the 304 and does not blow up on a nil cached body.
4. **Map growth unbounded under pagination.** Reviewer: stub a caller that issues `GET ?page=1..2000`; assert the cache stays ≤ 1024 entries via FIFO eviction.
5. **Race between `RLock` lookup and `Lock` write.** Reviewer: `go test -race` on `TestETagTransport_ConcurrentRoundTrips_NoRace`; confirm no race detected.
6. **Phantom 304 on a stale ETag that GitHub rotated.** GitHub may rotate ETags on backend cache invalidation; a stale `If-None-Match` is harmless (we get a fresh 200 + new ETag) but the test `TestETagTransport_StaleETagFreshGET` should pin this: stub returns 200 + new ETag in response to an `If-None-Match` the stub doesn't recognize.

---

## Implementer brief

Five-step concrete checklist:

1. **Land failing tests FIRST** (`feedback_tdd_discipline`). Write `internal/ghclient/etag_transport_test.go` covering A1, A2, A4, plus a stub `ObsSink`. Commit with `[FEAT] BUG-1164: failing tests for ETag conditional GET transport`. Capture RED output (`go test ./internal/ghclient/... 2>&1 | head -40`) in the PR body under a `## RED output` H2.
2. **Implement `internal/ghclient/etag_transport.go`** to the §Design above. Wire the in-memory cache, the `cacheKey` shape, the 304 short-circuit, the body-buffer + `NopCloser` swap, and the FIFO eviction at 1024 entries. Make tests A1, A2, A4 GREEN. Commit `[FEAT] BUG-1164: ETag conditional GET transport with 304 short-circuit`.
3. **Extend `internal/obs/events.go`** with `EventGhclientPollHit`, `EventGhclientPollMiss`, `EventGhclientPollNotModified` const + registry entry + event-kind table row per the obs/ vocabulary discipline. Add the `X-RateLimit-Remaining` gauge primitive (read by the transport on every 200/304 with the header present). Commit `[FEAT] BUG-1164: obs events + rate-limit gauge for ghclient poll transport`.
4. **Add the integration soak test (A3)** under build-tag `integration` with the 5-min idle-window measurement. Document the `GH_TOKEN` + test-repo env requirement in a header comment on the test file. Make A3 GREEN locally against a real test repo; capture the gauge delta in the PR body under `## Soak output`. Commit `[FEAT] BUG-1164: soak test pins ≥80% rate-budget reduction on idle window`.
5. **PR opening + reviewer dispatch.** Open PR with `gh pr ready` (NOT `gh pr merge --auto` per `feedback_no_implementer_automerge`). Body MUST cite `closes #1164` (one keyword per line per `feedback_github_auto_close_syntax`), include the RED + Soak fences, AND leave `Reviewer-agent-id:` + `Reviewer-recommendation:` BLANK for the operator-dispatched independent reviewer to fill (`feedback_no_self_tagged_approve`). Run `make pre-push-check` before pushing. On reviewer feedback: fix inline OR file tracking issue + cite # before any APPROVE token lands.

Compress `make ci-check` output per `feedback_subagent_cicheck_compress`:
`make ci-check 2>&1 | tee /tmp/cicheck.log | grep -E "^(FAIL|ok|---|Error|error:|PASS)" | tail -40` + exit code.

---

## Reopen trigger

Reopen this brief (file a new follow-up brief; do NOT mutate this one once shipped) when **any** of:

- **Poll-frequency increase post-merge.** Any consumer reduces its `PollInterval` below the values current at merge time (`adaptersync` 10s, `prwatch` 5s). Higher cadence may push the 304-ratio below the ≥80% target measured in A3 and require Rung 2 acceleration or Rung 3 GH-events-API consideration per sibling spec §5.3.
- **Rate-limit-exhaustion signal in `obs`.** The `X-RateLimit-Remaining` gauge drops below 500 (10% of the 5000/hr budget) sustained for ≥3 consecutive 5-min windows. Indicates the 304-ratio degraded, the consumer set grew, OR cross-repo pressure (sibling spec §5.3) became binding. File a follow-up brief that either (a) tightens the ETag cache shape, (b) accelerates Rung 2 backoff, or (c) reopens Rung 3.
- **Cache-eviction churn.** If the 1024-entry FIFO eviction fires on a steady-state idle workload (visible via a new `ghclient.poll.cache_evict` counter — Rung 2 brief owns adding it), the bound is undersized for actual call shapes. File a follow-up brief to right-size the bound OR introduce LRU.
