---
id: PHASE-AUTONOMY-W1
title: alarm-webhook — AlertManager firing → GH issue with autonomous label
lane: self-host
kind: feature
status: planned
gate: phase-autonomy-entry (Phase S3 closed per next-horizon §4)
source_ref: docs/engineer/briefs/2026-06-02-phase-autonomy-amendment.md §11 W1
dependencies: none
linked_artifact: docs/engineer/briefs/2026-06-02-phase-autonomy-amendment.md
---

Source brief: PHASE AUTONOMY amendment `docs/engineer/briefs/2026-06-02-phase-autonomy-amendment.md` §11 W1 (Landing 1, head-of-chain).

## Scope

Build `cmd/regatta-alarm-webhook`: HTTP server receives Prometheus AlertManager webhook payload → calls `gh issue create --label autonomous --label obs-alert --label <severity>`. Body carries alarm name, SLO breach summary, current metric value, dashboard URL, and a `regatta replay` reproduce command. Dedup by alarm name: if an open issue already exists for the same `alertname`, comment instead of opening a new one.

Operator picks deployment shape: either as a sidecar binary under launchd/systemd OR co-hosted inside `regatta serve` (W3 supervisor handles either).

## Approach

- Adopt AlertManager's [webhook payload shape](https://prometheus.io/docs/alerting/latest/configuration/#webhook_config) verbatim — receive `{status, alerts: [{labels, annotations, ...}]}`.
- Adopt `github.com/google/go-github` for issue/comment calls; already in `go.mod`.
- Build the receiver (~200 LoC) + one config knob `alarm_webhook.listen_addr` in `regatta.yaml`.
- Dedup primitive: `gh issue list --label obs-alert --state open --search "in:title <alertname>"` → if hit, `gh issue comment`. Else `gh issue create`.

## Acceptance criteria

- [planned] c1: AlertManager-format POST creates an issue with `autonomous + obs-alert + <severity>` labels.
- [planned] c2: Dedup — second firing of the same `alertname` comments on the open issue instead of opening a new one.
- [planned] c3: Body includes alarm name, SLO threshold, current metric value, Grafana/dashboard URL, and a `regatta replay` reproduce command.
- [planned] c4: Receiver runs either as a sidecar binary (`cmd/regatta-alarm-webhook`) OR inside `regatta serve` — operator picks via supervisor unit.
- [planned] c5: Unit test against the literal AlertManager JSON sample from the upstream config docs.
- [planned] c6: Adversarial reviewer subagent spawned per `feedback_adversarial_review`; reviewer comment posted on the PR before automerge.

## B/A/A+ rubric

| Tier | Criteria |
|---|---|
| B (floor) | (a) c1+c2+c3 ship. (b) Single binary, no new runtime deps beyond `github.com/google/go-github`. (c) Release-notes fence in PR body. |
| A (target) | B + (d) c4+c5+c6. (e) Span coverage via W6 OTel for each `/webhook` HTTP request. (f) Per-criterion citation in PR body. |
| A+ (stretch) | A + (g) Property test: 100 random alarm payloads, none produce a duplicate issue when fed twice. (h) Receiver self-files a `self-improvement` issue when ≥3 alarms fire from the same `alertname` within 7 days (handoff to W4). (i) Replay-command embedded in body works end-to-end against the substrate fixture. |

## Cites

- `docs/engineer/briefs/2026-06-02-phase-autonomy-amendment.md` §11 W1
- AlertManager webhook docs (Apache 2) — payload contract source
- `grafana/oncall` (AGPL-3) — dedup-by-alarm-name pattern reference
- `feedback_research_design_principles` — adopt-first; payload-shape adopted, receiver built
- `feedback_decision_priority` — operator UX: unattended-night beats SLO-blind-night
- `feedback_grade_rubric` — scorecard posted verbatim in PR body
