# Regatta Style Guide

These are the rules. The reasoning behind them lives in
[`PRINCIPLES.md`](PRINCIPLES.md) - read that first if you want
the *why* and the lenses for situations no rule covers.

This guide is deliberately incomplete: regatta is pre-
implementation. Language-specific sections (Go layout, logging,
error wrapping, testing, build / release) get filled in as the
binary takes shape. The conventions below apply to docs and to
any code that lands.

## Comments earn their place

Default to writing none. Add one only when *removing* it would
confuse a future reader.

A comment must explain something the code cannot:

- A non-obvious *why* (a workaround for a specific bug, a
  constraint from outside, a subtle invariant).
- A trap a contributor (or agent) might walk into if they
  edited naively.

A comment must not:

- Restate the code.
- Reference a current task, ticket, or PR (those rot; use git
  blame).
- Claim "this is performance-critical" without a benchmark.
- Apologize for the code (fix it instead).

The same principle applies to documentation. See
[`PRINCIPLES.md`](PRINCIPLES.md) #7 for the underlying lens.

## Names earn their slot

A name should let a reader skip reading the body. If a function
is called `process()` and the caller has to read it to
understand what it processes, the name is wrong.

- No abbreviations except by language convention (`ctx`, `err`,
  `i`, `n`, `req`, `w` in Go).
- No prefix-style suffixes on type names (`UserStruct`,
  `LoggerInterface`). Drop the suffix.
- Package names are lowercase, single-word, no underscores. The
  package name is part of the call site (`gates.Run()`), so
  don't repeat it in symbols (`gates.GetGates()` is
  `gates.All()` or `gates.List()`).

The same discipline applies to schema field names. `GateResult`
fields are read by both humans and agents - they deserve API-
quality naming.

## Fast feedback over thorough feedback

A check that takes a second and runs every commit beats a check
that takes a minute and runs nightly.

- `make check` must complete in under 60 seconds on a developer
  laptop.
- Gate-stack latency is agent feedback latency. L3 / L4 / L5
  budget under 5 minutes per gate; the full stack stays under
  ~15 minutes per PR so the agent loop stays tight.
- Benchmarks are advisory, not gate-keeping. Slow CI burns
  trust faster than missing coverage.

## Commits

- **Atomic**: each commit must build (when there is a binary)
  and pass tests on its own.
- **Imperative present tense**: "Add canary corpus", not "Added
  canary corpus".
- **Subject 72 char cap**, body wraps at 72.
- **DCO sign-off required**: `git commit -s`.
- **One concern per commit**: don't bundle a refactor with a
  feature; don't slip a fix into a doc change.

Agent-authored commits follow the same rules; the review stack
treats agent and human commits identically. See
[`.claude/notes/commit-discipline.md`](.claude/notes/commit-discipline.md)
for the AI-attribution forward-only-compliance rule.

## Changelog

- `CHANGELOG.md` at repo root, [Keep a
  Changelog](https://keepachangelog.com/) format.
- Categories: `Added`, `Changed`, `Deprecated`, `Removed`,
  `Fixed`, `Security`.
- One entry per user-visible change, written for operators not
  committers.

## Documentation

- Root `README.md`: pitch, install, quickstart, link to docs.
- `STYLE.md` (this file): contributor conventions.
- `PRINCIPLES.md`: the why behind the rules.
- `AGENTS.md`: agent / contributor onboarding brief, load-
  bearing lessons, topic index.
- `docs/rfcs/NNNN-title.md`: one file per architectural
  decision; template at [`docs/rfcs/0000-template.md`](docs/rfcs/0000-template.md).

## Local + CI gates

`make check` (target in `Makefile`) is the single source of truth
for what is verified locally and in CI. Constituent gates:

- `doc-check` - markdown link integrity, banned-phrase lint,
  comment-noise diff.
- `prose-dup` - regression-seed list; fails if a previously
  collapsed prose duplicate reappears in 2+ markdown files.
- `vet`, `tidy-check`, `mod-verify`, `go-check` - Go correctness.

`make ci-check` extends `check` with the weekly `stale-todo` scan
(also runs on its own cron).

## Branch protection

`scripts/apply-branch-protection.sh` writes the required-check
list to `main` via gh api; it is idempotent and the source of
truth for what is required to land. `verify-repo-config` reads the
live state back. Change protection by editing
`.github/branch-protection.yml` (intent), updating both the apply
script (live state) and the verify expected values.

## When this guide is wrong

Open an RFC. Don't quietly diverge.
