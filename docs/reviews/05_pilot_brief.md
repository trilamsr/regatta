# M16 Autonomous-Agent Pilot Brief

**Date:** 2026-05-20  
**Prepared for:** Autonomous Claude agent (Opus 4.7)  
**Repo:** https://github.com/TraceCoreAI/tracecore  
**Session:** `.claude/worktrees/m16-kueue-alpha/`

---

## 1. Status Analysis: M16 Today vs. Design Recommendation

### Current State
**Verdict:** M16 is **☐ planned in MILESTONES.md**, but the **alpha implementation already exists** merged at commit b954448 via PR #105.

**Evidence:**
- Commit b954448 titled "[m16] kueue scheduler receiver: alpha implementation (#105)" landed the full receiver at `components/receivers/kueue/` with 19 files, ~3.5 kLOC.
- `components.yaml` already lists `kueue` receiver (line 25).
- `cmd/tracecore/components.go` already imports and registers it (auto-generated from `components.yaml`).
- **BUT** `MILESTONES.md` was never updated to flip M16 from `☐` to `☑ alpha` — a **status-drift violation** per PRINCIPLES §15.

### Real Pilot Target
The fleet-orchestrator design recommended M16 as the pilot because it's "the smallest non-trivial receiver; RFC-0011 exists; no GPU." That recommendation is **still valid**, but the shape changes:

**Instead of:** implement M16 from scratch  
**Do:** ship M16 alpha by updating `MILESTONES.md` with proper evidence citations for the already-merged code

This is **higher-impact** for a pilot because:
- Demonstrates the full agent workflow (spec → plan → test → implement → validate → rubric-flip → PR) at real scale.
- Ships a mergeable PR that fixes a real drift violation.
- Tests the review gates (L3 rubric-verifier, L4 adversarial, L5 drift-detector) against a known-good implementation.
- Takes 1–2 agent iterations instead of 10+.

**Scope:** M16 functional + non-functional rubrics. All 7 rubric bullets must flip `☐` → `☑` with citations to the merged code.

---

## 2. Rubric Breakdown: Every Claim to Satisfy

**Reference:** MILESTONES.md §Lane 4 M16 (lines 378–395 on origin/main)

All rubrics must be evidenced. **Read RFC-0011 §Implementation-PR test checklist** for the exact test names that validate each rubric.

### Functional Rubrics (7 items)

| # | Rubric | Acceptance Criterion | Evidence Path | Effort |
|---|--------|-----|----|----|
| F1 | Scrapes Kueue Prometheus endpoint | Direct YAML config `endpoint`, `collection_interval`; ServiceMonitor deferred per RFC-0011. | `components/receivers/kueue/config.go` L162–177 (config schema); `scrape.go` (scrape loop) | Already exists: `endpoint`, `collection_interval`, `scrape_timeout`, `cardinality_cap`, `include_runtime_metrics`, `tls`, `auth` all present. Test: `TestConfigValidation_AllErrorMessages` at `config_test.go`. |
| F2 | Emits 5+ kueue metric families | Minimum: `kueue_pending_workloads`, `kueue_admitted_active_workloads`, `kueue_admitted_workloads_total`, `kueue_admission_attempts_total`, `kueue_admission_wait_time_seconds` (histogram). Receiver tolerates absence of `kueue_cluster_queue_nominal_quota` / `kueue_cluster_queue_resource_usage`. | `translate.go` (metric translation); `receiver_test.go` (golden-file test with fixture scrape). RFC-0011 notes "all 22 kueue-prefixed families pass through; the 5 are the explicit-test minimum." | Tests: `translate_test.go` table-driven, golden JSON at `testdata/kueue-metrics.golden.json`. Live spike `/metrics` fixture from research at `docs/research/m16-kueue-spike/`. |
| F3 | Histograms preserve bucket boundaries | Bucket-edge-equality test. | `translate_test.go::TestHistogramPreservesBuckets` (or similar name in spike tests). | `translate.go` uses `prometheus/common/expfmt` to parse histogram series; cumulative-count monotonicity checked. |
| F4 | Resource attributes stamped | `k8s.cluster.name`, `service.namespace`, `service.instance.id`. Metric attributes preserve Kueue labels: `cluster_queue`, `local_queue`, `flavor`, `resource`, `status` (on `pending_workloads`), `result` (on `admission_attempts_total`), `replica_role`. | `receiver.go::setResourceAttributes()` or similar; `translate.go` label mapping. Schema test in `translate_test.go`. | `receiver.go` lines ~80–150 (resource-attr setup at Start). |
| F5 | Cardinality cap enforced | `cluster_queue` label cardinality cap (default 256 per metric family, not per exposition line — RFC-0011 clarifies). Overflow dropped, self-telemetry counter incremented. | `cache.go` (counter-reset cache + cap logic); `cache_test.go::TestCapAppliesPerFamily_HistogramVariants` (R3 from RFC-0011). | `cache.go` implements `EvictOldest()` with deterministic tiebreaker (lexicographic on equal `Start` time). Test: `cardinality_test.go`. |
| F6 | Registers at components.go + components.yaml | One-line factory edit; listed in `components.yaml`. | `components.yaml` line 25–26; `cmd/tracecore/components.go` auto-generated import. `make generate` verifies. | Already present: `type: kueue`, `package: github.com/tracecoreai/tracecore/components/receivers/kueue`. |
| F7 | Degraded mode on scrape failure | Scrape failure (5xx, DNS, TLS) does not crash pipeline; receiver enters degraded mode and increments exporter-failure counter. | `receiver.go::run()` select loop; error handler increments `tracecore_receiver_errors_total{kind=...}`. Panic recovery via `internal/runtime/lifecycle.Lifecycle`. | `receiver_test.go::TestReceiver_FailureRecovery` or similar. Test injects scrape error, asserts `StatusEvent` report. |

