# Wedge: plan-as-code (`.regatta/plans/*.yaml`)

Prospective. Not on the milestone path. See
[`README.md`](./README.md).

## Thesis

The planner generates DAGs at runtime from a natural-language
brief. Plan-as-code lets authors instead declare DAGs in
`.regatta/plans/*.yaml` -- versioned, PR-reviewable, diff-able.
The runtime planner becomes one of two upstreams; both emit the
same artifact; one executor consumes it.

Maps to **Trap Catalog P3** (trusted instructions from `main`
only) and **P10** (signed prompt artifacts).

### Defensibility under Dynamic Workflows

Honest weakness call. Claude Code Dynamic Workflows
(2026-05-28) already produce inspectable, rerunnable JS plans.
The "Buildkite-style artifact-capture" pattern below is no longer
the differentiator. What remains defensible is **PR-reviewability
with typed inputs, CUE validation, signed plans for protected
lanes, and same-origin enforcement** -- primitives that live
outside the agent runtime and survive a model swap. Treat this
wedge as the lowest-priority of the five; its trigger metric must
fire from operator pull, not from defensive posture.

## Prior art

| System | Schema | Templating | Dep model | Reusability primitive |
|---|---|---|---|---|
| [GitHub Actions](https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions) | Ad-hoc YAML | `${{ expr }}` with typed contexts | Explicit `needs: [job_id]` | [Reusable workflows](https://docs.github.com/en/actions/using-workflows/reusing-workflows) with typed `inputs` / `outputs` / `secrets` |
| [GitLab CI](https://docs.gitlab.com/ee/ci/yaml/) | Ad-hoc YAML | `$VAR` + `rules:` | `stages` (implicit) + `needs:` (DAG) | `extends:`, `include:`, anchors |
| [CircleCI](https://circleci.com/docs/reusing-config/) | Versioned YAML, shape-checked | `<< parameters.x >>` evaluated at *compile time* | Workflow `requires: [job]` | **Orbs** — versioned, namespaced bundles |
| [Argo Workflows](https://argo-workflows.readthedocs.io/en/latest/workflow-templates/) | Kubernetes CRD (OpenAPI schema) | `{{inputs.parameters.x}}` | `dag.tasks[].dependencies` | `WorkflowTemplate` + `ClusterWorkflowTemplate` |
| [Tekton](https://tekton.dev/docs/pipelines/pipelines/) | Kubernetes CRD | `$(tasks.<name>.results.<r>)` | **Implicit-by-result reference** — the edge is created by referencing a result | `Task` / `Pipeline` CRDs + Tekton Hub |
| [Dagster](https://docs.dagster.io/concepts/assets/software-defined-assets) | Python decorators | n/a | Implicit by function-argument names | Python imports + groups |
| [Prefect](https://docs.prefect.io/v3/deploy/infrastructure-concepts/prefect-yaml) | Flows + `prefect.yaml` | Jinja in YAML | Implicit via Python | Per-deployment overrides |
| [Buildkite dynamic pipelines](https://buildkite.com/docs/pipelines/configure/dynamic-pipelines) | YAML uploaded at runtime | `$VAR` upload-time, `$$VAR` runtime | `depends_on:` | Pipeline templates + dynamic upload |
| [CUE](https://cuelang.org/docs/concept/the-logic-of-cue/) | Typed config language | Native | Encoded | Content-addressed imports; schema-and-emit dual role |

## Patterns worth stealing

1. **Compile-time vs. runtime parameters** (CircleCI). Parameters
   resolve *before* the build graph is built, so conditional
   steps produce different DAGs without a separate templating
   phase. The runtime planner becomes a compile step, not a
   separate subsystem.
2. **Reusable workflow contracts** (GHA). Typed `inputs:` /
   `outputs:` / `secrets:` give a *typed I/O contract* a caller
   must satisfy. Each plan declares `inputs:` with types so PR
   review can diff the *interface* separately from the *body*.
3. **Implicit DAG from result references** (Tekton). Referencing
   `$(tasks.A.results.x)` creates the A→B edge automatically; you
   cannot reference a result without declaring the edge.
   Eliminates the stale-`needs:` bug class.
4. **Dynamic upload as first-class** (Buildkite). Artifact-capture
   the generated pipeline before upload so every dynamic build
   leaves an auditable YAML record.
5. **Cluster vs. namespaced templates** (Argo). Project-local
   `.regatta/plans/*.yaml` plus user-global
   `~/.regatta/plans/*.yaml`, with project taking precedence.
6. **CUE schema that types AND emits.** Regatta already ships
   `contracts/schemas/regatta.v1.cue`. The same CUE can validate
   user YAML and emit canonical JSON.
7. **YAML anchors gated to opt-in zones** (GitLab). Forbid `&` /
   `*` in `.regatta/plans/*.yaml`; require named, importable
   fragments to keep diffs readable across `include:` boundaries.
8. **Outputs as the only inter-step protocol** (Tekton +
   Dagster). Both refuse "talk by side effect." Regatta's
   `acceptance_criteria[].citation` is the existing
   inter-WorkItem channel; make it the *only* legal one.

## Runtime planner vs. declarative YAML

| Dimension | Runtime planner (current) | Declarative `.regatta/plans/*.yaml` |
|---|---|---|
| Wins when | Brief is novel; shape unknown; exploration | Workflow is recurring; team wants PR review |
| Reviewability | Post-hoc (need to run to see DAG) | Git diff before execution |
| Drift risk | Plan ≠ last plan (noise) | Plan == file (stable) |
| Latency | LLM call to plan | Zero — parsed in milliseconds |
| Cost | Tokens per run | Tokens only on plan authoring |
| Expressiveness | Anything the LLM can imagine | Bounded by schema |
| Determinism | Low without seed pinning | High |

**Hybrid (recommended).** Two phases: (a) the planner *emits*
plan YAML (Buildkite pattern); (b) the executor *only* consumes
plan YAML. Authors and planner write the same artifact through
one execution path. Add a `plan_source: {kind: "authored" |
"generated", brief_sha?: …}` envelope so audit can tell them
apart. Pulumi's ["agentic infrastructure"
framing](https://www.pulumi.com/blog/the-agentic-infrastructure-era/) --
"every action previewed, policy-checked, reviewed, audited" --
captures the value prop better than the Buildkite framing does.

## Proposed minimal schema

The existing `work_item.schema.json` already carries `id`,
`dependencies: [id]`, `acceptance_criteria`, `source`, and `lane`.
The plan file is a typed container of WorkItems plus inputs.

```yaml
# .regatta/plans/migrate-postgres.yaml
apiVersion: regatta.dev/v1
kind: Plan
metadata:
  id: migrate-postgres
  description: "Move users table to Postgres"
inputs:                                  # GHA-style typed inputs
  target_env: { type: string, required: true, enum: [staging, prod] }
  dry_run:    { type: boolean, default: true }
items:                                   # array of WorkItem (existing schema)
  - id: schema
    title: "Author schema migration"
    acceptance_criteria:
      - { id: ac1, text: "tests pass", state: planned }
    status: planned
    source: { kind: file, locator: "rfc/pg.md:1-40", sha: HEAD }
  - id: rollout
    dependencies: [schema]               # implicit-by-ID — matches WorkItem.dependencies
    title: "Run migration in ${{ inputs.target_env }}"
```

JSON Schema sketch (five top-level fields, all required):

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://github.com/regatta/schemas/plan.schema.json",
  "type": "object",
  "required": ["apiVersion", "kind", "metadata", "items"],
  "additionalProperties": false,
  "properties": {
    "apiVersion": { "const": "regatta.dev/v1" },
    "kind":       { "const": "Plan" },
    "metadata":   { "type": "object", "required": ["id"],
                    "properties": { "id": {"type": "string"},
                                    "description": {"type": "string"} } },
    "inputs":     { "type": "object", "additionalProperties":
                    { "type": "object", "required": ["type"],
                      "properties": { "type": {"enum": ["string","boolean","number","enum"]},
                                      "required": {"type":"boolean"},
                                      "default": {}, "enum": {"type":"array"} } } },
    "items":      { "type": "array", "minItems": 1,
                    "items": { "$ref": "work_item.schema.json" } }
  }
}
```

Templating stays minimal -- `${{ inputs.x }}` only, expanded
before the DAG is built (CircleCI's compile-time pattern). No
arbitrary code, no env access, no file inclusion.

## Security model

- **No code in plans.** String-only substitution into declared
  `${{ inputs.x }}` slots (Tekton model). No Jinja `{% for %}`,
  no `eval`, no `${env:…}` injection.
- **Bounded expression sub-language.** Like GHA's
  `${{ }}` — finite context list (`inputs`, `items`, `vars`) and
  nothing else.
- **CUE-validated up front.** Reject plan files that do not
  unify with `regatta.v1.cue` *before* any planner or executor
  sees them.
- **Same-origin actions only.** Reusable workflows are required
  to live in the same repo by default (GitHub policy is the
  precedent). `WorkItem.source.kind = file` defaults to same-repo
  SHA; cross-repo file pointers require an allowlist.
- **Plan ≠ execution.** Parsing the YAML yields a *data
  structure*, never code execution. Tools and agents the items
  invoke must be allowlisted in repo config, not inlined in the
  plan.
- **Signed plans for `kind: program`.** `WorkItem.source.sha`
  already exists; extend `Plan.metadata.signature` and require
  it for items that touch protected lanes.

## Migration: runtime → reusable YAML

1. **Capture.** Every runtime planner run writes
   `.regatta/runs/<run_id>/plan.yaml` (Buildkite
   artifact-capture pattern).
2. **Promote.** `regatta plan promote <run_id> --as <name>`
   copies `.regatta/runs/<id>/plan.yaml` →
   `.regatta/plans/<name>.yaml`, strips run-specific IDs, and
   prompts for `inputs:` extraction (heuristic: any literal that
   varies across two runs becomes an input — the Dagster
   "parameterise after the fact" pattern).
3. **Diff.** `regatta plan diff <name>` shows drift between
   the latest generated plan and the authored canonical plan.
   Catches planner regressions.
4. **Re-execute.** `regatta run --plan <name> --inputs
   target_env=prod` runs the authored plan with no LLM call.

## Regatta extension points

- Extend the `SpecAdapter` interface with `Mutate(ctx, patch)`
  (non-breaking addition; existing adapters return
  `ErrNotSupported`).
- Custom adapter implementation runs the YAML-as-code DSL; gates
  verify DSL syntax via the existing L0 signature mechanism
  before the planner sees it.
- `contracts/schemas/regatta.v1.cue` becomes the canonical
  validator for plan files.

## Trigger metric (when to adopt)

- First customer running the same brief ≥3 times in a week.
- OR first request to PR-review a regatta workflow without
  executing it.
- OR first detected planner-output drift between two runs of the
  identical brief that the operator could not explain.

## Grade rubric

| Tier | Criterion |
|---|---|
| **B** | Schema implemented; YAML parses into existing WorkItem array; `${{ inputs.x }}` substitution; runtime planner emits the same envelope. |
| **A** | All B + CUE validator wired + `regatta plan diff` command + signed `kind: program` plans + same-origin enforcement. |
| **A+** | All A + `regatta plan promote` round-trip + reusable plan imports + zero code-execution paths from a hostile PR (fuzz-tested with malicious YAML corpus). |

## References to existing repo state

- `contracts/schemas/work_item.schema.json` — `dependencies`
  edge model the plan envelope re-uses unchanged.
- `contracts/schemas/regatta.v1.cue` — candidate canonical
  validator.
- `contracts/schemas/program_brief.schema.json` — upstream of the
  planner; the round-tripped output of the planner becomes a
  `Plan` envelope.
- `docs/incidents.md` P3 and P10.
