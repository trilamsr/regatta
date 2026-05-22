# Contributing

Internal engineer onboarding. Closed-source repo; this file is the
human peer to `AGENTS.md` (which serves AI agents).

Read first: `AGENTS.md`, `STYLE.md`, `PRINCIPLES.md`,
`ARCHITECTURE.md`. The rest of this file is the delta.

## Setup

```bash
make install-hooks   # commit-msg + DCO + (Wave 3) more
make check           # local gate; under 60 seconds
```

## Branching

`<type>/<short-slug>` (feat/foo, fix/bar, refactor/baz, docs/qux).
Max lifetime ~5 working days; rebase or ship past that.

## Commits

Conventional Commits + DCO. The commit-msg hook validates the
subject; the prepare-commit-msg hook auto-appends Signed-off-by.
Types: `feat fix docs style refactor perf test chore build security`.
Subject under 50 chars, imperative present. Body explains WHY, not
WHAT.

## PRs

PR body needs: 1-line What + release-notes fenced block (category
prefix). For bugfixes, also a Root-Cause line (Wave 1 pr-lint
workflow enforces). Self-merge after gates green; branch
protection blocks force-push and skip-hooks.
