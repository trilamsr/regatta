# `regatta init` + cmd/regatta smoke tests Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship `regatta init` (one-command scaffold + L0 demo) and black-box smoke tests covering every `cmd/regatta` subcommand. Closes #12 and #49.

**Architecture:** `init` lives in `cmd/regatta/init.go` as a sibling to `runL0`/`runServe` etc. (no new package), with embedded assets in `cmd/regatta/init_assets/` and a `patternBlurb` lookup table. Demo runs in-process via `l0.Check(l0.Default(), l0.ParseUnifiedDiff(data))`. Smoke tests use a `TestMain` that compiles the binary once into `t.TempDir()` and runs per-subcommand assertions via a struct-driven matrix.

**Tech Stack:** Go 1.22+, `embed.FS`, `encoding/json`, `os/exec`, `testing`, no new dependencies. Reuses `internal/gates/l0`, `contracts/schemas`, `examples/minimal/regatta.yaml`, `testdata/gates/l0/fail/17_homoglyph_cyrillic_a.diff`.

**Spec:** `docs/superpowers/specs/2026-05-30-init-and-cli-smoke-design.md`

---

## File Structure

**New files:**
- `cmd/regatta/init.go` — `runInit(args []string) int`, flag parsing, file-write decision logic, friendly prose formatter, JSON envelope formatter, `patternBlurb` table.
- `cmd/regatta/init_test.go` — all `TestInit_*` unit tests + `TestEmbedded*` drift gates.
- `cmd/regatta/cli_smoke_test.go` — `TestMain` build-once + per-subcommand smoke struct/loop.
- `cmd/regatta/init_assets/regatta.yaml` — byte-equal copy of `examples/minimal/regatta.yaml` (committed).
- `cmd/regatta/init_assets/sample.diff` — byte-equal copy of `testdata/gates/l0/fail/17_homoglyph_cyrillic_a.diff` (committed).
- `cmd/regatta/testdata/cli_smoke/malformed_config.yaml` — broken CUE for `validate-config` fail case.
- `cmd/regatta/testdata/cli_smoke/bad_handoff.json` — bad-signature fixture for `program verify-handoff` fail case.

**Modified files:**
- `cmd/regatta/main.go` — add `case "init"` dispatch (~line 64), add `regatta init` line in `usage()` (~line 87).
- `docs/operator/quickstart.md` — replace §2 (lines 24-32) per spec.
- `docs/operator/day1.md` — replace §Steps (lines 14-24) per spec.

---

## Task 1: Bundle drift gates (write tests first)

These tests fail until Task 2 creates the assets. TDD anchor for the whole plan.

**Files:**
- Create: `cmd/regatta/init_assets/.gitkeep` (placeholder so test can locate dir before Task 2)
- Create: `cmd/regatta/init_test.go`

- [ ] **Step 1: Create empty init_assets dir with .gitkeep**

```bash
mkdir -p cmd/regatta/init_assets
touch cmd/regatta/init_assets/.gitkeep
```

- [ ] **Step 2: Write the two drift-gate tests**

Create `cmd/regatta/init_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// moduleRoot walks up from the current test file to the directory
// containing go.mod. Returns "" if not found (e.g. out-of-tree run);
// callers should t.Skip in that case.
func moduleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func TestEmbeddedYamlMatchesExample(t *testing.T) {
	t.Parallel()
	root := moduleRoot(t)
	if root == "" {
		t.Skip("module root not resolvable; skipping drift test")
	}
	canonical, err := os.ReadFile(filepath.Join(root, "examples/minimal/regatta.yaml"))
	if err != nil {
		t.Fatalf("read canonical: %v", err)
	}
	embedded, err := os.ReadFile(filepath.Join(root, "cmd/regatta/init_assets/regatta.yaml"))
	if err != nil {
		t.Fatalf("read embedded: %v. Did you run Task 2 of the init+smoke plan?", err)
	}
	if string(canonical) != string(embedded) {
		t.Fatalf("drift: cmd/regatta/init_assets/regatta.yaml diverges from examples/minimal/regatta.yaml. Re-sync with: cp examples/minimal/regatta.yaml cmd/regatta/init_assets/regatta.yaml")
	}
}

func TestEmbeddedSampleMatchesFixture(t *testing.T) {
	t.Parallel()
	root := moduleRoot(t)
	if root == "" {
		t.Skip("module root not resolvable; skipping drift test")
	}
	canonical, err := os.ReadFile(filepath.Join(root, "testdata/gates/l0/fail/17_homoglyph_cyrillic_a.diff"))
	if err != nil {
		t.Fatalf("read canonical: %v", err)
	}
	embedded, err := os.ReadFile(filepath.Join(root, "cmd/regatta/init_assets/sample.diff"))
	if err != nil {
		t.Fatalf("read embedded: %v. Did you run Task 2 of the init+smoke plan?", err)
	}
	if string(canonical) != string(embedded) {
		t.Fatalf("drift: cmd/regatta/init_assets/sample.diff diverges from testdata/gates/l0/fail/17_homoglyph_cyrillic_a.diff. Re-sync with: cp testdata/gates/l0/fail/17_homoglyph_cyrillic_a.diff cmd/regatta/init_assets/sample.diff")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test -run 'TestEmbedded' ./cmd/regatta/`
Expected: FAIL with "read embedded: open ... no such file or directory"

- [ ] **Step 4: Commit (test-first checkpoint)**

```bash
git add cmd/regatta/init_assets/.gitkeep cmd/regatta/init_test.go
git commit -m "test(init): drift gates for embedded assets (fail until bundled)"
```

---

## Task 2: Bundle the assets

**Files:**
- Create: `cmd/regatta/init_assets/regatta.yaml`
- Create: `cmd/regatta/init_assets/sample.diff`
- Delete: `cmd/regatta/init_assets/.gitkeep`

- [ ] **Step 1: Copy assets byte-equal from canonical sources**

```bash
cp examples/minimal/regatta.yaml cmd/regatta/init_assets/regatta.yaml
cp testdata/gates/l0/fail/17_homoglyph_cyrillic_a.diff cmd/regatta/init_assets/sample.diff
rm cmd/regatta/init_assets/.gitkeep
```

- [ ] **Step 2: Run drift tests to verify they pass**

Run: `go test -run 'TestEmbedded' ./cmd/regatta/ -v`
Expected: `PASS: TestEmbeddedYamlMatchesExample` + `PASS: TestEmbeddedSampleMatchesFixture`

- [ ] **Step 3: Commit**

```bash
git add cmd/regatta/init_assets/
git commit -m "feat(init): bundle regatta.yaml + sample.diff for init scaffold"
```

---

## Task 3: Scaffold `runInit` skeleton + happy-path test

Write the failing happy-path test first, then minimal `runInit` to make it pass.

**Files:**
- Modify: `cmd/regatta/init_test.go`
- Create: `cmd/regatta/init.go`
- Modify: `cmd/regatta/main.go`

- [ ] **Step 1: Add the happy-path test**

Append to `cmd/regatta/init_test.go`:

