# Quickstart

Reader: customer-operator running Regatta for the first time.
Read time: 5 minutes.
Goal: validated config + verified repo posture in <60 seconds.
Expires when: `regatta init` / `validate-config` / `verify-repo-config`
output format changes.

## Status

Regatta is pre-implementation. Commands below describe the v1
release surface; the schemas + this quickstart are the contract.
The binary follows.

## 1. Install

```sh
brew install trilamsr/regatta/regatta
# or:  go install github.com/trilamsr/regatta/cmd/regatta@latest
```

See [install.md](install.md) for offline / pinned-version paths.

## 2. Scaffold + validate

```sh
cd ~/code/myproject
regatta init                       # writes regatta.yaml skeleton
$EDITOR regatta.yaml               # fill in version, repo, spec_adapter,
                                   # ci.command, gates, safety
regatta validate-config            # CUE-validates regatta.yaml
```

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
