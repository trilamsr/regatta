// cost subcommand tree. `status` prints the W5 global daily-cap state.
// `backfill <run_id>` re-derives token_spend rows from the Anthropic
// Usage API for a run window — the §9 R4 recovery primitive when a
// spawner crash left an open llm_call span without a substrate row.
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
	"github.com/trilamsr/regatta/internal/cost/reconcile"
	"github.com/trilamsr/regatta/internal/cost/spend"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

const (
	subcmdCost           = "cost"
	defaultRegattaConfig = "regatta.yaml"
)

// costDeps lifts every side-effect (clock, DSN, config bytes, DB opener) so tests can substitute deterministic fakes; Opener nil in prod, counting wrapper in tests.
type costDeps struct {
	Stdout     io.Writer
	Stderr     io.Writer
	Clock      func() time.Time
	DSN        string
	ConfigPath string
	Opener     func(context.Context, string) (*state.DB, error)
}

func runCost(args []string) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(os.Stderr, "regatta cost: expected sub-subcommand (status|backfill)")
		return 2
	}
	switch args[0] {
	case "status": //nolint:goconst // sub-subcommand verb shared with merge/secret; per-parent scoping reads cleaner than a global enum.
		return runCostStatus(args[1:])
	case "backfill":
		return runCostBackfill(args[1:])
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
	_ = fs.String("db", defaultStateDB(), "Path to sqlite state DB")
	_ = fs.String("config", defaultRegattaConfig, "Path to regatta.yaml")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(deps.Stderr, "Usage: regatta cost status [--db <path>] [--config <path>]")
		_, _ = fmt.Fprintln(deps.Stderr, "Prints current 24h spend, configured daily cap, scheduler state, and auto-resume horizon.")
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	ctx := context.Background()
	enf, closeDB, err := buildEnforcer(ctx, deps)
	if err != nil {
		_, _ = fmt.Fprintf(deps.Stderr, "regatta cost status: %v\n", err)
		return 1
	}
	defer closeDB()
	if err := costcap.PrintStatus(ctx, deps.Stdout, enf); err != nil {
		_, _ = fmt.Fprintf(deps.Stderr, "regatta cost status: %v\n", err)
		return 1
	}
	return 0
}

// buildEnforcer wires a read-only Enforcer; caller MUST defer the returned closeDB because the Enforcer retains the *state.DB via Recorder/Spend/Resume. CapMicro=0 when regatta.yaml has no cost.cap block — PrintStatus renders the unset path.
func buildEnforcer(ctx context.Context, deps costDeps) (*costcap.Enforcer, func(), error) {
	clock := deps.Clock
	if clock == nil {
		clock = time.Now
	}
	settings, _ := validate.LoadCostCapSettings(readConfigBytes(deps.ConfigPath))
	opener := deps.Opener
	if opener == nil {
		opener = state.Open
	}
	db, err := opener(ctx, deps.DSN)
	if err != nil {
		return nil, func() {}, fmt.Errorf("open state db: %w", err)
	}
	closeDB := func() { _ = db.Close() }
	tz := time.UTC
	if settings.TZ != "" {
		loc, err := time.LoadLocation(settings.TZ)
		if err != nil {
			closeDB()
			return nil, func() {}, fmt.Errorf("invalid cost.cap.timezone %q: %w", settings.TZ, err)
		}
		tz = loc
	}
	enf, err := costcap.New(costcap.Config{
		CapMicro:   settings.CapMicro,
		TenantID:   substrate.DefaultTenantID,
		TZ:         tz,
		MemoizeTTL: settings.MemoizeTTL,
		Spend:      spend.NewReader(db.SQL(), clock),
		Recorder:   db,
		Resume:     newResumeReader(db),
		Clock:      clock,
	})
	if err != nil {
		closeDB()
		return nil, func() {}, err
	}
	return enf, closeDB, nil
}

