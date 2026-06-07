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

## Verifying the wired spec adapter

After editing `spec_adapter.type` and restarting `regatta serve`, confirm the change took effect via the boot log. Every boot emits one `adapter.configured` INFO record naming the wired type plus the resolved selector / items root (#867):

```
level=INFO msg=adapter.configured type=github_issues selector=label:autonomous repo=trilamsr/regatta
level=INFO msg=adapter.configured type=markdown_catalog items_root=/repo
```

If `regatta.yaml` parses but does not match the schema, a `adapter.config_load_failed` WARN record names the parse error before the default `markdown_catalog` fallback engages — so a malformed yaml never silently downgrades a `github_issues` deployment.

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
format (stub today; full shape lands with the audit-sink writer).

## Context

`context.trusted_doc_paths` lists the prose surfaces L4 reads from
the signed `main` SHA. Typical entries: `PRINCIPLES.md`, `STYLE.md`,
`AGENTS.md`. Documents outside this list are treated as data, not
instructions.

## Prompts

### `prompts.planner_sha` (optional)

Hex-encoded sha256 of `contracts/prompts/planner.md`. When set, the
binary refuses to start with a prompt file that doesn't match -
fail-closed against on-disk drift between the operator-pinned
contract and the file the planner actually loads. Unset means the
binary falls back to its embedded copy of the prompt and does not
read from disk.

Compute the digest with:

```sh
sha256sum contracts/prompts/planner.md
```

Paste the hex string into `regatta.yaml`:

```yaml
prompts:
  planner_sha: 8f4e...c0
```

Rotation: bump the file, recompute the digest, update the config in
the same commit. A mismatch on startup logs the expected vs. actual
sha and exits non-zero before any work is scheduled.

## Program briefs (MVP-2 conditional DAG)

`regatta.yaml` configures the orchestrator; program briefs live
separately under `.regatta/programs/<program_id>.json` and decompose
parent work items into features.

`schema_version: 2` briefs add:

- `outputs_schema` on a feature declares the JSON shape that
  feature's terminal output must produce. Used at brief-load time to
  type-check predicates referencing `out.<field>`.
- `edges` carries outgoing relations from a feature; each edge has a
  `from`, `to`, optional CEL `predicate`, and `on_skip` mode
  (`cascade` default, or `ignore` for diamond joins).
- `default_next` names the fallback target when every predicated
  outgoing edge resolves false at runtime. Required whenever a
  feature has at least one predicated edge.

Walkthrough + minimal example: [quickstart.md §5](quickstart.md#5-authoring-a-conditional-dag-mvp-2).
Fixture pairing: [`testdata/v2_briefs_e2e/PROG-2.md`](../../testdata/v2_briefs_e2e/PROG-2.md)
plus [`internal/program/end_to_end_v2_test.go`](../../internal/program/end_to_end_v2_test.go).
Rejection sentinels are documented in the same section.

## Secrets

The `secrets:` block consolidates every operator-supplied credential
into one place. Per-secret you choose a source (`env`, `keychain`,
`pass`, `file`). Omit the block to keep the pre-#911 behaviour: each
canonical key resolves from its default env-var name.

| Canonical key      | Default env-var fallback                | YAML field          |
|--------------------|------------------------------------------|---------------------|
| anthropic_api_key  | `ANTHROPIC_API_KEY`                      | `anthropic_api_key` |
| gh_token           | `GH_TOKEN` then `GITHUB_TOKEN`           | `gh_token`          |
| brief_hmac         | `REGATTA_HMAC_KEYRING` then `REGATTA_HMAC_KEY` | `brief_hmac`  |
| audit_hmac         | `REGATTA_AUDIT_HMAC_KEY`                 | `audit_hmac`        |
| approval_token     | `REGATTA_APPROVAL_TOKEN_KEY`             | `approval_token`    |

Example:

```yaml
secrets:
  anthropic_api_key:
    source: keychain
    name: regatta/anthropic
  gh_token:
    source: env
    name: GH_TOKEN_REVIEWER
  brief_hmac:
    source: file
    path: /etc/regatta/brief.key
    key_id: brief-2026-06
```

If you use defaults today, change nothing — the loader falls back to
the env-var names above when the corresponding yaml field is absent.

`source: file` requires the file mode to be `0600` or stricter; a
world- or group-readable path fails closed at boot.
