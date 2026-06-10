---
title: "MVR-2 T4 — P3.8 LLM-gateway adapter (LiteLLM | portkey)"
status: active
summary: "Pre-fetch skeleton for MVR-2 T4. Replaces direct Anthropic shell-out (`claude` CLI / direct API call) with a P3.8-style LLM-gateway adapter. Two candidate libraries: LiteLLM (Python proxy, OpenAI-shaped) vs Portkey (Go-friendly, observability-first). Score at dispatch time. Required by persona-B/C who want OpenAI/Gemini/Azure-OpenAI without rebuilding regatta. M (2-3 wk) effort. SKELETON."
---

# MVR-2 T4 — P3.8 LLM-gateway adapter — skeleton spec

_Pre-fetch skeleton, 2026-06-03. Material elaboration deferred to MVR-2 dispatch. Source-of-truth: `docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md` §4 MVR-2-T4. Prior adapter pattern: `docs/engineer/specs/2026-06-01-adapter-contracts-design.md` §6 (sql.Register-style). Companion: MVR-1-T5 SCM adapter (`docs/engineer/specs/phase-x/2026-06-02-mvr-1-t3-p38-scm-adapter-gitea-first.md`) — same contract shape, different domain._

## 1. Scope

### 1.1 In scope

`internal/llm/iface.go` — `LLMAdapter` interface with the minimum surface regatta calls today:

| Method | Used today by | Notes |
|---|---|---|
| `Complete(ctx, msgs, opts) (Response, error)` | L4 reviewer, plan-author, comment-author | Streaming optional in v1 |
| `Stream(ctx, msgs, opts) (<-chan Chunk, error)` | (future) live operator chat | Defer; add iface placeholder + `ErrNotImplemented` returner for v1 |
| `CountTokens(ctx, msgs) (int, error)` | Cost estimator before spawn | Critical for cost-cap gate |
| `Models(ctx) ([]ModelInfo, error)` | Operator `regatta llm models` | Lists what's available + pricing |

Two concrete adapters at land:

1. `internal/llm/anthropic/` — wraps current direct Anthropic SDK call. Byte-equal output on the 6 existing call-sites (golden tests carried over). NO behavior change.
2. `internal/llm/gateway/` — wraps LiteLLM proxy (HTTP) OR Portkey (Go SDK). **Pick at dispatch time** based on the score-card in §6.

`regatta.yaml` schema gains `llm:` block:

```yaml
llm:
  kind: anthropic | gateway
  endpoint: <url for gateway kind>
  default_model: <e.g., claude-sonnet-4-7-20260403>
  fallbacks: [<alternate models on rate-limit / outage>]
```

`regatta llm test` subcommand: opens + closes a 100-token completion against the configured backend to verify auth + endpoint + cost budget. Wired into `regatta init` step-7 smoke test.

### 1.2 Out of scope

- Streaming completion surface — defer to phase X
- Custom rate-limit-bucket per-tenant — followup wedge (cost-cap already does per-tenant spend caps)
- Local model adapter (Ollama, llama.cpp) — followup; gateway should already cover these via LiteLLM
- Prompt-versioning / A/B testing — Wave-3 wedge per roadmap §5.7
- Adapter for Anthropic's `claude` CLI shell-out — explicitly deprecated path (replaced by SDK call in anthropic/)

## 2. Architecture (high-level)

### 2.1 Adapter contract

Same `sql.Register`-style pattern as P3.8 SCM:

```go
// internal/llm/registry.go
func Register(kind string, factory FactoryFn)
func Open(ctx, kind, cfg) (LLMAdapter, error)
```

### 2.2 LiteLLM vs Portkey score-card (decide at dispatch)

| Criterion | LiteLLM | Portkey | Weight |
|---|---|---|---|
| Go SDK quality | Python proxy only (HTTP from Go) | Native Go SDK | High |
| Observability built-in | Basic | OTel + cost tracking | High |
| Provider count | 100+ | 60+ | Medium |
| License | MIT | Apache 2 | Equal |
| Self-host friction | Docker compose | Docker compose OR cloud | Medium |
| Persona-B fit (private deploy) | Excellent (full self-host) | Good (cloud-or-self) | High |

Tentative pick: **LiteLLM** (self-host + provider count favors persona-B). Locked at dispatch.

### 2.3 Migration plan

Order of call-site migration (one PR each, after the interface lands):

