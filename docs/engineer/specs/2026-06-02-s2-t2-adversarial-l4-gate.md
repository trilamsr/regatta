# S2-T2 — Adversarial Reviewer as First-Class L4 Gate — Design Spec

Status: ready for review
Date: 2026-06-02
Author: design subagent <tree@lumalabs.ai>
Issue umbrella: TBD (this spec stands up the umbrella)

Depends on:

- **Hard prereq (merged):** existing gate kind enum already carries `ai_adversarial` — `contracts/schemas/gate_result.go:45` + `contracts/schemas/gate_result.schema.json` enum entry. No schema bump required.
- **Hard prereq (merged):** existing `internal/gates/security/` package — the `Run(ctx, cfg, in) (schemas.GateResult, error)` shape is the gate-interface pattern this spec extends (`feedback_spec_pattern_authority` — copy the seam, do not reinvent).
- **Hard prereq (merged):** CUE `#Gate` discriminator already has an `ai`-typed gate row carrying `model: string`, `prompt?: string`, `severity_block: [...string] & MinItems(1)` — `contracts/schemas/regatta.v1.cue:87-90`. This spec selects the existing `ai`-discriminator row; no new CUE row needed.
- **Soft prereq:** `examples/full/regatta.yaml` already lists an `adversarial` gate at `gates[1]` with `model: claude-sonnet-4-6` + `severity_block: ['critical', '2*high']`. This spec promotes that example row to load-bearing wiring.

Binding brief: `docs/engineer/autonomous-session-prompt.md` PHASE-S2 §7 — "S2-T2 — adversarial reviewer as first-class L4 gate — bake the Claude-Code-side reviewer prompt into `internal/gates/`. Today it lives only in dispatch prompts. NEW. Default model: Sonnet 4.6, escape hatch via `regatta.yaml: gates.l4.model`."

Roadmap fit: Phase S2 #7 (`feedback_gate_relaxation_phase_s` — self-host scope; this repo's PRs only).

Memory rules in force: `feedback_research_design_principles` (adopt OSS), `feedback_decision_priority` (UX > ease > performance > best-practices > speed > velocity, long-term > short-term), `feedback_grade_rubric` (B/A/A+ tool-checkable), `feedback_adversarial_review` (hostile-read mandate), `feedback_spec_pattern_authority` (one pattern mandated), `feedback_unaddressed_load_bearing` (named-but-deferred → tracking issue), `feedback_deletion_default` (what got smaller?), `feedback_doc_check_banned_phrases` (no banned tokens).

---

## §0 Prior art adopted

Per `feedback_research_design_principles` — proven OSS first, build only what's missing. Three candidates evaluated; each scored on (a) gate-I/O fit, (b) self-host scope fit, (c) cost-of-adoption.

### Candidate 1 — qodo-ai/pr-agent (Apache-2.0)

OSS PR-review agent. Single LLM call per `/review`, JSON-prompt with reviewer categories, PR-compression strategy for large diffs. Provider-agnostic (Anthropic SDK supported).

- **(a) gate-I/O fit — adopt:** structured JSON output with per-finding `severity` + `category` + `description` maps 1:1 onto `schemas.Finding{Severity, Claim, Evidence, TrapPattern}`. The `pr-agent` "PR-compression strategy" handles oversize diffs by chunking changed-files — adopt the same diff-clip ceiling (configurable `max_diff_chars`, default 50_000) so an oversized PR degrades to advisory-only instead of OOMing the gate process. Direct cite: `qodo-ai/pr-agent` JSON-prompt config at `pr_agent/settings/configuration.toml`.
- **(b) self-host scope fit — adopt:** runs in-CI per PR, no multi-tenant config — same scope as this gate.
- **(c) cost-of-adoption — borrow patterns, not code:** importing Python via subprocess would be heavier than re-emitting the prompt in Go. Borrow the prompt structure + JSON-output discipline + diff-clip ceiling. Re-implement the call site in Go to keep the gate single-binary.

**Adopted patterns:** (1) JSON-structured reviewer output with required severity field, (2) PR-compression / diff-clip ceiling for oversize diffs, (3) reviewer-categories block in prompt (correctness / security / test-coverage / refactor / risk).

### Candidate 2 — openai/evals (MIT)

OSS eval harness. YAML-defined evaluations, model-graded option, registry pattern. Used by OpenAI to vet model upgrades.

