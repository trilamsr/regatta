---
id: PHASE-AUTONOMY-W6
title: secret-credential autonomic fetch — supervisor unlocks via pass + gpg-agent
lane: self-host
kind: feature
status: planned
gate: phase-autonomy-landing-2 (W3 merged)
source_ref: docs/engineer/briefs/2026-06-02-phase-autonomy-amendment.md §11 W6
dependencies: PHASE-AUTONOMY-W3
linked_artifact: docs/engineer/briefs/2026-06-02-phase-autonomy-amendment.md
---

Source brief: PHASE AUTONOMY amendment §11 W6 (Landing 2, depends on W3 — the supervisor process owns the secret-fetch step).

## Scope

Supervisor unlocks credentials at boot via gpg-agent; operator no longer types `export ANTHROPIC_API_KEY=...` per wake. Adopt `pass` for the three secrets: `ANTHROPIC_API_KEY` + `GH_TOKEN` + `REGATTA_BRIEF_HMAC_KEYS`. Fallback to env-var if `pass` not installed.

gpg-agent unlock TTL configurable via `regatta.yaml: secrets.gpg_agent_ttl_seconds`. `regatta status` shows secret source (`pass` vs `env`) per secret without printing the value.

## Approach

- Adopt `pass` (GPL-2) as the secret store; GPG-backed, file-tree-shaped, no vendor dependency. Reference `gopasspw/gopass` (MIT) as the Go-native alt if shelling out proves awkward.
- Reject Vault (too heavy for persona-A one-binary operator). Reject systemd `LoadCredential` (macOS lacks parity).
- Build (~100 LoC) supervisor shim that reads the three secrets at boot, exports as env vars to the regatta serve process, fails-closed with a clear error if `pass` isn't installed AND no fallback env vars set.

## Acceptance criteria

- [planned] c1: `regatta install-service` (from W3) checks for `pass` + prompts to initialize if missing.
- [planned] c2: Boot path: gpg-agent → `pass show regatta/anthropic_api_key` → env var → `regatta serve`.
- [planned] c3: Same shape for `GH_TOKEN` + `REGATTA_BRIEF_HMAC_KEYS`.
- [planned] c4: Fallback — if `pass` not installed, fall back to existing env-var read; emit substrate event `secret_source=env`.
- [planned] c5: gpg-agent unlock TTL configurable via `regatta.yaml: secrets.gpg_agent_ttl_seconds`.
- [planned] c6: Adversarial reviewer subagent posts.

## B/A/A+ rubric

| Tier | Criteria |
|---|---|
| B (floor) | (a) c1+c2 ship. (b) Env-var fallback intact. (c) Release-notes fence. |
| A (target) | B + (d) c3+c4+c5+c6. (e) Rotation drill: `pass insert -e regatta/anthropic_api_key` + `regatta reload-secrets` rotates without restart. |
| A+ (stretch) | A + (f) gopass integration tested as a drop-in alt to `pass`. (g) Secret-presence diagnostic in `regatta status` — shows source per secret without printing the value. (h) Failure mode: gpg-agent absent → clear error + recovery doc link, not a stack trace. |

## Cites

- `docs/engineer/briefs/2026-06-02-phase-autonomy-amendment.md` §11 W6
- `pass` (GPL-2) — adopted secret store
- `gopasspw/gopass` (MIT) — adopted alt
- systemd `LoadCredential` — rejected (no macOS parity)
- HashiCorp Vault — rejected (too heavy for persona-A)
- `feedback_decision_priority` — operator UX: zero-touch-credential-at-wake is the weekend-laptop-closed unblock
- `feedback_research_design_principles` — adopt-first; secret store adopted, supervisor shim built
