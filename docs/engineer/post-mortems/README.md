# docs/engineer/post-mortems/

One file per post-incident write-up. Each file is the source of
truth that PRINCIPLES.md cites when a rule names a specific
incident as its origin.

## Activation trigger

First production incident or near-miss whose resolution introduces
or hardens a load-bearing rule. Until then the directory is empty
by design - inventing fictional incidents to justify a rule is the
opposite of what the post-mortem corpus is for.

## File naming

`YYYY-MM-DD-<short-slug>.md`. Slug is the visible failure mode,
not the root cause (the file body covers root cause). Date is the
detection date, not the patch date.
