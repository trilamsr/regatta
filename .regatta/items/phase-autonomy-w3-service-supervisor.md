---
id: PHASE-AUTONOMY-W3
title: service-supervisor — launchd + systemd units, /healthz, install-service cmd
lane: self-host
kind: feature
status: blocked
gate: phase-autonomy-landing-2 (W1+W2 merged)
source_ref: docs/engineer/briefs/2026-06-02-phase-autonomy-amendment.md §11 W3
dependencies: PHASE-AUTONOMY-W1, PHASE-AUTONOMY-W2
linked_artifact: docs/engineer/briefs/2026-06-02-phase-autonomy-amendment.md
---

Source brief: PHASE AUTONOMY amendment §11 W3 (Landing 2, depends on W1+W2 — restart correctness is testable only with the loop active).

## Scope

Ship `regatta install-service`: writes the right launchd plist (macOS) or systemd unit (Linux), registers it with the OS init system, and verifies `/healthz` returns 200 within 30s. Add the `/healthz` endpoint to the serve binary. Ship crontab lines under `dist/cron/regatta.crontab` for daily-digest + `make items` + `make followups`.

Operator stops being the restart actor. Loop survives reboots + crashes + log churn via OS-native facilities only.

## Approach

- Adopt systemd verbatim on Linux: `Restart=on-failure`, `WatchdogSec=30`, `EnvironmentFile=/etc/regatta.env`.
- Adopt launchd verbatim on macOS: `KeepAlive`, `RunAtLoad`, `StandardErrorPath`-rotation.
- Adopt the k8s `/healthz` convention for the endpoint.
- Build: `regatta install-service` (~150 LoC) writes the right unit/plist, bootstraps it, polls `/healthz`. Build: `/healthz` handler in serve (~20 LoC).
- Two service-file templates under `dist/services/` (~30 lines each).

## Acceptance criteria

- [planned] c1: `regatta install-service` on macOS writes the launchd plist + `launchctl bootstrap`s it.
- [planned] c2: Same command on Linux writes the systemd unit + `systemctl enable --now`s it.
- [planned] c3: `kill -9` on the regatta PID — supervisor restarts within 10s; `/healthz` returns 200.
- [planned] c4: Log rotation via OS-native facility (journald on Linux, launchd `StandardErrorPath` rotation on macOS). No log-rotator added.
- [planned] c5: Crontab lines for daily-digest + `make items` + `make followups` land under `dist/cron/regatta.crontab`.
- [planned] c6: `regatta uninstall-service` reverses cleanly (lock-file removed, unit deregistered).
- [planned] c7: Adversarial reviewer subagent posts.

## B/A/A+ rubric

| Tier | Criteria |
|---|---|
| B (floor) | (a) c1+c2+c3 ship. (b) `/healthz` ≤ 30 lines. (c) Release-notes fence. |
| A (target) | B + (d) c4+c5+c6+c7. (e) Pidfile + lock-file prevent double-start. |
| A+ (stretch) | A + (f) Lock-file race tested with 50 concurrent `install-service` calls; only one wins. (g) Service file signed via cosign as part of release. (h) `regatta install-service --dry-run` prints the unit/plist + exits without mutating. |

## Cites

- `docs/engineer/briefs/2026-06-02-phase-autonomy-amendment.md` §11 W3
- systemd — Linux init contract (adopted verbatim)
- launchd — macOS init contract (adopted verbatim)
- `grafana/agent` (Apache 2) — reference for how a Go daemon ships both unit files
- Kubernetes `/healthz` convention — adopted endpoint shape
- `feedback_decision_priority` — operator UX: loop-survives-reboot is the load-bearing weekend unblock
- `feedback_research_design_principles` — adopt-first; init contracts adopted, install command built