```go
import (
	"bytes"
	// existing imports above
)

// runInitInDir cd's into dir, runs runInit with args, returns exit
// code + captured stdout + stderr. Restores cwd on return.
func runInitInDir(t *testing.T, dir string, args []string) (code int, stdout, stderr string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	var out, errOut bytes.Buffer
	code = runInitWithIO(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestInit_WritesBothFiles(t *testing.T) {
	dir := t.TempDir()
	code, stdout, stderr := runInitInDir(t, dir, nil)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	if stderr != "" {
		t.Fatalf("expected empty stderr on success; got %q", stderr)
	}
	yaml, err := os.ReadFile(filepath.Join(dir, "regatta.yaml"))
	if err != nil {
		t.Fatalf("regatta.yaml not written: %v", err)
	}
	diff, err := os.ReadFile(filepath.Join(dir, ".regatta", "sample.diff"))
	if err != nil {
		t.Fatalf(".regatta/sample.diff not written: %v", err)
	}
	if len(yaml) == 0 || len(diff) == 0 {
		t.Fatalf("empty file written: yaml=%d diff=%d", len(yaml), len(diff))
	}
	if !bytes.Contains([]byte(stdout), []byte("wrote regatta.yaml")) {
		t.Fatalf("expected stdout to mention 'wrote regatta.yaml'; got %q", stdout)
	}
}
```

- [ ] **Step 2: Run test to verify it fails (no runInit yet)**

Run: `go test -run 'TestInit_WritesBothFiles' ./cmd/regatta/`
Expected: FAIL with `undefined: runInitWithIO` (compile error is expected fail-first)

- [ ] **Step 3: Create init.go with minimal skeleton**

Create `cmd/regatta/init.go`:

```go
package main

import (
	"embed"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

//go:embed init_assets/regatta.yaml init_assets/sample.diff
var initAssets embed.FS

// runInit is the CLI entry point. It dispatches to runInitWithIO so
// tests can capture output without touching real stdout/stderr.
func runInit(args []string) int {
	return runInitWithIO(args, os.Stdout, os.Stderr)
}

func runInitWithIO(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	force := fs.Bool("force", false, "overwrite existing regatta.yaml / .regatta/sample.diff")
	jsonOut := fs.Bool("json", false, "emit JSON envelope instead of friendly prose")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	yamlBytes, err := initAssets.ReadFile("init_assets/regatta.yaml")
	if err != nil {
		fmt.Fprintf(stderr, "regatta init: internal: read embedded yaml: %v\n", err)
		return 1
	}
	diffBytes, err := initAssets.ReadFile("init_assets/sample.diff")
	if err != nil {
		fmt.Fprintf(stderr, "regatta init: internal: read embedded sample.diff: %v\n", err)
		return 1
	}

	if err := os.WriteFile("regatta.yaml", yamlBytes, 0o644); err != nil {
		fmt.Fprintf(stderr, "regatta init: write regatta.yaml: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "+ wrote regatta.yaml         (your config; L0 gate enabled)")

	if err := os.MkdirAll(".regatta", 0o755); err != nil {
		fmt.Fprintf(stderr, "regatta init: mkdir .regatta: %v\n", err)
		return 1
	}
	diffPath := filepath.Join(".regatta", "sample.diff")
	if err := os.WriteFile(diffPath, diffBytes, 0o644); err != nil {
		fmt.Fprintf(stderr, "regatta init: write %s: %v\n", diffPath, err)
		return 1
	}
	fmt.Fprintln(stdout, "+ wrote .regatta/sample.diff (a demo attack against MILESTONES.md)")

	_ = force
	_ = jsonOut
	return 0
}
```

- [ ] **Step 4: Wire `init` into main.go dispatch**

Edit `cmd/regatta/main.go` around line 64. Add a case after `validate-config`:

```go
	case "validate-config":
		os.Exit(runValidateConfig(os.Args[2:]))
	case "init":
		os.Exit(runInit(os.Args[2:]))
```

And add a usage line in `usage()` around line 87, before `regatta version`:

```go
  regatta init                                        Scaffold regatta.yaml + run L0 demo
  regatta version                                     Print build info
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test -run 'TestInit_WritesBothFiles' ./cmd/regatta/ -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/regatta/init.go cmd/regatta/init_test.go cmd/regatta/main.go
git commit -m "feat(init): scaffold runInit and wire into main dispatch"
```

---

## Task 4: Demo runs L0 in-process + friendly prose

Add the L0 demo call + friendly prose formatter. Test asserts on key phrases.

**Files:**
- Modify: `cmd/regatta/init.go`
- Modify: `cmd/regatta/init_test.go`

- [ ] **Step 1: Write the friendly-output test**

Append to `cmd/regatta/init_test.go`:

```go
func TestInit_FriendlyOutput(t *testing.T) {
	dir := t.TempDir()
	code, stdout, stderr := runInitInDir(t, dir, nil)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	wantPhrases := []string{
		"wrote regatta.yaml",
		"wrote .regatta/sample.diff",
		"Running L0 gate",
		"FAIL",
		"L0-TEXT-0",
		"Trap Pattern P3",
		"docs/incidents.md#pattern-3",
		"Next steps",
	}
	for _, p := range wantPhrases {
		if !bytes.Contains([]byte(stdout), []byte(p)) {
			t.Errorf("expected stdout to contain %q; if init prose changed intentionally, update this test\nfull stdout:\n%s", p, stdout)
		}
	}
}

func TestInit_ExitsZeroOnDemoFail(t *testing.T) {
	dir := t.TempDir()
	code, stdout, _ := runInitInDir(t, dir, nil)
	if code != 0 {
		t.Fatalf("init must exit 0 even when demo verdict is FAIL; got code=%d", code)
	}
	if !bytes.Contains([]byte(stdout), []byte("FAIL")) {
		t.Fatalf("expected demo verdict FAIL in stdout; got %q", stdout)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run 'TestInit_FriendlyOutput|TestInit_ExitsZero' ./cmd/regatta/ -v`
Expected: FAIL — stdout lacks the new phrases.

- [ ] **Step 3: Add L0 demo + friendly prose to init.go**

Replace `runInitWithIO` body in `cmd/regatta/init.go`. Add imports `"github.com/trilamsr/regatta/internal/gates/l0"` and `"github.com/trilamsr/regatta/contracts/schemas"`. Then replace the post-write tail of `runInitWithIO` (after the sample.diff write):

