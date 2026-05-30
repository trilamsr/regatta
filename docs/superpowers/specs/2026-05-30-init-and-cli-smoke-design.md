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

  + wrote regatta.yaml         (your config; L0 gate enabled)
  + wrote .regatta/sample.diff (a demo attack against MILESTONES.md)

Running L0 gate against the demo to show you what regatta catches:

  FAIL: spec criterion text changed without citation (L0-TEXT-0)

  In MILESTONES.md line 2, the criterion "Auth tokens are scoped..."
  was rewritten. L0 blocks this because criteria are the contract
  between you and the agent: silent edits move the goalposts.

  The catch is sneakier than it looks: the diff replaces Latin "A"
  with Cyrillic "А" (U+0410). They render identically. A human
  reviewer scanning the diff sees "Auth -> Auth" and approves; L0
  compares NFC-normalized code points and rejects.

  Trap Pattern P3 (fetch trusted instructions from main, treat all
  other text as data). See docs/incidents.md#pattern-3.

Next steps:
  - Run `regatta l0 <your-diff>` on a real PR diff
  - Run `regatta verify-repo-config` to audit your repo's branch
    protection and CODEOWNERS
  - Edit regatta.yaml to enable more gates as they ship

Done.
```

Output stream discipline: every line above is on **stdout**. Stderr is empty on success. The "Done." sentinel is plain ASCII (no Unicode markers, no timing claim, no color).

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

The demo runs by calling `l0.Check(l0.Default(), l0.ParseUnifiedDiff(data))` directly (see `internal/gates/l0/gate.go:27`), not by exec'ing the binary. Simpler, faster, no PATH issues, no double error stream.

`init` formats the returned `schemas.GateResult` (which contains `Findings[].TrapPattern`, `Findings[].Location`, `Findings[].ID`) into the friendly prose. The pattern blurb is generated, not hardcoded: a `patternBlurb(string) string` lookup table covers P1-P13 from the Trap Catalog (`docs/incidents.md`), with a generic fallback if a new pattern is added without a blurb.

### Smoke tests — cmd/regatta/cli_smoke_test.go

**Pattern:**

- `TestMain` resolves module root via `runtime.Caller` walk-up, runs `go build -buildvcs=false -o <tmp>/regatta ./cmd/regatta` once. Skip suite (not fail) if `go` not on PATH or if not invoked from a tree (out-of-module install).
- Per-subcommand struct: `{name, helpArgs, okArgs, failArgs (optional, nil = skip), helpStream (stdout|stderr), expectUsageRegexp}`.
- Asserts:
  - help → exit 0; matched stream contains regex `(?i)usage`. The "help" semantic differs across subcommands: `regatta help` writes top-level usage to **stdout** (see `main.go usage(os.Stdout)`); subcommand `-h` runs through `flag.ExitOnError` which writes to **stderr** with text `"Usage of <subcommand>:"` (default `flag` format) for subs that do NOT set a custom `fs.Usage` (`l0-refs`, `l0-merge`, `validate-config`, `verify-repo-config`, `serve`), and `"Usage: regatta <sub> ..."` (custom) for those that do (`l0`, `program plan`, `program verify-handoff`). The struct's `helpStream` + `expectUsageRegexp` per row makes this explicit.
  - ok → exit 0
  - fail → exit != 0 && stderr != "" (do NOT assert exit == 1; the binary uses 2 for usage errors, 1 for runtime fails — mix is intentional)
- All subtests `t.Parallel()`-safe: own `t.TempDir()`; env via `cmd.Env`, not `t.Setenv`.
- Failing assertions include actionable hints, e.g. `t.Fatalf("expected stdout to contain 'wrote regatta.yaml'; got %q. If init.go output format changed intentionally, update this test", gotStdout)`.

**Coverage matrix:**

| Subcommand | Help (stream) | Happy path | Fail path |
|------------|------|------------|-----------|
| `regatta help` (bare top-level) | stdout | — | — |
| `regatta` (no args) | — | — | exit 2 + stderr usage |
| `regatta unknownsub` | — | — | exit 2 + "unknown subcommand" |
| `version` | stdout | (no-op) | — (no fail path) |
| `l0` | stderr (custom Usage) | `testdata/gates/l0/pass/00_*.diff` | `testdata/gates/l0/fail/00_*.diff` |
| `l0` stdin path | — | `-` flag reads stdin | — |
| `l0-refs` | stderr (default) | git repo with two refs | bogus ref |
| `l0-merge` | stderr (default) | merge-commit fixture | bogus SHA |
| `validate-config` | stderr (default) | `examples/minimal/regatta.yaml` | malformed inline |
| `verify-repo-config` | stderr (default) | — (needs GITHUB_TOKEN; skip with t.Skip + hint) | missing `-owner` |
| `serve --tick-once --spawner=stub` | stderr (default) | empty items dir + tmpdir db | `--spawner=bogus` |
| `program` (bare) | stderr | — | exit 2 (no sub) |
| `program verify-handoff` | stderr (custom Usage) | signed-handoff fixture | bad-signature fixture |
| `init` | stderr (default) | empty tmpdir | re-run without --force on diverged file |

**Out of scope** (covered by existing tests):

- `serve` with real claude spawner — `serve_claude_test.go` owns it
- `program plan` happy path — needs `ANTHROPIC_API_KEY`; existing test owns it
- JSON schema validation — `contracts/schemas/gate_result_test.go` owns it

### Docs updated same PR

Both files currently describe `regatta init` as writing a skeleton that the operator then hand-edits. New design writes a complete starter config + demos L0 in one shot. Both docs need updating to match reality.

**`docs/operator/quickstart.md` §2 "Scaffold + validate" (lines 24-32):** replace the three-command block (init / $EDITOR / validate-config) with:

```sh
cd ~/code/myproject
regatta init
```

Delete the `$EDITOR regatta.yaml` step (init writes a complete config, not a skeleton). Delete the `regatta validate-config` step (init's demo run is the validation moment for L0; `validate-config` remains available for advanced editing but is not first-touch). Keep the §"Required fields" paragraph and the `examples/full/regatta.yaml` link as the next-step pointer for operators who want to extend beyond L0.

**`docs/operator/day1.md` §"Steps" (lines 14-24):** replace with:

```sh
brew install trilamsr/regatta/regatta   # or `go install ...`
cd ~/code/myproject
regatta init                            # writes config + runs demo
regatta verify-repo-config              # audits branch protection + CODEOWNERS
```

Delete the `$EDITOR`, `validate-config`, and `validate-spec --dry-run` lines. (The `validate-spec` command is tracked under issue #37 and does not exist; the doc currently lies. This PR fixes the lie by removing the line.)

Update §Goal to reflect that init's demo IS the parsed-items + NFC + invisible-glyph cleanliness report (just shown via a canned fixture rather than the operator's real items).

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
operator: regatta init [--force] [--json]
  ↓
runInit:
  1. Resolve .regatta/ state:
     - os.Lstat(".regatta"): if exists and not a regular directory
       (symlink, file, device), exit 2 with friendly error.
     - if absent: os.MkdirAll(".regatta", 0o755).
     - if exists as regular dir (e.g. populated by `serve`): leave
       its contents alone; only write sample.diff inside.
  2. Per-file write decision (applied identically to regatta.yaml
     and .regatta/sample.diff):
     a. If file absent: write via OpenFile(O_CREATE|O_EXCL|O_WRONLY,
        0o644) from embed.FS. Print "+ wrote <path> (<blurb>)".
     b. If file present and bytes match embedded blob (idempotent
        re-run): print "= <path> unchanged". No write.
     c. If file present and bytes diverge and !force: exit 2 with
        friendly error naming the file and the --force escape hatch.
        No partial write of the other file (fail before any write).
     d. If --force: unconditional truncate+write. Print "! overwrote
        <path>".
     The rule is symmetric across both files. Step 2c short-circuits
     before any write so partial-state is impossible on the
     divergence path. (Step 2a + disk-full mid-write can still
     leave one file present and one absent; see Failure modes.)
  3. Run demo: l0.Check(l0.Default(), l0.ParseUnifiedDiff(<bytes
     just written or already on disk>)). Sample.diff is always
     re-read from disk so the verdict reflects what the operator
     sees, not what was embedded.
  4. Format output:
     - default (TTY or no --json): friendly prose generated from
       the GateResult (header, finding ID, location, plain-English
       explanation, pattern blurb via patternBlurb lookup, link to
       docs/incidents.md#pattern-N). Always on stdout.
     - --json: single JSON object on stdout:
         {"written":   ["regatta.yaml",".regatta/sample.diff"],
          "skipped":   [],
          "overwritten":[],
          "gate_result": <full schemas.GateResult>}
       No prose on stderr unless an error occurred.
  5. Exit 0. The demo verdict (FAIL) is informational; `init`'s job
     is scaffolding + showing-not-telling, not gating. Operator can
     re-run `regatta l0 .regatta/sample.diff` and get exit 1 if
     they want the gate exit code.
```

