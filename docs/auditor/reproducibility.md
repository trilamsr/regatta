# Reproducibility (build-level) vs. verdict tamper-evidence (gate-level)

Reader: customer security team under NDA verifying build
reproducibility and reading the gate-verdict audit posture.
Read time: 5 minutes.
Expires when: the build determinism guarantees (trimpath,
SOURCE_DATE_EPOCH, prompt-SHA pinning) or the gate-verdict payload
shape (issue #550) change.

## Two distinct properties — do not conflate

| Property | Scope | What replay proves |
|---|---|---|
| **Build reproducibility** | The `regatta` binary itself | Same source + toolchain ⇒ byte-identical binary. |
| **Verdict tamper-evidence** | One row in the gate-verdict journal | The recorded verdict was not mutated after sign. HMAC chain is intact. |
| **Verdict reproducibility** | A specific verdict | Re-running the gate's producer with the same input yields the same Pass/Reason. Holds ONLY when `verdict.deterministic = true`. |

Issue #550 reframe: gate verdicts are **tamper-evident** by default;
verdict-reproducibility is a per-gate property the producer declares.
Non-deterministic gates (LLM threat models, CVE scanners against a
moving DB) are **non-replayable by construction** — re-running them
later can legitimately produce a different verdict even when nothing
was tampered with.

## Build reproducibility

Two builds of `regatta` at the same source SHA must produce byte-
identical binaries. Mechanisms:

- `-trimpath` always; binary contains no local filesystem paths.
- `SOURCE_DATE_EPOCH` honoured; falls back to git commit time,
  never to `time.Now()`.
- Single Go module; no submodules; pinned `go.mod` + `go.sum`
  hash-verified in CI.
- `make ci` is the single source of truth for what is verified
  before release.

## Determinism inside the orchestrator

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

## Prompts

- Agent prompts live in `contracts/prompts/` (populated MVP-1+).
- Each prompt has a stable SHA pinned in `regatta.yaml`; mismatch
  fails closed at runtime.
- Edits go through the same gate stack as code (prompt-as-code).

## Verdict audit posture (`regatta audit verify`)

Every `gate_verdict` event journals four metadata fields that
collapse the reproducibility question to a single boolean:

| Field | Purpose |
|---|---|
| `tool` | Names the producer: `cel`, `gitleaks`, `osv-scanner`, `opa`, `anthropic-api`, etc. |
| `tv` (tool_version) | Pins the producer version: git-SHA, scanner tag, model id + snapshot. |
| `db_v` | `state.CurrentSchemaVersion` at write time. Lets `audit verify` flag schema-skew. |
| `det` (deterministic) | `true` ⇒ posture `reproduce`. `false` ⇒ posture `verify-only`. |

`regatta audit verify --run-id <id>` walks every gate_verdict for the
run and emits one row per gate with the (tool, version, deterministic,
hmac-status, schema-skew) tuple. The HMAC chain is checked
universally; the `deterministic` flag is informational and tells the
auditor whether a re-run would even be expected to agree.

### Posture semantics

- `reproduce` — `verdict.deterministic = true`. Compliance tooling
  MAY re-execute the gate and bit-for-bit compare against the
  recorded verdict. Disagreement is a bug.
- `verify-only` — `verdict.deterministic = false`. The HMAC chain
  proves the recorded verdict was not mutated, but the gate's
  producer is non-deterministic (LLM, scanner DB, wall-clock-bound).
  Re-execution disagreement is NOT evidence of tampering.

### Legacy rows

Verdicts written before issue #550 lack the new fields. `audit
verify` backfills these as `tool=unknown-legacy`,
`tool_version=unknown-legacy`, `deterministic=false`. They surface in
the `verify-only` bucket so auditors see them flagged distinctly from
properly-tagged non-deterministic verdicts.

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

For verdict-level audit (NOT build-level):

```sh
export REGATTA_AUDIT_HMAC_KEY=<32-byte hex>
export REGATTA_AUDIT_HMAC_KEY_ID=<key id>
regatta audit verify --run-id <run-id> --db regatta.db --format json
```