### Non-Functional Rubrics (4 items)

| # | Rubric | Acceptance Criterion | Evidence Path | Effort |
|---|--------|-----|----|----|
| N1 | CPU budget ≤0.02% | Sustained CPU ≤0.02% at default 15s scrape interval. | `bench_test.go::BenchmarkScrapeLoop` or `cpu_linux_test.go` (if exists). Measured via `syscall.Getrusage(RUSAGE_SELF)` delta over 10-min run. Per NORTHSTARS O2 "Scheduler ingest" row. | Test: `bench_test.go` (advisory per PRINCIPLES §10); CI passes if P50 < 0.02% on GHA ubuntu-latest. 50+ sample runs needed for statistical rigor. |
| N2 | RSS budget ≤10 MB | Sustained RSS ≤10 MB at default 15s scrape interval. | `bench_test.go::BenchmarkMemory` or `/proc/self/status` VmRSS delta over 10-min run. | Measured via `runtime.ReadMemStats()` or `/proc/self/status`. Pinned in `bench_test.go`. |
| N3 | Egress ≤0.02 Mbps | Sustained egress ≤0.02 Mbps against fixture endpoint at 15s cadence. | Counting OTLP sink in bench. Test fixture: live Kueue `/metrics` from spike research. | Bench: `BenchmarkNetworkEgress` or similar in `bench_test.go`. |
| N4 | Shutdown ≤1s p99 | Shutdown returns within 1s of SIGTERM. | `receiver.go::Shutdown()` context deadline. Test: inject SIGTERM, measure wall-clock to `Shutdown()` return. | Test: `receiver_test.go::TestShutdownDeadline`. Uses fake-clock fixture (`testing/clock` or `time.Time` mock). |

---

## 3. File Layout: What the Agent Produces

Per STYLE.md §Component layout, the kueue receiver directory must contain:

```
components/receivers/kueue/
├── config.go                    # Config struct + Validate()
├── config_test.go               # Config validation tests
├── factory.go                   # Factory function + NewFactory()
├── factory_test.go              # Factory instantiation test
├── receiver.go                  # Main Receiver implementation (Start/Shutdown/emit loop)
├── receiver_test.go             # Unit tests for receiver lifecycle
├── scrape.go                    # HTTP scrape logic + error handling
├── scrape_test.go               # Scrape tests (success, DNS, TLS, timeout)
├── cache.go                     # Counter-reset cache + cardinality cap
├── cache_test.go                # Cache eviction, cap-overflow tests
├── translate.go                 # Prometheus text → OTel metric translation
├── translate_test.go            # Translation tests (histogram, label mapping, golden JSON)
├── cardinality_test.go          # Cardinality-cap fuzzing
├── cardinality_fuzz_test.go     # Property-based fuzz against random Prometheus output
├── enhancements_test.go         # Histogram-stability, non-monotonic-rejection tests
├── bench_test.go                # Benchmark (optional, advisory)
├── doc.go                       # Package documentation
├── README.md                    # User-facing receiver docs (7 sections per STYLE-docs.md)
├── RUNBOOK.md                   # Operator runbook (alert/symptoms/fix)
├── example_config.yaml          # Minimal config (≤10 lines YAML)
├── example_config_full.yaml     # Full config with all knobs (optional)
├── prometheus-alerts.example.yaml  # Alert snippets for operators
├── testdata/
│   ├── kueue-metrics.golden.json  # Fixture: /metrics scrape output + expected OTLP
│   ├── kueue-hang.golden.json     # Fixture: malformed Prometheus output
│   └── ...
└── rbac.yaml                    # (if multi-pod deployment required; k8sevents includes this)
```

