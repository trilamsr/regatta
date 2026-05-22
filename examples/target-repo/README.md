# examples/target-repo

Toy target repository for end-to-end smoke tests of the Regatta
agent loop against a real-but-small workspace.

## Status

Stub. Populated when Wave 2 quickstart e2e smoke lands. Until then,
the operator quickstart's "spawn an agent against a target repo"
step uses the operator's own pilot repository.

## Activation trigger

`docs/operator/quickstart.md` references a runnable smoke test that
spawns an agent against this directory. When that test lands, this
directory gains a small set of fixture files + a regatta.yaml that
points the agent at the contained spec source.
