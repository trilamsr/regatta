---
title: "MVR-2 T3 — Retract primitive (G10 adversarial-gate)"
status: active
summary: "Pre-fetch skeleton for MVR-2 T3. Adversarial gate: retract a PR if a downstream finding lands inside the merge soak window. Closes G10 gap from research-mode vision (`policies/research/retract_claim.rego`) and gives operators a one-call revert path. SCM-adapter call (P3.8 / MVR-1-T5) does the actual revert; this spec wires the gate, audit, and CLI. XS effort (1-2 days). SKELETON."
---

# MVR-2 T3 — Retract primitive (G10) — skeleton spec

_Pre-fetch skeleton, 2026-06-03. Material elaboration deferred to MVR-2 dispatch. Source-of-truth: `docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md` §4 MVR-2-T3. G10 origin: `docs/engineer/briefs/2026-06-01-regatta-research-vision.md` (`retract_claim` Rego rule). Reuses: P3.8 SCM adapter (`docs/engineer/specs/2026-06-02-mvr-1-t3-p38-scm-adapter-gitea-first.md`) for the actual GitHub/Gitea revert call; W8 OPA Authorizer for the two-key authz check._

## 1. Scope

### 1.1 In scope

A single primitive: `regatta retract <pr_number> --reason <text>` that:

1. Authorizes the call via W8 `Authorizer.Check(ctx, principal, "pr.retract", pr_number)` (Rego rule `retract_claim` — two-key by default, single-key for owner-emergency override).
2. Calls `SCMAdapter.RevertPR(ctx, pr_number)` — adapter creates a revert PR + auto-merges if green, OR opens an "intent-to-revert" PR if branch-protection requires review.
3. Writes a substrate event `kind=pr_retracted` with `{pr_number, reason, retracted_by, original_merge_sha, downstream_finding_ref}` payload.
4. Emits OTel counter `regatta_pr_retracted_total{reason_class}` for the cost/audit panel.
5. Optionally posts a follow-up comment on the original PR linking the revert PR (operator-facing audit trail).

### 1.2 Out of scope

- Auto-retract on downstream-finding signal — explicit human-in-the-loop only in v1. The adversarial-gate trigger lands a Linear issue (or substrate event) flagged for operator action; operator decides whether to call `regatta retract`. Auto-retract = followup wedge (post-MVR-3).
- Multi-PR cascade retract (revert N PRs that touched the same file) — followup.
- Cross-fork retract (revert in fork branches that consumed the original) — explicitly rejected; out of scope of single-repo gate.
- UI button for retract — Wave-3 (T5) polish.

## 2. Architecture (high-level)

### 2.1 CLI surface

`cmd/regatta/retract_cmd.go` (~80 LoC):

```
regatta retract <pr_number> --reason <text> [--owner-emergency]
```

Flags:
- `--reason` (required) — free text written to substrate; categorized via simple regex into `{bug, downstream-break, policy-violation, security, other}` for OTel cardinality
- `--owner-emergency` — bypasses two-key, requires `principal.role=owner`; logged with elevated audit weight

### 2.2 Substrate event

`EventKind = "pr_retracted"` registered via `RegisterPayloadValidator` (substrate W1 open-extension pattern). Validator ensures `pr_number > 0`, `reason != ""`, `retracted_by != ""`, `original_merge_sha` matches `[0-9a-f]{40}`, and `downstream_finding_ref` is either empty or a Linear issue ID or substrate event ULID.

Reducer: idempotent — repeated retract calls for the same PR are no-ops (sqlite UNIQUE constraint on `(kind, pr_number)`).

### 2.3 SCM-adapter call

`SCMAdapter.RevertPR(ctx, pr_number)` is a new method added to the P3.8 SCM-adapter interface (MVR-1-T5). For GitHub: `gh pr revert <num>` (gh CLI 2.50+) OR `POST /repos/{owner}/{repo}/pulls/{n}/revert` once gh adapter migrates to native API. For Gitea: `POST /repos/{owner}/{repo}/issues/{n}/comments` with auto-revert payload (Gitea 1.21 lacks native revert; manual revert PR creation).

