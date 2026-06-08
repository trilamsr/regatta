---
title: "contracts/schemas/regatta.v1.cue modular split — Design Spec"
status: active
summary: "Split the single-file CUE schema (`contracts/schemas/regatta.v1.cue`, 329 LOC) into per-subsystem files under `contracts/schemas/regatta/` so feature additions stop colliding on one anchor; CUE's same-package multi-file semantics make this an additive, byte-equal refactor with no Go-side changes."
---

# `contracts/schemas/regatta.v1.cue` modular split — Design Spec

Status: ready for review
Date: 2026-06-08
Author: design session <tri@maydow.com>

Memory rules in force: `feedback_decision_priority`, `feedback_default_simpler`, `feedback_cascade_rebase_root_cause`, `feedback_deletion_default`, `feedback_research_design_principles`, `feedback_adversarial_review`, `feedback_spec_pattern_authority`, `feedback_no_signatures`, `feedback_unaddressed_load_bearing`.

---

## §0 Closing trigger

Done when: slice-3 lands AND `contracts/schemas/regatta.v1.cue` is either a 1-line `package regattav1` stub OR removed AND `internal/config/validate.LoadConfigFile` accepts the existing `regatta.yaml` byte-equal pre/post AND no `regatta.yaml`-driven test changes.

Reopen-trigger: if a future feature touches a per-subsystem file and ≥3 PRs go DIRTY on the same per-subsystem file in one session, the split granularity is wrong — re-split that one file.

---

## §1 Problem

`contracts/schemas/regatta.v1.cue` is the single CUE schema for `regatta.yaml`. Every config-touching feature appends to it:

- #911 `#Secrets` + `#Secret` (40 LOC).
- #929 multi-target repo block.
- #934 `key_id` keychain-name selector inside `#Secret`.
- #928 BYOM model providers block.

Today's session merged 4 PRs that each amended this one file. Cascade-rebase risk is now a recurring pattern, not "merge math" — same defect class as `internal/orchestrator/state` (#737), `cmd/regatta/serve.go`, and `docs/engineer/specs/README.md` (auto-generated, then untracked in #957). Per `feedback_cascade_rebase_root_cause`: ≥3 PRs DIRTY on a shared anchor = design defect, not normal merge math. Fix structurally.

Symptom shape: every feature PR appends a `#Foo` definition; rebase conflict surface is a god-file with N independent grow-points.

---

## §2 Decision priority

Per CLAUDE.md §"Decision priority": UX > ease > performance > best-practices > speed > velocity. Long-term > short-term.

For one operator + one binary + one repo (`docs/engineer/briefs/2026-06-01-self-host-first.md` §1):

- **Operator UX**: `grep -rn '#Secrets' contracts/schemas/` already works post-split; co-locating a definition with its package-mate cousins (e.g. `#Secret`+`#Secrets`) is a wash. Win is **rebase-locality**: feature PRs touch one small file, not one large file.
- **Ease**: CUE supports multi-file packages out of the box. No build-system change. No Go-side change. No new tooling. Per `feedback_default_simpler`: pick the simplest viable option — co-locate per subsystem, no `cue.mod`/module reorg.
- **Best practice** (cohesion-per-file): a small win, but real.
- **Velocity**: 3-slice incremental migration; each slice independently shippable.

Long-term: every future feature PR's diff fits in one small file. Reviewer's eyes scan one schema concern at a time.

---

## §3 Design

### §3.1 Target layout

```
contracts/schemas/
  embed.go                    # unchanged surface: package schemas + RegattaV1CUE (string)
  regatta.v1.cue              # 1-line `package regattav1` stub (or removed in slice-3)
  regatta/
    root.cue                  # #Config + version pin + import "list"
    secrets.cue               # #Secret + #Secrets
    spec_adapter.cue          # #SpecAdapter
    ci.cue                    # #CI + #PRTemplate
    gates.cue                 # #Gate + #ApprovalTier
    lanes.cue                 # #Lane
    safety.cue                # #Safety + #Authz + #CostGovernor + #CostCap
    repo.cue                  # #Repo
    context.cue               # #Context
    telemetry.cue             # #Telemetry
    prompts.cue               # #Prompts
    programs.cue              # #Programs
    alarm_webhook.cue         # #AlarmWebhook
```

