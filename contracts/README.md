# contracts/

Normative operator-facing surface. Everything operators read,
implement against, or sign against lives here.

## Layout

- `schemas/` - JSON Schema + CUE + Go interfaces backing the
  schemas. `*.schema.json` is the JSON Schema source of truth;
  `regatta.v1.cue` is the CUE source for `regatta.yaml`; Go files
  in this dir form Go package `schemas` and provide typed views
  + signing.
- `prompts/` - signed agent prompts loaded by the orchestrator
  + gate runners. Populated as MVP-1 -> MVP-3 land.
- `wire/` - wire-protocol docs for plugin authors (custom
  SpecAdapter, custom gate). Populated when first plugin seam ships.

## Versioning

Pre-v1.0: anything may break; CHANGELOG records every shape
change. After v1.0: deprecation cycle per PRINCIPLES #11 (warn one
minor, fail next). Promotion from `internal/` to `contracts/` is a
one-way ratchet; demoting later is a breaking change.

## Promotion policy

A move from `internal/` to `contracts/` requires an ADR
(`docs/rfcs/NNNN-<title>.md`). Closed-source posture: every
exported surface is a support burden + competitive surface;
promote only when an operator/plugin author needs it.
