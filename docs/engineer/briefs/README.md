# Engineer briefs

Strategic vision that survives sessions. One brief per inflection point. Briefs frame what we are about to build and why; the locked design lives in `../specs/`; accepted decisions for shipped milestones live in `../../rfcs/`.

## Index

- [`2026-06-01-self-host-first.md`](2026-06-01-self-host-first.md) — **active roadmap.** Self-host-first reorder: S1 dogfood-ready core → S2 trust-the-loop → S3 durability. Defers W7 htmx UI / W8 multi-tenant scope / W10 Sigstore / W11 blackboard / W12 billing / P3.8 adapters / W9 Temporal-impl to Phase X (external-customer trigger). Supersedes `2026-05-31-mvp-3-next-level.md` §4 rank ordering.
- [`2026-05-31-mvp-3-next-level.md`](2026-05-31-mvp-3-next-level.md) — MVP-3 next-level moves: substrate-first sequencing, W6 OTel finish, W7 operator web UI, W8 OPA RBAC, W9 replay+diff (Temporal hybrid), P3.8 swap-out adapters. _§4 rank ordering superseded by self-host-first brief; other sections still load-bearing as the external-buyer roadmap (Phase X reopen target)._

## What goes here

- Strategic framing that outlasts a single iteration.
- Wedge selection + ranking that drives the next 4-8 waves.
- Open questions surfaced for design subagents to answer.

## What does NOT go here

- Locked design (goes in `../specs/`).
- Accepted decisions for shipped milestones (goes in `../../rfcs/` with a monotonic number).
- Per-iteration plans, dispatch prompts, working reviews (stay under `docs/superpowers/`, gitignored, one-shot).

Promotion criterion: a brief survives the work it triggered. If the work shipped and the brief's framing is no longer load-bearing, archive by replacement (write a successor brief that supersedes by name).
