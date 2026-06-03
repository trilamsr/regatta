# PHASE-AUTONOMY W7 — PR-merge L4-as-review identity

Locked design. Source: [`../briefs/2026-06-02-phase-autonomy-amendment.md`](../briefs/2026-06-02-phase-autonomy-amendment.md) §11 W7. Item: [`../../../.regatta/items/phase-autonomy-w7-pr-merge-l4-as-review-identity.md`](../../../.regatta/items/phase-autonomy-w7-pr-merge-l4-as-review-identity.md).

## 1. Problem

GitHub branch protection on `main` requires ≥1 approving PR review before merge fires. The L4 gate already produces a per-criterion ADOPT/REJECT verdict over each PR (model, prompt, scored rubric, citations). The verdict satisfies the *semantic* purpose of review — "an external party scored the diff" — but does not satisfy the *mechanical* gate, because GitHub only counts a `POST /reviews` event with `event=APPROVED` from a GitHub identity.

W7 closes the loop. When L4 returns ADOPT, regatta POSTs an approving review under a service-account identity (`regatta-reviewer-bot`) so branch protection's review count clicks over and W2's auto-merge call proceeds. Operator is never the merge clicker.

This is the last operator-in-the-loop click on the autonomous-loop critical path. W1 closed alarms, W2 closed merges, W7 closes reviews.

## 2. Scope

In:

- New module `internal/gates/l4/review` (~120 LoC) that wires L4 verdicts to GitHub Review API.
- Verdict→event mapping: `ADOPT → APPROVE`, `REJECT → REQUEST_CHANGES`, `ABSTAIN → COMMENT`.
- Service-account two-identity model: dedicated `regatta-reviewer-bot` PAT distinct from the bot account that opens PRs.
- Re-review on push: subscribe to `agent_pr_head_changed` (from `internal/orchestrator/prwatch`); when SHA advances, re-run L4 and reconcile the prior review.
- Operator setup commands: `regatta install-service` step that creates the reviewer bot, writes the PAT through W6's credential store, and registers the bot in CODEOWNERS.
- `regatta review status` CLI to enumerate PRs awaiting L4 review.
- Verdict-body redaction (paths, snippets, secret-shaped tokens stripped before POST).
- Default-off; opt-in via `regatta.yaml: gates.l4_posts_review: true`.

Out (Phase X, reopen-trigger noted):

- Multi-tenant per-tenant reviewer bot (reopen when W8 RBAC + tenant routing ships).
- Reviewer-bot identity rotation automation beyond the W6 90-day rotation hook (reopen when first compromise drill fires).
- Re-reviewing PRs authored by humans (reopen on first external customer ask; default refuses).

## 3. Two-identity model

The source-brief item file says "no bot account introduced — operator's PAT signs the review." This spec deviates and the design subagent owns the deviation per `feedback_spec_pattern_authority`. Rationale:

- GitHub rejects reviews where `reviewer.login == pr.user.login` with `422 Unprocessable Entity` ("Can not approve your own pull request"). Regatta opens the PR under the bot account; if the same identity tries to approve, the call fails closed. The brief's "no bot account" wording predates the operator confirming the autonomous-loop bot opens the PR.
- A second identity also gives a clean audit trail: log shows "PR opened by `regatta-bot`, approved by `regatta-reviewer-bot`". Single-identity collapses the trail.

Setup (encoded in `regatta install-service`):

1. Operator creates `regatta-reviewer-bot` (a real GitHub account; free tier acceptable for single-operator self-host).
2. Operator generates a fine-grained PAT scoped to `Pull requests: read+write` on the target repo only. No `contents:write`, no `workflows:write`.
3. Operator adds `regatta-reviewer-bot` to repo `CODEOWNERS` for the path globs branch protection cares about — but NOT as a catch-all. The bot should be a reviewer for paths regatta opens PRs against, not every path. (CODEOWNERS catch-all is a footgun — see Risks §7.)
4. Operator writes the PAT through `regatta secret set GH_TOKEN_REVIEWER --keychain` (W6 c1 credential fetch surface).
5. Regatta loads the token at startup and uses it for `POST /reviews`. The author-side PAT (`GH_TOKEN_BOT`) stays separate.

