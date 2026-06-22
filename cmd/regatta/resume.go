// resume subcommand. Operator override for the W5 global daily-cap
// throttle. Writes a `cost_cap_resumed` audit event with the
// operator's identity so the override leaves an audit trail (spec
// §6.2). Idempotent — multiple resumes within a day add audit rows
// but do not change state.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"os"
	"os/user"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

const subcmdResume = "resume"

// sqlErrNoRows is the sentinel cost.go references via errNoRows() —
// avoids pulling database/sql into cost.go's import block.
var sqlErrNoRows = sql.ErrNoRows

// resumeDeps injects every side-effect the resume path touches.
// Opener mirrors costDeps.Opener — test seam for Close-assertion.
type resumeDeps struct {
	Stdout     io.Writer
	Stderr     io.Writer
	Clock      func() time.Time
	DSN        string
	ConfigPath string
	Actor      string // "" → derived from os/user
	Opener     func(context.Context, string) (*state.DB, error)
}

func runResume(args []string) int {
	return runResumeWith(resumeDeps{
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
		Clock:      time.Now,
		DSN:        stateDSNFromArgs(args),
		ConfigPath: defaultConfigPath(args),
	}, args)
}

// stateDSNFromArgs is a thin wrapper so test wiring can override
// without duplicating defaultDBPath logic. Delegates to state.DSN so
// the resume path picks up every pragma the orchestrator's main DB
// uses (busy_timeout, foreign_keys, WAL, synchronous, _txlock).
func stateDSNFromArgs(args []string) string {
	return state.DSN(defaultDBPath(args))
}

func runResumeWith(deps resumeDeps, args []string) int {
	fs := flag.NewFlagSet("resume", flag.ContinueOnError)
	fs.SetOutput(deps.Stderr)
	actor := fs.String("actor", deps.Actor, "Operator identity recorded in the audit event (defaults to local user)")
	_ = fs.String("db", defaultStateDB(), "Path to sqlite state DB")
	_ = fs.String("config", "regatta.yaml", "Path to regatta.yaml")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(deps.Stderr, "Usage: regatta resume [--actor <id>] [--db <path>] [--config <path>]")
		_, _ = fmt.Fprintln(deps.Stderr, "Lifts the W5 cost-cap throttle until the next day rollover. Emits a cost_cap_resumed audit event.")
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *actor == "" {
		*actor = deriveActor()
	}
	ctx := context.Background()
	enf, closeDB, err := buildEnforcer(ctx, costDeps{
		Stdout:     deps.Stdout,
		Stderr:     deps.Stderr,
		Clock:      deps.Clock,
		DSN:        deps.DSN,
		ConfigPath: deps.ConfigPath,
		Opener:     deps.Opener,
	})
	if err != nil {
		_, _ = fmt.Fprintf(deps.Stderr, "regatta resume: %v\n", err)
		return 1
	}
	defer closeDB()
	if enf.CapMicro() == 0 {
		_, _ = fmt.Fprintln(deps.Stderr, "regatta resume: cost.cap.daily_usd is unset — there is no global throttle to lift")
		return 1
	}
	if err := enf.Resume(ctx, *actor); err != nil {
		_, _ = fmt.Fprintf(deps.Stderr, "regatta resume: %v\n", err)
		return 1
	}
	spendMicro, capMicro, _, resumeAt, err := enf.Snapshot(ctx)
	if err != nil {
		_, _ = fmt.Fprintf(deps.Stderr, "regatta resume: snapshot: %v\n", err)
		return 1
	}
	_, _ = fmt.Fprintf(deps.Stdout, "override accepted (actor=%s)\n", *actor)
	_, _ = fmt.Fprintf(deps.Stdout, "state    : Active (until next rollover %s)\n",
		resumeAt.In(enf.TZ()).Format("2006-01-02 15:04 MST"))
	_, _ = fmt.Fprintf(deps.Stdout, "24h spend: $%.2f / $%.2f cap\n", spendMicro.USD(), capMicro.USD())
	return 0
}

// deriveActor walks os/user for the operator identity. Returns
// "unknown" when the lookup fails — the audit event always carries a
// non-empty actor so log greps remain reliable.
func deriveActor() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return "unknown"
}
