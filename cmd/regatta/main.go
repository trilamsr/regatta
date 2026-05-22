// regatta is the autonomous-agent-fleet CLI.
//
// Subcommands shipped today (pre-fleet):
//
//	regatta l0 <diff-file>          Run L0 spec-immutability check against a unified diff.
//	regatta verify-repo-config      Audit a GitHub repo against the P2 canonical recipe.
//	regatta mission plan            One-shot decompose a parent WorkItem (kind: mission) into a signed FeaturePlan.
//	regatta mission verify-handoff  Structurally validate (+ optionally verify HMAC of) a handoff.json.
//	regatta version                 Print build info.
//
// All other subcommands from docs/design.md are pending implementation.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/trilamsr/regatta/internal/l0"
	"github.com/trilamsr/regatta/internal/missions"
	"github.com/trilamsr/regatta/internal/verifyrepo"
	"github.com/trilamsr/regatta/schemas"
)

const version = "0.0.1-dev"

func main() {
	if len(os.Args) < 2 {
		usage(os.Stderr)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "l0":
		os.Exit(runL0(os.Args[2:]))
	case "verify-repo-config":
		os.Exit(runVerifyRepoConfig(os.Args[2:]))
	case "mission":
		os.Exit(runMission(os.Args[2:]))
	case "version", "-v", "--version":
		fmt.Println("regatta", version)
	case "help", "-h", "--help":
		usage(os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "regatta: unknown subcommand %q\n", os.Args[1])
		usage(os.Stderr)
		os.Exit(2)
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `Usage:
  regatta l0 <diff-file>                       Run L0 spec-immutability check
  regatta verify-repo-config                   Audit GitHub repo against P2 recipe
  regatta mission plan <work-item.json>        One-shot decompose into signed FeaturePlan
  regatta mission verify-handoff <path>        Validate a handoff.json (schema + optional HMAC)
  regatta version                              Print build info
  regatta help                                 This message

L0 reads a unified diff from <diff-file> ("-" for stdin) and emits a
GateResult JSON document to stdout. Exit code 0 on pass, 1 on fail,
2 on usage error.

verify-repo-config requires GITHUB_TOKEN and -owner/-repo flags.

mission plan:
  -model        <id>   Claude model id (default "claude-opus-4-7")
  -hmac-key-env <ENV>  Env var holding the HMAC key (required)
  -hmac-key-id  <ID>   key_id stamped into the signature (default "k1")
  Requires ANTHROPIC_API_KEY in the environment.

mission verify-handoff:
  -hmac-key-env <ENV>   If set, verify handoff.signature against the key
                        held in the named environment variable. Without
                        this flag, only structural validation runs.
  -hmac-key-id <ID>     key_id to assign in the keyring (default "k1").
`)
}

func runL0(args []string) int {
	fs := flag.NewFlagSet("l0", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: regatta l0 <diff-file>  ('-' for stdin)")
	}
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}
	var data []byte
	var err error
	if fs.Arg(0) == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(fs.Arg(0))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "regatta l0:", err)
		return 2
	}
	result := l0.Check(l0.Default(), l0.ParseUnifiedDiff(string(data)))
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(result)
	if result.Verdict != "pass" {
		return 1
	}
	return 0
}

func runVerifyRepoConfig(args []string) int {
	fs := flag.NewFlagSet("verify-repo-config", flag.ExitOnError)
	owner := fs.String("owner", "", "GitHub repo owner")
	repo := fs.String("repo", "", "GitHub repo name")
	branch := fs.String("branch", "main", "Protected branch name")
	asJSON := fs.Bool("json", false, "Emit JSON instead of human-readable summary")
	_ = fs.Parse(args)

	if *owner == "" || *repo == "" {
		fs.Usage()
		fmt.Fprintln(os.Stderr, "regatta verify-repo-config: -owner and -repo required")
		return 2
	}

	res, err := verifyrepo.Run(context.Background(), verifyrepo.Config{
		Owner:  *owner,
		Repo:   *repo,
		Branch: *branch,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "regatta verify-repo-config:", err)
		return 2
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(res)
	} else {
		for _, c := range res.Checks {
			mark := "✓"
			if !c.Passed {
				mark = "✗"
			}
			fmt.Printf("%s %s -- %s\n", mark, c.ID, c.Title)
			if c.Detail != "" {
				fmt.Printf("    %s\n", c.Detail)
			}
			if !c.Passed && c.Rationale != "" {
				fmt.Printf("    rationale: %s\n", c.Rationale)
			}
		}
		if res.OK {
			fmt.Println("\nverify-repo-config: PASS -- repo is configured for Regatta deployment")
		} else {
			fmt.Printf("\nverify-repo-config: FAIL -- %d check(s) failed: %v\n", len(res.FailedOK), res.FailedOK)
		}
	}
	if !res.OK {
		return 1
	}
	return 0
}

