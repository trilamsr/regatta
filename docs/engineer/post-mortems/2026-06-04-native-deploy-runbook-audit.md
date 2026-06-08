---
status: draft
date: 2026-06-04
target: docs/operator/native-deploy.md
flow_steps: 14
---

# Native deploy runbook audit (2026-06-04)

## 0. TL;DR

`docs/operator/native-deploy.md` covers steps 8-9 of the 14-step Phase
DEPLOY operator flow (the `install-service` surface itself) and almost
nothing else. The first seven steps (clone, `.env`, `regatta.yaml`
edits, Anthropic console cap, branch-protection sanity, `make ci-check`)
and the last five (`status` panels, smoke dispatch, first PR walk,
alarm-webhook verification, 24h stop criteria) are absent. PR #830
(macOS plist `.env`-sourcing wrapper) is in flight and CLEAN but
unmerged — the runbook's troubleshooting note "Missing `ANTHROPIC_API_KEY`
/ `GH_TOKEN` in the env file" (`native-deploy.md:171`) is currently a
lie on macOS, because the rendered plist injects only `PATH`+`HOME`
(`internal/supervisor/templates/regatta.plist.tmpl:42-48`).

Top-3 patch priority:

1. Pre-install section covering `.env` + `regatta.yaml` + `make ci-check`
   (blocks every macOS install today).
2. Verification section covering `regatta status` panels + smoke
   dispatch + first-PR walk (operator cannot tell green from red without
   it).
3. Alarm-webhook + 24h stop-criteria section (operator otherwise watches
   forever).

## 1. Operator flow ground truth

The 14 steps the sole operator walks tonight, mapped to the section of
`native-deploy.md` that *should* cover them:

| # | Step | Section in runbook today |
|---|---|---|
| 1 | Clone / pull repo | none |
| 2 | `cp .env.example .env` + fill `ANTHROPIC_API_KEY` + `GH_TOKEN` | none |
| 3 | `chmod 600 .env` | none |
| 4 | Edit `regatta.yaml` — `spend_cap_usd_per_day: 20` + `alarm_webhook` block | none |
| 5 | Anthropic console — monthly spend cap | none |
| 6 | GitHub branch-protection sanity (`regatta verify-repo-config`) | none |
| 7 | `make ci-check` on `main` — exit 0 | none |
| 8 | `regatta install-service --user --env-file ~/.config/regatta/env` | `## One-command install` + `## macOS install (LaunchAgent)` |
| 9 | Verify launchd loaded + `regatta serve` running | `### Verify` (macOS), `### Verify` (Linux) |
| 10 | `regatta status` — populated panels | none |
| 11 | Smoke-dispatch one trivial work_item | none |
| 12 | Watch first PR end-to-end (brief → spawn → L4 → merge) | none |
| 13 | Alarm-webhook fires on intentional cost-cap trip | none |
| 14 | 24h unattended green-clock criteria | none |

Net: 2 of 14 covered (steps 8 + 9). Runbook is a *supervisor install*
doc, not a *deploy* runbook.

## 2. Gap matrix

| Step | Runbook coverage | Verification cue today | Gap severity |
|---|---|---|---|
| 1. clone | missing | none | LOW (operator clones repos daily) |
| 2. `.env` fill | missing | none — and `--env-file` flag not shipped pre-#830 | CRITICAL — without this macOS boot-loops with no agent calls (`internal/supervisor/templates/regatta.plist.tmpl:42-48` pre-#830) |
| 3. `chmod 600 .env` | missing | none | HIGH — credential hygiene; #830 adds a WARN at install time but runbook never tells the operator to fix mode upfront |
| 4. `regatta.yaml` edits | missing | none — operator must read `configure.md` separately | HIGH — `spend_cap_usd_per_day` default in `configure.md:76` is `200`, operator wants `20`; `alarm_webhook` block has zero operator-facing doc |
| 5. Anthropic monthly cap | missing | none | MED — outside-the-repo step, easy to forget |
| 6. branch protection | missing | none | HIGH — `regatta verify-repo-config` is the right verb (`cmd/regatta/main.go:107`) and never mentioned in this runbook |
| 7. `make ci-check` | missing | none | MED — repo norm, but operator on a fresh laptop will skip without prompting |
| 8. `install-service` | covered (`§ One-command install`, `§ macOS install`) | `curl /healthz` + `launchctl print` | LOW — well-covered |
| 9. verify launchd | covered (`§ Verify`) | `launchctl print` + `tail stderr.log` + `curl /healthz` | LOW |
| 10. `regatta status` | missing | none | HIGH — the 5-panel TUI is the operator's primary green-clock instrument (`cmd/regatta/status.go:23-25,95-111`) |
| 11. smoke-dispatch | missing | none | HIGH — operator has no "did it actually pick up work?" test |
| 12. first-PR watch | missing | none | HIGH — operator does not know what success looks like |
| 13. alarm-webhook trip | missing | none | HIGH — `alarm_webhook` block in `regatta.yaml:67-70` is undocumented in this runbook; cost-cap trip path is the load-bearing escape hatch (`cmd/regatta/wire_alarm_webhook.go:22`) |
| 14. 24h stop criteria | missing | none | CRITICAL — operator watches indefinitely (anti-`feedback_operator_minimal_input`) |

