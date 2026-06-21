# Session hygiene

Lessons about isolating session work via worktrees, classifier
quirks, and background-job state. Newest-first.

### Pre-create the worktree before dispatching a file-editing background job

This is the launcher-side prevention that complements the
recovery entry below. A file-editing background job dispatched
from the main session does NOT auto-provision a worktree — it
lands cwd-pinned on `main` (read-only). If it improvises instead
of stopping, edits hit the primary tree; several such jobs on the
same package collide (`X redeclared`, duplicate methods) with no
isolation to catch it.

Prevention: the launcher runs `git worktree add
.claude/worktrees/<slug> -b <branch> origin/main` BEFORE spawning
the job, and the job's first instruction is `EnterWorktree(path:
".claude/worktrees/<slug>")` — a cwd-pinned job may switch into an
existing registered worktree by path, even though it cannot create
one. Note the dispatch template at
`docs/engineer/dispatch-templates/implementer.md` line 45 ("You
are ALREADY inside the harness-provided worktree") holds only for
orchestrator-spawned agents; a main-session-launched job has no
such guarantee, so pre-creation is the launcher's job.

Anchor: `docs/engineer/dispatch-templates/implementer.md` line 45;
regression shows as `git status` dirtying the primary checkout
while a background edit job runs.

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
