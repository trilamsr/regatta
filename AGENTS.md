# Regatta agent and contributor brief

Repo-resident notes for anyone working in this codebase. Regatta
is itself built primarily by autonomous agents, so this file
doubles as the agent-onboarding surface. Reads top-to-bottom in
under a minute. Three sections: a small list of universal
load-bearing lessons, an index of repo-wide topic notes, and an
index of agent-internal topic notes.

## Load-bearing lessons

Universal rules every change should respect. The file is capped at
200 lines. Promotion into this section is explicit - most lessons
belong in a topic note instead.

- **Every PR description needs a fenced `release-notes` block with a
  category prefix.** The `pr-lint` workflow scans the PR body for a
  ` ```release-notes ` block and fails if it is missing, empty, or
  the unedited template. Category must be one of `[FEATURE]`,
  `[ENHANCEMENT]`, `[BUGFIX]`, `[PERF]`, `[SECURITY]`, `[CHANGE]`,
  or write `NONE` for changes with no user-visible effect. Anchor:
  `.github/PULL_REQUEST_TEMPLATE.md` "Release notes" section.

- **`make check` is the single source of truth for what is verified
  locally and in CI.** Run it before declaring a change complete or
  opening a PR. Today `check` aliases `doc-check`; as gates land
  (Go lint, gate-stack tests, schema validation) they get folded
  in here, not into ad-hoc scripts. Anchor: `Makefile` target
  `check`.

- **Forward-only compliance resolves rule-tightening collisions.**
  When a repo-resident rule tightens mid-flight, history rewrites
  stay banned. Existing commits and PRs grandfather; new commits
  forward-comply. Apply this resolution to any future rule update -
  do not retroactively rewrite to chase a new standard.

- **Ceremony without a falsifiable consumer is bloat -
  `grep -c '<name>' <file> == 0` is the test.** A discipline, gate,
  template, or check that no downstream code, test, or workflow
  consumes is removable. Before adding a process artifact, name the
  consumer that would notice if it were missing.

- **Verify named identifiers exist before echoing them as fact in
  repo docs.** GitHub teams, mailboxes, domains, file paths, and
  RFC numbers cited in governance docs are assertions the named
  thing exists. A `CODEOWNERS` rule pointing at a non-existent team
  is silently ignored; a `SECURITY.md` mailbox that bounces is a
  policy violation surfaced months later. A 30-second existence
  check before landing the reference is the cure. Anchor:
  `gh api /orgs/<org>/teams/<slug>` returns 200 for real teams;
  `dig MX <domain>` for mailboxes; `[ -f path ]` for file refs.

- **RFC commitments must be self-falsifying - every "X gate enforces
  Y" line points at a real gate or labels itself deferred.** Every
  sentence in an RFC body that asserts a CI gate, lint rule, gate-
  stack check, or test exists must be verifiable in the current
  tree. Before drafting "X enforces Y," grep + inspect the workflow -
  if the gate isn't fail-closed today, either land it in the same
  PR or reword as "tracked in FOLLOWUPS / becomes load-bearing at
  <milestone>."

- **Aggregate CI gates with `if: always()` and explicit
  `needs.*.result` checks; default semantics silently bypass branch
  protection.** GitHub short-circuits an aggregator job's `needs:`
  to SKIPPED on any sub-job failure, and treats SKIPPED required
  checks as satisfied. Fix shape: aggregator runs `if: always()`,
  evaluates each `needs.<job>.result`, and exits non-zero when any
  is not `"success"`. Applies the moment regatta adds a multi-job
  CI workflow.

- **Agent-authored commits need the same discipline as human ones.**
  Atomic, imperative-present subject, body that explains the why
  not the what, DCO sign-off. An agent that bundles a refactor with
  a feature in one commit is a contributor that needs feedback, not
  a sub-process whose output is privileged. The review stack treats
  agent and human commits identically.

## Topic index - repo-wide

Per-topic notes that apply to anyone working in this codebase.
Read the relevant note when work touches that area.

- [Style](STYLE.md) - comments earn their place, names earn their
  slot, fast-feedback bias, commit / changelog conventions.
- [Principles](PRINCIPLES.md) - the why behind the rules and the
  lenses for situations no rule covers.

## Topic index - agent-internal

Per-topic notes specific to AI-agent workflows. Human contributors
can skip these.

- [Tooling](.claude/notes/tooling.md) - pinned plugins
  (superpowers, ralph-loop, claude-mem, caveman) and MCP servers
  (context7, github, fetch) wrapped by `caveman-shrink`; required
  env vars.
- [Review patterns](.claude/notes/review-patterns.md) - self-rating
  cycle (B+ to A to A+), adversarial pass after graded reviewers
  converge, pre-loading prior-round findings, mutation-verify
  falsifier tests.
- [Session hygiene](.claude/notes/session-hygiene.md) -
  `EnterWorktree` before the first edit in a background job.
- [Automation](.claude/notes/automation.md) - hook-matcher
  word-boundary regex, backtick-in-heredoc trap, slash-command
  side-effect forensics.
- [Commit discipline](.claude/notes/commit-discipline.md) - atomic
  commits, imperative subject, "why not what" body, DCO sign-off,
  AI attribution under forward-only compliance.

<!-- Add one line per topic note. Newest entries go to the matching
     note's top, not here; this index is just pointers. -->