Caller behavior: if `RevertPR` returns "branch-protection-blocks-automerge", the spec accepts: substrate event is still written (audit trail), follow-up PR opens for human merge.

### 2.4 W8 Rego rule

`policies/research/retract_claim.rego` (already templated in vision doc) gets a `regatta/v1/retract.rego` sibling:

```rego
package regatta.v1.retract
default allow := false
allow {
  input.action == "pr.retract"
  count_approvals(input.resource) >= 2
}
allow {
  input.action == "pr.retract"
  input.owner_emergency
  input.principal.role == "owner"
}
```

Bundled via W8 default `embed.FS` at `regatta/v1/default/retract.rego`.

## 3. Key risks (named, ≥6)

| # | Risk | Mitigation seed |
|---|---|---|
| R1 | Revert PR fails on a merged-then-rebased branch (commit no longer in tree) | SCM adapter returns explicit error class `ErrRevertNotApplicable`; CLI surfaces "manual revert required" + opens linked Linear issue |
| R2 | Two-key authz blocked by single-operator deployments (no second principal exists) | `--owner-emergency` flag exists exactly for this; audit weight + OTel counter let cost panel surface frequency of emergency overrides |
| R3 | Substrate event double-write on retry → reducer must be idempotent | UNIQUE `(kind, pr_number)` enforced at sqlite layer; test `TestRetract_DoubleCallIdempotent` |
| R4 | Revert PR auto-merge floods CI | Caller checks for branch-protection AND existing red CI; on detection, opens intent-to-revert PR without auto-merge |
| R5 | `downstream_finding_ref` is a Linear ID at write but Linear issue deleted later | Reference is a string snapshot, not a live foreign key; audit trail still has the ID even if the issue is gone |
| R6 | Operator retracts a PR that pulls in a security fix → re-introduces CVE | Pre-retract check: SCM adapter scans the PR's labels for `security` and prompts confirmation. `--force` bypasses with audit-event flag |
| R7 | Reason free-text leaks PII | Regex strips emails + bearer-token-shaped strings before substrate write; raw reason kept in encrypted local audit log only (crypto-shred forward-fit) |
| R8 | Cross-tenant retract — operator retracts PR from tenant A while authenticated to tenant B | W8 Authorizer.Check includes tenant scope; rule denies cross-tenant by default |

## 4. Test plan (≥8)

- `TestRetract_HappyPath_OpensRevertPR` — gh adapter mock returns success, substrate event written
- `TestRetract_DoubleCallIdempotent` — second call to same PR is no-op
- `TestRetract_OwnerEmergencyBypassesTwoKey` — `--owner-emergency` + role=owner allows single-call
- `TestRetract_NonOwnerEmergencyDenied` — `--owner-emergency` without role=owner → ErrDenied
- `TestRetract_BranchProtectionBlocked_OpensIntentToRevertPR` — gh adapter returns branch-protection error
- `TestRetract_ReasonPIIStripped` — email/token in reason redacted in substrate event
- `TestRetract_SecurityLabelPromptsConfirm` — PR labeled `security` requires `--force`
- `TestRetract_CrossTenantDenied` — Authorizer denies cross-tenant retract
- `TestRetract_OTelCounterIncrements` — `regatta_pr_retracted_total{reason_class=bug}` increments
- `TestRetractRego_TwoApprovalsAllowed` — Rego rule unit test
- `TestRetractRego_OneApprovalDenied` — Rego rule unit test

## 5. Dependency order

`MVR-1-T5 SCM-adapter` lands first (interface includes `RevertPR`) → `W8 OPA RBAC` lands (Authorizer available) → `T2 multi-tenant` lands (cross-tenant guard) → this spec lands (1-2 days). Total dispatch: ≤1 week if dependencies queued.

## 6. Deferred to dispatch-time elaboration

- gh CLI version probe (does `gh pr revert` exist in operator's installed gh?)
- Linear-issue-creation API call vs substrate-event-only (pick at dispatch time per Linear adapter status)
- Audit-log entry format (JSON line vs structured log)
- Telemetry: should `reason_class` cardinality be capped (≤5)? — yes, enforce at OTel boundary

```release-notes
none (internal — design spec skeleton, pre-fetched for MVR-2)
```