## Error handling

| Condition | Stream | Exit | Message |
|-----------|--------|------|---------|
| regatta.yaml exists, bytes diverge, no --force | stderr | 2 | `regatta.yaml already exists and differs from the bundled template. To re-init: rm regatta.yaml .regatta/sample.diff. To overwrite: regatta init --force.` |
| .regatta/sample.diff exists, bytes diverge, no --force | stderr | 2 | same shape, naming sample.diff |
| .regatta/ is a symlink, regular file, device, or other non-dir | stderr | 2 | `refusing to write: .regatta/ exists but is not a regular directory (got: <mode>). Remove or rename it, then re-run.` |
| .regatta/ exists as dir but EACCES on MkdirAll/write (SELinux, read-only mount) | stderr | 1 | wrapped fs error with cwd context: `regatta init: write .regatta/sample.diff: <wrapped err>. Check filesystem permissions and SELinux context for <cwd>.` |
| Disk full mid-write (regatta.yaml written, sample.diff fails) | stderr | 1 | wrapped fs error. Init does NOT roll back the already-written file; operator gets a clear error naming what was written and what failed. Second invocation with --force completes the job. |
| Demo L0 run errors (parse fail, corpus broken — should be unreachable post-drift-test) | stderr | 1 | `internal: embedded demo failed: <err>; please file a bug at github.com/trilamsr/regatta/issues` |
| init in a sub-directory of a repo that already has regatta.yaml at root | stdout warning, no error | 0 | `note: parent directory <abs-path> already has regatta.yaml. This init writes a separate config in <cwd>. To configure the parent repo, re-run there.` (Init still proceeds in cwd.) |
| init in a non-git directory | (no error) | 0 | Init does not require a git repo; the demo fixture is self-contained. |
| --force + --json combined | (no special handling) | 0 | --force flag is honored; --json envelope reports `"overwritten"` array populated. |
| Bare `regatta init --help` / `regatta init -h` | stdout | 0 | Prints flag usage |