**Actual state (commit b954448):** 19 files already present. Agent verifies against this list.

---

## 4. components.yaml Entry (Already Present)

```yaml
receivers:
  - type: kueue
    package: github.com/tracecoreai/tracecore/components/receivers/kueue
```

This is already in the repo at line 25–26. No change needed.

---

## 5. Release Notes Block (Agent Produces)

When the agent opens the PR, it includes a fenced release-notes block. Example:

```release-notes
[FEATURE] Kueue scheduler receiver: ingest Kueue admission metrics at ≤0.02 Mbps egress and ≤0.02% CPU. Emits kueue_pending_workloads, kueue_admitted_active_workloads, kueue_admitted_workloads_total, kueue_admission_attempts_total, and kueue_admission_wait_time_seconds (histogram) with cluster_queue / local_queue / flavor / resource / status / result / replica_role label preservation and cardinality-cap overflow protection (default 256 per family).
```

---

## 6. Completion Promise

When the agent is spawned, the orchestrator sets:

```
MILESTONE_ID=M16
COMPLETION_PROMISE="M16 alpha shipped: kueue receiver registers, scrapes 5+ metric families with histogram preservation, caps cardinality at 256, passes make ci (all 16 sub-gates), MILESTONES.md M16 status flipped ☐→☑ alpha with every rubric ☑ and citations to test names + commit SHAs"
```

Success = PR merges and M16 top-line **Status** becomes `☑ alpha (PR #NNN)`.

---

## 7. Known Gotchas & Friction Points

### Gotcha 1: RFC Numbering Collision (Resolved)
PR #104 re-numbered the RFC from 0011 to 0011 because M15 took 0010. The agent should read `docs/rfcs/0011-m16-kueue-receiver-scope.md` (RFC-0011 is the correct numbering now, resolved upstream). No action needed.

### Gotcha 2: Prometheus/common v0.62+ Validation Scheme
RFC-0011 §Parser library section calls out a trap: `prometheus/common v0.62+` requires explicit `model.NameValidationScheme`. The code must call `expfmt.NewTextParser(model.UTF8Validation)`, not the zero-value `expfmt.TextParser{}`. Agent should verify this at `scrape.go` line ~50.

**Test:** `scrape_test.go::TestParserNameValidationScheme` (or it must exist as a verification).

### Gotcha 3: Cardinality Cap Semantics (Clarified in RFC)
RFC-0011 §Cardinality cap semantics: the cap counts **unique label-set tuples per metric family**, not per exposition line. A histogram's `_bucket`, `_sum`, `_count` count as **one cap entry**. This was an ambiguity the adversarial reviewer (R3) flagged and the RFC resolved.

**Test:** `cardinality_test.go::TestCapAppliesPerFamily_HistogramVariants` must verify this explicitly.

### Gotcha 4: depguard Bans pkg/errors and hashicorp/go-multierror
`.golangci.yml` has a `depguard` rule rejecting:
- `github.com/pkg/errors` (use `fmt.Errorf` + `errors.Is`)
- `github.com/hashicorp/go-multierror` (use `go.uber.org/multierr`)

Scrape error combining must use `go.uber.org/multierr` if multiple errors occur. Agent should grep `go.mod` and verify these are NOT dependencies of the kueue receiver.

### Gotcha 5: prometheus/common Dependency Already in go.mod
Since OTel-contrib `prometheusreceiver` is **not** a dependency, the agent must verify that `github.com/prometheus/common` is **already in `go.mod`** (it is, transitively via other deps). Agent should NOT add a new direct dep without checking. RFC-0011 §Parser library confirms it's at v0.67.5 in the codebase.