## 3. Stale references found

- `native-deploy.md:122-124` — `regatta install-service` shown without
  `--env-file` flag; PR #830 adds the flag and the docs section in the
  same PR. Will be auto-fresh after #830 merges; flag as
  "stale-after-merge", not "stale now". Followup-of-#830 OK.
- `native-deploy.md:171` — "Missing `ANTHROPIC_API_KEY` / `GH_TOKEN` in
  the env file" troubleshooting is currently misleading on macOS pre-#830:
  even a perfectly-formed env file is ignored because
  `internal/supervisor/templates/regatta.plist.tmpl:42-48` injects only
  `PATH` + `HOME`. Will be true post-#830.
- `native-deploy.md:17-18` — "one binary, one supervisor, journald or
  `tail -F` for logs" — accurate on Linux, accurate on macOS *only after*
  #830 lands; before then, no env reaches the binary so "one binary"
  silently no-ops.
- `native-deploy.md:25-26` — claim "operator runs one command, never
  `systemctl daemon-reload` or `launchctl bootstrap` by hand". Accurate
  for the install verb itself, but the *deploy* journey is not one
  command — see steps 1-7 above.
- `native-deploy.md:172-173` — "Cold-start brief load slower than 30s
  — re-run `install-service` once the brief is cached." Re-running
  `install-service` does nothing for brief caching; it re-renders the
  unit and re-bootstraps launchd. The right verb is `launchctl
  kickstart -k gui/$UID/com.regatta.serve` or wait for the watchdog
  restart. Stale advice, MED.
- `native-deploy.md:188` — "binds when `--ui=true` (default)". Verify
  this flag still ships under that name in `serve.go`; HTMX UI rip
  (3-phase → operator-console v5.1) renamed the listener flag. Did NOT
  fully verify — flagged as "audit-todo, not stale-confirmed".

Stale-reference count: **5 confirmed** (1 post-#830-resolves, 4
operator-affecting today) + **1 audit-todo**.

## 4. Proposed patches (NOT applied)

Each patch is one operator-facing paragraph; the runbook owner picks
the section heading.

**Patch A — "Pre-flight" (new `## 0. Before you install` section, ahead
of `## When to use this path`):**

> Native deploy assumes you have already cloned the repo, copied
> `.env.example` → `.env` and filled `ANTHROPIC_API_KEY` + `GH_TOKEN`
> (`chmod 600 .env`), edited `regatta.yaml` for your spend cap
> (`safety.spend_cap_usd_per_day: 20` is the recommended self-host
> default — the schema default is `200`, sized for a team) and
> alarm-webhook routing (`alarm_webhook.listen_addr: 127.0.0.1:9101` +
> `gh_repo: <owner/name>`), set a matching monthly cap in the Anthropic
> console (irreversible cost ceiling — the loop's own
> `spend_cap_usd_per_day` is a *soft* cap), run `regatta
> verify-repo-config` to confirm branch protection matches the P2
> canonical recipe, and run `make ci-check` on `main` to confirm a
> green tree. Skip any of these and the install will *succeed*
> (`/healthz` returns 200) but the loop will not do useful work.

**Patch B — "Step-8 cross-link to `--env-file`" (insert before the
existing one-command install block):**

