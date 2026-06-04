# Getting started — self-host operator

Reader: operator standing up Regatta against their own repo for the
first time, end-to-end from `git clone` to a first agent-opened PR.
Read time: 15 minutes.
Goal: a running `regatta serve` against a real repo, one work item
ingested, one PR opened by the agent, cost caps + key rotation +
adversarial gate configured.
Expires when: `regatta.yaml` schema, the `regatta serve` flag
surface, or the `keys`/`l4` configuration shapes change.

This doc is the long-form companion to [quickstart.md](quickstart.md).
Quickstart pins the contract in five minutes; this walkthrough takes
the operator through each surface with the rationale baked in.

## 1. Clone and build

```sh
git clone https://github.com/trilamsr/regatta
cd regatta
make check   # local gate; <60s. Same gate CI runs.
go build -trimpath -o regatta ./cmd/regatta
```

`make check` runs `go-check`, `lint`, `tidy-check`, `doc-check`,
`prose-dup`, and `verify-vendored-assets` (see [`Makefile`](../../Makefile)).
The full pinned-tag / Homebrew / `go install` flow lives in
[install.md](install.md); building from source is the audit-posture
path for operators who plan to read the diff between releases.

After build, drop the binary on `PATH` or invoke it as `./regatta`
through the rest of this doc.

## 2. Author `regatta.yaml`

Copy [`examples/minimal/regatta.yaml`](../../examples/minimal/regatta.yaml)
into your target repo and edit `repo.owner` + `repo.name`. That file
is the smallest config that passes `regatta validate-config`:

```sh
cp /path/to/regatta/examples/minimal/regatta.yaml ./regatta.yaml
$EDITOR ./regatta.yaml
regatta validate-config --config ./regatta.yaml
```

Every required field is named in the file: `version`, `repo`,
`spec_adapter`, `ci.command`, `gates`, `safety`. Defaults from
`contracts/schemas/regatta.v1.cue` apply to everything else; the full
surface is in [`examples/full/regatta.yaml`](../../examples/full/regatta.yaml)
and field-by-field semantics live in [configure.md](configure.md).

For a markdown-driven workflow (recommended for self-host — no GitHub
Issues round-trip), switch `spec_adapter`:

```yaml
spec_adapter:
  type: markdown_catalog
  root: .   # items live at <repo>/.regatta/items/
```

The Regatta repo's own `regatta.yaml` at the repo root is a worked
example of the markdown-catalog shape with human-merge approval gate
+ doc-check deterministic gate; cite it when in doubt.

Commit `regatta.yaml`. Add `.regatta/state.db*` + `.regatta/programs/`
+ `.regatta/worktrees/` to `.gitignore`; `.regatta/items/` IS tracked
when you use `markdown_catalog`.

## 3. Ingest work items

Regatta needs at least one work item before `serve` does anything
useful. Three ingestion shapes; pick the one that fits the team's
existing process.

**Hand-authored markdown.** Drop a file under `.regatta/items/`:

```sh
mkdir -p .regatta/items
cat > .regatta/items/ITEM-1.md <<'EOF'
---
id: ITEM-1
title: add /healthz endpoint
lane: server
status: planned
---

## Acceptance criteria

- [planned] c1: `GET /healthz` returns 200 + JSON readiness envelope
- [planned] c2: handler test under internal/server/...
- [planned] c3: route wired in cmd/server/main.go
EOF
```

**Boot-prompt converter.** If a roadmap document already exists at
`docs/engineer/autonomous-session-prompt.md` (or equivalent), run:

```sh
make items
```

This invokes [`cmd/boot-prompt-to-items`](../../cmd/boot-prompt-to-items)
and writes one item file per PRIORITY entry. Idempotent — safe to
re-run when the roadmap changes.

**GitHub `[followup]` triage.** If the team files follow-up issues
on GH with a `followup` label, run:

```sh
make followups
```

This invokes [`cmd/gh-followup-to-items`](../../cmd/gh-followup-to-items)
which uses `gh issue list` and writes one `gh-issue-<n>-<slug>.md` per
labeled issue. Also idempotent.

The Regatta repo runs all three sources side by side; see
`.regatta/items/` at HEAD for the on-disk shape.

## 4. `regatta serve` — watch a PR get opened

Generate an HMAC key (briefs and approval tokens are signed):

```sh
export REGATTA_HMAC_KEY=$(openssl rand -hex 32)
```

Then start the orchestrator:

```sh
regatta serve --repo . --spawner=claude
```

What happens on the first tick:

