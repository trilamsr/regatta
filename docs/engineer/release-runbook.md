# Release runbook

Reader: internal engineer cutting a Regatta release.
Read time: 5 minutes.

## Status

`release.yml` runs on tag push: verifies the tag signature,
re-runs `make ci-check` at the tagged commit, builds the
artifact, generates SLSA build provenance, and publishes a
GitHub Release with `--generate-notes`. The maintainer flips
CHANGELOG `## Unreleased` to a dated section by hand on `main`
before tagging.

## Pre-release checklist

- [ ] `main` is green: `make ci-check` locally + all required CI
      checks pass.
- [ ] CHANGELOG.md `## Unreleased` section is populated. Run
      `bash scripts/changelog-gen.sh <last-tag>..HEAD` to derive
      a draft; sanity-check Conventional-Commit categorization.
- [ ] Schema versions in `contracts/schemas/` align with the
      `version:` major intended for this release.
- [ ] If contracts/ promotions happened since the last release,
      the corresponding ADRs under `docs/rfcs/` are present.
- [ ] Smoke: `regatta validate-config --config examples/minimal/regatta.yaml`
      and `examples/full/regatta.yaml` both pass.

## Cutting the release

```sh
# 1. Pick the next version. Pre-v1.0 == anything goes; record the
#    rationale in the CHANGELOG release header.
VERSION=v0.X.Y

# 2. Flip CHANGELOG.
sed -i.bak "s|^## Unreleased\$|## Unreleased\n\n## $VERSION - $(date +%Y-%m-%d)|" CHANGELOG.md
rm CHANGELOG.md.bak
$EDITOR CHANGELOG.md  # confirm the section header lands cleanly

# 3. Commit + tag.
git add CHANGELOG.md
git commit -s -m "chore(release): $VERSION"
git tag -s "$VERSION" -m "$VERSION"

# 4. Push.
git push origin main
git push origin "$VERSION"
```

## Post-release

- [ ] GitHub Release published from the signed tag.
- [ ] Provenance attestation (`<tag>.intoto.jsonl`) attached to
      the release.
- [ ] Customer-release-notes derived: strip lines under
      `### Internal` from the CHANGELOG section; the remainder
      ships to customers via their support channel.
- [ ] `regatta.yaml` schema version bump (if any) documented in
      [`docs/operator/upgrade.md`](../operator/upgrade.md) and the
      `regatta migrate-config` path exercised end-to-end.

## Rollback

If the release ships with a regression:

1. Revert the regressing commit on `main`; cut a patch release
   (`v0.X.Y+1`).
2. Mark the bad tag with a "do not use" annotation in the GitHub
   Release UI.
3. Customer-impact: notify via the channel in
   [`SECURITY.md`](../../SECURITY.md).

Never delete a tag. Never force-push to `main`.
