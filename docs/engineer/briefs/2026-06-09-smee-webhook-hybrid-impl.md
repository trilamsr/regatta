---
name: "smee.io / gh-webhook-forward hybrid — implementer brief for polling-improvement-ladder Rung 4"
slug: 2026-06-09-smee-webhook-hybrid-impl
status: draft
phase: x-forward-fit
owner: trilamsr@gmail.com
created: 2026-06-09
summary: "Phase-X forward-fit implementer brief for Rung 4 of the polling-improvement-ladder spec (`docs/engineer/specs/phase-x/2026-06-09-polling-improvement-ladder.md`). Outbound-only webhook relay (`smee.io` / `gh webhook forward` / Cloudflare Tunnel) forwards GitHub webhooks to a `127.0.0.1`-bound listener under `internal/ghclient/webhook/`, validates HMAC against `WEBHOOK_SECRET`, deduplicates on `event_id` within a 30s window, and enqueues a targeted poll. Webhook payload is informational ONLY — never authoritative. Closes #1165 once reopen trigger fires."
---

# smee.io / `gh webhook forward` webhook hybrid — Implementer Brief (Rung 4)

Tracks: #1165 (Phase-X forward-fit; closes once §Reopen trigger fires).
Sibling spec: `docs/engineer/specs/phase-x/2026-06-09-polling-improvement-ladder.md` (Rung 4, §3 + §5.4).
Sibling brief: `2026-06-09-etag-conditional-get-impl.md` (Rung 1 — the targeted GETs this brief enqueues benefit from the ETag cache).
Cross-ref: `internal/ghclient/client.go` (the existing seam; this brief introduces a sibling `internal/ghclient/webhook/` package — does not modify the gh-CLI client).

Memory rules in force: `feedback_default_simpler`, `feedback_tdd_discipline`, `feedback_validate_before_ship`, `feedback_adversarial_review_every_step`, `feedback_no_signatures`, `feedback_root_cause`, `feedback_no_self_tagged_approve`, `feedback_no_implementer_automerge`.

**Phase context.** This brief is Phase-X (forward-fit) per the sibling spec §3 Rung 4 — DO NOT dispatch an implementer today. The brief exists so that when the §Reopen trigger fires, the design surface is legible and an implementer wave can land without a re-design cycle. Per `CLAUDE.md` §"Self-host filter (Phase context)": Phase-X tokens (`tenant_id`, `RBAC`, `Stripe`, `Sigstore`, `Rekor`, `blackboard`, `Temporal`, `htmx`) are an operator-glance hint surfaced by `make pre-push-check` post-MAY-31 — this brief uses none of them in unwrapped form.

---

## Problem

The polling-improvement-ladder spec §1 identifies two orthogonal cost axes — rate-budget consumption (attacked by Rung 1 ETag) and end-to-end latency floor (~5s mean, 10s p99 at default 10s poll cadence). Rung 1 + Rung 2 close the budget axis on the self-host phase; the latency axis remains at "perceived sluggishness on PR-review handoffs" — real to the operator but not dispatch-blocking, hence Phase-X.

When the §Reopen trigger fires (external-customer ask, review-handoff cadence becomes throughput bottleneck, OR Rung 2 over-tunes — sibling spec §5.4), the latency floor becomes load-bearing. Rung 4 attacks it by routing GitHub webhooks through an outbound-only relay (`smee.io` channel, `gh webhook forward` subcommand, OR Cloudflare Tunnel) to a `127.0.0.1`-bound listener inside regatta. On valid webhook receipt, regatta enqueues a targeted GET for the affected resource — which itself goes through Rung 1's ETag cache, so an in-flight poll hit on the same resource within the cache window short-circuits to 304. The webhook is the "did something happen" signal; the poll is the "what changed" authoritative read. Lost webhooks are non-fatal: the Rung 1 + Rung 2 poll loop catches up on the next tick.

