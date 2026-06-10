# govulncheck 30d soak — day-30 decision runbook

Tracks #1237. **Soak window starts on first cron execution after PR #1238 merges** — date filter starts from the PR merge commit date, NOT this file's authoring date (2026-06-10). Revisit after 30 calendar days of cron coverage on `main`.

## Context

PR #1207 hit 170-min wall-clock from post-approval govulncheck flake. The job runs on every PR even though vuln scope cannot change without a `go.mod`/`go.sum` diff. The workflow now:

- Triggers on PR only when `go.mod` or `go.sum` change.
- Runs nightly via cron `0 8 * * *` (08:00 UTC) + on every `push: main` for main-branch coverage.
- Carries `continue-on-error: true` so a flake cannot stall a PR during the 30d measurement window.

## Day-30 decision (date = PR #1238 merge date + 30d)

### 1. Compute flake rate

Set `SOAK_START` to the PR #1238 merge date (ISO 8601, e.g. `2026-06-11`) before running:

```bash
SOAK_START="<merge-date>"  # e.g. 2026-06-11
gh run list --workflow=govulncheck --branch main --limit 100 \
  --json conclusion,createdAt,event \
  | jq --arg start "$SOAK_START" \
       '[.[] | select(.createdAt > $start)
              | select(.event=="schedule" or .event=="push")]
        | {total: length,
           failure: ([.[] | select(.conclusion=="failure")] | length),
           cancelled: ([.[] | select(.conclusion=="cancelled")] | length),
           success: ([.[] | select(.conclusion=="success")] | length)}'
```

The `--branch main` filter + `event` selector restrict the sample to scheduled/push runs on `main`. PR-event runs are excluded because they only fire on `go.mod`/`go.sum` diffs and skew the denominator.

Flake rate = (`failure` + `cancelled` runs whose root cause is NOT an actual CVE) / `total`.

### 2. Spot-check for real CVE findings (MANDATORY)

`continue-on-error: true` suppresses job failure regardless of cause — a genuine CVE finding will look identical to an infra flake in the conclusion field. Before deciding, manually inspect:

```bash
# List all non-success runs in the window for triage.
gh run list --workflow=govulncheck --branch main --limit 100 \
  --json databaseId,conclusion,createdAt,event,headSha \
  | jq --arg start "$SOAK_START" \
       '[.[] | select(.createdAt > $start)
              | select(.conclusion!="success")]'

# For each non-success run, view the job log and grep for vuln markers.
gh run view <databaseId> --log | grep -E 'Vulnerability|GO-[0-9]+|CVE-' || echo "no CVE markers"
```

Classify each non-success run as: (a) infra flake (network, action timeout, toolchain download), (b) real CVE finding, (c) other. A run in bucket (b) means the suppression has masked a real signal — the gate failed its purpose; promotion path requires fixing the upstream vuln BEFORE flipping `continue-on-error`.

### 3. Branch

- **Flake rate > 5%** → keep `continue-on-error: true`; the check stays informational. Do NOT add to required-status-checks on `main` branch protection. File a separate issue if the flake source is fixable upstream.
- **Flake rate ≤ 5% AND zero real CVE findings missed during the soak (per §2 spot-check)** → drop `continue-on-error`, optionally promote to required check. The paths-filter + nightly cron stay regardless.

### 4. Comment-back on #1237

Post the flake-rate numbers + spot-check classification table + decision on issue #1237, then close.

## Tracker

- **Day-10 reminder**: file `[CI] govulncheck soak day-30 spot-check reminder (#1237 follow-up)` 10 days after PR #1238 merge to surface §2 spot-check as a calendar-visible task; suppression hides real signal between now and day-30 otherwise.
- **Day-30 follow-up**: auto-filed via the `audit-session` skill on the soak end date. If the skill misses it, file manually as `[CI] govulncheck soak day-30 decision (#1237 follow-up)`.