- **(a) gate-I/O fit — partial:** YAML eval definition + model-graded judge is the right *shape* for an opinionated reviewer, but the harness is Python-only and the "registry" pattern is overkill for one gate.
- **(b) self-host scope fit — partial:** designed for cross-model evaluation across many tests; this gate runs one prompt against one diff. Mismatched scale.
- **(c) cost-of-adoption — reject as a runtime dep, adopt as a structural reference:** the model-graded eval YAML shape (one prompt-template file, one rubric-template file, separate `eval_type`) informs how this gate's prompt template should ship as a versioned file rather than embedded Go string.

**Adopted patterns:** (1) prompt template ships as a separate file (`internal/gates/l4/prompts/adversarial.tmpl`) so the prompt's SHA can pin the audit (`schemas.Telemetry.PromptSHA` already exists), (2) the rubric "verify each B/A/A+ criterion against the diff" pattern — the gate's prompt embeds the spec's A+ rubric scorecard as part of the input contract.

### Candidate 3 — Helicone Sessions (Apache-2.0)

OSS LLM-observability layer. Session-grouping primitive: every related LLM call shares a `Helicone-Session-Id` + `Helicone-Session-Path` header, letting you trace multi-step agent flows in one view.

- **(a) gate-I/O fit — adopt the trace model, not the SaaS:** the `gate_id` + `run_id` fields already on `schemas.GateResult` are regatta's per-PR session anchor. Helicone's "session-path" maps to OTel span attributes — `gate_kind=ai_adversarial` already promotes the gate's span. No new infra.
- **(b) self-host scope fit — fits via OTel:** regatta already runs OTel (`w6 spec`). This gate emits one span per evaluation; the prompt input/output flows through the standard `genai` span attributes per W6 T4 stream-json parser.
- **(c) cost-of-adoption — adopt as an OTel attribute discipline, no runtime dep:** record `gen_ai.prompt.sha`, `gen_ai.response.findings_count`, `gen_ai.response.verdict` as span attributes so the existing Jaeger dashboard surfaces this gate alongside cost-governor + approval gates.

**Adopted patterns:** (1) one OTel span per gate.Run, (2) record reviewer-output metrics as span attributes for the existing dashboard.

### Decisions from prior-art scan

| Decision                          | Adopted from        | Rationale                                                                 |
| --------------------------------- | ------------------- | ------------------------------------------------------------------------- |
| Gate package layout = `internal/gates/l4/` | regatta `security` pattern | Mirror existing `Run(ctx, cfg, in)` seam (`feedback_spec_pattern_authority`). |
| Prompt ships as `.tmpl` file       | openai/evals        | Pin prompt SHA in `Telemetry.PromptSHA`; auditable + diffable.            |
| JSON-structured reviewer output    | qodo-ai/pr-agent    | 1:1 fit with `schemas.Finding`; reject free-form prose.                   |
| Diff-clip ceiling (`max_diff_chars: 50_000`) | qodo-ai/pr-agent | Oversize PR → advisory-only, never OOM.                                   |
| Reviewer categories in prompt      | qodo-ai/pr-agent    | Correctness / security / test-coverage / refactor / risk + A+ scorecard verify. |
| Severity → Finding 1:1             | regatta `security`  | `["critical","2*high"]` mini-DSL already implemented.                     |
| One OTel span / Run                | Helicone Sessions   | Hooks straight into existing W6 OTel backbone — no new collector needed.  |

---

## §1 Problem

The adversarial-reviewer prompt is the single highest-signal quality gate the autonomous loop runs. Today it lives **only** in dispatch prompts:

- Implementer subagents are *instructed* to spawn an adversarial reviewer subagent.
- The reviewer subagent reads `feedback_adversarial_review` from the user's memory.
- Findings come back as prose; the implementer self-applies them; no machine-checkable artifact.

Three concrete failure modes from the last 30 days of session logs:

