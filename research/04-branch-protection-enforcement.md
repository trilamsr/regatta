# 04 — Branch protection enforcement: what GitHub (and GitLab) actually enforce

- **Status:** research, feeds Regatta design doc §Threat Model and §Day 1→Day 30 Runbook
- **Author:** Tri Lam
- **Created:** 2026-05-20
- **Scope:** What the platform mechanically enforces vs. what is policy-only. Primary-source citations throughout.

This note exists because two load-bearing claims in `docs/design.md` —
**L6** ("AI review is advisory and rejecting, never approving";
"CODEOWNERS-protected"; "two-key approval on irreversible actions")
and **P2** ("Two-key approval on irreversible actions") — only hold if
the *platform* enforces them. Prompts and policy don't count. Every
gotcha below has been validated against GitHub Docs, GitHub's REST
API surface, or a documented incident.

The Tracecore CODEOWNERS-team-typo incident
([`incidents.md`](../docs/incidents.md) §silent-ignore) was
the trigger: a CODEOWNERS line pointed paths to
`@TraceCoreAI/maintainers`, the team did not exist, and GitHub silently
ignored the rule for weeks. That is one mechanism among many.

## 1. Two enforcement systems: classic Branch Protection vs. Rulesets

GitHub now ships **two parallel** enforcement systems on the same
repository:

- **Classic branch protection rules** — per-branch, per-repo, configured
  under *Settings → Branches*. Fields exposed via the REST API at
  `PUT /repos/{owner}/{repo}/branches/{branch}/protection`
  ([REST docs][rest-bp]).
- **Repository rulesets** — named, layered, can target multiple
  branches and multiple repos via org-level rulesets. Settings →
  Rules → Rulesets. As GitHub documents: *"A ruleset is a named list
  of rules that applies to a repository or to multiple repositories
  in an organization for customers on GitHub Team and GitHub
  Enterprise plans"* ([Rulesets overview][rulesets-overview]).

Rulesets are GitHub's strategic direction (GA Aug 2023; org-level
extensions through 2024–2025). They are **not yet a strict superset**
of classic branch protection — both can be in force simultaneously and
*"the most restrictive version of the rule applies"* when they
conflict ([Rulesets overview][rulesets-overview]). Regatta must
defend on both surfaces or it'll trip on a repo that uses one and not
the other.

### Tier matrix (relevant subset)

| Feature | Free (public) | Free (private) | Team | Enterprise Cloud |
|---|---|---|---|---|
| Classic branch protection | yes | yes | yes | yes |
| Rulesets (repo-level) | yes | yes | yes | yes |
| Org-level rulesets (multi-repo) | no | no | **yes** | yes |
| "Restrict who can push" actor list | yes (public) | no | yes | yes |
| Merge queue | yes (public) | no | yes (public+private since 2024) | yes |
| Required signed commits | yes | yes | yes | yes |
| Bypass list (granular roles/apps/teams) | limited | limited | yes | yes |
| Custom roles for "bypass branch protections" | no | no | partial | yes |

Source: [About protected branches][about-protected] notes the push-restriction tier limit explicitly: *"You can enable branch restrictions in public repositories owned by a GitHub Free organization and in all repositories owned by an organization using GitHub Team or GitHub Enterprise Cloud."*

**Implication for Regatta:** the minimum supported deploy target should
be **GitHub Team**. GitHub Free private repos cannot restrict pushers
to the protected branch, which guts P4. Document this in §Day 1 of the
runbook.

## 2. The canonical "required GitHub configuration for Regatta"

Every setting below is required. Each is named by its UI label *and*
its REST API / ruleset field so `regatta verify-repo-config` can audit
it from CI.

### A. `main` branch protection (classic; or equivalent ruleset)

