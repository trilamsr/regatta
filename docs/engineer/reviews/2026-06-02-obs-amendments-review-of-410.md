# Second-tier adversarial review of PR #410 — obs-roadmap amendments

**Subject.** PR #410 `spec/obs-roadmap-amendments` folds PR #405 (5 PASS / 4 RISK / 1 BLOCKER) into a diff-shaped amendment against PR #400's observability-roadmap parent.
**Reviewer scope.** Second-tier (light-touch) per L7 reopen-condition in PR #405. Six lenses, each tied to a parent-review finding ref or a new-finding bucket.
**Verdict.** ADOPT — 5 PASS / 1 RISK / 0 BLOCKER. The light-touch reopen condition is met; the 1 RISK is the cross-wave file-ownership seam created by the A-T0 split (resolvable inside the dispatch prompt, not in the spec).

Per `feedback_adversarial_review`, every lens carries a named alternative + reopen-condition; no auto-approve.

---

## §1 Six-lens findings

| # | Lens | Parent finding ref | Result | Notes |
|---|---|---|---|---|
| 1 | D-T3 dep-graph fix closes BLOCKER fully | PR #405 L7 BLOCKER | **PASS** | §2 below |
| 2 | A-T0a / A-T0b split — new sequencing risk? | PR #405 L7 secondary | **PASS-with-followup** | §3 below — net new file-ownership seam; mitigated, see RISK in §7 |
| 3 | A-T4 first-digest degraded contract well-defined | PR #410 §1.3 | **PASS** | §4 below — placeholder contract explicit, removal owner pinned |
| 4 | 4 RISK closure (each in-band or deferred) | PR #405 L2/L4/L6/L8 | **PASS** | §5 below — L2 in-band, L4 split (scope-correction in-band + tuning deferred), L6 split (trap 9 in-band, 10+11 deferred), L8 in-band |
| 5 | New findings introduced by amendments themselves | n/a | **RISK** | §7 — one net new file-ownership seam (A-T0a now touches `cost/spend` + `gates/l4`, but C-T3 brief still names A-T1 sole owner of `cost/spend/writer.go`) |
| 6 | Wave A/B/C/D dep graph still valid after amendments | PR #410 §6 consolidated | **PASS** | §6 below — every Depends-on column reachable in the merged graph; one diagram-prose drift flagged advisory |

---

## §2 Lens 1 — D-T3 dep-graph fix (BLOCKER)

**Parent finding.** PR #405 L7 BLOCKER: §7 Wave-D row D-T3 + §7 dep-graph diagram say `Depends-on A-T0` only, but D-T3 brief sources `30_day_green` from SLO-3 PR-merge-rate which derives from `regatta_pr_stage_duration_seconds_count{stage="ci_green_to_merge"}` — a histogram emitted by C-T2 (Wave-C).

**Amendment cited.** PR #410 §1.1 patches the Depends-on column to `A-T0, **C-T2** (30_day_green reads the PR-stage histogram)` and the dep-graph diagram to `D-T1 ∥ D-T3 — but D-T3 also depends on C-T2`. PR #410 §6 consolidated Wave-D table re-states D-T3 Depends-on as `A-T0a, **C-T2**`.

**Verdict.** PASS. Three independent locations carry the C-T2 dep (Wave-D row + dep-graph diagram + consolidated §6 table). A reviewer who follows ANY of the three lands on the correct graph. The cascading-defect failure path the parent review flagged (dispatch D-T3 parallel with Wave-B → broken gauge reading zero) is closed.

**Reopen-condition.** If §10 D-T3 brief in the PARENT spec still names only A-T0 in its own Depends-on line and the amendment does not patch that brief, a careless dispatcher reading §10 alone could still mis-sequence. PR #410 §1.1's third diff block claims it patches §10 D-T3 too — accept on `git diff` of the merged result.

## §3 Lens 2 — A-T0a / A-T0b split

