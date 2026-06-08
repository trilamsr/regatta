# Implementer dispatch template (bundled default)

Code-writing subagent. Used when the target repo carries no
implementer dispatch template of its own.

## Comments: zero by default

Write no comments unless removing the comment would leave a future
reader confused about WHY. A clear name plus signature plus types
document the WHAT. Hard rules a reviewer rejects on:

- No restating the symbol name in its own godoc or docstring.
- No restating the signature.
- No section banners.
- No multi-paragraph implementation narration.
- No untagged TODO / FIXME / XXX / HACK. Cite an issue on the same
  line or omit.
- No commented-out code blocks.

## TDD

Failing test first. Capture the failing output in the pull-request
body. Then implement. Then green. Order matters; the commit log must
show the failing test landed first.

## Adversarial review

After green, ask an independent reviewer (subagent or human) to read
the diff. Address every Risk-tier-or-higher finding inline or file a
tracking issue and cite it in the pull-request body.

Skip review when proportional: dependency bumps with continuous
integration green and fewer than twenty lines changed; pull requests
that touch only documentation, configuration, or scripts.

## No signatures

Do not add `Co-Authored-By:`, AI footers, or "Generated with" tags
anywhere in commits, pull request bodies, or code.

## Release notes

Every pull-request body must include a fenced release-notes block:

```
```release-notes
<type>: <one-line user-visible change OR "none (internal)">
```
```

## Pull request body hygiene

Pass the body through `--body-file <path>` when invoking the GitHub
CLI. Inline HEREDOC bodies silently break the release-notes fence.

## Worktree discipline

You are already inside the harness-provided worktree. Confirm with
`pwd`, `git branch --show-current`, and `git remote -v` before
editing. Never run `git clone` or `git worktree add` from inside the
subagent.

## Definition of done

- Worktree branch, not the primary checkout.
- Failing test landed first; commit log shows it.
- Local test command runs green.
- Reviewer cleared, or auto-skip condition documented.
- Release-notes fence present in the pull-request body.
- No AI signatures anywhere.
