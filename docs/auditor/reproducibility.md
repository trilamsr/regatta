# Reproducibility

Reader: customer security team under NDA verifying build
reproducibility and gate-verdict determinism.
Read time: 5 minutes.
Expires when: the build determinism guarantees in this file
(trimpath, SOURCE_DATE_EPOCH, prompt-SHA pinning) change shape.

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

### Engine-version journaling

- Every `ProgramBrief` carries `engine_version` (git-SHA of the
  binary that produced it) and `engine_build_dirty` (true when the
  source tree had uncommitted changes at build time).
- `regatta program show <brief.json>` surfaces both fields so an
  auditor can inspect which engine produced a months-old program
  without parsing JSON by hand.
- `regatta program replay-skew-check <brief.json>` compares the
  brief's record-time engine to the current binary. Default mode is
  WARN (tag the digest, keep the loop unblocked); pass `--strict`
  for audit-grade runs that must fail closed on any divergence
  (different SHA, missing SHA on either side, or either build dirty).
- A brief whose `engine_version` is `unknown` (built with
  `-buildvcs=false` and no `-ldflags` pin) is treated as skew even
  against another `unknown` binary — refusal beats silent green.

### Audit-log visibility

- Genuine non-determinism (LLM sampling) is logged with the
  sampler seed + temperature.
- The audit log is tamper-evident (see [audit-log.md](audit-log.md))
  so a reproduction can be compared bit-for-bit against the
  original verdict trail.

## Release attestation

- Every release tag is SSH-signed; GitHub's API verification gates
  the release workflow (`.github/workflows/release.yml`).
- The release binary ships with a SLSA build-provenance attestation
  generated via `actions/attest-build-provenance` and verifiable
  with `gh attestation verify`.
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
