# gates/security — hybrid security custom gate (MVP-3)

Status: **Draft design, pre-implementation.** Schema + this README
are the contract; the binary follows.

This is the only new gate kind the mission layer introduces. See
[`docs/design.md` §Missions §Security custom gate](../../docs/design.md).
A repo opts in by adding it to the `gates:` list in `regatta.yaml`
the same way any other custom gate is added.

## Why a security gate

Regatta's value is incident-pattern coverage (Trap Catalog P1–P13),
not OWASP-generic SAST. Commercial AI code-review tools
(CodeRabbit, Greptile, Snyk) already cover OWASP; their AI judge
models are opaque, so we cannot satisfy P13 (judge-LLM lineage
isolation) if we delegate the AI phase to them.

This gate composes battle-tested OSS static tools as the
deterministic floor (P1: deterministic gate before AI gate on
destructive ops) with an in-house AI threat-modeler whose prompt
is pinned to the Trap Catalog.

## Hybrid shape

```
PR diff
   │
   ▼ Deterministic floor (no LLM, short-circuits on any failure)
   ├── gitleaks    ── secrets / API keys
   ├── osv-scanner ── known CVEs in changed dependencies
   ├── semgrep     ── taint / injection / OWASP rule packs (optional)
   ├── syft        ── SBOM diff (optional, for supply-chain surface)
   │
   ▼ If floor passes: AI phase (Opus-class, cross-family from worker)
   └── Threat-model subagent ── ≥3 attack hypotheses per feature,
                                each pinned to a Trap Catalog P-pattern,
                                each carrying a citation (file=… | test=… | commit=…)
   │
   ▼
GateResult (existing schemas/gate_result.schema.json)
   ├── gate_kind: ai_adversarial   (already valid)
   ├── findings[]: severity + Trap Catalog pattern + remediation
   └── signature: same HMAC discipline as every other gate
```

## Config (`regatta.yaml`)

```yaml
gates:
  - id: security
    type: hybrid
    severity_block: ['critical', '2*high']
    determinism_floor:
      gitleaks:     { enabled: true,  version: 8.21.0 }
      osv_scanner:  { enabled: true,  version: 1.9.0 }
      semgrep:      { enabled: false, config: auto }
      syft:         { enabled: false }
    ai:
      model: claude-opus-4-7
      mode: adversarial
      min_falsifications: 3
      lineage_isolation: cross_family       # P13: must differ from worker binding
      trap_pattern_floor: ['P1','P2','P3','P4','P6','P7','P9','P10']
```

The `lineage_isolation: cross_family` constraint is checked at
config load by `regatta validate-config`; if the configured worker
binding's model family equals the security gate's AI model family,
the config is rejected (no `--accept-degraded-lineage` for this
gate at v1).

## Determinism floor — invocation contract

Each tool is shelled out as a single binary, version-pinned via
`safety.tool_versions` (mission-layer addition to `regatta.yaml`,
TBD), JSON output parsed into the existing `GateResult.findings[]`
shape. **No wrapper library** (Trivy, etc.) — keep per-tool version
pinning and per-tool failure attribution.

| Tool | Pin | JSON output | Trap pattern mapping |
|---|---|---|---|
| gitleaks    | `safety.tool_versions.gitleaks`    | `--report-format=json --report-path=-` | P4 (credentials) |
| osv-scanner | `safety.tool_versions.osv_scanner` | `--format=json`                        | P11 (supply chain) |
| semgrep     | `safety.tool_versions.semgrep`     | `--json`                               | P7 / P6 (taint / injection) |
| syft        | `safety.tool_versions.syft`        | `-o syft-json`                         | P11 (SBOM surface) |

Each finding emitted by a floor tool gets a `trap_pattern` field
on the resulting `findings[]` entry (existing `gate_result.schema.json`
property — additive, no schema change).

## AI phase — prompt contract

The AI phase uses a SHA-pinned prompt template at
`prompts/security_gate.md` (TBD). The template MUST:

1. Open with the Trap Catalog summary (load-bearing context).
2. Constrain output to the existing `gate_result.schema.json`
   shape (server-enforced JSON Schema when the provider supports
   it; JSON-mode + Go-side strict-decode + bounded retry otherwise).
3. Require ≥3 falsification attempts per feature, each tagged with
   `mutation_kind` (matches `handoff.schema.json`'s falsification
   enum: `null | empty | boundary | race | inject | overflow |
   auth_bypass | path_traversal | ssrf | deserialization |
   regex_dos | supply_chain | other`).
4. Refuse to emit a `pass` verdict if fewer than 3 falsifications
   with `outcome != "inconclusive"` are produced — schema-enforced
   `minItems: 3` + Go-side post-validation lint.
5. Forbid the AI from reading worker chain-of-thought, session
   state, or sibling validator output (P9 context segregation).
6. NEVER suggest changes; only emit findings. Fixes are
   orchestrator-driven via `Decision{Action: Iterate, FixFeatures}`
   from `internal/missions/route.go`.

## What this gate is NOT

- **Not a parallel validator subsystem.** It's one row in the
  existing `gates:` list. Mission PRs flow through L0–L5 plus this
  gate plus L6 like any other PR.
- **Not a replacement for L4 adversarial.** L4 stays. This gate
  has narrower scope (security-only) but deeper falsification
  discipline (per-finding Trap Catalog pin).
- **Not a CodeRabbit/Greptile/Snyk integration.** Their tools yes
  (deterministic floor), their AI no (P13 surface + Trap Catalog
  fit).
- **Not browser-based.** User-flow validation is deferred entirely
  until a customer asks; if so, a separate `gates/user_testing/`
  ships with Playwright CLI (Microsoft's 2026 recommendation,
  4× fewer tokens than MCP for coding agents).

## Testdata layout (when implementation begins)

```
gates/security/
├── README.md                                 (this file)
└── testdata/
    ├── pass/                                 (clean diffs, gate passes)
    ├── fail_floor_gitleaks/                  (each floor tool's catch path)
    ├── fail_floor_osv/
    ├── fail_ai_p3_prompt_injection/          (AI catches a P-pattern)
    ├── fail_ai_p7_taint/
    ├── fail_ai_insufficient_falsifications/  (AI tries to bypass min_falsifications)
    └── edge/                                 (long diffs, mixed signals, refusal coercion)
```

Each testdata case ships with: input diff, expected `GateResult`
JSON, expected `findings[]` digest. Same shape as
`gates/l0/testdata/` — keep one fixture-corpus discipline.

## Stop condition (mission-layer §Stop conditions)

If this gate's net-helpfulness improvement on the canary corpus
does not show ≥5 pp gain over no-security-gate baseline at MVP-3,
the gate is documented as not-yet-useful and the mission layer is
re-evaluated per the §Stop conditions kill switch.
