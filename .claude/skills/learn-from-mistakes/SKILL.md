---
name: learn-from-mistakes
description: Capture and surface repo-resident lessons in regatta. Use when the user says "/learn this", "remember that", "add a lesson about X", or any phrasing that asks to record a lesson — and self-activate when a correction pattern appears (user pushback "no/don't/revert/stop", an in-session rollback, the same test failing twice for related reasons, or rediscovering an answer previously ruled out).
---

# learn-from-mistakes

Regatta-scoped capture and read flow for the lessons stored in
`AGENTS.md` (brief), `` (repo-wide deep notes),
and `.claude/notes/<topic>.md` (agent-internal deep notes).

## When to activate

**Explicit triggers.** The user says any of:

- `/learn this`, `/learn <thing>`
- "remember that", "remember this for next time"
- "add a lesson about X", "add a note about X"
- "record this in AGENTS.md" or "record this in the notes"

**Friction-detection triggers.** Self-activate (silently consider, then
offer a draft) when you observe any of:

- The user pushes back: `no`, `don't`, `stop`, `revert`, `undo that`.
- You roll back a change within the same session.
- A test fails twice consecutively for related reasons.
- You re-discover an answer you previously ruled out.

On a friction trigger, **draft a candidate entry and surface it**. Do
not write. The user accepts, edits, or rejects.

False negatives are fine. False positives train the user to dismiss
prompts — so be conservative.

## Read flow (every session)

`AGENTS.md` is auto-loaded by the standard agent convention. When
session work touches a topic listed in either topic index, read the
corresponding `` or `.claude/notes/<topic>.md`
*before* taking the first action in that area.

**Verify before acting on a cited lesson.** If a lesson cites a file +
line, test name, flag, or command, confirm it still exists/applies in
the current tree before following the lesson. If stale, propose an
update or removal via the capture flow.

## Capture flow

For every new or updated entry:

### 1. Draft

Compose:

- **Title.** Short imperative case (`Run make ci before commit`, not
 `Running make ci`).
- **Body.** 1–3 sentences. What happened / why it matters / what to do.
- **Anchor.** File path + line, test name, command, or grep query that
 would catch regression if removed. Required.

### 2. Pick destination

- **Load-bearing.** The lesson belongs in every session's prompt.
 Destination: `AGENTS.md` under `## Load-bearing lessons`. Promotion
 into this section requires the user to explicitly say "load-bearing"
 or equivalent — default to a topic note.
- **Topic note — repo-wide.** A lesson a human contributor would care
 about (CI, code style, PR workflow, reproducibility, branch
 protection, conftest, code review). Destination:
 ``. If the topic file does not exist, create
 it (see template in ``) and add one index line
 to `AGENTS.md` under `## Topic index — repo-wide`.
- **Topic note — agent-internal.** A lesson a human contributor would
 not encounter: slash-command side effects, classifier behavior,
 durable agent memory, skill authoring, multi-agent review patterns,
 background-job session hygiene. Destination:
 `.claude/notes/<topic>.md`. If the topic file does not exist,
 create it (format mirrors ``; see
 `.claude/notes/README.md`) and add one index line to `AGENTS.md`
 under `## Topic index — agent-internal`.

### 3. Run the format check

Reject the draft if any of these is true:

- **Banned vocabulary.** The draft contains any of (case-insensitive
 substring): `ralph`, `Loop N`, `Pass N`, `four-loop`, `subagent`,
 `reviewer agent`, `loop design`, `loop prompt`.
- **First-person AI phrasing.** The draft contains any of (case-
 insensitive substring): `as an AI`, `the model`, `the session`, `we
 (AI)`, `I (the assistant)`.
- **AI attribution.** The draft contains any of: `Assisted-by:`,
 `Co-Authored-By: Claude`.
- **Missing anchor.** No `Anchor:` line in the entry body.
- **Brief-cap violation.** Destination is `AGENTS.md` and the resulting
 file would exceed 200 lines.

On any failure: surface the offending line(s), explain which check
failed, and ask the user to revise. Do not proceed to step 4 until the
draft is clean.

On brief-cap failure specifically: propose demoting the oldest
non-load-bearing entry to a topic note as a separate capture flow before
re-attempting the addition.

### 4. Show the diff

Present the proposed change as a unified diff in the chat. **Do not
write the file yet.** Be explicit that nothing has been written.

### 5. Wait for the user

Accept / edit / reject.

- Accept → step 6.
- Edit → integrate the user's edits, then re-run the format check.
- Reject → drop the draft. No commit.

### 6. Commit on accept

Write the file(s). Stage and commit:

- Subject style: `[docs] <imperative title>` for `AGENTS.md`,
 `*`, `.claude/notes/*`, and changes to this skill
 itself. Match the area
 prefix style observed in recent `git log`.
- DCO trailer: include `Signed-off-by: <Name> <email>`. Use
 `git commit -s`.
- **No AI attribution trailers.** No `Assisted-by:`. No
 `Co-Authored-By:` for AI.
- Body optional. If present, plain contributor prose; no banned
 workflow vocabulary; no first-person AI phrasing.

## Curation flow (stale entries)

When you notice a stale entry during session work:

1. Open the capture flow with the change being a remove or rewrite.
2. The anchor for the curation is the evidence of staleness (commit
 hash, current file content, test that now fails differently).
3. Same diff → user-approval → commit cycle.

No automatic pruning. Entries only leave a file via a user-approved
diff.

## Pointers

- Brief: `AGENTS.md` at repo root.
- Repo-wide topic notes (human + agent): ``.
- Agent-internal topic notes: `.claude/notes/<topic>.md`.
- Format reminder: `` (mirrored by
 `.claude/notes/README.md`).
- This skill: `.claude/skills/learn-from-mistakes/SKILL.md`.

No external scripts. No plugin install. The format check is a set of
substring searches and a `wc -l` you perform inline before opening the
diff.