Granularity rule: one file per top-level subsystem (one `#Config` field group). Co-locate sub-definitions used **only** by that subsystem (`#Secret` lives with `#Secrets`; `#ApprovalTier` lives with `#Gate`; `#CostCap`+`#CostGovernor`+`#Authz` live with `#Safety`).

### §3.2 CUE multi-file package semantics

CUE merges every `*.cue` file declaring the same `package <name>` clause in the same directory into one logical package — like Go packages, but `#Definition` symbols leak across files for free (no import needed within-package). `cue eval ./contracts/schemas/regatta/` and `cue vet regatta.yaml ./contracts/schemas/regatta/` both walk the directory and auto-merge. Confirmed at https://cuelang.org/docs/concept/packages-modules/ (consulted at spec-author time; pin in slice-1 PR body with commit-sha of the docs page).

Within-package symbol resolution: `#Config.secrets: #Secrets` in `root.cue` resolves to `#Secrets` defined in `secrets.cue` with no `import` because both declare `package regattav1`.

### §3.3 Go-side load path

`internal/config/validate.LoadBytes` and `LoadConfig` compile via:

```
schema := ctx.CompileString(schemas.RegattaV1CUE, cue.Filename("regatta.v1.cue"))
```

`schemas.RegattaV1CUE` is a `string` embedded via `//go:embed regatta.v1.cue` (see `contracts/schemas/embed.go`). Two options for the embed in slice-3:

- **(A) Concat embed (chosen)**: `//go:embed regatta/*.cue` → `embed.FS`; concat the files into a single string at package-init. Single-string compile path preserved — `LoadBytes`/`LoadConfig` callers untouched.
- **(B) cue/load directory walk**: replace `ctx.CompileString` with `load.Instances` + filesystem path. More mechanism, more surface, runtime now depends on filesystem (regression vector — current code is build-hermetic via embed).

Pick (A). Per `feedback_default_simpler`: existing `string` API stays, no caller changes. Concat order is alphabetical (deterministic). `cue.Filename("regatta.v1.cue")` is a label only — error messages still useful since each `#Definition` lives in one place and grep-finds it.

Concat sketch (slice-3):

```go
//go:embed regatta/*.cue
var regattaFS embed.FS

// RegattaV1CUE concatenates regatta/*.cue in alphabetical order.
var RegattaV1CUE = mustConcat(regattaFS)
```

### §3.4 Build/test impact

- `cue export contracts/schemas/regatta.v1.cue` callers in `Makefile` / scripts → grep for direct path references; either update to point at the directory (`cue export ./contracts/schemas/regatta/`) or keep a 1-line `regatta.v1.cue` stub that `import`s. Slice-1 confirms `rg "regatta\.v1\.cue" --type-not go` is bounded.
- `internal/config/validate/load.go` callers: zero. `LoadBytes` / `LoadConfigFile` signatures unchanged.
- `internal/config/validate/load_test.go` golden cases: byte-equal `regatta.yaml` must validate; assertion is the same boolean.
- No `Makefile` target rename, no `make check` target rename.

### §3.5 Acceptance

Same `regatta.yaml` validates byte-equal pre/post. Mechanically:

1. Snapshot `internal/config/validate.LoadConfigFile("regatta.yaml")` Go-struct output pre-split (golden JSON).
2. Run same on each slice's branch tip. Diff = ∅.
3. `cue vet regatta.yaml contracts/schemas/regatta/` rejects the same set of malformed fixtures that `cue vet regatta.yaml contracts/schemas/regatta.v1.cue` rejects today (slice-1 captures the failing-fixtures table).

Test: `internal/config/validate/load_test.go::TestLoadConfig_GoldenByteEqual` (RED commit, slice-1) snapshots the decoded Config and compares post-merge.

---

## §4 Migration plan (3 slices)

Each slice is a single file-disjoint PR; cascade-rebase risk is low because slices touch different files within the same directory (CUE auto-merges).

### Slice 1 — split `#Secret` + `#Secrets` (the highest-churn block this session)

