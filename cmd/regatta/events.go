package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

const (
	subcmdEvents     = "events"
	eventsVerbTail   = "tail"
	eventsDefaultLim = 200
)

type eventsTailDeps struct {
	Stdout io.Writer
	Stderr io.Writer
	Clock  func() time.Time
	DSN    string
}

func runEvents(args []string) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(os.Stderr, "regatta events: expected sub-subcommand (tail)")
		return 2
	}
	switch args[0] {
	case eventsVerbTail:
		return runEventsTail(args[1:])
	default:
		_, _ = fmt.Fprintf(os.Stderr, "regatta events: unknown subcommand %q\n", args[0])
		return 2
	}
}

func runEventsTail(args []string) int {
	return runEventsTailWith(eventsTailDeps{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Clock:  time.Now,
		DSN:    state.DSN(defaultDBPath(args)),
	}, args)
}

func runEventsTailWith(deps eventsTailDeps, args []string) int {
	fs := flag.NewFlagSet("events tail", flag.ContinueOnError)
	fs.SetOutput(deps.Stderr)
	agentFlag := fs.Int64("agent", 0, "Filter to events whose agent_id matches (0 = no filter)")
	kindFlag := fs.String("kind", "", "Filter to events of this kind (empty = no filter)")
	sinceFlag := fs.Duration("since", 24*time.Hour, "Only emit events whose created_at is within this window")
	formatFlag := fs.String("format", formatTable, "Output format: table | json")
	followFlag := fs.Bool("f", false, "Follow: poll for new events every 1s until SIGINT")
	_ = fs.String("db", defaultStateDB(), "Path to sqlite state DB")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(deps.Stderr, "Usage: regatta events tail [--db PATH] [--agent N] [--kind K] [--since DUR] [--format=table|json] [-f]")
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *formatFlag != formatTable && *formatFlag != logFormatJSON {
		_, _ = fmt.Fprintf(deps.Stderr, "regatta events tail: --format must be table|json, got %q\n", *formatFlag)
		return 2
	}

	ctx := context.Background()
	db, err := state.OpenWithClock(ctx, deps.DSN, deps.Clock)
	if err != nil {
		_, _ = fmt.Fprintln(deps.Stderr, "regatta events tail: open db:", err)
		return 2
	}
	defer func() { _ = db.Close() }()

	cutoff := deps.Clock().Add(-*sinceFlag)
	lastID, err := emitEventsPage(deps.Stdout, deps.Stderr, db, ctx, *kindFlag, *agentFlag, cutoff, 0, *formatFlag, true)
	if err != nil {
		_, _ = fmt.Fprintln(deps.Stderr, "regatta events tail:", err)
		return 1
	}

	if !*followFlag {
		return 0
	}
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		newLast, err := emitEventsPage(deps.Stdout, deps.Stderr, db, ctx, *kindFlag, *agentFlag, cutoff, lastID, *formatFlag, false)
		if err != nil {
			_, _ = fmt.Fprintln(deps.Stderr, "regatta events tail:", err)
			return 1
		}
		if newLast > lastID {
			lastID = newLast
		}
	}
	return 0
}

func emitEventsPage(stdout, stderr io.Writer, db *state.DB, ctx context.Context, kind string, agentID int64, cutoff time.Time, sinceID int64, format string, header bool) (int64, error) {
	var rows []state.Event
	var err error
	cutoffUnix := int64(0)
	if !cutoff.IsZero() {
		cutoffUnix = cutoff.Unix()
	}
	if kind != "" {
		rows, err = db.ListEventsByKindSinceTime(ctx, kind, sinceID, cutoffUnix, eventsDefaultLim)
	} else {
		rows, err = db.ListEventsSince(ctx, sinceID, cutoffUnix, eventsDefaultLim)
	}
	if err != nil {
		return sinceID, err
	}

	filtered := rows[:0:0]
	maxID := sinceID
	for _, e := range rows {
		if e.ID <= sinceID {
			continue
		}
		if agentID != 0 {
			if !e.AgentID.Valid || e.AgentID.Int64 != agentID {
				continue
			}
		}
		filtered = append(filtered, e)
		if e.ID > maxID {
			maxID = e.ID
		}
	}

	switch format {
	case logFormatJSON:
		if err := emitEventsJSON(stdout, filtered); err != nil {
			return maxID, err
		}
	default:
		if err := emitEventsTable(stdout, filtered, header); err != nil {
			return maxID, err
		}
	}
	_ = stderr
	return maxID, nil
}

type eventsRow struct {
	ID            int64  `json:"id"`
	AgentID       *int64 `json:"agent_id"`
	Kind          string `json:"kind"`
	PayloadJSON   string `json:"payload_json"`
	CreatedAtUnix int64  `json:"created_at_unix"`
}

func emitEventsJSON(out io.Writer, evs []state.Event) error {
	rows := make([]eventsRow, 0, len(evs))
	for _, e := range evs {
		row := eventsRow{
			ID:            e.ID,
			Kind:          e.Kind,
			PayloadJSON:   e.PayloadJSON,
			CreatedAtUnix: e.CreatedAt.Unix(),
		}
		if e.AgentID.Valid {
			v := e.AgentID.Int64
			row.AgentID = &v
		}
		rows = append(rows, row)
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(rows)
}

func emitEventsTable(out io.Writer, evs []state.Event, header bool) error {
	if header && len(evs) == 0 {
		_, _ = fmt.Fprintln(out, "no events in window.")
		return nil
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if header {
		_, _ = fmt.Fprintln(tw, "ID\tAGENT\tKIND\tCREATED_AT\tPAYLOAD")
	}
	for _, e := range evs {
		agent := "-"
		if e.AgentID.Valid {
			agent = fmt.Sprintf("%d", e.AgentID.Int64)
		}
		_, _ = fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\n",
			e.ID, agent, e.Kind,
			e.CreatedAt.UTC().Format(time.RFC3339),
			e.PayloadJSON,
		)
	}
	return tw.Flush()
}