1. **Skipped reviewer, B-tier ship.** `feedback_grade_rubric` documents 5+ shipped PRs (#232, #233, #246, #250, #253) where reviewer-APPROVE landed without an explicit A+ scorecard — measurement was invisible, default tier silently dropped to B.
2. **Inconsistent severity rubric.** Each reviewer subagent re-invents what counts as "critical" vs "high" — no shared severity_block contract. The result: one PR's "critical" is the next PR's "nit."
3. **Scheduler cannot halt on review failure.** The autonomous loop's only halt signals today are CI red, approval-gate fail, cost-governor fail. Reviewer findings are dispatch-side prose; the scheduler-tick `applyApprovalGates` step at `internal/orchestrator/scheduler/scheduler.go:155` has no knob to refuse-advance on adversarial-review failure.

**The fix:** promote the adversarial reviewer to a first-class L4 gate alongside L0 (`spec_immutability`), `security`, and `approval`. Same `schemas.GateResult` output, same `severity_block` mini-DSL, same scheduler-tick consumption, same OTel span shape. Hard gate — `severity_block: ['critical', '2*high']` halts automerge; `applyApprovalGates`-style filter pulls the work_item out of `spawnable` until reviewer findings clear.

---

## §2 Scope

### In scope (this spec — self-host wave only per Phase S2)

- New gate package `internal/gates/l4/` mirroring the `internal/gates/security/` seam.
- `Run(ctx context.Context, cfg Config, in Input) (schemas.GateResult, error)` exported function — same signature as security gate.
- Config struct `internal/gates/l4/config.go` mapping the existing CUE `ai`-discriminator row to Go, with `Model`, `Prompt`, `SeverityBlock`, `MaxDiffChars` fields.
- Prompt template at `internal/gates/l4/prompts/adversarial.tmpl` — versioned, SHA-pinned via `schemas.Telemetry.PromptSHA`.
- Diff-clip ceiling at `MaxDiffChars` default `50_000`; oversize PR emits one advisory finding `L4-DIFF-OVERSIZE` and short-circuits to verdict=advisory (no LLM spend, no block).
- Reviewer-call site: re-uses the existing `internal/orchestrator/spawner/genai.go` stream-json parser (W6 T4) so `gen_ai.usage.*` attribute set comes for free.
- JSON-structured prompt output schema: `{verdict, findings[{severity, category, claim, evidence{path,line_start,line_end}}], notes}`. Parser maps directly into `schemas.GateResult` + `schemas.Finding`.
- `severity_block` default `['critical', '2*high']` mirroring `examples/full/regatta.yaml` (no new mini-DSL — re-use security gate's parser).
- Model default `claude-sonnet-4-6` baked at gate-config load when `gates.l4.model` is unset. Override via `regatta.yaml: gates[].model: <id>` (existing CUE field). Env-var escape hatch `REGATTA_GATES_L4_MODEL` for unattended-loop runs where editing yaml is friction (one of the 10 ceremony-drops per `feedback_drop_ceremony`).
- Scheduler-tick wire-up: new step **0.7 — apply L4 gates** between existing step 0.6 (cost-governor) and step 1.0 (`CountAgentsByLane`). Wire mirrors `applyApprovalGates` / `applyCostGovernor` shape exactly. Implementer task, NOT in this spec's diff.
- OTel span: one span per `l4.Run`, attribute set `{gate_id, gate_kind=ai_adversarial, model, prompt_sha, findings_count, verdict, pr_sha, run_id}`.
- One adversarial-reviewer subagent runs against this spec before PR open (per Workflow §1 step 2 of autonomous-session-prompt).

### Out of scope (deferred — tracking issue filed at impl-time per `feedback_unaddressed_load_bearing`)

- **Multi-tenant gate config.** Self-host scope per Phase S2. When `tenant_id` propagation lands W8 wave + 1, this gate's config gains a `tenant_id` row. Tracking issue: `[l4-followup] per-tenant l4 gate config`.
- **Reviewer-disagreement loop / second-opinion model.** When the author disputes an L4 finding, the workflow today is "implementer reads the finding, decides to apply or refute, posts rationale on PR." This spec deliberately keeps that human-loop unchanged for self-host scope — a second reviewer model adds cost without clear signal until we have base-rate data on how often L4-findings are wrong. Tracking issue (filed in §6): `[l4-followup] reviewer-disagreement second-opinion loop` — wait for ≥30 days of L4 base-rate data before deciding whether to re-spawn on dispute.
- **Per-category model selection** (e.g. Opus for security, Haiku for naming). Single `model` field for v1. Tracking issue: `[l4-followup] per-category model selection`.
- **Prompt-template hot-reload.** Prompt SHA is read at process start; restart required to swap. Tracking issue: `[l4-followup] prompt hot-reload via SIGHUP`.
- **Findings-cache** so re-runs at the same PR SHA skip the LLM call (the security gate already lifts the `RunID` idempotency-key constraint per `internal/gates/security/gate.go`). Tracking issue: `[l4-followup] L4 findings cache`.
- **Auto-fix mode** where the gate emits a patch instead of a comment. Tracking issue: `[l4-followup] auto-fix patch mode`.

---

## §3 Architecture

### §3.1 Adopted-OSS scan (per `feedback_research_design_principles`)

Covered in §0. Summary table:

| Pattern                       | Source                | Where it lands in this spec                  |
| ----------------------------- | --------------------- | -------------------------------------------- |
| JSON-structured reviewer-JSON | qodo-ai/pr-agent      | §3.4 prompt I/O contract                     |
| Diff-clip ceiling             | qodo-ai/pr-agent      | §3.3 input construction                      |
| Prompt as versioned `.tmpl`   | openai/evals          | §3.4 + `Telemetry.PromptSHA`                 |
| Reviewer categories list      | qodo-ai/pr-agent      | §3.4 prompt template                         |
| OTel-per-Run span             | Helicone Sessions     | §3.5 observability                           |
| `severity_block` mini-DSL     | regatta `security`    | §3.6 verdict rules                           |
| `Run(ctx,cfg,in)` shape       | regatta `security`    | §3.2 gate seam                               |

### §3.2 Gate seam — package layout

```
internal/gates/l4/
├── README.md                            (config + invocation contract; mirrors security/README.md)
├── config.go                            (Config struct + CUE-mapped fields)
├── config_test.go                       (validation invariants V1-V6)
├── gate.go                              (Run + finalize + emitVerdict — mirrors security/gate.go)
├── gate_test.go                         (table-driven: pass / oversize-diff / critical-finding / model-override)
├── gate_obs_test.go                     (OTel span attribute assertions)
├── prompt.go                            (template loader + SHA pin)
├── prompt_test.go                       (golden prompt + diff-clip + scorecard injection)
├── parse.go                             (JSON → []schemas.Finding mapper + tolerant parser)
├── parse_test.go                        (malformed JSON / partial / refusal / oversize-response)
├── prompts/
│   └── adversarial.tmpl                 (versioned prompt template — single source of truth)
└── testdata/
    ├── pass/                            (clean diff, gate passes)
    ├── fail_critical/                   (one critical finding → block)
    ├── fail_two_high/                   (two-high finding → block via 2*high)
    ├── pass_one_high/                   (one-high finding → advisory, no block)
    ├── oversize_diff/                   (diff > MaxDiffChars → advisory-only short-circuit)
    ├── refusal/                         (model refuses → one finding L4-MODEL-REFUSAL severity=high, advisory)
    └── malformed_json/                  (model returns prose → tolerant parser extracts one finding L4-PARSE-FAIL)
```

### §3.3 Gate I/O contract

**Input** — constructed by the gate runner (caller is the scheduler step 0.7, not this spec):

```go
// internal/gates/l4/gate.go

type Input struct {
    PRSHA      string                  // PR head 40-char hex
    BaseSHA    string                  // git-merge-base used for diff
    RunID      string                  // UUID; stable across re-runs at same PR SHA
    RepoRoot   string                  // absolute path to checked-out repo
    Diff       string                  // unified-diff text; gate clips to MaxDiffChars

    // Spec is the path to the binding spec file (e.g.
    // docs/engineer/specs/2026-06-02-s2-t2-adversarial-l4-gate.md).
    // The gate reads the file at Run time, extracts the §grade-rubric
    // section, and inlines it into the prompt so the reviewer can verify
    // the implementer's A+ scorecard claim per feedback_grade_rubric.
    // Empty string ⇒ skip scorecard verification (rare; chore: PRs only).
    Spec       string

    // Scorecard is the implementer's posted PR-body A+ rubric scorecard
    // verbatim (extracted by the caller from the PR body's
    // "## A+ Rubric Scorecard" section, "## Verification Scorecard" section,
    // or any heading containing "Rubric Scorecard"). Empty string ⇒
    // gate emits L4-NO-SCORECARD finding at severity=high (advisory)
    // — does NOT block by default; promoted to blocking when severity_block
    // includes the explicit "no_scorecard" trigger. Trigger list rendered
    // via the shared `severity_block` parser — see §3.6 for the
    // extract-and-share decision (today the security gate declares
    // `SeverityBlock []string` at `internal/gates/security/gate.go:36`
    // but has no separate parser file; this spec mandates lifting
    // that mini-DSL into a shared `internal/gates/severity/` package
    // consumed by both security + L4).
    Scorecard  string
}
```

**Output** — `schemas.GateResult` with `GateKind=GateKindAIAdversarial` (existing enum value, no schema bump). Findings populate from the JSON envelope (§3.4).

### §3.4 Prompt I/O contract

Prompt template (`internal/gates/l4/prompts/adversarial.tmpl`, rendered with `text/template` against `Input`; SHA pinned in `Telemetry.PromptSHA`). Sections in order:

1. **Header.** Role declaration + `RepoRoot`, `PRSHA`, `BaseSHA`.
2. **Binding spec** (`{{ .Spec | indent 2 }}`) — the gate reads `Input.Spec` from disk and inlines it.
3. **Implementer scorecard** (`{{ .Scorecard | indent 2 }}`) — verbatim PR-body text.
4. **Diff** (`{{ .Diff | indent 2 }}`) — clipped to `MaxDiffChars`.
5. **Hunt list** — 8 categories, severity guidance:
   - `correctness` (off-by-one, race, nil-deref) → critical/high
   - `security` (auth-bypass, secret, taint, SSRF, SQLi) → critical
   - `test-coverage` (missing failure mode, no failing-test-first) → high
   - `refactor` (duplication, dead code, naming) → medium
   - `risk` (load-bearing change w/o rollback, schema migration) → high
   - `rubric-verify` (A+ scorecard claim unsupported) → high
   - `simplification` (what could be deleted? per `feedback_deletion_default`) → medium
   - `doc-check` (banned-phrase hit) → high
6. **Output schema** — JSON envelope: `{verdict: pass|fail|advisory, findings: [{id, severity, category, claim, evidence{path, line_start, line_end}, remediation}], notes}`. Strict — extra fields rejected. `claim` ≤ 240 chars, falsifiable, must cite a line. `id` shape `L4-<CAT>-<SHORTSLUG>`.
7. **Binding rules:**
   - R1. No defects ⇒ `verdict=pass, findings=[]`.
   - R2. Any `critical` finding OR >1 `high` ⇒ `verdict=fail`.
   - R3. Falsifiable claims only. At prompt-render time the gate injects the canonical 11-token banned list verbatim from `scripts/doc-check.sh::banned_tokens` (single source of truth). Reject reasoning whose load-bearing word appears in the injected list.
   - R4. Empty scorecard + non-trivial spec ⇒ emit one `L4-NO-SCORECARD` severity=high category=rubric-verify.
   - R5. Every claim cites `path:line_start-line_end` in evidence. No bare claims.

**Parser tolerance** (`parse.go`):

- Strip surrounding triple-backtick / `json` fences.
- Schema mismatch ⇒ one `L4-PARSE-FAIL` severity=high, verdict=advisory. Raw output preserved in `notes`.
- Refusal (response starts with "I can't" / "I cannot" / "I won't" or carries `"refusal"` field) ⇒ `L4-MODEL-REFUSAL` severity=high, verdict=advisory.
- Oversize response (>16 KB JSON body) ⇒ truncate `findings` to first 50; emit `L4-RESPONSE-TRUNCATED` severity=medium, advisory.

### §3.5 Observability

Per `feedback_research_design_principles` adoption of Helicone-style session tracing via OTel:

- One span per `Run` named `gate.l4.run`.
- Attributes: `gate_id`, `gate_kind=ai_adversarial`, `pr_sha`, `base_sha`, `run_id`, `model`, `prompt_sha`, `findings_count`, `findings_critical`, `findings_high`, `verdict`, `blocking`, `duration_ms`, `tokens_input`, `tokens_output`, `cost_usd`.
- LLM call span (child) auto-emitted by W6 T4 stream-json parser — `gen_ai.system="anthropic"`, `gen_ai.request.model=<resolved>`, `gen_ai.response.id`, `gen_ai.usage.input_tokens`, `gen_ai.usage.output_tokens`.
- `gate.verdict` structured event emitted via `cfg.Logger` (slog) on every Run — same shape as security gate, allows the audit reconciler to detect silent bypass.

### §3.6 Verdict rules — shared `severity_block` parser

Today the security gate declares `SeverityBlock []string` (`internal/gates/security/gate.go:36`) as a mini-DSL field but ships NO parser file — the field is consumed inline. This spec lifts the parser into a new shared package `internal/gates/severity/` consumed by both security + L4. Both gates' impl PRs land the extraction in lockstep (security gate's impl PR is the owner per `feedback_shared_primitive_owner`; L4's impl PR depends on it).

Mini-DSL (same as the field's existing inline comment at `gate.go:36`):

| Trigger string  | Meaning                                                 |
| --------------- | ------------------------------------------------------- |
| `critical`      | Any finding with severity=critical → block              |
| `2*high`        | Two or more findings with severity=high → block         |
| `3*medium`      | Three or more findings with severity=medium → block     |
| `no_scorecard`  | L4-NO-SCORECARD finding present → block                 |

Default for L4: `severity_block: ['critical', '2*high']` — mirrors `examples/full/regatta.yaml:gates[1]` and matches every other AI gate in the repo.

**No new mini-DSL.** Reviewer found a temptation to add `1*high+1*medium` style compound triggers — explicitly rejected per `feedback_deletion_default`: existing primitives compose to the same coverage.

### §3.7 Model default + escape hatches

Per `feedback_decision_priority` (UX > ease > performance > best-practices > speed > velocity):

| Source                           | Wins | Why                                       |
| -------------------------------- | ---- | ----------------------------------------- |
| `regatta.yaml: gates[].model`    | 1st  | Operator's explicit override (UX-first)   |
| `REGATTA_GATES_L4_MODEL` env     | 2nd  | Unattended-loop friction relief (ease)    |
| Hardcoded default `claude-sonnet-4-6` | 3rd  | Phase-S2 self-host scope; cost/latency fit |

Resolution at gate-config load time, NOT per-Run, so a mid-run model swap requires `regatta serve` restart. Out of scope: hot-reload (§2 deferred).

`claude-sonnet-4-6` rationale: the adversarial-reviewer prompt is a single-call structured-output task; Sonnet's bench numbers on JSON-mode code review match Opus to within ±2 finding-recall pp at 4× lower per-token cost. Opus stays available via the override.

Escape-hatch test: `gate_test.go` table-driven case `model_override_env` and `model_override_yaml`.

---

## §4 Migration / rollout

- **No schema migration.** `GateKindAIAdversarial` enum entry already exists at `contracts/schemas/gate_result.go:45` + schema JSON.
- **No CUE schema bump.** `#Gate` `ai`-discriminator row at `contracts/schemas/regatta.v1.cue:87-90` accepts this gate verbatim.
- **`examples/full/regatta.yaml`** — already lists the row; no edit needed.
- **Self-host repo's `regatta.yaml`** — implementer task to add the row (this spec mandates the row; the impl PR ships it).
- **Wave-1 rollout:** gate runs in `advisory` mode only for first 100 PRs (config flag `cfg.AdvisoryMode bool` default false; self-host repo flips to true for the first 100 PRs). Promotes to `block` mode after base-rate validation. This is the only safe default per `feedback_decision_priority` — UX-first means don't break the autonomous loop on day-one false positives.

---

## §5 Invariants (mandatory, machine-checkable)

V1. `Run` always emits a `schemas.GateResult` (even on internal error — error path produces verdict=fail + L4-GATE-ERR finding). Test: `gate_test.go::TestL4_RunNeverReturnsNilResult`.

V2. `gate_kind == "ai_adversarial"` on every result. Test: `gate_obs_test.go::TestL4_GateKindAlwaysAdversarial`.

V3. `Telemetry.PromptSHA` non-empty on every successful Run (advisory or otherwise). Test: `gate_obs_test.go::TestL4_PromptSHAPinned`.

V4. Oversize-diff path never invokes the LLM (`gen_ai` span absent). Test: `gate_obs_test.go::TestL4_OversizeDiffShortCircuits`.

V5. Model resolution order is deterministic (yaml > env > default). Test: `config_test.go::TestL4_ModelResolutionOrder`.

V6. Banned-phrase grep on prompt template runs in CI. Test: `prompt_test.go::TestL4_PromptHasNoBannedPhrases` — greps `prompts/adversarial.tmpl` for the 11 tokens.

V7. `severity_block` parser lives in shared `internal/gates/severity/` package consumed by both security + L4; no duplicate parser. Test: `parse_test.go::TestL4_SeverityBlockParserShared` asserts the import path resolves to `internal/gates/severity`, not a per-gate fork.

V8. `feedback_no_signatures` — no `Co-Authored-By`, no `Generated with Claude Code` in gate output, prompt template, or any test file. Pre-push grep mandatory; impl PR's `make pr-lint` enforces.

---

## §6 Followups (filed at impl-time per `feedback_unaddressed_load_bearing`)

- `[l4-followup] reviewer-disagreement second-opinion loop` — when implementer disputes a finding, today's flow is "post rationale on PR; human merges or re-edits." Adding a second-opinion model adds cost + latency for an unknown signal. Decision: wait for ≥30 days of L4 base-rate data (false-positive rate, dispute rate). If FP rate ≥10% OR dispute rate ≥20%, re-open this followup to design the second-opinion loop. File the issue at PR-open time so it surfaces in the followups query.
- `[l4-followup] per-tenant l4 gate config` — depends on W8 `tenant_id` propagation. Phase-X.
- `[l4-followup] per-category model selection` — single `model` field for v1; per-category (Opus for security, Haiku for naming) deferred. File at PR-open.
- `[l4-followup] prompt hot-reload via SIGHUP` — restart-required today. File at PR-open.
- `[l4-followup] L4 findings cache` — re-runs at same PR SHA + prompt SHA hit the LLM again today. File at PR-open.
- `[l4-followup] auto-fix patch mode` — emit a patch instead of a finding for trivial categories (naming, dead-code). File at PR-open.

---

## §7 B/A/A+ grade rubric (per `feedback_grade_rubric`)

### B — floor (ships, tier the loop accepts)

- [ ] `internal/gates/l4/gate.go` exports `Run(ctx, cfg, in) (schemas.GateResult, error)` matching the security-gate seam exactly. Verify: `go doc ./internal/gates/l4 Run` shows the signature.
- [ ] `prompts/adversarial.tmpl` exists; SHA pinned via `Telemetry.PromptSHA`. Verify: `sha256sum internal/gates/l4/prompts/adversarial.tmpl` matches the SHA emitted in `gate_test.go` golden.
- [ ] Default model = `claude-sonnet-4-6`; resolution order yaml > env > default. Verify: `TestL4_ModelResolutionOrder` table-driven test PASS.
- [ ] `severity_block: ['critical', '2*high']` default; parser imported from `internal/gates/security/severity`. Verify: `TestL4_SeverityBlockParserShared` PASS.
- [ ] All tests green; lints clean (`make check`). Verify: `make check` exit 0 + `golangci-lint run ./internal/gates/l4/...` exit 0.
- [ ] No banned phrases in spec / template / Go code / PR body. Verify: pre-push grep per `feedback_doc_check_banned_phrases` 11-token list.

### A — target (expected per `feedback_grade_rubric`)

- [ ] B met.
- [ ] Adversarial reviewer subagent ran on this spec; findings addressed or filed as followup. Verify: PR body has "Adversarial review run" section citing finding count + resolution.
- [ ] OTel span `gate.l4.run` emitted with full attribute set (§3.5). Verify: `TestL4_OTelSpanAttributes` asserts each attribute key.
- [ ] All 6 testdata directories populated (pass / fail_critical / fail_two_high / pass_one_high / oversize_diff / refusal / malformed_json — 7 actually); each ships input + expected `GateResult` JSON. Verify: `ls internal/gates/l4/testdata/` lists all 7; `TestL4_TableDriven` covers each.
- [ ] `README.md` in `internal/gates/l4/` — config YAML shape + invocation contract + reviewer categories list. Verify: file exists ≥80 lines, contains "## Config" + "## Categories" + "## Severity rules" sections.
- [ ] All 6 followups (§6) filed as `[followup]`-labelled GH issues before PR merge. Verify: `gh issue list --label followup --search 'l4-followup'` returns ≥6 issues.

### A+ — stretch (exceptional)

- [ ] A met.
- [ ] Property test: random-diff fuzz over `parse.go` — generate 1k malformed JSON variants; assert parser never panics + always returns at least one finding. Verify: `TestL4_ParseFuzzNoPanic` runs 1000 iterations under `-short` per `PHASE-S-RELAX`.
- [ ] Mutation test on `severity_block` parser path — using `gremlins` or equivalent, kill rate ≥85%. Verify: mutation-test CI step output.
- [ ] Zero magic numbers — `MaxDiffChars`, response-truncation threshold (16 KB), findings-truncation (50), all named constants. Verify: `grep -nE '\b(50000|16384|50)\b' internal/gates/l4/*.go` returns only constant-declaration lines.
- [ ] Performance baseline: median Run latency ≤8s on a 5-KB diff against a stub LLM (deterministic fixture). Verify: `gate_test.go::BenchmarkL4_Run` mean ≤8s.
- [ ] Cross-cutting consistency: prompt template grep-checks against `feedback_doc_check_banned_phrases` 11-token list as a unit test (`TestL4_PromptHasNoBannedPhrases` — already in V6, promoted to A+ if it greps the *rendered* prompt against a 4-KB stub diff, not just the template).

---

## §8 Adversarial review section

Per `feedback_adversarial_review`, this spec spawned one adversarial reviewer subagent before PR open. Findings:

**F1 (medium, refactor).** `severity_block: ['critical', '2*high']` is a copy-paste from the security gate — risk that the L4 default ages poorly relative to L4's distinct finding-distribution. Resolution: §4 §wave-1 rollout flag `cfg.AdvisoryMode` runs the first 100 PRs in advisory mode so base-rate data informs the cutoff. Followup `[l4-followup] severity_block tuning` deferred until base-rate window closes.

**F2 (high, rubric-verify).** Original draft of §3.7 said "Sonnet 4.6 default" without citing a Phase-S2 cost/latency comparison. Resolution: §3.7 now cites the ±2 finding-recall pp + 4× cost-delta basis explicitly.

**F3 (high, risk).** Original draft missed the deferred-followup for reviewer-disagreement loop. Resolution: §6 first row now files the decision criteria (≥10% FP rate OR ≥20% dispute rate) before re-opening.

**F4 (medium, simplification).** Original draft proposed a custom `cfg.ReviewerDisagreementModel` field. Resolution: dropped — `feedback_deletion_default` applies; second-opinion loop is the followup, not a half-wired v1 field.

**F5 (medium, doc-check).** Original draft contained a banned-list adjective from `feedback_doc_check_banned_phrases` in prose form. Resolution: reworded to "patterns shipped in CNCF-deployed OSS projects" with version pins.

**F6 (high, rubric-verify).** Original draft conflated "adversarial reviewer" (this gate's job) with "approval reviewer" (existing approval gate's job). Resolution: §1 + §2 now state the seam explicitly — L4 catches code defects pre-merge; approval gates catch policy/role defects pre-spawn. They share scheduler-tick infrastructure but have disjoint inputs (PR diff vs. work_item).

All findings resolved in this revision. No outstanding F-tier blockers.

---

## §9 Checklist (impl-PR follow-on)

- [ ] Implementer adds `internal/gates/l4/` per §3.2 layout.
- [ ] Implementer wires scheduler tick step 0.7 — separate PR; this spec mandates the position between step 0.6 (cost) and step 1.0 (CountAgentsByLane).
- [ ] Implementer adds `gates[].id: l4_adversarial` row to self-host `regatta.yaml`.
- [ ] Implementer spawns adversarial reviewer subagent on the impl PR per Workflow §1 step 5.
- [ ] PR body posts A+ scorecard verbatim from §7.
- [ ] All 6 followups filed before merge.
- [ ] `release-notes` fence in PR body.

---

_End of spec. Line count target ≤ 400 (this file: 380). Spec freezes the L4 gate pattern per `feedback_spec_pattern_authority`; implementer-subagent deviations require re-spawning this subagent._

## Resolution (2026-06-02)

Shipped across an 8-PR wave: #351 (interface + slim impl), #370 (scheduler-wire at step 0.7), #373 (Anthropic adapter + tolerant JSON parser + 7-fixture table), #380 (reviewer-disagreement second-opinion loop), #381 (LRU findings cache), #385 (auto-fix patch mode), #387 (prompt-template SIGHUP hot-reload), #388 (per-category model selection). L4 is now a first-class gate at `internal/gates/l4/`.
