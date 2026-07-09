package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/gates/approval"
)

//go:embed init_assets/regatta.yaml init_assets/sample.diff
var initAssets embed.FS

const (
	actionWrite     = "write"
	actionSkip      = "skip"
	actionOverwrite = "overwrite"
	actionDiverge   = "diverge"
)

// runInit is the CLI entry point; thin wrapper around runInitWithIO so tests inject stdout/stderr.
func runInit(args []string) int {
	return runInitWithIO(args, os.Stdout, os.Stderr)
}

func runInitWithIO(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet(subcmdInit, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		_, _ = fmt.Fprintln(fs.Output(), "Usage: regatta init [--force] [--json]")
		_, _ = fmt.Fprintln(fs.Output())
		_, _ = fmt.Fprintln(fs.Output(), "Scaffolds regatta.yaml and a demo attack at .regatta/sample.diff in the")
		_, _ = fmt.Fprintln(fs.Output(), "current directory, then runs the L0 gate against the demo so you see in")
		_, _ = fmt.Fprintln(fs.Output(), "one command what regatta catches.")
		_, _ = fmt.Fprintln(fs.Output())
		_, _ = fmt.Fprintln(fs.Output(), "Idempotent on re-run: files matching the bundled template are skipped;")
		_, _ = fmt.Fprintln(fs.Output(), "files that have diverged cause exit 2 unless --force is passed (in which")
		_, _ = fmt.Fprintln(fs.Output(), "case they are overwritten).")
		_, _ = fmt.Fprintln(fs.Output())
		_, _ = fmt.Fprintln(fs.Output(), "Flags:")
		fs.PrintDefaults()
		_, _ = fmt.Fprintln(fs.Output())
		_, _ = fmt.Fprintln(fs.Output(), "Exit codes: 0 success, 1 internal error, 2 usage/refusal.")
	}
	force := fs.Bool("force", false, "overwrite existing regatta.yaml / .regatta/sample.diff")
	jsonOut := fs.Bool("json", false, "emit JSON envelope instead of friendly prose")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	yamlBytes, err := initAssets.ReadFile("init_assets/regatta.yaml")
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "regatta init: internal: read embedded yaml: %v\n", err)
		return 1
	}
	// Auto-detect owner/name from `git remote get-url origin` so the
	// scaffold matches this repo instead of the embedded placeholder;
	// on parse failure fall back to a placeholder + WARN so the operator
	// sees the failure explicitly.
	yamlBytes = substituteRepoIdentity(yamlBytes, stderr)
	diffBytes, err := initAssets.ReadFile("init_assets/sample.diff")
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "regatta init: internal: read embedded sample.diff: %v\n", err)
		return 1
	}

	// Defense before classification: if .regatta/ exists but is not a
	// real directory (regular file, symlink, device), refuse early so
	// we surface the friendly explanation instead of a generic
	// "not a directory" from os.ReadFile.
	if info, err := os.Lstat(".regatta"); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			_, _ = fmt.Fprintf(stderr, "regatta init: refusing to write: .regatta exists but is not a regular directory (got mode %s). Symlinks and non-directory entries are rejected for security isolation (prevents symlink-injection into paths outside the repo). Remove or rename it, then re-run.\n", info.Mode())
			return 2
		}
	}

	// Atomic refusal: classify before any write so divergence never leaves partial state.
	type decision struct {
		path   string
		blurb  string
		bytes  []byte
		action string
	}
	files := []decision{
		{path: defaultRegattaConfig, blurb: "(your config; L0 gate enabled)", bytes: yamlBytes},
		{path: filepath.Join(".regatta", "sample.diff"), blurb: "(a demo attack against MILESTONES.md)", bytes: diffBytes},
	}
	for i := range files {
		existing, err := os.ReadFile(files[i].path)
		switch {
		case os.IsNotExist(err):
			files[i].action = actionWrite
		case err != nil:
			_, _ = fmt.Fprintf(stderr, "regatta init: stat %s: %v\n", files[i].path, err)
			return 1
		case bytes.Equal(existing, files[i].bytes):
			files[i].action = actionSkip
		case *force:
			files[i].action = actionOverwrite
		default:
			files[i].action = actionDiverge
		}
	}
	// Short-circuit divergence before any write so refusal stays atomic.
	for _, d := range files {
		if d.action == actionDiverge {
			_, _ = fmt.Fprintf(stderr, "regatta init: %s already exists and differs from the bundled template.\n", filepath.ToSlash(d.path))
			_, _ = fmt.Fprintf(stderr, "  To re-init: rm regatta.yaml .regatta/sample.diff\n")
			_, _ = fmt.Fprintf(stderr, "  To overwrite: regatta init --force\n")
			return 2
		}
	}

	for _, d := range files {
		if d.action == actionSkip {
			continue
		}
		if dir := filepath.Dir(d.path); dir != "." {
			if err := safeMkdir(dir); err != nil {
				_, _ = fmt.Fprintf(stderr, "regatta init: %v\n", err)
				return 2
			}
		}
	}

	var written, skipped, overwritten []string
	for _, d := range files {
		// Forward-slash so output matches docs/incidents.md prose across OSes.
		display := filepath.ToSlash(d.path)
		switch d.action {
		case actionWrite:
			if err := os.WriteFile(d.path, d.bytes, 0o600); err != nil {
				_, _ = fmt.Fprintf(stderr, "regatta init: write %s: %v\n", display, err)
				return 1
			}
			if !*jsonOut {
				_, _ = fmt.Fprintf(stdout, "+ wrote %s %s\n", padPath(display), d.blurb)
			}
			written = append(written, display)
		case actionSkip:
			if !*jsonOut {
				_, _ = fmt.Fprintf(stdout, "= %s unchanged\n", display)
			}
			skipped = append(skipped, display)
		case actionOverwrite:
			if err := os.WriteFile(d.path, d.bytes, 0o600); err != nil {
				_, _ = fmt.Fprintf(stderr, "regatta init: write %s: %v\n", display, err)
				return 1
			}
			if !*jsonOut {
				_, _ = fmt.Fprintf(stdout, "! overwrote %s %s\n", padPath(display), d.blurb)
			}
			overwritten = append(overwritten, display)
		}
	}

	// Re-read from disk so operator edits to sample.diff flow into the demo verdict.
	diffPath := files[1].path
	onDisk, err := os.ReadFile(diffPath) //nolint:gosec // G304: diffPath is filepath.Join(".regatta","sample.diff"), a fixed literal, never user input
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "regatta init: re-read %s: %v\n", diffPath, err)
		return 1
	}
	res := approval.L0Check(approval.L0Default(), approval.L0ParseUnifiedDiff(string(onDisk)))

	if *jsonOut {
		if err := emitInitJSON(stdout, written, skipped, overwritten, res); err != nil {
			_, _ = fmt.Fprintf(stderr, "regatta init: encode JSON: %v\n", err)
			return 1
		}
		return 0
	}
	emitInitProse(stdout, res)
	return 0
}