## Testing

### `init` unit tests (init_test.go)

- `TestInit_WritesBothFiles` — empty tmpdir, run, assert both files exist with bytes equal to embedded blobs.
- `TestInit_FriendlyOutput` — assert stdout matches each of: `"wrote regatta.yaml"`, `"L0-TEXT-0"` (finding ID), `"Trap Pattern P3"`, `"docs/incidents.md#pattern-3"`, `"Next steps"`. (Match by substring on key phrases, not full text — friendly prose may evolve.)
- `TestInit_JSONOutput` — `--json` emits a single parseable JSON object on stdout matching: `{written: [_,_], skipped: [], overwritten: [], gate_result: {verdict: "fail", gate_id: "l0_spec_immutability", findings: [{trap_pattern: "P3", ...}]}}`. No prose on stderr.
- `TestInit_PatternBlurbFallback` — call `patternBlurb("P99")` directly; asserts generic fallback string is returned (not panic, not empty).
- `TestInit_ExitsZeroOnDemoFail` — even though the demo verdict is FAIL, `init` itself exits 0 (scaffolding succeeded). Asserts the `init`-is-not-a-gate invariant.
- `TestInit_RefusesDivergedYaml` — pre-write regatta.yaml with non-matching bytes, re-run without --force → exit 2, stderr contains `regatta.yaml`, `--force`.
- `TestInit_IdempotentReRun` — pre-write both files with byte-matching content, re-run → exit 0; stdout contains `"= regatta.yaml unchanged"` and `"= .regatta/sample.diff unchanged"`. No prompt, no error, no silent.
- `TestInit_DivergedSampleDiff` — pre-write differing sample.diff, run → exit 2, stderr names `.regatta/sample.diff` + `--force`.
- `TestInit_FailsAtomicallyOnDivergence` — pre-write a diverged regatta.yaml, run without --force in a tmpdir where sample.diff does NOT exist → exit 2 AND sample.diff still absent (no partial write).
- `TestInit_ForceOverwrites` — pre-write both with differing bytes, --force → both overwritten with embedded bytes; stdout uses `"! overwrote"` markers.
- `TestInit_RefusesSymlinkRegatta` — pre-create `.regatta` as a symlink to a tmpdir, run → exit 2, stderr says `not a regular directory`. Skip on Windows (symlink semantics differ).
- `TestInit_LeavesPopulatedRegattaDirAlone` — pre-create `.regatta/items/foo.md` (simulating `serve` already ran), run init in same dir → init proceeds, writes sample.diff next to existing items, leaves items/foo.md untouched. Asserts coexistence with `serve`.
- `TestInit_SubdirOfRegattadRepo` — parent dir has regatta.yaml; cwd is a sub-dir without one. Run → exit 0, stdout contains the warning note from the error table, both files written in cwd.
- `TestInit_NonGitDir` — run in a tmpdir with no `.git` → exit 0, both files written. (init has no git dependency.)
- `TestInit_HelpFlag` — `regatta init -h` and `regatta init --help` → exit 0, stderr matches `(?i)usage`.
- `TestEmbeddedYamlMatchesExample` — bytes equal `examples/minimal/regatta.yaml`. Drift gate. Resolves module root via `runtime.Caller` walk-up; skips with hint if not found (out-of-tree run).
- `TestEmbeddedSampleMatchesFixture` — bytes equal `testdata/gates/l0/fail/17_homoglyph_cyrillic_a.diff`. Same walk-up + skip pattern.
- `TestInitUsesEmbeddedBytes` — temporarily move `examples/minimal/regatta.yaml` to a side path (or use a test helper that patches `runtime.Caller`), run init, assert the bytes written match the embed.FS blob (not the disk file). Catches the "reads from disk and silently passes drift gate" failure mode.