Files touched:
- `contracts/schemas/regatta/secrets.cue` (NEW): hoist `#Secret` + `#Secrets` from `regatta.v1.cue`.
- `contracts/schemas/regatta.v1.cue`: remove `#Secret`+`#Secrets` definitions.
- `contracts/schemas/embed.go`: switch embed to `//go:embed regatta.v1.cue regatta/secrets.cue`; concat at init.
- `contracts/schemas/regatta/secrets.cue`: declare `package regattav1`.
- `internal/config/validate/load_test.go`: RED first — add `TestLoadConfig_GoldenByteEqual` capturing the existing `regatta.yaml` decode, verifying split is byte-equal.

Acceptance: `LoadConfigFile("regatta.yaml")` byte-equal Config struct. `cue vet regatta.yaml ./contracts/schemas/` (when `cue.mod` is set up) OR `cue vet regatta.yaml regatta.v1.cue regatta/secrets.cue` validates the existing config.

LOC delta: −40 from monolith, +42 in `secrets.cue`, +6 in `embed.go`. Net +8. Per `feedback_deletion_default`: the **structural deletion** is "1 god-file → N small files"; per-PR LOC is roughly conserved.

### Slice 2 — split `#Scheduler` + `#Adapter` (and the SCHEDULER subsystem)

Note: existing schema has no `#Scheduler` definition; scheduler config is implicit / handled in Go. The cascade-source this session was `#SpecAdapter` + `#Gate` and friends. Re-scoped slice-2 to:

Files touched:
- `contracts/schemas/regatta/spec_adapter.cue` (NEW): hoist `#SpecAdapter`.
- `contracts/schemas/regatta/gates.cue` (NEW): hoist `#Gate` + `#ApprovalTier`.
- `contracts/schemas/regatta.v1.cue`: remove those definitions.
- `contracts/schemas/embed.go`: extend embed glob.

If a future PR adds a top-level `#Scheduler` block (e.g. adapter-sync cadence config promoted from `#CI`), that PR lands in `scheduler.cue` from day one — no monolith touch.

Acceptance: same byte-equal `regatta.yaml` decode test passes.

### Slice 3 — finalize + drop monolith

Files touched:
- Hoist remaining definitions: `#Repo`, `#CI`, `#PRTemplate`, `#Lane`, `#Safety` (with `#Authz` + `#CostGovernor` + `#CostCap`), `#Context`, `#Telemetry`, `#Prompts`, `#Programs`, `#AlarmWebhook`, `#Config`.
- `contracts/schemas/regatta/root.cue` (NEW): owns `#Config` + `package regattav1` + `import "list"`.
- `contracts/schemas/regatta.v1.cue`: delete (or shrink to 1-line `package regattav1` stub if any external caller relies on the path — slice-1 audit confirms).
- `contracts/schemas/embed.go`: simplify glob to `//go:embed regatta/*.cue`.
- Update `Makefile` / scripts that `cue export contracts/schemas/regatta.v1.cue` to use the directory.

Acceptance: same byte-equal decode test passes. `find contracts/schemas/regatta -name '*.cue' | wc -l` = 12 (one per subsystem).

---

## §5 Out of scope

- Schema-version bump (no v2 yet). The `#Config.version: 1` constant stays. Modular split is an **internal organization** of the v1 schema, not a v1→v2 migration.
- `cue.mod/module.cue` package-module reorganization — current import "list" works without a module. If a future feature wants cross-directory CUE imports (`contracts/schemas/regatta-v2/`), a `cue.mod` lands then, not now.
- Replacing `string`-embed with `cue/load` directory walk (option B in §3.3).
- Splitting `internal/config/validate/load.go` Go-side types per subsystem — those follow the schema split only if reviewer cost demands it (separate decision).

---

## §6 Adversarial review

Six attack vectors hunted against this design.

### A1 — Closed-struct semantics preserved across multi-file?

CUE struct definitions starting with `#` are *closed* (no extra fields allowed). When `#Secrets` lives in `secrets.cue` and `#Config.secrets: #Secrets` lives in `root.cue`, does closedness still apply?

**Answer**: yes. CUE merges files first, then evaluates. The closed-struct constraint on `#Secrets` is a property of the definition value, not of the file it lives in. `cue vet` on a `regatta.yaml` with an unknown `secrets.foo` field will still reject. Slice-1 captures a closed-struct rejection test (`testdata/fixtures/secrets_extra_field.yaml`) before-and-after.

### A2 — Definition leakage across packages?

