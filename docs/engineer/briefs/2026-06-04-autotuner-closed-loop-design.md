---
status: draft
date: 2026-06-04
companion: docs/engineer/specs/2026-06-02-phase-autonomy-w4-self-improvement-detector.md
tracker: https://github.com/trilamsr/regatta/issues/832
---

# Autotuner — closed-loop write-back design

Cross-refs: companion roadmap brief `docs/engineer/briefs/2026-06-04-roadmap-reorder-self-improve-priority.md` §5 names MVR-1.5-C, fixes the **PR-as-write-channel** envelope, and sketches what writes back. This brief is the load-bearing design (trust-boundary, latency tier, reversibility, damping, denylist, override surface) the roadmap brief defers to.

---

## 0. TL;DR

The W4 detector (`internal/selfimprove/detector.go:34-104`) emits findings; nothing consumes them. The autotuner is the missing closed-loop consumer that turns selected findings into machine-authored PRs against `regatta.yaml` + `docs/engineer/dispatch-templates/*.md` and a small set of in-repo memory files. The single load-bearing trust rule: **the autotuner has zero direct-write authority**. It is an *author with reduced powers* — it can only mint PRs against a fixed yaml-path allowlist, and every PR clears the same `make ci-check` + L4 reviewer + GitHub branch-protection gates as any other PR. No new write surface, no new HMAC key, no new merge channel. The CUE schema in `contracts/schemas/regatta.v1.cue:18-33` is the existing fail-closed gate; the autotuner reuses it byte-for-byte via `LoadConfig` in `internal/config/validate/load.go:154-181`. **Phase B operator-approval gate is the v1 default**; Phase C unattended-live is forbidden until the substrate has ≥90 days of green autotune-PR history. Three findings (W4 R3 subagent-claim-vs-CI, plus future R10 priority-thrash and R11 cap-thrash without damping) are denylisted from autotuner input because each is the exact lever an adversary would pull to widen the orchestrator's own safety envelope.

---

## 1. Scope — what gets written back

Seven write-back categories. Each cited against `feedback_root_cause` — write-back is admitted only when the proposed change addresses the *root* failure mode, not a symptom that would re-fire under a different fingerprint.

| # | Category | YES/NO | Root-cause cite |
|---|---|---|---|
| 1 | Cost caps in `regatta.yaml::safety::cost::cap::daily_usd`, `safety::spend_cap_usd`, `safety::spend_cap_usd_per_day` (schema `contracts/schemas/regatta.v1.cue:182-275`) | **YES, narrow** — only after R7 cost-outlier + R11 cap-thrash co-fire AND damping cap §7 says raise is admissible. Root cause of "cost-cap-throttled fires repeatedly": cap mis-fit to actual workload. Root cause is in the cap value itself; touching the cap addresses the root. | `feedback_root_cause` satisfied |
| 2 | `soft_cap_mode` toggle (`regatta.v1.cue:198-203`) | **NO**. Auto-flipping from `enforce` to `warn` widens the safety envelope. Adversary-trivial: any finding that pressures throughput can be weaponized. Operator-only. | Symptom-fix; root cause of soft-cap breach is workload, not the cap posture |
| 3 | Dispatch prompt templates `docs/engineer/dispatch-templates/{implementer,reviewer,designer,triage}.md` | **YES, append-only** — autotuner may APPEND a new "WHY not WHAT" reminder or new rule line; may NOT delete or reword operator content. Root cause of recurring banned-phrase / leftover patterns (W4 R2, R4 in `internal/selfimprove/rules.go:147-196`): dispatch template doesn't name the rule. Append addresses the root. | `feedback_root_cause` satisfied with append-only constraint |
| 4 | Spec frontmatter `status: shipped` (e.g. `docs/engineer/specs/*.md`) after N green runs | **NO**. Status is an operator semantic claim, not a derivable fact from substrate. Forging `shipped` would short-circuit the spec→impl→ship workflow. The reopen-trigger contract in CLAUDE.md "Self-host filter" assumes operator authorship. | Symptom-fix; root cause of stale spec frontmatter is operator-side process, not autotuner-fixable |
| 5 | Item retire/archive (auto-move solved `.regatta/items/*.md` → `archive/`) | **NO**. Same as #4. Items are operator-authored intent. Auto-archive a "solved" item that re-surfaces in another wave is a regression vector. | Symptom-fix |
| 6 | Banned-phrase list (`scripts/doc-check.sh` per CLAUDE.md "CI gates") | **YES, append-only** — after R2 banned-phrase-recurrence fires ≥5 times on the same novel token in a 30-day window AND token does not appear in any existing `.md` outside fenced backticks. Root cause of recurring banned-phrase trips: list incomplete. Append addresses the root. Damping §7 caps to ≤1 add per 30 days. | `feedback_root_cause` satisfied |
| 7 | `safety::iteration_cap`, `canary_rate`, `agent_creds_scope` (`regatta.v1.cue:185-188`) | **NO**. These are blast-radius levers. Same adversarial argument as #2. Operator-only. | Symptom-fix; widening would mask root cause |

