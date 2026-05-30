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
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	validateconfig "github.com/trilamsr/regatta/internal/config/validate"
	verifyrepo "github.com/trilamsr/regatta/internal/config/verify"
	"github.com/trilamsr/regatta/internal/gates/l0"
	"github.com/trilamsr/regatta/internal/orchestrator"
	"github.com/trilamsr/regatta/internal/orchestrator/adapter"
	"github.com/trilamsr/regatta/internal/orchestrator/reaper"
	"github.com/trilamsr/regatta/internal/orchestrator/spawner"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/program"
	"github.com/trilamsr/regatta/contracts/schemas"
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
	case "l0-refs":
		os.Exit(runL0Refs(os.Args[2:]))
	case "l0-merge":
		os.Exit(runL0Merge(os.Args[2:]))
	case "verify-repo-config":
		os.Exit(runVerifyRepoConfig(os.Args[2:]))
	case "program":
		os.Exit(runProgram(os.Args[2:]))
	case "serve":
		os.Exit(runServe(os.Args[2:]))
	case "validate-config":
		os.Exit(runValidateConfig(os.Args[2:]))
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
	_, _ = fmt.Fprint(w, `Usage:
  regatta l0 <diff-file>                              Run L0 against a unified diff
  regatta l0-refs -repo <dir> -base <ref> -head <ref> Run L0 against git refs (merge-base diff)
  regatta l0-merge -repo <dir> -commit <sha>          Re-run L0 on a merge commit vs first parent
  regatta validate-config                             CUE-validate regatta.yaml
  regatta verify-repo-config                          Audit GitHub repo against P2 recipe
  regatta serve                                       Run the orchestrator daemon (skeleton)
  regatta program plan <work-item.json>               One-shot decompose into signed ProgramBrief
  regatta program verify-handoff <path>               Validate a handoff.json (schema + optional HMAC)
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

func runL0(args []string) int {
	fs := flag.NewFlagSet("l0", flag.ExitOnError)
	fs.Usage = func() {
		_, _ = fmt.Fprintln(fs.Output(), "Usage: regatta l0 <diff-file>  ('-' for stdin)")
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
	return emitL0(result)
}

func runL0Refs(args []string) int {
	fs := flag.NewFlagSet("l0-refs", flag.ExitOnError)
	repoDir := fs.String("repo", ".", "Path to the git repository")
	baseRef := fs.String("base", "", "Base ref (branch, tag, or sha)")
	headRef := fs.String("head", "", "Head ref (branch, tag, or sha)")
	_ = fs.Parse(args)
	if *baseRef == "" || *headRef == "" {
		fs.Usage()
		fmt.Fprintln(os.Stderr, "regatta l0-refs: -base and -head required")
		return 2
	}
	result, err := l0.CheckRefs(context.Background(), l0.Default(), *repoDir, *baseRef, *headRef)
	if err != nil {
		fmt.Fprintln(os.Stderr, "regatta l0-refs:", err)
		return 2
	}
	return emitL0(result)
}

func runL0Merge(args []string) int {
	fs := flag.NewFlagSet("l0-merge", flag.ExitOnError)
	repoDir := fs.String("repo", ".", "Path to the git repository")
	commit := fs.String("commit", "", "Merge commit sha")
	_ = fs.Parse(args)
	if *commit == "" {
		fs.Usage()
		fmt.Fprintln(os.Stderr, "regatta l0-merge: -commit required")
		return 2
	}
	result, err := l0.CheckMergeCommit(context.Background(), l0.Default(), *repoDir, *commit)
	if err != nil {
		fmt.Fprintln(os.Stderr, "regatta l0-merge:", err)
		return 2
	}
	return emitL0(result)
}

func emitL0(result schemas.GateResult) int {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(result)
	if result.Verdict != schemas.VerdictPass {
		return 1
	}
	return 0
}

// laneCapsFlag implements flag.Value for repeated `-lane name:cap` flags.
type laneCapsFlag map[string]int

func (l laneCapsFlag) String() string {
	parts := make([]string, 0, len(l))
	for k, v := range l {
		parts = append(parts, fmt.Sprintf("%s:%d", k, v))
	}
	return strings.Join(parts, ",")
}

func (l laneCapsFlag) Set(s string) error {
	name, capStr, ok := strings.Cut(s, ":")
	if !ok {
		return fmt.Errorf("expected name:cap, got %q", s)
	}
	n, err := strconv.Atoi(capStr)
	if err != nil {
		return fmt.Errorf("invalid cap %q: %w", capStr, err)
	}
	if n < 0 {
		return fmt.Errorf("cap must be non-negative, got %d", n)
	}
	l[strings.TrimSpace(name)] = n
	return nil
}

func runServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	dbPath := fs.String("db", "regatta.db", "Path to sqlite state DB")
	itemsRoot := fs.String("items-root", ".", "Repo root containing .regatta/items/*.md")
	tickOnce := fs.Bool("tick-once", false, "Run one poll+schedule cycle and exit")
	pollDur := fs.Duration("poll", 30*time.Second, "SpecAdapter poll interval")
	tickDur := fs.Duration("tick", 5*time.Second, "Scheduler tick interval")
	heartDur := fs.Duration("heartbeat", 60*time.Second, "Lock heartbeat interval")
	lockTTL := fs.Duration("lock-ttl", 15*time.Minute, "Hotspot lock heartbeat lease")
	spawnerName := fs.String("spawner", "stub", "Spawner backend: stub | claude")
	repoRoot := fs.String("repo", ".", "Repo root for the claude spawner (worktrees live under <repo>/.regatta/worktrees)")
	claudeBin := fs.String("claude", "claude", "Path to the claude binary (used when -spawner=claude)")
	baseRef := fs.String("base-ref", "HEAD", "Git ref a new agent worktree branches from")
	laneCaps := laneCapsFlag{}
	fs.Var(laneCaps, "lane", "Per-lane concurrency cap, repeatable (e.g. -lane server:1)")
	_ = fs.Parse(args)

	logger := log.New(os.Stderr, "regatta: ", log.LstdFlags|log.Lmicroseconds)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := state.Open(ctx, state.DSN(*dbPath))
	if err != nil {
		logger.Printf("open db: %v", err)
		return 2
	}
	defer func() { _ = db.Close() }()

	ad, err := adapter.NewMarkdownCatalog(adapter.MarkdownCatalogConfig{Root: *itemsRoot})
	if err != nil {
		logger.Printf("adapter: %v", err)
		return 2
	}

	set, err := buildSpawner(*spawnerName, *repoRoot, *claudeBin, *baseRef)
	if err != nil {
		logger.Printf("spawner: %v", err)
		return 2
	}

	o := orchestrator.New(db, ad, set.Spawner, orchestrator.Config{
		PollInterval:      *pollDur,
		TickInterval:      *tickDur,
		HeartbeatInterval: *heartDur,
		LockTTL:           *lockTTL,
		LaneCaps:          map[string]int(laneCaps),
	})
	o.SetLogger(logger.Printf)
	if set.Worktrees != nil {
		o.SetReaper(reaper.New(db, set.Worktrees, set.Killer))
	}

	if err := o.Recover(ctx); err != nil {
		logger.Printf("recover: %v", err)
		return 1
	}

	if *tickOnce {
		if err := o.PollOnce(ctx); err != nil {
			logger.Printf("poll: %v", err)
			return 1
		}
		if err := o.ScheduleOnce(ctx); err != nil {
			logger.Printf("schedule: %v", err)
			return 1
		}
		if err := o.ReapTerminal(ctx); err != nil {
			logger.Printf("reap: %v", err)
			return 1
		}
		return 0
	}

	if err := o.Run(ctx); err != nil {
		logger.Printf("run: %v", err)
		return 1
	}
	return 0
}

// spawnerSet bundles the three handles a serve invocation needs to
// wire the Spawner + Reaper. Only the claude backend populates
// Killer + Worktrees; the stub leaves them nil so runServe knows to
// skip the Reaper.
type spawnerSet struct {
	Spawner   spawner.Spawner
	Killer    reaper.ChildKiller
	Worktrees *spawner.WorktreeManager
}

// buildSpawner returns the spawnerSet selected by the -spawner flag.
func buildSpawner(name, repoRoot, claudeBin, baseRef string) (spawnerSet, error) {
	switch name {
	case "", "stub":
		return spawnerSet{Spawner: spawner.NewStub()}, nil
	case "claude":
		wm, err := spawner.NewWorktreeManager(spawner.WorktreeManagerConfig{RepoRoot: repoRoot})
		if err != nil {
			return spawnerSet{}, fmt.Errorf("worktree manager: %w", err)
		}
		cs, err := spawner.NewClaudeSpawner(wm, spawner.ClaudeSpawnerConfig{
			Command: claudeBin,
			BaseRef: baseRef,
		})
		if err != nil {
			return spawnerSet{}, fmt.Errorf("claude spawner: %w", err)
		}
		return spawnerSet{Spawner: cs, Killer: cs, Worktrees: wm}, nil
	default:
		return spawnerSet{}, fmt.Errorf("unknown spawner %q (want stub|claude)", name)
	}
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

// runProgram dispatches the `program ...` subcommand tree.
func runProgram(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "regatta program: expected sub-subcommand (verify-handoff)")
		return 2
	}
	switch args[0] {
	case "plan":
		return runProgramPlan(args[1:])
	case "verify-handoff":
		return runProgramVerifyHandoff(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "regatta program: unknown subcommand %q\n", args[0])
		return 2
	}
}

// runProgramPlan: read a WorkItem from a JSON file on disk, call
// the Anthropic planner, validate + sign the resulting ProgramBrief,
// emit to stdout. Source adapters are deferred -- for v1, operators
// hand-author the parent WorkItem JSON file or extract it from
// their adapter into one.
func runProgramPlan(args []string) int {
	fs := flag.NewFlagSet("program plan", flag.ExitOnError)
	model := fs.String("model", "claude-opus-4-7", "Claude model id")
	keyEnv := fs.String("hmac-key-env", "", "Env var holding HMAC key (required)")
	keyID := fs.String("hmac-key-id", "k1", "key_id to stamp into signature")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(fs.Output(), "Usage: regatta program plan <work-item.json>")
		fs.PrintDefaults()
	}
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}
	if *keyEnv == "" {
		fmt.Fprintln(os.Stderr, "regatta program plan: -hmac-key-env is required")
		return 2
	}
	key := os.Getenv(*keyEnv)
	if key == "" {
		fmt.Fprintf(os.Stderr, "regatta program plan: $%s is empty\n", *keyEnv)
		return 2
	}

	raw, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, "regatta program plan:", err)
		return 2
	}
	var parent schemas.WorkItem
	if err := json.Unmarshal(raw, &parent); err != nil {
		fmt.Fprintln(os.Stderr, "regatta program plan: parse work-item.json:", err)
		return 2
	}
	if parent.ID == "" || len(parent.AcceptanceCriteria) == 0 {
		fmt.Fprintln(os.Stderr, "regatta program plan: work-item must have id and acceptance_criteria")
		return 2
	}
	if parent.Kind != schemas.KindProgram {
		fmt.Fprintf(os.Stderr, "regatta program plan: work-item kind must be %q, got %q\n", schemas.KindProgram, parent.Kind)
		return 2
	}

	client, err := program.NewAnthropicPlanner(*model)
	if err != nil {
		fmt.Fprintln(os.Stderr, "regatta program plan:", err)
		return 2
	}

	plan, err := program.Run(context.Background(), program.PlannerOptions{
		Client:    client,
		HMACKey:   []byte(key),
		HMACKeyID: *keyID,
	}, parent)
	if err != nil {
		fmt.Fprintln(os.Stderr, "regatta program plan:", err)
		return 1
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(plan)
	return 0
}

func runProgramVerifyHandoff(args []string) int {
	fs := flag.NewFlagSet("program verify-handoff", flag.ExitOnError)
	keyEnv := fs.String("hmac-key-env", "", "Env var holding the HMAC key (if set, verify signature)")
	keyID := fs.String("hmac-key-id", "k1", "key_id to expect in the signature")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(fs.Output(), "Usage: regatta program verify-handoff <handoff.json>")
		fs.PrintDefaults()
	}
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}

	h, err := program.LoadAndValidate(fs.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, "regatta program verify-handoff:", err)
		return 1
	}

	report := struct {
		ProgramID         string `json:"program_id"`
		FeatureID         string `json:"feature_id"`
		WorkerRunID       string `json:"worker_run_id"`
		SuccessState      string `json:"success_state"`
		SchemaOK          bool   `json:"schema_ok"`
		SignatureVerified bool   `json:"signature_verified"`
		SignatureChecked  bool   `json:"signature_checked"`
	}{
		ProgramID:    h.ProgramID,
		FeatureID:    h.FeatureID,
		WorkerRunID:  h.WorkerRunID,
		SuccessState: h.SuccessState,
		SchemaOK:     true,
	}

	if *keyEnv != "" {
		report.SignatureChecked = true
		key := os.Getenv(*keyEnv)
		if key == "" {
			fmt.Fprintf(os.Stderr, "regatta program verify-handoff: $%s is empty\n", *keyEnv)
			return 2
		}
		keyring := map[string][]byte{*keyID: []byte(key)}
		if err := h.VerifySignature(keyring); err != nil {
			fmt.Fprintln(os.Stderr, "regatta program verify-handoff: signature:", err)
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

func runValidateConfig(args []string) int {
	fs := flag.NewFlagSet("validate-config", flag.ExitOnError)
	cfgPath := fs.String("config", "regatta.yaml", "Path to regatta.yaml")
	_ = fs.Parse(args)

	if err := validateconfig.LoadFile(*cfgPath); err != nil {
		fmt.Fprintf(os.Stderr, "validate-config: FAIL\n%s\n", err)
		return 1
	}
	fmt.Printf("validate-config: PASS - %s validates against regatta.v1\n", *cfgPath)
	return 0
}