1. `spec_adapter` reads `.regatta/items/*.md`, inserts new rows into
   `work_items`.
2. Scheduler picks one spawnable work item per lane, opens a worktree
   under `.regatta/worktrees/`, signs a brief into
   `.regatta/programs/<id>.json`, spawns the agent.
3. Agent runs CI (`ci.command` from `regatta.yaml`), gates fire, then
   the agent opens a PR via `gh pr create`.

For a one-shot smoke test that exits after a single tick (useful for
scripted CI of your own config), pass `--tick-once`:

```sh
regatta serve --repo . --spawner=claude --tick-once
```

Verify on-disk state:

```sh
sqlite3 .regatta/state.db \
  "SELECT id, status FROM work_items"
```

The first PR shows up at
`https://github.com/<owner>/<name>/pulls?q=is:pr+author:app/claude`.
Approval gates (human-merge in particular) hold the PR until an
operator clicks merge; see [approval-gates.md](approval-gates.md) for
the resolve / escalate / timeout surface.

## 5. Cost caps

`safety.cost` gates every spawn against a USD ceiling. Full surface
+ precedence rules in [cost-governor.md](cost-governor.md); this
section pins the two knobs new operators set wrong the most often.

Minimal block:

```yaml
safety:
  cost:
    per_dag_usd: 100
    per_operator_usd: 50
    period: 1d
```

**`estimation_strategy`: `upper_bound` vs `history`.** The pre-call
estimator computes a USD figure that the scheduler checks against
the configured caps. Two strategies:

| Strategy | When to use |
| --- | --- |
| `upper_bound` (default) | Cold-start posture. `input × price_in + max_output × price_out` — deterministic and pessimistic. Burns a few extra cents of headroom; never undershoots. |
| `history` | After 100+ recorded `token_spend` rows per `(tenant, operator, model)` cohort. P95 of the cohort's recent calls; cold-start falls back to `upper_bound` until ≥10 samples. Cuts soft-cap thrash on workloads whose actual output is consistently below `max_output`. |

Start with `upper_bound`. Switch to `history` only after the
`token_spend` table has enough rows and the operator sees repeated
soft-cap WARN-logs on calls that ended up well under cap.

**`pricing_override_path`.** The hardcoded pricing tables under
`internal/cost/pricing/` cover the Standard tier for Anthropic
direct + Bedrock + Vertex. Operators on Bedrock Batch / Provisioned
Throughput, Vertex Provisioned Throughput, marketplace resellers, or
private contracts MUST provide an override file:

```yaml
safety:
  cost:
    pricing_override_path: /etc/regatta/pricing.override.json
```