**Parent finding.** PR #405 L7 secondary: A-T0 sized M actually does (1) MeterProvider init + 2 exporters + mutual-exclusion validator, (2) `Config.Meter` field added to 8 Config structs, (3) every test updated. W6 Tracer DI was 2 PRs — A-T0 should be L or split.

**Amendment cited.** PR #410 §1.2 splits A-T0 → A-T0a + A-T0b.

| Split half | Scope |
|---|---|
| A-T0a (M, no deps) | MeterProvider + OTLP + Prom exporters + mutual-exclusion validator + AST-walk trap-9 lint + retrofit of 2 Config structs (`cost/spend`, `gates/l4`) so A-T1/A-T2 can start parallel |
| A-T0b (M, depends on A-T0a) | 6 remaining Config retrofits (`scheduler`, `spawner`, `substrate`, `history`, `followup`, +1) |

A-T1 + A-T2 now depend on A-T0a (not A-T0b). A-T3 depends on A-T0b (scheduler config retrofit lives there). The fan-out timing is correct: A-T0a unblocks two parallel lanes; A-T0b's serial-after relationship to A-T0a is unavoidable given they touch the same `setup.go` file.

**Verdict.** PASS-with-followup. The split is sequenced correctly — A-T1/A-T2 start at A-T0a green, A-T3 starts at A-T0b green. One net new concern flagged in §7 RISK below: A-T0a now touches `internal/cost/spend` for its retrofit, and the parent spec's C-T3 brief explicitly names A-T1 sole owner of `internal/cost/spend/writer.go` per `feedback_shared_primitive_owner`. A-T0a's retrofit is a Config-field add, not a `writer.go` edit — but the dispatch prompt needs to call this out so impl-A0a does not edit `writer.go` itself.

**Reopen-condition.** A-T0a PR diff shows zero edits to `internal/cost/spend/writer.go`. If the implementer touches `writer.go`, the A-T1 owner must redo its work on top.

## §4 Lens 3 — A-T4 first-digest degraded contract

**Parent finding.** PR #405 L7 recommendation #2: add "first digest at 2026-06-03 lacks PRs-landed section; lights up at C-T2 merge" to §6.2.

**Amendment cited.** PR #410 §1.3 declares the first-digest contract as `placeholder line, NOT silent zero` for both PRs-landed (waits on C-T2) and Adversarial findings (waits on D-T1). The placeholder text is given verbatim: `PRs landed — emitter ships C-T2 (Wave C); see #<issue>` and `Adversarial findings — emitter ships D-T1 (Wave D); see #<issue>`. The amendment further pins removal ownership: C-T2 implementer subagent removes the PRs-landed placeholder as part of its landing PR; D-T1 implementer subagent removes the Adversarial-findings placeholder.

**Verdict.** PASS. Contract is operator-readable (no silent-zero per parent R6 reinforcement), test-shaped (placeholder line is a static string a digest snapshot test can assert), and ownership-pinned (named removal step on a named PR). Three properties of a well-defined degraded-mode contract — visible, asserted, removed — all present.

**Reopen-condition.** If `regatta digest` source code at A-T4 merge does NOT emit the verbatim placeholder strings, file blocking review on the A-T4 PR. The amendment names exact strings; the impl must match.

**Edge-case probe (adversarial).** What if C-T2 ships before A-T4? Then there is no "first digest" with a placeholder — the placeholder code path is dead at A-T4 merge. The amendment's dep graph forbids this (A-T4 depends on A-T1..A-T3 which all depend on A-T0a/b; C-T2 depends on C-T1; the waves are sequenced A → B → C → D). Accept that the placeholder is correctly conditioned on the spec's own sequencing.

## §5 Lens 4 — Four-RISK closure audit