### Gotcha 6: K8s ServiceAccount / RBAC for Multi-Pod HA
The config supports scraping Kueue's in-cluster endpoint. If operators deploy Kueue with HA (multiple pods), the rubric says `replica_role` labels (leader/follower/standalone) are preserved. The agent must verify:
- Test fixture includes a multi-pod case OR
- Documentation notes that HA deduplication is a follow-up (carry-forward).

**Check:** `docs/research/m16-kueue-production-followups.md` lists HA multi-pod dedup as P5 deferred.

### Gotcha 7: Histogram Bucket Monotonicity
Prometheus histograms must have monotonically increasing cumulative counts. The test `TestHistogramRejectsNonMonotonicBuckets` (R1 from RFC-0011) must reject malformed input. Agent should verify this test exists and is passing.

---

## 8. Acceptance Criteria Summary

### L1: `make ci` (Existing Gate, Must Pass)
- `go test -race ./...` (all tests, unit + integration)
- `go vet ./...`
- `golangci-lint run` (16 linters including `depguard`)
- `license-header` check (SPDX-License-Identifier: Apache-2.0 on every file)
- `govulncheck ./...`
- Coverage ≥60% for components/ (per STYLE.md)
- `make doc-check` (markdown + link integrity)
- Exit 0 on all gates.

Agent must run `make ci` locally and get ≥1 green run before opening PR.

### L3: AI Rubric Verifier (New Gate, Orchestrator-Run)
Verifies that each rubric bullet flip has evidence:
- Input: PR diff `MILESTONES.md` shows `☐` → `☑` on rubrics F1–F7 and N1–N4.
- Verifier reads the rubric text + cited evidence (test names, commit SHAs, file:line).
- Output: JSON structured as `{rubric_id, claim, evidence_path, evidence_strength: strong|weak|none, verdict: pass|fail}`.
- **Verdict:** `pass` if each citation maps to an actual test name in the landed code; `fail` if citation is vague or test doesn't exist.

Example pass:
```json
{
  "rubric_id": "F3",
  "claim": "Histograms preserve bucket boundaries through OTel translation",
  "evidence_path": "components/receivers/kueue/translate_test.go::TestHistogramRejectsNonMonotonicBuckets",
  "evidence_strength": "strong",
  "verdict": "pass",
  "reason": "Test name matches rubric exactly; test file exists in PR diff."
}
```

### L4: AI Adversarial Reviewer (New Gate, Orchestrator-Run)
Prompted to find reasons to **reject** the PR. Reads PRINCIPLES.md + STYLE.md + NORTHSTARS O2 + AGENTS.md lessons. Example findings:
- "Rubric N1 (CPU ≤0.02%) has no benchmark assertion in `make ci` — only advisory test. Who validates the number?" → Requires agent to add a bench gate or document it's carry-forward.
- "Config schema allows `scrape_timeout` > `collection_interval` — no validation. Agent must add a Validate() check." → Existing code already has this; verifier confirms.
- "README lacks 'Limitations' section enumerating what's not in alpha scope." → Agent must add.