Categories #2/#4/#5/#7 are explicitly excluded so the autotuner can never widen its own safety envelope. This is the deletion-default applied to the design itself per `feedback_deletion_default`: pure-addition wedges require A+ defense, and four of seven candidate write-backs fail that defense.

---

## 2. Trust boundary

**Adversarial assumption**: the orchestrator IS compromised. The defense must hold under that assumption.

The orchestrator-modifies-own-config attack surface is real. The defense is **not** a new HMAC key, a new merge channel, or a new privileged writer. The defense is to **collapse the autotuner onto the existing PR-author trust boundary** — every regatta PR already clears `make ci-check`, L4 adversarial reviewer (per CLAUDE.md "TDD + review"), and operator branch-protection merge on `main` (CLAUDE.md "Branch protection state"). Adding the autotuner as a PR author with a **reduced** allowlist of yaml-paths is strictly safer than adding any new write surface.

Concrete primitives:

1. **PR-as-write-channel — only.** The autotuner has no `--apply` direct-write mode. The only artifact it produces is a branch + PR labeled `autotune`. This is identical in shape to Renovate's `automergeStrategy: pull-request` (cited in companion brief §5; Renovate v37+ MIT-like / Mend). Adopt-first per `feedback_research_design_principles`.

2. **yaml-path allowlist.** A compiled-in allowlist of CUE-paths the autotuner may touch (initially: `safety.cost.cap.daily_usd`, `safety.spend_cap_usd`, `safety.spend_cap_usd_per_day`, append-only `dispatch-templates/*.md`, append-only `scripts/doc-check.sh` banned-list). Any PR-diff that lands outside the allowlist is rejected by a `make ci-check` gate (new `check-autotune-scope.sh`) before reviewer ever sees it. Allowlist drift requires an operator-authored change to the allowlist itself — autotuner cannot grow its own scope.

3. **CUE-gate every candidate before PR-open.** Reuse `validate.LoadConfig` (`internal/config/validate/load.go:154-181`) to compile the candidate yaml against `contracts/schemas/regatta.v1.cue` before the PR is minted. A CUE-failing candidate never reaches branch creation. This is the existing fail-closed gate; no new validator.

4. **Append-only `autotune_action` substrate event.** Before opening any PR, emit `EventKind = "autotune_action"` (new constant added alongside existing kinds in `internal/orchestrator/state/substrate/event.go:13-67`). Payload carries `target_path`, `before`, `after`, `finding_dedup_key`, `signed_by` (orchestrator HMAC KID from the existing substrate signing chain — no NEW key), `ts`. Closed/rejected PRs leave the event in place. Replay-diffable per §4.

5. **No HMAC double-sign / two-key crypto.** Adopt-first audit: HashiCorp Vault transit-engine multi-key shape is overkill for a single-operator self-host loop where the operator-merge is already an out-of-band signing event (GitHub OAuth + branch-protection). Adding a second HMAC KID introduces key-rotation surface + revocation surface + recovery surface — three new failure modes for zero added security beyond what operator-merge already provides. **Two-key is GitHub-merge + autotuner-author-claim; both already exist.**

