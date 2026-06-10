---
title: "BYOA — bring-your-own-agent spawner adapter"
status: active
phase: mvr-1
summary: "P3.8-pattern bring-your-own-agent spawner: today's Spawner iface is ready, but the only impl is hard-wired to `claude`. Move `ClaudeSpawner` into `internal/orchestrator/spawner/claude/`, add `Register/Open` to spawner top, ship an Aider skeleton as the second adapter so persona-B/C operators on Aider, Cursor headless, or Codex CLI can swap backends via `regatta.yaml::spawner.kind`."
---

# BYOA — Bring-Your-Own-Agent spawner adapter — Design Spec

Date: 2026-06-07
Phase: MVR-1 (adoption-cost collapse)
Companion specs:

- `docs/engineer/specs/2026-06-01-adapter-contracts-design.md` — P3.8 adapter-contract pattern (`sql.Register`-style; common skeleton).
- `docs/engineer/specs/phase-x/2026-06-02-mvr-1-t3-p38-scm-adapter-gitea-first.md` — sibling SCM adapter spec; mirror its shape (interface + first-party + second-party + registry).
- `docs/engineer/specs/2026-06-04-mvr-1-t4-github-issues-adapter-impl.md` — sibling work-item adapter spec; mirror its contract section.
- `docs/engineer/specs/phase-x/2026-05-31-mvp-3-w6-otel-backbone.md` — `operator_invocation` span contract every spawner adapter MUST emit.
- `docs/engineer/specs/2026-06-03-mvr-2-t4-p38-llm-gateway-adapter.md` — sibling LLM-gateway P3.8 skeleton (downstream of the agent runner; not this spec's scope).

```release-notes
docs: spec BYOA — bring-your-own-agent adapter pattern
```

---

## 1. Problem

`internal/orchestrator/spawner/spawner.go:56` defines the `Spawner` interface (one method: `Spawn(ctx, Request) (Result, error)`). Two impls exist today:

- `Stub` (`spawner.go:100`) — in-memory recorder; used by tests and by `regatta serve` until a real agent is wired.
- `ClaudeSpawner` (`claude.go:26`) — the only production impl; hard-wires the `claude` binary at `claude.go:88` (`cfg.Command = "claude"`) and reads its stream-json output via `genai.go:101 ParseStream`.

`cmd/regatta/wire_spawner.go:32` selects between them via `-spawner stub|claude`. Any operator who runs Aider, Cursor headless, Codex CLI, Cody, or a local-LLM runner (`ollama`, `llama.cpp` agent wrappers) cannot use regatta without patching Go code.

The Spawner interface is **ready** for a second consumer — only the implementation surface is Claude-shaped. Per `feedback_research_design_principles`, a second consumer is the only honest test that the interface is not Claude-shaped by accident.

Persona impact:

- **persona-B (OpenAI-shop OSS maintainer)** — wants Codex CLI or Cursor headless behind the same regatta orchestrator.
- **persona-C (cost-sensitive solo dev)** — wants Aider against a local Ollama model for self-host without API spend.
- **persona-D (Gitea operator, already cited in T3)** — wants any agent runner that does not require an Anthropic API key.

Roadmap row this fills: G8 — agent runner beyond Claude (sibling to G7 SCM-beyond-GitHub already addressed by T3).

## 2. Scope

### In scope

- `internal/orchestrator/spawner/iface.go` — extract the existing `Spawner` interface + supporting value types (`Request`, `Result`) into a stable file with a 1-line WHY-form godoc. No method-set change.
- `internal/orchestrator/spawner/registry.go` — `Register(kind string, Factory)` + `Open(ctx, kind, cfg) (Spawner, error)` — the P3.8 `sql.Register` shape from `2026-06-01-adapter-contracts-design.md` §Common skeleton.
- `internal/orchestrator/spawner/claude/` — move `claude.go` + `claude_test.go` + `claude_genai_test.go` here. `init()` calls `spawner.Register("claude", New)`. Byte-equal behavior on the existing path — golden tests from `claude_genai_test.go` carry over verbatim.
- `internal/orchestrator/spawner/aider/` — second-party skeleton. Constructs an `aider --message-file <prompt> --yes --no-pretty` subprocess; parses Aider's `--stream` JSONL into `StreamResultEvent` so the cost-governor callback fires the same shape it does for Claude.
- `regatta.yaml` `spawner:` block + CUE row in `contracts/schemas/regatta.v1.cue` — closed enum `kind: claude | aider`; `cursor` and `codex` reserved as documented Phase-X reopen-triggers (no schema row yet).
- `regatta spawner list` + `regatta spawner test <kind>` operator subcommands — list registered kinds + dry-run a no-op task against the configured backend.
- One canary path: `wire_spawner.go` switches from a hard-coded `switch name` to `spawner.Open(ctx, cfg.Spawner.Kind, cfg.Spawner)`. The stub stays a registered kind (`spawner.Register("stub", NewStub)`) for byte-equal test wiring.

### Out of scope (Phase-X reopen-triggers)

- **Cursor headless adapter** — reopen when persona-B inbound names Cursor + production-tier license. The headless flag landed in Cursor 0.42; today the protocol is undocumented (JSON-RPC over IPC). Pre-filed follow-up at PR merge per `feedback_unaddressed_load_bearing`.
- **Codex CLI adapter** — reopen on OpenAI shipping a stable headless flag + ToS that allows CI use. As of the cutoff date, the CLI exists in preview only.
- **Cody / Continue / local-LLM runners** — reopen on named inbound. The Aider second-party adapter proves the seam; adding more is mechanical at the cost of adapter LoC.
- **Per-agent telemetry deltas** — cost-cap calibration per backend (Aider tokens vs Claude tokens) is its own spec; this spec ships the seam, not the calibration table.
- **Multi-agent-per-PR routing** — running Claude + Aider in parallel on the same work item and picking the winner. Phase-X (depends on W11 shared-state primitive + a credible scoring oracle).
- **Vendor-LLM-gateway adapter** (`MVR-2-T4` LiteLLM/Portkey) — different seam: that adapter sits **inside** the agent runner (between Claude and Anthropic). This spec sits **above** the agent runner (between the orchestrator and any runner). The two are orthogonal — both can land independently.

### Self-host filter

The internal operator (Tri) uses Claude today, so a Claude-only path already ships. Self-host filter passes because:

- Without a second adapter, the `Spawner` interface is unfalsifiable — `Spawn(ctx, Request) Result` looks abstract but in fact mirrors Claude's "feed prompt on stdin, read stream-json on stdout" assumption verbatim (`claude.go:131-138`). Aider is the load-bearing test rig that catches the assumption.
- Aider is fastest to wire: one binary on `$PATH`, prompt on `--message-file`, stream JSONL output already documented, MIT license. Estimated ~400 LoC adapter — counted by reference to `claude.go` (300 LoC) plus an Aider-specific stream decoder (~100 LoC) replacing `genai.go` for the Aider path.
- The deferred adapters (Cursor, Codex, Cody) explicitly fail the self-host filter — they exist for hypothetical external operators only.

## 3. Survey: existing seams

### 3.1 `Spawner` interface (`spawner.go:56`)

```go
type Spawner interface {
    Spawn(ctx context.Context, req Request) (Result, error)
}

type Request struct {
    AgentID    int64
    WorkItemID string
    Lane       string
    OperatorID string
    DAGID      string
    RunID      string
    ItemBody   string
    RepoRoot   string
}

type Result struct {
    PID       int
    SessionID string
}
```

Surface area is one method. The cost-governor callback (`ClaudeSpawnerConfig.OnResultEventFor`, `claude.go:67`) is wired through the config struct, not the interface — so adding a second impl does not pull cost wiring into the interface, but it does mean every adapter MUST emit a `StreamResultEvent` shape to honor the same cost-governor seam.

### 3.2 `ProcessStarter` seam (`claude.go:80`)

```go
type ProcessStarter func(ctx context.Context, name string, args []string,
    stdin io.Reader, stdout io.Writer, dir string) (*exec.Cmd, error)
```

Claude-only today (`claude.go:109 SetStarter`), but the shape generalizes. Aider, Cursor headless, and Codex CLI all spawn a subprocess; lifting `ProcessStarter` to the `spawner` package top level lets every adapter share the test seam.

### 3.3 `cfg.Command / cfg.Args` (`claude.go:38-44`)

Already configurable. An operator who has an alternate agent runner that speaks **stream-json on stdout** (e.g. a wrapper script that mimics Claude's protocol) can already point `cfg.Command` at it. Reality: no other agent runner speaks Claude's stream-json. The binary swap is necessary-but-not-sufficient — the parser is also Claude-shaped.

### 3.4 `wire_spawner.go::buildSpawner` (`cmd/regatta/wire_spawner.go:32`)

Today a `switch name` over a hard-coded `stub|claude` set. The canary migration in §6 replaces the switch with `spawner.Open(ctx, kind, cfg)`. Stub stays a registered kind; the call-site shape is one line.

### 3.5 Stream-event projection (`genai.go:66 StreamResultEvent`)

```go
type StreamResultEvent struct {
    Model                    string
    MessageID                string
    StopReason               string
    IsError                  bool
    InputTokens              int64
    OutputTokens             int64
    CacheReadInputTokens     int64
    CacheCreationInputTokens int64
}
```

The cost-governor consumes this projected shape. It is **already** decoupled from `streamEvent` (the Claude-CLI-internal type, `genai.go:38`) — Aider and any future adapter project their own native event into this shape so the cost row stays uniform. This is the single load-bearing contract the spec preserves verbatim.

### 3.6 `operator_invocation` span (`spawner.go:134`, `claude.go:130`)

Both impls open one `operator_invocation` span per Spawn. W6 spec §3.4–3.5 nails the attribute set:

- `agent.id`, `work_item.id`, `lane` are mandatory.
- `llm_call` child spans land per stream event.

Every BYOA adapter MUST open the same span shape and emit `llm_call` children for every observed call. The OTel-span contract is part of the seam, not optional.

## 4. Gap analysis

`genai.go::ParseStream` is named generically but only parses Claude's `--output-format=stream-json` shape (`type`/`subtype`/`session_id`/`usage`). Aider speaks **`--stream`** JSONL with different field names (`role`, `content`, `tokens_sent`, `tokens_received`, `cost`). Cursor headless: different again (JSON-RPC frames; method names like `agent.message`). Codex CLI: ChatGPT-shaped — `delta`, `finish_reason`, `usage` matching OpenAI's `chat.completions` streaming protocol.

| Surface | Today | Per-adapter need |
|---|---|---|
| Subprocess construction | `cmd.Command("claude", args)` (`claude.go:141`) | Per-adapter: binary name, flag shape, prompt-on-stdin vs prompt-on-arg vs prompt-on-file |
| Prompt delivery | `strings.NewReader(prompt)` on stdin | Aider: `--message-file`; Cursor: JSON-RPC `initialize` frame; Codex: stdin still works |
| Stream parsing | `genai.go::ParseStream` (Claude JSONL) | Per-adapter: native protocol → `StreamResultEvent` projection |
| Worktree management | `WorktreeManager` (`worktree.go`) | Shared. Every adapter operates inside a per-agent worktree — same lifecycle. |
| `KillAgent` | `ClaudeSpawner.KillAgent` (`claude.go:192`) | Shared shape: every adapter holds a `map[int64]*exec.Cmd` and SIGTERM-then-SIGKILL escalates. |
| Cost-governor callback | `OnResultEventFor(Request) ResultEventCallback` | Shared shape. Per-adapter parser projects native usage → `StreamResultEvent`. |
| `operator_invocation` span | Opened by `Spawn`; `llm_call` children opened by `ParseStream` | Shared shape; each adapter's parser opens children. |

Net: **subprocess construction + prompt delivery + stream parsing** are per-adapter; **worktree, kill, span, cost callback** are shared. The gap analysis settles the §5 option pick: an adapter-per-agent shape with a shared subprocess-management helper.

## 5. Options

### Option A (recommended) — Adapter per agent

```
internal/orchestrator/spawner/
  iface.go            // Spawner interface + Request/Result + Factory + ProcessStarter (lifted)
  registry.go         // Register / Open — sql.Register style
  process.go          // shared subprocess + worktree + kill helpers
  spawner.go          // Stub (registered as "stub")
  worktree.go         // shared, unchanged
  claude/
    adapter.go        // ClaudeSpawner → moved here, registers "claude"
    genai.go          // Claude-CLI stream-json parser (moved here, package-private)
    *_test.go         // moved
  aider/
    adapter.go        // AiderSpawner, registers "aider"
    stream.go         // Aider --stream JSONL parser → StreamResultEvent
    *_test.go
```

Each adapter owns its prompt delivery + stream parser; the shared `process.go` exposes `Run(ctx, opts) (*exec.Cmd, error)` so adapters do not re-implement the tee-stdout + worktree-dir + ctx-cancel plumbing.

**Pro**: per-adapter package boundary is the same shape every other P3.8 adapter uses (SCM, github_issues, secrets, LLM-gateway). One reading model across the codebase.

**Con**: Three packages instead of two. Mitigated by the move being mechanical (`git mv` + add `init()`).

### Option B — Generic ProcessSpawner + per-agent stream-parser plugin

One generic `ProcessSpawner` impl that takes a `StreamParser interface { Parse(io.Reader, ResultEventCallback) error }` plugin. The binary + args + prompt-delivery shape live in config.

**Pro**: less code. **Con**: tighter coupling — the config carries shape-of-flags for every supported agent, which leaks adapter knowledge into the operator's `regatta.yaml`. Also no clear home for adapter-specific quirks (Cursor's JSON-RPC handshake, Codex's auth probe). Cross-cutting against the established P3.8 pattern.

**Rejected**.

### Option C — Interface only; tell operators to write their own

Keep `Spawner` as the public interface; do not ship a second first-party adapter; document the contract for operators to implement.

**Pro**: zero new code. **Con**: persona-B/C never adopt — writing 400 LoC of Go to swap agent runners is past their friction tolerance. Also violates `feedback_research_design_principles` — a second consumer is the only honest test that the contract shape is right.

**Rejected**.

## 6. Recommendation: Option A

A mirrors:

- `internal/scm/{github,gitea}/` (T3) — same `init()` `Register("kind", New)` shape.
- `internal/orchestrator/adapter/{markdown_catalog,github_issues}.go` (T4) — same per-kind file boundary, same registry constructor shape.
- `internal/adapters/<name>/` (P3.8 §Common skeleton) — same package layout.

One reading model across all five adapter seams (SCM, work-item, spawner, LLM-gateway, secrets) lowers operator + implementer cost. The adapter-per-agent boundary is the same one used in `database/sql`'s driver registry — known-good Go convention.

## 7. Required changes (Option A)

### 7.1 File moves + adds

```
internal/orchestrator/spawner/
  spawner.go               // KEEP: Stub + Config + ResolveMeter + (NEW) init() Register("stub", NewStub)
  iface.go                 // NEW: extract Spawner / Request / Result, 1-line WHY godocs
  registry.go              // NEW: Register + Open + ListKinds (~60 LoC)
  process.go               // NEW: shared subprocess plumbing extracted from claude.go (~80 LoC)
  worktree.go              // KEEP: unchanged
  claude/
    adapter.go             // MOVE: claude.go → claude/adapter.go + init() Register("claude", New)
    genai.go               // MOVE: genai.go → claude/genai.go (now package-private)
    adapter_test.go        // MOVE: claude_test.go
    genai_test.go          // MOVE: claude_genai_test.go
  aider/
    adapter.go             // NEW: ~200 LoC; subprocess + prompt-file + stream-parser wiring
    stream.go              // NEW: ~150 LoC; Aider JSONL → StreamResultEvent
    adapter_test.go        // NEW: stub-subprocess + golden-output assertion
    stream_test.go         // NEW: parser unit tests against captured Aider --stream fixtures
    testdata/aider-stream-fixtures.jsonl  // captured fixture (live `aider` run, anonymized)

cmd/regatta/
  spawner_cmd.go           // NEW: `regatta spawner list|test` (~80 LoC)
  spawner_cmd_test.go      // NEW
  wire_spawner.go          // CHANGED: switch → spawner.Open(cfg.Spawner.Kind, cfg.Spawner)

contracts/schemas/
  regatta.v1.cue           // CHANGED: +#Spawner block (closed enum kind: stub | claude | aider)

regatta.yaml               // CHANGED: +spawner: kind: claude
```

Net delta estimate: 1 file removed (`claude.go` → moved), 2 files moved (`claude*.go`), 9 files added, 3 files changed. Across all paths: **~700 LoC added, ~50 LoC deleted** (the `switch name` in `wire_spawner.go` and the inline `genai.go` package doc that gets re-homed). Deletion column: the `wire_spawner.go::switch` (40 LoC) collapses to one `spawner.Open` call.

### 7.2 Interface lift (`iface.go`)

```go
// Spawner is the swap-seam between the orchestrator and an agent runner
// (Claude Code, Aider, Cursor headless, etc.). Impls MUST be safe for
// concurrent calls — the orchestrator may spawn multiple agents per tick.
type Spawner interface {
    Spawn(ctx context.Context, req Request) (Result, error)
}

// Killer is the seam the Reaper consumes to terminate runaway children.
// Optional capability — detected via type assertion (cf. http.Pusher).
// The stub omits it; the claude + aider adapters implement it.
type Killer interface {
    KillAgent(agentID int64) (bool, error)
}

// Factory constructs an adapter from a parsed config block. Adapters
// register themselves via init() — see registry.go.
type Factory func(ctx context.Context, cfg Config) (Spawner, error)
```

No method-set change to `Spawner`; the existing one-method surface survives the second consumer (verified in §11 review). `Killer` is broken out as a capability interface so the Reaper's adoption story (`#45`) does not need to change.

### 7.3 Registry (`registry.go`)

```go
var (
    mu        sync.RWMutex
    factories = map[string]Factory{}
)

func Register(kind string, f Factory) {
    if kind == "" || f == nil {
        panic("spawner: invalid Register call")
    }
    mu.Lock()
    defer mu.Unlock()
    if _, dup := factories[kind]; dup {
        panic("spawner: duplicate Register for kind " + kind)
    }
    factories[kind] = f
}

func Open(ctx context.Context, kind string, cfg Config) (Spawner, error) {
    mu.RLock()
    f, ok := factories[kind]
    mu.RUnlock()
    if !ok {
        return nil, fmt.Errorf("spawner: unknown kind %q (registered: %s)", kind, kindsLocked())
    }
    return f(ctx, cfg)
}

func ListKinds() []string { /* sorted snapshot */ }
```

`sql.Register`-style: panic on dup, return typed error on unknown kind with the registered set listed (`feedback_decision_priority` UX > performance). `ListKinds` powers `regatta spawner list`.

### 7.4 Aider adapter — first cut (`aider/adapter.go`)

Constructor signature mirrors the Claude side so callers can swap byte-for-byte:

```go
type Config struct {
    Command   string         // default "aider"
    Args      []string       // default ["--yes", "--no-pretty", "--stream"]
    Model     string         // default "" (Aider picks from $AIDER_MODEL or its own default)
    Tracer    trace.Tracer
    OnResultEventFor func(spawner.Request) spawner.ResultEventCallback
}

func New(ctx context.Context, cfg spawner.Config) (spawner.Spawner, error) { ... }

// Registered at package init:
func init() { spawner.Register("aider", New) }
```

`Spawn` writes the prompt to a worktree-local file (`<worktree>/.regatta/aider-prompt-<run_id>.md`), then execs `aider --message-file <path> ...`. Stream parser reads Aider's `--stream` line-JSON (`{"role":"assistant","content":"...","tokens_sent":N,"tokens_received":N,"cost":F,"model":"..."}`) and projects into `StreamResultEvent` with the cost-governor callback firing once per assistant turn.

Aider quirks the parser absorbs:

- Aider emits `{"info":"..."}` status frames interleaved with assistant frames — parser skips on missing `role`.
- Aider's `cost` is **already USD**, not tokens; the parser stamps `OutputTokens=0` and writes a side-channel `cost_usd` attribute on the `llm_call` span. Cost-governor consumes via the same `OnResultEventFor` shape, with the side-channel field — extending `StreamResultEvent` with `CostUSD float64` (new field; zero-value preserves Claude's byte-equal write).
- Aider does not emit `cache_read_input_tokens` — zero-value preserves Claude byte-equal contract.

### 7.5 Operator UX subcommand (`regatta spawner list|test`)

```
$ regatta spawner list
kind     description                                       registered_by
stub     in-memory recorder (test + bootstrap)             internal/orchestrator/spawner
claude   Anthropic Claude Code CLI (stream-json)           internal/orchestrator/spawner/claude
aider    Aider conversational coder (JSONL stream)         internal/orchestrator/spawner/aider

$ regatta spawner test aider
[1/4] binary on PATH       OK (aider 0.86.1)
[2/4] no-op spawn          OK (pid 49301, exited 0 in 1.2 s)
[3/4] stream parse         OK (1 assistant frame, 240 input + 18 output tokens)
[4/4] cleanup              OK (worktree removed)

spawner: aider backend healthy (3.4 s)
```

Wired into `regatta self-test` (MVR-1-T2 §step-7 smoke test) the same way `regatta scm test` is.

### 7.6 `regatta.yaml` schema additions

```yaml
spawner:
  kind: claude               # claude | aider | stub (closed enum)
  claude:
    command: claude          # default; override for nightly builds, etc.
    base_ref: HEAD           # default
  aider:
    command: aider           # default
    model: ""                # empty → Aider's own default
    extra_args: []           # appended after the defaults
```

CUE schema row in `contracts/schemas/regatta.v1.cue`:

```cue
#Spawner: {
    kind: "claude" | "aider" | "stub" | *"claude"
    claude?: {
        command: string | *"claude"
        base_ref: string | *"HEAD"
    }
    aider?: {
        command: string | *"aider"
        model:   string | *""
        extra_args: [...string] | *[]
    }
    if kind == "claude" { claude!: _ }
    if kind == "aider"  { aider!:  _ }
}
```

The disjoint-block constraint catches the `kind: aider` + missing `aider:` typo at config-load, not at first Spawn — same shape T3 used for SCM.

## 8. Acceptance

- (a) `Spawner` interface lives at `internal/orchestrator/spawner/iface.go` with the existing one-method surface unchanged.
- (b) `internal/orchestrator/spawner/registry.go` exports `Register / Open / ListKinds`; duplicate registration panics, unknown-kind `Open` returns a typed error listing the registered set.
- (c) Claude adapter at `internal/orchestrator/spawner/claude/` passes every existing `claude_test.go` + `claude_genai_test.go` assertion byte-equal — no behavior change on today's path.
- (d) Aider adapter at `internal/orchestrator/spawner/aider/` spawns a real `aider` subprocess against a stub no-op task and parses the `--stream` output into at least one `StreamResultEvent` matching `tokens_sent` / `tokens_received` / `cost_usd` from the captured fixture.
- (e) `regatta.yaml::spawner` block accepted; CUE rejects malformed (missing nested block) at load time.
- (f) `regatta spawner list` returns the three registered kinds; `regatta spawner test aider` exits 0 against a healthy aider install and exits 1 with a recoverable error message on a missing binary.
- (g) E2E: a smoke `regatta serve --spawner-kind=aider` against a noop work item completes one Spawn → ParseStream → `operator_invocation` span emitted with one `llm_call` child → Reaper cleans up the worktree.
- (h) Followup tracking issues filed at PR merge (§12) per `feedback_unaddressed_load_bearing`.

## 9. Out of scope

- Cost-cap calibration per agent (Claude tokens vs Aider USD vs OpenAI tokens).
- Multi-agent-per-PR routing + winner-pick scoring oracle (Phase-X, depends on W11 shared-state primitive).
- Cursor / Codex / Cody / Continue / local-LLM adapters (per-persona reopen-trigger; pre-filed follow-ups).
- Vendor-LLM-gateway adapter (LiteLLM / Portkey) — sibling spec at `2026-06-03-mvr-2-t4-p38-llm-gateway-adapter.md`; sits inside the agent runner, orthogonal to this spec.
- Per-adapter telemetry surface deltas — every adapter MUST emit the `operator_invocation` span + `llm_call` children, but per-adapter custom attributes are a follow-up.

## 10. Closes / tracks

- Spec authority for the spawner-adapter row called out in `2026-06-01-adapter-contracts-design.md` §"Common skeleton" but never filled (the contract row is reserved for the spawner seam; this spec fills it).
- File new tracking issue at PR merge: `byoa: cursor headless adapter (P3.8 third-consumer proof)` — reopen-trigger named persona-B inbound.
- File new tracking issue at PR merge: `byoa: codex CLI adapter` — reopen-trigger OpenAI ToS allows CI use.
- File new tracking issue at PR merge: `byoa: per-adapter cost-cap calibration table` — reopen-trigger first non-Claude operator hits a cost-cap surprise.

## 11. Adversarial review (folded inline)

Spawned reviewer subagent (`docs/engineer/dispatch-templates/reviewer.md`) targeting the five lenses per `feedback_adversarial_review`:

- **Edge cases.**
  - (i) Aider emits `info`-only frames before the first assistant frame — parser skips on missing `role`; covered by §7.4.
  - (ii) Aider exits non-zero on a model-side rate limit — the adapter projects the exit code into `ErrPermanent` only if no assistant frame landed; otherwise the partial result counts (operators see "1 turn completed; rate-limited" rather than a silent failure).
  - (iii) Worktree-local prompt file leaks the prompt sentinel if Aider crashes mid-write — `defer os.Remove(promptPath)` in `Spawn`. Sentinel hygiene matches Claude's stdin path (no on-disk artifact); explicit cleanup gap noted as a follow-up issue (§12).
  - (iv) Aider's `--stream` flag is undocumented as stable; pin version floor in adapter constructor — `New` calls `aider --version`, errors on `< 0.86` (the version that stabilized the JSONL frame shape per Aider CHANGELOG 2025-12).

- **Simplification candidates.**
  - (i) Drop the `Killer` capability interface and require it on `Spawner`? Rejected — the stub legitimately has nothing to kill; capability detection matches today's `Reaper` adoption story.
  - (ii) Inline the registry into `cmd/regatta/wire_spawner.go` instead of an exported `Open`? Rejected — breaks parity with every other P3.8 adapter (SCM, github_issues, secrets) and the operator subcommand needs `ListKinds`.
  - (iii) Skip `regatta spawner test` and rely on `regatta self-test`? Considered — but the per-kind diagnostic is one of the two surfaces persona-C looks at first when an install fails. Keep.

- **Deletion candidates.**
  - (i) Drop the canary `wire_spawner.go` migration from this PR? Rejected — it is the only honest test that the registry compiles against a real caller, mirroring T3 §4.
  - (ii) Drop the move of `genai.go` into `claude/`? Considered — but leaving `genai.go` at the spawner package root means every BYOA adapter has it on its import path, and the Claude-CLI-specific JSON shapes leak into the public package surface. Move.
  - (iii) Drop `regatta spawner list`? Considered — operators can `grep` the registered kinds from logs. Keep for symmetry with `regatta scm` and because it costs ~20 LoC.

- **Risk tiers.**
  - High: Claude byte-equal regression on the `claude/` move. Mitigation: every existing test in `claude_test.go` + `claude_genai_test.go` carries over verbatim; `wire_spawner.go::buildSpawner` golden test pinned at the cmd boundary.
  - Mid: Aider `--stream` format drift between minor versions. Mitigation: version-floor probe (§11 edge case iv) + a captured `testdata/aider-stream-fixtures.jsonl` golden that the CI re-asserts byte-equal.
  - Mid: cost-governor double-billing if the parser emits `cost_usd` AND `OutputTokens > 0` for the same turn. Mitigation: Aider parser writes one or the other (USD when emitted; tokens when USD absent); cost-governor SUMs both columns so duplication is observable at audit time.
  - Low: Aider prompt-file leak on crash. Mitigation: `defer cleanup`; followup-tracked.
  - Low: registry order surprise — `init()` order is undefined across packages. Mitigation: `wire_spawner.go` imports both `claude` and `aider` explicitly, so both `init()`s land before `Open` is called.

- **OSS reuse the spec missed.**
  - `github.com/Aider-AI/aider` is Python — no Go SDK; the adapter shells out, which is what every Aider integration does today.
  - `simonw/llm` CLI ships its own `--stream-json` shape similar to Claude's; a future `llm` adapter could ride a shared "OpenAI-stream-json" parser with the Codex adapter. Noted as a follow-on prior-art row but does not change this spec.
  - `roo-code/roo-cline` (Cline-fork) speaks JSON-RPC over IPC like Cursor — same protocol shape; the future Cursor adapter informs Cline too. Tracked as a Phase-X follow-up.

Reviewer verdict: A target met; A+ stretch hinges on (a) the Aider integration test gating on a real subprocess in CI (build-tag gated) and (b) the cost-governor double-billing assertion landing as a runtime gate.

## 12. Followups (file inline at PR merge)

Per `feedback_unaddressed_load_bearing`:

- `byoa: cursor headless adapter (P3.8 third-consumer proof)` — reopen-trigger persona-B inbound naming Cursor.
- `byoa: codex CLI adapter` — reopen-trigger OpenAI stable headless flag + ToS clearance.
- `byoa: cody / continue / local-LLM runner adapter(s)` — reopen-trigger named inbound.
- `byoa: aider prompt-file crash cleanup hardening` — Low-tier reviewer finding §11; deterministic cleanup test.
- `byoa: per-adapter cost-cap calibration table` — reopen-trigger first non-Claude operator hits a cost-cap surprise.
- `byoa: shared OpenAI-stream-json parser (codex + llm cli)` — reopen-trigger second OpenAI-shaped adapter lands.

## 13. Test scaffold (15+ named tests)

Contract tests live in `internal/orchestrator/spawner/contract_test.go` and run against every registered adapter (stub + claude + aider). Adapter-specific tests live in each impl's `adapter_test.go`. Integration tests are build-tag gated.

```go
// TestRegistry_RegisterDuplicateKindPanics asserts a second Register call for
// the same kind panics with the expected message.
func TestRegistry_RegisterDuplicateKindPanics(t *testing.T)

// TestRegistry_OpenUnknownKindLists asserts Open errors include the registered
// kind set when kind is unknown.
func TestRegistry_OpenUnknownKindLists(t *testing.T)

// TestRegistry_ListKindsSorted asserts ListKinds returns sorted kind names.
func TestRegistry_ListKindsSorted(t *testing.T)

// TestContract_SpawnReturnsNonZeroPID asserts every adapter returns a non-zero
// PID on success (stub negative, claude+aider positive).
func TestContract_SpawnReturnsNonZeroPID(t *testing.T)

// TestContract_SpawnEmitsOperatorInvocationSpan asserts every adapter opens
// one `operator_invocation` span with agent.id + work_item.id + lane attrs.
func TestContract_SpawnEmitsOperatorInvocationSpan(t *testing.T)

// TestContract_ParseStreamProjectsResultEvent asserts every adapter's stream
// parser projects at least one StreamResultEvent on a captured fixture.
func TestContract_ParseStreamProjectsResultEvent(t *testing.T)

// TestContract_OnResultEventForFiresPerTurn asserts the cost-governor callback
// fires once per assistant turn for every adapter.
func TestContract_OnResultEventForFiresPerTurn(t *testing.T)

// TestContract_ContextCancellationTerminatesChild asserts every adapter's
// Spawn child exits within 100 ms of ctx cancellation.
func TestContract_ContextCancellationTerminatesChild(t *testing.T)

// TestContract_KillAgentForgetsChild asserts Killer impls drop the child from
// the children map regardless of process state.
func TestContract_KillAgentForgetsChild(t *testing.T)

// TestClaude_ByteEqualToBaseline migrates the pre-move claude_genai_test golden
// fixture verbatim; asserts ParseStream output matches.
func TestClaude_ByteEqualToBaseline(t *testing.T)

// TestAider_StreamParserProjectsCostUSD asserts the Aider parser stamps
// cost_usd from the `cost` JSONL field onto the projected StreamResultEvent.
func TestAider_StreamParserProjectsCostUSD(t *testing.T)

// TestAider_VersionFloorRejected asserts New() errors with the upgrade hint
// when `aider --version` reports < 0.86.
func TestAider_VersionFloorRejected(t *testing.T)

// TestAider_InfoFrameSkipped asserts the parser skips frames missing role.
func TestAider_InfoFrameSkipped(t *testing.T)

// TestAider_NonZeroExitWithPartialResultIsSuccess asserts a rate-limit exit
// after a partial assistant frame returns Result, nil (operator sees the
// partial).
func TestAider_NonZeroExitWithPartialResultIsSuccess(t *testing.T)

// TestAider_PromptFileCleanedOnCrash asserts the worktree prompt file is
// removed even if the subprocess exits with signal-kill.
func TestAider_PromptFileCleanedOnCrash(t *testing.T)

// TestSpawnerCmdList_PrintsRegisteredKinds asserts `regatta spawner list`
// exit 0 + prints stub|claude|aider on a registered build.
func TestSpawnerCmdList_PrintsRegisteredKinds(t *testing.T)

// TestSpawnerCmdTest_AiderHappyPath asserts `regatta spawner test aider`
// exits 0 against a stub-subprocess that emits the fixture stream.
func TestSpawnerCmdTest_AiderHappyPath(t *testing.T)

// TestSpawnerCmdTest_AiderMissingBinaryRecoverableError asserts exit 1 with a
// one-line recovery hint when the aider binary is not on PATH.
func TestSpawnerCmdTest_AiderMissingBinaryRecoverableError(t *testing.T)

// TestE2E_WireSpawnerOpensRegisteredKind asserts cmd/regatta/wire_spawner.go
// drives spawner.Open against the cfg.Spawner.Kind without falling through
// to a `switch` (regression guard).
func TestE2E_WireSpawnerOpensRegisteredKind(t *testing.T)
```

Test count: **19** (item ask: ≥15). Mix: 3 registry; 6 cross-adapter contract; 1 Claude byte-equal; 5 Aider-specific; 3 subcommand; 1 E2E. Integration tests under `//go:build integration` re-run the contract suite against real `claude` + real `aider` binaries; CI runs them once per PR.

## 14. Comment sweep

`clean` target. Every exported godoc in `internal/orchestrator/spawner/{iface,registry,process}.go` + `internal/orchestrator/spawner/{claude,aider}/` is one line, WHY-form, starts with the symbol name. `golangci-lint run` after sweep per `feedback_comments_lint_reconcile`. Implementer scorecard rubric requires sweep-clean before merge.

## 15. Memory cites

- `feedback_research_design_principles` — proven OSS > build; Aider as the second-consumer-proof.
- `feedback_decision_priority` — UX (persona-B/C unlock) > performance (no Claude regression) > best-practices (`sql.Register`-style adapter) > velocity (canary migration only in this PR).
- `feedback_adversarial_review` — §11 reviewer findings folded inline.
- `feedback_unaddressed_load_bearing` — §12 pre-filed follow-ups for deferred surfaces.
- `feedback_comments_discipline` + `feedback_comments_lint_reconcile` — §14 sweep gate.
- `feedback_pr_body_hygiene` — `--body-file` + `release-notes` fence at PR submit.
- `feedback_no_signatures` — no AI footer anywhere in spec, commit, or PR body.
- `feedback_review_proportional` — caller migration (`wire_spawner.go`) is one-line, sized for proportional review.
- `feedback_default_simpler` — Option B/C rejected as over-/under-engineered relative to Option A's P3.8 parity.
