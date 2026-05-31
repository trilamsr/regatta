# Upgrade

Reader: customer-operator upgrading Regatta or `regatta.yaml`.
Read time: 3 minutes.
Expires when: schema v2 lands OR upgrade workflow changes.

## Status

Pre-v1.0: anything may break. Read the release notes for the tag
you are upgrading to. `regatta migrate-config` is the planned schema-
migration entry point but is not yet implemented (see [`docs/engineer/followups.md`](../engineer/followups.md));
for v0.x, edit `regatta.yaml` by hand and re-run `regatta validate-config`.

After v1.0: SemVer strict; contracts follow the deprecation cycle
per PRINCIPLES #11 (warn one minor, fail the next).

## Binary upgrade

```sh
brew upgrade regatta
# or:
go install github.com/trilamsr/regatta/cmd/regatta@latest
regatta version
```

Confirm the new version + commit SHA against the release notes
before starting the orchestrator.

## Config upgrade

```sh
regatta migrate-config --from 1 --to 2 --in regatta.yaml --out regatta.yaml.next
regatta validate-config --config regatta.yaml.next
mv regatta.yaml.next regatta.yaml
```

The migration tool is the operator's only contract surface for
schema bumps; WE eat the migration burden, not you. Manual hand-
edits are not required.

## Rollback

```sh
brew install regatta@<previous-tag>
regatta migrate-config --from 2 --to 1 --in regatta.yaml --out regatta.yaml.next
mv regatta.yaml.next regatta.yaml
```

Reverse migration is supported one minor version back. Older
revivals require restoring `regatta.yaml` from git history.

## Database

`regatta.db` migrations are applied at `regatta serve` startup.
The orchestrator refuses to start if the db schema is newer than
the binary expects (forward compatibility) or the binary is newer
than the db expects without a migration applied (backward
compatibility). Back up `regatta.db` before upgrading minor
versions.
