# How to add a gate

Reader: internal engineer adding a custom or built-in gate.
Read time: 10 minutes.
Expires when: gate-runner package layout under `internal/gates/`
changes.

## Decision: built-in vs custom

| Kind | Lives at | Wired via | When |
|---|---|---|---|
| Built-in | `internal/gates/<name>/` | Hard-coded in the orchestrator gate runner | Gate is universal across customers (L0 shipped; L1-L5 deferred; L6 is branch protection, configured not coded). |
| Custom (deterministic) | Customer-supplied binary | `regatta.yaml gates:` row with `type: deterministic` + `command:` | Customer-specific policy (license audit, i18n completeness). |
| Custom (AI) | `regatta.yaml gates:` row with `type: ai` + `model:` + prompt | `contracts/prompts/<gate-id>.md` (signed) | Customer-specific judicial check. |

If the gate is universal but stage-deferred, file an RFC under
`docs/rfcs/` rather than implementing immediately. ADRs precede
new contract surfaces.

## Built-in gate skeleton

1. Pick the gate's id (`<name>`, lowercase, hyphen-allowed). The id
   appears in `GateResult.gate_id`.
2. Create `internal/gates/<name>/gate.go` with the package name
   matching `<name>`. Mirror the shape of `internal/gates/l0/gate.go`:
   - `Input` struct: bounded fields the runner passes in.
   - `Run(ctx, in) (schemas.GateResult, error)`.
3. Emit a `GateResult` per [`contracts/schemas/gate_result.schema.json`](../../contracts/schemas/gate_result.schema.json).
   Severity comes from the deterministic check or AI verdict; do
   not invent new severity tiers.
4. Add a fixture corpus at `testdata/gates/<name>/` per spec P6:
   - `pass/<NN>_<slug>.{diff,input,expected.json}` shape.
   - `fail/<NN>_<slug>.{...}`.
   - `edge/<NN>_<slug>.{...}` with sibling `<slug>.expected.json`
     specifying the expected verdict.
5. Add a table-driven contract test at
   `internal/gates/<name>/fixture_test.go` that sweeps the corpus
   and asserts each fixture's expected verdict. Mirror
   `internal/gates/l0/fixture_test.go`.
6. Wire the runner into the orchestrator gate-runner registry (the
   wiring point lands when L3-L5 ship; until then, `cmd/regatta`
   contains the per-gate subcommand entrypoints).

## Custom gate wire protocol

Customer-supplied binaries speak JSON-over-stdio. The wire format
will live at `contracts/wire/custom_gate_jsonio.md` (activation
trigger: first reference custom-gate impl under `plugins/gates/`).

## Naming + style

- Package name = directory name = gate id. No `gate` suffix on
  function names (the package name already says it).
- `Run` is the single entrypoint; helpers stay unexported.
- Verb-noun for function names (`Sign`, `Validate`, `Spawn`).
- Comments answer WHY, not WHAT (spec C5).
- Comment a doc-comment on exported symbols only when name +
  signature do not already convey the meaning (spec C6).

## Tests are first-class

- Contract via fixture corpus, not against impl internals (spec
  C3).
- No mocking of contracts (spec C11). Run against real
  implementations + fixture corpora.
- `t.Fatalf` on missing fixture dir (not `t.Skipf`) so path drift
  fails closed - see `internal/gates/l0/fixture_test.go` for the
  pattern.

## Promotion to contracts/

If the gate introduces a new contract surface (a Go interface
operators implement, a wire-protocol doc, a versioned schema), the
promotion is one-way; an ADR under `docs/rfcs/` is required
(spec P4). Once landed, follow the deprecation cycle in PRINCIPLES
#11.
