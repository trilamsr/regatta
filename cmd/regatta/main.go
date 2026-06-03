// regatta is the autonomous-agent-fleet CLI.
//
// Subcommands shipped today (pre-fleet):
//
//	regatta l0 <diff-file>          Run L0 against a unified diff.
//	regatta l0-refs ...             Run L0 against git refs (merge-base diff).
//	regatta l0-merge ...            Re-run L0 on a merge commit vs its first parent.
//	regatta validate-config         CUE-validate regatta.yaml.
//	regatta verify-repo-config      Audit a GitHub repo against the P2 canonical recipe.
//	regatta serve                   Run the orchestrator daemon (skeleton).
//	regatta program plan            One-shot decompose a parent WorkItem (kind: program) into a signed ProgramBrief.
//	regatta program verify-handoff  Structurally validate (+ optionally verify HMAC of) a handoff.json.
//	regatta program show            Print brief metadata (incl engine_version + dirty flag) as JSON.
//	regatta program replay-skew-check  Compare brief.engine_version against current binary; --strict refuses on skew.
//	regatta digest                  Generate docs/digests/<date>.md from Prom metrics + degraded placeholders.
//	regatta version                 Print build info.
//
// All other subcommands from docs/design.md are pending implementation.
package main

import (
	"fmt"
	"io"
	"os"
)

const version = "0.0.1-dev"

const (
	subcmdProgram          = "program"
	subcmdServe            = "serve"
	subcmdValidateConfig   = "validate-config"
	subcmdInit             = "init"
	subcmdVerifyRepoConfig = "verify-repo-config"
	subcmdApproval         = "approval"
	subcmdKeys             = "keys"
	subcmdDigest           = "digest"
)

// subcmdAudit is declared in audit.go (its run function lives there).

// subcommand binds a CLI verb to its run function. The table is the
// single source of truth for dispatch and is asserted by
// TestMain_DispatchTableCoversAllCommands to stay complete.
type subcommand struct {
	name string
	run  func(args []string) int
}

var subcommands = []subcommand{
	{"l0", runL0},
	{"l0-refs", runL0Refs},
	{"l0-merge", runL0Merge},
	{subcmdVerifyRepoConfig, runVerifyRepoConfig},
	{subcmdProgram, runProgram},
	{subcmdServe, runServe},
	{subcmdValidateConfig, runValidateConfig},
	{subcmdInit, runInit},
	{subcmdApproval, runApproval},
	{subcmdKeys, runKeys},
	{subcmdDigest, runDigest},
	{subcmdAudit, runAudit},
	{subcmdSecret, runSecret},
	{subcmdReloadSecrets, runReloadSecrets},
	{subcmdStatus, runStatus},
	{subcmdCost, runCost},
	{subcmdResume, runResume},
	{subcmdSelfImprove, runSelfImprove},
}

func main() {
	if len(os.Args) < 2 {
		usage(os.Stderr)
		os.Exit(2)
	}
	arg := os.Args[1]
	for _, sc := range subcommands {
		if sc.name == arg {
			os.Exit(sc.run(os.Args[2:]))
		}
	}
	switch arg {
	case "version", "-v", "--version":
		fmt.Println("regatta", version)
	case "help", "-h", "--help":
		usage(os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "regatta: unknown subcommand %q\n", arg)
		usage(os.Stderr)
		os.Exit(2)
	}
}

func usage(w io.Writer) {
	_, _ = fmt.Fprint(w, `Usage:
  regatta l0 <diff-file>                              Run L0 against a unified diff
  regatta l0-refs -repo <dir> -base <ref> -head <ref> Run L0 against git refs (merge-base diff)
  regatta l0-merge -repo <dir> -commit <sha>          Re-run L0 on a merge commit vs first parent
  regatta validate-config                             CUE-validate regatta.yaml
  regatta verify-repo-config                          Audit GitHub repo against P2 recipe
  regatta serve                                       Run the orchestrator daemon (skeleton)
  regatta program plan <work-item.json>               One-shot decompose into signed ProgramBrief
  regatta program verify-handoff <path>               Validate a handoff.json (schema + optional HMAC)
  regatta program show <brief.json>                   Print brief metadata (engine_version + dirty + counts) as JSON
  regatta program replay-skew-check [--strict] <p>    Compare brief engine_version to current binary (WARN | STRICT)
  regatta approval decide --token T --decision D ...  Record an approval-gate decision from a signed token
  regatta approval list [--mine ID] [--format F]      List pending approvals (table | json)
  regatta init                                        Scaffold regatta.yaml + run L0 demo
  regatta keys re-sign-briefs -old-key-id ...         Re-sign program briefs after HMAC key rotation
  regatta digest --date YYYY-MM-DD                    Generate docs/digests/<date>.md from Prom metrics
  regatta audit verify --run-id <id> [--db <path>]    Walk recorded gate verdicts; print tool/version/det/hmac-status per gate
  regatta secret set|get|status                       Manage operator credentials in OS keychain (macOS) / pass (Linux) with env fallback
  regatta reload-secrets [--pidfile P]                 Send SIGHUP to regatta-serve so the secret cache re-fetches (W6 §7 rotation drill)
  regatta status [--refresh DUR] [--once]             Terminal live-status TUI (5 panels: subagents, PRs, merges, cost, green-clock)
  regatta cost status [--db <path>] [--config <path>] Print W5 global daily-cap state (24h spend, cap, scheduler state, resume horizon)
  regatta resume [--actor <id>] [--db <path>]         Lift the W5 cost-cap throttle until the next day rollover (operator override audit event)
  regatta self-improve scan [--since=7d] [--apply]    Run rule scan over substrate; --apply files GH issues (default dry-run)
  regatta self-improve rules                          List registered self-improvement rules
  regatta version                                     Print build info
  regatta help                                        This message

All L0 commands emit a GateResult JSON document to stdout. Exit code 0
on pass, 1 on fail, 2 on usage error.

l0-refs computes the diff base as git merge-base(base, head), closing
the TOCTOU window where the base branch tightens a criterion while a
PR is in flight (testdata/README.md §1).

l0-merge re-runs the gate on a merge commit against its first parent.
This catches rubber-stamp merges that revert criterion tightening
landed on the base after the PR passed (testdata/README.md §7).

verify-repo-config requires GITHUB_TOKEN and -owner/-repo flags.

serve runs the markdown_catalog adapter against <root>/.regatta/items
and spawns a stub agent for each ready item. Pass --tick-once to run
a single poll+schedule cycle and exit.

program plan:
  -model        <id>   Claude model id (default "claude-opus-4-7")
  -hmac-key-env <ENV>  Env var holding the HMAC key (required)
  -hmac-key-id  <ID>   key_id stamped into the signature (default "k1")
  Requires ANTHROPIC_API_KEY in the environment.

program verify-handoff:
  -hmac-key-env <ENV>   If set, verify handoff.signature against the key
                        held in the named environment variable. Without
                        this flag, only structural validation runs.
  -hmac-key-id <ID>     key_id to assign in the keyring (default "k1").
`)
}
