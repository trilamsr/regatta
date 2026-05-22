# How to add a SpecAdapter

Reader: internal engineer adding a built-in spec-source adapter.
Read time: 10 minutes.
Expires when: `SpecAdapter` Go interface in
`contracts/schemas/spec_adapter.go` changes shape.

## Decision: built-in vs custom

| Kind | Lives at | Wired via | When |
|---|---|---|---|
| Built-in | `internal/orchestrator/adapter/<name>/` | Hard-coded in the orchestrator | Adapter is universal (`github_issues`, `markdown_catalog` today). |
| Custom | Customer-supplied binary on PATH | `regatta.yaml spec_adapter.type: custom` + `command:` | Customer-specific or proprietary spec source. |

If the adapter is universal, write an ADR under `docs/rfcs/`
before implementing. The adapter surface is operator-visible (the
adapter name shows up in `regatta init` output and CUE schema).

## Tier discipline

Adapters split into two tiers based on what the platform's API
exposes:

- **First-class:** immutable-snapshot retrieval (ETag for GitHub,
  commit SHA for markdown), audited edit history,
  atomic state transitions. L0 enforces byte-equality.
- **Degraded-mode:** signals THAT a description changed (e.g.
  `IssueHistory.updatedDescription` flag on Linear) but cannot
  return the prior body text. L0 falls back to "detect mutation,
  halt agent, file clarification item."

`regatta init` prints the selected adapter's tier and refuses to
advance without explicit acknowledgement when the tier is
degraded.

## Built-in adapter skeleton

1. Pick the adapter's id (`<name>`, lowercase, underscore-allowed).
   Must match a discriminator value in
   [`contracts/schemas/regatta.v1.cue`](../../contracts/schemas/regatta.v1.cue)
   `#SpecAdapter.type`.
2. Create `internal/orchestrator/adapter/<name>/<name>.go`. Mirror
   `internal/orchestrator/adapter/markdown.go` for the
   `markdown_catalog` shape.
3. Implement the `SpecAdapter` interface from
   [`contracts/schemas/spec_adapter.go`](../../contracts/schemas/spec_adapter.go).
   Document the tier in the package doc-comment.
4. For each `WorkItem` returned, fill the load-bearing fields per
   `docs/design.md` §Spec contract: `acceptance_criteria[].text`
   (immutable post-publication), `dependencies` (DAG), `source`
   (immutable pointer L0 uses).
5. Write a fixture corpus + table-driven test mirroring
   `internal/orchestrator/adapter/markdown_test.go`.
6. Update the CUE schema's `#SpecAdapter` per-type clause with the
   new fields the adapter needs (selector, project, jql, team,
   etc.).
7. Wire the adapter into the orchestrator's adapter registry.

## Custom adapter wire protocol

Customer-supplied binaries speak JSON-over-stdio per
`contracts/wire/spec_adapter_jsonio.md` (activation trigger: first
reference custom-adapter impl under `plugins/adapters/`).

## Verifying

```sh
regatta validate-spec --dry-run --adapter <name>
```

Lists items, NFC + invisible-glyph cleanliness, DAG verification.
This is the only smoke test until your fixture corpus is wired.

## Promotion to contracts/

The Go `SpecAdapter` interface already lives at
`contracts/schemas/spec_adapter.go`. Built-in adapter impls live at
`internal/orchestrator/adapter/` and are not themselves promoted -
the interface is the contract surface.
