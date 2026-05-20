# Session hygiene

Lessons about isolating session work via worktrees, classifier
quirks, and background-job state. Newest-first.

### Call `EnterWorktree` before the first edit in a background job

Background jobs running against the user's primary checkout get
bounced by a tool-level guard with the message "This background
session hasn't isolated its changes yet"; the recovery is to
use `EnterWorktree` so edits land in `.claude/worktrees/<name>`
instead. Run it before the first `Edit` or `Write`, not after
the first failure.

For regatta this discipline is doubly load-bearing: the
orchestrator's whole architecture is "spawn an agent in an
isolated worktree per work item." An agent that edits the user's
main checkout instead of its assigned worktree has violated the
isolation contract before its first commit. The worktree
contract is enforced at the tool level here; in regatta it will
be enforced at the orchestrator level.

Anchor: tool error substring `isolated its changes`;
`git worktree list` shows the `.claude/worktrees/` path while
active.
