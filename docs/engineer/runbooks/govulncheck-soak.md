# govulncheck 30d soak — day-30 decision runbook

Tracks #1237. Soak window: 2026-06-10 → 2026-07-10.

## Context

PR #1207 hit 170-min wall-clock from post-approval govulncheck flake. The job runs on every PR even though vuln scope cannot change without a `go.mod`/`go.sum` diff. The workflow now:

- Triggers on PR only when `go.mod` or `go.sum` change.
- Runs nightly via cron `0 8 * * *` (08:00 UTC) + on every `push: main` for main-branch coverage.
- Carries `continue-on-error: true` so a flake cannot stall a PR during the 30d measurement window.

## Day-30 decision (2026-07-10)

### 1. Compute flake rate

```bash
gh run list --workflow=govulncheck --limit 100 \
  --json conclusion,createdAt,event \
  | jq '[.[] | select(.createdAt > "2026-06-10")]
        | {total: length,
           failure: ([.[] | select(.conclusion=="failure")] | length),
           cancelled: ([.[] | select(.conclusion=="cancelled")] | length),
           success: ([.[] | select(.conclusion=="success")] | length)}'
```

Flake rate = (`failure` + `cancelled` runs whose root cause is NOT an actual CVE) / `total`.

Spot-check failure logs to confirm cause is infra flake (network, action timeout, Go toolchain download) vs. real vuln finding.

### 2. Branch

- **Flake rate > 5%** → keep `continue-on-error: true`; the check stays informational. Do NOT add to required-status-checks on `main` branch protection. File a separate issue if the flake source is fixable upstream.
- **Flake rate ≤ 5% AND zero real CVE misses on PRs that landed during the soak** → drop `continue-on-error`, optionally promote to required check. The paths-filter + nightly cron stay regardless.

### 3. Comment-back on #1237

Post the flake-rate numbers + decision on issue #1237, then close.

## Tracker

Day-30 follow-up is auto-filed via the `audit-session` skill on 2026-07-10. If the skill misses it, file manually as `[CI] govulncheck soak day-30 decision (#1237 follow-up)`.
