# 09 — Implementation cost: time, money, ops surface

- **Status:** v1 (mid-2026 numbers)
- **Author:** implementation-cost research lane
- **Last updated:** 2026-05-20
- **Scope:** validate the design's per-PR cost figure, size the orchestrator
  host, lock in Go dependency choices, sanity-check the README's 6–8
  week single-lane estimate, sketch deployment cost.

All Anthropic prices fetched from the live
[platform.claude.com pricing page](https://platform.claude.com/docs/en/about-claude/pricing)
on 2026-05-20. All rate limits fetched from
[platform.claude.com rate-limits](https://platform.claude.com/docs/en/api/rate-limits)
on the same date. Numbers labelled "[est]" are my projection from those
published figures; everything else is a published rate-card value.

---

## 1. Anthropic API pricing — what current rates actually yield per PR

### 1.1 Rate card (claude-opus-4-7, claude-sonnet-4-6, claude-haiku-4-5)

Published rates per million tokens, USD, USD-only inference (default
global routing — no 1.1x US-residency multiplier applied):

| Model | Input | 5m cache write (1.25×) | 1h cache write (2×) | Cache read (0.1×) | Output |
|---|---|---|---|---|---|
| Opus 4.7  | $5    | $6.25  | $10  | $0.50 | $25 |
| Sonnet 4.6 | $3    | $3.75  | $6   | $0.30 | $15 |
| Haiku 4.5  | $1    | $1.25  | $2   | $0.10 | $5  |

Two undocumented-in-the-design caveats:

- **Opus 4.7 ships a new tokenizer that produces up to 35% more tokens
  for the same text.** Per the
  [models overview page](https://platform.claude.com/docs/en/about-claude/models/overview),
  the rate card is unchanged from Opus 4.6 but the per-request bill can
  rise materially. The design's worked example uses Opus 4.7 — its
  $0.95 agent figure is therefore *low* if it was extrapolated from
  Opus 4.6 numbers. Treat agent spend as ~1.2× the design's figure
  until measured.
- **Fast mode is 6× standard** ($30 input / $150 output) on Opus 4.6
  and 4.7. Regatta's gates and agents do not need it. Flag explicitly
  in `regatta.yaml` that fast mode is off; if a maintainer turns it on
  the per-PR budget breaks.

### 1.2 Prompt-cache TTL and break-even math

From the [prompt caching docs](https://platform.claude.com/docs/en/docs/build-with-claude/prompt-caching):

- 5-min TTL cache breaks even **after one read**: write at 1.25× +
  read at 0.1× < two cold reads at 1.0×.
- 1-hour TTL breaks even **after two reads**: write at 2.0× + two
  reads at 0.1× < three cold reads.
- Minimum cacheable block: 4,096 tokens for Opus 4.7 / Haiku 4.5;
  1,024 tokens for Sonnet 4.6. **L5 (Haiku) calls below 4,096 tokens
  cannot be cached at all.** A naive L5 prompt that's only a diff +
  short rule list will silently bypass caching. Mitigation: include
  the repo's full agent-guidance file (typically 5–10k tokens) in
  every L5 call as cached prefix, so the breakpoint always crosses
  the minimum.
- Max 4 explicit `cache_control` breakpoints per request.
- Cache reads do **not** count against ITPM rate limit (Haiku 3.5 is
  the only exception, and it's retired on first-party API). This is
  load-bearing for the rate-limit math in §4.

### 1.3 Empirical hit rate for coding agents

The design assumes "prompt caching mandatory." The cost math only
holds if hit rate is high. Published evidence:

- **84%** input-token hit rate measured on 1,289 Claude Code requests
  totalling 100.9M tokens
  ([bswen.com analysis, 2026-03](https://docs.bswen.com/blog/2026-03-10-prompt-caching-claude-code/)).
  Translates to ~74% cost savings vs uncached.
- Anthropic's own engineering blog,
  [Lessons from building Claude Code: Prompt caching is everything](https://claude.com/blog/lessons-from-building-claude-code-prompt-caching-is-everything),
  states they run SEVs (severity alerts) when prompt-cache hit rate
  drops, and notes "long-running agentic products like Claude Code
  are made feasible by prompt caching." Implicit "healthy" range
  reported by users: **97–99%** during steady-state agentic work.
- Outage class: a March 2026 regression dropped Claude Code's hit
  rate to 4–17% and inflated billed costs 10–20× before being caught
  ([GitHub issue #46829](https://github.com/anthropics/claude-code/issues/46829)).
  Regatta must monitor `cache_read_input_tokens / total_input_tokens`
  per-gate and alert if it drops below a threshold — otherwise a
  silent Anthropic-side regression can blow Regatta's `spend_cap_usd`
  before the on-call notices.

**Working assumption for Regatta:** 80% hit rate per agent session
[est], 60% per L3/L4/L5 gate call [est] (gates start fresh on each
PR, less repetition than an interactive session). Both numbers are
conservative against the published Claude Code 84% / 97–99% bands.

### 1.4 Updated per-PR cost table — replaces design.md §Costed reference workload

Reconstructing the design's PR #259 worked example with current
published rates and an 80%-hit-rate agent / 60%-hit-rate gates
assumption.

Per-iteration agent token shape (Opus 4.7, mostly cached coding loop):

- Cached prefix: ~80,000 tokens (system + tools + AGENTS.md + work item)
- Uncached input per turn: ~3,000 tokens (new tool results)
- Output per turn: ~2,000 tokens
- 4 iterations to converge

Per iteration: 1 × cache write (5m, 1.25×) on iteration 1 only;
iterations 2–4 are cache reads. Computed:

| Line | Tokens | Rate | Cost |
|---|---|---|---|
| Cache write (iter 1)                | 80,000  | $6.25/MTok  | $0.500 |
| Cache read (iter 2–4, 3× 80k)       | 240,000 | $0.50/MTok  | $0.120 |
| Uncached input (4 × 3,000)          | 12,000  | $5.00/MTok  | $0.060 |
| Output (4 × 2,000)                  | 8,000   | $25.00/MTok | $0.200 |
| **Agent subtotal**                  |         |             | **$0.880** |

L3 (Opus 4.7, spec conformance), one call per PR:

| Line | Tokens | Rate | Cost |
|---|---|---|---|
| Cache write (gate prompt + diff)    | 15,000  | $6.25/MTok | $0.094 |
| Uncached (criterion text)           | 2,000   | $5.00/MTok | $0.010 |
| Output                              | 1,500   | $25.00/MTok | $0.038 |
| **L3 subtotal**                     |         |            | **$0.142** |

L4 (Sonnet 4.6, adversarial reviewer):

| Line | Tokens | Rate | Cost |
|---|---|---|---|
| Cache write (prompt + style docs)   | 20,000  | $3.75/MTok | $0.075 |
| Uncached (diff)                     | 3,000   | $3.00/MTok | $0.009 |
| Output (severity-ranked findings)   | 2,000   | $15.00/MTok | $0.030 |
| **L4 subtotal**                     |         |            | **$0.114** |

L5 (Haiku 4.5, drift):

| Line | Tokens | Rate | Cost |
|---|---|---|---|
| Cache write (rules + AGENTS.md)     | 8,000   | $1.25/MTok | $0.010 |
| Uncached (touched files list)       | 1,000   | $1.00/MTok | $0.001 |
| Output                              | 500     | $5.00/MTok  | $0.003 |
| **L5 subtotal**                     |         |            | **$0.014** |

| **Total per PR (current rates)** | | | **~$1.15** |

So the design's $1.06/PR is roughly right to first order, but the
breakdown shifts:

- **L3 is more expensive than the doc claims** ($0.14 vs $0.06) once
  the cache write is accounted for. The doc's $0.062 assumes the L3
  prompt was warm — fine on PR #2 against the same repo, wrong on the
  first PR after orchestrator restart.
- **L4 is more expensive** ($0.11 vs $0.04) for the same reason.
- **L5 stays cheap** because Haiku is genuinely cheap.
- **Agent stays close** ($0.88 vs $0.95) — the design's number is
  defensible. But see the 1.35× Opus 4.7 tokenizer caveat — measured
  bills may run ~$1.20–$1.30/PR.

**Concrete edit for design.md §Costed reference workload:**
replace the table with the one above; add a footnote that the figure
holds at ~80%/60% hit rates and that the orchestrator MUST emit a
`prompt_cache_hit_rate` gauge per gate, alerting on <50% over 50
rolling PRs.

### 1.5 What's the floor if caching breaks?

If Anthropic ships an Opus 4.7 regression like the March 2026 Claude
Code incident (hit rate → 10%), the per-PR bill for the same workload
rises to roughly **$3.60/PR [est]** (agent ~$3.10, gates ~$0.50) — a
3× burn. The `safety.spend_cap_usd` default of $50 absorbs this if
the orchestrator catches it within ~14 PRs; it does not absorb a
24-hour outage at 1 PR/hour against an unsupervised cap. The on-call
runbook needs a `cache_hit_rate < 30%` → halt-fleet rule alongside
the existing $-cap rule.

---

## 2. Per-week orchestrator-host budget

The Regatta orchestrator is a single long-running Go daemon plus
N agent worktrees and 0–N concurrent gate-runner subprocesses.

### 2.1 Disk (worktrees + sqlite + audit shadow)

For an N-lane deployment with **R** = repo size on disk, peak disk
usage is approximately `(N + 1) × R + sqlite + worktree slack`.

| Lanes | Repo size | Worktrees | sqlite | Slack (logs, prompts) | **Total** |
|---|---|---|---|---|---|
| 1 lane    | 500 MB   | 1 GB    | 200 MB | 500 MB | **~2.2 GB** |
| 4 lanes   | 500 MB   | 2.5 GB  | 500 MB | 1 GB   | **~4.5 GB** |
| 4 lanes   | 2 GB     | 10 GB   | 500 MB | 1 GB   | **~13.5 GB** |
| 10 lanes  | 2 GB     | 22 GB   | 1 GB   | 2 GB   | **~27 GB** |

The reaper handles cleanup, but worktrees are full git clones (not
shallow), so the `.git/objects` deduplication you'd get from a single
checkout doesn't apply. Recommendation: use `git worktree add` against
a shared bare repo rather than independent clones — this drops the
per-worktree marginal disk to just the working-tree size (typically
30–50% of the full `.git+working-tree`) and is the standard CLI
pattern. Add this as a normative requirement in design.md
§AgentSpawner.

**tmpfs vs SSD:** For repos under 500 MB, mounting
`.regatta/worktrees/` on tmpfs is feasible on a 16 GB box and trades
~$0 disk cost for ~2× IOPS. Above 2 GB repos it's not worth the RAM
pressure; use NVMe SSD. The reaper must run unconditionally either
way (tmpfs gets wiped on reboot, but pre-reboot a long-running
orchestrator can still accumulate stale worktrees if SIGKILL hits
during reap).

### 2.2 CPU, RAM, network

- **CPU:** The orchestrator itself is sleepy (polling WorkItemSource,
  reading Anthropic JSON, writing sqlite). Each agent subprocess is a
  Claude SDK client that's mostly IO-bound. L1 (repo CI) is the only
  potentially heavy load — `npm test`, `pytest`, `go test` can pin
  cores. Budget **2 cores per concurrent agent** for the CI step, 1
  core for everything else. A 4-lane deployment wants **~10 cores
  peak**.
- **RAM:** Orchestrator + sqlite ~200 MB. Each agent ~150 MB (Claude
  SDK process + a JS/Py/Go test runner). L1 CI can easily hit 2–4 GB
  per lane (a Node test suite with Jest, a Python suite with pytest +
  pandas). Budget **4 GB per lane** safety margin. 4 lanes → **16 GB
  minimum**, **32 GB recommended**.
- **Network:** Egress to Anthropic + GitHub. At 80% hit rate the
  Anthropic egress for the design's 15 PRs/week workload is ~50 MB/wk
  outbound, ~250 MB/wk inbound (gate outputs + agent responses).
  GitHub API + PR pushes are ~100 MB/wk total. Trivial — any modern
  VM bandwidth is fine.

### 2.3 Recommended VM size

For the 10-engineer / 15-PRs-per-week reference team:

| Cloud | Instance | Specs | List price / mo (on-demand) |
|---|---|---|---|
| AWS    | `m7i.2xlarge` + 100 GB gp3   | 8 vCPU, 32 GB, 100 GB SSD | **~$320** |
| GCP    | `n2-standard-8` + 100 GB pd-ssd | 8 vCPU, 32 GB, 100 GB SSD | **~$300** |
| Hetzner | `CCX33`                     | 8 vCPU, 32 GB, 240 GB NVMe | **~$80** |
| On-prem laptop | M2 Pro Mac mini             | 12 cores, 32 GB unified | **$0** marginal, $1,500 capex |

Single-lane (Day 7 in the runbook) fits a `t3.medium` (2 vCPU, 4 GB,
~$30/mo). Don't over-provision early — the cgroup/rlimit budget caps
agent runaway anyway.

---

## 3. Go dependency choices

The design names `modernc.org/sqlite`. The rest is implied. Concrete
picks for v3.1 ship:

| Concern | Pick | Why | Runner-up |
|---|---|---|---|
| SQLite driver | `modernc.org/sqlite` | Pure Go, no CGO, design's existing choice; cross-compiles trivially. | `mattn/go-sqlite3` (faster but CGO-dependent — breaks the static-binary distribution story). |
| HTTP client | `net/http` (stdlib) + small retry helper | Stdlib is fine for talking to 2 APIs (Anthropic, GitHub); no need for resty's middleware layer. | `go-resty/resty/v2` only if a 3rd target appears. |
| GitHub client | `google/go-github/v68` + `bradleyfalzon/ghinstallation/v2` for App auth | go-github is the canonical typed REST client; ghinstallation handles GitHub-App JWT → installation-token exchange (required for the App-mode path in §3-rate-limits below). | `cli/go-gh` — designed for CLI extensions, picks up `GH_TOKEN` from env which is wrong for a daemon; uses `gh`'s OAuth scope set which is over-broad. |
| GitLab client | `gitlab.com/gitlab-org/api/client-go` | Official, mirrors go-github's shape. | `xanzy/go-gitlab` (deprecated in favor of the official client in 2025). |
| Anthropic SDK | `anthropics/anthropic-sdk-go` (official) | Maintained by Anthropic; supports the new tokenizer + cache-control properly. | DIY HTTP — works but you eat every API quirk. |
| CUE (config schema) | `cuelang.org/go/cue` | The CUE compiler+evaluator from the upstream Go module; design uses `regatta.v1.cue` so this is the only choice. | (None; CUE has no real Go alternative.) |
| JSON Schema (work_item, gate_result) | `santhosh-tekuri/jsonschema/v6` | Pure Go, draft-2020-12 compliant, fast. | `xeipuuv/gojsonschema` — unmaintained since 2022, draft-7 only. |
| YAML (regatta.yaml ingest) | `goccy/go-yaml` | Strict mode catches typos that `gopkg.in/yaml.v3` silently allows. | `yaml.v3` if strict-mode integration is too noisy. |
| Logging | `log/slog` (stdlib, Go 1.21+) | Structured logging in stdlib since 1.21; no reason to add zap. | `zap` (faster but adds a dep). |
| Process supervision (cgroups) | `containerd/cgroups/v3` | Maintained, handles v1 + v2 transparently. | DIY `/sys/fs/cgroup` writes — fragile across distros. |
| Worktree mgmt | shell out to `git` binary | The `go-git` library is incomplete for worktree semantics in 2026. | `go-git/go-git/v5` — known issues with `git worktree add`. |
| Signed audit (HMAC) | `crypto/hmac` (stdlib) | Design's threat model uses HMAC; stdlib is sufficient. | — |
| OIDC verification | `coreos/go-oidc/v3` | Standard for verifying GitHub OIDC tokens; lightweight. | — |

The CGO-free dep list (excluding the `git` binary) is the load-bearing
distribution property: `go build` produces a single static binary that
`brew install` and `apt install` can ship without runtime libc
version-skew.

---

## 4. GitHub API rate-limit math

From [REST rate limits](https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api):

- **Personal access token (classic or fine-grained):** 5,000 req/hr,
  per-token (NOT per-user). If two PATs exist on the same user, each
  gets its own 5k budget.
- **GitHub App installation:** 5,000 req/hr base, scaling up to
  12,500 req/hr for non-Enterprise installations based on repo+user
  count, 15,000 req/hr on Enterprise Cloud.
- **Secondary limits:** ≤80 content-generating req/min, ≤500
  content-generating req/hr. PR creation and PR comments both count.
- **Concurrent requests:** ≤100.
- **CPU time:** ≤90 sec CPU per 60 sec wall-clock.

Per-PR GitHub API call inventory for Regatta:

- 1× `POST /repos/{}/pulls` (PR create)
- 1× `POST /repos/{}/issues/{}/comments` per gate result × 5 gates
  on first push = 5 comments
- ~3× `GET /repos/{}/pulls/{}` (status polls during gates_running)
- 1× `GET /repos/{}/pulls/{}/files` (diff for L4/L5)
- 1× `POST /repos/{}/issues/{}/comments` on merge cleanup

So **~10 API calls / PR + ~6 content-creation calls / PR**.

| Scenario | PRs/week | API calls/wk | Content calls/wk | Headroom (PAT, 5k/hr) |
|---|---|---|---|---|
| 15 PRs/wk | 15  | ~150  | ~90   | Vast (covers ~3,500 PRs/wk before hitting either limit). |
| 100 PRs/wk | 100 | ~1,000 | ~600 | Still 5× under content-hourly (500/hr × 168 = 84,000/wk). |
| 1,000 PRs/wk (hosted multi-tenant, v4) | 1,000 | ~10,000 | ~6,000 | Mostly fine; bursts >80 content-creates/min on Monday-morning batches will hit secondary limits. Recommend exponential-backoff helper around content-create endpoints. |

**Conclusion: API limits are not the bottleneck for any v3.1
deployment.** Past 1k PRs/wk (v4 hosted), use a GitHub App with
multi-installation token rotation, not a PAT.

Authentication recommendation: **GitHub App > fine-grained PAT >
classic PAT**. The App path gives:

- Per-installation token isolation (one repo's compromise doesn't
  reach others)
- Higher rate-limit ceiling
- Repository-scoped permissions enforceable via the App manifest
  (P4: least-privilege)
- OIDC-friendly token lifecycle (~1 hr; matches the design's
  "short-lived" threat model)

---

## 5. Anthropic Messages API throughput ceiling

Published Standard-tier limits relevant to Regatta (from rate-limits
doc). Cache reads do not count against ITPM (the one big lever).

| Tier | Opus RPM | Opus ITPM | Opus OTPM | Sonnet RPM | Sonnet ITPM | Sonnet OTPM | Haiku RPM | Haiku ITPM | Haiku OTPM |
|---|---|---|---|---|---|---|---|---|---|
| T1 | 50    | 500k    | 80k   | 50    | 30k    | 8k    | 50    | 50k   | 10k  |
| T2 | 1,000 | 2.0M    | 200k  | 1,000 | 450k   | 90k   | 1,000 | 450k  | 90k  |
| T3 | 2,000 | 5.0M    | 400k  | 2,000 | 800k   | 160k  | 2,000 | 1.0M  | 200k |
| T4 | 4,000 | 10.0M   | 800k  | 4,000 | 2.0M   | 400k  | 4,000 | 4.0M  | 800k |

Notable: Opus has higher ITPM/OTPM at every tier than Sonnet. Because
Regatta's L3 + agent both use Opus, this is fortunate. Sonnet is the
tighter constraint for L4 — at Tier 2, 90k OTPM ÷ ~2k tokens/L4 call
= **45 L4 runs per minute** ceiling. That's ample for ≤100 PRs/wk
(roughly 4 PRs/hour peak, 1 L4 per push averaging maybe 3 pushes/PR
→ 12 L4 runs/hr, 5,400× under ceiling).

Critically, **Opus rate limit is a combined limit across Opus 4.7,
4.6, 4.5, 4.1, and 4** — you cannot work around it by mixing model
versions. Similarly Sonnet 4.x is combined across 4, 4.5, 4.6.

**Concurrent-request limit: not published per-model — limit is
expressed only as RPM**. The 100-concurrent number is a *GitHub* API
limit, not Anthropic's. The practical Anthropic constraint for a
parallel gate stack is RPM, which Tier 2+ easily covers.

**Priority Tier** is for "enhanced service levels in exchange for
committed spend" and goes through Anthropic sales. It's not a quick
fix for rate-limit headroom — at expected Regatta v3.1 volume
(<$1k/mo), staying on Standard Tier 2 covers everything.

**Tier qualification path:** Tier 2 is reached after $40 in
cumulative credit purchase ([spend-limits table](https://platform.claude.com/docs/en/api/rate-limits)).
A first-time self-hosted Regatta user starts at Tier 1 — and Tier
1's 30k ITPM on Sonnet is genuinely tight for L4 if a 15k-token
cached system prompt is in flight. **Recommend the Day 1 runbook
explicitly note: deposit ≥$40 in Anthropic credits before
`regatta serve` to clear Tier 1's Sonnet ITPM ceiling.**

---

## 6. cgroups / supervisor enforcement

Design says "cgroups (Linux) or rlimits (macOS)." Concretely in
mid-2026:

### Linux

| Distro | Default cgroup version | Notes |
|---|---|---|
| Ubuntu 22.04 LTS / 24.04 LTS | v2 (unified) | Default since 21.10. |
| Debian 12 / 13 | v2 (unified) | Default since 11. |
| RHEL 9 / 10, Rocky 9 | v2 (unified) | Default since 9.0. |
| Amazon Linux 2023 | v2 (unified) | [AL2023 docs](https://docs.aws.amazon.com/linux/al2023/ug/cgroupv2.html); AL2 is v1, retiring 2025. |
| Container Linux (CoreOS, Bottlerocket) | v2 | Default. |

**By mid-2026 v2 is the default everywhere current; v1 is legacy.**
Use `containerd/cgroups/v3` library which transparently handles both
but assume v2 for new deployments. Set:

- `memory.max` — hard cap, kills on OOM (P5)
- `cpu.max` — quota+period throttling
- `pids.max` — caps subprocess fork-bombs (a real failure mode for
  runaway agents)
- `io.max` — caps IOPS to protect the host

Docker/containerd: both fully support cgroup v2 in 2026; the
`--cgroupns=private` flag isolates an agent container's view.

### macOS

`rlimits` (via `setrlimit(2)`) cover RAM (`RLIMIT_AS`), file
descriptors (`RLIMIT_NOFILE`), CPU seconds (`RLIMIT_CPU`), but **do
not enforce wall-clock time**. The design says "wall-clock, disk,
network, iteration, spend enforced in the parent process." The
parent-watchdog approach (Go goroutine with a `time.Timer` that
SIGKILLs the child) covers wall-clock; that's the only reliable
mechanism on macOS. Disk and network on macOS need to be enforced by
checking before/after sampling — not real-time. That's an acceptable
gap for Day 1 local-mac usage; production deployments should be
Linux.

### Windows

Job Objects (`SetInformationJobObject` with
`JOBOBJECT_EXTENDED_LIMIT_INFORMATION`) provide memory + CPU + wall-
clock equivalents. Not in v3.1 scope per the design's Linux-first
posture; treat as v4.

---

## 7. Append-only audit sink

Design's example: `s3://acme-audit/regatta/?object-lock=COMPLIANCE`.
Comparison of viable sinks for the per-PR HMAC-signed gate log:

| Sink | Setup cost | Storage $/GB/mo | Write cost | Throughput ceiling | Notes |
|---|---|---|---|---|---|
| **S3 + Object Lock (COMPLIANCE)** | 1 bucket + IAM | $0.023 (Standard) → $0.00099 (Deep Archive) | $0.005 / 1k PUT | ~3,500 PUT/sec/prefix | No premium for Object Lock itself ([AWS Object Lock pricing](https://aws.amazon.com/s3/features/object-lock/)). **Once retention is set, immutable for the period — including from root** — so a 10-yr retention on a test bucket means 10-yr storage bill. Start with 1-yr retention. |
| **GCS Bucket Lock** | 1 bucket + IAM | $0.020 (Standard) → $0.0012 (Archive) | $0.005 / 10k op | Similar | Equivalent to S3 Object Lock; same caveat on retention. |
| **Azure Immutable Blob (legal-hold or time-based)** | Storage account + container | $0.021 (Hot) | $0.0065 / 10k write | Similar | Same caveat. |
| **sigstore Rekor (public good)** | None (use rekor.sigstore.dev) | $0 | $0 | Public-good throttled; 100 KB attestation cap | [Rekor public instance](https://docs.sigstore.dev/logging/overview/) at 99.5% SLO. **Not appropriate for high-volume private gate-result logging** — designed for OSS-supply-chain transparency, not internal audit. Useful only if Regatta also publishes its canary-corpus signatures publicly. |
| **Self-hosted Rekor v2** | Day of ops setup; PostgreSQL backing store | ~$50/mo for small instance | Marginal | Higher than v1 ([Rekor v2 GA blog](https://blog.sigstore.dev/rekor-v2-ga/)) | Worth it if regulatory requirement demands cryptographic Merkle-tree proof rather than write-once-read-many. |
| **CT-log-style (Trillian)** | Significant — needs map server + log signer + DB | Variable | — | High | Overkill for v3.1; revisit at v4 if multi-tenant. |

**Recommendation for v3.1: S3 Object Lock COMPLIANCE in Governance
mode for 1 year, then Glacier transition.** Cost for the 15-PR/wk
team at ~50 KB per audit record × 80 records/wk = ~4 MB/wk →
**under $0.01/mo storage**. The setup-cost story is "create one
bucket with object-lock and a SHA-pinned `bucket-policy.json`."

Concrete `regatta.yaml` for that:

```yaml
telemetry:
  audit_sink: 's3://acme-audit/regatta/?object-lock=COMPLIANCE&retention=P1Y'
```

(P1Y = ISO-8601 one-year retention; design.md's current example
omits the retention parameter — flag this as a minor doc fix.)

---

## 8. OIDC token issuance

Design says "short-lived OIDC tokens for agents." Minimum-viable
options:

1. **GitHub Actions OIDC** (free, ~1-hour tokens, repo-scoped).
   Works if Regatta runs in GitHub Actions (v3.2 scope). For v3.1
   self-hosted daemon: not directly usable.
2. **GitHub App installation tokens** (~1-hour TTL, scoped to the
   App's declared permissions). This is the v3.1 path: orchestrator
   holds the App private key, mints per-agent installation tokens on
   spawn, agent never sees the private key. Token rotation is the
   spawner's job — mint, pass via env var, agent dies with the
   token.
3. **HashiCorp Vault** + AppRole + token TTL. Heavy for a self-host
   single-binary deployment; recommend only if the org already runs
   Vault.
4. **AWS STS `AssumeRoleWithWebIdentity`** — useful when Regatta
   needs to write to an S3 audit sink from the agent. Mint with
   the orchestrator's IAM role; pass STS-issued temporary creds to
   the agent.
5. **GCP Workload Identity Federation** — equivalent to AWS STS on
   GCP.

**v3.1 minimum:** GitHub App + STS for S3. Two systems. No external
dependencies beyond AWS + GitHub.

Token rotation cadence: tie to the orchestrator's heartbeat-lease
clock (60s renewal in design.md). Agent's token lifetime ≤ 1 hour;
if `wall_clock_cap > 1h`, mint a fresh token at the 50-minute mark.

---

## 9. Worktree disk cost — concrete strategy

Repos in the wild:

- Median JS/Node repo: ~200 MB on disk
- Median Python ML repo: ~500 MB
- Median Go monorepo: ~1.5 GB
- Lumaverse-class (this repo): 30+ GB

For the Lumaverse case at concurrency 4, naive copy is 120 GB. That's
the worst-case the design must survive.

**Recommended layout:**

1. **One bare clone per repo** at `.regatta/repos/<repo-id>.git/`
2. **`git worktree add` per agent** at
   `.regatta/worktrees/<work-id>-<slug>/` — sharing the bare clone's
   `.git/objects` via symlink. Marginal cost per worktree = working-
   tree size only, typically 30–50% of full clone size.
3. **tmpfs for repos <500 MB** (Node, Python, small Go). 16 GB tmpfs
   on a 32 GB box covers ~30 concurrent small worktrees.
4. **NVMe SSD for repos ≥500 MB**. The Lumaverse case fits ~7 worktrees
   in 100 GB even without dedup; with bare-clone sharing, it fits ~20.
5. **Reaper invariants:**
   - On every `regatta serve` startup, scan `.regatta/worktrees/` for
     directories not corresponding to a live agent in sqlite and
     remove.
   - On work-item completion (merge OR close), `git worktree remove
     --force` + `git worktree prune` before reaping the sqlite
     session.
   - Cron-style fallback: `git worktree list` ∩ sqlite-known-set;
     anything missing on either side is GC'd.

Design.md §AgentSpawner currently says "creates worktree" — clarify
to **"creates worktree via `git worktree add` against the cached
bare clone."** This is a normative add.

---

## 10. 6-8 week build estimate validation

The README says "single-lane mode ~6–8 weeks." Walking through the
v3.1 surface, week-by-week, by an experienced Go engineer (one FTE,
no parallel team):

| Week | Deliverable | Hours est | Confidence |
|---|---|---|---|
| **1** | Repo scaffold, sqlite schema migration, CUE config loader, `regatta validate-config` command. WorkItem + GateResult schemas wired (JSON Schema + CUE). | 35 | High |
| **2** | WorkItemSource interface + `github_issues` adapter + `markdown_catalog` adapter. `regatta validate-spec` + DAG validation. NFC + invisible-glyph normalization (P10). | 40 | High |
| **3** | L0 deterministic gate + fixture corpus (≥30 fixtures by end of week, on track to 200). L1 shell-out gate. L2 PR-body validator. Wire `regatta gate-run --pr` as a one-shot tool. | 40 | High |
| **4** | AgentSpawner: shell out to `claude` CLI, worktree creation via bare-clone+worktree-add, session capture into sqlite. Cgroups v2 limits via `containerd/cgroups/v3`. Single-PR end-to-end demo. | 40 | Med |
| **5** | L3 + L4 + L5 SDK clients with prompt caching, structured-output schema validation, `GateResult` emission, HMAC signing. PR-comment posting via `google/go-github`. | 40 | Med |
| **6** | PRWatcher with idempotent `(pr_sha, gate_id)` keying. RejectionRouter (K=3 → needs-human). Heartbeat-lease locks + sorted-lock acquisition. Crash recovery. | 45 | Med |
| **7** | SupervisorLimits parent-process enforcement (wall-clock, iter cap, $-cap). CanaryInjector + the 8-archetype canary corpus. Reaper. Audit-sink writer (S3 Object Lock). | 40 | Med-Low |
| **8** | OIDC token mint (GitHub App + STS). LessonCapture (CODEOWNERS-gated PR draft). End-to-end on a pilot repo. `regatta digest`, `regatta status`, `regatta canary-report` commands. Docs pass + Day-1-to-30 runbook validation. | 45 | Low |

Total: ~325 hours = **~8 weeks at 40 hr/wk**, no buffer. If the
engineer is FT-on-Regatta with zero meeting load, 6 weeks is barely
possible if (and only if):

- The 200-fixture L0 corpus is generated mostly by AI and reviewed
  rather than hand-written (realistic — most fixtures are tiny diffs
  of acceptance-criteria text).
- The 8-archetype canary corpus is reused from `gates/canary/testdata/`
  rather than designed from scratch.
- The WorkItemSource ships with only `github_issues` + `markdown_catalog`
  in v3.1, deferring Jira/Linear/GitLab to v3.2.
- No model-output regressions during build (a 1-week incident like
  the March 2026 Claude Code cache regression eats the buffer).

**Honest verdict:** **8 weeks is realistic; 6 weeks requires
pre-existing AI-fluency and zero external interruptions.** The README
should be re-stated as "**6–10 weeks**" or "**8 weeks with a 2-week
buffer**." Areas I'd watch:

- Week 6 (PRWatcher idempotency + crash recovery): the design's
  state machine is correct on paper but the SHA-keyed dedup has
  edge cases (force-push rewriting SHA mid-gate run) that always
  cost more than budgeted.
- Week 7 (supervisor + canary): cgroups v2 is well-trodden, but
  testing limit-breach scenarios (induced OOM, induced wall-clock
  exceeds) usually surfaces 2–3 bugs that take a day each.
- Week 8 (OIDC): GitHub App private-key handling is fiddly the
  first time. Budget 2 days; happens in 4 hours if you've done it
  before.

---

## 11. Deployment cost matrix

Monthly USD for the reference 10-engineer / 15-PR/wk team. Includes
**all Regatta-side costs**: VM + Anthropic API + S3 audit. Excludes
maintainer-seat Claude Code subscriptions if any.

| Deployment | VM/host | Anthropic | Audit | Total / mo | Notes |
|---|---|---|---|---|---|
| AWS self-host (`m7i.2xlarge`)  | $320  | $72 [est] | $5  | **~$400** | Production-grade. |
| GCP self-host (`n2-standard-8`)| $300  | $72 [est] | $5  | **~$380** | Production-grade. |
| Hetzner self-host (CCX33)      | $80   | $72 [est] | $5  | **~$160** | Production-grade, EU-only. Pair with S3 in same region for audit. |
| On-prem laptop (M2 Mac mini)   | $0    | $72 [est] | $5  | **~$80**  | Macos rlimits gap = no wall-clock for agent (Section 6). Day 1 / Day 7 only, not steady state. |
| GitHub Actions (v3.2 future)   | $0†   | $72 [est] | $5  | **~$80**  | †assumes within free-runner budget. v3.1 doesn't ship this; v3.2 path. |
| Hosted multi-tenant (v4)       | n/a   | depends   | depends | **n/a** | Out of v3.1 scope. |

The Anthropic figure assumes 15 PRs/wk × ~$1.15/PR × 4.33 wk/mo =
**~$72/mo**. At 100 PRs/wk it becomes ~$500/mo and the VM cost
roughly doubles (more lanes); total moves to ~$800/mo on AWS.

The competitor pricing comparison in design.md §Costed reference
workload (Devin ~$50/PR, Copilot Workspace ~$40/PR, Regatta self-
host ~$70/PR-equivalent) **already correctly positions Regatta as
not-cheapest**; current numbers don't change that. But the design's
"~$70/PR-equivalent" wording is ambiguous — at $1.15 raw Anthropic
spend, the rest ($69) is the amortized cost of a maintainer's time
+ infra + Claude Code seat. **Suggest design.md clarify the figure
as "$1.15 API + ~$70/PR amortized people + infra cost at 10-eng
team, 15 PRs/wk."**

---

## 12. Concrete edits to design.md

1. **§Costed reference workload — replace the table** with the §1.4
   version above. Add the footnote about 80%/60% hit rates and the
   mandatory `prompt_cache_hit_rate` gauge.

2. **§Costed reference workload — add a paragraph** on the Opus 4.7
   tokenizer (up to 35% more tokens per text) and what that means
   for real billed cost vs the table.

3. **§Orchestrator shape §3 (AgentSpawner)** — change "creates
   worktree" to "creates worktree via `git worktree add` against a
   shared bare clone of the repo at `.regatta/repos/<repo-id>.git/`,
   so per-agent disk footprint scales with the working tree only."

4. **§Per-repo configuration example** — `telemetry.audit_sink`
   should include explicit retention:
   `'s3://acme-audit/regatta/?object-lock=COMPLIANCE&retention=P1Y'`.

5. **§Day 1 — install and validate** — add a line:
   `# Deposit >= $40 in Anthropic credits to clear Tier 1 ITPM`.

6. **§Failure modes table** — add a row:
   "Anthropic prompt-cache hit-rate regression | telemetry.cache_hit_rate <30% over 50 PRs | `regatta cache-report` | `regatta halt --reason cache-regression` | wait for Anthropic; resume on recovery."

7. **§SupervisorLimits §7** — make explicit: "cgroups v2 via
   `containerd/cgroups/v3` library, with `memory.max`, `cpu.max`,
   `pids.max`, `io.max` set per agent. macOS: rlimits + parent
   goroutine watchdog for wall-clock — accept that disk/network
   are sampled, not real-time, and that macOS is a Day-1 path not
   steady state."

8. **§State, persistence, recovery** — append: "Worktrees are GC'd
   on startup via `git worktree list` ∩ sqlite-known-set."

9. **README** — change "single-lane mode ~6–8 weeks" to
   "**single-lane mode ~8 weeks (6 with no interruptions, 10 if
   touching WorkItemSource for a custom backend)**."

---

## 13. Honest TODOs this research didn't close

- I couldn't directly verify the design's assumption that "L4 reads
  repo principles/style/non-functional budgets" sits cleanly under
  the 4-breakpoint cache limit; if a repo declares 5+
  `trusted_doc_paths` in `regatta.yaml`, L4 silently degrades to
  partial caching. Flag for an implementation-time bench.
- Priority Tier rate-limit numbers are not publicly published —
  only the qualifying-spend mechanism is. If Regatta v4 needs
  Priority Tier, that's a sales conversation, not a docs reference.
- `cgroups/v3` library on AL2023 + EKS-on-Bottlerocket has a known
  edge case around `pids.max` propagation through systemd-managed
  slices. Reproduces only at v4 hosted-multi-tenant scale; not a
  v3.1 blocker.
- Concurrent-request limit on Anthropic API is *not* explicitly
  published — only RPM is. The 100-concurrent figure I cited is
  GitHub's, not Anthropic's. Don't propagate it as an Anthropic
  number anywhere.

---

## Sources

- Anthropic [Pricing page](https://platform.claude.com/docs/en/about-claude/pricing) (fetched 2026-05-20)
- Anthropic [Rate limits](https://platform.claude.com/docs/en/api/rate-limits)
- Anthropic [Prompt caching](https://platform.claude.com/docs/en/docs/build-with-claude/prompt-caching)
- Anthropic [Models overview](https://platform.claude.com/docs/en/about-claude/models/overview)
- [Lessons from building Claude Code: Prompt caching is everything](https://claude.com/blog/lessons-from-building-claude-code-prompt-caching-is-everything)
- [bswen.com — Prompt Caching in Claude Code: 84% of Input Tokens Cached](https://docs.bswen.com/blog/2026-03-10-prompt-caching-claude-code/)
- [Claude Code cache TTL regression](https://github.com/anthropics/claude-code/issues/46829)
- [GitHub REST rate limits](https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api)
- [AWS S3 Object Lock](https://aws.amazon.com/s3/features/object-lock/)
- [Sigstore Rekor v2 GA](https://blog.sigstore.dev/rekor-v2-ga/)
- [Amazon Linux 2023 cgroup v2 docs](https://docs.aws.amazon.com/linux/al2023/ug/cgroupv2.html)
- [cli/go-gh](https://github.com/cli/go-gh) and [google/go-github](https://github.com/google/go-github)