1. `internal/agent/l4/reviewer.go` — smallest surface, single completion call
2. `internal/agent/planauthor/` — multi-call but read-only
3. `internal/agent/commentauthor/` — write path; gates on cost-cap
4. `cmd/regatta/spawn/` — boot-time auth probe migrates last

### 2.4 Cost-cap integration

`CountTokens` is the load-bearing call for cost-cap pre-spawn gate. Adapter MUST be deterministic on `CountTokens` (same input → same count) so the cap memoize cache stays sound. Anthropic adapter uses the SDK's `client.Messages.CountTokens`; gateway adapter uses LiteLLM's `/v1/utils/token_counter` endpoint.

## 3. Key risks (named, ≥6)

| # | Risk | Mitigation seed |
|---|---|---|
| R1 | LiteLLM proxy adds an extra network hop → +50-200 ms p99 latency vs direct SDK | Adapter operator-flag `--no-gateway` keeps direct anthropic path; gateway is opt-in until persona-B asks |
| R2 | Provider price drift between gateway's reported pricing and actual invoice | Cost-ledger writes both `gateway_reported_micro` and `provider_native_micro`; weekly reconcile job (followup) |
| R3 | `CountTokens` non-determinism on gateway (provider model upgrade silently changes tokenizer) | Pin model version in `llm.default_model`; gateway adapter refuses to call without explicit version |
| R4 | LiteLLM auth model differs (master key vs per-key) — operator config error common | `regatta init` step-7 smoke test catches auth failures pre-prod; clear error message |
| R5 | Rate-limit semantics differ across providers behind one gateway | Adapter exposes `ErrRateLimited` uniformly; caller-side retry budget in cost-cap |
| R6 | Streaming surface drift — chunk format varies per provider behind gateway | Streaming explicitly out-of-scope v1; defer until persona ask |
| R7 | Persona-B air-gapped deployment — LiteLLM Python runtime adds 100MB+ image weight | Document as deploy footprint, not behavior risk; offer Anthropic direct-SDK path as zero-extra-runtime alternative |
| R8 | Cost-cap memoize keyed on `(tenant, model)` — gateway swaps model on fallback → cache stale | Adapter signals fallback via response metadata; cache invalidates on fallback signal |
| R9 | Token-count drift between Anthropic SDK and gateway's tokenizer (off by ±5%) | `TestTokenCount_AnthropicAndGatewayAgreeWithin5Pct` regression test; if drift > 5% gateway adapter logs WARN |

## 4. Test plan (≥8)

- `TestLLM_AnthropicAdapter_ByteEqualToBaseline` — golden test on L4 reviewer fixture
- `TestLLM_GatewayAdapter_RoutesToLiteLLM_Mock` — fake LiteLLM proxy, asserts request shape
- `TestLLM_CountTokens_Deterministic` — same input twice → same count
- `TestLLM_CountTokens_AnthropicGatewayAgreeWithin5Pct` — drift gate
- `TestLLM_RateLimit_ReturnsErrRateLimited` — unified error class across adapters
- `TestLLM_FallbackOnRateLimit_PicksNextModel` — gateway fallback chain
- `TestLLM_Models_ListsConfiguredOnly` — operator-facing `regatta llm models` matches config
- `TestLLM_CostCapIntegration_PreSpawnGateBlocks` — `CountTokens` × price > cap → blocked
- `TestLLM_RegattaInitStep7_SmokeTestPasses` — wizard catches misconfig
- `TestLLM_RegistryRejectsUnknownKind` — `kind: openai-direct` (typo) → clear error

## 5. Dependency order

`MVR-1-T5 SCM-adapter` lands first (validates the P3.8 adapter-contract pattern at second-consumer level → this spec is third-consumer, sharpens the pattern) → `cost-cap` (#596, shipped) — adapter respects existing per-tenant cap → this spec lands → call-site migration PRs follow (one per call site) → cost-cap memoize update (small) lands last.

## 6. Deferred to dispatch-time elaboration

- LiteLLM vs Portkey: final score-card with vendor-state-at-dispatch-time
- Exact LiteLLM Docker pin version
- OpenTelemetry span attribute naming (`llm.model`, `llm.tokens.input`, etc.) — align with OTel semconv 1.27+ AI namespace
- Fallback chain configuration format (string list vs structured priority)
- Streaming surface — re-evaluate at dispatch if a Wave-3 surface needs it

```release-notes
none (internal — design spec skeleton, pre-fetched for MVR-2)
```
