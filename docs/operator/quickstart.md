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

## 4. Next

- [day1.md](day1.md) walks through the full Day 1 install + validate
  loop.
- [day7.md](day7.md) covers turning the orchestrator on for one lane.
- [day30.md](day30.md) covers all-lane promotion criteria.
- [configure.md](configure.md) explains every field in
  `regatta.yaml`.
- [upgrade.md](upgrade.md) covers `regatta migrate-config` for schema
  bumps.
