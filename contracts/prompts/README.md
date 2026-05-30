# contracts/prompts/

Signed agent prompts loaded by the orchestrator + gate runners.

## Status

- `AGENT_BRIEF.md` -- worker-agent runtime contract (P3 trusted
  instructions from `main` only). SHA pinned via
  `regatta.yaml prompts.agent_brief_sha`.
- `planner.md` -- one-shot program planner. Embedded in
  `internal/program/provider_anthropic.go` as the build-hermetic
  fallback; MVP-1 wires loading-from-disk + SHA verification at
  runtime.
- `security_gate.md` -- not yet written; MVP-3 activation.

## Discipline

- Each prompt has a stable SHA pinned in `regatta.yaml`. Mismatch
  fails closed at runtime.
- Edits go through the same gate stack as code. Prompt-as-code.
- Never log full prompt bodies; cite the path + SHA.
