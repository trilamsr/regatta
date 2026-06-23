package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

const (
	subcmdAgents      = "agents"
	agentsSubList     = "list"
	formatTable = "table"
)

// runAgents dispatches the agents sub-subcommand tree. Read-only over state.db; closes #1078.
func runAgents(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "regatta agents: expected sub-subcommand (list)")
		return 2
	}
	switch args[0] {
	case agentsSubList:
		return runAgentsList(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "regatta agents: unknown subcommand %q\n", args[0])
		return 2
	}
}

// knownAgentStates returns every AgentState the substrate currently defines so the default --state filter shows the full universe (closes a reviewer a57487820abd65001 risk: AgentEscalated was previously omitted, hiding escalated agents from the operator).
func knownAgentStates() []state.AgentState {
	return []state.AgentState{
		state.AgentPending, state.AgentSpawning, state.AgentRunning,
		state.AgentPROpen, state.AgentGatesRunning, state.AgentAwaitingMerge,
		state.AgentDone, state.AgentWithdrawn, state.AgentCrashed,
		state.AgentGatesFailed, state.AgentEscalated,
	}
}

func isKnownAgentState(s state.AgentState) bool {
	for _, k := range knownAgentStates() {
		if k == s {
			return true
		}
	}
	return false
}

func knownAgentStateLabels() []string {
	all := knownAgentStates()
	out := make([]string, len(all))
	for i, s := range all {
		out[i] = string(s)
	}
	return out
}

func runAgentsList(args []string) int {
	fs := flag.NewFlagSet("agents list", flag.ContinueOnError)
	dbPath := fs.String("db", defaultStateDB(), "Path to sqlite state DB (relative to cwd unless absolute; ENV: REGATTA_STATE_DB)")
	stateFlag := fs.String("state", "", "Filter by agent state (eg running,pr_open). Comma-separated for multiple.")
	laneFlag := fs.String("lane", "", "Filter by lane")
	format := fs.String("format", formatTable, "Output format: table | json")
	// --json is sugar for --format=json (matches `gh pr list --json` muscle memory; R31-Bug-D).
	jsonFlag := fs.Bool("json", false, "Shorthand for --format=json")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *jsonFlag {
		*format = logFormatJSON
	}

	resolved := *dbPath
	// Legacy REGATTA_DB env fallback (pre-MAY-R3 alias); REGATTA_STATE_DB
	// is the canonical name + already flows through defaultStateDB().
	if envDB := os.Getenv("REGATTA_DB"); resolved == stateDBDefaultLiteral && envDB != "" { // canonical-env-skip: MAY-R3 back-compat fallback to legacy REGATTA_DB; canonical is REGATTA_STATE_DB
		resolved = envDB
	}
	ctx := context.Background()
	db, err := state.Open(ctx, state.DSN(resolved))
	if err != nil {
		fmt.Fprintf(os.Stderr, "regatta agents list: open db %q: %v\n", resolved, err)
		return 1
	}
	defer func() { _ = db.Close() }()

	var states []state.AgentState
	if *stateFlag != "" {
		for _, s := range strings.Split(*stateFlag, ",") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			as := state.AgentState(s)
			if !isKnownAgentState(as) {
				fmt.Fprintf(os.Stderr,
					"regatta agents list: unknown state %q (known: %s)\n",
					s, strings.Join(knownAgentStateLabels(), ","))
				return 2
			}
			states = append(states, as)
		}
	} else {
		states = knownAgentStates()
	}
	rows, err := db.ListAgentsByState(ctx, states...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "regatta agents list: query: %v\n", err)
		return 1
	}
	if *laneFlag != "" {
		filtered := rows[:0]
		for _, a := range rows {
			if a.Lane == *laneFlag {
				filtered = append(filtered, a)
			}
		}
		rows = filtered
	}
	switch *format {
	case logFormatJSON:
		return emitAgentsJSON(os.Stdout, rows)
	case formatTable:
		return emitAgentsTable(os.Stdout, rows)
	default:
		fmt.Fprintf(os.Stderr, "regatta agents list: unknown format %q (want table|json)\n", *format)
		return 2
	}
}

type agentListRow struct {
	ID         int64  `json:"id"`
	WorkItemID string `json:"work_item_id"`
	Lane       string `json:"lane"`
	State      string `json:"state"`
	PID        int    `json:"pid"`
	SessionID  string `json:"session_id"`
	PRSHA      string `json:"pr_sha,omitempty"`
	UpdatedAt  string `json:"updated_at"`
}

func toAgentListRows(rows []state.Agent) []agentListRow {
	out := make([]agentListRow, 0, len(rows))
	for _, a := range rows {
		out = append(out, agentListRow{
			ID:         a.ID,
			WorkItemID: a.WorkItemID,
			Lane:       a.Lane,
			State:      string(a.State),
			PID:        a.PID,
			SessionID:  a.SessionID,
			PRSHA:      a.PRSHA,
			UpdatedAt:  a.UpdatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}
	return out
}

func emitAgentsJSON(w io.Writer, rows []state.Agent) int {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(toAgentListRows(rows)); err != nil {
		fmt.Fprintf(os.Stderr, "regatta agents list: encode json: %v\n", err)
		return 1
	}
	return 0
}

func emitAgentsTable(w io.Writer, rows []state.Agent) int {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "ID\tWORK_ITEM\tLANE\tSTATE\tPID\tSESSION\tUPDATED")
	for _, a := range rows {
		_, _ = fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%d\t%s\t%s\n",
			a.ID, a.WorkItemID, a.Lane, a.State, a.PID, a.SessionID,
			a.UpdatedAt.Format("2006-01-02T15:04:05Z"))
	}
	if err := tw.Flush(); err != nil {
		return 1
	}
	return 0
}