### CLI smoke tests (cli_smoke_test.go)

- One subtest per coverage-matrix row above
- Per-subtest: `t.Parallel()`, own `t.TempDir()`, env via `cmd.Env`
- Assertion helpers: `expectExitZero(t, cmd)`, `expectUsageError(t, cmd)`, `expectStderrContains(t, cmd, substr)`
- Subtest names describe user-facing behavior: `TestCLI_Init_PrintsFriendlyExplanation`, not `TestCLI_Init_Sub3`
- Failing assertions include actionable hint: "expected stdout to contain X; got Y. If init.go output format changed intentionally, update this test."

## Open questions

1. **Windows glyph fallback.** Output uses ASCII markers `+`/`=`/`!` (no Unicode `✓`). Should also honor `NO_COLOR` / `TERM=dumb` for any future color additions. Confirmed safe for legacy `cmd.exe` cp437.
2. **`docs/operator/quickstart.md` rewrite scope.** Current quickstart §2 (lines 27-32 per reviewer) walks through `regatta init` writing only `regatta.yaml`, then `$EDITOR regatta.yaml`, then `regatta validate-config`. New design: replace §2 with two lines — `cd your-repo` + `regatta init`. Delete the `$EDITOR` + `validate-config` steps (init is self-demonstrating). `day1.md` similar: drop `validate-spec --dry-run` reference (unimplemented).
3. **`patternBlurb` source of truth.** Hardcoded in init.go vs. extracted to a shared `docs/incidents.md` parser. Pick: hardcoded for now (13 patterns, low churn); extract later if a second consumer appears.

## Risks

1. **Embedded fixture drift** — mitigated by `TestEmbeddedYamlMatchesExample` + `TestEmbeddedSampleMatchesFixture` byte-equality tests + `TestInitUsesEmbeddedBytes` consumption test. Failure is loud + fix is local.
2. **L0 trap-pattern drift** — if `internal/gates/l0/gate.go` changes which `TrapPattern` the homoglyph fixture emits, `TestInit_FriendlyOutput` will fail naming the expected vs actual pattern. The fix in that case is updating `patternBlurb` + the friendly prose generator, not the test.
3. **Smoke test compile cost** — ~2-4s cold, <1s cached. ~25 exec invocations × ~50ms ≈ 1.5s. Negligible vs current CI runtime.
4. **`--force` foot-gun** — could overwrite operator's edited regatta.yaml. Mitigation: error message names the file + escape hatch; operator who runs --force is taking explicit responsibility.
5. **Friendly prose lock-in** — every word change in the demo output ripples to a test. Mitigation: assertions match key phrases (`"L0-TEXT-0"`, `"Trap Pattern P3"`, `"Next steps"`), not full text.
6. **File mode portability** — files `0o644`, dirs `0o755`. umask interaction inherited from process — operators with non-default umask get filtered modes; this is conventional Unix behavior and not worth special-casing.
7. **Symlink test on Windows** — `TestInit_RefusesSymlinkRegatta` skips on Windows because symlink creation requires admin / dev-mode. The defense itself still runs on Windows; only the test exercising it is skipped.

## Rollback

- Single PR, single feature branch. Revert the merge commit. No data migration, no schema change, no dependency.
- The drift-detection tests will keep `init_assets/` in lockstep with their canonical sources even if `init` is later removed — until those tests are also deleted.
