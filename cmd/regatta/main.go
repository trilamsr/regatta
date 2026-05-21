// regatta is the autonomous-agent-fleet CLI.
//
// Subcommands shipped today (pre-fleet):
//
//	regatta l0 <diff-file>      Run L0 spec-immutability check against a unified diff.
//	regatta verify-repo-config  Audit a GitHub repo against the P2 canonical recipe.
//	regatta serve               Run the orchestrator daemon (skeleton).
//	regatta version             Print build info.
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

	"github.com/trilamsr/regatta/internal/l0"
	"github.com/trilamsr/regatta/internal/orchestrator"
	"github.com/trilamsr/regatta/internal/orchestrator/adapter"
	"github.com/trilamsr/regatta/internal/orchestrator/spawner"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/verifyrepo"
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
	case "serve":
		os.Exit(runServe(os.Args[2:]))
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
  regatta l0 <diff-file>          Run L0 spec-immutability check
  regatta verify-repo-config      Audit GitHub repo against P2 recipe
  regatta serve                   Run the orchestrator daemon (skeleton)
  regatta version                 Print build info
  regatta help                    This message

L0 reads a unified diff from <diff-file> ("-" for stdin) and emits a
GateResult JSON document to stdout. Exit code 0 on pass, 1 on fail,
2 on usage error.

verify-repo-config requires GITHUB_TOKEN and -owner/-repo flags.

serve runs the markdown_catalog adapter against <root>/.regatta/items
and spawns a stub agent for each ready item. Pass --tick-once to run
a single poll+schedule cycle and exit.
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
	laneCaps := laneCapsFlag{}
	fs.Var(laneCaps, "lane", "Per-lane concurrency cap, repeatable (e.g. -lane server:1)")
	_ = fs.Parse(args)

	logger := log.New(os.Stderr, "regatta: ", log.LstdFlags|log.Lmicroseconds)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", *dbPath)
	db, err := state.Open(ctx, dsn)
	if err != nil {
		logger.Printf("open db: %v", err)
		return 2
	}
	defer db.Close()

	ad, err := adapter.NewMarkdownCatalog(adapter.MarkdownCatalogConfig{Root: *itemsRoot})
	if err != nil {
		logger.Printf("adapter: %v", err)
		return 2
	}

	o := orchestrator.New(db, ad, spawner.NewStub(), orchestrator.Config{
		PollInterval:      *pollDur,
		TickInterval:      *tickDur,
		HeartbeatInterval: *heartDur,
		LockTTL:           *lockTTL,
		LaneCaps:          map[string]int(laneCaps),
	})
	o.SetLogger(logger.Printf)

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
		return 0
	}

	if err := o.Run(ctx); err != nil {
		logger.Printf("run: %v", err)
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
			fmt.Printf("%s %s — %s\n", mark, c.ID, c.Title)
			if c.Detail != "" {
				fmt.Printf("    %s\n", c.Detail)
			}
			if !c.Passed && c.Rationale != "" {
				fmt.Printf("    rationale: %s\n", c.Rationale)
			}
		}
		if res.OK {
			fmt.Println("\nverify-repo-config: PASS — repo is configured for Regatta deployment")
		} else {
			fmt.Printf("\nverify-repo-config: FAIL — %d check(s) failed: %v\n", len(res.FailedOK), res.FailedOK)
		}
	}
	if !res.OK {
		return 1
	}
	return 0
}