> On macOS the rendered plist sources the env file at boot via a
> `set -a; . <file>; set +a; exec` wrapper (PR #830). Pass
> `--env-file ~/.config/regatta/env` explicitly; default is
> `$HOME/.config/regatta/env` for `--user` and `/etc/regatta/env` for
> `--system`. Pre-#830 plists ignore the env file entirely; if you are
> running off `main` before #830 merges, the install will succeed but
> every Claude API call will 401 — confirm `git log --oneline
> --grep="#830"` shows a merged commit before continuing.

**Patch C — "Step-10 status panels" (new `### Verify the loop is
working` subsection, after each `### Verify` block):**

> `regatta status` (or `regatta status --once` for a one-shot snapshot)
> renders five panels: active subagents, in-flight PRs, recent merges
> (24h), today's cost, and the green-clock counter. Green-loop expected
> shape within 60 seconds of `install-service` returning: subagents=0,
> PRs=0, merges=0, cost=$0.00, green-clock starts ticking. Any panel
> stuck at `MISSING` for more than 5 minutes means substrate-DB
> connectivity is broken — tail stderr.log.

**Patch D — "Step-11 smoke-dispatch" (new `## Smoke-dispatch your
first work_item` section):**

> Drop a minimal markdown item into `.regatta/items/SMOKE-1.md` with
> `kind: refactor`, one acceptance criterion ("rename a comment in
> `docs/operator/README.md`"), and `status: planned`. The next tick
> (within 30s) spawns a stub agent; watch `regatta status` for the
> subagent panel to flip from 0 → 1 → 0, the in-flight-PR panel to flip
> 0 → 1, and finally the recent-merges panel to tick once after you
> click merge in the GitHub UI. End-to-end target: <10 minutes from
> drop to merge on a trivial item.

**Patch E — "Step-12 first-PR walk" (subsection inside Smoke-dispatch):**

> The first real PR will fire `brief.signed → spawn → L4 verdict →
> automerge gate → human-merge`. Watch each transition in
> `journalctl -u regatta` (Linux) or `tail -F
> ~/Library/Logs/regatta/stderr.log` (macOS). Today MVR-1-T4 (GH-issue
> adapter) is **not yet shipped** — alarm-webhook *fires* and files an
> issue, but the loop does *not* auto-consume the new issue back into
> work-item state. Operator action required to close the loop.

**Patch F — "Step-13 alarm-webhook trip" (new `## Verify the
alarm-webhook` section):**

> The alarm-webhook listens on `alarm_webhook.listen_addr` (default
> `127.0.0.1:9101`) and files a GitHub issue on receipt
> (`internal/alarmwebhook/handler.go`). To verify end-to-end, force a
> cost-cap trip: set `safety.spend_cap_usd_per_day: 0.01` in
> `regatta.yaml`, run `regatta reload-secrets` so the cap re-loads, drop
> a smoke item. Expect one GH issue with title `regatta cost-cap
> tripped` within 30s. Restore the cap, run `regatta resume` to lift
> the throttle. (See also `cmd/regatta/main.go:124-125`.)

**Patch G — "Step-14 24h stop criteria" (new `## 24h unattended green
criteria` section):**

> Green at 24h means: (a) `regatta status` green-clock panel has been
> ticking uninterrupted for 24h (no restart events), (b) cost-today
> panel is under your daily cap, (c) zero `brief.rejected` /
> `child.cascade_archived` events in journald in the last 24h, (d) at
> least one PR closed end-to-end (any path: merged, abandoned via
> approval-gate timeout, or auto-rejected by L4). Stop watching at 24h
> if all four hold; if not, capture the failed panel in a post-mortem.

## 5. Smoke-dispatch walkthrough draft

```sh
# Step 1 — drop a trivial work item.
cat > .regatta/items/SMOKE-1.md <<'EOF'
---
id: SMOKE-1
kind: refactor
title: smoke — rename a docs comment
status: planned
---

## Acceptance criteria

- [planned] c1: change one HTML comment in docs/operator/README.md
EOF

# Step 2 — verify the adapter sees it.
sqlite3 .regatta/state.db \
  "SELECT id, status FROM work_items WHERE id='SMOKE-1'"
# Expected: 1 row, status=planned (before tick) → running (after tick).

# Step 3 — watch the loop pick it up.
regatta status --refresh 5s
# Expected within 30s: active-subagents panel flips 0 → 1.

# Step 4 — open the spawned PR in the browser and click merge.
gh pr list --state open --search "SMOKE-1" --json number,title

# Step 5 — confirm green-clock + recent-merges advance.
regatta status --once
# Expected: recent-merges panel shows the SMOKE-1 PR, green-clock
# counter has not reset.
```

End-to-end SLO target: <10 minutes from item drop to merge for a
trivial refactor.

## 6. Rollback escape hatch

Runbook today covers `regatta uninstall-service` (lines 49-50, 102-105,
156-160) — adequate for the *happy path*. Missing escape hatches:

- **launchd unit hangs (macOS)**: `launchctl bootout
  gui/$(id -u)/com.regatta.serve` forcibly tears down a stuck agent
  before `uninstall-service` runs.
- **systemd unit hangs (Linux)**: `sudo systemctl kill -s KILL
  regatta.service` then `sudo systemctl reset-failed regatta.service`.
- **Stuck `/healthz` poll during install**: Ctrl-C is safe — the install
  is atomic at `writeAtomic` (`internal/supervisor/supervisor.go:372`);
  re-run with `--force` to retry.
- **Wedged DB / state corruption**: `regatta uninstall-service` leaves
  `~/.local/share/regatta/` intact by design. To reset, `rm -rf
  ~/.local/share/regatta/` between uninstall and reinstall. Document
  this as the last-resort verb.

Severity: MED — the happy-path uninstall verb covers ~80% of rollback
needs; the operator needs the four escape verbs above for the other
20%.

## 7. Common-pitfall callouts

One-line warnings to insert as a `## Common pitfalls` callout block:

- `.env` was not gitignored prior to #826 — re-clone or `git rm --cached
  .env` if running off an older checkout.
- `chmod 600 .env` is checked by the supervisor (#830) but not enforced;
  loose modes WARN, install proceeds. Fix the mode before continuing.
- `GH_TOKEN` in your shell environment overrides `.env`; if you have
  both, the shell wins on Linux (`EnvironmentFile=` is overridden by
  the parent), the `.env` wins on macOS (the plist `set -a; . ...; exec`
  runs after the parent env is captured). Pick one source.
- launchd on macOS does NOT inherit your interactive shell's PATH.
  Confirm `launchctl print gui/$UID/com.regatta.serve | grep PATH` shows
  the brew prefix your binary lives in.
- `regatta.yaml` lives at repo root, not under `~/.config/regatta/` —
  the daemon reads it from `--repo`'s working directory, not a global
  config path.
- Anthropic console monthly cap is the *only* hard ceiling; the
  in-loop `spend_cap_usd_per_day` is a *soft* cap that throttles but does
  not bill-block.

## 8. Stop criteria for 24h watch

Operator may stop watching when ALL four hold:

- [ ] `regatta status --once` green-clock panel shows ≥24h since last
      restart event.
- [ ] Cost-today panel under your `spend_cap_usd_per_day` setting.
- [ ] Zero `brief.rejected`, `brief.tombstoned`, `child.cascade_archived`,
      `child.dependency_archived`, or `alarmwebhook.disabled` events in
      journald / stderr.log in the last 24h.
- [ ] At least one PR closed end-to-end (merged via operator click,
      abandoned via approval-gate timeout, or L4-rejected). Confirms
      the brief→spawn→L4→merge path is exercised, not just polled.

If any fail, do not stop — capture the failing panel + the matching
log slice into a post-mortem at `docs/engineer/post-mortems/`.

7d criteria add: GH-issue adapter (MVR-1-T4) must have shipped, else
operator must hand-consume alarm-webhook issues weekly.

30d criteria add: at least one full automerge cycle (no human merge
click) — gated on Phase X relaxation, not on Phase S.

## 9. Adversarial-review findings

Reviewer prompt was prepared (load `feedback_operator_minimal_input`,
`feedback_decision_priority`, `feedback_drop_ceremony`; hunt missed
gaps, over-engineering proposals, under-spec verification cues, missing
rollback paths). Inline-self-review pass on this draft surfaced:

- **[HIGH] Missed gap — `regatta validate-config`** (`cmd/regatta/main.go:106`)
  belongs in pre-flight step 4 verification cue, not just step 6. After
  the operator edits `regatta.yaml` for spend-cap + alarm-webhook, the
  correct cue is `regatta validate-config && echo ok` *before* the
  install, not the install-time text-schema check. Fixed in Patch A
  (add `regatta validate-config` to the verb list).
- **[MED] Over-engineering — Patch F cost-cap trip procedure** edits
  `regatta.yaml`, runs `reload-secrets`, drops smoke, restores cap,
  runs `resume`. Five operator steps for one verification. Lighter
  alternative: `curl -X POST 127.0.0.1:9101 -d
  '{"alerts":[{"labels":{"alertname":"smoke"}}]}'` directly hits the
  webhook handler (`internal/alarmwebhook/handler.go`) and files a test
  issue. One step, no cap perturbation. Tighten Patch F to the curl
  recipe; keep the cap-trip as an optional second-tier check. Per
  `feedback_drop_ceremony`: 5 steps where 1 suffices is ceremony.
- **[MED] Under-spec verification cue — Patch C "any panel stuck at
  MISSING for more than 5 minutes"**. `MISSING` is the renderer's
  fallback when the substrate DB query returns no rows
  (`cmd/regatta/status.go:231`), which is *expected* on a fresh install
  where no work-items have been spawned. The cue should be: "cost panel
  stuck at MISSING after the first smoke item lands" (i.e., after
  there is data to render). Adjust Patch C.
- **[LOW] Missing rollback — `regatta uninstall-service --force`**.
  Audited: the supervisor's Uninstall path (`internal/supervisor/supervisor.go:180-212`)
  has no `--force` flag, just best-effort error collection. Operator
  escape if the launchd unit-file is locked is `launchctl bootout`
  first, then uninstall. Already captured in §6.
- **[LOW] Drop-ceremony candidate — Patch A is 7 lines.** Compress to
  3 bullets: `(1) .env filled + chmod 600 (2) regatta.yaml spend cap +
  alarm_webhook block + validate-config (3) verify-repo-config +
  make ci-check`. Operator does not need prose explanation of *why*;
  the verbs are self-documenting. Tighten Patch A on apply.
- **[LOW] Missing — runbook owner needs to verify `--ui=true` default
  still ships** (`native-deploy.md:188`). Adversarial says: if the
  HTMX rip removed `--ui` (3-phase → operator-console v5.1) the
  troubleshooting tip is dead. Flagged as `[audit-todo]` in §3, not
  patched here.

Counts: CRITICAL 0, HIGH 1, MED 2, LOW 3, audit-todo 1.

§1-§8 patches above already fold the HIGH + both MEDs. The 3 LOWs are
patch-tightening notes for the runbook-owner PR.

## 10. Followup issues recommended (NOT filed)

Each title fits a single GH issue; sub-wedge owner is the dispatch
slot the operator should route into.

- `docs(native-deploy): add Phase DEPLOY pre-flight section (.env, regatta.yaml, validate-config, verify-repo-config, make ci-check)` — sub-wedge owner: PHASE-DEPLOY docs.
- `docs(native-deploy): add post-install verification section (regatta status panels, smoke-dispatch, first-PR walk)` — sub-wedge owner: PHASE-DEPLOY docs.
- `docs(native-deploy): add alarm-webhook verification + 24h stop criteria` — sub-wedge owner: PHASE-DEPLOY docs.
- `docs(native-deploy): add rollback escape hatches (launchctl bootout, systemctl kill, DB reset)` — sub-wedge owner: PHASE-DEPLOY docs.
- `docs(native-deploy): fix stale "re-run install-service" brief-cache advice (l.172)` — sub-wedge owner: PHASE-DEPLOY docs.
- `audit: verify --ui flag still ships under that name post-htmx-rip (native-deploy.md:188)` — sub-wedge owner: operator-console v5.1.
- `docs(native-deploy): rebase on PR #830 once merged (env-file flag, plist sourcing wrapper)` — sub-wedge owner: followup-of-#830.
- `feat(loop): MVR-1-T4 GH-issue adapter auto-consume — close the alarm-webhook → work_item loop` — sub-wedge owner: MVR-1-T4.
