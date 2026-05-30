package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/gates/l0"
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

	// Classify each file's action BEFORE any write so divergence
	// causes an atomic refusal — never partial state.
	type decision struct {
		path   string
		blurb  string
		bytes  []byte
		action string // "write", "skip", "overwrite", "diverge"
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
	// Short-circuit on any divergence — print one friendly error per
	// diverged file and refuse before any write happens.
	for _, d := range files {
		if d.action == "diverge" {
			fmt.Fprintf(stderr, "regatta init: %s already exists and differs from the bundled template.\n", d.path)
			fmt.Fprintf(stderr, "  To re-init: rm regatta.yaml .regatta/sample.diff\n")
			fmt.Fprintf(stderr, "  To overwrite: regatta init --force\n")
			return 2
		}
	}

	// Ensure .regatta/ exists if any file inside it needs writing.
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

	// Apply.
	var written, skipped, overwritten []string
	for _, d := range files {
		switch d.action {
		case "write":
			if err := os.WriteFile(d.path, d.bytes, 0o644); err != nil {
				fmt.Fprintf(stderr, "regatta init: write %s: %v\n", d.path, err)
				return 1
			}
			if !*jsonOut {
				fmt.Fprintf(stdout, "+ wrote %s %s\n", padPath(d.path), d.blurb)
			}
			written = append(written, d.path)
		case "skip":
			if !*jsonOut {
				fmt.Fprintf(stdout, "= %s unchanged\n", d.path)
			}
			skipped = append(skipped, d.path)
		case "overwrite":
			if err := os.WriteFile(d.path, d.bytes, 0o644); err != nil {
				fmt.Fprintf(stderr, "regatta init: write %s: %v\n", d.path, err)
				return 1
			}
			if !*jsonOut {
				fmt.Fprintf(stdout, "! overwrote %s %s\n", padPath(d.path), d.blurb)
			}
			overwritten = append(overwritten, d.path)
		}
	}

	// Re-read sample.diff so the demo verdict reflects what's on
	// disk (operator might inspect or edit it).
	diffPath := files[1].path
	onDisk, err := os.ReadFile(diffPath)
	if err != nil {
		fmt.Fprintf(stderr, "regatta init: re-read %s: %v\n", diffPath, err)
		return 1
	}
	res := l0.Check(l0.Default(), l0.ParseUnifiedDiff(string(onDisk)))

	if *jsonOut {
		if err := emitInitJSON(stdout, written, skipped, overwritten, res); err != nil {
			fmt.Fprintf(stderr, "regatta init: encode JSON: %v\n", err)
			return 1
		}
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
	location := ""
	if f.Evidence != nil {
		location = f.Evidence.Path
	}
	fmt.Fprintf(w, "  FAIL: spec criterion text changed without citation (%s)\n", f.ID)
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  In %s, the criterion was rewritten. L0 blocks this because\n", location)
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
func emitInitJSON(w io.Writer, written, skipped, overwritten []string, res schemas.GateResult) error {
	type envelope struct {
		Written     []string           `json:"written"`
		Skipped     []string           `json:"skipped"`
		Overwritten []string           `json:"overwritten"`
		GateResult  schemas.GateResult `json:"gate_result"`
	}
	env := envelope{
		Written:     nilToEmpty(written),
		Skipped:     nilToEmpty(skipped),
		Overwritten: nilToEmpty(overwritten),
		GateResult:  res,
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(env)
}

func nilToEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