// runMission dispatches the `mission ...` subcommand tree.
func runMission(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "regatta mission: expected sub-subcommand (verify-handoff)")
		return 2
	}
	switch args[0] {
	case "plan":
		return runMissionPlan(args[1:])
	case "verify-handoff":
		return runMissionVerifyHandoff(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "regatta mission: unknown subcommand %q\n", args[0])
		return 2
	}
}

// runMissionPlan: read a WorkItem from a JSON file on disk, call
// the Anthropic planner, validate + sign the resulting FeaturePlan,
// emit to stdout. Source adapters are deferred -- for v1, operators
// hand-author the parent WorkItem JSON file or extract it from
// their adapter into one.
func runMissionPlan(args []string) int {
	fs := flag.NewFlagSet("mission plan", flag.ExitOnError)
	model := fs.String("model", "claude-opus-4-7", "Claude model id")
	keyEnv := fs.String("hmac-key-env", "", "Env var holding HMAC key (required)")
	keyID := fs.String("hmac-key-id", "k1", "key_id to stamp into signature")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: regatta mission plan <work-item.json>")
		fs.PrintDefaults()
	}
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}
	if *keyEnv == "" {
		fmt.Fprintln(os.Stderr, "regatta mission plan: -hmac-key-env is required")
		return 2
	}
	key := os.Getenv(*keyEnv)
	if key == "" {
		fmt.Fprintf(os.Stderr, "regatta mission plan: $%s is empty\n", *keyEnv)
		return 2
	}

	raw, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, "regatta mission plan:", err)
		return 2
	}
	var parent schemas.WorkItem
	if err := json.Unmarshal(raw, &parent); err != nil {
		fmt.Fprintln(os.Stderr, "regatta mission plan: parse work-item.json:", err)
		return 2
	}
	if parent.ID == "" || len(parent.AcceptanceCriteria) == 0 {
		fmt.Fprintln(os.Stderr, "regatta mission plan: work-item must have id and acceptance_criteria")
		return 2
	}

	client, err := missions.NewAnthropicPlanner(*model)
	if err != nil {
		fmt.Fprintln(os.Stderr, "regatta mission plan:", err)
		return 2
	}

	plan, err := missions.Run(context.Background(), missions.PlannerOptions{
		Client:    client,
		HMACKey:   []byte(key),
		HMACKeyID: *keyID,
	}, parent)
	if err != nil {
		fmt.Fprintln(os.Stderr, "regatta mission plan:", err)
		return 1
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(plan)
	return 0
}

func runMissionVerifyHandoff(args []string) int {
	fs := flag.NewFlagSet("mission verify-handoff", flag.ExitOnError)
	keyEnv := fs.String("hmac-key-env", "", "Env var holding the HMAC key (if set, verify signature)")
	keyID := fs.String("hmac-key-id", "k1", "key_id to expect in the signature")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: regatta mission verify-handoff <handoff.json>")
		fs.PrintDefaults()
	}
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}

	h, err := missions.LoadAndValidate(fs.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, "regatta mission verify-handoff:", err)
		return 1
	}

	report := struct {
		MissionID         string `json:"mission_id"`
		FeatureID         string `json:"feature_id"`
		WorkerRunID       string `json:"worker_run_id"`
		SuccessState      string `json:"success_state"`
		SchemaOK          bool   `json:"schema_ok"`
		SignatureVerified bool   `json:"signature_verified"`
		SignatureChecked  bool   `json:"signature_checked"`
	}{
		MissionID:    h.MissionID,
		FeatureID:    h.FeatureID,
		WorkerRunID:  h.WorkerRunID,
		SuccessState: h.SuccessState,
		SchemaOK:     true,
	}

	if *keyEnv != "" {
		report.SignatureChecked = true
		key := os.Getenv(*keyEnv)
		if key == "" {
			fmt.Fprintf(os.Stderr, "regatta mission verify-handoff: $%s is empty\n", *keyEnv)
			return 2
		}
		keyring := map[string][]byte{*keyID: []byte(key)}
		if err := h.VerifySignature(keyring); err != nil {
			fmt.Fprintln(os.Stderr, "regatta mission verify-handoff: signature:", err)
			report.SignatureVerified = false
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			_ = enc.Encode(report)
			return 1
		}
		report.SignatureVerified = true
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(report)
	return 0
}
