// regatta is the autonomous-agent-fleet CLI. Run `regatta help` for the
// tiered command list; `regatta help <command>` prints per-command flags.
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
	subcmdConfigValidate   = "config-validate" // R31-Bug-C alias.
	subcmdInit             = "init"
	subcmdVerifyRepoConfig = "verify-repo-config"
	subcmdApproval         = "approval"
	subcmdKeys             = "keys"
	subcmdDigest           = "digest"
	subcmdInstallService   = "install-service"
	subcmdUninstallService = "uninstall-service"
	// subcmdDoctor is declared in doctor.go.

	flagLongHelp = "--help"
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
	// Alias: docs + operator muscle memory invoke `config-validate` (R31-Bug-C).
	{subcmdConfigValidate, runValidateConfig},
	{subcmdInit, runInit},
	{subcmdApproval, runApproval},
	{subcmdKeys, runKeys},
	{subcmdDigest, runDigest},
	{subcmdAudit, runAudit},
	{subcmdSecret, runSecret},
	{subcmdReloadSecrets, runReloadSecrets},
	{subcmdStatus, runStatus},
	{subcmdTriggers, runTriggers},
	{subcmdCost, runCost},
	{subcmdResume, runResume},
	{subcmdSelfImprove, runSelfImprove},
	{subcmdMerge, runMerge},
	{subcmdReview, runReview},
	{subcmdInstallService, runInstallService},
	{subcmdUninstallService, runUninstallService},
	{subcmdDoctor, runDoctor},
	{subcmdAgents, runAgents},
	{subcmdEvents, runEvents},
	{subcmdHealthcheck, runHealthcheck},
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
		emitVersion(os.Args[2:])
	case "help", "-h", flagLongHelp:
		// `regatta help <command>` delegates to the subcommand's own -h so
		// the footer promise in usage() ("Use \"regatta help <command>\"")
		// resolves to real per-flag output rather than a dead lookup.
		if len(os.Args) >= 3 {
			for _, sc := range subcommands {
				if sc.name == os.Args[2] {
					os.Exit(sc.run([]string{"-h"}))
				}
			}
		}
		usage(os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "regatta: unknown subcommand %q\n", arg)
		usage(os.Stderr)
		os.Exit(2)
	}
}

func usage(w io.Writer) {
	_, _ = fmt.Fprint(w, `Usage: regatta <command> [flags]

Commands:
  regatta init                    Scaffold regatta.yaml + run L0 demo
  regatta serve                   Run the orchestrator daemon
  regatta doctor                  Preflight: secrets, binaries, gh auth, git, config, branch protection
  regatta validate-config         CUE-validate regatta.yaml
  regatta verify-repo-config      Audit GitHub repo against P2 branch-protection recipe
  regatta status                  Terminal live-status TUI (subagents, PRs, merges, cost, green-clock)

Approvals:
  regatta approval                Decide or list pending approval-gate tokens
  regatta keys                    Rotate HMAC keys and re-sign program briefs

Advanced:
  regatta l0 | l0-refs | l0-merge  Run the L0 spec-immutability gate against a diff, refs, or merge commit
  regatta program                 Plan / verify / show / replay-skew-check a ProgramBrief
  regatta digest                  Generate docs/digests/<date>.md from Prom metrics
  regatta audit                   Walk recorded gate verdicts (tool/version/det/hmac-status)
  regatta secret                  Manage credentials in OS keychain (macOS) / pass (Linux) / env
  regatta reload-secrets          SIGHUP regatta-serve so the secret cache re-fetches
  regatta triggers                List active triggers (threshold | window | day-count | status)
  regatta cost                    Print daily-cap state (24h spend, cap, scheduler state)
  regatta resume                  Lift the cost-cap throttle until the next day rollover
  regatta self-improve            Scan substrate for improvement rules; list registered rules
  regatta review                  List recent L4 verdicts; wire reviewer-bot via CODEOWNERS
  regatta merge                   Print merge-worker status
  regatta install-service         Install OS-native supervisor (launchd or systemd)
  regatta uninstall-service       Reverse install-service (idempotent)
  regatta agents                  List agents from state.db (read-only)
  regatta events                  Tail substrate events (table | json)
  regatta healthcheck             Probe /healthz; exit 0 on 200 (docker HEALTHCHECK target)
  regatta version                 Print build info

Use "regatta help <command>" for more info about a command.
`)
}