// newResumeReader returns a costcap.ResumeReader closure reading the
// last cost_cap_resumed audit event via state.DB.
func newResumeReader(db *state.DB) costcap.ResumeReader {
	return func(ctx context.Context) (time.Time, error) {
		ev, err := db.LatestEventByKind(ctx, costcap.EventKindResumed)
		if err != nil {
			if errors.Is(err, sqlErrNoRows) {
				return time.Time{}, nil
			}
			return time.Time{}, err
		}
		return ev.CreatedAt, nil
	}
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

// backfillDeps mirrors costDeps + adds keyring/active-key/BaseURL the backfill path needs; Opener kept so tests assert per-invocation Close parity with `cost status`.
type backfillDeps struct {
	Stdout     io.Writer
	Stderr     io.Writer
	Clock      func() time.Time
	DSN        string
	ConfigPath string
	Opener     func(context.Context, string) (*state.DB, error)
	Keys       map[string][]byte
	ActiveKey  string
	BaseURL    string
}

// adminKeyEnv names the env var Anthropic admin endpoints authenticate
// against; matches reconcile.ClientConfig.UsageAPIKeyEnv default so the
// CLI fast-fail at parse time validates the same key the inner client
// will pull at request time (#705 R3).
const adminKeyEnv = "ANTHROPIC_ADMIN_KEY" //nolint:gosec // env var name, not a credential

func runCostBackfill(args []string) int {
	keys, active := loadBriefKeyringWithActive()
	return runCostBackfillWith(backfillDeps{
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
		Clock:      time.Now,
		DSN:        state.DSN(defaultDBPath(args)),
		ConfigPath: defaultConfigPath(args),
		Keys:       keys,
		ActiveKey:  active,
	}, args)
}

func runCostBackfillWith(deps backfillDeps, args []string) int {
	fs := flag.NewFlagSet("cost backfill", flag.ContinueOnError)
	fs.SetOutput(deps.Stderr)
	_ = fs.String("db", defaultStateDB(), "Path to sqlite state DB")
	_ = fs.String("config", defaultRegattaConfig, "Path to regatta.yaml")
	tag := fs.String("backfill-tag", "", "Operator-supplied pricing_rev disambiguator (#705 R1)")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(deps.Stderr, "Usage: regatta cost backfill <run_id> [--db <path>] [--config <path>] [--backfill-tag <tag>]")
		_, _ = fmt.Fprintln(deps.Stderr, "Re-derives token_spend rows from Anthropic Usage API for the run window (spec §9 R4).")
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	pos := fs.Args()
	if len(pos) == 0 {
		fs.Usage()
		return 2
	}
	runID := pos[0]

	key := deps.Keys[deps.ActiveKey]
	if len(key) == 0 {
		_, _ = fmt.Fprintln(deps.Stderr, "regatta cost backfill: REGATTA_HMAC_KEY unset — backfill rows would not sign")
		return 1
	}
	if os.Getenv(adminKeyEnv) == "" {
		_, _ = fmt.Fprintf(deps.Stderr, "regatta cost backfill: %s unset — Anthropic Usage API would reject (#705 R3)\n", adminKeyEnv)
		return 1
	}

	clock := deps.Clock
	if clock == nil {
		clock = time.Now
	}
	opener := deps.Opener
	if opener == nil {
		opener = state.Open
	}
	ctx := context.Background()
	db, err := opener(ctx, deps.DSN)
	if err != nil {
		_, _ = fmt.Fprintf(deps.Stderr, "regatta cost backfill: open state db: %v\n", err)
		return 1
	}
	defer func() { _ = db.Close() }()

	res, err := reconcile.Backfill(ctx, reconcile.BackfillConfig{
		DB:       db.SQL(),
		RunID:    runID,
		TenantID: substrate.DefaultTenantID,
		BaseURL:  deps.BaseURL,
		Key:      key,
		KeyID:    deps.ActiveKey,
		Tag:      *tag,
		Now:      clock,
	})
	if err != nil {
		if errors.Is(err, reconcile.ErrRunNotFound) {
			_, _ = fmt.Fprintf(deps.Stderr, "regatta cost backfill: run %q has no substrate events\n", runID)
			return 1
		}
		_, _ = fmt.Fprintf(deps.Stderr, "regatta cost backfill: %v\n", err)
		return 1
	}
	if res.SingleEventWindow {
		_, _ = fmt.Fprintf(deps.Stderr,
			"regatta cost backfill: warn: run %q has a single substrate event — synthetic 1h window, no calls in window expected (#705 R4)\n",
			runID)
	}
	_, _ = fmt.Fprintf(deps.Stdout,
		"backfilled run=%s window=[%s, %s) emitted=%d skipped=%d pricing_rev=%s\n",
		runID,
		res.WindowStart.Format(time.RFC3339),
		res.WindowEnd.Format(time.RFC3339),
		res.RowsEmitted, res.RowsSkipped, res.PricingRev,
	)
	return 0
}
