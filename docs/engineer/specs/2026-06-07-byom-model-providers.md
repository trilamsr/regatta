---
title: "BYOM model providers — companion to LLM-gateway skeleton"
status: active
summary: "Companion spec to `2026-06-03-mvr-2-t4-p38-llm-gateway-adapter.md` (parent skeleton). Elaborates the bring-your-own-model contract end-to-end: provider taxonomy (vendor-direct / proxy-gateway / local), per-provider seams (prompt-format, tool-call, streaming, cache-control), cost-cap routing fed by `internal/cost/pricing/` per-vendor tables, and fallback chaining. Acceptance: operator picks `llm.provider:` in `regatta.yaml` and the same config works on Anthropic, OpenAI, or a local model — only pricing + cache behavior differ. Cites #911 (secrets unification) as prereq for keying each provider. Does NOT duplicate the parent's interface / score-card / call-site migration order — those stay authoritative in the parent."
---

# BYOM model providers — companion spec

_Authored 2026-06-07. Companion to (not replacement of) `docs/engineer/specs/2026-06-03-mvr-2-t4-p38-llm-gateway-adapter.md` (parent). The parent owns the `LLMAdapter` interface, the LiteLLM-vs-Portkey score-card, and the call-site migration order. This spec owns the cross-provider contract (taxonomy, per-provider seams, cost-cap routing, fallback semantics, cache-control coverage) so an implementer can land BYOM in slices once the parent's interface lands._

## 1. Scope

### 1.1 In scope

- **Provider taxonomy** — three tiers (vendor-direct, proxy-gateway, local) with a default-list per tier and an extensibility seam for the rest.
- **Per-provider adapter seams** — prompt-format, tool-call, streaming protocol, cache-control, error taxonomy. Defined once at the contract level; each provider adapter implements its slice.
- **Cost-cap routing** — model id resolves to `(provider, sku)` which feeds the existing `internal/cost/pricing/` lookup (already has `anthropic.go`, `bedrock.go`, `vertex.go`). Pricing-miss is a closed-fail.
- **Fallback semantics** — per-provider rate-limit / outage triggers ordered secondary chain from `llm.fallbacks:` (parent §1.1).
- **Cache-control coverage matrix** — which providers honor prompt-cache and which silently no-op, so a model swap doesn't silently delete the L4 gate's #852 cache savings.
- **`regatta.yaml` schema extension** — `llm.provider:` + `llm.model:` + tier-specific blocks (`llm.aws:` for Bedrock, `llm.gcp:` for Vertex, `llm.local:` for self-hosted). Parent's `llm.fallbacks:` carries over.
- **Lift order for the 5 callsites where the Anthropic surface is hard-wired today** — §4.

### 1.2 Out of scope

- **Per-tenant routing** — Phase-X. Self-host filter (`CLAUDE.md` §Self-host filter) says single-tenant; multi-tenant routing reopens with MVR-2-T2 (already skeleton-pre-fetched: `2026-06-03-mvr-2-t2-w8-multi-tenant-routing.md`).
- **Per-PR provider rotation** — over-engineered (default-simpler gate). `llm.fallbacks:` already covers the failure case; rotation-for-load-balancing serves no current operator.
- **Training-data export / fine-tune publish** — not a regatta concern. Operators with fine-tuned models bring the endpoint via `llm.local:` or vendor-direct config.
- **Streaming surface for the chat UX** — parent §1.2 defers; this spec re-affirms.
- **Prompt-versioning / A/B** — parent §1.2 defers (Wave-3 wedge).
- **Concrete LiteLLM-vs-Portkey decision** — owned by parent §2.2 score-card; not re-litigated here.

## 2. Provider taxonomy

Three tiers. Tiering is a default-simpler bet: ship one provider per tier first, fill in siblings via the same adapter pattern when an operator asks.

### 2.1 Tier-1 must-have (lands with the parent's gateway adapter)