// safeMkdir refuses non-directory targets (symlink/file/device) to defend against pre-created .regatta -> /etc.
func safeMkdir(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return os.MkdirAll(path, 0o700)
	}
	if err != nil {
		return fmt.Errorf("lstat %s: %w", path, err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to write: %s exists but is not a regular directory (got mode %s). Remove or rename it, then re-run", path, info.Mode())
	}
	return nil
}

// padPath right-pads the path so prose columns align.
func padPath(p string) string {
	const width = 22
	if len(p) >= width {
		return p
	}
	return p + strings.Repeat(" ", width-len(p))
}

// emitInitProse renders the demo block straight from the GateResult so L0 fixture changes ripple without a hand-edit here.
func emitInitProse(w io.Writer, res schemas.GateResult) {
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Running L0 gate against the demo to show you what regatta catches:")
	_, _ = fmt.Fprintln(w)
	if res.Verdict != schemas.VerdictFail || len(res.Findings) == 0 {
		_, _ = fmt.Fprintf(w, "  Verdict: %s (no findings)\n", res.Verdict)
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintln(w, "Next steps:")
		_, _ = fmt.Fprintln(w, "  - Run `regatta l0 <your-diff>` on a real PR diff")
		return
	}
	f := res.Findings[0]
	location := ""
	if f.Evidence != nil {
		location = f.Evidence.Path
	}
	_, _ = fmt.Fprintf(w, "  FAIL: spec criterion text changed without citation (%s)\n", f.ID)
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintf(w, "  In %s, the criterion was rewritten. L0 blocks this because\n", location)
	_, _ = fmt.Fprintln(w, "  criteria are the contract between you and the agent: silent")
	_, _ = fmt.Fprintln(w, "  edits move the goalposts.")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "  The catch is sneakier than it looks: the diff replaces Latin")
	_, _ = fmt.Fprintln(w, "  \"A\" with Cyrillic \"А\" (U+0410). They render identically. A")
	_, _ = fmt.Fprintln(w, "  human reviewer scanning the diff sees \"Auth -> Auth\" and")
	_, _ = fmt.Fprintln(w, "  approves; L0 compares NFC-normalized code points and rejects.")
	_, _ = fmt.Fprintln(w)
	blurb := patternBlurb(f.TrapPattern)
	_, _ = fmt.Fprintf(w, "  Trap Pattern %s %s\n", f.TrapPattern, blurb)
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Next steps:")
	_, _ = fmt.Fprintln(w, "  - Run `regatta l0 <your-diff>` on a real PR diff")
	_, _ = fmt.Fprintln(w, "  - Run `regatta verify-repo-config` to audit your repo's branch")
	_, _ = fmt.Fprintln(w, "    protection and CODEOWNERS")
	_, _ = fmt.Fprintln(w, "  - Edit regatta.yaml to enable more gates as they ship")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Done.")
}