File shape and ownership rules are in
[cost-governor.md §`pricing_override_path`](cost-governor.md#pricing_override_path--escape-hatch-for-forked-rates).
Two points trip people up: the file MUST be mode `0600` owned by the
regatta process user (boot fails closed otherwise), and the load is
one-shot at boot — restart the process after editing.

## 6. HMAC key rotation drill

Briefs are signed under an HMAC key set via `REGATTA_HMAC_KEY` (or
the multi-key `REGATTA_HMAC_KEYRING` env, `keyID:hex,keyID:hex,...`).
The verifier walks every key in the keyring; the signer uses the
last entry (or the explicit `REGATTA_HMAC_KEY_ID` override).

Four-step rotation drill — verify-side wins, no fleet downtime:

```sh
# 1. ADD the new key alongside the current one. Verify-side picks up
#    both; signer keeps using the prior active key until step 3.
export REGATTA_HMAC_KEYRING="k1:$(printenv REGATTA_HMAC_KEY_HEX_OLD),k2:$(openssl rand -hex 32)"

# 2. WAIT for the next reconcile cycle (or restart `regatta serve`)
#    so the running process picks up the expanded keyring. Existing
#    briefs continue to verify under k1 unchanged.

# 3. RETIRE k1 by re-signing every brief in .regatta/programs/ under
#    the new active key.
regatta keys re-sign-briefs \
  -old-key-id k1 -old-key-env REGATTA_HMAC_KEY_HEX_OLD \
  -new-key-id k2 -new-key-env REGATTA_HMAC_KEY_HEX_NEW \
  -dir .regatta/programs \
  -dry-run    # inspect first; drop --dry-run to write.

# 4. RECOVER if step 3 fails any brief — re-signer fails loud on the
#    first mismatch and writes nothing else. Re-run with --dry-run to
#    inspect, fix the bad brief by hand, then drop --dry-run and
#    re-run. Once `re-sign-briefs` reports 0 skipped, drop k1 from the
#    keyring env and restart.
```

Why this shape: verify-side accepts the union, so step 1 is a no-op
from the agent's perspective; signer flips atomically at step 3 once
every existing brief is re-MACed. Step 4 is the safety net — the tool
refuses to corrupt a brief it cannot verify under the retiring key.

## 7. Adversarial-review gate (L4)

The L4 gate spawns an adversarial-reviewer LLM against every PR diff
and blocks merge on critical findings. Defaults are sane; the four
knobs operators tune:

```yaml
gates:
  - id: l4_adversarial
    type: ai
    model: claude-sonnet-4-6           # primary reviewer
    severity_block: ['critical', '2*high']
    second_opinion_model: claude-opus-4-7   # alt when PR body disputes
    auto_fix: false                    # opt-in: emit patches on findings
    models:
      security: claude-opus-4-7        # per-category override
      refactor: claude-haiku-4-5
```

**`model`.** Primary reviewer. Default `claude-sonnet-4-6` is the
balance point; Opus 4.7 catches more at higher token cost; Haiku 4.5
runs in advisory mode for cost-bounded lanes. Resolution order is
`yaml > REGATTA_GATES_L4_MODEL env > default`.

**`models.<category>`.** Per-category routing. Categories follow the
spec hunt-list (`security`, `refactor`, `correctness`, ...). The gate
buckets findings by resolved model and emits one Invoker call per
distinct bucket — uniform overrides collapse to one call so cost
stays flat when every category points at the same model.

**`second_opinion_model`.** Escalation alt-model. When a PR body
contains a `[L4-DISPUTE] <finding-id>` line (one per line, or
comma-separated IDs after the marker), the gate re-runs only the
disputed findings through this model and drops any that the alt
fails to confirm. Use Opus 4.7 here even if Sonnet is the primary —
disagreement is the load-bearing reason to spend the extra tokens.

**`auto_fix`.** Off by default. When `true`, findings the primary
model flags `auto_fixable=true` carry a unified-diff `Patch` body
which downstream PR commenters render in a fenced ```diff block. The
gate never applies the patch; the operator (or a follow-up agent
loop) decides.

## 8. Troubleshooting

**Substrate divergence audit.** Every state mutation lands in
`substrate_events` (append-only). When the dashboard shows a status
the operator does not expect, query the raw event stream first:

```sh
sqlite3 .regatta/state.db \
  "SELECT id, kind, run_id, ts FROM substrate_events
   WHERE run_id = '<id>' ORDER BY id"
```

Divergence between the projected `work_items.status` and the event
trail is a `state_reducer` bug; capture both sides before filing.

**Brief rejection audit.** When agents fail to spawn after step 4 of
the rotation drill, search the operator log:

```sh
journalctl -u regatta | grep brief.rejected
# or, if running attached:
regatta serve --repo . 2>&1 | grep -E 'brief\.(rejected|tombstoned)'
```

Each rejection logs `path` + `reason`. Most common: stale HMAC key
after rotation (re-run `keys re-sign-briefs`) or a sha256 mismatch
(the brief on disk was hand-edited; remove + regenerate).

**L4 findings cache.** Per-PR L4 results are journaled to
`substrate_events` with `kind='l4_result'`. When a finding looks
wrong, pull the exact prompt + model output:

```sh
sqlite3 .regatta/state.db \
  "SELECT payload_json FROM substrate_events
   WHERE kind='l4_result' AND run_id='<pr-sha>' LIMIT 1"
```

A `[L4-DISPUTE]` line in the PR body is the operator-visible path;
the cache query is for debugging the gate itself.

## Next

- [day1.md](day1.md) — the install + validate audit for a fresh
  deployment.
- [day7.md](day7.md) — orchestrator on, one lane.
- [day30.md](day30.md) — all-lane promotion + halt criteria.
- [configure.md](configure.md) — every `regatta.yaml` field with
  defaults and semantics.
- [cost-governor.md](cost-governor.md) — full cost-governor runbook
  (reconciliation, drift alerts, pricing refresh cadence).
- [approval-gates.md](approval-gates.md) — HITL gate operator surface
  (decide, escalate, timeout, MTTD runbook).
- [rbac-onboarding.md](rbac-onboarding.md) — multi-tenant policy
  revisions and OPA hot-reload.
- [observability.md](observability.md) — OTel spans, slog events,
  audit-sink wiring.
- [upgrade.md](upgrade.md) — version bumps, schema migrations,
  rollback.
</content>
</invoke>