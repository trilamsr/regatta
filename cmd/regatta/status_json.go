package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// StatusJSON is the single observation envelope emitted by
// `regatta status --json`. Shape pins the skill OBSERVE single-command
// primitive (MAY-47) — each top-level group maps to one cheap-first
// channel from the prior 4-channel sweep (gh pr list, /healthz,
// /ui/panels/agents, sqlite events).
type StatusJSON struct {
	GeneratedAt  string              `json:"generated_at"`
	TrafficLight string              `json:"traffic_light"`
	Orchestrator OrchestratorStatus  `json:"orchestrator"`
	Agents       AgentsStatus        `json:"agents"`
	PRs          PRsStatus           `json:"prs"`
	Events       EventsStatus        `json:"events"`
	// Errors holds free-form operator logs only — not structured for jq routing.
	Errors []string `json:"errors,omitempty"`
}

// OrchestratorStatus reports liveness derived from /healthz when the
// HTTP socket answers, falling back to "unknown" markers when the
// daemon is wedged or absent (--db direct path). PID is a sentinel
// int (NOT a real OS pid yet) — regatta exposes no introspection
// endpoint for the daemon's pid, so the field encodes probe outcome:
// -1 = no socket reached, -2 = socket alive but pid not advertised,
// 0 = uninitialised (reserved; never emitted). Operator scripts MUST
// treat negative values as opaque sentinels, not as pid math. A
// future /healthz extension will replace the -2 sentinel with the
// real pid without changing the field name; consumers MUST tolerate
// both shapes (any int >= 1 means "real pid known").
type OrchestratorStatus struct {
	Alive         bool   `json:"alive"`
	PID           int    `json:"pid"`
	UptimeSeconds int64  `json:"uptime_seconds"`
	Version       string `json:"version,omitempty"`
	HealthStatus  string `json:"health_status,omitempty"`
}

// AgentsStatus carries the in-flight agent population. ByState keys
// are state.AgentState string values (`spawning`, `running`, ...);
// callers reading the envelope MUST tolerate missing keys (empty
// buckets are elided to keep the wire compact).
type AgentsStatus struct {
	InFlight int            `json:"in_flight"`
	ByState  map[string]int `json:"by_state"`
}

// PRsStatus mirrors the GitHub PR sweep prior approach. ByMergeState
// is keyed on `gh pr` mergeStateStatus values — CLEAN / BLOCKED /
// DIRTY / UNSTABLE / BEHIND / UNKNOWN. The map is always non-nil so
// jq `.prs.by_merge_state.BLOCKED // 0` is safe.
type PRsStatus struct {
	Open          int            `json:"open"`
	ByMergeState  map[string]int `json:"by_merge_state"`
}

// EventsStatus pulls the three load-bearing last-event timestamps the
// operator polls before deciding to intervene. Empty string when no
// event of that kind has fired.
type EventsStatus struct {
	LastSpawnAt string `json:"last_spawn_at,omitempty"`
	LastExitAt  string `json:"last_exit_at,omitempty"`
	LastMergeAt string `json:"last_merge_at,omitempty"`
	LastAnyAt   string `json:"last_any_at,omitempty"`
}

const (
	trafficLightRed    = "red"
	trafficLightYellow = "yellow"
	trafficLightGreen  = "green"

	eventKindSpawnCompleted = "spawn.completed"
	eventKindAgentExited    = "agent.exited"
	eventKindMergeCompleted = "merge_completed"
)

// statusJSONOpts is the constructor seam so tests can pin Now + the
// socket URL without poking package globals.
type statusJSONOpts struct {
	DBPath       string
	SocketURL    string
	ConnectTimeo time.Duration
	Now          func() time.Time
}