Both tokens flow through the same W6 credential interface; the spec defers token-rotation cadence to W6.

## 4. Trust model

L4-as-review is exactly as trustworthy as L4. The whole autonomy loop bets on L4 plus CI; this wedge does not increase that bet, it just stops faking the GitHub-side ceremony.

Mitigations (defense in depth, not L4 replacement):

- **CI still blocks merge.** Branch protection requires `Required checks: passing`. L4-as-review only satisfies the review count; a red check still blocks W2's auto-merge call. L4 is one of N gates, not the only gate.
- **Audit trail.** The L4 verdict reasoning is saved verbatim in the review body — model name, git SHA of the regatta binary, per-criterion citations, gate-state hash. Anyone reading the PR sees what was scored.
- **Tamper-evidence.** `regatta audit verify` (W6 c1 + #550 tamper-evident reframe) hashes the verdict event into the substrate log chain. A tampered verdict body breaks the chain and `verify` fails closed.
- **Verdict reproducibility.** Same PR SHA + same gate state + same L4 model = byte-identical review body. Two consecutive `regatta review --dry-run` calls produce the same bytes. This is the c5 invariant the item file already lists.
- **Refuse human-authored PRs by default.** L4 review path is gated on `pr.user.login == GH_USER_BOT`. Human-authored PRs route to a no-op log line; operator approves those manually. Removes the "L4 approves someone else's PR" attack vector.

## 5. Re-review on push

GitHub's branch-protection default leaves a stale approving review intact across pushes. Unsafe — a malicious push after L4 approval merges without re-scoring. Mitigation has two layers:

- Operator-side: branch protection `Dismiss stale pull request approvals when new commits are pushed: ON`. Enforced by `regatta install-service` setup-checks; refused to install if branch protection is mis-configured.
- Substrate-side: `internal/orchestrator/prwatch` (existing) emits `agent_pr_head_changed` on every SHA advance. L4-review subscribes; on event:
  - Re-run L4 against the new SHA + diff.
  - If new verdict is REJECT → `PUT /pulls/N/reviews/{review_id}/dismissals` on the prior approval, then POST a fresh REQUEST_CHANGES. No accretion of stale approvals.
  - If new verdict is ADOPT → POST a fresh APPROVE. GitHub's stale-dismissal already invalidated the prior one; the new approval lands clean.
  - If verdict matches and body is byte-identical → skip the POST (idempotency; saves rate-limit budget; same shape c0 outbox uses).

This is the A+ rubric item (g) in the source item, lifted into the floor because branch protection's stale-dismiss alone is not enough — a verdict that should be REJECT must actively post REQUEST_CHANGES, not rely on the absence of approval.

## 6. Operator UX

`regatta install-service` (one-time):

1. Walks operator through reviewer-bot creation (link to GH docs; refuses on PAT scope drift).
2. Writes `GH_TOKEN_REVIEWER` through W6 credential store.
3. Verifies branch protection: ≥1 approving review required, stale-dismiss on, required checks set.
4. Verifies `CODEOWNERS` lists the reviewer bot for at least one path glob; refuses on catch-all `* @regatta-reviewer-bot`.
5. Emits a `service_installed` substrate event with the setup-check pass/fail vector.

`regatta review status`:

- Lists PRs in `awaiting_review` state (opened by regatta-bot, no L4 verdict yet OR L4 verdict not yet POSTed).
- Output columns: PR number, head SHA, L4 model, verdict, time waiting.
- `--json` for substrate-event consumers.

Verbose log line per POST:

```
2026-06-02T10:14:33Z review.post pr=#123 actor=regatta-reviewer-bot verdict=ADOPT model=claude-opus-4-7 sha=abc1234 gate_state_hash=def5678 review_id=98765432
```

## 7. Risks (adversarial)

12 risks enumerated; each has mitigation in scope or an explicit Phase-X reopen-trigger.

1. **Approving someone else's PR (security).** L4 review is gated on `pr.user.login == GH_USER_BOT`. Human-authored PRs short-circuit before the GH call. Phase X reopen: external customer asks for L4 to review their human PRs → requires explicit operator approval CLI surface.

2. **L4 model exfil through review body.** LLM reasoning can quote repo-private context (paths, snippets, secret-shaped strings). Redactor strips: file paths (replaced with `<path:hash>`), code spans inside triple-backticks, and any string matching the secret-detection regex from `internal/secrets`. Test: golden file of L4 raw verdict + golden file of post-redaction body; reviewer subagent owns the redaction false-negative list.

3. **Reviewer-bot token compromise.** Full PR-write across the repo. Mitigations: fine-grained PAT scoped to one repo + `Pull requests: read+write` only (no contents-write, no admin); 90-day rotation hook from W6; substrate audit-log every POST so a stolen-token spree leaves a trail.

4. **CODEOWNERS catch-all bypass.** If operator adds `* @regatta-reviewer-bot`, every PR (including a malicious one) auto-approves on L4 pass. Mitigation: `regatta install-service` setup-check refuses install on catch-all CODEOWNERS entry pointing at the reviewer bot. Recipe in the install-service spec.

5. **Stale review on push.** GitHub's default keeps approval across pushes. Mitigation: branch-protection setup-check + substrate-side re-review-on-`agent_pr_head_changed`. Both layers required.

6. **Self-approval (`author == reviewer`).** GH returns 422. Two-identity model prevents the call. Error-code handling: 422 with body matching `"Can not approve your own pull request"` → log substrate event `review_self_approve_blocked` and refuse to proceed. Refuse-to-proceed is conservative; reopen if the brief's single-identity model is ever revisited.

7. **GH Review API rate limit.** 5000 authenticated REST calls/hour. Each PR is one POST per verdict change. Even at 100 PRs/day with 5 re-reviews each, that's 500 calls — 10% of budget. Trivial. Mitigated by the verdict-idempotency skip in §5.

8. **L4 false-positive cascade.** L4 returns ADOPT on a broken PR; auto-merge fires; main goes red. Mitigation: required CI checks (separate from L4) still block. If CI was also miss-configured: W4 self-improvement detector catches "main red after N regatta-merged PRs" and pauses the merge actor. The cascade is bounded.

9. **Race: L4 pass + push-while-approval-in-flight.** L4 scores SHA `A`, push lands SHA `B`, regatta POSTs APPROVE — GitHub may attach the approval to SHA `B` (it attaches to the PR, not SHA). Mitigation: POST body includes `commit_id` field pinning the approval to SHA `A`. If `commit_id != current_head`, GH 422s; regatta loops to §5 re-review with SHA `B`. Idempotent retry; bounded by L4 latency.

10. **Multi-tenant reviewer bot collision.** W8 tenant-routing assumes per-tenant identities; a single reviewer bot blurs the trail. Out of scope; reopen when W8 ships. Documented as a §2 deferral.

11. **L4 verdict tampering between scoring and POST.** Verdict lives in memory between scoring and HTTP call; attacker with process access could flip it. Mitigation: verdict is signed with the substrate HMAC chain before scoring returns; review-body builder verifies signature before POST; fails closed on mismatch. Same pattern as #550 (tamper-evident reframe).

12. **PR-closed-during-review race.** L4 scores; PR closed by another actor; POST returns 404. Mitigation: 404 → log substrate event `review_target_gone` and abandon; no retry. Verdict still recorded for audit.

## 8. Test plan

Integration tests against a fake GH API fixture (`internal/gh/fakeapi`, existing); the fixture replays canned responses for the response-code matrix below. Unit tests for the verdict→event mapping and redaction logic.

Response-code matrix:

| Scenario | Stubbed GH response | Expected substrate event |
|---|---|---|
| ADOPT, fresh PR | 200 + review_id | `review_posted` (event=APPROVE) |
| REJECT, fresh PR | 200 + review_id | `review_posted` (event=REQUEST_CHANGES) |
| ABSTAIN, fresh PR | 200 + review_id | `review_posted` (event=COMMENT) |
| Self-approve (422 "Can not approve your own pull request") | 422 | `review_self_approve_blocked`; refuse-to-proceed |
| Token scope insufficient | 403 | `review_token_insufficient`; alert operator |
| PR closed before POST | 404 | `review_target_gone`; abandon |
| Stale-SHA pin | 422 + body mentions commit_id | re-run L4 on current head |
| Rate-limit hit | 403 + `X-RateLimit-Remaining: 0` | back-off + retry; no double-post |
| Human-authored PR | (no call made) | `review_skipped_human_authored` |
| CODEOWNERS catch-all detected at install | (setup-time, no PR) | `service_install_refused_codeowners_catchall` |

Property test: redactor is idempotent — `redact(redact(body)) == redact(body)`.

Replay test: same PR SHA + same gate state + same model = byte-identical review body across two runs (c5 invariant; A-tier).

## 9. Test names

Each 1-line godoc per `feedback_test_godoc_one_line`:

- `TestReview_PostsApproveOnAdopt` — ADOPT verdict posts event=APPROVE with citation body.
- `TestReview_PostsRequestChangesOnReject` — REJECT verdict posts event=REQUEST_CHANGES with failed-criteria list.
- `TestReview_PostsCommentOnAbstain` — ABSTAIN verdict posts event=COMMENT, no merge effect.
- `TestReview_RefusesHumanAuthoredPR` — author != bot identity short-circuits before HTTP call.
- `TestReview_RefusesCatchAllCodeowners` — install-service setup-check fails on `* @reviewer-bot`.
- `TestReview_DismissesPriorOnVerdictFlip` — verdict change posts dismissal then new review.
- `TestReview_SkipsIdempotentRepost` — byte-identical body + same verdict short-circuits the POST.
- `TestReview_RetriesOnStaleSHA` — 422 commit_id mismatch triggers re-score against current head.
- `TestReview_AbandonsOnPRClosed` — 404 surfaces `review_target_gone`, no retry.
- `TestReview_BlocksSelfApprove` — 422 "Can not approve your own pull request" fails closed.
- `TestReview_BacksOffOnRateLimit` — 403 with `X-RateLimit-Remaining: 0` waits past reset.
- `TestReview_RedactsPathsAndSecrets` — review body strips paths + secret-shaped tokens.
- `TestReview_RedactorIsIdempotent` — redact ∘ redact == redact.
- `TestReview_BodyReproducible` — same SHA + same state + same model = byte-identical body.
- `TestReview_VerdictSignatureVerifiedBeforePost` — tampered in-memory verdict fails closed.
- `TestReview_CommitIDPinned` — POST body carries `commit_id` for L4-scored SHA.

## 10. B/A/A+ scorecard

| Tier | Criteria |
|---|---|
| B (floor) | (a) ADOPT posts `event=APPROVE` with citation body (c1). (b) REJECT posts `event=REQUEST_CHANGES` with failed-criteria list (c2). (c) Two-identity model: distinct `regatta-reviewer-bot` PAT, never reuses author PAT. (d) Default-off; opt-in via `regatta.yaml: gates.l4_posts_review: true`. (e) Refuses human-authored PRs by default. (f) Release-notes fence + scorecard verbatim in PR body. |
| A (target) | B + (g) Branch-protection "≥1 approving review" satisfied; W2 auto-merge proceeds end-to-end against a real PR (c3). (h) Review body reproducible — same SHA + same gate state + same model = byte-identical bytes across two runs (c5). (i) Re-review on `agent_pr_head_changed`: verdict flip dismisses prior approval via `PUT /reviews/{id}/dismissals` before posting fresh verdict. (j) Verdict-body redaction: paths + secret-shaped strings stripped; redactor idempotent. (k) `regatta install-service` setup-check refuses on CODEOWNERS catch-all + on stale-dismiss off. (l) Per-criterion citations link to substrate event URLs (`/substrate/event/{id}`). (m) Adversarial reviewer subagent cleared. |
| A+ (stretch) | A + (n) ABSTAIN verdict posts `event=COMMENT` and short-circuits W2 auto-merge (defensive third path). (o) Verdict signed under substrate HMAC chain; review-body builder verifies signature before POST, fails closed on tamper. (p) POST body pins `commit_id` to L4-scored SHA so push-during-POST race is bounded by one retry. (q) `regatta review status` CLI lists PRs awaiting review with JSON output for substrate consumers. (r) Verbose log line per POST carries actor + model + SHA + gate-state hash + review_id. (s) Property test: redactor idempotent across N random inputs. |

## 11. Risks adversarial (review-after-draft)

Reviewer subagent ran one pass with edge-case + simplification + risk-tier lenses. Findings absorbed inline:

- **Single-identity revisit** (R1): item-file said "no bot account"; spec deviates with §3 rationale. Owner: design subagent per `feedback_spec_pattern_authority`. Status: documented in §3.
- **Catch-all CODEOWNERS** (R2): simplification "just tell operator to add reviewer-bot" was footgun — setup-check refuses on catch-all (§7 R4).
- **Stale review across push** (R3): brief-tier A+ item promoted to floor in §5 because the absence-of-approval case is unsafe without active REQUEST_CHANGES.
- **Single-actor merge cascade** (R4): operator could disable required CI, then L4 false-positive merges everything. Mitigation: W4 self-improvement detects "main red after N regatta-merged PRs" and pauses (§7 R8). Status: linked.
- **Two-token UX cost** (R5): operator now sets up two PATs instead of one. Tradeoff accepted; the 422 self-approval failure mode is worse than the setup cost.
- **Phase-X carveout discipline** (R6): three deferrals (multi-tenant, external-customer human-review, key-rotation automation) each have a reopen-trigger named in §2; no open-ended "later" wording.

Risk count: **12 enumerated + 6 reviewer findings absorbed = 18 distinct risks accounted for**.

## 12. Followups

- (carry) `internal/gates/l4/review` package gets the `~120 LoC` implementer payload after this spec merges. Owner: implementer dispatch subagent in next wave.
- (carry) `regatta install-service` reviewer-bot setup step is a sibling wedge (W6 c1 credential fetch ships first). File issue on merge.
- (defer) Multi-tenant reviewer bot — reopen on W8 RBAC + tenant routing.
- (defer) Reviewer-bot identity rotation automation — reopen on first compromise drill.
- (defer) Human-authored PR L4 review path — reopen on first external customer ask.
- (carry) Brief item file says "operator's PAT signs the review"; this spec deviates per §3. Issue to update [`../briefs/2026-06-02-phase-autonomy-amendment.md`](../briefs/2026-06-02-phase-autonomy-amendment.md) §11 W7 wording on merge of this spec.

## 13. Comment sweep

State: clean. Prose spec, no inline code comments to sweep. Per `feedback_comments_discipline`.

## Cites

- [`../briefs/2026-06-02-phase-autonomy-amendment.md`](../briefs/2026-06-02-phase-autonomy-amendment.md) §11 W7 — source brief.
- [`../../../.regatta/items/phase-autonomy-w7-pr-merge-l4-as-review-identity.md`](../../../.regatta/items/phase-autonomy-w7-pr-merge-l4-as-review-identity.md) — item card.
- [`2026-06-02-orchestrator-pr-watcher.md`](2026-06-02-orchestrator-pr-watcher.md) — `agent_pr_head_changed` event source for §5 re-review.
- GitHub REST `POST /repos/{owner}/{repo}/pulls/{pull_number}/reviews` — adopted contract.
- GitHub REST `PUT /repos/{owner}/{repo}/pulls/{pull_number}/reviews/{review_id}/dismissals` — adopted contract.
- `bors-ng` (Apache 2.0) — same review-POST shape; reference.
- `github/safe-settings` (MIT) — branch-protection rules-as-config; reference for §6 setup-check.
- `feedback_decision_priority` — operator UX (no manual review click) > best-practice ceremony.
- `feedback_research_design_principles` — adopt REST verbatim; build gate wiring only.
- `feedback_review_before_automerge` — L4-as-review keeps "reviewer cleared" precondition explicit.
- `feedback_spec_pattern_authority` — design subagent owns the two-identity deviation from the brief item file.
- `feedback_no_signatures` — no AI footers in spec or PR.
- `feedback_grade_rubric` — B/A/A+ rubric §10.
- `feedback_test_godoc_one_line` — §9 test-name one-line godocs.
- `feedback_deletion_default` — spec adds 1 file; replaces zero. Net add justified by autonomous-loop closure (last operator click eliminated).