Adversarial framing: a naive implementation treats the webhook payload as authoritative, encoding state directly from the push notification. That couples regatta's state model to GitHub's webhook schema (versioned independently of the REST API), introduces replay-store correctness obligations, and fails open when the relay is compromised. The hybrid shape — push notifies, regatta still pulls detail — is the load-bearing design choice per `feedback_root_cause`. The poll-loop fallback is the correctness floor; the webhook is a latency optimization on top.

---

## Design

### `internal/ghclient/webhook/` package layout

New sibling package to `internal/ghclient/`:

```
internal/ghclient/webhook/
  listener.go        # 127.0.0.1-bound http.Server + handler
  hmac.go            # X-Hub-Signature-256 validation against WEBHOOK_SECRET
  dedup.go           # 30s sliding window keyed by event_id
  enqueue.go         # targeted-poll enqueue seam
  listener_test.go
  hmac_test.go
  dedup_test.go
  enqueue_test.go
```

The package exposes one constructor:

```go
// New returns a webhook listener bound to 127.0.0.1:<port> that validates
// HMAC against secret, deduplicates on event_id within window, and
// enqueues a targeted poll via enqueue. Returns nil + error when the
// listener cannot bind or the secret is empty.
func New(port int, secret []byte, window time.Duration, enqueue EnqueueFunc, obs ObsSink) (*Listener, error)

// EnqueueFunc is the seam to the existing adaptersync / prwatch poll loops —
// the listener calls this with the (resource_kind, identifier) tuple parsed
// from a valid webhook, NOT with the webhook payload itself.
type EnqueueFunc func(ctx context.Context, kind ResourceKind, id string)
```

### `127.0.0.1`-bound listener

`http.Server{Addr: "127.0.0.1:" + strconv.Itoa(port)}` — explicit loopback bind, not `:port` (which listens on all interfaces). The bind address is non-configurable; an operator who wants public ingress is on Rung 5 (sibling spec §5.5), not Rung 4. Reviewer must confirm the bind cannot be widened by env var or flag.

Listener responsibilities (in order, per request):

1. `POST` only — `405` on anything else.
2. Path `/webhook` only — `404` on anything else.
3. Read body into a `bytes.Buffer` capped at 1 MiB (GitHub webhook payloads do not exceed this for events we care about; defensive cap against malicious payloads).
4. Validate `X-Hub-Signature-256` via the HMAC primitive below. `401` on mismatch. Emit `webhook.hmac.fail` on every failure (load-bearing observability — an unexpected fail rate signals a compromised relay or a rotated secret).
5. Validate `X-GitHub-Event` is in the expected set (`pull_request`, `pull_request_review`, `pull_request_review_comment`, `issue_comment`, `issues`, `check_run`, `check_suite`). `204` on out-of-scope events (acknowledge but no-op).
6. Extract `event_id` from `X-GitHub-Delivery` header.
7. Check dedup window — if `event_id` seen within the 30s window, `204` + emit `webhook.dedup.drop`. Else record + continue.
8. Parse payload minimally to extract `(resource_kind, id)` — `pull_request.number` for PR events, `issue.number` for issue events. Use a typed minimal struct per event kind, NOT a generic `map[string]any` — explicit schema = explicit failure mode if GitHub rotates a field.
9. Call `enqueue(ctx, kind, id)` on a background goroutine with `context.WithTimeout(2*time.Second)` so the inbound HTTP handler does not block on the enqueue.
10. Respond `202` immediately.

### HMAC validation against `WEBHOOK_SECRET`

`X-Hub-Signature-256` header carries `sha256=<hex>` per GitHub's spec. Implementation:

```go
// ValidateSignature returns true when sig matches HMAC-SHA256(secret, body).
// sig MUST carry the "sha256=" prefix; otherwise returns false. Comparison
// uses subtle.ConstantTimeCompare to defeat timing attacks.
func ValidateSignature(secret, body []byte, sig string) bool
```

