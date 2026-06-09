# Session 1106 — Open BUG-10xx Followup Roadmap

Triage of the 17 open limitation issues remaining after the 2026-06-08 docker-soak session. Already addressed this session and excluded from this roadmap: #1063, #1066, #1085, #1089, #1090, #1095, #1097, #1098, #1099. Issue #1078 is in-flight (Task #19, scriptable state.db CLI) and listed in Tier S for completeness.

Decision priority applied per `CLAUDE.md`: UX > ease > performance > best-practices > speed > velocity; long-term > short-term; self-host filter ("does the sole internal operator need this to dispatch regatta-the-binary at this repo unattended?") governs Tier-B deferrals.

Total open issues in scope: **17**.

## Tier S — operator-friction blockers (ship next session)

PRs/changes that remove burning friction observed during the 2026-06-08 17h45m soak. Strict self-host fit, ≤2 file-disjoint dispatches each.

| # | Title (short) | Why Tier S |
|---|---|---|
| #1078 | scriptable state.db CLI (`regatta state agents/events/zombies`) | Operator ran raw `sqlite3` 8+ times this session; shipping this removes the most-repeated manual step. Already in flight (Task #19). |
| #1092 | `orchestrator.item_body_missing` — adapter does not cache issue body before spawn | Direct soak observation: spawn proceeds with empty body, implementer gets no WHY/acceptance text. Single adapter fix; recurring trap on every fresh boot. |
| #1096 | credit-balance exit halts dispatch silently; no operator alarm | Same soak: daemon looked green for hours while every spawn died on `Credit balance is too low`. Needs WARN+terminal classification (pairs with the existing #1063 classifier work). |
| #1086 | claude spawn inherits all host MCP servers; 6 child processes per agent (42 for 7 agents) | Measured ~50-200 MB × 42 idle subprocesses + ~10-15s boot delay per agent. Smallest-surface fix unblocks higher parallel-implementer caps. |
| #1077 | orchestrator never self-files issues from observed friction | Operator filed 21+ issues manually this session for patterns the orchestrator already logged. `cmd/regatta/selfimprove.go` is the existing surface — closes the friction loop. |

## Tier A — meaningful improvements (next 1–2 weeks)

Real operator value, but not blocking the next soak. Sized for designer-first or single-implementer dispatch once Tier S is clear.

| # | Title (short) | Rationale |
|---|---|---|
| #1079 | running daemon's binary is frozen at startup; no in-place self-update | Operator decision is already "docker = main run + rebuild + restart frequently"; gives a built-in path so the docker workflow is honest. Needs design (graceful drain vs. fast restart). |
| #1080 | introduce L6.1 risk-tiered auto-merge for low-risk PRs | Inverse of the existing path classifier; deps bumps + doc typos no longer need click-merge. Self-host fit (single operator burden); must ship with adversarial review + soak window guardrails. |
| #1081 | L6.2 session-batch merge (one click, N green PRs, topo-sorted) | Complements #1080 for the load-bearing case where every PR needs a click. ~3.5min/session burden today; compounds with parallel agents. |
| #1082 | adapter ignores `## Reopen trigger` + `## Why deferred`; wedge issues dispatched as ready | Caused operator to manually exclude #832 this session. Adapter-level grammar extension — single package change. |
| #1087 | single-pass reviewer subagents miss real findings (5/6 wrong this session) | Direct evidence the canonical adversarial-review-once mandate is insufficient. Pairs with dispatch-template + scripts/check-reviewer-verdict.sh updates. Needs designer pass (how many passes, when to budget). |
| #1083 | WorkItem schema is single-phase; multi-phase items (e.g. #832) conflate into one dispatch | Schema + adapter change; unlocks the Wave/Phase A/B/C pattern already in use by roadmap issues. |
| #1084 | scheduler dispatches `implementer` only; research-then-build shapes can't designer-first | Same root cause as #1083's symptom on #832; pairs naturally with it (one dispatch wave). |
| #1088 | no mechanical mock-vs-real test-ratio check; CLAUDE.md rule is honor-system | Pure-addition script + CI gate; closes the honor-system gap the test-coverage-audit-per-wave rule already names. Small, file-disjoint. |
| #1076 | selector grammar is single-label only; cannot AND-combine roadmap + priority labels | Spec_adapter.selector grammar extension (follow-on to #1068). One-package fix; operator hits this whenever they want `roadmap-mvr AND priority-high`. |

## Tier B — Phase-X / forward-fit / deferred

Per CLAUDE.md self-host filter: defer with explicit reopen-trigger. None of these are needed for the sole internal operator to dispatch regatta-the-binary at this repo unattended.

| # | Title (short) | Reopen trigger |
|---|---|---|
| #1074 | no Jira adapter; enterprise teams on Jira cannot use regatta | First external enterprise customer ask with Jira as system-of-record. Phase-X per self-host filter. |
| #1075 | no RFC-document adapter (Rust/K8s style) | First external team adopting regatta whose roadmap-as-RFCs is non-negotiable. Phase-X per self-host filter. |
| #1093 | `spawn.failed` retried every 5s with no backoff / circuit breaker / max-retries | Already substantially mitigated by #1090 (fix that landed this session). Reopen if soak shows the residual hot loop after #1090 is in production. Single-failing-item case is annoying but not blocking the operator. |
| #1094 | stub→claude spawner swap causes `recovered_crashed` phantom holding lane | Reproduces only when operator boots stub then claude on the same `regatta-data` volume — a configuration the docker workflow no longer uses (docker boots claude directly post-#1090). Reopen if the swap path returns. |

## Tier C — won't-fix / superseded / duplicate

(none in this batch)

---

## Roll-up

- Tier S: 5 (one in flight)
- Tier A: 9
- Tier B: 4
- Tier C: 0
- **Total: 17** (cross-check: 17 inputs above the in-flight #1078; #1078 is the in-flight Tier-S item)

Sort within each tier is operator-friction-descending (Tier S/A) and self-host-distance-descending (Tier B). No code changes proposed; no commits; no PRs.