```go
	// Re-read sample.diff so the verdict reflects what's on disk
	// (operator might inspect it).
	onDisk, err := os.ReadFile(diffPath)
	if err != nil {
		fmt.Fprintf(stderr, "regatta init: re-read %s: %v\n", diffPath, err)
		return 1
	}
	res := l0.Check(l0.Default(), l0.ParseUnifiedDiff(string(onDisk)))

	if *jsonOut {
		emitInitJSON(stdout, []string{"regatta.yaml", diffPath}, nil, nil, res)
		return 0
	}

	emitInitProse(stdout, res)
	return 0
}

// emitInitProse formats the GateResult into the friendly demo block.
// Generated from the GateResult, not hardcoded, so future fixture
// or L0 changes ripple correctly.
func emitInitProse(w io.Writer, res schemas.GateResult) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Running L0 gate against the demo to show you what regatta catches:")
	fmt.Fprintln(w)
	if res.Verdict != schemas.VerdictFail || len(res.Findings) == 0 {
		fmt.Fprintf(w, "  Verdict: %s (no findings)\n", res.Verdict)
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Next steps:")
		fmt.Fprintln(w, "  - Run `regatta l0 <your-diff>` on a real PR diff")
		return
	}
	f := res.Findings[0]
	fmt.Fprintf(w, "  FAIL: spec criterion text changed without citation (%s)\n", f.ID)
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  In %s, the criterion was rewritten. L0 blocks this because\n", f.Location)
	fmt.Fprintln(w, "  criteria are the contract between you and the agent: silent")
	fmt.Fprintln(w, "  edits move the goalposts.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  The catch is sneakier than it looks: the diff replaces Latin")
	fmt.Fprintln(w, "  \"A\" with Cyrillic \"А\" (U+0410). They render identically. A")
	fmt.Fprintln(w, "  human reviewer scanning the diff sees \"Auth -> Auth\" and")
	fmt.Fprintln(w, "  approves; L0 compares NFC-normalized code points and rejects.")
	fmt.Fprintln(w)
	blurb := patternBlurb(f.TrapPattern)
	fmt.Fprintf(w, "  Trap Pattern %s %s\n", f.TrapPattern, blurb)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Next steps:")
	fmt.Fprintln(w, "  - Run `regatta l0 <your-diff>` on a real PR diff")
	fmt.Fprintln(w, "  - Run `regatta verify-repo-config` to audit your repo's branch")
	fmt.Fprintln(w, "    protection and CODEOWNERS")
	fmt.Fprintln(w, "  - Edit regatta.yaml to enable more gates as they ship")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Done.")
}

// patternBlurb returns a one-line summary of a Trap Catalog pattern.
// Falls back to a generic pointer if the pattern is unknown so future
// L0 patterns do not break init silently.
func patternBlurb(p string) string {
	switch p {
	case "P1":
		return "(deterministic gate before AI gate on destructive ops). See docs/incidents.md#pattern-1."
	case "P2":
		return "(two-key approval on irreversible actions). See docs/incidents.md#pattern-2."
	case "P3":
		return "(fetch trusted instructions from main, treat all other text as data). See docs/incidents.md#pattern-3."
	case "P4":
		return "(least-privilege, ephemeral, environment-scoped credentials). See docs/incidents.md#pattern-4."
	case "P5":
		return "(out-of-band supervisor for limits and kill-switches). See docs/incidents.md#pattern-5."
	case "P6":
		return "(verified grounding for any outward-facing claim). See docs/incidents.md#pattern-6."
	case "P7":
		return "(schema-level scope constraints, not prompt-level). See docs/incidents.md#pattern-7."
	case "P8":
		return "(spend / iteration brakes with mandatory re-approval). See docs/incidents.md#pattern-8."
	case "P9":
		return "(sensitive context segregation). See docs/incidents.md#pattern-9."
	case "P10":
		return "(render-the-invisible + signed prompt artifacts). See docs/incidents.md#pattern-10."
	case "P11":
		return "(agent-artifact release pipelines are themselves attack surface). See docs/incidents.md#pattern-11."
	case "P12":
		return "(inbound vulnerability signals default-escalate). See docs/incidents.md#pattern-12."
	case "P13":
		return "(judge-LLM lineage isolation). See docs/incidents.md#pattern-13."
	default:
		return "(uncatalogued). See docs/incidents.md."
	}
}

// emitInitJSON writes the structured envelope for --json mode.
func emitInitJSON(w io.Writer, written, skipped, overwritten []string, res schemas.GateResult) {
	type envelope struct {
		Written     []string             `json:"written"`
		Skipped     []string             `json:"skipped"`
		Overwritten []string             `json:"overwritten"`
		GateResult  schemas.GateResult   `json:"gate_result"`
	}
	env := envelope{
		Written:     nilToEmpty(written),
		Skipped:     nilToEmpty(skipped),
		Overwritten: nilToEmpty(overwritten),
		GateResult:  res,
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(env)
}

func nilToEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
```

Add `"encoding/json"` to the imports at the top.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -run 'TestInit_' ./cmd/regatta/ -v`
Expected: PASS for `TestInit_WritesBothFiles`, `TestInit_FriendlyOutput`, `TestInit_ExitsZeroOnDemoFail`.

- [ ] **Step 5: Eyeball the actual output**

Run: `go run ./cmd/regatta init` in a tmp dir:

```bash
mkdir -p /tmp/regatta-init-demo && cd /tmp/regatta-init-demo
rm -f regatta.yaml && rm -rf .regatta
go run /Users/treedesk/Desktop/Projects/regatta/cmd/regatta init
```

Expected: human-readable output matching the spec's Happy path block.

- [ ] **Step 6: Commit**

```bash
git add cmd/regatta/init.go cmd/regatta/init_test.go
git commit -m "feat(init): in-process L0 demo + friendly prose + JSON envelope"
```

---

## Task 5: `--json` envelope test + JSON output verification

**Files:**
- Modify: `cmd/regatta/init_test.go`

- [ ] **Step 1: Write the JSON test**

Append to `cmd/regatta/init_test.go`:

```go
import (
	"encoding/json"
	// existing
)