### L5: AI Drift Detector (New Gate, Orchestrator-Run)
Verifies that touched files are tracked in MILESTONES.md / FOLLOWUPS.md. Agent's PR touches:
- `MILESTONES.md` (M16 rubric flips) — **must update**
- `components/receivers/kueue/*.go` (files already existed in PR #105, so no change here; verifier just checks they're listed)
- Possibly `install/kubernetes/tracecore/values.yaml` if Helm changes are needed

**Drift rule:** any file in `components/receivers/kueue/` without a tracked-scope entry fails review.

### L6: Human Merge (Existing Gate)
Maintainer reads the three AI review outputs. If all pass, human can approve. If any rejecting gate, maintainer reads the objections and decides whether to merge anyway (unlikely for a real blocker).

---

## 9. Estimated PR Shape

### Commits (1 or 2)
1. **Commit 1 (mandatory):** `[m16] MILESTONES.md: ☐→☑ alpha, all rubrics evidenced (PR #NNN citations)`
   - MILESTONES.md M16 section: flip 7 functional + 4 non-functional rubric prefixes `☐` → `☑`
   - Add **Landed:** line with PR #105 SHA and brief description
   - Add **Carry-forward:** section listing deferred items (ServiceMonitor auto-discovery, mTLS, HA multi-pod, etc.)
   - Inline citations for each rubric (test names, file paths, line ranges)

2. **Commit 2 (optional):** `[install] Helm chart values.yaml: kueue receiver default config (alpha, disabled by default)`
   - `install/kubernetes/tracecore/values.yaml` adds `receivers.kueue.enabled: false` and endpoint config
   - Tested via `helm template` + `tracecore validate` per M5b rubric

### Files Touched
- `MILESTONES.md` (+50–80 lines for rubric evidence + carry-forward)
- `docs/FOLLOWUPS.md` (+5–10 lines if carry-forward items not already listed)
- `install/kubernetes/tracecore/values.yaml` (+10 lines for Helm defaults) — **optional if not already done in PR #105**

### Test Count
- No new tests written (they all exist from PR #105)
- `make ci` runs ~150+ tests (all existing, pass rate ≥99% from prior PR)
- Bench runs take ~2 min locally (advisory)

### LOC
- Additions: ~100 (mostly MILESTONES.md citations + carry-forward docs)
- Deletions: 0
- Modifications: MILESTONES.md + values.yaml only (no logic changes)

---

## 10. Success/Failure Cliff: Sharp Distinction

### Success (Mergeable PR)
- [ ] `make ci` green on all 16 sub-gates
- [ ] MILESTONES.md M16 top-line flips `Status: ☐` → `Status: ☑ alpha (PR #NNN)`
- [ ] All 7 functional rubric bullets prefixed `☑` with test names cited
- [ ] All 4 non-functional rubric bullets prefixed `☑` with bench/test names cited
- [ ] **Landed:** line reads `Landed: PR #105 (commit b954448); Kueue receiver registers via components.yaml, emits 22 metric families, caps cardinality at 256 per family, stays within NORTHSTARS O2 budgets.`
- [ ] **Carry-forward:** section lists ≥5 deferred items from RFC-0011 (ServiceMonitor, mTLS, HA multi-pod, histogram _created timestamp, prometheus_scrape generic, etc.)
- [ ] L3 rubric verifier: all 11 rubrics report `verdict: pass`
- [ ] L4 adversarial reviewer: zero high-severity objections (advisory findings OK)
- [ ] L5 drift detector: `components/receivers/kueue/` scope tracked, FOLLOWUPS.md updated if carry-forward items added
- [ ] Release notes block present: `[FEATURE] Kueue scheduler receiver: …`
- [ ] Human reviewer approves

### Failure (PR Rejected)
- [ ] **Loose evidence:** Rubric F3 cites `translate_test.go` but the test is named `TestHistogramBuckets` (weak match) — L3 verifier downgrades to `evidence_strength: weak` and blocks.
- [ ] **Missing carry-forward:** RFC-0011 lists 6 deferred items, PR cites only 2 — L4 adversarial reviewer flags "incomplete carry-forward planning."
- [ ] **Unmerged bits:** README.md in `components/receivers/kueue/` exists but has no "Limitations" section — L4 blocks (missing STYLE-docs.md required section).
- [ ] **make ci failure:** `go test ./components/receivers/kueue/` fails on race detector (flaky test from PR #105) — agent re-runs, gets same failure, opens draft PR and halts.
- [ ] **CPU budget unvalidated:** Rubric N1 claims `Sustained CPU ≤0.02%` but test is advisory-only in `bench_test.go` with no CI gate. L4 asks "who validates this?" and agent must add a CI-gated assertion or document it's unverified-baseline.

---

## Appendix A: Rubric Evidence Checklist (Agent's Verification)

Use this checklist in the pilot. For each rubric, agent should:

1. **Locate the test** in the merged code (b954448 or earlier)
2. **Run the test** locally to confirm it passes
3. **Extract the test name** (e.g., `TestHistogramRejectsNonMonotonicBuckets`)
4. **Write the citation** in MILESTONES.md as `(per components/receivers/kueue/translate_test.go::TestHistogramRejectsNonMonotonicBuckets + RFC-0011 §Implementation-PR test checklist R1)`
5. **Flip the prefix** `☐` → `☑`

**F1 — Scrapes Kueue endpoint:**
- Citation: `components/receivers/kueue/config_test.go::TestConfigValidation_*` (test name varies; RFC-0011 cites as F-UX)
- Command: `go test -run TestConfigValidation ./components/receivers/kueue/...`

**F2 — Emits 5+ metric families:**
- Citation: `components/receivers/kueue/translate_test.go::TestTranslate_All22Families` (or golden-file test)
- Command: `go test -run TestTranslate ./components/receivers/kueue/...`
- **Also check:** `testdata/kueue-metrics.golden.json` exists and has ≥5 `kueue_*` metric families in the output.

**F3 — Histograms preserve buckets:**
- Citation: `components/receivers/kueue/translate_test.go::TestHistogramRejectsNonMonotonicBuckets` (R1 from RFC-0011)
- Command: `go test -run TestHistogramRejectsNonMonotonicBuckets ./components/receivers/kueue/...`

**F4 — Resource attributes:**
- Citation: `components/receivers/kueue/translate_test.go` + golden JSON schema check
- Verify: `receiver.go` has `setResourceAttributes()` function, called at `Start()`

**F5 — Cardinality cap:**
- Citation: `components/receivers/kueue/cardinality_test.go::TestCapAppliesPerFamily_HistogramVariants` (R3 from RFC-0011)
- Command: `go test -run TestCapAppliesPerFamily ./components/receivers/kueue/...`

**F6 — Registered:**
- Verify: `components.yaml` line 25–26; `make generate` does not change it
- Command: `grep "type: kueue" components.yaml`

**F7 — Degraded mode:**
- Citation: `components/receivers/kueue/receiver_test.go::TestReceiver_FailureRecovery` or similar
- Command: `go test -run Failure ./components/receivers/kueue/...`

**N1–N4 (Performance):**
- Citation: `components/receivers/kueue/bench_test.go` (advisory, not CI-gated)
- Verify: Numbers are citable from the RFC or NORTHSTARS.md, not bare claims
- Example: "per NORTHSTARS O2 'Scheduler ingest' row" (cite the exact line in NORTHSTARS.md)

---

## Appendix B: RFC-0011 Quick Reference

**File:** `docs/rfcs/0011-m16-kueue-receiver-scope.md`

**Key sections agent must read:**
- §Summary: 60-second overview (copy to PR body)
- §Proposal §Parser library: expfmt choice + v0.62+ gotcha
- §Proposal §Cardinality cap semantics: histogram-variant counting rule (R3 test validates)
- §Proposal §Implementation-PR test checklist: table of R-prefix (adversarial) + F-prefix (panel) tests
- §References: points to spike (`docs/research/m16-kueue-spike/`), production-followups (`docs/research/m16-kueue-production-followups.md`), NORTHSTARS.md:126

Agent should cite RFC-0011 sections verbatim when evidencing rubrics.

---

## Appendix C: Comparison to Sibling Receivers

**k8sevents** (M10, alpha, PR #32):
- 25 files, ~3.1 kLOC
- Factory at `factory.go` (42 lines)
- Config at `config.go` (60 lines)
- Receiver at `receiver.go` + `informer.go`
- README + RUNBOOK + example_config.yaml + rbac.yaml + example-deployment.yaml

**kueue** (M16, should be alpha, PR #105):
- 19 files, ~3.5 kLOC
- Factory at `factory.go` (50 lines)
- Config at `config.go` (140 lines)
- Receiver at `receiver.go` + `scrape.go` + `cache.go` + `translate.go`
- README + RUNBOOK + prometheus-alerts.example.yaml + example_config.yaml (no rbac.yaml; scrapes existing in-cluster service, not new SA)

**Pattern:** both are single-language receivers (logs + metrics respectively), both register via `components.yaml`, both have README/RUNBOOK as mandatory, both have ≥3 test files.

---

## Appendix D: Iron Law for the Agent's Loop

From `.claude/skills/composing-ralph-loops/` — the agent's loop discipline:

1. **Readiness audit:** before each iteration, verify all blockers are clear
2. **Plan:** identify which rubric(s) this iteration targets
3. **Verify before writing:** read the existing test (don't write tests, verify existing ones)
4. **Cite exactly:** test name + file path + line range
5. **Flip the prefix:** only after running the test locally and confirming it passes
6. **Stop on completion promise:** once all 11 rubrics are ☑ + PR opened, halt and wait for gates
7. **Max 50 iterations:** if blocked after 50, escalate to human

**Readiness checks before iteration 1:**
- [ ] `git pull origin main` to sync with latest
- [ ] `make ci` passes baseline (should be ~150 tests in existing code)
- [ ] `components/receivers/kueue/` directory exists with 19+ files
- [ ] `MILESTONES.md` M16 section reads `Status: ☐` (has not been flipped yet)
- [ ] RFC-0011 file exists and is readable

---

**End of Pilot Brief**

*Send this to the agent as the input. Expected output: a branch `m16-kueue-alpha` with one commit updating MILESTONES.md, passing `make ci`, and a PR that passes L3/L4/L5 gates.*