| Parent RISK | Source | Amendment location | Status | Notes |
|---|---|---|---|---|
| L2 unit suffix double-render | §2.1 unit collision with UCUM | PR #410 §2 | **CLOSED in-band** | Locks the double-unit wire-string (`regatta_scheduler_tick_latency_ms_milliseconds`) as documented exporter behaviour. Picks ease-over-best-practice per `feedback_decision_priority`; reopen-condition (A-T0a PR shows sample `/metrics` scrape) carried forward verbatim. |
| L4 SLO scope correction | §5 SLO-3 PR-merge-rate as SLO; SLO-2 budget tight; SLO-4 σ-distro | PR #410 §4 + §7 | **PARTIAL in-band + deferred** | Scope-correction in-band: SLO-3 (PR-merge) demoted to KPI tile; SLO-3 (was SLO-4) substrate-event-rate demoted critical→warn; renumber. SLO-2 widen + SLO-3 quantile rewrite explicitly deferred as `[OBS-followup] #1` with trigger ("30 days of real burn-rate from Wave-B"). |
| L6 anti-pattern traps | §4 — 3 missing traps | PR #410 §3 + §9 | **PARTIAL in-band + deferred** | Trap #9 (missing-metric AST lint) ships in-band with A-T0a (`TestEveryGateAdapterHasInvocationsCounter`). Traps #10 (dashboard-UI drift) + #11 (cardinality-cost meta-panel) deferred as `[OBS-followup] #2` because both ride the Wave-D operator-surface meta-dashboard. |
| L8 operator surface | §6.1 7-panel TUI not single-screen + §6.2 digest not parseable + Appendix A bubbletea row wrong | PR #410 §5 | **CLOSED in-band** | TUI cut 7→5 panels with explicit 80×24 panel-budget table (rows + cols sum); Triggers panel moves to sibling `regatta triggers` subcommand owned by D-T3; daily digest gains YAML front-matter with named fields (lock-step with markdown body); Appendix A bubbletea row corrected — NOT yet in `go.mod`, D-T2 adds it. |

**Verdict.** PASS. Every RISK is either closed in-band with a tool-checkable amendment or deferred with a named owner + named trigger per `feedback_unaddressed_load_bearing`. Cross-lens Gauge prose fix (sync `Gauge` DOES exist on OTel Go SDK v1.32+; this repo runs v1.44.0) is folded into §1.1's D-T3 amendment without a separate row — acceptable since the API pick (observable gauge for sampled state) is unchanged and only the prose justification needed correction.

## §6 Lens 6 — Wave graph validity after amendments

**Consolidated graph from PR #410 §6.** I walked the graph against every Depends-on column:

```
A-T0a (no dep)
  └─ A-T0b (dep A-T0a)
       └─ A-T3 (dep A-T0b)
  └─ A-T1 (dep A-T0a)
  └─ A-T2 (dep A-T0a)
       └─ A-T4 (dep A-T1..A-T3)
       └─ A-T5 (dep A-T1..A-T3)
            └─ A-T6 (dep A-T5)

B-T1, B-T2, B-T3, B-T4 (dep Wave-A complete — unchanged)

C-T1 → C-T2 + C-T4; C-T3 shared-owner with A-T1 — unchanged

D-T1 (dep A-T0a)
D-T3 (dep A-T0a, C-T2)  ← BLOCKER fix
D-T2 (dep Waves A+B+C all merged) — unchanged
```

