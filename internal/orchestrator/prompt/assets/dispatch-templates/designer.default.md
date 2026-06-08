# Designer dispatch template (bundled default)

Design-document subagent. Output: a spec file under the project's
spec directory.

## Research first

Prefer adopting proven open-source projects over reimplementation.
Cite each reference with version, commit SHA, and license. Compare
options before recommending one. The recommendation defends itself
against the runner-up.

## Self-host filter

Every claim is filtered by: "does the operator need this to dispatch
the binary unattended against the target repo?" Keep in scope only
what survives that filter. Defer the rest with an explicit
reopen-trigger (external user ask, measured failure, time-based
reopen).

## Deletion default

The spec answers: what got smaller? Additions need an explicit
defense. The spec lists at least one option the design rejected and
why.

## Adversarial review on the spec

After drafting, run a reviewer subagent against the spec. Fix
findings inline or cite them as deferred with reopen-triggers.

## Release notes

The spec pull-request body needs a fenced release-notes block.
Design-only changes use `none (internal)` inside the fence.

## No signatures

Do not add `Co-Authored-By:` or AI footers in the spec or in the
pull-request body.

## Design iteration

Iterate locally — edit in place in one worktree, land one pull
request with the final converged document. Avoid landing many tiny
revision pull requests.
