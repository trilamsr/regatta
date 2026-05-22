# contracts/prompts/

Signed agent prompts loaded by the orchestrator + gate runners.

## Status

Empty until MVP-1 lands. Activation trigger: first `prompts/*.md`
referenced by code (`internal/program/planner.go` will load
`planner.md`; `internal/securitygate` will load
`security_gate.md`).

## Discipline

- Each prompt has a stable SHA pinned in `regatta.yaml`. Mismatch
  fails closed at runtime.
- Edits go through the same gate stack as code. Prompt-as-code.
- Never log full prompt bodies; cite the path + SHA.