Secret is read from `WEBHOOK_SECRET` env at process start; nil + error on empty. The secret is a separate primitive from `GITHUB_TOKEN` — GitHub issues a per-webhook secret on installation, not derived from the API token. Operator runbook (§Implementer brief step 5) covers the rotation procedure.

The HMAC check is mandatory even though the relay is outbound-only — defense-in-depth against a compromised `smee.io` channel or `gh webhook forward` process. The reviewer must confirm there is no path through the handler that skips HMAC validation (no `// SKIP_HMAC_FOR_TESTING` flag in the production binary).

### 30s dedup window on `event_id`

```go
type Dedup struct {
    window time.Duration
    mu     sync.Mutex
    seen   map[string]time.Time // event_id -> first-seen
    clock  func() time.Time      // injectable
}

// CheckAndRecord returns true when the eventID is fresh (not seen within
// the window). When fresh, records the timestamp atomically and returns
// true; when duplicate, leaves state unchanged and returns false. Stale
// entries (clock - first_seen > window) are evicted opportunistically on
// each call to bound the map.
func (d *Dedup) CheckAndRecord(eventID string) bool
```

30s window covers the worst-case relay retry behavior (smee.io reconnect window is ~15s; `gh webhook forward` retries on EOF within ~10s). Eviction is lazy on `CheckAndRecord` — a separate sweep goroutine is rejected per `feedback_default_simpler`.

### Enqueue-targeted-poll seam (NOT authoritative payload)

The handler MUST NOT pass the webhook payload to `enqueue`. The contract is:

```go
enqueue(ctx, ResourceKindPR, "1234")  // GOOD — caller fetches PR #1234 itself
enqueue(ctx, ResourceKindPR, payload) // BAD — couples state to webhook schema
```

The downstream `adaptersync` / `prwatch` loops own the authoritative read. When the targeted GET fires, it goes through the Rung 1 ETag transport — if the webhook caught a real edge, the GET returns a fresh 200 with new ETag; if the webhook fired but state had already been observed via the poll loop, the GET returns 304 free of charge. The webhook is a "wake up and check now" signal — nothing more.

### Relay setup runbook (operator-facing, ships in `docs/engineer/runbooks/webhook-relay-setup.md` alongside this brief's implementer PR)

Three supported relay options (operator picks one):