// runStatusJSON emits one StatusJSON envelope to w. Returns 0 always
// (even on degraded / DB-missing) — operator scripts pipe to jq, the
// envelope itself carries the failure signal via traffic_light=red +
// the errors[] array. Non-zero exit is reserved for argv / flag
// parse failures handled upstream in runStatus.
func runStatusJSON(ctx context.Context, w io.Writer, opts statusJSONOpts) int {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.ConnectTimeo == 0 {
		opts.ConnectTimeo = 250 * time.Millisecond
	}
	envelope := StatusJSON{
		GeneratedAt: opts.Now().UTC().Format(time.RFC3339),
		Orchestrator: OrchestratorStatus{
			PID: -1,
		},
		Agents: AgentsStatus{ByState: map[string]int{}},
		PRs:    PRsStatus{ByMergeState: map[string]int{}},
	}

	probeOrchestrator(ctx, &envelope, opts)
	dbReachable := probeSQLite(ctx, &envelope, opts)

	envelope.TrafficLight = deriveTrafficLight(envelope, dbReachable, opts.Now())

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(envelope); err != nil {
		fmt.Fprintln(os.Stderr, "regatta status --json: encode:", err)
		return 1
	}
	return 0
}

// probeOrchestrator dials /healthz on the configured socket; absent
// or non-responsive socket leaves the orchestrator block in its
// default pid=-1 / alive=false state with an errors[] note.
func probeOrchestrator(ctx context.Context, env *StatusJSON, opts statusJSONOpts) {
	url := opts.SocketURL
	if url == "" {
		url = "http://127.0.0.1" + defaultListenerAddr
	}
	probeCtx, cancel := context.WithTimeout(ctx, opts.ConnectTimeo)
	defer cancel()

	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, url+"/healthz", nil)
	if err != nil {
		env.Errors = append(env.Errors, "orchestrator probe: build req: "+err.Error())
		return
	}
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: opts.ConnectTimeo}
	resp, err := client.Do(req)
	if err != nil {
		env.Errors = append(env.Errors, "orchestrator probe: "+err.Error())
		return
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		env.Errors = append(env.Errors, "orchestrator probe: read: "+err.Error())
		return
	}
	var hz struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(body, &hz); err != nil {
		env.Errors = append(env.Errors, "orchestrator probe: parse: "+err.Error())
		return
	}
	env.Orchestrator.Alive = resp.StatusCode == http.StatusOK && hz.Status == "ok"
	env.Orchestrator.HealthStatus = hz.Status
	env.Orchestrator.Version = hz.Version
	// PID + uptime require an introspection endpoint regatta does not
	// yet expose; pin to 0/0 here so consumers see "alive but PID
	// unknown" rather than synthesising a value. Sentinel pid=0 was
	// reserved for "not probed" — once alive, switch to -2 ("alive,
	// pid not advertised") so the seam stays observable.
	env.Orchestrator.PID = -2
}

// probeSQLite opens the read-only sqlite conn and folds agent +
// event aggregates into env. Returns true when the DB was reachable
// (file exists, schema present); false signals the degraded-source
// path so deriveTrafficLight can paint red.
func probeSQLite(ctx context.Context, env *StatusJSON, opts statusJSONOpts) bool {
	if opts.DBPath == "" {
		env.Errors = append(env.Errors, "sqlite: no --db path supplied")
		return false
	}
	if _, err := os.Stat(opts.DBPath); err != nil {
		env.Errors = append(env.Errors, "sqlite: stat: "+err.Error())
		return false
	}
	dsn := fmt.Sprintf("file:%s?mode=ro&_journal_mode=WAL&_query_only=1", opts.DBPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		env.Errors = append(env.Errors, "sqlite: open: "+err.Error())
		return false
	}
	defer func() { _ = db.Close() }()

	pingCtx, cancel := context.WithTimeout(ctx, opts.ConnectTimeo)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		env.Errors = append(env.Errors, "sqlite: ping: "+err.Error())
		return false
	}

	loadAgentsByState(ctx, db, env)
	loadLastEvents(ctx, db, env)
	return true
}