// patternBlurbs maps Trap Catalog pattern IDs to one-line summaries; unknown IDs fall back via patternBlurb so new L0 patterns never crash init.
var patternBlurbs = map[string]string{
	"P1":  "(deterministic gate before AI gate on destructive ops)",
	"P2":  "(two-key approval on irreversible actions)",
	"P3":  "(fetch trusted instructions from main, treat all other text as data)",
	"P4":  "(least-privilege, ephemeral, environment-scoped credentials)",
	"P5":  "(out-of-band supervisor for limits and kill-switches)",
	"P6":  "(verified grounding for any outward-facing claim)",
	"P7":  "(schema-level scope constraints, not prompt-level)",
	"P8":  "(spend / iteration brakes with mandatory re-approval)",
	"P9":  "(sensitive context segregation)",
	"P10": "(render-the-invisible + signed prompt artifacts)",
	"P11": "(agent-artifact release pipelines are themselves attack surface)",
	"P12": "(inbound vulnerability signals default-escalate)",
	"P13": "(judge-LLM lineage isolation)",
}

func patternBlurb(p string) string {
	if b, ok := patternBlurbs[p]; ok {
		num := strings.TrimPrefix(p, "P")
		return b + ". See docs/incidents.md#pattern-" + num + "."
	}
	return "(uncatalogued). See docs/incidents.md."
}

// emitInitJSON writes the --json mode envelope so callers can script `regatta init` output.
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

// gitRemoteURL runs `git remote get-url origin` in cwd and returns the
// trimmed URL; overridden in tests so we do not shell out.
var gitRemoteURL = func() (string, error) {
	out, err := exec.Command("git", "remote", "get-url", "origin").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// remoteRE matches github.com URLs in ssh (git@) and https forms, e.g.
// `git@github.com:trilamsr/regatta.git` or `https://github.com/trilamsr/regatta`.
var remoteRE = regexp.MustCompile(`github\.com[:/]([^/]+)/([^/.]+)`)

// substituteRepoIdentity rewrites the embedded `owner:` / `name:` lines
// from `git remote get-url origin`. Non-git directories keep the embedded
// `example / myproject` placeholder silently (no git remote means the
// operator will hand-edit anyway). When the git command succeeds but the
// URL doesn't match a github.com form we emit a WARN and drop
// `YOUR_ORG # replace` / `YOUR_REPO # replace` so the gap is visible.
func substituteRepoIdentity(yamlBytes []byte, stderr io.Writer) []byte {
	url, err := gitRemoteURL()
	if err != nil {
		return yamlBytes
	}
	m := remoteRE.FindStringSubmatch(url)
	if len(m) != 3 {
		_, _ = fmt.Fprintf(stderr, "regatta init: WARN: `git remote get-url origin` returned %q which does not match a github.com URL; wrote placeholders in regatta.yaml\n", url)
		yamlBytes = bytes.Replace(yamlBytes, []byte("  owner: example\n"), []byte("  owner: YOUR_ORG # replace\n"), 1)
		yamlBytes = bytes.Replace(yamlBytes, []byte("  name: myproject\n"), []byte("  name: YOUR_REPO # replace\n"), 1)
		return yamlBytes
	}
	yamlBytes = bytes.Replace(yamlBytes, []byte("  owner: example\n"), []byte("  owner: "+m[1]+"\n"), 1)
	yamlBytes = bytes.Replace(yamlBytes, []byte("  name: myproject\n"), []byte("  name: "+m[2]+"\n"), 1)
	return yamlBytes
}
