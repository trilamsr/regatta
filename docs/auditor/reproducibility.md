# Reproducibility

Reader: customer security team under NDA verifying build
reproducibility and gate-verdict determinism.
Read time: 5 minutes.
Expires when: release pipeline (Wave 3) lands signed-tag +
provenance attestation.

## Property

Two runs of the same agent against the same work item, with the
same prompt SHA and the same target-repo SHA, must produce gate-
stack verdicts that differ only by genuine non-determinism in the
agent itself - and that non-determinism is visible in the audit
log. Reproducibility breakage is a P0 bug.

(Authoritative statement: [`PRINCIPLES.md` #12](../../PRINCIPLES.md).)

## Mechanisms

### Build

- `-trimpath` always; binary contains no local filesystem paths.
- `SOURCE_DATE_EPOCH` honoured; falls back to git commit time,
  never to `time.Now()`.
- Single Go module; no submodules; pinned `go.mod` + `go.sum`
  hash-verified in CI.
- `make ci` is the single source of truth for what is verified
  before release.

### Prompts

- Agent prompts live in `contracts/prompts/` (populated MVP-1+).
- Each prompt has a stable SHA pinned in `regatta.yaml`; mismatch
  fails closed at runtime.
- Edits go through the same gate stack as code (prompt-as-code).

### Determinism in code

- Time access via injected `Clock`; never `time.Now()` in business
  logic.
- Random via injected `rand.Source`; never package-global.
- Map serialization sorts keys.
- Filesystem walks sorted by name.

### Audit-log visibility

- Genuine non-determinism (LLM sampling) is logged with the
  sampler seed + temperature.
- The audit log is tamper-evident (see [audit-log.md](audit-log.md))
  so a reproduction can be compared bit-for-bit against the
  original verdict trail.

## What lands at Wave 3 release pipeline

- Signed git tag for every release.
- Provenance attestation file (SLSA-flavored) bundled with the
  binary.
- Customer-audit teams can request the attestation via the
  channel in [`SECURITY.md`](../../SECURITY.md).

## Customer verification recipe

```sh
git clone https://github.com/trilamsr/regatta
git checkout <tag>
make ci
SOURCE_DATE_EPOCH=$(git log -1 --format=%ct) go build -trimpath -o regatta-local ./cmd/regatta
sha256sum regatta-local regatta-released
```

The two sha256 hashes match when build environments agree on Go
toolchain version (`.go-version` pins minor) and OS/arch.
