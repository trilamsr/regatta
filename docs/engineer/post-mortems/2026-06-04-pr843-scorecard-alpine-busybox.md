# 2026-06-04 — PR #843 scorecard cited `alpine:3.20`, artifact uses `busybox:1.37`

Detected: 2026-06-04, after PR #843 had already merged. Tracking issue: #860.

## Failure mode

PR #843 shipped a chown-only init sidecar for the docker-compose stack. The A+ scorecard row claimed the sidecar base image was pinned to `alpine:3.20`. The merged artifact at `docker-compose.yml:29` pins `busybox:1.37`. The merged scorecard evidence row therefore does not match the merged code.

## What was actually used and why

The init sidecar runs `chown` once at container start. `busybox:1.37` carries `chown` in a roughly 4 MB image; `alpine:3.20` is roughly 7 MB and pulls in apk plus a package index that the sidecar never invokes. The smaller image was the correct call for a single-syscall init container — the scorecard row was authored from the spec draft (which had named `alpine:3.20`) and not refreshed against the final commit.

## Why the gate did not catch it

`scripts/check-scorecard.sh` validates that every `[x]` row cites a token shaped like `Test*` / `path.ext:NN` / `#NNN` / `N/A — …`. The PR #843 row carried a citation token of the right shape, so the regex passed. The gate validates citation shape, not citation truth — it cannot diff a claimed image tag against the actual `FROM` / `image:` line in the merged diff. Token-shape conformance is necessary but not sufficient.

## Lesson

Scorecard rows must cite the actual artifact token present in the diff at the time the body is written, not the token the spec drafted or the token the author remembered. When the spec and the implementation diverge mid-PR, the scorecard is the place the divergence has to be reconciled before merge — not the place the stale spec value gets copy-pasted. Operator-side mitigation: re-grep the diff for the cited token (`git diff origin/main...HEAD -- <path> | grep -F '<claimed-token>'`) before pushing the body. Citation: `feedback_scorecard_evidence_token_required`.

## Disposition

The PR is merged and the artifact on `main` is the busybox sidecar; there is no operational defect to fix. The historical PR body cannot be rewritten post-merge without rewriting history, which is not worth the cost for a single inaccurate evidence cell. This file is the durable correction of the record. Closes #860.