| # | UI label | REST field | Required value | Rationale |
|---|---|---|---|---|
| 1 | Require a pull request before merging | `required_pull_request_reviews` | enabled | No direct pushes to `main` ([rest][rest-bp]) |
| 2 | Required approving review count | `required_approving_review_count` | **2** | P2 two-key. GitHub never counts the author, so 2 = 2 distinct non-author humans (see §3 below) |
| 3 | Require review from Code Owners | `require_code_owner_reviews` | true | Routes `regatta.yaml`, `prompts/*.md`, `AGENTS.md` through maintainer review |
| 4 | Dismiss stale pull request approvals when new commits are pushed | `dismiss_stale_reviews` | true | Prevents "approve, then fixup-push" attack class |
| 5 | Require approval of the most recent reviewable push | `require_last_push_approval` | true | Closes the [Mercari "Pull Request Hijacking"][mercari] bypass: attacker pushes onto someone else's PR, approves, merges |
| 6 | Restrict who can dismiss pull request reviews | (in `restrictions`) | maintainers only | Prevents the agent (a GitHub App) from dismissing a rejection |
| 7 | Require status checks to pass before merging | `required_status_checks` | enabled, with an aggregator job (see §5) | L1 / regatta gates surface here |
| 8 | Require branches to be up to date before merging | `required_status_checks.strict` | true | Forces L0/L1 re-evaluation against merge-base |
| 9 | Require conversation resolution before merging | `required_conversation_resolution` | true | Prevents merging while a rejecting gate comment is unresolved |
| 10 | Require signed commits | `required_signatures` | true | Defends P10 against author spoofing |
| 11 | Require linear history | `required_linear_history` | true | Auditability; simplifies L0 base/HEAD diff |
| 12 | Do not allow bypassing the above settings | `enforce_admins` | true | See §6 — admin bypass is silently allowed by default |
| 13 | Allow force pushes | `allow_force_pushes` | false | Closes "rewrite history post-approval" |
| 14 | Allow deletions | `allow_deletions` | false | Closes "delete the protected branch and recreate" |
| 15 | Restrict who can push to matching branches | `restrictions` | empty (force PR flow); explicit allow-list for hotfix ops only | Closes direct-push to `main` |
| 16 | Lock branch | `lock_branch` | false on `main`; **true on `release/*` after cut** | P2 for irreversible release branches |
| 17 | Require merge queue | `required_merge_queue` | recommended (see §4) | Re-runs CI on merge-base for every queued PR |

If the repo uses **rulesets** instead, the equivalents are documented
in [Available rules for rulesets][rulesets-rules]. The mapping is
direct; the only consequential delta is the **bypass list semantics**
(§6).

### B. Repo-wide settings (Settings → General / Actions)

- **Settings → Actions → General → "Allow GitHub Actions to create
  and approve pull requests"** → **disabled**. This is the
  [Mercari Pattern 2/3][mercari] mitigation. Otherwise a workflow
  triggered by an attacker can use `GITHUB_TOKEN` to approve PRs.
- **Settings → Code security → Push protection** → enabled (defends
  against secret pushes from compromised orchestrator → P4).
- **Settings → Webhooks → require SSL + secret**. Regatta's PRWatcher
  uses these as its primary input; an unverified webhook is a
  spoofable trust root.

### C. CODEOWNERS contents