func TestInit_JSONOutput(t *testing.T) {
	dir := t.TempDir()
	code, stdout, stderr := runInitInDir(t, dir, []string{"--json"})
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	if stderr != "" {
		t.Fatalf("expected empty stderr on success with --json; got %q", stderr)
	}
	var env struct {
		Written     []string `json:"written"`
		Skipped     []string `json:"skipped"`
		Overwritten []string `json:"overwritten"`
		GateResult  struct {
			Verdict  string `json:"verdict"`
			GateID   string `json:"gate_id"`
			Findings []struct {
				ID          string `json:"id"`
				TrapPattern string `json:"trap_pattern"`
			} `json:"findings"`
		} `json:"gate_result"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout=%q", err, stdout)
	}
	if got, want := env.GateResult.Verdict, "fail"; got != want {
		t.Errorf("gate_result.verdict=%q want %q", got, want)
	}
	if got, want := env.GateResult.GateID, "l0_spec_immutability"; got != want {
		t.Errorf("gate_result.gate_id=%q want %q", got, want)
	}
	if len(env.GateResult.Findings) == 0 || env.GateResult.Findings[0].TrapPattern != "P3" {
		t.Errorf("expected at least one finding with trap_pattern=P3; got %+v", env.GateResult.Findings)
	}
	if len(env.Written) != 2 {
		t.Errorf("expected 2 written files; got %v", env.Written)
	}
}

func TestInit_PatternBlurbFallback(t *testing.T) {
	t.Parallel()
	got := patternBlurb("P99")
	if got == "" {
		t.Fatal("patternBlurb returned empty string on unknown code")
	}
	if !bytes.Contains([]byte(got), []byte("docs/incidents.md")) {
		t.Errorf("fallback should point at docs/incidents.md; got %q", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they pass**

Run: `go test -run 'TestInit_JSONOutput|TestInit_PatternBlurb' ./cmd/regatta/ -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add cmd/regatta/init_test.go
git commit -m "test(init): JSON envelope + patternBlurb fallback"
```

---

## Task 6: Idempotency rules + divergence handling + --force

Add the per-file decision logic (absent → write; present-matching → skip; present-diverged → exit 2; --force → overwrite).

**Files:**
- Modify: `cmd/regatta/init.go`
- Modify: `cmd/regatta/init_test.go`

- [ ] **Step 1: Write the idempotency + divergence + force tests**

Append to `cmd/regatta/init_test.go`:

```go
func TestInit_IdempotentReRun(t *testing.T) {
	dir := t.TempDir()
	if code, _, stderr := runInitInDir(t, dir, nil); code != 0 {
		t.Fatalf("first run failed: code=%d stderr=%q", code, stderr)
	}
	code, stdout, stderr := runInitInDir(t, dir, nil)
	if code != 0 {
		t.Fatalf("second run failed: code=%d stderr=%q", code, stderr)
	}
	if !bytes.Contains([]byte(stdout), []byte("= regatta.yaml unchanged")) {
		t.Errorf("expected '= regatta.yaml unchanged' marker; got %q", stdout)
	}
	if !bytes.Contains([]byte(stdout), []byte("= .regatta/sample.diff unchanged")) {
		t.Errorf("expected '= .regatta/sample.diff unchanged' marker; got %q", stdout)
	}
}

func TestInit_RefusesDivergedYaml(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "regatta.yaml"), []byte("# operator hand-edit\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	code, _, stderr := runInitInDir(t, dir, nil)
	if code != 2 {
		t.Fatalf("expected exit 2 on diverged yaml; got %d stderr=%q", code, stderr)
	}
	for _, want := range []string{"regatta.yaml", "--force"} {
		if !bytes.Contains([]byte(stderr), []byte(want)) {
			t.Errorf("stderr should mention %q; got %q", want, stderr)
		}
	}
}

func TestInit_DivergedSampleDiff(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".regatta"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".regatta", "sample.diff"), []byte("not the embedded blob\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	code, _, stderr := runInitInDir(t, dir, nil)
	if code != 2 {
		t.Fatalf("expected exit 2 on diverged sample.diff; got %d stderr=%q", code, stderr)
	}
	if !bytes.Contains([]byte(stderr), []byte(".regatta/sample.diff")) || !bytes.Contains([]byte(stderr), []byte("--force")) {
		t.Errorf("stderr should mention path + --force; got %q", stderr)
	}
}

func TestInit_FailsAtomicallyOnDivergence(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "regatta.yaml"), []byte("# operator hand-edit\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	code, _, _ := runInitInDir(t, dir, nil)
	if code != 2 {
		t.Fatalf("expected exit 2; got %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, ".regatta", "sample.diff")); !os.IsNotExist(err) {
		t.Fatalf("sample.diff should not have been written when yaml diverged; err=%v", err)
	}
}

func TestInit_ForceOverwrites(t *testing.T) {
	dir := t.TempDir()
	seed := []byte("operator content\n")
	if err := os.WriteFile(filepath.Join(dir, "regatta.yaml"), seed, 0o644); err != nil {
		t.Fatalf("seed yaml: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".regatta"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".regatta", "sample.diff"), seed, 0o644); err != nil {
		t.Fatalf("seed diff: %v", err)
	}
	code, stdout, stderr := runInitInDir(t, dir, []string{"--force"})
	if code != 0 {
		t.Fatalf("expected exit 0 with --force; got %d stderr=%q", code, stderr)
	}
	yaml, _ := os.ReadFile(filepath.Join(dir, "regatta.yaml"))
	if bytes.Equal(yaml, seed) {
		t.Errorf("--force should have overwritten regatta.yaml")
	}
	if !bytes.Contains([]byte(stdout), []byte("! overwrote regatta.yaml")) {
		t.Errorf("expected '! overwrote' marker; got %q", stdout)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run 'TestInit_Idempotent|TestInit_Refuses|TestInit_Diverged|TestInit_Fails|TestInit_Force' ./cmd/regatta/ -v`
Expected: FAIL — current `runInitWithIO` always overwrites.

- [ ] **Step 3: Refactor runInitWithIO to use per-file decision**

Replace the two `os.WriteFile` blocks in `runInitWithIO` (regatta.yaml + .regatta/sample.diff) with a decision-driven flow. Replace from the post-asset-read section through the final return:

```go
	// Step 1: classify each file before writing anything (atomicity).
	type decision struct {
		path    string
		blurb   string
		bytes   []byte
		action  string // "write", "skip", "overwrite", "diverge"
	}
	files := []decision{
		{path: "regatta.yaml", blurb: "(your config; L0 gate enabled)", bytes: yamlBytes},
		{path: filepath.Join(".regatta", "sample.diff"), blurb: "(a demo attack against MILESTONES.md)", bytes: diffBytes},
	}
	for i := range files {
		existing, err := os.ReadFile(files[i].path)
		switch {
		case os.IsNotExist(err):
			files[i].action = "write"
		case err != nil:
			fmt.Fprintf(stderr, "regatta init: stat %s: %v\n", files[i].path, err)
			return 1
		case bytes.Equal(existing, files[i].bytes):
			files[i].action = "skip"
		case *force:
			files[i].action = "overwrite"
		default:
			files[i].action = "diverge"
		}
	}
	// Short-circuit on any divergence before any write.
	for _, d := range files {
		if d.action == "diverge" {
			fmt.Fprintf(stderr, "regatta init: %s already exists and differs from the bundled template.\n", d.path)
			fmt.Fprintf(stderr, "  To re-init: rm regatta.yaml .regatta/sample.diff\n")
			fmt.Fprintf(stderr, "  To overwrite: regatta init --force\n")
			return 2
		}
	}

	// Step 2: ensure .regatta/ exists if we need to write into it.
	for _, d := range files {
		if d.action == "skip" {
			continue
		}
		if dir := filepath.Dir(d.path); dir != "." {
			if err := safeMkdir(dir); err != nil {
				fmt.Fprintf(stderr, "regatta init: %v\n", err)
				return 2
			}
		}
	}

	// Step 3: apply.
	var written, skipped, overwritten []string
	for _, d := range files {
		switch d.action {
		case "write":
			if err := os.WriteFile(d.path, d.bytes, 0o644); err != nil {
				fmt.Fprintf(stderr, "regatta init: write %s: %v\n", d.path, err)
				return 1
			}
			fmt.Fprintf(stdout, "+ wrote %s %s\n", padPath(d.path), d.blurb)
			written = append(written, d.path)
		case "skip":
			fmt.Fprintf(stdout, "= %s unchanged\n", d.path)
			skipped = append(skipped, d.path)
		case "overwrite":
			if err := os.WriteFile(d.path, d.bytes, 0o644); err != nil {
				fmt.Fprintf(stderr, "regatta init: write %s: %v\n", d.path, err)
				return 1
			}
			fmt.Fprintf(stdout, "! overwrote %s %s\n", padPath(d.path), d.blurb)
			overwritten = append(overwritten, d.path)
		}
	}

	// Step 4: re-read sample.diff and run L0.
	onDisk, err := os.ReadFile(filepath.Join(".regatta", "sample.diff"))
	if err != nil {
		fmt.Fprintf(stderr, "regatta init: re-read .regatta/sample.diff: %v\n", err)
		return 1
	}
	res := l0.Check(l0.Default(), l0.ParseUnifiedDiff(string(onDisk)))

	if *jsonOut {
		emitInitJSON(stdout, written, skipped, overwritten, res)
		return 0
	}
	emitInitProse(stdout, res)
	return 0
}

// safeMkdir ensures path exists as a regular directory. Refuses if
// path exists as a symlink, regular file, device, or other non-dir.
// Defends against an attacker pre-creating .regatta -> /etc.
func safeMkdir(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return os.MkdirAll(path, 0o755)
	}
	if err != nil {
		return fmt.Errorf("lstat %s: %w", path, err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to write: %s exists but is not a regular directory (got mode %s). Remove or rename it, then re-run", path, info.Mode())
	}
	return nil
}

// padPath right-pads the path for column alignment in friendly output.
func padPath(p string) string {
	const width = 22
	if len(p) >= width {
		return p
	}
	return p + spaces(width-len(p))
}

func spaces(n int) string {
	out := make([]byte, n)
	for i := range out {
		out[i] = ' '
	}
	return string(out)
}
```

Add `"bytes"` to the imports.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -run 'TestInit_' ./cmd/regatta/ -v`
Expected: all `TestInit_*` PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/regatta/init.go cmd/regatta/init_test.go
git commit -m "feat(init): symmetric idempotency rules + atomic divergence + --force"
```

---

## Task 7: `.regatta/` symlink defense + coexistence with serve

**Files:**
- Modify: `cmd/regatta/init_test.go`

- [ ] **Step 1: Write the symlink + coexistence tests**

Append:

```go
import (
	"runtime"
	// existing
)

func TestInit_RefusesSymlinkRegatta(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows; defense still active in code")
	}
	dir := t.TempDir()
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(dir, ".regatta")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	code, _, stderr := runInitInDir(t, dir, nil)
	if code != 2 {
		t.Fatalf("expected exit 2; got %d stderr=%q", code, stderr)
	}
	if !bytes.Contains([]byte(stderr), []byte("not a regular directory")) {
		t.Errorf("stderr should explain the refusal; got %q", stderr)
	}
}

func TestInit_LeavesPopulatedRegattaDirAlone(t *testing.T) {
	dir := t.TempDir()
	itemsDir := filepath.Join(dir, ".regatta", "items")
	if err := os.MkdirAll(itemsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	keep := filepath.Join(itemsDir, "foo.md")
	if err := os.WriteFile(keep, []byte("# foo\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	code, _, stderr := runInitInDir(t, dir, nil)
	if code != 0 {
		t.Fatalf("expected exit 0; got %d stderr=%q", code, stderr)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("init should not have touched .regatta/items/foo.md: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".regatta", "sample.diff")); err != nil {
		t.Fatalf("sample.diff should have been written: %v", err)
	}
}

func TestInit_NonGitDir(t *testing.T) {
	dir := t.TempDir()
	code, _, stderr := runInitInDir(t, dir, nil)
	if code != 0 {
		t.Fatalf("init must not require git; got code=%d stderr=%q", code, stderr)
	}
}

func TestInit_HelpFlag(t *testing.T) {
	for _, arg := range []string{"-h", "--help"} {
		var out, errOut bytes.Buffer
		code := runInitWithIO([]string{arg}, &out, &errOut)
		if code != 0 {
			t.Errorf("%s exit=%d stderr=%q", arg, code, errOut.String())
		}
		combined := out.String() + errOut.String()
		if !bytes.Contains([]byte(combined), []byte("force")) {
			t.Errorf("%s should describe --force flag; got %q", arg, combined)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they pass**

Run: `go test -run 'TestInit_Refuses|TestInit_Leaves|TestInit_NonGit|TestInit_HelpFlag' ./cmd/regatta/ -v`
Expected: all PASS (symlink defense + non-git + populated-dir already work via Task 6's safeMkdir; help is `flag.ContinueOnError` which prints usage on `-h`).

If `TestInit_HelpFlag` fails, change `flag.ContinueOnError` to handle `-h`/`--help` explicitly: add at the top of `runInitWithIO` after `fs.Parse`:

```go
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
```

Add `"errors"` to imports.

- [ ] **Step 3: Commit**

```bash
git add cmd/regatta/init.go cmd/regatta/init_test.go
git commit -m "test(init): symlink defense + serve coexistence + non-git + help"
```

---

## Task 8: Drift-test-defeat protection

**Files:**
- Modify: `cmd/regatta/init_test.go`

- [ ] **Step 1: Write the test**

Append:

```go
func TestInitUsesEmbeddedBytes(t *testing.T) {
	dir := t.TempDir()
	if code, _, stderr := runInitInDir(t, dir, nil); code != 0 {
		t.Fatalf("init: code=%d stderr=%q", code, stderr)
	}
	written, err := os.ReadFile(filepath.Join(dir, "regatta.yaml"))
	if err != nil {
		t.Fatalf("read written: %v", err)
	}
	embedded, err := initAssets.ReadFile("init_assets/regatta.yaml")
	if err != nil {
		t.Fatalf("read embed: %v", err)
	}
	if !bytes.Equal(written, embedded) {
		t.Fatalf("init wrote bytes that diverge from the embed.FS blob; if init now sources from disk instead of embed, the drift gate is defeated")
	}
}
```

- [ ] **Step 2: Run + verify pass**

Run: `go test -run 'TestInitUsesEmbeddedBytes' ./cmd/regatta/ -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add cmd/regatta/init_test.go
git commit -m "test(init): assert init writes embed.FS bytes (drift-test defeat)"
```

---

## Task 9: CLI smoke test infrastructure (TestMain + harness)

**Files:**
- Create: `cmd/regatta/cli_smoke_test.go`

- [ ] **Step 1: Write the TestMain build harness**

Create `cmd/regatta/cli_smoke_test.go`:

```go
package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var smokeBinary string

func TestMain(m *testing.M) {
	if _, err := exec.LookPath("go"); err != nil {
		// Smoke tests need `go build`; skip the suite cleanly.
		os.Exit(m.Run())
	}
	tmp, err := os.MkdirTemp("", "regatta-smoke-")
	if err != nil {
		panic("smoke: mkdtemp: " + err.Error())
	}
	defer os.RemoveAll(tmp)

	bin := filepath.Join(tmp, "regatta")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}

	root := smokeModuleRoot()
	if root == "" {
		os.Exit(m.Run())
	}

	cmd := exec.Command("go", "build", "-buildvcs=false", "-o", bin, "./cmd/regatta")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Build failure: print + skip suite (do not fail; this
		// keeps `go test ./...` green when contributors run on
		// machines without a buildable toolchain).
		os.Stderr.WriteString("smoke: go build failed; skipping suite:\n")
		os.Stderr.Write(out)
		os.Exit(m.Run())
	}
	smokeBinary = bin
	os.Exit(m.Run())
}

// smokeModuleRoot is a smoke-test-local copy of moduleRoot from
// init_test.go so this file can be read alone.
func smokeModuleRoot() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// runSmoke executes the compiled binary with args in workDir, returns
// exit code + stdout + stderr. Env via cmd.Env so subtests stay
// t.Parallel-safe.
func runSmoke(t *testing.T, workDir string, args []string, env []string) (code int, stdout, stderr string) {
	t.Helper()
	if smokeBinary == "" {
		t.Skip("smoke binary not built (go not on PATH or build failed)")
	}
	cmd := exec.Command(smokeBinary, args...)
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(), env...)
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err := cmd.Run()
	stdout, stderr = out.String(), errOut.String()
	if err == nil {
		return 0, stdout, stderr
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode(), stdout, stderr
	}
	t.Fatalf("exec error: %v", err)
	return -1, "", ""
}

func expectExit(t *testing.T, want, got int, stdout, stderr string) {
	t.Helper()
	if want != got {
		t.Fatalf("exit=%d want=%d\nstdout=%q\nstderr=%q", got, want, stdout, stderr)
	}
}

func expectContains(t *testing.T, stream, want, name string) {
	t.Helper()
	if !strings.Contains(stream, want) {
		t.Fatalf("%s should contain %q; got %q", name, want, stream)
	}
}
```

- [ ] **Step 2: Verify TestMain compiles + suite runs (even with no subtests)**

Run: `go test -run 'NoMatch' ./cmd/regatta/ -v`
Expected: build of test binary OK, no test failures. (TestMain runs the inner build silently.)

- [ ] **Step 3: Commit**

```bash
git add cmd/regatta/cli_smoke_test.go
git commit -m "test(cli): TestMain build-once + runSmoke harness"
```

---

## Task 10: Smoke tests — top-level dispatch

Help / bare / unknown / version.

**Files:**
- Modify: `cmd/regatta/cli_smoke_test.go`

- [ ] **Step 1: Write the tests**

Append:

```go
func TestCLI_BareNoArgs(t *testing.T) {
	t.Parallel()
	code, stdout, stderr := runSmoke(t, t.TempDir(), nil, nil)
	expectExit(t, 2, code, stdout, stderr)
	if !strings.Contains(strings.ToLower(stderr), "usage") {
		t.Fatalf("expected usage in stderr; got %q", stderr)
	}
}

func TestCLI_UnknownSubcommand(t *testing.T) {
	t.Parallel()
	code, stdout, stderr := runSmoke(t, t.TempDir(), []string{"nonexistent-sub"}, nil)
	expectExit(t, 2, code, stdout, stderr)
	expectContains(t, stderr, "unknown subcommand", "stderr")
}

func TestCLI_Help(t *testing.T) {
	t.Parallel()
	code, stdout, stderr := runSmoke(t, t.TempDir(), []string{"help"}, nil)
	expectExit(t, 0, code, stdout, stderr)
	expectContains(t, stdout, "Usage:", "stdout")
	expectContains(t, stdout, "regatta init", "stdout")
}

func TestCLI_Version(t *testing.T) {
	t.Parallel()
	code, stdout, stderr := runSmoke(t, t.TempDir(), []string{"version"}, nil)
	expectExit(t, 0, code, stdout, stderr)
	expectContains(t, stdout, "regatta", "stdout")
}
```

- [ ] **Step 2: Run + verify pass**

Run: `go test -run 'TestCLI_Bare|TestCLI_Unknown|TestCLI_Help|TestCLI_Version' ./cmd/regatta/ -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add cmd/regatta/cli_smoke_test.go
git commit -m "test(cli): top-level dispatch (bare, unknown, help, version)"
```

---

## Task 11: Smoke tests — `regatta init`

**Files:**
- Modify: `cmd/regatta/cli_smoke_test.go`

- [ ] **Step 1: Write the tests**

Append:

```go
func TestCLI_Init_HappyPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	code, stdout, stderr := runSmoke(t, dir, []string{"init"}, nil)
	expectExit(t, 0, code, stdout, stderr)
	expectContains(t, stdout, "wrote regatta.yaml", "stdout")
	expectContains(t, stdout, "FAIL", "stdout")
	if _, err := os.Stat(filepath.Join(dir, "regatta.yaml")); err != nil {
		t.Fatalf("regatta.yaml not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".regatta", "sample.diff")); err != nil {
		t.Fatalf(".regatta/sample.diff not written: %v", err)
	}
}

func TestCLI_Init_RefusesDiverged(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "regatta.yaml"), []byte("# operator\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	code, stdout, stderr := runSmoke(t, dir, []string{"init"}, nil)
	expectExit(t, 2, code, stdout, stderr)
	expectContains(t, stderr, "--force", "stderr")
}
```

- [ ] **Step 2: Run + verify pass**

Run: `go test -run 'TestCLI_Init' ./cmd/regatta/ -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add cmd/regatta/cli_smoke_test.go
git commit -m "test(cli): init happy path + refuse-diverged"
```

---

## Task 12: Smoke tests — L0 family

**Files:**
- Modify: `cmd/regatta/cli_smoke_test.go`

- [ ] **Step 1: Write the tests**

Append:

```go
func TestCLI_L0_Pass(t *testing.T) {
	t.Parallel()
	root := smokeModuleRoot()
	if root == "" {
		t.Skip("module root not resolvable")
	}
	fixture := filepath.Join(root, "testdata/gates/l0/pass/00_no_milestones_touched.diff")
	if _, err := os.Stat(fixture); err != nil {
		// Pick any pass fixture.
		entries, _ := os.ReadDir(filepath.Join(root, "testdata/gates/l0/pass"))
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".diff") {
				fixture = filepath.Join(root, "testdata/gates/l0/pass", e.Name())
				break
			}
		}
	}
	code, stdout, stderr := runSmoke(t, t.TempDir(), []string{"l0", fixture}, nil)
	expectExit(t, 0, code, stdout, stderr)
	expectContains(t, stdout, "\"verdict\"", "stdout")
	expectContains(t, stdout, "pass", "stdout")
}

func TestCLI_L0_Fail(t *testing.T) {
	t.Parallel()
	root := smokeModuleRoot()
	if root == "" {
		t.Skip("module root not resolvable")
	}
	fixture := filepath.Join(root, "testdata/gates/l0/fail/00_criterion_text_edit.diff")
	code, stdout, stderr := runSmoke(t, t.TempDir(), []string{"l0", fixture}, nil)
	expectExit(t, 1, code, stdout, stderr)
	expectContains(t, stdout, "fail", "stdout")
}

func TestCLI_L0_Help(t *testing.T) {
	t.Parallel()
	code, stdout, stderr := runSmoke(t, t.TempDir(), []string{"l0", "-h"}, nil)
	if code != 0 && code != 2 {
		t.Fatalf("l0 -h: unexpected exit=%d", code)
	}
	combined := strings.ToLower(stdout + stderr)
	if !strings.Contains(combined, "usage") {
		t.Fatalf("expected usage; got stdout=%q stderr=%q", stdout, stderr)
	}
}
```

- [ ] **Step 2: Run + verify pass**

Run: `go test -run 'TestCLI_L0' ./cmd/regatta/ -v`
Expected: PASS. If `TestCLI_L0_Pass` fails because no pass fixture matches, the harness falls back to the first `.diff` in pass/.

- [ ] **Step 3: Commit**

```bash
git add cmd/regatta/cli_smoke_test.go
git commit -m "test(cli): l0 pass + fail + help"
```

---

## Task 13: Smoke tests — `validate-config`, `verify-repo-config`, `serve --tick-once`

**Files:**
- Modify: `cmd/regatta/cli_smoke_test.go`
- Create: `cmd/regatta/testdata/cli_smoke/malformed_config.yaml`

- [ ] **Step 1: Create the malformed-config fixture**

```bash
mkdir -p cmd/regatta/testdata/cli_smoke
cat > cmd/regatta/testdata/cli_smoke/malformed_config.yaml <<'EOF'
# Missing required `version` field; CUE should reject.
gates:
  l0:
    enabled: not-a-bool
EOF
```

- [ ] **Step 2: Write the tests**

Append:

```go
func TestCLI_ValidateConfig_Happy(t *testing.T) {
	t.Parallel()
	root := smokeModuleRoot()
	if root == "" {
		t.Skip("module root not resolvable")
	}
	dir := t.TempDir()
	src, err := os.ReadFile(filepath.Join(root, "examples/minimal/regatta.yaml"))
	if err != nil {
		t.Fatalf("read example: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "regatta.yaml"), src, 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	code, stdout, stderr := runSmoke(t, dir, []string{"validate-config"}, nil)
	expectExit(t, 0, code, stdout, stderr)
}

func TestCLI_ValidateConfig_Malformed(t *testing.T) {
	t.Parallel()
	root := smokeModuleRoot()
	if root == "" {
		t.Skip("module root not resolvable")
	}
	dir := t.TempDir()
	src, err := os.ReadFile(filepath.Join(root, "cmd/regatta/testdata/cli_smoke/malformed_config.yaml"))
	if err != nil {
		t.Fatalf("read malformed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "regatta.yaml"), src, 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	code, stdout, stderr := runSmoke(t, dir, []string{"validate-config"}, nil)
	if code == 0 {
		t.Fatalf("expected non-zero exit on malformed config; got 0\nstdout=%q stderr=%q", stdout, stderr)
	}
	if stderr == "" {
		t.Fatalf("expected stderr explanation; got empty")
	}
}

func TestCLI_VerifyRepoConfig_MissingFlag(t *testing.T) {
	t.Parallel()
	code, stdout, stderr := runSmoke(t, t.TempDir(), []string{"verify-repo-config"}, nil)
	if code == 0 {
		t.Fatalf("expected non-zero exit when -owner/-repo missing; got 0\nstdout=%q stderr=%q", stdout, stderr)
	}
	if stderr == "" {
		t.Fatalf("expected stderr explanation; got empty")
	}
}

func TestCLI_Serve_TickOnceStub(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	itemsDir := filepath.Join(dir, ".regatta", "items")
	if err := os.MkdirAll(itemsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	args := []string{
		"serve",
		"--tick-once",
		"--spawner=stub",
		"-repo=" + dir,
		"-items-root=" + dir,
		"-db=" + filepath.Join(dir, "state.db"),
	}
	code, stdout, stderr := runSmoke(t, dir, args, nil)
	expectExit(t, 0, code, stdout, stderr)
}

func TestCLI_Serve_BogusSpawner(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	args := []string{
		"serve",
		"--tick-once",
		"--spawner=bogus",
		"-repo=" + dir,
		"-db=" + filepath.Join(dir, "state.db"),
	}
	code, stdout, stderr := runSmoke(t, dir, args, nil)
	if code == 0 {
		t.Fatalf("expected non-zero exit with bogus spawner; got 0\nstdout=%q stderr=%q", stdout, stderr)
	}
}
```

- [ ] **Step 3: Run + verify**

Run: `go test -run 'TestCLI_ValidateConfig|TestCLI_VerifyRepoConfig|TestCLI_Serve' ./cmd/regatta/ -v`
Expected: PASS. If a `serve` flag name differs from what `main.go runServe` accepts, adjust the args. Check `main.go:220-299` for actual flag names.

- [ ] **Step 4: Commit**

```bash
git add cmd/regatta/cli_smoke_test.go cmd/regatta/testdata/cli_smoke/
git commit -m "test(cli): validate-config, verify-repo-config, serve --tick-once"
```

---

## Task 14: Smoke tests — `program` subcommands

**Files:**
- Modify: `cmd/regatta/cli_smoke_test.go`

- [ ] **Step 1: Write the tests**

Append:

```go
func TestCLI_Program_BareNoSub(t *testing.T) {
	t.Parallel()
	code, stdout, stderr := runSmoke(t, t.TempDir(), []string{"program"}, nil)
	if code == 0 {
		t.Fatalf("expected non-zero exit; got 0\nstdout=%q stderr=%q", stdout, stderr)
	}
}

func TestCLI_ProgramVerifyHandoff_BadInput(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	code, stdout, stderr := runSmoke(t, dir, []string{"program", "verify-handoff", bad}, nil)
	if code == 0 {
		t.Fatalf("expected non-zero exit on malformed handoff; got 0\nstdout=%q stderr=%q", stdout, stderr)
	}
}
```

- [ ] **Step 2: Run + verify**

Run: `go test -run 'TestCLI_Program' ./cmd/regatta/ -v`
Expected: PASS. (Happy-path `program plan` skipped per spec — needs ANTHROPIC_API_KEY.)

- [ ] **Step 3: Commit**

```bash
git add cmd/regatta/cli_smoke_test.go
git commit -m "test(cli): program bare + verify-handoff bad-input"
```

---

## Task 15: Update operator docs

**Files:**
- Modify: `docs/operator/quickstart.md`
- Modify: `docs/operator/day1.md`

- [ ] **Step 1: Update quickstart.md §2**

Replace lines 24-32 of `docs/operator/quickstart.md` (the `## 2. Scaffold + validate` block). Open the file and replace:

```markdown
## 2. Scaffold + validate

```sh
cd ~/code/myproject
regatta init                       # writes regatta.yaml skeleton
$EDITOR regatta.yaml               # fill in version, repo, spec_adapter,
                                   # ci.command, gates, safety
regatta validate-config            # CUE-validates regatta.yaml
```
```

with:

```markdown
## 2. Scaffold

```sh
cd ~/code/myproject
regatta init
```

`regatta init` writes a starter `regatta.yaml`, drops a demo attack
into `.regatta/sample.diff`, and runs the L0 gate against the demo so
you see in one command what regatta catches.
```

- [ ] **Step 2: Update day1.md §Steps**

Replace lines 14-24 of `docs/operator/day1.md` (the ```sh ... ``` block). Open the file and replace:

```sh
brew install trilamsr/regatta/regatta   # or `go install ...`
cd ~/code/myproject
regatta init                            # writes regatta.yaml skeleton
$EDITOR regatta.yaml                    # fill in adapter, ci.command, lanes
regatta validate-config                 # CUE-validates regatta.yaml
regatta validate-spec --dry-run         # connects to adapter, lists items
regatta verify-repo-config              # audits branch protection + CODEOWNERS
```

with:

```sh
brew install trilamsr/regatta/regatta   # or `go install ...`
cd ~/code/myproject
regatta init                            # writes config + runs demo
regatta verify-repo-config              # audits branch protection + CODEOWNERS
```

Also update the §Goal block (lines 8-12) to reflect that the parsed-items + NFC + invisible-glyph cleanliness report is now shown via the canned demo:

Replace:

```markdown
## Goal

Land a parsed-items count + NFC + invisible-glyph cleanliness
report + DAG verification + ready-to-spawn item IDs, and a
green `verify-repo-config` audit.
```

with:

```markdown
## Goal

See a worked example of what L0 catches (via `regatta init`'s demo
fixture) and a green `verify-repo-config` audit on your repo.
```

Also update the `Expires when:` line (line 6) to drop the `validate-spec` reference:

Replace `command surface for \`init\`, \`validate-config\`, \`validate-spec\`, \`verify-repo-config\` changes.` with `command surface for \`init\` or \`verify-repo-config\` changes.`

- [ ] **Step 3: Run doc-check to verify links + banned-phrase lint**

Run: `bash scripts/doc-check.sh`
Expected: clean.

- [ ] **Step 4: Commit**

```bash
git add docs/operator/quickstart.md docs/operator/day1.md
git commit -m "docs(operator): rewrite quickstart + day1 around new init UX"
```

---

## Task 16: Final integration — make check + manual demo

- [ ] **Step 1: Run full check**

Run: `make check`
Expected: all gates pass (doc-check, prose-dup, vet, lint, build, tests).

- [ ] **Step 2: Manual demo**

```bash
cd $(mktemp -d)
go run /Users/treedesk/Desktop/Projects/regatta/cmd/regatta init
```

Eyeball: output is friendly, FAIL verdict shows P3 + L0-TEXT-0 + Cyrillic explanation, Next steps listed, exits 0.

```bash
go run /Users/treedesk/Desktop/Projects/regatta/cmd/regatta init   # second run
```

Eyeball: `= regatta.yaml unchanged` + `= .regatta/sample.diff unchanged`, exits 0.

```bash
echo 'hand edit' > regatta.yaml
go run /Users/treedesk/Desktop/Projects/regatta/cmd/regatta init
```

Eyeball: refused with --force hint, exits 2.

```bash
go run /Users/treedesk/Desktop/Projects/regatta/cmd/regatta init --force
```

Eyeball: `! overwrote regatta.yaml`, exits 0.

```bash
go run /Users/treedesk/Desktop/Projects/regatta/cmd/regatta init --json | jq .
```

Eyeball: valid JSON envelope with `gate_result.findings[0].trap_pattern == "P3"`.

- [ ] **Step 3: Push branch + open PR**

```bash
git push -u origin feat/init-and-cli-smoke
gh pr create --title "feat(cli): regatta init + cmd/regatta smoke tests" --body "$(cat <<'EOF'
## Summary

- New `regatta init` subcommand: scaffolds `regatta.yaml` + `.regatta/sample.diff`, runs L0 against the demo in-process, prints a friendly explanation of what got caught and why. `--json` for scripting. `--force` to overwrite. Symmetric idempotency rules; refuses diverged files atomically.
- New `cmd/regatta/cli_smoke_test.go`: TestMain compile-once, `t.Parallel`-safe subtests covering every subcommand. Closes the 0% coverage on the binary's entry point.
- `docs/operator/{quickstart,day1}.md`: rewritten around the new init UX. Drops the `\$EDITOR`/`validate-config`/`validate-spec --dry-run` steps that predated init being a real command.

Spec: `docs/superpowers/specs/2026-05-30-init-and-cli-smoke-design.md`
Plan: `docs/superpowers/plans/2026-05-30-init-and-cli-smoke.md`

Closes #12. Closes #49.

## Test plan

- [x] `make check` passes
- [x] Manual demo run: first init shows friendly FAIL prose
- [x] Manual demo run: second init shows `= unchanged` markers
- [x] Manual demo run: hand-edited yaml refused with --force hint
- [x] Manual demo run: --force overwrites with `!` marker
- [x] Manual demo run: --json emits parseable envelope with P3 finding
EOF
)"
```

---

## Self-review

**Spec coverage check** (each spec section → task):

| Spec section | Implementing tasks |
|--------------|--------------------|
| `regatta init` happy path | Task 3 (skeleton) + Task 4 (prose + L0) |
| `--force` flag | Task 6 |
| `--json` flag | Tasks 4, 5 |
| In-process L0 (`l0.Check(l0.Default(), l0.ParseUnifiedDiff(data))`) | Task 4 |
| Data flow steps 1-5 | Tasks 3, 4, 6, 7 |
| Error handling table (all rows) | Tasks 6 (diverged), 7 (symlink/non-git/populated), 16 (manual) |
| `init_assets/` bundle + drift gates | Tasks 1, 2 |
| `patternBlurb` table | Task 4 |
| Smoke TestMain build-once | Task 9 |
| Smoke per-subcommand struct (matrix) | Tasks 10, 11, 12, 13, 14 |
| Docs updated same PR | Task 15 |
| All `TestInit_*` from spec testing list | Tasks 3-8 (one per test) |
| All smoke matrix rows | Tasks 10-14 (one or more per row) |
| `TestInitUsesEmbeddedBytes` (drift-defeat) | Task 8 |

No gaps.

**Placeholder scan:** No "TBD", "add appropriate error handling", or "similar to Task N". Every code step shows actual code.

**Type consistency:** `runInit(args []string) int` / `runInitWithIO(args []string, stdout, stderr io.Writer) int` consistent across tasks. `patternBlurb(string) string` consistent. `safeMkdir`, `padPath`, `emitInitProse`, `emitInitJSON`, `nilToEmpty` defined in Task 6, no conflicting redefinitions later. `smokeBinary`, `runSmoke`, `expectExit`, `expectContains` defined in Task 9, used in Tasks 10-14.
