# Day 1 - install and validate

Reader: customer-operator on the first day with Regatta.
Read time: 10 minutes.
Expires when: command surface for `init` or `verify-repo-config` changes.

## Goal

See a worked example of what L0 catches (via `regatta init`'s demo
fixture) and a green `verify-repo-config` audit on your repo.

## Steps

```sh
brew install trilamsr/regatta/regatta   # or `go install ...`
cd ~/code/myproject
regatta init                            # writes config + runs demo
regatta verify-repo-config              # audits branch protection + CODEOWNERS
```

## What verify-repo-config catches

Several silent-bypass classes that GitHub does not surface itself
would otherwise defeat L6 in production:

- Admins bypass branch protection by default.
- CODEOWNERS patterns matching nothing.
- SKIPPED required checks satisfy branch protection.
- `required_approving_review_count: 0` is a no-op.
- CODEOWNERS files >3 MB silently ignored.
- Missing `require_last_push_approval` enables PR hijacking
  (Mercari-class).

`verify-repo-config` checks the P2 canonical recipe
(`required_approving_review_count: 2`, `require_code_owner_reviews`,
`require_last_push_approval`, `dismiss_stale_reviews`,
`enforce_admins: true`, Regatta-critical paths routed to two
disjoint CODEOWNERS teams) and refuses to start if any check fails
unless `--accept-degraded` is passed (the gap is logged to the
audit sink either way).

## Next

- [day7.md](day7.md): orchestrator on, one lane.
- [configure.md](configure.md): every `regatta.yaml` field.
