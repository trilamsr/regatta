# Quickstart

Reader: customer-operator running Regatta for the first time.
Read time: 5 minutes.
Goal: validated config + verified repo posture in <60 seconds.
Expires when: `regatta init` / `validate-config` / `verify-repo-config`
output format changes.

## Status

`regatta init`, `l0`, `validate-config`, and `verify-repo-config`
ship today. AI gates (L3/L4/L5) and the orchestrator runtime are
in progress; the schemas + this quickstart pin the contract.

## 1. Install

```sh
brew install trilamsr/regatta/regatta
# or:  go install github.com/trilamsr/regatta/cmd/regatta@latest
```

See [install.md](install.md) for offline / pinned-version paths.

## 2. Scaffold

```sh
cd ~/code/myproject
regatta init
```

`regatta init` writes a starter `regatta.yaml`, drops a demo attack
into `.regatta/sample.diff`, and runs the L0 gate against the demo so
you see in one command what regatta catches.

Commit `regatta.yaml` to git. Add `.regatta/` to `.gitignore` — the
directory holds local state (`sample.diff`, future `items/`,
`worktrees/`, `state.db`) that should not be versioned.

Required fields per the v1 schema: `version`, `repo`, `spec_adapter`,
`ci.command`, `gates`, `safety`. See
[configure.md](configure.md#required-fields) for the full surface
with defaults and semantics.

Use [`examples/minimal/regatta.yaml`](../../examples/minimal/regatta.yaml)
as a starting point. The full surface is in
[`examples/full/regatta.yaml`](../../examples/full/regatta.yaml).

## 3. Audit the repo

```sh
regatta verify-repo-config         # branch protection + CODEOWNERS audit
```

Mandatory before the first `regatta serve`. Fails closed on the
silent-bypass classes documented in [day1.md](day1.md). Pass
`--accept-degraded` only when the gap is named in your security
posture (it is logged to the audit sink either way).

## 4. Program plan + serve walkthrough (MVP-1)

Once `regatta init` is done, the program-plan loop looks like this:

```sh
# 1. Author a program markdown item with at least 3 acceptance criteria.
cat > .regatta/items/PROG-1.md <<'EOF'
---
id: PROG-1
kind: program
title: first program
lane: server
status: planned
---

## Acceptance criteria

- [planned] c1: add foo
- [planned] c2: add bar
- [planned] c3: add baz
EOF

# 2. Plan, sign, and persist the brief.
export REGATTA_HMAC_KEY=$(openssl rand -hex 32)
regatta program plan --hmac-key-env=REGATTA_HMAC_KEY --write \
  .regatta/items/PROG-1.md

# 3. Spin up one tick to spawn child agents.
regatta serve --spawner=stub --tick-once --repo .

# 4. Verify.
sqlite3 .regatta/state.db \
  "SELECT id, status FROM work_items WHERE parent_program_id='PROG-1'"
# Expected: 3 rows, status planned before the tick and running after.
```

`--write` performs an atomic temp+rename into
`.regatta/programs/<program_id>.json`; signature, sha256, and
acceptance set are persisted in one file. `--tick-once` runs a single
poll-and-spawn cycle and exits, which is the supported shape for
scripted smoke tests and end-to-end fixtures.

### Troubleshooting

- **`ErrFlockHeld`**: another `regatta serve` process holds the
  database lock. Check with `lsof .regatta/state.db.lock` or read the
  PID from the lockfile directly. If you know the process is dead,
  delete the lockfile and retry; Regatta also auto-reclaims on the
  next call when the recorded PID is no longer alive.

- **Rejected briefs**: search the operator log:
  ```sh
  journalctl -u regatta | grep brief.rejected
  ```
  Each rejection logs `path` and `reason`. Most common: stale HMAC
  key after rotation. Re-run `regatta program plan --write` with the
  new key.

### slog event reference (MVP-1)

WARN-level events the orchestrator emits during the program-plan
loop, with the trigger that produces each:

| Event | Trigger |
|---|---|
| `brief.rejected` | HMAC verify failed, sha256 mismatch, or parse error during `LoadAndVerifyBrief`. |
| `brief.tombstoned` | A previously-loaded brief file disappeared between ticks. |
| `adapter.tombstoned` | A SpecAdapter item disappeared between ticks; the corresponding work_item is archived. |
| `child.cascade_archived` | Parent program archived; child work_items archived alongside. |
| `child.dependency_archived` | A child's `depends_on` target was tombstoned; the child is archived. |

Happy-path runs emit none of these. Any WARN during a steady-state
run is a real signal; the adversarial scenarios in
[`docs/engineer/mvp-1-dod-checklist.md`](../engineer/mvp-1-dod-checklist.md)
enumerate when each is expected.

## 5. Authoring a conditional DAG (MVP-2)

`schema_version: 2` briefs add conditional routing: an upstream
feature's journaled output drives which downstream features spawn.
Predicates are CEL expressions over `out.<field>` paths declared in
the upstream's `outputs_schema`.

Concrete fixture: [`testdata/v2_briefs_e2e/PROG-2.md`](../../testdata/v2_briefs_e2e/PROG-2.md)
is the parent work item; the e2e test
[`internal/program/end_to_end_v2_test.go`](../../internal/program/end_to_end_v2_test.go)
stamps the corresponding signed v2 brief and asserts on the wired
behaviour. Use that pairing as the copy-paste starting point.

The signed brief drops into `.regatta/programs/<program_id>.json`
and decomposes the parent. Minimal shape:

```json
{
  "schema_version": 2,
  "program_id": "m-aaaaaaaaaaaa",
  "parent_work_item_id": "PROG-2",
  "parent_criteria": [
    {"id": "c1", "text": "scan + tag severity"},
    {"id": "c2", "text": "deep-remediate when severity=high"},
    {"id": "c3", "text": "fast-path otherwise"}
  ],
  "planner_model_id": "stub:v2",
  "features": [
    {
      "id": "F-SCAN",
      "title": "scan",
      "fulfills": ["c1"],
      "outputs_schema": {
        "type": "object",
        "properties": {"severity": {"type": "string"}}
      },
      "edges": [
        {"from": "F-SCAN", "to": "F-DEEP", "predicate": "out.severity == \"high\"", "on_skip": "cascade"}
      ],
      "default_next": "F-QUICK"
    },
    {
      "id": "F-DEEP",
      "title": "deep-remediate",
      "fulfills": ["c2"],
      "edges": [{"from": "F-DEEP", "to": "F-QUICK"}]
    },
    {"id": "F-QUICK", "title": "fast-path", "fulfills": ["c3"]}
  ],
  "produced_at": "2026-05-31T12:00:00Z"
}
```

Routing semantics:

- **Predicated edges** fire when the CEL expression evaluates `true`
  against the upstream feature's journaled output. The supported CEL
  surface is comparison ops, boolean logic, `has(out.x)` for optional
  fields, and `out.x in ["a","b"]` for enum membership.
- **`default_next`** is required whenever a feature emits at least
  one predicated outgoing edge; it pins the fallback target so the
  scheduler never strands flow. The default's target must be
  reachable from the source via the forward closure of outgoing
  edges (`CheckReachability` gate; rejection sentinel
  `ErrEdgeUnreachable`).
- **`on_skip: cascade`** (the default) propagates skips downstream;
  `on_skip: ignore` is the diamond-join escape hatch — the target
  spawns when at least one inbound edge fired, matching Airflow's
  `none_failed_min_one_success`.

Inspect routing decisions via the journal + edges tables:

```sh
sqlite3 .regatta/state.db "SELECT work_item_id, attempt_no, content_sha FROM work_item_outputs ORDER BY id"
sqlite3 .regatta/state.db "SELECT from_id, to_id, fired, predicate_cel FROM work_item_edges"
```

`content_sha` pins the canonical sha256 of the upstream output that
gated each edge decision — replay tooling lands in a follow-up wave.

Rejection sentinels surfaced under `brief.rejected` slog events:

| Sentinel | Operator action |
|---|---|
| `ErrPredicateCompile` | Fix CEL syntax in the failing edge predicate. |
| `ErrPredicateUnknownField` | Add the referenced field to `outputs_schema`, or correct the predicate. |
| `ErrPredicateTypeMismatch` | Align the literal type against `outputs_schema.<field>.type`. |
| `ErrEdgeMissingDefault` | Add `default_next` on the feature whose outgoing edges are predicated. |
| `ErrEdgeUnknownTarget` | The edge points at a feature ID absent from the brief; check spelling. |
| `ErrEdgeUnreachable` | Make `default_next` reachable through outgoing edges (chain through a sibling). |

## 6. Next

- [day1.md](day1.md) walks through the full Day 1 install + validate
  loop.
- [day7.md](day7.md) covers turning the orchestrator on for one lane.
- [day30.md](day30.md) covers all-lane promotion criteria.
- [configure.md](configure.md) explains every field in
  `regatta.yaml`.
- [upgrade.md](upgrade.md) covers `regatta migrate-config` for schema
  bumps.