| Provider | Tier | Surface | Auth key (post-#911) |
|---|---|---|---|
| Anthropic direct | vendor-direct | Messages API + tool_use | `regatta.anthropic_api_key` (today) |
| OpenAI direct | vendor-direct | Chat Completions + function-calling | `regatta.openai_api_key` |
| LiteLLM proxy | proxy-gateway | OpenAI-shaped passthrough | `regatta.litellm_master_key` |

Rationale: Anthropic ships today; OpenAI is the most-requested second provider (parent's motivation); LiteLLM proxy covers everything else without n× direct adapters.

### 2.2 Tier-2 nice-to-have (one PR each, file-disjoint after Tier-1)

| Provider | Tier | Surface | Auth key |
|---|---|---|---|
| Google Gemini direct | vendor-direct | `generateContent` + function-declarations | `regatta.gemini_api_key` |
| AWS Bedrock | vendor-direct (cloud) | Converse API + tool config | AWS SDK chain (IAM role / `~/.aws/credentials`) |
| Google Vertex | vendor-direct (cloud) | `predict` + tool config | GCP ADC (service-account JSON) |
| OpenRouter | proxy-gateway | OpenAI-shaped marketplace | `regatta.openrouter_api_key` |
| Ollama | local | OpenAI-shaped (since 0.1.36) | none (URL only) |

Bedrock + Vertex already have stub pricing tables (`internal/cost/pricing/bedrock.go`, `vertex.go`) — wiring is the remaining gap.

### 2.3 Tier-3 Phase-X (reopen-trigger gated)

| Provider | Reopen trigger |
|---|---|
| Portkey | If LiteLLM picked at parent's dispatch + a customer requests Portkey's per-key observability |
| Azure OpenAI | First customer ask gated by AAD-tenant policy |
| llama.cpp / vllm direct | Operator with a non-OpenAI-shaped local runtime |
| Mistral / Cohere / Deepseek / Groq direct | One-customer-each demand |

Phase-X providers stay un-implemented; the adapter contract (§3) is the seam they slot into when the trigger fires.

## 3. Per-provider seams

Each provider adapter implements the parent's `LLMAdapter` (`Complete` / `CountTokens` / `Models` / `Stream` placeholder). The cross-provider contract this spec adds:

### 3.1 Prompt-format adapter

LLM APIs disagree on request shape; the adapter MUST translate a single internal `Request` into the provider's native body.

| Provider | System prompt | Tool-call shape | Reference |
|---|---|---|---|
| Anthropic | Top-level `system: [...]` block | `tool_use` content block + JSON Schema `input_schema` | Today's `internal/gates/l4/adapter.go::buildAnthropicPayload` + `internal/program/provider_anthropic.go::buildRequest` |
| OpenAI | First `messages[].role=system` | `tools[].function` + `tool_calls[]` response | OpenAI Chat Completions ref |
| Gemini | `systemInstruction` field | `tools[].functionDeclarations` + `functionCall` response | Gemini API ref |
| Bedrock | Embedded per-model (Claude wrapper uses Anthropic shape) | Converse `toolConfig` | Bedrock Converse API |
| Vertex | `systemInstruction` (Gemini-on-Vertex) OR Anthropic shape (Claude-on-Vertex) | Per underlying model | Vertex AI ref |
| Local (Ollama / vLLM / LiteLLM-proxy) | OpenAI-shaped passthrough | OpenAI `tools[].function` | OpenAI compat surface |

Internal `Request` shape (proposed):

```go
type Request struct {
    Model    string          // logical model id; provider adapter maps to vendor SKU
    System   []SystemBlock   // ordered; each carries Cacheable bool
    Messages []Message       // role + content
    Tools    []ToolSchema    // JSON Schema input; adapter renames to vendor field
    Options  RequestOptions  // temperature, max_tokens, stop, etc.
}
```

### 3.2 Tool-call adapter

L4 reviewer + planner both depend on server-enforced JSON-schema output via tool_use / function-calling. Adapter MUST:

1. Translate `Tools[].InputSchema` (JSON Schema 2020-12) to the provider's accepted dialect (OpenAI: JSON Schema subset; Gemini: OpenAPI 3.0 schema; Anthropic: JSON Schema 2020-12).
2. Translate the response tool-call block back to a unified `ToolCall{Name, Arguments}` so callers (`program.AnthropicPlanner.Plan`, `gates/l4/adapter.do`) don't branch per provider.
3. Refuse silently-degrading behavior: if a provider doesn't support `tool_choice: "required"` (Gemini today), the adapter returns `ErrUnsupported` rather than fall back to text-mode JSON parsing.

### 3.3 Streaming protocol

Out of scope at v1 per parent §1.2. Adapter contract reserves `Stream(ctx, req) (<-chan Chunk, error)` returning `ErrNotImplemented`; each provider's chunk format (Anthropic SSE event-stream, OpenAI SSE delta, Gemini server-sent JSON-lines) is recorded as a §4 followup so the v2 implementer doesn't re-research.

### 3.4 Cache-control coverage

Cache-control is the load-bearing perf surface (#852 lands ~80% input-token reduction on L4). Adapter MUST expose `Cacheable bool` per `SystemBlock` and degrade per-provider:

| Provider | Prompt cache support | Adapter behavior on `Cacheable=true` |
|---|---|---|
| Anthropic direct | Yes — `cache_control: {type: ephemeral}` (Messages API) | Emit cache_control header per #852 |
| OpenAI direct | Automatic (server-side, ≥1024-token static prefix) | No request-side annotation; observe `prompt_tokens_details.cached_tokens` in response |
| Gemini direct | Explicit — `cachedContent` API call (separate endpoint) | v1: no-op + WARN "prompt-cache requires separate cachedContents.create" + log lost-savings estimate. v2: pre-create + reuse name. Filed as followup. |
| Bedrock (Claude family) | Yes — same `cache_control` field as Anthropic direct | Emit cache_control |
| Bedrock (non-Claude) | No (Titan/Llama) | No-op + WARN once-per-process |
| Vertex (Claude-on-Vertex) | Yes — same as Anthropic direct | Emit cache_control |
| Vertex (Gemini-on-Vertex) | Explicit `cachedContent` | Same as Gemini direct |
| LiteLLM proxy | Passthrough per underlying provider | Translate to the provider-native field; LiteLLM ≥1.40 understands `cache_control` block |
| OpenRouter | Passthrough per underlying provider | Same as LiteLLM |
| Ollama / vLLM / llama.cpp | No (operator owns the runtime; KV cache is implicit) | No-op silently (no upstream WARN noise) |

Operator-facing: `regatta llm cache-status` (extends the parent's `regatta llm test`) reports the active provider's cache support so a misconfigured fallback doesn't silently regress L4 cost.

### 3.5 Cost-cap routing

Cost-cap gate (`internal/cost/spend/writer.go`, `internal/cost/pricing/lookup.go`) is keyed on `(provider, sku)`. The pricing table already partitions by provider (`anthropic.go`, `bedrock.go`, `vertex.go`). Adapter contract:

1. `Complete` response includes `Usage.Provider` + `Usage.SKU` + `Usage.{Input,Output,CacheRead,CacheCreation}Tokens`.
2. Spend writer looks up `(Provider, SKU)` in pricing tables; **miss is closed-fail**, not zero-USD (would silently un-gate the cost-cap).
3. `CountTokens` pre-spawn gate uses the adapter's `CountTokens(ctx, msgs)` — already specced in parent §2.4 as deterministic.
4. New pricing tables land per-tier: `openai.go` (Tier-1), `gemini.go` (Tier-2). LiteLLM proxy reports the underlying provider so existing tables resolve.
5. `pricing.Validate` (existing) extends to all new tables — guarantees no zero-row.

### 3.6 Fallback semantics

`llm.fallbacks:` (parent §1.1) is a model-id list. This spec defines the trigger semantics:

| Error class | Adapter returns | Fallback fires? |
|---|---|---|
| HTTP 429 (rate limit) | `ErrRateLimited` | Yes |
| HTTP 5xx (provider outage) | `ErrProviderUnavailable` | Yes |
| HTTP 401 (auth) | `ErrUnauthorized` | No (config bug — surface to operator) |
| HTTP 400 (request shape) | `ErrBadRequest` | No (caller bug) |
| Context-deadline | `context.DeadlineExceeded` | No (caller cancelled) |
| Cost-cap pre-spawn block | `ErrCostCapped` | No (caller decision) |

Fallback chain rules:

- Each fallback attempt logs `fallback_used` audit event with `from_model` + `to_model` + `error_class`.
- Cost-cap memoize cache invalidates on fallback (parent §3 R8 already names this risk; this spec resolves: `Response.Metadata.FallbackUsed bool` is the signal).
- Fallback budget: at most `len(fallbacks)` attempts per call; no recursive fallback-of-fallback.
- A fallback model in a different provider tier carries its own auth key resolution path (so `fallbacks: [openai/gpt-4o]` after `provider: anthropic` requires `regatta.openai_api_key` set).

## 4. Lift order — Anthropic-hard-wired callsites

5 production callsites import Anthropic-shape directly (verified by `grep -rn "api.anthropic.com\|x-api-key\|anthropic-version" --include='*.go' .`):

| # | File:line | Surface | Lift order |
|---|---|---|---|
| 1 | `internal/gates/l4/adapter.go:27` (base URL), `:60` (NewAnthropicAdapter), `:110-111` (auth headers), `:160-177` (buildAnthropicPayload), `:182-187` (anthropicUsage) | L4 reviewer gate — single Invoker closure | First. Smallest surface, single Complete call, hottest cost path (cache_control lives here). |
| 2 | `internal/program/provider_anthropic.go:34-64` (AnthropicPlanner struct + ctor), `:76-105` (Plan → HTTP), `buildRequest` (tool_use schema) | Planner — wraps tool_use for ProgramBrief | Second. Tool-call shape exercise. Multi-call signal but read-only — clean rollback if shape-drift surfaces. |
| 3 | `internal/cost/reconcile/client.go:67-98` (BaseURL + AnthropicVerHdr), `:194-195` (auth headers) | Cost reconcile — Anthropic Usage API client | Third. Provider-specific by definition (no `OpenAI Usage API` equivalent in the same shape). Stays Anthropic-only; lifted to live behind `internal/cost/reconcile/anthropic/` subpackage so the seam exists for OpenAI's usage endpoint later. Not part of the `LLMAdapter` interface. |
| 4 | `cmd/regatta/program.go:54` (`-model` default), `:57-58` (`-planner anthropic`), `:102,112` (planner dispatch) | CLI surface — `regatta program plan` | Fourth. Mechanical rename: `-planner anthropic` becomes `-llm-provider anthropic`. Parent's `regatta llm test` subcommand absorbs the smoke check. |
| 5 | `internal/secrets/secrets.go:34` (`KeyAnthropic` only) + `:41` (`CanonicalKeys` set) | Secrets — single canonical key | Resolved by #911 (`2026-06-07-secrets-config-unification.md`). `secrets:` block in regatta.yaml registers per-provider keys; CanonicalKeys becomes derived from the active `llm.provider:` + fallback chain. |

Lift sequencing: 1 → 2 in parallel (file-disjoint, both behind the parent's `LLMAdapter` interface), 4 follows once 1+2 land (CLI surface depends on both), 3 lands independently (reconcile is its own seam), 5 lands as #911 (already specced).

## 5. `regatta.yaml` extension

Parent's block (§1.1):

```yaml
llm:
  kind: anthropic | gateway
  endpoint: <url for gateway kind>
  default_model: <e.g., claude-sonnet-4-7-20260403>
  fallbacks: [<alternate models on rate-limit / outage>]
```

This spec extends to:

```yaml
llm:
  provider: anthropic | openai | gemini | bedrock | vertex | litellm | openrouter | ollama
  model: <provider-scoped model id, e.g. gpt-4o-2024-11-20>
  endpoint: <override for proxy or local; ignored for vendor-direct>
  fallbacks:
    - openai/gpt-4o-2024-11-20
    - anthropic/claude-sonnet-4-7-20260403
  # tier-specific blocks (only the active provider's block is read)
  aws:    { region: us-east-1, profile: default }    # Bedrock
  gcp:    { project: my-project, region: us-central1 }  # Vertex
  local:  { url: http://localhost:11434 }             # Ollama / vLLM / llama.cpp
```

Acceptance: same `regatta.yaml` works on Anthropic OR OpenAI OR a local model by swapping `provider:` + `model:`. Differences operators see: pricing (per-provider table) + cache support (§3.4 matrix) — surfaced by `regatta llm test` + `regatta llm cache-status`.

`secrets:` block (per #911) carries the API key for whatever provider is active; absent block → env-var fallback per #911 §3.

## 6. Risks

| # | Risk | Mitigation seed |
|---|---|---|
| BR1 | Cost-cap silently disabled when a provider's pricing table is missing | Pricing-miss is closed-fail (§3.5 #2); `make check` adds `verify-pricing-tables` step that lists active providers vs available tables. |
| BR2 | Cache savings lost on provider swap without operator awareness (Anthropic → OpenAI direct → Gemini direct = 80% → ~auto → 0% with no warning) | `regatta llm cache-status` exits non-zero when active provider's cache tier is lower than the previous run's; operator-facing migration warning. |
| BR3 | Tool-call schema dialect drift (OpenAI rejects `additionalProperties: false` placement that Anthropic accepts) | Adapter normalizes the canonical internal schema to each provider's accepted dialect at request time; golden test per provider asserts byte-equal cross-provider output on the L4 fixture. |
| BR4 | Fallback chain crosses providers but key for fallback provider is unset (`fallbacks: [openai/...]` after `provider: anthropic` without `openai_api_key`) | `regatta llm test` walks the full fallback chain and verifies each provider's key resolves; closed-fail at config-load. |
| BR5 | LiteLLM-proxy adds a hop that disguises the real provider on the wire — debugging "which provider answered?" gets harder | Audit event `llm_invocation` carries `wire_provider` (LiteLLM passthrough header `x-litellm-model-id`) separate from `configured_provider`. |
| BR6 | Local-model adapters (Ollama / vLLM) have no documented pricing — cost-cap goes blind | Local-tier pricing table fixed at `$0.00/MTok` with a one-line operator warning at `regatta llm test`; cost-cap stays armed for the token-budget axis (`MaxOutputTokens`). |
| BR7 | A fallback fires under cost-cap memoize cache hit, but the cached cost was for the primary model with different pricing | Memoize cache keyed on `(tenant, provider, sku, prompt_hash)`; provider+sku change → cache miss (parent R8 + this spec's §3.6 close together). |
| BR8 | Provider deprecates a model mid-deployment (OpenAI gpt-4-turbo retirement pattern) | `regatta llm models` cross-checks configured `model:` against provider's live list; CI nightly cron warns when configured model approaches deprecation date (manifested in provider's model card). Followup wedge. |
| BR9 | Gemini's `cachedContent` separate-endpoint model breaks the "single Complete call" assumption | v1 no-op + WARN (§3.4); v2 implements the create-and-reuse dance behind the adapter so callers stay single-call. Tracking issue at land time. |
| BR10 | OpenAI's auto-cache silently bills cached tokens at full rate when the static prefix sits below the 1024-token threshold | Adapter logs `prompt_tokens_details.cached_tokens` per response; `regatta cost report` shows cache-hit-rate per model so misalignment surfaces in the weekly review. |

## 7. Test plan

- `TestBYOM_Registry_RejectsUnknownProvider` — `provider: foo` returns clear error at config-load, not at first Complete call.
- `TestBYOM_PricingMissIsClosedFail` — provider with no pricing table refuses to load; CI verifies all Tier-1+Tier-2 tables present.
- `TestBYOM_OpenAIAdapter_RoutesToolCall` — JSON-schema input becomes `tools[].function` in request; response `tool_calls[0]` becomes unified `ToolCall`.
- `TestBYOM_GeminiAdapter_CacheControl_NoOpWithWARN` — `Cacheable=true` on Gemini direct logs WARN exactly once per process and emits no `cachedContents.create` call (v1 deferral).
- `TestBYOM_FallbackChain_FiresOn429NotOn401` — auth error doesn't burn fallback budget.
- `TestBYOM_FallbackChain_CrossProviderKeyMissing_ClosedFailAtConfigLoad` — `provider: anthropic` + `fallbacks: [openai/...]` without `openai_api_key` → load error.
- `TestBYOM_CostCapMemoize_InvalidatesOnFallback` — primary's memoized cost doesn't satisfy fallback's gate (cache keyed on `(tenant, provider, sku, prompt_hash)`).
- `TestBYOM_LiteLLMProxy_PassthroughCacheControl` — cache_control block in request reaches Anthropic-behind-LiteLLM.
- `TestBYOM_RegattaLLMTest_WalksFallbackChain` — each provider's key + endpoint verified; missing key in chain is closed-fail.
- `TestBYOM_AuditEvent_CarriesWireProvider` — LiteLLM hop records both `configured_provider=litellm` and `wire_provider=anthropic` so debug logs unambiguous.
- `TestBYOM_ToolSchema_DialectDrift_Anthropic_vs_OpenAI` — golden test asserts internal schema normalizes correctly for both dialects.
- `TestBYOM_LocalAdapter_PricingZeroWithWARN` — Ollama returns `$0` cost + WARN at `regatta llm test`; token-budget axis still gates.

## 8. Dependency order

1. Parent's `LLMAdapter` interface lands (`internal/llm/iface.go` + registry) — owned by parent spec.
2. **#911 secrets unification** lands — keys for OpenAI / Gemini / OpenRouter / LiteLLM resolvable via `secrets:` block. This spec's §3 / §5 depend on it.
3. Tier-1 PRs land file-disjoint:
   - `internal/llm/openai/` — OpenAI direct adapter (lift #1 above absorbs L4; lift #2 absorbs planner).
   - `internal/llm/gateway/litellm/` — LiteLLM proxy adapter (parent §2.2 picks LiteLLM tentatively).
   - `internal/cost/pricing/openai.go` — pricing table.
4. Tier-2 PRs land one-per-provider, file-disjoint with Tier-1.
5. Lift #3 (cost reconcile) lands independently as `internal/cost/reconcile/anthropic/` subpackage.
6. Lift #4 (CLI surface) lands once Tier-1 in place.
7. Tier-3 stays Phase-X until per-provider trigger fires.

## 9. Open questions for dispatch-time

- Internal `Request` struct exact field set — pinned at parent's `LLMAdapter` dispatch.
- Pricing-table maintenance cadence — already addressed for Anthropic via `pricingRevTag` (`internal/cost/spend/writer.go`); extends per-provider with the same monthly-review pattern.
- `regatta llm models` source-of-truth — vendor's `/models` endpoint vs. embedded list; vendor endpoint preferred (avoids stale-list drift) but requires auth — defer to per-provider PR.
- LiteLLM `cache_control` translation correctness — vendor doc says ≥1.40 honors it for Anthropic-behind-LiteLLM; verify on first integration test.

```release-notes
docs: spec BYOM model-providers (extends LLM-gateway skeleton)
```