// loadAgentsByState scans the agents table grouping by state. A
// missing table (early-boot) leaves env.Agents.ByState empty +
// records the cause in errors[].
func loadAgentsByState(ctx context.Context, db *sql.DB, env *StatusJSON) {
	rows, err := db.QueryContext(ctx, `SELECT state, COUNT(*) FROM agents GROUP BY state`)
	if err != nil {
		env.Errors = append(env.Errors, "sqlite: agents by_state: "+err.Error())
		return
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var state string
		var n int
		if err := rows.Scan(&state, &n); err != nil {
			env.Errors = append(env.Errors, "sqlite: agents scan: "+err.Error())
			return
		}
		env.Agents.ByState[state] = n
		if state == "spawning" || state == triggerStatusRunning || state == "awaiting_merge" {
			env.Agents.InFlight += n
		}
	}
}

// loadLastEvents grabs the most-recent created_at for each of the
// three kinds the skill polls. events.created_at is UNIX-epoch
// seconds (per migrations/0001).
func loadLastEvents(ctx context.Context, db *sql.DB, env *StatusJSON) {
	kinds := map[string]*string{
		eventKindSpawnCompleted: &env.Events.LastSpawnAt,
		eventKindAgentExited:    &env.Events.LastExitAt,
		eventKindMergeCompleted: &env.Events.LastMergeAt,
	}
	for kind, dst := range kinds {
		var ts sql.NullInt64
		row := db.QueryRowContext(ctx, `SELECT MAX(created_at) FROM events WHERE kind = ?`, kind)
		if err := row.Scan(&ts); err != nil {
			env.Errors = append(env.Errors, fmt.Sprintf("sqlite: last %s: %s", kind, err.Error()))
			continue
		}
		if ts.Valid {
			*dst = time.Unix(ts.Int64, 0).UTC().Format(time.RFC3339)
		}
	}
	var ts sql.NullInt64
	row := db.QueryRowContext(ctx, `SELECT MAX(created_at) FROM events`)
	if err := row.Scan(&ts); err == nil && ts.Valid {
		env.Events.LastAnyAt = time.Unix(ts.Int64, 0).UTC().Format(time.RFC3339)
	}
}

// deriveTrafficLight maps the cheap signals to a green/yellow/red
// enum the operator skill can branch on without parsing the rest of
// the envelope. Rules (simplest viable per feedback_default_simpler):
//   - red   = DB unreachable, OR orchestrator probe failed AND no
//             event has fired in the last 60 s (60 s = ~2 spawn ticks;
//             shorter races healthy boot, longer hides wedged daemon).
//   - yellow = any non-fatal error in errors[], OR last-event age > 5 min
//             (5 min = operator-glance interval per regatta-operator skill;
//             beyond this the loop is suspect, below this it is in motion).
//   - green = orchestrator alive AND event activity within 5 min AND
//             no errors recorded.
func deriveTrafficLight(env StatusJSON, dbReachable bool, now time.Time) string {
	if !dbReachable {
		return trafficLightRed
	}
	stale := lastEventAge(env, now)
	if !env.Orchestrator.Alive && stale > 60*time.Second {
		return trafficLightRed
	}
	if len(env.Errors) > 0 {
		return trafficLightYellow
	}
	if stale > 5*time.Minute {
		return trafficLightYellow
	}
	if !env.Orchestrator.Alive {
		return trafficLightYellow
	}
	return trafficLightGreen
}

// lastEventAge returns the age of the most-recent event kind we
// track. A never-fired surface returns a year so the gate falls into
// the stale branch on a fresh DB.
func lastEventAge(env StatusJSON, now time.Time) time.Duration {
	candidates := []string{env.Events.LastAnyAt, env.Events.LastSpawnAt, env.Events.LastExitAt, env.Events.LastMergeAt}
	var newest time.Time
	for _, raw := range candidates {
		if raw == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			continue
		}
		if t.After(newest) {
			newest = t
		}
	}
	if newest.IsZero() {
		return 365 * 24 * time.Hour
	}
	return now.Sub(newest)
}
