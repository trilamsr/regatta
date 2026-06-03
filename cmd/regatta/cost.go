// cost subcommand tree. Today: `cost status` prints the W5 global
// daily-cap state — 24h spend, configured cap, scheduler state, and
// auto-resume horizon. Operator runs this to answer "why is regatta
// idle?" without grepping the substrate.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/trilamsr/regatta/internal/config/validate"
	costcap "github.com/trilamsr/regatta/internal/cost/cap"
	"github.com/trilamsr/regatta/internal/cost/spend"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

const (
	subcmdCost           = "cost"
	defaultRegattaConfig = "regatta.yaml"
)

// costDeps injects every side-effect the cost path touches so tests
// substitute a fixed clock + temp-DB DSN + config bytes.
type costDeps struct {
	Stdout     io.Writer
	Stderr     io.Writer
	Clock      func() time.Time
	DSN        string
	ConfigPath string
}

func runCost(args []string) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(os.Stderr, "regatta cost: expected sub-subcommand (status)")
		return 2
	}
	switch args[0] {
	case "status":
		return runCostStatus(args[1:])
	default:
		_, _ = fmt.Fprintf(os.Stderr, "regatta cost: unknown subcommand %q\n", args[0])
		return 2
	}
}

func runCostStatus(args []string) int {
	return runCostStatusWith(costDeps{
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
		Clock:      time.Now,
		DSN:        state.DSN(defaultDBPath(args)),
		ConfigPath: defaultConfigPath(args),
	}, args)
}

func runCostStatusWith(deps costDeps, args []string) int {
	fs := flag.NewFlagSet("cost status", flag.ContinueOnError)
	fs.SetOutput(deps.Stderr)
	_ = fs.String("db", "regatta.db", "Path to sqlite state DB")
	_ = fs.String("config", defaultRegattaConfig, "Path to regatta.yaml")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(deps.Stderr, "Usage: regatta cost status [--db <path>] [--config <path>]")
		_, _ = fmt.Fprintln(deps.Stderr, "Prints current 24h spend, configured daily cap, scheduler state, and auto-resume horizon.")
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	ctx := context.Background()
	enf, err := buildEnforcer(ctx, deps)
	if err != nil {
		_, _ = fmt.Fprintf(deps.Stderr, "regatta cost status: %v\n", err)
		return 1
	}
	if err := costcap.PrintStatus(ctx, deps.Stdout, enf); err != nil {
		_, _ = fmt.Fprintf(deps.Stderr, "regatta cost status: %v\n", err)
		return 1
	}
	return 0
}

// buildEnforcer wires a read-only Enforcer from regatta.yaml + the
// state DB. Used by `cost status` (no writes) and `resume` (with
// Recorder=DB). Returns an Enforcer with CapMicro=0 when the config
// has no cost.cap block — PrintStatus then renders the unset path.
func buildEnforcer(ctx context.Context, deps costDeps) (*costcap.Enforcer, error) {
	clock := deps.Clock
	if clock == nil {
		clock = time.Now
	}
	settings, _ := validate.LoadCostCapSettings(readConfigBytes(deps.ConfigPath))
	db, err := state.Open(ctx, deps.DSN)
	if err != nil {
		return nil, fmt.Errorf("open state db: %w", err)
	}
	tz := time.UTC
	if settings.TZ != "" {
		loc, err := time.LoadLocation(settings.TZ)
		if err != nil {
			return nil, fmt.Errorf("invalid cost.cap.timezone %q: %w", settings.TZ, err)
		}
		tz = loc
	}
	return costcap.New(costcap.Config{
		CapMicro:   settings.CapMicro,
		TenantID:   substrate.DefaultTenantID,
		TZ:         tz,
		MemoizeTTL: settings.MemoizeTTL,
		Spend:      spend.NewReader(db.SQL(), clock),
		Recorder:   db,
		Resume:     resumeReader{db: db},
		Clock:      clock,
	})
}

// resumeReader adapts state.DB.LatestEventByKind to costcap.ResumeReader.
type resumeReader struct{ db *state.DB }

func (r resumeReader) LatestResumeAt(ctx context.Context) (time.Time, error) {
	ev, err := r.db.LatestEventByKind(ctx, costcap.EventKindResumed)
	if err != nil {
		if errors.Is(err, errNoRows()) {
			return time.Time{}, nil
		}
		return time.Time{}, err
	}
	return ev.CreatedAt, nil
}

// errNoRows wraps sql.ErrNoRows behind a thin function so cost.go does
// not pull database/sql into its top-level imports.
func errNoRows() error {
	return sqlErrNoRows
}

func readConfigBytes(path string) []byte {
	if path == "" {
		return nil
	}
	b, err := os.ReadFile(path) //nolint:gosec // operator-supplied regatta.yaml path; same posture as serve.go reads
	if err != nil {
		return nil
	}
	return b
}

func defaultConfigPath(args []string) string {
	for i, a := range args {
		switch a {
		case "--config", "-config":
			if i+1 < len(args) {
				return args[i+1]
			}
		}
	}
	return defaultRegattaConfig
}
