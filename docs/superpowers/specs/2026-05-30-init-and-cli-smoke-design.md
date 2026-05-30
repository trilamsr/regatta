# `regatta init` + cmd/regatta black-box smoke tests

- **Status:** draft
- **Created:** 2026-05-30
- **Closes:** #12, #49
- **Author:** Tri Lam
- **Reviewers:** 3 parallel subagents (adversarial, operator-UX, test-design) — findings folded in

## Goal

Ship two changes in one PR:

1. **`regatta init`** — a single command that scaffolds a new repo and demonstrates regatta's value in under a second, with zero copy-paste required.
2. **`cmd/regatta/cli_smoke_test.go`** — black-box tests covering every CLI subcommand (currently 0% coverage on the binary's entry point).

The combined PR exists because the smoke tests need to cover `init`, and `init` is the activation moment that makes the smoke coverage worth building first.

## Non-goals

- AI gate implementation (#33) — `init` writes a config with L0 enabled, future gates as inert reference.
- Adapter implementations beyond `markdown_catalog` (#35).
- Schema-level JSON validation in smoke tests — `contracts/schemas/` unit tests already own that.
- Production AgentSpawner (#14) — `serve --tick-once --spawner=stub` is what smoke exercises.

## Design

### `regatta init` — one command, value in seconds

**Happy path:**

```
$ regatta init

Setting up regatta in current directory...

  ✓ wrote regatta.yaml         (your config — L0 gate enabled)
  ✓ wrote .regatta/sample.diff (a demo attack — Cyrillic letter that
                                looks identical to Latin A)

Running L0 gate against the demo to show you what regatta catches:

  FAIL — homoglyph attack

  In sample.diff line 3, the Latin letter "A" was replaced with the
  Cyrillic letter "А". The two are visually identical but represent
  different characters. An attacker could use this to silently rewrite
  spec criteria, API names, or domain names in a PR that looks clean
  to human reviewers.

  This is pattern P10 from the Regatta Trap Catalog. Real-world
  example: see docs/incidents.md#p10.

Next steps:
  • Edit regatta.yaml to enable more gates
  • Run `regatta l0 <your-diff>` on real PRs
  • Try `regatta verify-repo-config` to audit your repo's settings

Done in 0.8s.
```

**Files written:**

- `./regatta.yaml` — byte-equal embed of `examples/minimal/regatta.yaml` (single source of truth; drift caught by test)
- `./.regatta/sample.diff` — byte-equal embed of `testdata/gates/l0/fail/17_homoglyph_cyrillic_a.diff` (single source of truth; drift caught by test)

**Re-run flow (friendly errors):**

```
$ regatta init    # second run, no --force
regatta.yaml already exists in this directory.

To re-initialize, either delete it first:
  rm regatta.yaml .regatta/sample.diff

Or force overwrite:
  regatta init --force
```

**Flags:**

- `--force` — overwrite existing files
- `--json` — emit GateResult JSON instead of friendly prose (CI / scripting)

**In-process L0:**

The demo runs by calling `internal/gates/l0.Run()` directly, not by exec'ing the binary. Simpler, faster, no PATH issues, no double error stream.

### Smoke tests — cmd/regatta/cli_smoke_test.go

**Pattern:**

- `TestMain` resolves module root via `runtime.Caller`, runs `go build -buildvcs=false -o <tmp>/regatta ./cmd/regatta` once. Skip suite (not fail) if `go` not on PATH.
- Per-subcommand struct: `{name, helpArgs, okArgs, failArgs (optional)}`.
- Asserts:
  - help → exit 0, stderr contains "Usage:"
  - ok → exit 0
  - fail → exit != 0 && stderr != "" (do NOT assert exit == 1; the binary uses 2 for usage errors, 1 for runtime fails — mix is intentional)
- All subtests `t.Parallel()`-safe: own `t.TempDir()`; env via `cmd.Env`, not `t.Setenv`.

**Coverage matrix:**

| Subcommand | Help | Happy path | Fail path |
|------------|------|------------|-----------|
| `version` | ✓ | — (no fail) | — |
| `l0` | ✓ | `testdata/gates/l0/pass/*.diff` | `testdata/gates/l0/fail/*.diff` |
| `l0-refs` | ✓ | live git repo | bogus ref |
| `l0-merge` | ✓ | merge-commit fixture | bogus SHA |
| `validate-config` | ✓ | `examples/minimal/regatta.yaml` | malformed inline |
| `verify-repo-config` | ✓ | — (needs GITHUB_TOKEN; skip) | missing-flag |
| `serve --tick-once --spawner=stub` | ✓ | empty items dir | `--spawner=bogus` |
| `program` (bare) | ✓ | — | — |
| `program verify-handoff` | ✓ | fixture | bad-signature fixture |
| `init` | ✓ | empty tmpdir | re-run without --force |
| (bare `regatta`) | — | — | exit 2 + Usage |
| (unknown sub) | — | — | exit 2 + "unknown subcommand" |

**Out of scope** (covered by existing tests):

- `serve` with real claude spawner — `serve_claude_test.go` owns it
- `program plan` happy path — needs `ANTHROPIC_API_KEY`; existing test owns it
- JSON schema validation — `contracts/schemas/gate_result_test.go` owns it

### Docs updated same PR

- `docs/operator/quickstart.md` — rewrite around new init UX (current text predates init; lies about behavior)
- `docs/operator/day1.md` — same

## Components

```
cmd/regatta/
  init.go               (new) — runInit(args) int; embed.FS for assets
  init_test.go          (new) — unit tests + embed-drift tests
  init_assets/          (new) — embedded copies committed to git
    regatta.yaml        (byte-identical to examples/minimal/regatta.yaml)
    sample.diff         (byte-identical to testdata/gates/l0/fail/17_homoglyph_cyrillic_a.diff)
  cli_smoke_test.go     (new) — black-box subcommand coverage
  testdata/             (new dir) — minimal CLI fixtures (malformed config etc.)
  main.go               (edit) — wire `case "init"` + usage()
docs/operator/
  quickstart.md         (edit) — reflect actual init behavior
  day1.md               (edit) — same
```

**Why `init_assets/` not just `testdata/` references:** `//go:embed` is package-relative. Reaching across the module to `examples/minimal/regatta.yaml` or `testdata/gates/l0/fail/...` requires either (a) symlinks (don't survive Windows / git clone on some platforms), (b) `init_assets/` as a separate committed copy with a drift-detection test enforcing byte-equality, or (c) build-time codegen. (b) is the simplest durable answer. The drift test is the contract: change the canonical file, the test fails, the contributor updates `init_assets/` in the same commit.

## Data flow

```
operator: regatta init
  ↓
runInit:
  1. Lstat .regatta/ — symlink/non-dir → exit 2 with friendly error
  2. OpenFile(O_CREATE|O_EXCL) on regatta.yaml + .regatta/sample.diff
     - exists + bytes match embedded + !force → silent idempotent skip
     - exists + bytes diverge + !force → exit 2 with friendly error
     - --force → unconditional overwrite
  3. Write both files from embed.FS
  4. Call internal/gates/l0.Run() against the just-written sample.diff
  5. Format result:
     - default: friendly prose (file: line: explanation: incident-link)
     - --json: GateResult JSON to stdout
  6. Exit 0 (init succeeded; demo verdict is informational, not exit-coded)
```

## Error handling

| Condition | Stream | Exit | Message |
|-----------|--------|------|---------|
| regatta.yaml exists (no --force) | stderr | 2 | "regatta.yaml already exists... rm regatta.yaml...or regatta init --force" |
| sample.diff exists, bytes diverge | stderr | 2 | similar with --force hint |
| .regatta/ is a symlink or non-dir | stderr | 2 | "refusing: .regatta/ is not a regular directory" |
| Write fails (disk full, perm) | stderr | 1 | wrapped fs error |
| Demo L0 run errors (corpus broken) | stderr | 1 | "internal: embedded demo failed: <err>; please file a bug" |

## Testing

### `init` unit tests (init_test.go)

- `TestInit_WritesBothFiles` — empty tmpdir, run, assert both files exist with embedded content
- `TestInit_FriendlyOutput` — assert stdout contains "wrote regatta.yaml", "homoglyph", "P10", "Next steps"
- `TestInit_JSONOutput` — --json suppresses prose, emits parseable GateResult
- `TestInit_RefusesExistingYaml` — pre-write regatta.yaml, re-run → exit 2, stderr has --force hint
- `TestInit_IdempotentSampleDiff` — pre-write byte-matching sample.diff, run → silent skip (exit 0)
- `TestInit_DivergedSampleDiff` — pre-write differing sample.diff, run → exit 2
- `TestInit_ForceOverwrites` — pre-write both, --force → both overwritten
- `TestInit_RefusesSymlinkRegatta` — pre-create .regatta as symlink, run → exit 2
- `TestEmbeddedYamlMatchesExample` — bytes equal `examples/minimal/regatta.yaml` (drift gate)
- `TestEmbeddedSampleMatchesFixture` — bytes equal `testdata/gates/l0/fail/17_homoglyph_cyrillic_a.diff` (drift gate)

### CLI smoke tests (cli_smoke_test.go)

- One subtest per coverage-matrix row above
- Per-subtest: `t.Parallel()`, own `t.TempDir()`, env via `cmd.Env`
- Assertion helpers: `expectExitZero(t, cmd)`, `expectUsageError(t, cmd)`, `expectStderrContains(t, cmd, substr)`
- Subtest names describe user-facing behavior: `TestCLI_Init_PrintsFriendlyExplanation`, not `TestCLI_Init_Sub3`
- Failing assertions include actionable hint: "expected stdout to contain X; got Y. If init.go output format changed intentionally, update this test."

## Open questions

(none — design v3 incorporated 3 parallel reviews)

## Risks

1. **Embedded fixture drift** — mitigated by byte-equality tests; failure is loud + fix is local.
2. **Smoke test compile cost** — ~2-4s cold, <1s cached. 27 exec invocations × ~50ms = ~1.5s. Negligible vs current CI runtime.
3. **`--force` foot-gun** — could overwrite operator's edited regatta.yaml. Mitigation: error message names the file; operator who runs --force is taking explicit responsibility.
4. **Friendly prose lock-in** — every word change in the demo output ripples to a test. Mitigation: assertion helpers match on key phrases only (`"homoglyph"`, `"P10"`), not full text.

## Rollback

- Single PR, single feature branch. Revert the merge commit. No data migration, no schema change, no dependency.
- The drift-detection tests will keep `init_assets/` in lockstep with their canonical sources even if `init` is later removed — until those tests are also deleted.
