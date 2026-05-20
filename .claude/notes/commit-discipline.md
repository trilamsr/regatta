# Commit discipline

Lessons about writing commits an agent or human reads six months
later without context. Newest-first.

### Atomic commits, imperative subject, why-not-what body

The shape, in priority order:

- **Atomic**: one concern per commit. A refactor + a feature
  bundled together is two reverts away from broken.
- **Imperative present tense subject**: "Add canary archetype",
  not "Added canary archetype" and not "Adding canary
  archetype". The convention reads naturally as "if applied,
  this commit will...".
- **Subject 72 char cap, body wraps at 72.**
- **Body explains the why, not the what.** The diff already
  shows the what; future-you wants to know *why* a change was
  necessary, what alternatives were considered, what
  constraint forced this shape.
- **DCO sign-off required**: `git commit -s`.

Subject prefixes (loose convention, not enforced):
`[feature]`, `[fix]`, `[chore]`, `[docs]`, `[refactor]`,
`[research]`, `[rfc]`. Pick one that matches the dominant
change.

### Agent-authored commits follow the same rules

Regatta is built primarily by autonomous agents. The review
stack treats agent and human commits identically: same atomic
discipline, same subject style, same body shape. An agent that
bundles a refactor with a feature in one commit is a
contributor that needs feedback, not a sub-process whose
output is privileged.

When an agent authors a commit, it should still co-author with
the human operator who initiated the session - the human owns
the merge decision and the responsibility trail.

### Forward-only compliance on rule-tightening

When a commit-discipline rule tightens mid-flight (e.g., new
AI-attribution trailer required, or an existing one retired),
history rewrites stay banned. Existing commits and PRs
grandfather; new commits forward-comply.

Resolution lens: the moment the rule lands, every *future*
commit follows it. Every *past* commit is frozen as-is. This
holds even when the past commits would look "wrong" under the
new rule. Rewriting history to chase a new standard is a
worse failure mode than the visible drift.
