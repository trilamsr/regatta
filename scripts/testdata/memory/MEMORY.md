# Test fixture — memory index (NOT operator memory)

This tree exists so `scripts/check-memory-citations.sh` has a CI-portable
default to resolve `feedback_<slug>` citations against on machines without
the operator's `~/.claude/projects/.../memory/` directory.

Every sibling `feedback_*.md` is a 1-line stub. Authoritative rules live in
`CLAUDE.md`, `docs/engineer/autonomous-session-prompt.md`, and
`docs/engineer/dispatch-templates/*.md`.

Operators override the default via `MEMORY_DIR=…` to point the gate at
their real per-machine memory dir.
