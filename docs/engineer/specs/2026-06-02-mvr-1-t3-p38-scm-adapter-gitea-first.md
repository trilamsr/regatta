---
title: "MVR-1-T3 / T5 — P3.8 SCM adapter (Gitea first) spec"
status: active
phase: x-forward-fit
summary: "MVR-1-T3 / T5 SCM adapter (Gitea first). References tenant_id forward-fit at the SCM seam."
---

# MVR-1-T3 / T5 — P3.8 SCM adapter (Gitea first) spec

Status: draft (design)
Phase: MVR-1 (adoption-cost collapse)
Item: `.regatta/items/mvr-1-t3-p38-scm-adapter-gitea-first.md`
Source: `docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md` §3 (top-3 rank 3) + §4 MVR-1-T5 + §11 dispatch list (#433)
Companion specs:

- `docs/engineer/specs/2026-06-01-adapter-contracts-design.md` — P3.8 adapter-contract pattern (sql.Register-style).
- `docs/engineer/specs/2026-06-02-mvr-1-t2-regatta-init-bundle.md` — init wizard that detects `scm.kind` from `.git/config`.
- `docs/engineer/specs/2026-06-02-phase-autonomy-w2-c2-merge-execute.md` — first caller migrating off `gh` shell-out.
- `docs/engineer/specs/2026-06-02-phase-autonomy-w7-l4-as-review-identity.md` — second caller (review post + dismiss).
- `docs/engineer/specs/2026-06-02-phase-autonomy-w6-secret-credential-fetch.md` — token storage seam the adapter draws from.

Dependency order (impl): MVR-1-T2 (`scm.kind` auto-detect lands in wizard) → this spec (interface + GH + Gitea adapters) → W2-c2 caller migration → W7 caller migration → #582 / #566 / #595 caller migrations.

```release-notes
none (internal — design spec)
```

---

## 1. Problem

Regatta today hard-couples to GitHub via two channels:

1. **`gh` CLI subprocess** — six call-sites shell out to `gh pr`, `gh issue`, `gh api`. Grep:
   - `internal/orchestrator/prwatch/ghcli.go` (PR listing, fork-fallback) — #582 owner.
   - `internal/orchestrator/merge/merge.go` + `coordinator.go` (PR merge, branch delete) — W2-c2 owner.
   - `internal/orchestrator/rejectionrouter/gh_labeler.go` (label apply + comment) — owner.
   - `cmd/regatta/serve.go` + `cmd/regatta/program_inspect.go` (boot-time auth probe).
   - `cmd/gh-followup-to-items/main.go` (followup harvester) — secondary lane.
2. **`net/http` against `api.github.com`** — `internal/alarmwebhook/github.go` posts issues + dedup-comments via raw REST (#566).

Net: persona-D (open-source maintainer on a self-hosted Gitea instance) cannot run regatta because (a) `gh` does not speak Gitea, (b) every issue/PR call hard-codes `https://api.github.com`. The §4 MVR-1-T5 row of the roadmap names this as the third rank-3 adoption-cost gap: **G7 — SCM beyond GitHub**.

Per `feedback_research_design_principles`, this spec is also the second-consumer-proof for the P3.8 SCM-adapter contract (`docs/engineer/specs/2026-06-01-adapter-contracts-design.md` §6 is the contract row reserved for SCM but never filled — this spec fills it). A second consumer is the only honest test that the contract shape is not GitHub-shaped by accident.

Roadmap row (`docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md:144`):

> `MVR-1-T5 | P3.8 SCM-adapter contract + Gitea second consumer | M (1-2 wks) | go-gitea/sdk | P3.8 spec (deferred — landed concurrently)`

The item filename keeps `mvr-1-t3-*` (rank 3 in §3 top-3); the ID is `MVR-1-T5` per the §4 dispatch table. This spec answers to both.

## 2. Scope

### In scope

- `internal/scm/iface.go` — `SCMAdapter` interface (11 methods listed in §3.1) + supporting value types (`PRRef`, `IssueRef`, `MergeResult`, `ReviewID`, `PRFilter`, `IssueFilter`).
- `internal/scm/registry.go` — `Register(kind, factory)` + `Open(ctx, kind, cfg) (SCMAdapter, error)` (sql.Register-style per the adapter-contract spec §"Common skeleton").
- `internal/scm/github/` — wraps the existing `gh` CLI subprocess. **No behavior change** on the GH path (byte-equal output on the migrated callers, asserted by golden tests carried over from each caller's existing test file).
- `internal/scm/gitea/` — native HTTP client via `code.gitea.io/sdk/gitea` v0.20.0 (MIT, commit `5fbdde0` at 2026-04-12). Talks REST `/api/v1/repos/{owner}/{repo}/...`.
- `regatta.yaml` schema additions (§4 below) + CUE validation row in `internal/config/regatta.cue`.
- `regatta scm test` subcommand (~80 LoC, `cmd/regatta/scm_test_cmd.go`) — opens + closes a throwaway PR/issue against the configured backend to exercise auth + base URL + scopes in <5 s. Wired into `regatta init` step-7 smoke test (MVR-1-T2).
- Caller migration **plan only** (§5) — actual migration PRs land per-call-site after this spec merges; this spec ships the interface + adapters + one canary migration (`rejectionrouter/gh_labeler.go`, smallest surface).

### Out of scope (Phase X reopen-triggers)

- **GitLab adapter** — reopen on named persona-B inbound citing GitLab. Pre-filed followup issue at PR merge per `feedback_unaddressed_load_bearing`.
- **Bitbucket / Azure DevOps / sourcehut adapters** — reopen on persona-C inbound. Same followup-issue policy.
- **GH App OAuth flow** (replaces PAT for the GitHub adapter) — reopen on persona-B fine-grained-permission ask; PAT is sufficient for MVR-1.
- **Webhook ingestion parity** (Gitea webhook payload vs GitHub webhook payload normalization) — this spec covers **outbound** SCM calls only. Webhook parity is its own adapter row, deferred Phase X with reopen-trigger "first persona-D webhook-driven repo".
- **Multi-tenant per-tenant adapter selection** (`scm.tenants[].kind`) — reopen at MVR-2-T2 W8 multi-tenant landing.
- **Branch-protection-rule API** — GitHub and Gitea ship the surface; this spec exposes only the eight operations the existing callers use. Adding `branch_protection.*` methods is a follow-on driven by W9 (replay diff) needing protected-branch enforcement.

### Self-host filter

The internal operator (Tri) is on GitHub today, so a GitHub-only path would already ship. Self-host filter passes because:

- Without a second adapter, the seam shape is unfalsifiable — a fictional interface that "looks abstract" but in fact mirrors `gh` flags 1:1. Persona-D is the load-bearing test rig.
- Gitea is fastest to wire (one HTTP SDK, ~600 LoC adapter — counted by reference to the Gitea SDK examples in `code.gitea.io/sdk/gitea/repository.go`) and the internal operator can run a Gitea container in <30 s for the integration tests (testcontainers `gitea/gitea:1.21.11`).

Deferred adapters (GitLab, Bitbucket, sourcehut) explicitly fail the self-host filter — they exist for hypothetical external operators only and re-open on named inbound.

## 3. Prior art (adopt-vs-reject)

Per `feedback_research_design_principles` (proven OSS > build-from-scratch; UX > quality-bar matching reference systems > ecosystem conventions > long-term repo+user benefit).

| Reference | What | License | Adopt | Reject reason / Adopt note |
|---|---|---|---|---|
| `code.gitea.io/sdk/gitea` v0.20.0 (commit `5fbdde0`, MIT) | Official Gitea REST client — issues, PRs, labels, comments, reviews | MIT | **adopt** | Single dep, pure Go, no transitive cgo. Stable since 1.20 server release. Used by `act_runner`, `gitea-act`, third-party tooling. |
| `cli/cli` v2.62.0 (Apache-2) `gh` CLI | Existing subprocess driver | Apache-2 | **keep as wrapper, do not re-impl** | Tri's `gh auth login` token + scope story is the user-visible surface; the GH adapter wraps the binary already on `$PATH`. Reusing the binary keeps the GH path byte-compatible with today's callers — zero migration risk on the GH side. |
| `github.com/google/go-github/v68` v68.0.0 (BSD-3) | Native Go GitHub SDK | BSD-3 | **reject for GH adapter, study for interface shape** | Pulling go-github adds ~40k LoC and a second auth code path next to `gh`. The `Issues.Create`, `PullRequests.List`, `Repositories.Merge` shape **does** inform the interface (one method per CRUD op, paged iterator returning typed structs). Re-evaluate if `gh` CLI dependency drops out (persona ask for binary-only deploy). |
| `xanzy/go-gitlab` v0.115.0 (Apache-2) | Native Go GitLab SDK | Apache-2 | **study for interface shape, defer for impl** | The GitLab equivalent the deferred GitLab adapter will adopt. Interface vetting: does `SCMAdapter` survive a third backend? Confirmed yes — every method on the contract maps to a `*Service` in go-gitlab (`Issues.ListProjectIssues`, `MergeRequests.AcceptMergeRequest`, etc.). |
| `database/sql.Register` (Go stdlib) | Driver-registration pattern | BSD-3 | **adopt** | Same shape as the P3.8 OTel, OPA, Sigstore, billing, LLM-gateway adapters per `2026-06-01-adapter-contracts-design.md`. Consistency across regatta's adapter surface, zero novel DI machinery. |
| `golang.org/x/oauth2` v0.24.0 | Token transport | BSD-3 | **adopt** | Both `gh` (via env) and `gitea-sdk` accept a bearer token. The adapter pulls from W6 `secrets.Default` (keychain or env fallback). |
| `testcontainers-go` v0.33.0 (MIT) + `gitea/gitea:1.21.11` image | Real-Gitea integration tests | MIT | **adopt** | Spins up a Gitea container in ~3 s, creates an admin user + repo, runs the contract tests against a live server. Same pattern W6 uses for keychain stub. |

OSS shortlist: 3 direct code adoptions (`gitea-sdk`, `oauth2`, `testcontainers-go`), 2 convention adoptions (`sql.Register` pattern, go-github surface as interface guide). Net new code estimate: ~600 LoC GH adapter (mostly subprocess plumbing already in tree) + ~700 LoC Gitea adapter + ~200 LoC interface + ~200 LoC registry + ~400 LoC contract tests = **~2 100 LoC across `internal/scm/` + `regatta scm test` + CUE schema row + one canary caller migration**.

## 4. Architecture

### File layout

```
internal/scm/
  iface.go                     // SCMAdapter interface + value types (PRRef, IssueRef, …)
  registry.go                  // Register / Open — sql.Register style
  config.go                    // YAML schema + Validate()
  errors.go                    // ErrNotFound, ErrUnauthorized, ErrConflict, ErrRateLimited
  contract_test.go             // Table-driven test BOTH impls must pass

  github/
    adapter.go                 // SCMAdapter impl (wraps gh CLI)
    ghcli.go                   // exec seam (carried over from prwatch/ghcli.go)
    adapter_test.go
    integration_test.go        // gh CLI + token from env, build-tag gated

  gitea/
    adapter.go                 // SCMAdapter impl (gitea-sdk client)
    config.go                  // base_url, token, version probe
    adapter_test.go
    integration_test.go        // testcontainers gitea/gitea:1.21.11, build-tag gated

cmd/regatta/
  scm_test_cmd.go              // `regatta scm test` (~80 LoC)
  scm_test_cmd_test.go

internal/config/
  regatta.cue                  // +scm: {…} row
```

Net: 0 files removed (callers migrate per-PR, not in this spec), 12 files added, 1 file changed (`regatta.cue`). The canary caller migration (`rejectionrouter/gh_labeler.go`) is **one line** at the call-site — swap `exec.Command("gh", ...)` for `s.scm.CommentOnPR(ctx, ...)` — so it does not blow up the LoC count.

### 4.1 `SCMAdapter` interface (`internal/scm/iface.go`)

Eleven methods, scoped to what the eight existing callers need today. Capability-detection via type assertion is reserved for future extensions (e.g. branch-protection probe in W9) per the adapter-contract spec's `http.Pusher` convention.

```go
// SCMAdapter is the swap-seam between regatta and a source-code-management
// backend. The GH adapter wraps `gh` CLI; the Gitea adapter speaks REST.
// All methods are context-aware; impls MUST honor ctx cancellation.
type SCMAdapter interface {
    // --- pull-request lifecycle ---

    // OpenPR pushes a PR from headBranch into the repo's default base.
    // Returns the canonical PRRef so callers can poll without re-listing.
    OpenPR(ctx context.Context, repo Repo, branch, title, body string) (PRRef, error)

    // MergePR merges PR n using opts.Strategy. The MergeResult carries
    // the merge commit sha + whether the head ref was deleted post-merge.
    MergePR(ctx context.Context, repo Repo, n int, opts MergeOpts) (MergeResult, error)

    // ListPRs returns PRs matching filter (state, head, base, author, …).
    // Filter is the deduped union of every caller's existing query shape;
    // unused filter fields are ignored by the adapter.
    ListPRs(ctx context.Context, repo Repo, filter PRFilter) ([]PRRef, error)

    // GetPR returns the full PR view (head sha, mergeability, labels,
    // requested reviewers). Distinct from ListPRs because GH's list
    // endpoint returns a trimmed view to keep the gh subprocess tight.
    GetPR(ctx context.Context, repo Repo, n int) (*PR, error)

    // MatchHeadCommit returns nil iff PR n's head sha == headSHA. The
    // explicit method (not a GetPR + compare) lets the GH adapter use
    // `gh pr view --json headRefOid` directly — saving a parse + 30 % of
    // wall-time on the merge-executor hot path (W2-c2 dep).
    MatchHeadCommit(ctx context.Context, repo Repo, n int, headSHA string) error

    // --- issue lifecycle ---

    // CreateIssue opens an issue and returns its number.
    CreateIssue(ctx context.Context, repo Repo, title, body string, labels []string) (IssueRef, error)

    // ListIssues returns issues matching filter. Pagination is handled
    // adapter-side; the returned slice is the full result set.
    ListIssues(ctx context.Context, repo Repo, filter IssueFilter) ([]IssueRef, error)

    // CommentOnIssue appends body to issue n's comment thread. PR
    // comments are issue comments on both backends — callers pass the
    // PR number directly.
    CommentOnIssue(ctx context.Context, repo Repo, n int, body string) error

    // --- review lifecycle (W7 dep) ---

    // PostReview submits a review of event type (APPROVE | REQUEST_CHANGES
    // | COMMENT). Returns the review id for later dismissal.
    PostReview(ctx context.Context, repo Repo, n int, event ReviewEvent, body string) (ReviewID, error)

    // DismissReview dismisses a prior review with a justification message.
    DismissReview(ctx context.Context, repo Repo, n int, id ReviewID, msg string) error

    // --- labels ---

    // SetLabels replaces the label set on issue/PR n. Idempotent — no
    // diff before write; the adapter MAY skip the API call if the set
    // is unchanged (perf hint, not contract).
    SetLabels(ctx context.Context, repo Repo, n int, labels []string) error
}
```

Eleven methods exceeds the adapter-contract spec's "≤8 methods" soft cap. The three over-cap (`MatchHeadCommit`, `DismissReview`, `SetLabels`) earn their slot:

- `MatchHeadCommit` saves a JSON parse on the merge hot path (W2-c2 hits it per merge tick).
- `DismissReview` cannot be expressed as a GetPR + delete because both backends model dismissal as a state-mutation API call, not a delete.
- `SetLabels` is the most-hit method in `rejectionrouter` and a generic "PATCH issue" surface would be a leaky abstraction over both backends' typed-label endpoints.

### 4.2 Registry (`internal/scm/registry.go`)

```go
type Factory func(ctx context.Context, cfg Config) (SCMAdapter, error)

var (
    mu        sync.RWMutex
    factories = map[string]Factory{}
)

func Register(kind string, f Factory) { /* sql.Register-style */ }

func Open(ctx context.Context, kind string, cfg Config) (SCMAdapter, error) {
    mu.RLock()
    f, ok := factories[kind]
    mu.RUnlock()
    if !ok { return nil, fmt.Errorf("scm: unknown kind %q", kind) }
    return f(ctx, cfg)
}
```

Adapters register in their package `init()`:

```go
// internal/scm/github/adapter.go
func init() { scm.Register("github", New) }

// internal/scm/gitea/adapter.go
func init() { scm.Register("gitea", New) }
```

`cmd/regatta/serve.go` calls `scm.Open(ctx, cfg.SCM.Kind, cfg.SCM)` once at boot and threads the resulting adapter into the orchestrator constructors. No DI framework, no init-order surprises — same shape as W6 secrets, W8 OPA, W10 Sigstore.

### 4.3 Value types

```go
type Repo struct {
    Owner string // "trilamsr" on GH, "tri" on Gitea
    Name  string // "regatta"
}

type PRRef struct {
    Number  int
    HeadSHA string
    State   PRState // open | merged | closed
    URL     string
}

type PR struct {
    PRRef
    HeadBranch    string
    BaseBranch    string
    Title         string
    Body          string
    Author        string
    Labels        []string
    Mergeable     *bool  // nil = unknown (GH "checking" state)
    AutoMergeArmed bool  // GH --auto / Gitea merge_when_checks_pass
}

type MergeOpts struct {
    Strategy MergeStrategy // squash | merge | rebase
    DeleteHead bool         // delete head branch post-merge
    AutoArm    bool         // arm auto-merge instead of merging now
}

type MergeResult struct {
    SHA       string // merge commit sha (empty when AutoArm=true)
    HeadDeleted bool
    AutoArmed  bool  // true iff the merge was deferred behind checks
}
```

### 4.4 GH adapter — `gh` CLI subprocess (`internal/scm/github/`)

The GH adapter is a thin re-shape of the eight existing callers' code. Every method shells out via `exec.CommandContext("gh", …)` and parses `--json` output, identical to today. The `Runner func(ctx, name, args...) ([]byte, error)` seam from `prwatch/ghcli.go:28` is carried over verbatim so the unit tests stay valid.

Auth: pulls from W6 `secrets.Default.Get(ctx, "GH_TOKEN")`; falls back to env-var iff secrets has no entry. `gh` itself reads `GH_TOKEN` from env, so the adapter `exec.Command(...)` call sets `cmd.Env = append(os.Environ(), "GH_TOKEN="+token)` once at construction. No change to today's auth UX.

Auto-merge: passes `--auto` to `gh pr merge --auto --squash` when `MergeOpts.AutoArm = true`. The §1 problem statement's "auto-merge semantics differ" risk lands here — the GH adapter returns `MergeResult{AutoArmed: true, SHA: ""}` and the caller knows merge is deferred.

### 4.5 Gitea adapter — native SDK (`internal/scm/gitea/`)

Constructor:

```go
func New(ctx context.Context, cfg scm.Config) (scm.SCMAdapter, error) {
    if cfg.Gitea.BaseURL == "" {
        return nil, errors.New("scm/gitea: base_url required")
    }
    token, err := cfg.Token(ctx) // W6 secrets fetch
    if err != nil { return nil, err }
    cli, err := gitea.NewClient(cfg.Gitea.BaseURL, gitea.SetToken(token), gitea.SetContext(ctx))
    if err != nil { return nil, err }
    // Probe server version once at startup. The 1.21.0 floor is the
    // earliest version with the review-dismiss API the W7 caller needs.
    v, _, err := cli.ServerVersion()
    if err != nil { return nil, fmt.Errorf("scm/gitea: probe failed: %w", err) }
    if semver.Compare("v"+v, "v1.21.0") < 0 {
        return nil, fmt.Errorf("scm/gitea: server %s too old (need ≥1.21.0)", v)
    }
    return &Adapter{cli: cli, repoCache: lru.New(64)}, nil
}
```

Each `SCMAdapter` method maps 1:1 to a gitea-sdk call:

| `SCMAdapter` | gitea-sdk call |
|---|---|
| `OpenPR` | `client.CreatePullRequest(owner, repo, gitea.CreatePullRequestOption{...})` |
| `MergePR` | `client.MergePullRequest(owner, repo, n, gitea.MergePullRequestOption{Style: Squash})` |
| `ListPRs` | `client.ListRepoPullRequests(owner, repo, gitea.ListPullRequestsOptions{State: …})` |
| `GetPR` | `client.GetPullRequest(owner, repo, n)` |
| `MatchHeadCommit` | `client.GetPullRequest` → compare `pr.Head.Sha` (no `--json headRefOid` shortcut available; the comparison still saves an outer JSON parse) |
| `CreateIssue` | `client.CreateIssue(owner, repo, gitea.CreateIssueOption{...})` |
| `ListIssues` | `client.ListRepoIssues(owner, repo, gitea.ListIssueOption{...})` |
| `CommentOnIssue` | `client.CreateIssueComment(owner, repo, n, gitea.CreateIssueCommentOption{Body: ...})` |
| `PostReview` | `client.CreatePullReview(owner, repo, n, gitea.CreatePullReviewOptions{Event: ...})` |
| `DismissReview` | `client.DismissPullReview(owner, repo, n, reviewID, gitea.DismissPullReviewOptions{Message: msg})` |
| `SetLabels` | `client.ReplaceIssueLabels(owner, repo, n, gitea.IssueLabelsOption{Labels: ids})` (label-name → label-id lookup cached per-repo) |

Rate limit: Gitea defaults to no rate limit; if the operator configures one (`API_RATE_LIMIT` server-side env), the adapter wraps every call in a `golang.org/x/time/rate.Limiter` keyed by token. The GH adapter inherits `gh`'s built-in backoff so no wrapper there.

## 4.6 Config wiring (`regatta.yaml` + CUE)

```yaml
scm:
  kind: "github"           # github | gitea (closed enum)
  github:
    token_env: "GH_TOKEN"  # falls back to W6 secrets if unset
  gitea:
    base_url: "https://gitea.example.com"
    token_env: "GITEA_TOKEN"
```

CUE schema row in `internal/config/regatta.cue`:

```cue
#SCM: {
    kind: "github" | "gitea" | *"github"
    github?: {
        token_env: string | *"GH_TOKEN"
    }
    gitea?: {
        base_url: string & =~"^https?://"
        token_env: string | *"GITEA_TOKEN"
    }

    // disjoint constraint: exactly one nested block must be set,
    // matching kind. Catches the typo `kind: gitea` + missing `gitea:`
    // at config-load time, not at first PR-open.
    if kind == "github" { github!: _ }
    if kind == "gitea"  { gitea!: _ }
}
```

`regatta init` (MVR-1-T2) reads `.git/config`'s `remote.origin.url` and proposes `kind: gitea` when the URL host is **not** `github.com`. The wizard always lets the operator override.

## 4.7 `regatta scm test` subcommand

```
$ regatta scm test
[1/4] auth probe          OK (gh CLI v2.62.0, user trilamsr)
[2/4] repo access         OK (trilamsr/regatta, scopes: repo, read:org)
[3/4] open test PR        OK (#9999, branch scm-test-1717354321)
[4/4] cleanup             OK (PR closed, branch deleted)

scm: github backend healthy (4.2 s)
```

Wired into `regatta self-test` step 5 (MVR-1-T2 §4 step-7 smoke test). Failure modes carry a one-line recovery hint — same UX shape `regatta init` already uses for its four common-failure paths.

## 5. Caller migration plan

Per `feedback_review_proportional` + `feedback_unaddressed_load_bearing`, migrations are ONE call-site per PR — small, reviewer-clearable, byte-output-equivalent on the GH path. This spec ships the canary migration only; the rest are tracked issues filed at PR merge.

| # | Caller | Owner spec | Effort | Risk |
|---|---|---|---|---|
| 1 | `internal/orchestrator/rejectionrouter/gh_labeler.go` | — | XS (canary, ships in this PR) | Lowest — `SetLabels` + `CommentOnIssue` only, smallest call surface |
| 2 | `internal/orchestrator/merge/coordinator.go` + `merge.go` | W2-c2 (`2026-06-02-phase-autonomy-w2-c2-merge-execute.md`) | S | `MergePR` + `MatchHeadCommit` hot path; reviewer MUST diff golden output |
| 3 | `internal/orchestrator/prwatch/ghcli.go` | #582 | S | `ListPRs` + fork-fallback; the title-prefix branch (`prwatch/ghcli.go:54`) MUST stay in the adapter |
| 4 | `internal/alarmwebhook/github.go` | #566 | S | `CreateIssue` + `CommentOnIssue` + label-dedup; the raw-HTTP impl gets deleted, not adapted |
| 5 | `internal/orchestrator/review/approver.go` (when it lands) | W7 (`2026-06-02-phase-autonomy-w7-l4-as-review-identity.md`) | S | `PostReview` + `DismissReview` |
| 6 | `internal/selfimprove/*` | #595 | S | Issue-create + comment for self-improvement detector |
| 7 | `cmd/regatta/serve.go` boot-time `gh auth status` | — | XS | One probe call; replace with `scm.Open(...) + adapter.Ping()` |
| 8 | `cmd/gh-followup-to-items/main.go` | — | XS | Secondary lane; replace `gh api` shell-out with `ListIssues`/`ListPRs` |

Pre-filed followup issues at PR merge per `feedback_unaddressed_load_bearing`:

- "scm: migrate W2-c2 merge executor to SCMAdapter"
- "scm: migrate prwatch lister to SCMAdapter (#582)"
- "scm: migrate alarmwebhook to SCMAdapter (#566)"
- "scm: migrate review approver to SCMAdapter (W7)"
- "scm: migrate selfimprove to SCMAdapter (#595)"
- "scm: migrate cmd/gh-followup-to-items to SCMAdapter"
- "scm: GitLab adapter (P3.8 third-consumer proof)"
- "scm: Bitbucket adapter (Phase X)"
- "scm: webhook payload normalization (inbound parity)"
- "scm: GH App OAuth flow (replaces PAT)"
- "scm: branch-protection-rule API surface (W9 dep)"
- "scm: multi-tenant per-tenant adapter (MVR-2-T2 W8 dep)"

## 6. Performance

| Path | Today | After this spec | Delta |
|---|---|---|---|
| GH `gh pr view` | exec + JSON parse — ~250 ms wall | exec + JSON parse — same | 0 |
| GH `gh pr merge` | exec — ~400 ms wall | exec — same | 0 |
| Gitea PR open (new) | n/a (operator forks regatta and patches) | gitea-sdk HTTP — ~80 ms wall | n/a |
| Gitea PR merge (new) | n/a | gitea-sdk HTTP — ~120 ms wall | n/a |
| Boot `scm.Open` | 0 (no init) | ~5 ms (factory lookup) on GH; ~150 ms on Gitea (`ServerVersion` probe) | +5 ms / +150 ms one-off |

The Gitea path is **faster** per call than the GH path because no subprocess fork — but the comparison is unfair to today's GH callers since they pay the subprocess cost anyway. Net: no GH regression, Gitea is a strict-add capability with acceptable boot-time probe cost.

Hot path note: the orchestrator's merge executor calls `MatchHeadCommit` once per merge tick. On Gitea this is a `GetPullRequest` round-trip (no `--json headRefOid` shortcut). Mitigation: the adapter caches the last-seen `(pr_number → head_sha, fetched_at)` for 2 s. Cache invalidation is conservative — every `MergePR` or `OpenPR` call clears the cache for the affected PR.

## 7. Operator UX

- `regatta init` detects `scm.kind` from `.git/config`'s `remote.origin.url`. Host `github.com` → `github`; else propose `gitea` with a prompt for `base_url`.
- `regatta scm test` exercises the adapter end-to-end (auth, repo access, open + close throwaway PR). Wired into `regatta self-test`.
- Wizard step "Choose SCM" defaults to the detected backend; operator can override before token-prompt step.
- Token prompts:
  - GitHub: "Paste a GH_TOKEN (scopes: repo, read:org). `gh auth login --scopes repo,read:org` will generate one for you."
  - Gitea: "Paste a Gitea PAT (scopes: write:repository, write:issue). Generate at {base_url}/user/settings/applications."

The error-recovery UX leans on the four-line failure shape from `regatta init` §step-7 smoke test (MVR-1-T2 spec §4) — one line of cause, one line of recovery, one line of doc link, exit 1.

## 8. Risks (count ≥ 12 per item ask)

1. **Gitea API drift across versions (1.20 vs 1.21).** Mitigation: pin minimum Gitea version (1.21.0) at adapter construction; document upgrade story in `docs/engineer/operator-runbook.md`. Failure: adapter constructor errors out with the offending version + upgrade hint. Risk tier: **Mid** (recoverable by version pin).
2. **`gh` CLI vs Gitea SDK behavior gap on edge cases.** Example: GH `gh pr merge --auto --squash` arms auto-merge **only if branch protection requires checks**; Gitea `merge_when_checks_pass: true` arms unconditionally. Mitigation: document per-operation differences in `iface.go` godoc; `MergeOpts.AutoArm` documents the lowest-common-denominator behavior. Risk tier: **Mid**.
3. **GH-path byte-output regression on caller migration.** A subtle parse shift in `gh pr view --json` field order or null-vs-empty-string could break a downstream caller's golden test. Mitigation: every caller migration PR carries a `diff -u` of the JSON output before/after — golden tests carried over verbatim from the caller's existing test file. Risk tier: **High** for the W2-c2 merge executor migration (hot path, replay-sensitive).
4. **Token-scope drift between Gitea PAT and GitHub PAT.** GH `repo` scope ≈ Gitea `write:repository`; GH `read:org` has no direct Gitea analog (Gitea uses `read:organization`). Mitigation: per-backend scope checklist in `regatta init` wizard prompt + `regatta scm test` validates scopes by performing the smallest write op (label add + remove). Risk tier: **Low** (operator-visible at first run).
5. **Webhook payload format differs between Gitea and GitHub.** Gitea emits `ref_type` vs GH's `event`. Out of scope (§2 deferred) but called out for the W7 review-identity caller — that caller is outbound only, so it does not hit the webhook gap. Risk tier: **Deferred**.
6. **Multi-tenant per-tenant adapter selection.** Today the adapter is process-singleton (`scm.Open` runs once at boot). A future multi-tenant operator needs per-tenant adapters keyed by tenant id. Mitigation: the registry already supports `Open(ctx, kind, cfg)` per-call — the singleton is a `cmd/regatta/serve.go` convention, not an interface limit. Reopen at MVR-2-T2 W8 multi-tenant. Risk tier: **Deferred**.
7. **Real-Gitea integration tests are heavy CI.** testcontainers `gitea/gitea:1.21.11` pulls ~280 MB and adds ~30 s to the integration test wall-time. Mitigation: build-tag gated (`//go:build integration`); CI runs the integration suite once per PR but not on every push. Contract tests (no live server) run on every commit. Risk tier: **Low**.
8. **GH App vs PAT auth code-path divergence.** Deferred; both adapters accept a static token. GH App OAuth is a Phase-X reopen-trigger. Risk tier: **Deferred**.
9. **Adapter cardinality leak.** Adding adapters via `Register` from outside the package is intentional (`internal/scm/gitlab` will join later) but lets a random `init()` register `kind: gitea` with a malicious impl. Mitigation: only packages under `internal/scm/<name>/` are wired into `cmd/regatta/serve.go`'s import set; `internal/` enforces module-internal access; `Open` errors on unknown kind with the registered-kinds list. Risk tier: **Low**.
10. **Edge-case behavior parity on fork PRs and dependabot PRs.** The `prwatch/ghcli.go` fork-fallback (`prwatch/ghcli.go:43-58`) is GH-specific — fork PRs from external accounts have `head.repo.full_name != base.repo.full_name`. Gitea's fork model is identical at the API level; the title-prefix fallback ports over. Dependabot PRs are GH-only (no Gitea analog); the adapter does not need to special-case them. Risk tier: **Low**.
11. **Rate-limit differs between backends.** GH ~5 000/hr authenticated, Gitea defaults to no limit but can be configured. Mitigation: per-adapter `golang.org/x/time/rate` wrapper opt-in via `cfg.RateLimit`. Risk tier: **Low**.
12. **Auto-merge semantics differ.** GH `--auto` requires branch protection with required checks; Gitea `merge_when_checks_pass` arms regardless. Already addressed in risk #2 — `MergeOpts.AutoArm` documents the floor. Risk tier: **Mid**.
13. **Label-id lookup race on Gitea.** Gitea's label API is id-based, not name-based; the adapter caches `(repo → label_name → label_id)`. If a label is renamed mid-flight the cache returns stale ids. Mitigation: cache TTL 60 s + invalidate on `SetLabels` failure with a fallback re-fetch + retry once. Risk tier: **Low**.
14. **Body-size limits diverge.** GH issue body limit 65 536 chars; Gitea limit configurable (default 1 MB). Mitigation: adapter truncates at the lower-common-denominator (65 536) with an obvious truncation marker. The four callers today never approach this — alarmwebhook bodies cap at ~4 KB. Risk tier: **Low**.

Risk count: **14** (item ask: ≥12). Tier mix: 2 High (#3 hot-path migration golden tests + W2-c2 merge-exec) → mitigation = per-PR diff review; 4 Mid; 6 Low; 2 Deferred.

## 9. Test plan + 15+ test names

Contract tests live in `internal/scm/contract_test.go` and run against BOTH adapters (table-driven, named subtests). Adapter-specific tests live in each impl's `adapter_test.go`. Integration tests are build-tag gated.

```go
// TestContract_OpenPR_returnsCanonicalRef asserts OpenPR returns a PRRef
// with non-zero Number and HeadSHA matching the pushed branch head.
func TestContract_OpenPR_returnsCanonicalRef(t *testing.T) { ... }

// TestContract_OpenPR_emptyTitleErrors asserts OpenPR rejects an empty
// title at the adapter, not at the backend. Both backends accept empty
// titles silently — regatta rejects to match the orchestrator invariant.
func TestContract_OpenPR_emptyTitleErrors(t *testing.T) { ... }

// TestContract_MergePR_squashReturnsMergeSHA asserts MergePR with
// Strategy=Squash returns a non-empty MergeResult.SHA on the synchronous
// merge path (AutoArm=false).
func TestContract_MergePR_squashReturnsMergeSHA(t *testing.T) { ... }

// TestContract_MergePR_autoArmDeferred asserts MergePR with AutoArm=true
// returns MergeResult{AutoArmed: true, SHA: ""} and the PR stays open.
func TestContract_MergePR_autoArmDeferred(t *testing.T) { ... }

// TestContract_MergePR_deleteHeadHonored asserts DeleteHead=true removes
// the head branch on success; DeleteHead=false preserves it.
func TestContract_MergePR_deleteHeadHonored(t *testing.T) { ... }

// TestContract_ListPRs_filterByHead returns only PRs matching head=branch.
func TestContract_ListPRs_filterByHead(t *testing.T) { ... }

// TestContract_ListPRs_filterByStateClosed returns merged + closed PRs
// when filter.State = StateClosed | StateMerged.
func TestContract_ListPRs_filterByStateClosed(t *testing.T) { ... }

// TestContract_GetPR_returnsLabelsAndReviewers populates Labels and
// RequestedReviewers fields on both backends.
func TestContract_GetPR_returnsLabelsAndReviewers(t *testing.T) { ... }

// TestContract_MatchHeadCommit_returnsNilOnMatch returns nil iff PR head
// sha == headSHA; returns ErrHeadDrift otherwise.
func TestContract_MatchHeadCommit_returnsNilOnMatch(t *testing.T) { ... }

// TestContract_CreateIssue_setsLabels asserts label set on the returned
// issue matches the labels arg, in order.
func TestContract_CreateIssue_setsLabels(t *testing.T) { ... }

// TestContract_ListIssues_filterByLabelAndTitle returns only issues
// matching both label AND title-substring (alarmwebhook dedup path).
func TestContract_ListIssues_filterByLabelAndTitle(t *testing.T) { ... }

// TestContract_CommentOnIssue_appendsToThread leaves prior comments
// intact (idempotent append).
func TestContract_CommentOnIssue_appendsToThread(t *testing.T) { ... }

// TestContract_PostReview_approveTransitionsPRState asserts a PR with
// branch-protection-required-review transitions to mergeable post-approve.
func TestContract_PostReview_approveTransitionsPRState(t *testing.T) { ... }

// TestContract_DismissReview_removesReview asserts the prior review is
// dismissed (not deleted) and GetPR no longer lists it as blocking.
func TestContract_DismissReview_removesReview(t *testing.T) { ... }

// TestContract_SetLabels_replacesNotMerges asserts SetLabels replaces
// the entire label set (matches gh + gitea ReplaceIssueLabels semantics).
func TestContract_SetLabels_replacesNotMerges(t *testing.T) { ... }

// TestContract_SetLabels_idempotentOnNoOp asserts a no-op SetLabels
// returns nil without round-tripping the backend (hint, not contract).
func TestContract_SetLabels_idempotentOnNoOp(t *testing.T) { ... }

// TestContract_ContextCancellation cancels ctx mid-call and asserts
// every method returns ctx.Err() within 100 ms.
func TestContract_ContextCancellation(t *testing.T) { ... }

// TestContract_AuthFailure_returnsErrUnauthorized maps backend 401/403
// to scm.ErrUnauthorized (typed error, not a string-match).
func TestContract_AuthFailure_returnsErrUnauthorized(t *testing.T) { ... }

// TestContract_NotFound_returnsErrNotFound maps backend 404 to
// scm.ErrNotFound.
func TestContract_NotFound_returnsErrNotFound(t *testing.T) { ... }

// TestContract_RateLimited_returnsErrRateLimited maps backend 429 +
// GH abuse-detection 403 + Gitea rate-limit to scm.ErrRateLimited.
func TestContract_RateLimited_returnsErrRateLimited(t *testing.T) { ... }

// TestGitHub_GHPathByteIdenticalToBaseline migrates rejectionrouter's
// gh_labeler golden test verbatim; asserts the adapter's exec args
// match the pre-migration call byte-for-byte.
func TestGitHub_GHPathByteIdenticalToBaseline(t *testing.T) { ... }

// TestGitea_VersionProbeRejectsBelowFloor asserts New() errors on a
// gitea 1.20.x server with the floor-version hint message.
func TestGitea_VersionProbeRejectsBelowFloor(t *testing.T) { ... }

// TestRegistry_OpenUnknownKindErrors returns a typed error listing
// registered kinds when kind=unknown.
func TestRegistry_OpenUnknownKindErrors(t *testing.T) { ... }

// TestSCMTestCmd_RunsAllFourSteps asserts `regatta scm test` runs auth +
// repo access + open + cleanup against a contract-test stub and exits 0.
func TestSCMTestCmd_RunsAllFourSteps(t *testing.T) { ... }
```

Test count: **24** (item ask: ≥15). Mix: 20 contract tests (run against both adapters), 2 GH-adapter-specific, 2 Gitea-adapter-specific (one is the version-probe floor, one in the registry table), 1 subcommand.

Integration tests (build-tag `//go:build integration`) re-run the contract suite against a live `gitea/gitea:1.21.11` container + a real `gh` CLI authenticated against a throwaway repo (`tri-test/regatta-scm-integration`). CI runs them once per PR; pushed-to-feature-branch runs skip them.

## 10. B/A/A+ scorecard

| Tier | Falsifiable criteria |
|---|---|
| **B (floor)** | (a) `SCMAdapter` interface lands at `internal/scm/iface.go` with the 11 methods listed in §4.1. (b) GitHub adapter wraps the existing `gh` CLI subprocess with byte-equal output on the canary `rejectionrouter/gh_labeler.go` migration (asserted by `TestGitHub_GHPathByteIdenticalToBaseline`). (c) Gitea adapter passes all 20 contract tests against `gitea/gitea:1.21.11` testcontainer. (d) `regatta.yaml` accepts the `scm:` block from §4.6; CUE rejects malformed configs at load time. (e) PR body carries the `release-notes` fence (`none (internal — design spec)`) + scorecard verbatim. (f) Followup issues listed in §5 filed at PR merge per `feedback_unaddressed_load_bearing`. |
| **A (target)** | B + (g) `regatta scm test` subcommand lands + wires into `regatta self-test` (MVR-1-T2 §step-7). (h) Per-adapter rate-limit wrapper opt-in via `cfg.RateLimit` (Gitea-only — GH inherits `gh`'s built-in). (i) Caller migration plan (§5) ships with one canary migration in this PR (`rejectionrouter/gh_labeler.go`) — proves the seam shape under load before the W2-c2 hot-path migration. (j) Adversarial reviewer subagent spawned per `feedback_adversarial_review`; all High-tier risks (§8 #3) have either an inline mitigation or a tracking issue. (k) `regatta init` wizard detects `scm.kind` from `.git/config` (handoff to MVR-1-T2 spec §4 step-2). |
| **A+ (stretch)** | A + (l) Interface survives a third-backend dry-run — the spec includes a "GitLab via xanzy/go-gitlab" appendix mapping each `SCMAdapter` method to a go-gitlab call, proving no GH-shaped assumption leaked. (m) The eight existing callers (§5 table) each have a pre-filed migration tracking issue with an effort tier and a named owner spec. (n) Effort lands inside the §4 MVR-1-T5 budget (1-2 wks). (o) Spec PR body cites the §3 score table (Gitea 4 / GitLab 3 / Bitbucket 1) verbatim. (p) Comment sweep `clean` per `feedback_comments_discipline` (every exported godoc one-line WHY-form, no superfluous WHAT). |

Implementer scorecard target: **A** floor on the first PR, **A+** stretch achievable with the GitLab appendix + comment sweep — both cheap once the GH+Gitea adapters land.

## 11. Adversarial review section

After draft, spawn reviewer subagent (`docs/engineer/dispatch-templates/reviewer.md`) targeting the five lenses in `feedback_adversarial_review`:

- **Edge cases** — Fork PRs, dependabot PRs, draft PRs, archived repos, soft-deleted issues. Confirmed addressed in §8 risk #10 (fork PRs) and §4.1 (`Mergeable *bool` nil for "checking"). Draft PRs: GH `gh pr view --json isDraft`; Gitea `pr.draft`. Adapter exposes `PR.IsDraft` (added below).
- **Simplification candidates** — (i) Collapse `MatchHeadCommit` into `GetPR + compare in caller`? **Rejected** per §4.1 explanation (W2-c2 hot path). (ii) Drop `SetLabels` and use a generic `PatchIssue(fields map[string]any)`? **Rejected** — leaky abstraction over typed backend APIs. (iii) Inline the registry into `cmd/regatta/serve.go`? **Rejected** — breaks adapter-contract spec parity and the test seam.
- **Deletion candidates** — (i) Drop the canary migration from this PR and ship interface + adapters only? **Considered** — but the canary is the only honest test that the seam compiles against a real caller. Keep. (ii) Drop the `regatta scm test` subcommand? **Considered** — but MVR-1-T2's step-7 smoke test depends on it. Keep.
- **Risk tiers** — High-tier risk #3 (golden-test regression on the W2-c2 hot path) calls for a sibling spec note in `2026-06-02-phase-autonomy-w2-c2-merge-execute.md` § "Caller migration risk" — file followup at PR merge.
- **OSS reuse the spec missed** — (i) `gitea-act` (gitea's runner) re-uses the SDK's auth flow; the adapter can borrow its retry wrapper rather than rolling one. Followup. (ii) `go-scm` (drone-io/go-scm) ships a similar multi-backend abstraction (Apache-2). **Studied** — covers more SCMs (Bitbucket, Stash, Gogs) but its interface is webhook-heavy and pre-dates context.Context everywhere. Rejected for adoption; cited as prior art only.

Findings folded inline: added `PR.IsDraft` to §4.3; added the W2-c2 cross-spec followup-issue line to §5.

Reviewer subagent verdict (post-draft): **A target met**; A+ stretch hinges on the GitLab appendix landing in a follow-up doc PR (followup tracked) and the comment sweep at impl time.

## 12. Followups (filed inline at PR merge)

See §5 follow-up issues list. Each carries:

- effort tier (XS / S / M / L)
- reopen-trigger (named persona inbound, gate, named PR)
- owner spec where applicable

The pre-filed followups satisfy `feedback_unaddressed_load_bearing` for every deferred surface called out in §2 and §8.

## 13. Comment sweep

`clean` (target). Per `feedback_comments_discipline`:

- Every exported godoc in `internal/scm/` is one line, WHY-form, starts with the symbol name.
- No superfluous WHAT comments.
- `golangci-lint run` after sweep per `feedback_comments_lint_reconcile`.

Verification at impl time: implementer scorecard rubric (§10 tier A+ row `p`) requires sweep-clean before merge.

## 14. Memory cites

- `feedback_research_design_principles` — proven OSS > build; second-consumer-proof for adapter contracts.
- `feedback_decision_priority` — UX (Gitea self-host unlock for persona-D) → performance (no GH regression) → best-practices (sql.Register-style adapter) → velocity (canary migration only in this PR).
- `feedback_adversarial_review` — §11 reviewer-subagent verdict folded.
- `feedback_grade_rubric` — §10 B/A/A+ tiers with falsifiable criteria.
- `feedback_unaddressed_load_bearing` — §5 + §12 pre-filed followups.
- `feedback_comments_discipline` + `feedback_comments_lint_reconcile` — §13 sweep gate.
- `feedback_pr_body_hygiene` — `--body-file` + `release-notes` fence at PR submit time.
- `feedback_no_signatures` — no AI footer.
- `feedback_review_proportional` — §5 caller migrations are one-per-PR, sized for proportional review.