- File at `.github/CODEOWNERS`, ≤3 MB (over the limit, *"a CODEOWNERS
  file over this limit will not be loaded, which means that code owner
  information is not shown"* — [About code owners][codeowners]).
- Catch-all line `* @org/maintainers` at top of file. Every other rule
  is an override.
- Regatta-critical paths under **two** owners each (P2):
  ```
  /regatta.yaml          @org/maintainers @org/security
  /prompts/*.md          @org/maintainers @org/security
  /AGENTS.md             @org/maintainers @org/security
  /.github/CODEOWNERS    @org/maintainers @org/security
  ```
- Every team referenced **must already exist** and **must have explicit
  write access**. See §2-gotcha below.

## 3. CODEOWNERS silent-ignore taxonomy

CODEOWNERS is the spine of P2; if it silently no-ops, P2 dies quietly.
Six documented silent-failure modes:

1. **Team doesn't exist.** *"If you specify a user or team that
   doesn't exist or has insufficient access, a code owner will not be
   assigned"* ([About code owners][codeowners]). This is the
   Tracecore incident.
2. **Team has no write access.** Same outcome, same docs.
3. **User left the org / lost access.** Owner silently dropped.
4. **Invalid syntax on a line.** *"If any line in your CODEOWNERS file
   contains invalid syntax, that line will be skipped"* — only flagged
   in the in-repo file viewer's UI highlight, **not** in PR review
   flow.
5. **CODEOWNERS file > 3 MB.** Entire file silently not loaded.
6. **Pattern matches nothing.** No error; the rule is dormant until a
   matching file is touched, which may be never.

GitHub does expose a single audit surface for the first four classes:
`GET /repos/{owner}/{repo}/codeowners/errors`
([REST docs][rest-codeowners]). The response is an array of `{line,
column, source, kind, suggestion, message, path}`. **Regatta must
call this endpoint on every PR webhook** (and on `regatta
verify-repo-config`) and treat any non-empty `errors` as a hard fail.

**Pattern-matches-nothing (case 6) is not flagged by the API.** The
only defense is the canary corpus: include canary PRs that touch each
sensitive path and assert at least one expected owner is auto-requested.

## 4. Required-approval mechanics — does the author count?

GitHub's `required_approving_review_count` is the floor of *non-author*
distinct approvers with write access. This is documented obliquely
through several rules that compose to the same conclusion:

- **The PR author cannot approve their own PR.** GitHub disallows
  this in the review UI (the Approve radio is hidden for the author).
  Multiple sources confirm this is platform-enforced, not just policy
  ([Graphite guide][graphite-approval]).
- **`require_last_push_approval = true`** further forces "approval
  from someone other than the last person who pushed". So if a
  maintainer pushes a fixup to the agent's PR, *that* maintainer can
  no longer be the merging approver — a second human is required
  ([REST docs][rest-bp]).
- **GitHub Apps cannot approve PRs they opened.** The agent
  (`regatta-bot[bot]`) opens the PR; its own installation cannot then
  post an Approve review. Apps *can* post review comments and
  Request Changes, but Approve from the PR author identity is blocked
  the same way it is for users. This is the mechanical backstop for
  the design doc's "AI review is advisory and rejecting, never
  approving."

### What this means for P2 ("two-key approval")

- `required_approving_review_count: 2` + Regatta opens PRs as
  `regatta-bot[bot]` ⇒ floor of 2 *human* approvers. The bot does not
  occupy a slot.
- `require_code_owner_reviews: true` + `regatta.yaml` owned by
  `@org/maintainers @org/security` ⇒ both teams must each contribute at
  least one approval (one per touched section).
- `require_last_push_approval: true` ⇒ if a maintainer pushes a
  hotfix to the agent PR, the maintainer count drops by one and a
  *second* maintainer must approve. The "1-key" exploit (single
  maintainer pushes a fix and self-merges) is closed.

### Gotcha: `required_approving_review_count: 1` ≠ two-key

A common misread is that requiring 1 approval plus CODEOWNERS = 2
people. It does **not**, because a single CODEOWNERS-owning
maintainer's approval satisfies both rules simultaneously. **Two-key
requires `required_approving_review_count ≥ 2` AND
`require_code_owner_reviews: true` AND the touched-path owners must be
disjoint enough that one human cannot satisfy both.**

The Regatta canonical layout achieves this by listing two *teams*
(`@org/maintainers @org/security`) on every Regatta-critical path. A
maintainer who is in *both* teams collapses two-key into one-key —
deploy must verify team membership is disjoint for at least one path
covered by each critical file. `regatta verify-repo-config` should
emit this warning.

## 5. Required status checks — the SKIPPED-as-success hole

This is the most-cited GitHub gotcha and the one that silently bit
Tracecore (PR #74 documented it). Quote from
[Troubleshooting required status checks][troubleshooting] / the
About protected branches doc:

> *"Required status checks must have a `successful`, `skipped`, or
> `neutral` status before collaborators can make changes to a
> protected branch."*

A required check that is **skipped** counts as satisfied. The most
common way this happens: a GitHub Actions job declares
`needs: [pre-flight]`, the `pre-flight` job decides via path filter
that the change is irrelevant and exits, and the downstream required
job is skipped. Result: a green checkmark on a PR that ran zero gates.

### Canonical fix shape: the "alls-green" aggregator

Documented at length by [Wharton][wharton] and [Emmer][emmer]. The
required check must be an aggregator job that:

1. Lists every gate job as `needs:` dependencies.
2. Uses `if: always()` so it cannot itself be skipped.
3. Explicitly fails if any dependency was skipped, cancelled, or
   failed.

```yaml
regatta-gates-gate:
  if: always()
  needs: [l0, l1, l2, l3, l4, l5, custom-gates]
  runs-on: ubuntu-latest
  steps:
    - uses: re-actors/alls-green@release/v1
      with:
        jobs: ${{ toJSON(needs) }}
```

Only `regatta-gates-gate` is listed in `required_status_checks`. The
underlying gates may be skipped freely on path-irrelevant PRs and the
aggregator still surfaces the truth.

**Regatta must ship `gates/l0/testdata/aggregator.yml`** as a canonical
example and `regatta verify-repo-config` must flag any individual gate
job listed in `required_status_checks` directly (a leaky pattern).

### Secondary gotcha: `merge_group` event

When merge queue is enabled, GitHub Actions workflows triggered only
by `pull_request` do **not** re-run when the PR enters the merge
queue. The workflow must also list `merge_group:` as a trigger
([merge_group event][merge-group]):

> *"If your repository uses GitHub Actions to perform required checks
> on pull requests in your repository, you need to update the
> workflows to include the `merge_group` event as an additional
> trigger."*

If the trigger is missing, the queue receives no fresh check report,
the configured `status_check_timeout` elapses, and… *the PR is
dropped from the queue and never merges*. So this fails closed, not
open — good. But the verify command should still flag it because the
*intent* is clearly broken.

## 6. Merge queue — does it re-evaluate or trust PR-time green?

GA'd Jul 2023 ([Changelog announcement][merge-queue-ga]). It
**re-evaluates** by design:

> *"The merge queue will ensure the pull request's changes pass all
> required status checks when applied to the latest version of the
> target branch"* ([Managing a merge queue][merge-queue]).

Mechanism: the queue creates an ephemeral `gh-readonly-queue/main/...`
branch holding `main + queued_PR_1 + ... + queued_PR_N`, dispatches a
`merge_group` event, waits for required checks against that ephemeral
SHA. Only the green ephemeral SHA is fast-forwarded to `main`.

**Implication for Regatta:** L0/L1/L3/L4/L5 *will* re-run at merge
time **if** they are triggered by `merge_group`. The design doc's L0
merge-time re-run claim is mechanically enforced if and only if the
workflow lists the `merge_group` trigger.

**Cost** of re-evaluation: every gate re-runs against every merge
group. A bursty PR backlog can multiply L3/L4/L5 spend by 2–5×.
Mitigation: cache by `(file_diff_hash, gate_id)` and skip if the cached
verdict was `pass`. `regatta gate-run` already keys by `(pr_sha,
gate_id)` per design doc; extend to `(merge_group_base_sha,
file_diff_hash, gate_id)`.

### Merge queue tier note

Merge queue is available on Free (public repos), Team (public+private),
and Enterprise Cloud. **Not available on Free private repos** —
reinforces the Team-minimum recommendation.

## 7. Administrator bypass — the silent-allow default

`enforce_admins` (classic) / equivalent ruleset toggle defaults to
**false**. From the changelog of the new bypass permission
([Bypass branch protections][bypass-permission]):

> *"Previously, to bypass branch protections you had to be an Admin
> which provides additional permissions that may not be needed."*

So by default any repo admin can:

- Merge a PR without required reviews.
- Push directly to `main`.
- Delete `main`.

Each bypass **is** audit-logged. From the audit-log event catalog and
third-party detection guides ([Datadog rule][datadog-bypass]):

- `protected_branch.policy_override` — generic bypass.
- `protected_branch.review_policy_override` — required-reviews
  bypassed.
- `protected_branch.required_status_override` — required-status
  bypassed.
- `protected_branch.review_policy_and_required_status_override` — both.

These events flow to the org audit log (and to the streamed audit log
on Enterprise Cloud). They are *not* visible in the repo's PR UI by
default — the merge looks normal to anyone reading the PR.

**Regatta deploy requirement.** `enforce_admins: true` for the
`main` branch protection. *Then* even admins must go through
the gate stack; the only way around it is to delete or modify the
branch-protection rule itself, which is audit-logged separately
(`protected_branch.update` / `protected_branch.destroy`).

For the **compromised maintainer** scenario the design doc
explicitly does not defend (§Threat Model: "Malicious maintainer"),
note this is the right hook to add a single mitigation later:
streaming the audit log to the same out-of-band sink that holds gate
results, with an alert on any `protected_branch.*_override` event
involving Regatta-critical paths.

## 8. App-vs-user PR author distinctions

The agent opens PRs via a GitHub App installation. Surfaces to verify:

- **Approve own PR.** Blocked. A GitHub App cannot approve a PR
  authored by its own installation, same rule as for users.
- **Approval count.** A GitHub App's approve *would* count toward
  `required_approving_review_count` if the App weren't the author —
  but Regatta should never put the bot in that position. Bot
  identities approving anything is a footgun.
- **Bypass list.** Apps can be added to ruleset bypass lists, which
  is exactly the wrong move; `regatta verify-repo-config` should
  refuse to start if `regatta-bot[bot]` is on the bypass list.
- **Review comments.** Apps freely post comments and Request Changes
  reviews. **Request Changes from a bot is the mechanically enforced
  shape for "AI review is rejecting only":** the bot's `CHANGES_REQUESTED`
  state blocks merge until dismissed (and the dismissal can be
  restricted via `restrict_dismissals`). Approve simply isn't on the
  menu for the bot.
- **GITHUB_TOKEN approvals.** Disabled by the Settings → Actions
  toggle from §2-B. Without that toggle, a malicious workflow under
  `pull_request_target` could approve its own PR ([Mercari][mercari]).

## 9. Self-approval edges

- **Author dismissing reviews.** By default any user with write
  permission can dismiss any review; this includes the author. The
  remedy is "Restrict who can dismiss pull request reviews"
  (`restrict_dismissals`) listing only maintainers.
- **Reviewer re-approving after fixup.** Defaults allow it. Toggle
  on `dismiss_stale_reviews` and the prior approval is auto-dismissed
  when the head SHA changes.
- **Bot review survival.** `dismiss_stale_reviews` also dismisses the
  bot's `CHANGES_REQUESTED` on a new push. This is desirable — the
  bot re-evaluates against the new SHA. But it means a maintainer
  can't rely on a sticky "this PR has been rejected by Regatta" badge;
  the gate must re-run and re-comment on each push. Already implied
  by the design doc's `(pr_sha, gate_id)` idempotency key.

## 10. GitLab parity table

| GitHub feature | GitLab equivalent | Tier | Gap |
|---|---|---|---|
| Branch protection (classic) | Protected branches | Free | Largely equivalent |
| Repository rulesets | Push rules + merge request approval rules | Premium | No single unified "ruleset" object; rules are scattered |
| `required_approving_review_count` | Approval rules → "Approvals required" | **Premium/Ultimate** (Free is advisory only) | **Free can't require any approvals.** Hard floor for Regatta on GitLab is Premium. |
| `require_code_owner_reviews` | CODEOWNERS on protected branch | **Premium/Ultimate** | Same caveat; Free has no CODEOWNERS enforcement |
| `dismiss_stale_reviews` | "Remove all approvals when commits are added" | Premium | Available |
| `require_last_push_approval` | "Prevent approval by users who add commits" | Premium | Available |
| Author can approve own PR | "Prevent approvals by author" / "Prevent approval by merge request creator" | **Free** | Same default (off); easier to flip on Free than required-approvals |
| `required_status_checks` | "Pipelines must succeed" | Free | Available |
| `required_signatures` | "Reject unsigned commits" | Premium | Available |
| Merge queue | Merge trains | **Premium/Ultimate** | Different model (sequential rebase + run); same re-evaluation semantics. Auto-merge ("merge when checks pass") is the lower-tier cousin ([GitLab Auto-merge docs][gitlab-automerge]). |
| Audit log of protection bypass | Audit events | Premium audit-events; Ultimate compliance frameworks | Ultimate gates compliance-grade replication |
| Bot identity restrictions | Service accounts; bot users | Premium | Coarser-grained than GitHub Apps |
| Push restrictions ("only X can push") | Protected branches → "Allowed to push and merge" | Free | Available |
| `enforce_admins` (no admin bypass) | "Code owner approval required" + project-level restriction | Premium | No exact equivalent of `enforce_admins`. Project owners on GitLab Free always can bypass. **This is the largest gap.** |

[GitLab CODEOWNERS docs][gitlab-codeowners],
[GitLab MR approvals][gitlab-approvals].

**Practical recommendation:** Regatta's GitLab adapter (the
`gitlab_issues` SpecAdapter already in scope) should refuse to start
against a project where the matching GitLab tier is Free, with a
clear "required minimum: Premium" message.

## 11. Known-bypass cheat-sheet

| Bypass | Failure mode | Mitigation |
|---|---|---|
| **CODEOWNERS team typo** (Tracecore) | Owners silently not assigned; rule is policy-only | `regatta verify-repo-config` calls `GET /repos/.../codeowners/errors`; fails build on any error |
| **CODEOWNERS pattern matches nothing** | Rule dormant; never fires | Canary PRs touching each declared sensitive path |
| **CODEOWNERS user lost write access** | Owner silently dropped | Same `codeowners/errors` API; periodic re-check |
| **SKIPPED required check** | Aggregator job not configured; required check skipped on path-filtered PRs ⇒ green ✓ | Single `alls-green` aggregator job listed in required checks |
| **Pull Request Hijacking** ([Mercari][mercari]) | Attacker pushes onto someone else's PR, approves, merges | `require_last_push_approval: true` + `dismiss_stale_reviews: true` |
| **GitHub Actions self-approval** | `GITHUB_TOKEN` from a workflow approves PRs | Disable "Allow GitHub Actions to create and approve pull requests" |
| **Admin merge without review** | `enforce_admins: false` (default) | `enforce_admins: true` and stream `protected_branch.*_override` events to the audit sink |
| **Merge queue with no `merge_group` trigger** | Workflow doesn't re-run on merge; queue times out | Verify each required workflow lists `merge_group:` |
| **GitHub App on bypass list** | Bot can push direct to `main` | `verify-repo-config` refuses to start if `regatta-bot[bot]` is in bypass list |
| **Same human in both required CODEOWNERS teams** | "Two-key" collapses to one-key | `verify-repo-config` warns if team membership intersects on Regatta-critical paths |
| **One-approver count + CODEOWNERS** | A single owning maintainer satisfies both rules ⇒ one-key | `required_approving_review_count >= 2` |
| **CODEOWNERS > 3 MB** | Entire file silently not loaded | `verify-repo-config` checks file size |
| **Reviewer re-approves after fixup** | Approve survives a malicious follow-up push | `dismiss_stale_reviews: true` |
| **Author dismisses bot's rejection** | Agent's `CHANGES_REQUESTED` is dismissed by the agent itself | `restrict_dismissals` to maintainers-only |
| **Force push rewrites history post-approval** | The approved commit graph is no longer the merged commit graph | `allow_force_pushes: false` + `require_signed_commits: true` |
| **Branch deleted and recreated** | Re-creation may bypass rules on first push | `allow_deletions: false` + `block_creations: true` on `main` pattern |

## 12. Proposed edits to `docs/design.md`

### Edit 1 — §Threat Model, replace the "Malicious maintainer" bullet

> **Current text** (lines 131–133):
>
> > *Malicious maintainer*. Two-key approval on `regatta.yaml` edits
> > (CODEOWNERS requires ≥2 reviewers) is the only mitigation;
> > collusion is not modeled.

**Proposed replacement:**

> *Malicious maintainer.* Two-key approval is mechanically enforced
> via `required_approving_review_count: 2` + `require_code_owner_reviews:
> true` + `require_last_push_approval: true`, with `regatta.yaml`,
> `prompts/*.md`, and `AGENTS.md` listed under two disjoint
> CODEOWNERS teams (typically `@org/maintainers` and `@org/security`).
> See [research/04-branch-protection-enforcement.md][this-doc] for
> the full enforcement matrix and the SKIPPED-check, CODEOWNERS-typo,
> and admin-bypass gotchas that must be closed by `regatta verify-repo-config`
> at deploy time. Collusion across both owning teams is not modeled.

### Edit 2 — §Day 1 → Day 30 Runbook, add a Day-0 verify step

Insert before "Day 1 — install and validate":

> ### Day 0 — verify target-repo configuration
>
> ```sh
> $ regatta verify-repo-config --repo example/myproject
> ```
>
> Audits all branch protection / ruleset settings, CODEOWNERS errors,
> and required-Actions-toggle state against the canonical set in
> [research/04-branch-protection-enforcement.md §2][this-doc]. Any
> finding in `[required]` is a hard fail. Findings in `[advisory]`
> (e.g., team-membership disjointness) emit warnings.
>
> This step is also wired as a pre-flight check at `regatta serve`
> startup; the daemon refuses to start against a misconfigured repo.

### Edit 3 — §Gate stack, L6 paragraph (line 296)

> **Current:**
>
> > **L6 — human merge** (mandatory, branch-protection enforced). The
> > maintainer reads L0–L5 + custom-gate `GateResult` comments and
> > makes the merge call. AI review is **advisory and rejecting**,
> > never approving.

**Proposed replacement (adds the mechanical hook):**

> **L6 — human merge** (mandatory, branch-protection enforced). The
> maintainer reads L0–L5 + custom-gate `GateResult` comments and
> makes the merge call. AI review is **advisory and rejecting**,
> never approving — mechanically enforced by (i) the bot opening the
> PR (a GitHub App author cannot approve its own PR) and (ii) gate
> rejections surfacing as `CHANGES_REQUESTED` reviews, not Approves.
> Two-key is enforced platform-side via `required_approving_review_count:
> 2` + CODEOWNERS on Regatta-critical paths; see
> [research/04-branch-protection-enforcement.md §4][this-doc].

### Edit 4 — §Trap Catalog, P2 row clarification

The current table cell for P2 (line 156) doesn't say *where* two-key
is enforced. Suggest extending the row to:

> | **P2** | Two-key approval on irreversible actions, enforced via
> branch protection + CODEOWNERS + `require_last_push_approval`
> (research/04 §2). |

## 13. Recommendation: ship `regatta verify-repo-config`

**Yes — and it is high-leverage cheap.** Every gotcha in §11 is
detectable from the GitHub REST API (or GitLab API), takes <2 s to
audit, and otherwise relies on every Regatta deployer perfectly
configuring 17+ toggles. This is the same shape as the
SKIPPED-required-check problem the upstream platform refuses to fix:
the only mitigation is a deterministic out-of-band check.

Concrete v1 scope:

1. Pull branch protection for the configured default branch (REST
   `GET /repos/{owner}/{repo}/branches/{branch}/protection`).
2. Pull active rulesets (REST `GET /repos/{owner}/{repo}/rulesets`).
3. Pull `GET /repos/{owner}/{repo}/codeowners/errors`; fail on any.
4. Pull org-level Actions setting (`actions/permissions`) for
   "Allow GitHub Actions to approve PRs".
5. Diff against the canonical set in §2; categorize findings into
   `[required]` (hard fail) / `[advisory]` (warn).
6. Optional: pull the audit log for the last 30 days and surface any
   `protected_branch.*_override` event involving the target repo.
7. Re-run as a pre-flight at `regatta serve` startup and on each PR
   webhook (cheap; cache for 1 h).

This earns its keep on the first deployment that catches a Tracecore-
class CODEOWNERS typo, an undefaulted `enforce_admins`, or a workflow
missing the `merge_group` trigger.

A symmetric `regatta verify-gitlab-config` lands with the GitLab
adapter and refuses to start against Free-tier projects per §10.

## 14. Open follow-ups

1. **Enterprise Server differences.** The above is validated against
   github.com (Enterprise Cloud and GHEC.com). GHES 3.17+ should be
   API-compatible but the audit-log event names have differed in the
   past ([GHES audit events docs][ghes-audit]). `verify-repo-config`
   should detect host and adjust.
2. **Streamed audit log integration.** Worth a follow-up to tie
   `protected_branch.*_override` events into the same out-of-band
   audit sink (`telemetry.audit_sink`) that holds gate results — closes
   the "compromised maintainer" gap one step further than the current
   threat model concedes.
3. **GitLab tier gating UX.** Need a decision on whether GitLab Free
   is a flat-out non-target or a "warn but proceed". My instinct:
   non-target. Free has neither required approvals nor CODEOWNERS
   enforcement; two-key cannot exist.

## References

- [GitHub: About protected branches][about-protected]
- [GitHub: About rulesets][rulesets-overview]
- [GitHub: Available rules for rulesets][rulesets-rules]
- [GitHub: Managing a branch protection rule][managing-bp]
- [GitHub REST: Branch protection][rest-bp]
- [GitHub REST: List CODEOWNERS errors][rest-codeowners]
- [GitHub: About code owners][codeowners]
- [GitHub: Managing a merge queue][merge-queue]
- [GitHub Changelog: Merge queue GA][merge-queue-ga]
- [GitHub Changelog: Bypass branch protections permission][bypass-permission]
- [GitHub: `merge_group` event][merge-group]
- [Datadog detection: branch-protection override][datadog-bypass]
- [Mercari: How to bypass GitHub's Branch Protection][mercari]
- [Emmer: Skippable GitHub Status Checks Aren't Really Required][emmer]
- [Wharton: Fan-in to a single required GitHub Action][wharton]
- [Graphite: PR approval permissions and rules][graphite-approval]
- [GitLab: Merge request approvals][gitlab-approvals]
- [GitLab: Code Owners][gitlab-codeowners]
- [GitLab: Auto-merge][gitlab-automerge]
- [GHES: Audit log events for your enterprise][ghes-audit]

[about-protected]: https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-protected-branches/about-protected-branches
[rulesets-overview]: https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-rulesets/about-rulesets
[rulesets-rules]: https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-rulesets/available-rules-for-rulesets
[managing-bp]: https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-protected-branches/managing-a-branch-protection-rule
[rest-bp]: https://docs.github.com/en/rest/branches/branch-protection
[rest-codeowners]: https://docs.github.com/en/rest/repos/repos#list-codeowners-errors
[codeowners]: https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/customizing-your-repository/about-code-owners
[merge-queue]: https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/configuring-pull-request-merges/managing-a-merge-queue
[merge-queue-ga]: https://github.blog/changelog/2023-07-12-pull-request-merge-queue-is-now-generally-available/
[bypass-permission]: https://github.blog/changelog/2022-08-18-bypass-branch-protections-with-a-new-permission/
[merge-group]: https://docs.github.com/en/actions/using-workflows/events-that-trigger-workflows#merge_group
[datadog-bypass]: https://docs.datadoghq.com/security/default_rules/github-branch-protection-override/
[mercari]: https://engineering.mercari.com/en/blog/entry/20241217-github-branch-protection/
[emmer]: https://emmer.dev/blog/skippable-github-status-checks-aren-t-really-required/
[wharton]: https://jakewharton.com/fan-in-to-a-single-required-github-action/
[graphite-approval]: https://graphite.com/guides/pull-request-approval-permissions-rules-github
[troubleshooting]: https://docs.github.com/en/pull-requests/collaborating-with-pull-requests/collaborating-on-repositories-with-code-quality-features/troubleshooting-required-status-checks
[gitlab-approvals]: https://docs.gitlab.com/ee/user/project/merge_requests/approvals/
[gitlab-codeowners]: https://docs.gitlab.com/ee/user/project/codeowners/
[gitlab-automerge]: https://docs.gitlab.com/ee/user/project/merge_requests/auto_merge.html
[ghes-audit]: https://docs.github.com/en/enterprise-server@3.17/admin/monitoring-activity-in-your-enterprise/reviewing-audit-logs-for-your-enterprise/audit-log-events-for-your-enterprise
[this-doc]: research/04-branch-protection-enforcement.md