Could a file accidentally declare `package regattav2` and silently break the merge?

**Answer**: `cue vet` walks the directory and complains if mixed-package files exist. Add a one-line `scripts/check-cue-package.sh` to grep `package regattav1` against every `*.cue` under `contracts/schemas/regatta/` and fail closed if any mismatch — OR rely on `cue vet` exit code in `make check`. Defer the explicit script per `feedback_default_simpler` (cue's own gate already catches it).

### A3 — Top-level field union: does `#Config` still see every `#Foo`?

`#Config: { secrets?: #Secrets, ... }` in `root.cue` references `#Secrets` defined in `secrets.cue`. Within-package references resolve.

**Answer**: yes, as long as both files declare the same package. The slice-1 RED test catches any drift in CI.

### A4 — Embed-string concat order ordering: does it matter?

CUE has no order-dependence between sibling definitions (`#Secret` can reference `#Secrets` regardless of file order). Order only matters if a `#Definition` is referenced *before* declaration in **the same file**, and even then CUE resolves by name, not file position. Alphabetical concat (`secrets.cue` < `spec_adapter.cue`) is deterministic; `embed.FS` ranges files in lexical order.

### A5 — `cue.Filename("regatta.v1.cue")` lies about source location post-concat.

**Answer**: yes, error messages will say `regatta.v1.cue:NN` when the offending field lived in `secrets.cue`. Mitigation: switch filename to `regatta.v1.cue.concat` and rely on the offending struct's grep-locality (one definition = one file, so grep-finds the line in seconds). Acceptable per `feedback_default_simpler` — operator UX of "grep `#Secret`" still works.

### A6 — Existing `Makefile` / scripts hardcode `contracts/schemas/regatta.v1.cue`?

Slice-1 audits via `rg "regatta\.v1\.cue" -g '!*.go' -g '!*.md'`. If hits exist (e.g. `Makefile`, `scripts/*.sh`), slice-3 either:
- Keeps `regatta.v1.cue` as a 1-line stub: `package regattav1\n` — preserves path.
- Updates each caller to point at the directory.

Default: 1-line stub. Cheaper than chasing references.

---

## §7 Risk register

| ID | Risk | Likelihood | Severity | Mitigation |
|---|---|---|---|---|
| R1 | Embed glob misses a new `*.cue` file added in a feature PR | Low | High | Glob `regatta/*.cue`; PR-template item "new schema file → confirm embed glob catches it" (no new lint script per `feedback_default_simpler`) |
| R2 | Concat order changes CUE behavior | Negligible | — | CUE is order-independent for definition refs (A4) |
| R3 | `cue vet` external callers (CI, docs) break on path change | Low | Medium | Slice-1 audits via `rg`; slice-3 either updates or stubs |
| R4 | Closed-struct constraint silently dropped | Low | Critical | Slice-1 RED test asserts a closed-struct rejection fixture |
| R5 | Build-time hermeticity lost (filesystem read at runtime) | Negligible | — | Option (A) keeps `//go:embed` — no FS read at runtime |

---

## §8 Implementer brief (3 slices)

Each slice is one PR, file-disjoint, cascade-rebase-safe.

### §8.1 Slice-1 brief — split `#Secret` + `#Secrets`

**Title**: `[REFACTOR] contracts/schemas: hoist #Secret + #Secrets to regatta/secrets.cue (cascade-rebase fix)`

**Scope**:
1. RED: `internal/config/validate/load_test.go::TestLoadConfig_GoldenByteEqual` — snapshot `LoadConfigFile("regatta.yaml")` decode, compare against post-split. Capture failing output in PR body.
2. Create `contracts/schemas/regatta/secrets.cue` with `package regattav1` declaration, hoist `#Secret` + `#Secrets` verbatim.
3. Remove `#Secret`+`#Secrets` from `contracts/schemas/regatta.v1.cue`.
4. Update `contracts/schemas/embed.go`: extend embed to include `regatta/secrets.cue`, concat at init.
5. Capture closed-struct rejection: add `internal/config/validate/testdata/secrets_extra_field.yaml` fixture; assert rejection.
6. GREEN: `make pre-push-check`.
7. Audit external callers: `rg "regatta\.v1\.cue" -g '!*.go' -g '!*.md' -g '!docs/**'` — report hits in PR body, NO action this slice.

**Path lock**: `contracts/schemas/regatta/secrets.cue` (exact slug).
**LOC budget**: ~60 net add, ~40 net delete (40 LOC hoist + 50 new test + 10 embed plumbing).
**Comment budget**: zero new comments inside the hoisted CUE; the definitions already carry godocs.
**Release-notes prefix**: `[REFACTOR]`.

**Reviewer dispatch**: yes — load-bearing under `contracts/schemas/` (per `Reviewer-recommendation` gate).

### §8.2 Slice-2 brief — split `#SpecAdapter` + `#Gate`

**Title**: `[REFACTOR] contracts/schemas: hoist #SpecAdapter + #Gate to regatta/{spec_adapter,gates}.cue (cascade-rebase fix)`

**Scope**:
1. Create `contracts/schemas/regatta/spec_adapter.cue`: hoist `#SpecAdapter`.
2. Create `contracts/schemas/regatta/gates.cue`: hoist `#Gate` + `#ApprovalTier`.
3. Remove from `regatta.v1.cue`.
4. Update embed glob.
5. Reuse `TestLoadConfig_GoldenByteEqual` from slice-1 (already RED-then-GREEN); assert still GREEN.

**Path lock**: `contracts/schemas/regatta/spec_adapter.cue`, `contracts/schemas/regatta/gates.cue`.
**LOC budget**: ~110 hoisted (#SpecAdapter ~28, #Gate + #ApprovalTier ~75), zero new logic.
**Release-notes prefix**: `[REFACTOR]`.

**Pre-merge**: must rebase onto slice-1 merge.

### §8.3 Slice-3 brief — finalize + drop monolith

**Title**: `[REFACTOR] contracts/schemas: finalize per-subsystem split; drop regatta.v1.cue monolith (cascade-rebase fix)`

**Scope**:
1. Hoist remaining definitions to per-subsystem files (§3.1 layout).
2. Create `contracts/schemas/regatta/root.cue` owning `#Config`.
3. Either shrink `regatta.v1.cue` to 1-line stub (`package regattav1`) OR delete (driven by §8.1 audit result).
4. Simplify embed glob to `//go:embed regatta/*.cue`.
5. Update any `Makefile` / scripts hits found in slice-1 audit.
6. Reuse golden test; assert byte-equal.
7. Confirm `find contracts/schemas/regatta -name '*.cue' | wc -l` = expected per-subsystem count.

**Path lock**: `contracts/schemas/regatta/{root,ci,repo,safety,lanes,context,telemetry,prompts,programs,alarm_webhook}.cue`.
**LOC budget**: ~180 hoisted across N files, ~280 deleted from monolith. Net **deletion** ≈ 100 LOC (monolith header comments + `package regattav1` deduped per file; pure structural shrink).
**Release-notes prefix**: `[REFACTOR]`.

**Pre-merge**: must rebase onto slice-2 merge.

---

## §9 Deletion accounting

Per `feedback_deletion_default`:

- Slice 1: structural deletion = "1 god-file as the sole grow-point" → "1 god-file + 1 per-subsystem grow-point". Per-PR LOC ≈ +8.
- Slice 2: structural deletion = same god-file shrinks by 110 LOC.
- Slice 3: structural deletion = monolith **removed** (or 1-line stub). Net repo LOC ≈ −100 across the 3 slices (header comments dedupe + monolith package-clause removal).

The slice that *grows* (slice-1) does so because it adds the golden test. Slice-3 nets the absolute deletion.

---

## §10 Pointers

- `contracts/schemas/regatta.v1.cue` — current monolith (329 LOC).
- `contracts/schemas/embed.go` — Go-side embed (10 LOC).
- `internal/config/validate/load.go` — consumer; `LoadBytes`, `LoadConfig`, `LoadConfigFile`.
- `internal/config/validate/load_test.go` — golden test home.
- CUE multi-file package semantics: https://cuelang.org/docs/concept/packages-modules/ (pin commit-sha in slice-1).
- Prior structural-split precedent: #737 (`internal/orchestrator/state` split), #957 (`docs/engineer/specs/README.md` untracked).
- Cascade-rebase root-cause memory: `feedback_cascade_rebase_root_cause`.

```release-notes
NONE
```