1. **`smee.io` channel.** Create a channel at `https://smee.io/new`; capture the channel URL. Run `smee --url <channel> --target http://127.0.0.1:<port>/webhook` as a long-running process (systemd unit / launchd plist / `tmux` session — operator's choice). Register the channel URL as the `Payload URL` on the GitHub repo webhook config; set `Content type: application/json`, `Secret: <WEBHOOK_SECRET value>`.
2. **`gh webhook forward`.** Use the GitHub CLI's `webhook forward` extension (`gh extension install cli/gh-webhook`). Run `gh webhook forward --repo o/r --events pull_request,pull_request_review,issue_comment,issues,check_run --url http://127.0.0.1:<port>/webhook`. No external service; relays through GitHub's WebSocket relay.
3. **Cloudflare Tunnel (outbound-only).** `cloudflared tunnel create regatta-webhook`; route to `http://127.0.0.1:<port>`; configure GitHub webhook to the tunnel's public hostname. Outbound-only — the tunnel egress connection terminates inbound traffic on Cloudflare's edge.

All three keep the operator's machine zero-public-ingress at the host level — the listener binds `127.0.0.1` only. Phase-X Rung 5 (sibling spec §5.5) is the direct-public-URL shape and is out of scope.

### Event vocabulary (`internal/obs/events.go` additions)

```go
const (
    EventWebhookReceived        EventKind = "webhook.received"         // post-HMAC-pass, pre-dedup
    EventWebhookHMACFail        EventKind = "webhook.hmac.fail"        // load-bearing security signal
    EventWebhookDedupDrop       EventKind = "webhook.dedup.drop"       // within-window duplicate
    EventWebhookEnqueued        EventKind = "webhook.enqueued"         // targeted poll dispatched
    EventWebhookSchemaUnknown   EventKind = "webhook.schema.unknown"   // event kind out of expected set
)
```

Per `CLAUDE.md` reviewer-verdict gate, `internal/obs/events.go` is load-bearing → implementer PR requires independent reviewer dispatch.

---

## Acceptance

### A1 — HMAC validation rejects mismatch (failing test FIRST, per `feedback_tdd_discipline`)

Test `TestListener_RejectsBadHMAC` in `internal/ghclient/webhook/listener_test.go`. With listener bound on an ephemeral loopback port + secret `s`:

- `POST /webhook` with body `B` + header `X-Hub-Signature-256: sha256=<HMAC(s', B)>` for `s' != s` → response status `401`. Assert `webhook.hmac.fail` event emitted exactly once. Assert `enqueue` was NOT called.
- Same body with correct HMAC → `202`. Assert `webhook.received` then `webhook.enqueued` emitted in order.

Failing-test commit lands first; RED output in PR body.

### A2 — 30s dedup window drops within-window duplicate

Test `TestDedup_WithinWindowDrop` + listener-level `TestListener_DedupWithinWindow`. Inject a fake clock:

- First `POST /webhook` with `X-GitHub-Delivery: evt-1` → `202`, `enqueue` called once.
- Second `POST` with same `evt-1` at `+15s` (within window) → `204`, `webhook.dedup.drop` emitted, `enqueue` NOT called.
- Third `POST` with same `evt-1` at `+45s` (outside window) → `202`, `enqueue` called again.

### A3 — Enqueue receives `(kind, id)`, NEVER the payload struct

Test `TestListener_EnqueueArgsDoNotCarryPayload`. Capture every `EnqueueFunc` invocation in a fake; assert each call's args are `(ResourceKind, string)` — the string is the issue/PR number, NOT a serialized payload. Use `reflect` to assert the captured args carry no `map`, `[]byte`, or struct types beyond the typed primitives.

This is the architectural-invariant gate per the §Design "webhook is informational only" contract. A future regression that smuggles the payload into `enqueue` MUST fail this test.

### A4 — Bind address is `127.0.0.1`, not all-interfaces

Test `TestListener_BindsLoopbackOnly`. After `New(...)`, inspect the bound address via `listener.Addr().String()`; assert prefix `127.0.0.1:`. Confirm via `net.Dial("tcp", "<machine_ip>:<port>")` from the test that the listener is unreachable on a non-loopback address (skip on environments without a non-loopback IP).

### A5 — Body cap rejects oversized payloads

Test `TestListener_RejectsOversizedBody`. `POST` a 2 MiB body → status `413`, no `enqueue` call, no panic.

### A6 — CI gates clean

- `make pre-push-check` passes.
- `bash scripts/doc-check.sh` passes (this brief inclusive).
- `bash scripts/check-tdd.sh` satisfied (failing-test-first ordering).
- `make pre-push-check` Phase-X hint (post-MAY-31) — informational only; this brief lives in `docs/engineer/briefs/` outside the spec scan scope, so Phase-X token mentions here do not surface in the hint.
- `bash scripts/check-reviewer-verdict.sh` — implementer PR touches `internal/obs/events.go` AND introduces a network-listening primitive; both are load-bearing. Independent reviewer dispatch mandatory. NO self-tagged APPROVE per `feedback_no_self_tagged_approve`.

---

## Out of scope

- **OS1.** Public-ingress webhook receiver (direct GitHub → public URL). Rung 5 territory; sibling spec §5.5. The §Reopen trigger for Rung 5 includes multi-operator deployment (`tenant_id`-shaped, surfaced by the MAY-31 pre-push Phase-X hint) and dedicated SRE ownership — neither applies to the self-host operator.
- **OS2.** Persisted dedup store (sqlite-backed `event_id` log). In-memory + lazy eviction is sufficient at 30s window; a restart-survival dedup primitive is `feedback_default_simpler`-rejected. If the reopen trigger reveals dedup correctness gaps, file a follow-up brief.
- **OS3.** Per-tenant secret rotation primitives. `WEBHOOK_SECRET` is a single env var; multi-tenant rotation (`RBAC`-shaped) is Phase-X.
- **OS4.** Signed delivery receipts (`Sigstore`-anchored webhook attestation). Phase-X enterprise wedge per `CLAUDE.md` self-host filter — explicitly rejected today.
- **OS5.** Treating the webhook payload as authoritative state. The architectural invariant — webhook signals, poll authoritative — is non-negotiable; a future PR proposing to skip the targeted-poll fetch on a "trusted" webhook MUST be rejected at design review.
- **OS6.** Workflow-orchestration primitives (`Temporal`, durable-task replay). Out of scope; the dedup primitive here is window-scoped only, not durability-scoped.
- **OS7.** Removing the poll loop. The poll loop is the correctness floor; the webhook is a latency optimization. Rungs 1 + 2 remain shipping prerequisites — this brief assumes both have landed.

---

## Adversarial

Independent reviewer pending — will be dispatched on the implementer PR per `feedback_adversarial_review_every_step`. Pre-dispatch hypotheses for the reviewer to hunt:

1. **HMAC bypass via empty-secret runtime path.** Reviewer: confirm `New(...)` returns error on empty `WEBHOOK_SECRET`; confirm there is no init-time path that wires a no-op validator.
2. **Constant-time comparison absent.** Reviewer: assert `ValidateSignature` uses `subtle.ConstantTimeCompare`; `bytes.Equal` is a finding.
3. **Bind address widened by misconfiguration.** Reviewer: grep for any flag / env / config-struct field that could change `127.0.0.1` to `0.0.0.0` or a public bind; assert none exists. Test A4 pins behavior, but a PR adding such a flag in the future must be rejected at review.
4. **Dedup window race under concurrent identical events.** Reviewer: assert `Dedup.CheckAndRecord` holds `Mutex` across the check+record window — a `sync.Map` `Load` + `Store` pair has a race window that admits both deliveries.
5. **Payload smuggling into `enqueue`.** Reviewer: re-read `EnqueueFunc` call sites; confirm only `(ResourceKind, string)` args; A3 pins behavior.
6. **`X-GitHub-Delivery` collision across repos.** Reviewer: GitHub guarantees `X-GitHub-Delivery` uniqueness per delivery, not per repo — but a misconfigured shared relay across multiple operator instances could collide. Confirm the dedup key includes nothing that would cause cross-instance collision in single-operator self-host deployment (it does not, by design); document the multi-operator collision risk in the §Reopen trigger.
7. **Webhook event-kind drift.** GitHub adds new event kinds; an out-of-scope kind today may become load-bearing tomorrow. Reviewer: confirm `webhook.schema.unknown` emission so operator notices when GitHub starts sending a kind regatta should care about.
8. **Backpressure on `enqueue`.** Reviewer: the `2 * time.Second` enqueue timeout — if the downstream poll loop is saturated, what happens? Confirm `webhook.enqueued` is emitted on enqueue ATTEMPT, with a separate event (file as a follow-up if missing) for enqueue TIMEOUT. The webhook handler must not block GitHub's delivery retry budget.
9. **Replay attack via valid HMAC + stale event_id outside window.** Per design, an attacker replaying a 31-second-old captured webhook payload + signature would be re-enqueued (dedup window expired). Mitigation: targeted-poll re-confirms current state — if the resource is unchanged the GET returns 304 and the wake-up is harmless. Reviewer: confirm this argument holds; if the targeted-poll has state-mutating side effects, the replay is exploitable and the brief is wrong.
10. **Relay setup runbook drift.** Reviewer: confirm the runbook covers all three relay options + secret rotation procedure + how to verify HMAC validation is live (a curl that should be rejected).

---

## Implementer brief

Five-step concrete checklist (DO NOT execute until §Reopen trigger fires):

1. **Land failing tests FIRST** (`feedback_tdd_discipline`). Write `internal/ghclient/webhook/{listener,hmac,dedup,enqueue}_test.go` covering A1-A5. Inject fake clock + fake `EnqueueFunc` + fake `ObsSink`. Commit `[FEAT] BUG-1165: failing tests for outbound-only webhook hybrid listener`. Capture RED output in PR body.
2. **Implement `internal/ghclient/webhook/` package** to §Design. Wire the listener, HMAC validator (constant-time), dedup window, and enqueue seam. Make A1-A5 GREEN. Commit `[FEAT] BUG-1165: 127.0.0.1 webhook listener with HMAC + 30s dedup`.
3. **Extend `internal/obs/events.go`** with the five event constants from §Design + registry table rows. Commit `[FEAT] BUG-1165: obs events for webhook hybrid`.
4. **Ship the runbook** (`docs/engineer/runbooks/webhook-relay-setup.md`) covering the three relay options (`smee.io`, `gh webhook forward`, Cloudflare Tunnel) + the `WEBHOOK_SECRET` rotation procedure + the verification curl. Commit `[DOCS] BUG-1165: webhook relay setup runbook`.
5. **PR opening + reviewer dispatch.** Open PR with `gh pr ready` (NOT `--auto` per `feedback_no_implementer_automerge`). Body MUST cite `closes #1165` (one keyword per line). Leave `Reviewer-agent-id:` + `Reviewer-recommendation:` BLANK for operator-dispatched reviewer (`feedback_no_self_tagged_approve`). Verify `make pre-push-check` before push. On reviewer feedback: fix inline OR file tracker + cite # before any APPROVE token lands.

Compress `make ci-check` output per `feedback_subagent_cicheck_compress`:
`make ci-check 2>&1 | tee /tmp/cicheck.log | grep -E "^(FAIL|ok|---|Error|error:|PASS)" | tail -40` + exit code.

---

## Reopen trigger

Per sibling spec §5.4. This brief becomes active (move `phase:` frontmatter from `x-forward-fit` to `self-host-first`, file a fresh follow-up issue, dispatch the implementer wave) when **any** of:

- **External-customer ask.** A non-operator user runs regatta against their own repo AND reports the 10s lag floor as a dispatch-blocker — i.e. the latency is load-bearing for someone other than the sole internal operator. Document the user + the reported workflow in the follow-up issue.
- **Review-handoff cadence becomes throughput bottleneck.** Sustained gap of ≥30 minutes between `prwatch.pr_ready_for_review` event and `reviewer.dispatched` event attributable to poll latency (NOT operator availability — confirm by correlating with operator-active windows). Measured via the existing event vocabulary across ≥1 week of operation.
- **Rung 2 over-tunes.** Busy-window polls hit `X-RateLimit-Remaining` pressure (gauge sustained < 500/hr) while idle-window backoff is at ceiling (`mult = 8`). Indicates the polling shape is the wrong primitive — webhook hybrid becomes net-cheaper than further poll-loop tuning.
- **Poll-frequency reduction below 5s.** Any consumer reduces `PollInterval` below 5s to chase latency. At that cadence the rate-budget math favors the webhook hybrid even on the self-host repo; reopen this brief instead of accepting the budget cost.

Trigger satisfaction MUST be cited in the follow-up issue body. The brief, once active, gets an independent design review (NOT just an implementer dispatch) before any code lands — Phase-X surface changes deserve a re-design pass per `feedback_spec_pattern_authority`.
