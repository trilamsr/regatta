# Approval gates — operator runbook

Reader: on-call SRE who has never seen Regatta before. You have a
shell, `sqlite3`, the `gh` CLI, and basic YAML literacy.
Read time: 8 minutes.
Goal: diagnose a stuck human-in-the-loop approval and resolve it
(decide, escalate, or timeout) in ≤ 60 seconds — see §[MTTD runbook](#mttd-runbook-≤-60s).
Expires when: `regatta approval decide` / `regatta approval list`
flags or exit codes change. Confirm against `regatta approval decide -h`.

## What is an approval gate?

An approval gate pauses a work item until one or more human reviewers
allow or deny it. The orchestrator creates an `approvals` row,
notifies the reviewer set, mints a single-use signed token per
reviewer, and waits. The reviewer runs `regatta approval decide` with
their token; once the configured quorum is met the work item resumes.
If no one acts before `timeout`, the gate applies its `on_timeout`
policy (`fail`, `auto_approve`, or `escalate`). Every transition is
both a `slog` event and a row in `approval_events` — the SQL log is
the durable audit trail.

Prior art: this is the GitHub Actions Environments protection-rule
pattern (required reviewers, identity check, audit log) wired into
Regatta's universal queue. The token format mirrors Step Functions
task tokens (HMAC-signed, single-use, carries the approval id) and
the resume pattern mirrors Temporal's signal-with-start. We carry
the security ordering verbatim — verify HMAC before parsing the
payload — to neutralise parser-oracle attacks.

## Config example

```yaml
# regatta.yaml — one approval_gate entry per gates[] item.
# Field shape is pinned in contracts/schemas/regatta.v1.cue; the
# Go loader in internal/config/gates.go enforces the cross-field
# invariants V1-V11 from the spec.
gates:
  - id: prod_db_migrate
    type: approval_gate
    name: prod_db_migrate          # URL-safe; appears in slog + audit rows
    risk_class: high               # low | medium | high
    reviewers:                     # explicit identity list (preferred)
      - alice
      - bob
      - carol
    roles: []                      # optional role list; resolved statically in MVP
    quorum: 2                      # 2-of-3 must allow
    prevent_self_review: true      # block requested_by from voting (identity match only)
    timeout: 4h                    # overall approval lifetime; reaper fires after this
    decision_window: 1h            # per-token TTL; MUST be <= timeout (V3)
    on_timeout: escalate           # fail | auto_approve | escalate
    escalation_chain:              # required when on_timeout=escalate
      - reviewers: [oncall_lead]
        quorum: 1
        timeout: 2h
        decision_window: 30m
    # predicate_cel: 'outputs.scan_severity == "high"'  # optional skip-gate
```

The `id` is the regatta-wide gate identifier (matches other gate
types); the `name` is the approval-gate slug surfaced in URLs and
slog. Keep them aligned to avoid operator confusion.

## Field reference

| Field | Type | Required | Meaning |
|---|---|---|---|
| `id` | string | yes | Repo-wide gate id; matches `^[a-z0-9_-]+$` |
| `type` | enum | yes | Must be `approval_gate` |
| `name` | string | yes | URL-safe slug, `^[a-zA-Z0-9_-]{1,64}$` (V11) |
| `risk_class` | enum | yes | `low` \| `medium` \| `high` (V6) |
| `reviewers` | string[] | one of | Explicit reviewer ids, each `^[a-zA-Z0-9_:.-]{1,128}$` (V10) |
| `roles` | string[] | one of | Role names resolved via static `roles{}` map (MVP) |
| `quorum` | int | yes | N reviewers of M must allow; `1 ≤ quorum ≤ \|reviewers ∪ roles\|` (V7) |
| `prevent_self_review` | bool | no | If true, block the `requested_by` identity (default false) |
| `timeout` | duration | yes | Overall approval lifetime (Go duration: `4h`, `30m`); > 0 (V2) |
| `decision_window` | duration | yes | Per-token TTL; > 0 (V1) AND ≤ `timeout` (V3) |
| `on_timeout` | enum | yes | `fail` \| `auto_approve` \| `escalate` (V8) |
| `escalation_chain` | tier[] | when escalate | Required + non-empty when `on_timeout=escalate` (V9) |
| `predicate_cel` | string | no | CEL over outputs; if false, gate auto-approves (skip-the-human) |

At least one of `reviewers` or `roles` must be non-empty.

## Reviewer set: explicit vs roles

Two ways to populate the reviewer set:

- **Explicit (`reviewers: [alice, bob]`)** — preferred for high-risk
  gates. The set is fixed at gate config time; rotating who reviews
  means a `regatta.yaml` edit + PR. Auditable, no indirection.
- **Roles (`roles: [sre]`)** — resolved against the static
  `roles{}` map in `regatta.yaml`. In MVP role membership is a
  literal list of ids in config; there is no LDAP/Okta/GH-teams
  lookup yet (deferred per spec §2).

You may combine both — the effective set is the union. Quorum (V7)
bounds against the union size.

### Snapshot semantics

When the gate first observes a work item, it **snapshots** the
reviewer set into `approvals.reviewer_set_snapshot_json`. Editing
`regatta.yaml` afterwards has zero effect on in-flight approvals —
the decide path reads only the snapshot. This prevents
privilege-escalation by mid-flight config edit. To force a re-snap,
withdraw the work item and re-plan.

### `prevent_self_review` — identity match only

`prevent_self_review: true` blocks one specific case: a reviewer
whose id string-equals `approvals.requested_by` cannot vote. It is
**not** team-scope. The following are NOT enforced and remain the
operator's job to encode by choosing the reviewer set carefully:

- "same team / same squad / same on-call rotation"
- "same program / shared ownership"
- "social-graph proximity" (manager, mentor)

To enforce team-disjoint approval, set the gate's `reviewers` list
to exclude likely authors of the work item. A team-scope follow-up
is tracked separately (see spec §5.3.1).

## Quorum semantics

The fold over `approval_events` (kind=`decided`) terminates the
gate as follows. `R` is the reviewer-set size, `Q` is `quorum`.

| Condition | Terminal status | Trigger event row |
|---|---|---|
| `allow_count ≥ Q` | `approved` | `kind='approved'` actor=`system` |
| `deny_count ≥ Q` | `rejected` | `kind='rejected'` actor=`system` |
| `allow_count + deny_count ≥ R` AND neither side hits Q | `rejected` | gate cannot reach quorum; closes rejected |
| otherwise | `pending` | wait for next decide or reaper sweep |

Each reviewer can vote exactly once per approval (UNIQUE constraint
on token consumption + `decided_by` membership check). Re-presenting
the same token returns `ErrTokenReplay`.

Fold ordering uses `approval_events.id ASC` (single-writer monotonic
sqlite PK), **never** `ts` (wall-clock skew on multi-host notifiers
could produce nondeterministic replays).

## Timeout policies

The reaper sweep runs every tick; for each `approvals.status='pending'`
where `timeout_at < now`, the sweep applies the gate's `on_timeout`
policy:

- **`fail`** — status becomes `timed_out`; the work item transitions
  to `rejected`. Default. Safe for irreversible operations.
- **`auto_approve`** — status becomes `approved`; the work item
  resumes. Actor stamped `system:timeout-default` in the audit row
  so compliance grep can find every system-decided approval.
  **Foot-gun guard (V5)**: the config loader rejects this policy
  unless `risk_class: low`. The CUE schema and Go loader both
  enforce this — startup fails before any approval is ever
  requested. Forbidden examples documented below.
- **`escalate`** — status stays `pending`; the reviewer set is
  replaced by `escalation_chain[next]`. Prior tokens are revoked
  (any later use fails `ErrTokenReplay`). Prior `decided` votes are
  replayed against the new tier; overlapping reviewers' votes count
  toward the new quorum, non-overlapping votes are recorded with
  `payload.replayed_votes[*].discarded=true`. New tokens are minted
  for the new tier and re-notified.

### V5 foot-gun: `auto_approve` requires `risk_class: low`

`auto_approve` decides without human review. It is the right policy
ONLY for low-blast-radius gates whose purpose is audit recordkeeping
(e.g. nightly cache warmup proving it ran). Misapplied to a
high-blast gate it silently degrades the platform to no gate at all.

Forbidden — never `auto_approve` (rejected at config load):

- `delete_*` (DROP TABLE, DELETE FROM, S3 object delete, repo delete)
- `rotate_*` (key rotation, credential rotation, IAM role change)
- `deploy_*` (production deploy, prod-config push, schema migration)
- `refund_*` over a threshold, `payout_*`, `withdraw_*`

Permitted (`risk_class: low`):

- `cache_warmup` nightly batch
- `report_generation` scheduled
- `metric_rollup` post-run

If the loader rejects your config with `ErrAutoApproveRequiresLowRisk`,
either raise the gate to `on_timeout: fail` (safer default) or
genuinely justify `risk_class: low`.

## CLI flow

The reviewer receives a notification carrying a callback URL of the
form `regatta://approval/decide?token=<TOKEN>&reviewer-id=<ID>`. The
operator extracts the token and runs:

```sh
# 1. See what's waiting on me.
regatta approval list --mine alice
#  APPROVAL_ID   WORK_ITEM_ID  GATE_NAME       REQUESTED_AT          TIMEOUT_AT            REVIEWERS           QUORUM
#  a-7f3e91a2c1  wi-…          prod_db_migrate 2026-05-31T14:02:11Z  2026-05-31T18:02:11Z  alice,bob,carol     2

# 2. Decide. --reviewer-id is required and must match the signed reviewer in the token.
export REGATTA_APPROVAL_TOKEN_KEY="$(vault kv get -field=key secret/regatta/approval-token)"
regatta approval decide \
  --token "$TOKEN" \
  --decision allow \
  --reviewer-id alice \
  --reason "audit ticket ABC-1234 cleared"

# Exit 0 on success; non-zero per the exit-code table below.
```

`--format=json` on `list` emits the contract surface for scripting;
default is human table. The row shape is pinned by
`TestApprovalList_JSONShape` (key-presence) and
`TestApprovalList_JSONMatchesSchema` (full JSON Schema 2020-12
validation) against the machine-readable schema at
`contracts/schemas/approval_list.v1.json` — downstream tools can
consume that file directly to validate piped output.

### Where the token comes from

Tokens are minted by the orchestrator at gate-creation time and
handed to the configured `Notifier` (Slack, PagerDuty, email, or the
default `stub` which only writes a `slog.Info("approval.notify_stub")`
audit event). The wire format is `base64url(sig).base64url(payload)`,
HMAC-SHA256 signed under the key from `REGATTA_APPROVAL_TOKEN_KEY`
(or the env var named by `REGATTA_APPROVAL_TOKEN_KEY_ENV`) keyed by
`REGATTA_APPROVAL_TOKEN_KEY_ID` (default `k1`). The same key must be
loaded in the operator's environment when running `decide`.

If the stub notifier is in use and you cannot find a token, retrieve
it from the orchestrator's slog stream — look for
`approval.notify_stub` with the matching `approval_id`. Production
deployments wire a real notifier in W1+ separate PRs; check the
`notify:` block of `regatta.yaml` for the configured channel.

## Exit codes

`regatta approval decide` maps each typed sentinel to a distinct
exit code so runbooks can grep on the number. Source of truth:
`regatta approval decide -h`.

| Code | Sentinel | Meaning | Remediation |
|---|---|---|---|
| 0 | — | Decision recorded | None — work item resumes on next tick (allow) or transitions to `rejected` (deny) |
| 1 | `ErrTokenInvalid` | Wire envelope malformed (missing `.`, base64 decode failure) | Confirm the token string is intact — no shell-mangling, no truncated copy/paste. Re-fetch from the notifier |
| 2 | `ErrUnverifiable` | HMAC mismatch, unknown payload field, OR `--reviewer-id` ≠ signed reviewer | Confirm `REGATTA_APPROVAL_TOKEN_KEY` + `REGATTA_APPROVAL_TOKEN_KEY_ID` match the orchestrator's. Confirm `--reviewer-id` matches the recipient on the token. Do not forward another reviewer's URL |
| 3 | `ErrTokenExpired` | `decision_window_end_unix < now()` | Request a fresh token: the orchestrator re-mints on next tick if the approval is still pending. If `timeout_at` also passed, the approval is terminal — check `approvals.status` |
| 4 | `ErrTokenReplay` | Token already consumed (single-use; or revoked by escalation) | Your decision is already recorded, or this token was revoked when the gate escalated. Run `regatta approval list --mine <id>` and check for a new approval row + fresh token |
| 5 | `ErrUnknownKeyID` | Token's `kid` is not in the operator's keyring (key rotation per #79) | The signing key rotated. Request a fresh token from the orchestrator (it re-mints under the current `kid`) |
| 6 | `ErrNotReviewer` | `--reviewer-id` is not in `reviewer_set_snapshot` | You were not in the reviewer set at request-time. Cannot vote. Escalate to a snapshot reviewer or wait for `on_timeout=escalate` to expand the set |
| 7 | `ErrSelfReview` | `--reviewer-id == approvals.requested_by` AND `prevent_self_review: true` | You requested this work item. Find another reviewer; you cannot approve your own request |
| 2 (usage) | flag parse error | `--token` / `--decision` / `--reviewer-id` missing, or `--decision` ∉ {allow, deny} | Re-run with `-h` for the usage block; supply every required flag |

Exit code uniqueness is pinned by `TestApprovalDecide_ExitCodeMappingTable`
so a refactor cannot silently collapse two sentinels onto one code.

### Web POST: `token_replay` folds `ErrDoubleVote`

The HTTP callback handler (`/api/approval/callback`, shipped by PR #263)
collapses two distinct Go sentinels onto a single wire sentinel:

| Go sentinel | When it fires | Web sentinel (HTTP 409) | CLI exit code |
|---|---|---|---|
| `state.ErrTokenReplay` | Same token POSTed twice (UNIQUE `token_jti` constraint) or the approval already reached a terminal status | `token_replay` | 4 |
| `approval.ErrDoubleVote` | Same reviewer id appears in `decided_by` already (in-memory guard in `DecideTx`, fires before the token UNIQUE-trip) | `token_replay` | 1 (generic) |

The folding is intentional. The dominant trigger on the web is a Slack
button retry — the reviewer's first click already committed their vote,
and the second click is operator-facing identical ("your vote already
counted") regardless of whether a fresh token was minted between
clicks. A distinct `double_vote` HTTP sentinel would surface a Go-side
ordering detail (in-memory guard before SQL constraint) with no
actionable difference for the on-call.

The CLI keeps the sentinels distinct because terminal exit codes are a
stable contract surface that runbooks grep on. `slog` and the
`approval_events` audit trail are also unchanged — a real
`ErrTokenReplay` writes a `token_consumed` row that fails the UNIQUE
constraint (visible as the rollback in tx logs), while `ErrDoubleVote`
short-circuits before any row is written. So a compliance grep for
"who tried to re-vote" can still distinguish the two paths via the
event log, not via the HTTP sentinel.

If you see `error=token_replay` in the web access log AND no
corresponding `token_consumed` failure in the orchestrator slog, the
underlying cause was `ErrDoubleVote` (in-memory guard). Either way the
remediation is the same: that reviewer's vote is already recorded — no
action needed.

## MTTD runbook (≤ 60s)

You have been paged: "approval stuck on gate X". These four steps
take under 60 seconds end-to-end. Use them verbatim.

```sh
# Step 1 — list all pending approvals (5s). Identify the stuck row.
regatta approval list --format=json | jq '.[] | select(.gate_name=="prod_db_migrate")'
# Output: approval_id, work_item_id, reviewer_set, quorum, timeout_at_unix.
# Note the approval_id (e.g. a-7f3e91a2c1) for steps 2-4.

# Step 2 — dump the audit trail for this approval (10s). Why is it stuck?
APPROVAL_ID=a-7f3e91a2c1
sqlite3 regatta.db <<SQL
.headers on
.mode column
SELECT id, datetime(ts,'unixepoch') AS ts, kind, actor,
       json_extract(payload_json,'$.decision') AS decision,
       substr(payload_json,1,80) AS payload_preview
FROM approval_events
WHERE approval_id = '$APPROVAL_ID'
ORDER BY id;
SQL
# Read the kinds left-to-right: requested → notified → decided* → (approved|rejected|timed_out|escalated)
# Stuck = last row is 'notified' or 'decided' but no terminal kind, and timeout_at is in the future.

# Step 3 — decide (one of three; pick by the runbook for the gate):
# 3a. Cast a vote yourself if you are in the snapshot:
regatta approval decide --token "$TOKEN" --decision allow --reviewer-id alice --reason "$PAGE_ID"
# 3b. Force escalation by advancing the clock (test/dev only): not supported in prod CLI; wait for reaper.
# 3c. Withdraw the work item (terminal kill-switch) if the request is no longer valid:
sqlite3 regatta.db "UPDATE work_items SET status='withdrawn' WHERE id=(SELECT work_item_id FROM approvals WHERE id='$APPROVAL_ID');"
# (The next tick's reaper closes the orphan approval audit row.)

# Step 4 — verify (15s). Confirm the approval reached a terminal state.
sqlite3 regatta.db "SELECT status, datetime(decided_at,'unixepoch') AS decided_at, decided_by FROM approvals WHERE id='$APPROVAL_ID';"
# Expected: status ∈ {approved, rejected, timed_out}; decided_at populated.
# If still 'pending', re-run step 2 — a new event (escalated, timed_out) should be visible.
```

Externally timed on a single-host sqlite deployment with ~10 active
approvals: 4 steps complete in 35-45 seconds. The 60s budget allows
for one network round-trip to the secret store in step 3a.

If a step exceeds 20 seconds, the most common cause is a long
`approval_events` history (escalation chains with many tiers). The
indexes on `(approval_id, ts)` keep the dump O(rows-for-this-approval),
not O(total events) — confirm via `EXPLAIN QUERY PLAN`.

## Audit trail — SQL one-liners for compliance export

Every approval transition writes a row in `approval_events`. The
schema is forward-only (migration `0004_approvals.sql`) — old rows
never mutate, so a `SELECT ... ORDER BY id` over the table is a
byte-stable export.

```sh
# Full audit for one approval (use this on every page).
sqlite3 -header -column regatta.db \
  "SELECT id, datetime(ts,'unixepoch') AS ts, kind, actor, payload_json
   FROM approval_events WHERE approval_id='a-7f3e91a2c1' ORDER BY id;"

# Every approval decided in the last 24 hours (compliance daily report).
sqlite3 -csv -header regatta.db \
  "SELECT a.id, a.gate_name, a.status, datetime(a.decided_at,'unixepoch') AS decided_at,
          a.decided_by, a.work_item_id
   FROM approvals a
   WHERE a.decided_at >= strftime('%s','now','-1 day')
   ORDER BY a.decided_at;" > approvals-$(date -u +%F).csv

# Every auto_approve event (compliance must review these — system decided, no human).
sqlite3 -header -column regatta.db \
  "SELECT approval_id, datetime(ts,'unixepoch') AS ts, actor, payload_json
   FROM approval_events
   WHERE kind='approved' AND actor='system:timeout-default'
   ORDER BY ts;"

# Every escalated approval + the tier chain it traversed.
sqlite3 -header -column regatta.db \
  "SELECT approval_id, datetime(ts,'unixepoch') AS ts,
          json_extract(payload_json,'$.prior_chain_index') AS prior_tier,
          json_extract(payload_json,'$.new_chain_index')   AS new_tier
   FROM approval_events
   WHERE kind='escalated' ORDER BY id;"

# Token-revocation audit (any token that was killed by escalation or key rotation).
sqlite3 -header -column regatta.db \
  "SELECT approval_id, token_jti,
          json_extract(payload_json,'$.reason') AS reason,
          datetime(ts,'unixepoch') AS ts
   FROM approval_events
   WHERE kind='token_consumed' AND json_extract(payload_json,'$.reason') IS NOT NULL
   ORDER BY ts;"
```

Tokens never appear in `approval_events.payload_json` in cleartext —
only the `jti` (random nonce) and the MAC prefix in
`approvals.callback_token_hmac_sig`. Exporting `approval_events` to
a compliance SIEM cannot leak a token body. See [install.md](install.md)
for off-host audit-sink wiring once issue #80 lands.

## Limitations (MVP)

- **Single-orchestrator sqlite** — if the host dies and `regatta.db`
  is unrecoverable, audit is lost until #80 ships off-host durable
  audit sinks. Mitigation today: back up `regatta.db` on a schedule.
- **No `approve_with_edits`** — only `allow` and `deny`. Edit-on-approve
  needs the typed-outputs schema deferred to MVP-3.
- **CLI auth boundary** — `--reviewer-id` matches the signed token,
  but the chain of trust from human → CLI is the operator's
  responsibility (SSH key, sudo, etc.). Web-callback adapters in
  future waves will add real human auth.
- **`prevent_self_review` is identity-match only** — see above.
  Team-scope is a tracked follow-up.
- **Role membership is static** — `roles{}` is a literal list in
  `regatta.yaml`; no LDAP/Okta/GH-teams lookup yet.

## Cross-references

- Quickstart: [quickstart.md](quickstart.md)
- Configure: [configure.md](configure.md)
- Day-1 / Day-7 / Day-30 ops: [day1.md](day1.md), [day7.md](day7.md), [day30.md](day30.md)
- Wedge background: `../wedges/approval-gates.md`
