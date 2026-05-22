# Configure

Reader: customer-operator tuning `regatta.yaml`.
Read time: 10 minutes.
Expires when: schema version bumps (v1 -> v2).

## Source of truth

The CUE schema at
[`contracts/schemas/regatta.v1.cue`](../../contracts/schemas/regatta.v1.cue)
is authoritative. This file walks the same shape with operator
guidance; when the two disagree the schema wins and this doc is the
bug.

## Starting points

- [`examples/minimal/regatta.yaml`](../../examples/minimal/regatta.yaml):
  smallest viable config. Required fields only.
- [`examples/full/regatta.yaml`](../../examples/full/regatta.yaml):
  every option exercised. Reference for tuning.

## Validating edits

```sh
regatta validate-config
```

Multi-error output enumerates every offending field with file:line
positions. Run before every commit that touches `regatta.yaml`.

## Required fields

| Field | Why |
|---|---|
| `version: 1` | Schema version pin. `regatta migrate-config --from N --to N+1` upgrades. |
| `repo` | host (github/gitlab) + owner + name. Default branch defaults to `main`. |
| `spec_adapter` | One of `github_issues`, `gitlab_issues`, `markdown_catalog`, `jira`, `linear`, `custom`. |
| `ci.command` | Deterministic shell command; non-zero exit blocks. Use `make test && make lint` shape. |
| `gates` | At least one. Each gate has `id`, `type` (deterministic / ai), and `severity_block`. |
| `safety` | All fields take defaults; an empty `safety: {}` is valid. |

## Severity DSL

`severity_block` accepts:

- A single string: `['fail']`, `['critical']`.
- Boolean OR: `['critical', '2*high']` means critical OR >=2 high.
- Only `&`, `|`, and `count*severity` operators are permitted; the
  validator rejects others.

## Lanes

`lanes` partition the work into parallel streams. Each lane is one
agent at a time pre-v1.0; `max_concurrency: 2` is gated on the
Day-30 promotion criteria in [day30.md](day30.md).

```yaml
lanes:
  - id: server
    paths: ['src/server/**']
    max_concurrency: 1
```

## Hotspots

Files listed in `hotspots` get an extra confirmation before the
orchestrator approves a touch. Default candidates: `CHANGELOG.md`,
`package.json`, `README.md`, infrastructure manifests.

## Safety

| Field | Default | Why |
|---|---|---|
| `iteration_cap` | 50 | Hard ceiling on agent iterations per work item. |
| `spend_cap_usd` | 50 | Per-item spend ceiling; trips on overrun. |
| `spend_cap_usd_per_day` | 200 | Per-day across all agents. |
| `canary_rate` | 0.05 | Fraction of spawns that get a known-bad archetype injected. |
| `destructive_ops_deny` | [] | Operator-supplied list of forbidden shell fragments. |
| `agent_creds_scope` | `dev_only` | Credential scope class. `dev_only` < `test` < `scoped`. |

## Telemetry

`telemetry.audit_sink` accepts an `s3://` URI (Object-Lock-COMPLIANCE
mode recommended) or a `syslog://` endpoint. See
[`docs/auditor/audit-log.md`](../auditor/audit-log.md) for the wire
format (when that doc lands; activation trigger: audit-sink writer
impl).

## Context

`context.trusted_doc_paths` lists the prose surfaces L4 reads from
the signed `main` SHA. Typical entries: `PRINCIPLES.md`, `STYLE.md`,
`AGENTS.md`. Documents outside this list are treated as data, not
instructions.