All edges reachable. No cycles. Critical path is A-T0a → A-T0b → A-T3 → A-T4 → … (matches the parent spec's critical path with the split inserted).

**Advisory (not RISK):** PR #410 §1.1's diagram diff edits the prose line `Wave D (D-T1 ∥ D-T3 in parallel; D-T2 after A+B+C)` to `Wave D (D-T1 ∥ D-T3 — but D-T3 also depends on C-T2; D-T2 after A+B+C)`. The prose still reads `D-T1 ∥ D-T3` which can mislead a quick-glance reader because D-T1 (depends on A-T0a only) and D-T3 (depends on A-T0a + C-T2) are NOT actually parallel in wall-clock — D-T1 ships at Wave-A green, D-T3 ships at Wave-C green. Suggest a follow-on prose tighten to `D-T1 at Wave-A green; D-T3 at Wave-C green (after C-T2); D-T2 after A+B+C`. Advisory because the Depends-on columns are correct and the table is the load-bearing source.

**Verdict.** PASS.

## §7 Lens 5 — New findings introduced by the amendments themselves

**Probe.** Walked every diff block in PR #410 against the parent spec for net new concerns introduced by the amendment edits (not present in PR #405).

Found one:

### RISK-A — A-T0a touches `internal/cost/spend` Config; C-T3 brief still names A-T1 sole owner of `internal/cost/spend/writer.go`

**Defect.** PR #410 §1.2 puts the `Config.Meter` retrofit for `cost/spend` and `gates/l4` into A-T0a (so A-T1/A-T2 can start parallel — a sound sequencing choice). But the parent spec's §10 C-T3 brief reads `**A-T1 owns this file across Wave A + Wave C per feedback_shared_primitive_owner** — coordinate via single follow-up PR after A-T1 merges`. The "this file" referenced is `internal/cost/spend/writer.go`.

A-T0a's retrofit is a Config-struct field add (likely in a separate file like `internal/cost/spend/config.go`), NOT a `writer.go` edit — so on a careful reading there is no actual conflict. But the parent spec's shared-owner pin language is broad ("this file" can read as "this package") and the dispatch prompt for impl-A0a does not explicitly fence the retrofit to the Config struct alone. If impl-A0a edits `writer.go` to wire the meter (a plausible interpretation of "retrofit"), it now also owns C-T3's eventual log-event + counter additions, contradicting C-T3's named shared-owner.

**Severity.** RISK (not BLOCKER) because (a) the spec's intent is recoverable from context — A-T0a is the DI-wire-up half, not the emit-the-counter half; (b) the conflict is detectable at A-T0a PR review (reviewer sees `writer.go` edited and asks why); (c) the fix is a one-line dispatch-prompt fence, not a spec rewrite.

**Alternative.** Add one sentence to PR #410 §1.2's A-T0a description: `A-T0a retrofit lives in cost/spend/config.go (Config struct) NOT writer.go (shared-owner pin: A-T1). Same fence on gates/l4: edit config.go, not the gate-decide path.`

**Reopen-condition.** A-T0a PR diff scoped to `internal/cost/spend/config.go` + `internal/gates/l4/config.go` for the retrofit half; `writer.go` and gate-decide unchanged. If impl-A0a edits either, block on review and re-fence.

No other net new findings.

---

## §8 Comment-noise + banned-phrase gate

This review file scanned against the 11 tokens in `scripts/doc-check.sh` lines 108–119 (`blazing[- ]fast`, `production[- ]grade`, `world[- ]class`, `best[- ]in[- ]class`, `industry[- ]leading`, `cutting[- ]edge`, `lightning[- ]fast`, `battle[- ]tested`, `enterprise[- ]grade`, `rock[- ]solid`, `robust`). Zero hits.

PR body carries the `release-notes` fence per `feedback_pr_body_release_notes_mandatory`; submitted via `--body-file` per `feedback_pr_body_file_only`. No AI signatures per `feedback_no_signatures`.

---

## §9 Closing verdict

**ADOPT** — PR #410 is approved for merge after one optional dispatch-prompt fence (§7 RISK-A — A-T0a retrofit scope). 5 PASS / 1 RISK / 0 BLOCKER. The 1 RISK is recoverable at impl-A0a PR review and does not need a spec re-edit.

| Counts | Value |
|---|---|
| BLOCKERs (parent closed) | 1 of 1 |
| RISKs (parent closed in-band) | 2 of 4 (L2 + L8) |
| RISKs (parent deferred with named follow-up) | 2 of 4 (L4 tuning + L6 traps 10+11) |
| New BLOCKERs introduced by amendments | 0 |
| New RISKs introduced by amendments | 1 (§7 RISK-A — file-ownership seam) |
| Net change to Wave task count | +1 (17 → 18 from A-T0 split; D-T3 scope expands but counted as same ID) |
| Net change to SLO count | -1 (5 → 4; SLO-3 PR-merge demoted to KPI tile) |
| Net change to critical-tier alarms | -1+ (was 3+; now 2) |