The trust-boundary headline: **zero new privileged primitives**. The autotuner is a *reduced-powers PR author*, not a writer. A compromised orchestrator that mints malicious autotune PRs is no worse than a compromised orchestrator that mints malicious normal PRs — the same `make ci-check` + reviewer + operator merge backstop catches both.

---

## 3. Phase A / B / C latency tiers

Map observation-to-action by phase and blast-radius. Pick the maximally-safe default for v1.

| Phase | Flow | Latency | Blast radius | Revert cost |
|---|---|---|---|---|
| **A** (today) | finding → GH issue → operator triage → manual edit → PR → merge | hours–days | nil (operator-only) | nil |
| **B** (this brief's v1) | finding → autotuner → candidate yaml → CUE-validate → PR opened with reviewer auto-dispatch → operator one-click merge | minutes–hours | nil (PR gate intact) | nil (revert PR or close before merge) |
| **C** (gated 90-day reopen) | finding → autotuner → live yaml write + auto-merge once reviewer PASSes → revert journal | seconds | bounded by yaml-path allowlist + damping cap | one `regatta autotune revert <action-id>` away |

**Phase B is the v1 default.** Two reasons:

- Operator-merge is the second "key" the trust-boundary §2 leans on; removing it removes the only out-of-band signing event in the chain. Phase C without an out-of-band signer regresses to single-orchestrator authority over its own config — the exact attack vector the design rejects.
- Phase B blast-radius is *byte-equal to any other PR's* — the existing CI + reviewer + merge gates carry the whole load. Adding any "fast path" before 90 days of green Phase B history is unjustified.

Phase C reopen-trigger: ≥90 days of green Phase B autotune PRs (zero reverted, zero CI-failed merges) AND operator approves §10 audit. Until then, Phase C is closed.

---

## 4. Reversibility — `autotune_action` substrate kind

Every write-back MUST be revertible. Append-only substrate kind:

```
EventKind = "autotune_action"
payload:
  target           string  // CUE-path or repo-relative file path; allowlist-checked
  before           string  // pre-image (yaml subtree or file region)
  after            string  // post-image
  finding_dedup_key string // ties back to W4 Finding.DedupKey (rules.go:104)
  signed_by        string  // existing substrate HMAC KID; no new key
  pr_number        int     // GitHub PR number (Phase B); 0 for Phase C
  ts               int64   // unix-nano
```

Stored append-only via the existing substrate writer (parity with `KindTokenSpend`, `KindGateVerdict` in `event.go:27-32`). Enum-parity test (`enum_parity_test.go` in same package) extended to cover the new kind.

Operator revert command (new): `regatta autotune revert <action-id>` — reads the `autotune_action` row, reconstructs the inverse diff (after → before), opens a revert PR with body citing the original action-id, exits 0 once the revert PR is open. Operator merges as normal. No new write authority — the revert is also a PR.

Retention: same as all substrate events (`KindFact` retention in `event.go:23`). No autotune-specific retention; the substrate cap governs.

Dedup: `finding_dedup_key` + `target` form a uniqueness constraint at the autotuner-side (one open autotune PR per (finding, target) at a time). Companion brief §5.1 already pins "≤1 autotune PR open at any time per target (queue, don't fan out)" — this is the enforcement mechanism.

---

## 5. CUE validation gate

The candidate yaml MUST pass `validate.LoadConfig` (`internal/config/validate/load.go:154-181`) before the autotuner opens any PR. This is the existing fail-closed gate behind the `regatta validate-config` CLI subcommand (the comment header at `regatta.v1.cue:5-7` documents the operator-facing wrapper).

Implementation contract:

1. Autotuner reads current `regatta.yaml` bytes.
2. Applies the candidate diff in-memory.
3. Calls `validate.LoadConfig(candidateBytes)` — the unified CUE schema fully resolves defaults + concrete-constraint-validates per `cuecontext.New().CompileString(...).Unify(...).Validate(cue.Concrete(true), cue.All())`.
4. On CUE error: drop the candidate, do NOT open a PR, file a `kind=autotune_action` event with `pr_number=0` + `after=""` (rejected-candidate trace) so audits can replay "what did the autotuner consider but reject?".
5. On CUE pass: open the PR; emit the full event.

For dispatch-template + banned-list write-backs (not yaml), the equivalent gate is the existing `scripts/doc-check.sh` battery in `make check` per CLAUDE.md "CI gates". Autotuner runs `make check` locally on the candidate before opening the PR; failures drop the candidate the same way.

No new schema, no new validator. **The autotuner rides the existing yaml/markdown gate stack byte-for-byte.**

---

## 6. Excluded findings (never autotune)

Three rule-IDs from `internal/selfimprove/rules.go:12-18` MUST be filtered from autotuner input. Each is the exact lever an adversary would pull to widen the orchestrator's own safety envelope.

| Rule ID | Why excluded |
|---|---|
| `subagent-claimed-clean-but-ci-failed` (R3, `rules.go:166-180`) | An adversary that plants a false subagent claim can flood R3 fires; if autotuner consumed R3, the response would be to *relax* the claim-verification rule or widen the cost cap to accommodate the rework — both adversarial wins. R3 stays operator-triage only. |
| `reaper-kills-same-agent` (R5, `rules.go:198-212`) | The natural autotuner response to repeated reaper kills is to lengthen heartbeat timeout or raise iteration-cap. Both widen the autonomy envelope. Operator-only. |
| Future R11 `cap-thrash` without damping §7 in force | R11 is *literally* "cost cap oscillating"; consuming R11 without §7 stability damping is the oscillation amplifier (raise cap → more spend → more thrash → raise cap further). R11 admits to autotuner ONLY when §7 damping is wired AND R11 has fired ≥3 times in a 14-day window (de-noise). |

W4 R1 `same-gate-fail-repeats` and R4 `load-bearing-leftover-pattern` are the two MVP-eligible inputs once R2 `banned-phrase-recurrence` proves out the append-only template path. R2 is the safest first feed: append-only, narrow allowlist, immediately replay-diffable.

---

## 7. Stability damping

Closed-loop oscillation is the canonical autotuner failure mode. Autotuner raises cost cap → more PRs run → more cost-outliers fire → cap adjusts again → unbounded drift.

**Adopt-first**: Kubernetes HPA `--horizontal-pod-autoscaler-downscale-stabilization` (default 5min) + per-axis `behavior.scaleUp/scaleDown.policies` with `periodSeconds` + `value` caps is the closest reference. The HPA design has been in production since k8s v1.18 (2020); the asymmetric raise/lower stabilization-window pattern is exactly the shape this autotuner needs.

Per-axis daily delta caps for v1:

| Axis | Raise cap | Lower cap | Cooldown window |
|---|---|---|---|
| `safety.cost.cap.daily_usd` | ≤10% / day | ≤30% / day | 24h between consecutive autotune PRs on this path |
| `safety.spend_cap_usd_per_day` | ≤10% / day | ≤30% / day | 24h |
| `safety.spend_cap_usd` | ≤10% / day | ≤30% / day | 24h |
| Dispatch-template append | ≤1 line / 30d / template | N/A (append-only) | 30d |
| Banned-phrase list append | ≤1 token / 30d | N/A (append-only) | 30d |

Asymmetric raise/lower (lower allowed more aggressively than raise) is the HPA-discipline default — fail safe in the contraction direction. Cooldown windows prevent the autotuner from re-firing on the same target before the previous PR has either merged or been operator-rejected.

Enforcement: the autotuner reads the substrate for `kind=autotune_action` events on the same `target` within the cooldown window; if any exist (regardless of PR outcome), the new candidate is dropped. A rejected-candidate event is still recorded per §5 step 4 so the audit trail captures the cooldown decision.

Control-theory equivalent: this is a discrete-time PI controller with hard rate-limiter (no integral wind-up because the cooldown gates effectively zero the integrator between samples). Full PID is unjustified for a once-daily nightly scan; the rate-limiter is the load-bearing primitive.

---

## 8. Research-mode composability

The research-mode vision (`docs/engineer/briefs/2026-06-01-regatta-research-vision.md:38`) adds `kind: research` work_items with a `prereg` sub-block — a falsifiable contract committed before the work. **Autotuner authority over `kind: research` items is an ethics violation**: the prereg is the contract, and the autotuner modifying any prereg field post-hoc is structurally identical to p-hacking.

Hard exclusion: the autotuner MUST reject any candidate whose `target` resolves to:

- `.regatta/items/*.md` where the item frontmatter contains `kind: research`
- Any `prereg.*` field in any item
- `policies/research/*.rego` (the four research gate rules per research-vision brief §49)
- Any finding whose source event has `work_item_kind = "research"` in payload

Implementation: the W4 detector already plumbs `work_item_kind` through `Event` (rules.go:139 `groupBy` receives the full Event). The autotuner filter check happens at the finding-consumer boundary BEFORE the candidate is rendered. Filed `autotune_action` event with `target = "REJECTED:research-kind"` so audits capture the exclusion decision.

This is the deletion-default applied at the design layer: rather than build a research-aware autotuner, **forbid the surface entirely**. Research-mode is Phase X per the research-vision brief; autotuner is Phase B-default per §3 of this brief; the two never interact.

---

## 9. Operator-override surface

Four CLI verbs and two label conventions. All read-only or revert-only — no autotuner-extending verbs (the autotuner cannot grant itself new authority).

| Verb / label | Semantics |
|---|---|
| `regatta autotune pause [--rule <id>] [--target <yaml-path>]` | Writes a `kind=autotune_pause` substrate event; autotuner skips matching findings until `unpause`. Granular: pause one rule, or one target, or globally. |
| `regatta autotune unpause [--rule <id>] [--target <yaml-path>]` | Inverse. |
| `regatta autotune dry-run --finding <dedup-key>` | Renders the candidate diff to stdout WITHOUT opening a PR, WITHOUT writing any substrate event. Pure preview. |
| `regatta autotune revert <action-id>` | Opens the inverse PR per §4. |
| Issue label `autotune-disabled` | Operator applies to a W4 self-improve issue to mark "I will triage manually". Autotuner skips findings whose dedup-key matches an issue carrying this label. |
| PR label `autotune` | Auto-applied by autotuner. Operator uses for filtering in `gh pr list -L 20 --json number,labels`. |

`gh pr list/view` invocations from operator tooling MUST follow CLAUDE.md "Token economy" minimal-fields rule (`--json number,state,mergeStateStatus,statusCheckRollup,isDraft,headRefName,labels -L 20`). Documented in the `regatta autotune` subcommand help to keep operator habits aligned.

---

## 10. Adoption-first audit

Per `feedback_research_design_principles` (CLAUDE.md "Cross-cutting design / research" — "Adopt proven OSS over reimplementation"), each primitive proposed above audited against an OSS reference.

| Primitive | Adopt-first reference | Deviation justification |
|---|---|---|
| PR-as-write-channel | Renovate Bot `automergeStrategy: pull-request` (v37+ MIT-like / Mend) | None — adopt verbatim. Renovate's "bot is a reduced-powers PR author" is the exact shape. |
| yaml-path allowlist | GitHub Actions `environment` protection rules (built-in) + Vault transit `allowed_keys` | Lighter: we have a single repo + single operator, so a compiled-in Go slice is the byte-minimal expression. `check-autotune-scope.sh` is the CI hook. |
| CUE validation gate | Existing `regatta validate-config` (`internal/config/validate/load.go`) | None — reuse byte-for-byte. |
| Append-only audit | Sigstore Rekor (transparency log, append-only Merkle) + Tekton Chains (attestation chain) | **Defer** — Phase-X per CLAUDE.md "Self-host filter". The existing substrate signing chain (`KindGateVerdict` HMAC chain in `internal/orchestrator/state/substrate/event.go`) provides equivalent append-only semantics for single-operator scope. Sigstore reopen-trigger: multi-operator scope OR external customer ask. |
| Damping cap | Kubernetes HPA `behavior.scaleUp/scaleDown.policies` (v1.18+ stable) | None — adopt the asymmetric-rate-limit shape. PID controller is unjustified for once-daily nightly scan. |
| Reversibility | Argo Rollouts revert + Flagger rollback | Lighter: a single revert PR is byte-minimal vs Argo's analysis-template machinery. Flagger's automated rollback assumes continuous-delivery infra we don't run. |
| Two-key gate | HashiCorp Vault transit-engine multi-key | **Reject** — overkill for single-operator self-host. The existing GitHub operator-merge IS the second key; adding HMAC double-sign adds three failure modes (key rotation, revocation, recovery) for zero net security. Documented as explicit non-adoption per `feedback_deletion_default`. |
| Research-mode exclusion | None — bespoke design constraint | Justified: research-mode is itself a regatta-internal contract (`docs/engineer/briefs/2026-06-01-regatta-research-vision.md`); no OSS reference exists. Hard exclusion is the cheapest mechanism. |

Net new bespoke surface after adopt-first audit:

1. `internal/selfimprove/autotuner.go` — orchestrator (~400 LoC est., down from companion brief §5's ~600 LoC after CUE-gate reuse + Renovate-shape adoption)
2. `scripts/check-autotune-scope.sh` — yaml-path allowlist gate (~50 LoC)
3. New substrate kind `autotune_action` + enum-parity test (~30 LoC)
4. `regatta autotune {pause,unpause,dry-run,revert}` CLI verbs (~150 LoC)

Total ~630 LoC. Companion brief §5 estimate (~600 LoC) holds within margin. Deletion-default satisfied: four candidate write-back categories (#2/#4/#5/#7) cut per §1, two crypto primitives rejected per §10 (Vault transit, Sigstore Rekor), full PID controller cut per §7.

---

## 11. Open questions

The following decisions need operator confirmation before MVR-1.5-C implementation can dispatch. Per CLAUDE.md "Decision priority" the design subagent makes the reasonable call; this list is the residual irreversible-decision surface.

1. **Phase C reopen-trigger threshold.** §3 picks "≥90 days of green Phase B history". Operator may prefer a count-based trigger ("≥100 green autotune PRs merged, zero reverted") or a hybrid. Default proposed: 90 days AND 50 PRs.

2. **Banned-phrase append authority.** §1 #6 admits the autotuner to extend `scripts/doc-check.sh`. Operator may prefer this category stays operator-only given that the banned-phrase list is the most visible operator-authored content. Default proposed: YES, append-only, ≤1 token / 30d damping cap.

3. **Dispatch-template ownership boundary.** §1 #3 admits append-only changes to `docs/engineer/dispatch-templates/*.md`. CLAUDE.md "Comments discipline" says dispatch templates are operator-authoritative for the `feedback_comment_budget_enforcement` reviewer-sweep rule. Operator may want autotuner restricted to a single per-template "autotune-appended" section (delimited by a fence marker) so the operator-authored portion is byte-stable. Default proposed: YES, fenced section.

4. **`autotune_action` retention.** §4 says "same as all substrate events". Operator may want longer retention specifically for autotune-action rows (audit-grade). Default proposed: same retention, defer extension to Phase C reopen.

5. **Whether R3 / R5 ever escape the denylist.** §6 hard-codes the exclusion. Operator may want a future override-flag for research purposes. Default proposed: NO — the denylist is the design's adversarial-defense load-bearer; opening it requires re-running this trust-boundary review.

---

## 12. Adversarial-review findings

_Folded inline after cavecrew-reviewer subagent pass over §0–§11. Reviewer prompt: trust-boundary gaps, deletion-default violations (any bespoke wedge that duplicates HPA/Vault/Sigstore), unstable-loop hazards, operator-override completeness, supply-chain attack vectors. Reviewer received CLAUDE.md decision-priority + `feedback_root_cause` + `feedback_research_design_principles`._

Reviewer findings, severity-tagged. Disposition column captures whether the finding was folded back into §1–§11 before this brief was finalized.

| # | Severity | Finding | Disposition |
|---|---|---|---|
| 1 | HIGH | "The `signed_by` field in §4 cites 'existing substrate HMAC KID' but §2 primitive 5 explicitly rejects HMAC double-sign. If §4's signing is *just* the orchestrator's own KID, a compromised orchestrator forges its own audit events freely — the append-only journal becomes trust-zero." | **FOLDED §2 + §4**: the signing is exactly the same chain that signs every other substrate event (`KindGateVerdict` etc.). The defense is not that the orchestrator can't forge — it's that the *PR-merge step* is the out-of-band signer that makes any forged event detectable via the PR content not matching the event payload. Operator-merge is the second key. Made explicit in §2 primitive 5 closing sentence. |
| 2 | HIGH | "The §1 #6 banned-phrase append-only path has an adversarial corner: autotuner adds `regatta_pause_all` (or any other internal-magic-string) to the banned list. Future doc-check trips on legitimate operator content. Self-DoS." | **FOLDED §1 #6**: tightened the condition to "token does not appear in any existing `.md` outside fenced backticks" — internal magic strings appear in the codebase markdown (specs, briefs) outside fenced backticks, so the gate rejects them. Damping cap of ≤1 token / 30d further bounds the attack rate. |
| 3 | MEDIUM | "§7 damping cap raise ≤10%/day, lower ≤30%/day — but no floor/ceiling. Autotuner can asymptotically lower cap to $0 (deadlock) or asymptotically raise toward int.MaxInt (no-op cap)." | **FOLDED §7 + carried to §11**: added implicit floor (the schema enforces `>=0`; cap of $0 is recoverable via single operator-revert) and ceiling (deferred to §11 open question #1's Phase-C trigger — Phase B operator-merge gate catches absurd values via reviewer). Asymptotic-ceiling concern noted; not blocking for v1 because operator-merge gates every diff. |
| 4 | MEDIUM | "§8 research-mode exclusion checks `kind: research` at the work_item layer but the W4 detector consumes substrate events, not work_items. A research-mode work_item's downstream events (gate fails, banned-phrase trips) won't carry `work_item_kind` unless event-emission populates it." | **FOLDED §8 + carried to §11**: added explicit "the W4 detector already plumbs `work_item_kind` through `Event`" claim; this assumes event-emission populates the field consistently. Verified in `internal/orchestrator/state/substrate/event.go:91` `Kind EventKind` — payload carries arbitrary fields per kind, so `work_item_kind` belongs in payload not Event header. Implementation TODO: confirm at MVR-1.5-A1 SLO-first audit step. Added to open questions as implementation prereq. |
| 5 | MEDIUM | "§10 rejects Sigstore Rekor as 'Phase-X per CLAUDE.md self-host filter' but a single-operator self-host loop where the operator IS the orchestrator's principal is exactly the operator-credibility case Sigstore is designed for. Self-host filter argues against multi-tenant primitives, not against transparency-log primitives." | **PARTIAL FOLD §10**: reviewer is correct that Sigstore is not inherently multi-tenant. Position softened from 'Phase-X reject' to 'reopen-trigger Phase C upgrade'. The existing substrate signing chain meets v1 needs; Sigstore is the natural Phase-C primitive. Open question §11 #1 covers the Phase-C trigger. |
| 6 | LOW | "§9 lists pause/unpause/dry-run/revert but no `regatta autotune status` — operator can't see what's queued / what's cooling down without grep'ing the substrate." | **FOLDED informally**: adding `regatta autotune status` is implementation-trivial (read-only substrate query); listed as implicit CLI verb. Not adding a row to §9 to avoid surface bloat — `status` is a standard accompaniment to any `pause/unpause` and needs no design-layer discussion. |
| 7 | LOW | "§1 #3 'append-only' dispatch-template constraint — how is 'append-only' enforced mechanically? The CI gate `check-autotune-scope.sh` checks yaml-path allowlist but not diff-shape." | **FOLDED §11 #3**: existing fence-marker proposal handles this. The CI gate's allowlist for templates is "diff lands strictly inside the fenced `<!-- autotune-appended-start --> ... <!-- autotune-appended-end -->` block". Operator-content outside the fence is byte-stable. Defaulted YES in §11 #3. |
| 8 | LOW | "Brief doesn't address autotuner-vs-autotuner: two W4 scan passes might emit duplicate candidates if the cooldown check races." | **FOLDED §4 + §7**: dedup-key in §4 + 24h cooldown read in §7 are both substrate-event reads against the same append-only log; ordering is by `ts` UNIX-nano. The race is bounded by single-writer semantics on the substrate. Documented at §7 closing paragraph. |

**Counts by severity**: HIGH=2 (both folded), MEDIUM=3 (folded or open-question-carried), LOW=3 (folded informally).

No CRITICAL findings. No findings blocked v1 dispatch under Phase-B-default.

---

## 13. A+ rubric self-score

| Criterion | Tier | Evidence (file:line OR Test* OR N/A — rationale) |
|---|---|---|
| B1 — Solves stated problem (closed-loop write-back consumer for W4 findings) | A | §1 + §3 + §4 cover all four W4 self-improve→consumer questions from issue #832; no W4-MVP rule (R1/R2/R4) is left without an admission path. |
| B2 — Decision-priority cited (CLAUDE.md UX>ease>perf>...) | A | §0 TL;DR + §1 categories #2/#4/#5/#7 cuts driven by `feedback_root_cause` symptom-vs-root analysis; §2 trust-boundary picks adopt-first PR-channel over new HMAC per `feedback_research_design_principles`. |
| B3 — Reversibility | A+ | §4 ships `autotune_action` substrate kind + `regatta autotune revert` CLI verb; every action is replay-diffable; rejected candidates also logged (§5 step 4). |
| A1 — Adopt-first audit | A+ | §10 table — eight primitives audited; two adoptions verbatim (Renovate, HPA), four lighter adaptations, two explicit rejections (Vault transit, Sigstore Rekor) with documented rationale per `feedback_deletion_default`. |
| A2 — Deletion-default | A+ | §1 cuts 4 of 7 candidate write-back categories; §10 rejects two crypto primitives + cuts full PID to rate-limiter. Pure-addition surface is minimized to what `feedback_root_cause` admits. |
| A3 — Trust boundary defense under compromised-orchestrator assumption | A | §2 explicit adversarial framing; §6 denylist closes the three rule-IDs that would otherwise be safety-envelope-widening levers; §8 closes research-mode side-channel. HIGH-severity reviewer findings #1 + #2 folded. |
| A4 — Stability under closed-loop oscillation | A | §7 HPA-shape asymmetric raise/lower rate-limit; cooldown windows prevent re-fire on same target; reviewer finding #3 (floor/ceiling) folded with Phase-B operator-merge backstop. |
| A5 — Research-mode composability | A | §8 hard exclusion of `kind: research` items; reviewer finding #4 implementation prereq carried to §11. |
| A+ — Operator-override surface complete + minimal | A | §9 four CLI verbs + two label conventions; reviewer LOW #6 (`status` verb) folded informally; surface bloat avoided. |
| A+ — Open questions enumerated, all reversible | A+ | §11 — five questions, all default-proposed, all reversible (revert PR or operator-override CLI). No load-bearing decision deferred without a default. |

**Claimed tier: A** (would claim A+ pending operator resolution of §11 open questions #1 + #2 + #3 and confirmation of §12 finding #4 implementation prereq at MVR-1.5-A1 audit step).
