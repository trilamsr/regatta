package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/trilamsr/regatta/internal/cost/spend"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

type dashboardSpendView struct {
	Last24hMicros        int64
	TodayMicros          int64
	LifetimeMicros       int64
	Spark                []int64
	Err                  string
	EmptyReason          string
	CreditExhaustedCount int
}

type dashboardAgentRow struct {
	ID          int64
	Title       string
	WorkItemID  string
	Lane        string
	State       state.AgentState
	PID         int
	SessionID   string
	PRSHA       string
	Elapsed     string
	SpendMicros int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type dashboardAgentsView struct {
	Rows []dashboardAgentRow
}

type dashboardWorkItemRow struct {
	ID        string
	Title     string
	Lane      string
	UpdatedAt time.Time
}

type dashboardBucket struct {
	Label string
	Count int
	Top   []dashboardWorkItemRow
}

type dashboardWorkItemsView struct {
	Buckets []dashboardBucket
}

type dashboardEventsView struct {
	Rows []state.Event
}

type dashboardFlowNode struct {
	Label string
	Count int
}

type dashboardFlowView struct {
	Nodes []dashboardFlowNode
	Halt  int
}

type dashboardLayoutView struct {
	Now time.Time
}

type dashboardWorkItemDetail struct {
	state.WorkItem
	Acceptance   string
	StatusLabel  string
	RecentEvents []state.Event
}

// statusLabelRunning / statusLabelPROpen / statusLabelBlocked are the operator-facing display tokens shared between work-item status, agent state, and flow-panel buckets so a future palette swap edits one source.
const (
	statusLabelRunning = "running"
	statusLabelPROpen  = "PR open"
	statusLabelBlocked = "blocked"
)

const (
	healthGreen         = "green"
	healthAmber         = "amber"
	healthRed           = "red"
	exitReasonCompleted = "completed"
)

type dashboardDockerSoakView struct {
	Uptime         string
	SpawnsLast1m   int
	ExitedLast1m   int
	LastExitReason string
	LastExitBadge  template.HTML
	Health         string
	HealthLabel    string
	HasLastExit    bool
}

func registerDashboardRoutes(mux *http.ServeMux, deps Dependencies) {
	mux.HandleFunc("/ui/panels/agents", func(w http.ResponseWriter, r *http.Request) {
		serveDashboardPanel(w, r, deps, "_agents", loadAgentsView)
	})
	mux.HandleFunc("/ui/panels/work-items", func(w http.ResponseWriter, r *http.Request) {
		serveDashboardPanel(w, r, deps, "_work_items", loadWorkItemsView)
	})
	mux.HandleFunc("/ui/panels/events", func(w http.ResponseWriter, r *http.Request) {
		serveDashboardPanel(w, r, deps, "_events", loadEventsView)
	})
	mux.HandleFunc("/ui/panels/spend", func(w http.ResponseWriter, r *http.Request) {
		serveDashboardPanel(w, r, deps, "_spend", loadSpendView)
	})
	mux.HandleFunc("/ui/panels/flow", func(w http.ResponseWriter, r *http.Request) {
		serveDashboardPanel(w, r, deps, "_flow", loadFlowView)
	})
	mux.HandleFunc("/ui/panels/docker-soak", func(w http.ResponseWriter, r *http.Request) {
		serveDashboardPanel(w, r, deps, "_docker_soak", loadDockerSoakView)
	})
	mux.HandleFunc("/ui/drawer/agent/", func(w http.ResponseWriter, r *http.Request) {
		serveAgentDrawer(w, r, deps)
	})
	mux.HandleFunc("/ui/drawer/work-item/", func(w http.ResponseWriter, r *http.Request) {
		serveWorkItemDrawer(w, r, deps)
	})
	mux.HandleFunc("/ui/drawer/event/", func(w http.ResponseWriter, r *http.Request) {
		serveEventDrawer(w, r, deps)
	})
}

func serveDashboardPanel(w http.ResponseWriter, r *http.Request, deps Dependencies, name string, loader func(context.Context, Dependencies) any) {
	w.Header().Set("Cache-Control", noStoreCacheControl)
	if deps.Templates == nil || deps.DB == nil {
		http.Error(w, "dashboard dependencies missing", http.StatusInternalServerError)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), dashboardPanelTimeoutSeconds*time.Second)
	defer cancel()
	data := loader(ctx, deps)
	if err := deps.Templates.Render(w, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func loadAgentsView(ctx context.Context, deps Dependencies) any {
	rows, err := deps.DB.ListAgentsByState(ctx,
		state.AgentPending, state.AgentSpawning, state.AgentRunning,
		state.AgentPROpen, state.AgentGatesRunning, state.AgentAwaitingMerge,
	)
	if err != nil || len(rows) == 0 {
		return dashboardAgentsView{}
	}
	ids := make([]string, 0, len(rows))
	for _, a := range rows {
		ids = append(ids, a.WorkItemID)
	}
	titles, _ := deps.DB.GetWorkItemsByIDs(ctx, ids)
	out := make([]dashboardAgentRow, 0, len(rows))
	for _, a := range rows {
		title := a.WorkItemID
		if t, ok := titles[a.WorkItemID]; ok && t != "" {
			title = t
		}
		out = append(out, dashboardAgentRow{
			ID:         a.ID,
			Title:      title,
			WorkItemID: a.WorkItemID,
			Lane:       a.Lane,
			State:      a.State,
			PID:        a.PID,
			SessionID:  a.SessionID,
			PRSHA:      a.PRSHA,
			Elapsed:    humanRelativeShort(deps.Clock(), a.CreatedAt),
			CreatedAt:  a.CreatedAt,
			UpdatedAt:  a.UpdatedAt,
		})
	}
	return dashboardAgentsView{Rows: out}
}

func loadWorkItemsView(ctx context.Context, deps Dependencies) any {
	statuses := []state.WorkItemStatus{
		state.WorkStatusPlanned,
		state.WorkStatusRunning,
		state.WorkStatusPROpen,
		state.WorkStatusMerged,
	}
	labels := []string{"Planned", "Running", statusLabelPROpen, "Merged"}
	summary, err := deps.DB.SummarizeWorkItemStatuses(ctx, statuses, dashboardWorkItemSampleCount)
	if err != nil {
		summary = map[state.WorkItemStatus]state.WorkItemStatusSummary{}
	}
	buckets := make([]dashboardBucket, len(statuses))
	for i, s := range statuses {
		sum := summary[s]
		top := make([]dashboardWorkItemRow, 0, len(sum.Samples))
		for _, w := range sum.Samples {
			top = append(top, dashboardWorkItemRow{
				ID: w.ID, Title: truncate(workItemTitleMaxBytes, workItemDisplayTitle(w)), Lane: w.Lane, UpdatedAt: w.UpdatedAt,
			})
		}
		buckets[i] = dashboardBucket{Label: labels[i], Count: sum.Count, Top: top}
	}
	return dashboardWorkItemsView{Buckets: buckets}
}

func loadFlowView(ctx context.Context, deps Dependencies) any {
	statuses := []state.WorkItemStatus{
		state.WorkStatusPlanned,
		state.WorkStatusRunning,
		state.WorkStatusPROpen,
		state.WorkStatusMerged,
		state.WorkStatusBlocked,
	}
	labels := []string{"backlog", "spawning", "in PR", "merged", statusLabelBlocked}
	summary, _ := deps.DB.SummarizeWorkItemStatuses(ctx, statuses, 0)
	out := make([]dashboardFlowNode, 0, len(statuses))
	halt := 0
	for i, s := range statuses {
		n := summary[s].Count
		out = append(out, dashboardFlowNode{Label: labels[i], Count: n})
		if s == state.WorkStatusRunning {
			halt = n
		}
	}
	return dashboardFlowView{Nodes: out, Halt: halt}
}

func loadEventsView(ctx context.Context, deps Dependencies) any {
	rows, err := deps.DB.ListEvents(ctx, dashboardEventsTailLimit)
	if err != nil {
		return dashboardEventsView{}
	}
	return dashboardEventsView{Rows: rows}
}

// loadDockerSoakView reports daemon uptime + last-1m spawn/exit tallies + latest exit_reason + a health pill so the operator does not have to tail docker logs to know whether the live daemon is healthy.
func loadDockerSoakView(ctx context.Context, deps Dependencies) any {
	view := dashboardDockerSoakView{Health: healthGreen, HealthLabel: "IDLE"}
	now := deps.Clock()
	booted := deps.BootedAt
	if booted.IsZero() {
		booted = now
	}
	view.Uptime = humanizeDuration(now.Sub(booted))
	if deps.DB == nil {
		return view
	}
	since := now.Add(-dockerSoakWindowSeconds * time.Second).Unix()
	spawns, exited, lastReason, lastPayload, nonCompleted, completed := tallyDockerSoakWindow(ctx, deps.DB, since)
	view.SpawnsLast1m = spawns
	view.ExitedLast1m = exited
	view.LastExitReason = lastReason
	view.HasLastExit = lastReason != ""
	view.LastExitBadge = exitReasonBadge(lastPayload)
	switch {
	case exited > 0 && nonCompleted == 0:
		view.Health, view.HealthLabel = healthGreen, "HEALTHY"
	case exited > 0 && completed == 0:
		view.Health, view.HealthLabel = healthRed, "DEGRADED"
	case nonCompleted > 0:
		view.Health, view.HealthLabel = healthAmber, "DEGRADING"
	}
	return view
}

// tallyDockerSoakWindow scans events since `since` and returns spawn/exit counts, latest exit_reason, its payload, and split completed/non-completed exit tallies.
func tallyDockerSoakWindow(ctx context.Context, db *state.DB, since int64) (spawns, exited int, lastReason, lastPayload string, nonCompleted, completed int) {
	rows, err := db.SQL().QueryContext(ctx,
		`SELECT kind, payload_json FROM events WHERE created_at >= ? AND kind IN ('spawn.started','agent.exited') ORDER BY id ASC`,
		since)
	if err != nil {
		return
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var kind, payload string
		if err := rows.Scan(&kind, &payload); err != nil {
			continue
		}
		if kind == "spawn.started" {
			spawns++
			continue
		}
		exited++
		var p struct {
			ExitReason string `json:"exit_reason"`
		}
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			continue
		}
		if p.ExitReason != "" {
			lastReason = p.ExitReason
			lastPayload = payload
		}
		switch p.ExitReason {
		case exitReasonCompleted:
			completed++
		default:
			// empty exit_reason = classifier didn't tag (data drift, pre-#1063 row, or payload schema break). Treat as non-completed so the HEALTHY-pill case (exited>0 && nonCompleted==0) cannot mask silent data corruption.
			nonCompleted++
		}
	}
	return
}

func loadSpendView(ctx context.Context, deps Dependencies) any {
	view := dashboardSpendView{}
	if deps.DB == nil {
		return view
	}
	reader := spend.NewReader(deps.DB.SQL(), deps.Clock)
	now := deps.Clock()
	last24, err := reader.RecordedUSDForWindow(ctx, "default", now.Add(-dashboardLast24hWindow*time.Hour), now)
	if err != nil {
		view.Err = err.Error()
		return view
	}
	view.Last24hMicros = int64(last24)
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	today, err := reader.RecordedUSDForWindow(ctx, "default", todayStart, now)
	if err != nil {
		view.Err = err.Error()
		return view
	}
	view.TodayMicros = int64(today)
	lifetime, err := reader.RecordedUSDForWindow(ctx, "default", time.Unix(0, 0), now)
	if err != nil {
		view.Err = err.Error()
		return view
	}
	view.LifetimeMicros = int64(lifetime)
	view.Spark = buildSparkSeries(ctx, reader, now)
	if view.Last24hMicros == 0 && view.TodayMicros == 0 && view.LifetimeMicros == 0 {
		annotateSpendEmptyReason(ctx, deps.DB, &view, now)
	}
	return view
}

// annotateSpendEmptyReason scans events from the last 24h for agent.exited rows whose payload exit_reason != "completed" so the all-zero spend panel reads as "agents exited before reporting usage" instead of "spend tracker broken". The exit_reason=provider_credit_exhausted count is surfaced separately because it is the dominant cause when the operator's API key is missing or rate-limited.
func annotateSpendEmptyReason(ctx context.Context, db *state.DB, view *dashboardSpendView, now time.Time) {
	since := now.Add(-dashboardLast24hWindow * time.Hour).Unix()
	rows, err := db.SQL().QueryContext(ctx,
		`SELECT payload_json FROM events WHERE kind = ? AND created_at >= ?`,
		"agent.exited", since)
	if err != nil {
		return
	}
	defer func() { _ = rows.Close() }()
	var exitedNonCompleted, credit int
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			continue
		}
		var p struct {
			ExitReason string `json:"exit_reason"`
		}
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			continue
		}
		if p.ExitReason != "" && p.ExitReason != exitReasonCompleted {
			exitedNonCompleted++
		}
		if p.ExitReason == "provider_credit_exhausted" {
			credit++
		}
	}
	if exitedNonCompleted > 0 {
		view.EmptyReason = "agents exited before reporting usage (likely provider auth)"
		view.CreditExhaustedCount = credit
	}
}

// buildSparkSeries fires one RecordedUSDForWindow per histogram bucket because spend.Reader does not (yet) expose a batched window query. 12 buckets × 30s poll = 24 queries/min — each is a small SUM that hits the same composite index, well under sqlite's local-disk cap. A batched API + memo cache lands when spend.Reader gains a window-iterator surface; tracking issue is left to the spend pkg owner since the dashboard is read-side only.
func buildSparkSeries(ctx context.Context, reader *spend.Reader, now time.Time) []int64 {
	out := make([]int64, dashboardSparkBuckets)
	step := dashboardLast24hWindow * time.Hour / dashboardSparkBuckets
	for i := 0; i < dashboardSparkBuckets; i++ {
		end := now.Add(time.Duration(-i) * step)
		start := end.Add(-step)
		v, err := reader.RecordedUSDForWindow(ctx, "default", start, end)
		if err == nil {
			out[dashboardSparkBuckets-1-i] = int64(v)
		}
	}
	return out
}

func serveAgentDrawer(w http.ResponseWriter, r *http.Request, deps Dependencies) {
	w.Header().Set("Cache-Control", noStoreCacheControl)
	if deps.Templates == nil || deps.DB == nil {
		http.NotFound(w, r)
		return
	}
	idStr := strings.TrimPrefix(r.URL.Path, "/ui/drawer/agent/")
	id, err := strconv.ParseInt(idStr, strconvBase10, strconvBitSize64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), dashboardPanelTimeoutSeconds*time.Second)
	defer cancel()
	aPtr, err := deps.DB.GetAgent(ctx, id)
	if err != nil || aPtr == nil {
		http.NotFound(w, r)
		return
	}
	a := *aPtr
	title := a.WorkItemID
	if wi, err := deps.DB.GetWorkItem(ctx, a.WorkItemID); err == nil && wi.Title != "" {
		title = wi.Title
	}
	view := dashboardAgentRow{
		ID: a.ID, Title: title, WorkItemID: a.WorkItemID, Lane: a.Lane,
		State: a.State, PID: a.PID, SessionID: a.SessionID, PRSHA: a.PRSHA,
		CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt,
	}
	if err := deps.Templates.Render(w, "_drawer_agent", view); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func serveWorkItemDrawer(w http.ResponseWriter, r *http.Request, deps Dependencies) {
	w.Header().Set("Cache-Control", noStoreCacheControl)
	if deps.Templates == nil || deps.DB == nil {
		http.NotFound(w, r)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/ui/drawer/work-item/")
	if id == "" {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), dashboardPanelTimeoutSeconds*time.Second)
	defer cancel()
	wi, err := deps.DB.GetWorkItem(ctx, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	view := dashboardWorkItemDetail{
		WorkItem:     wi,
		Acceptance:   prettyJSON(wi.AcceptanceJSON),
		StatusLabel:  workItemStatusLabel(wi.Status),
		RecentEvents: recentEventsForWorkItem(ctx, deps.DB, wi.ID, workItemDrawerEventTail),
	}
	if err := deps.Templates.Render(w, "_drawer_workitem", view); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// workItemDisplayTitle falls back to the work-item ID when the upstream adapter has not yet populated a title — otherwise the kanban card renders as an empty row.
func workItemDisplayTitle(w state.WorkItem) string {
	if w.Title != "" {
		return w.Title
	}
	return w.ID
}

// workItemStatusLabel maps the work_items.status enum onto a short verb so the drawer reads as narrative not raw token.
func workItemStatusLabel(s state.WorkItemStatus) string {
	switch s {
	case state.WorkStatusPlanned:
		return "planned"
	case state.WorkStatusRunning:
		return statusLabelRunning
	case state.WorkStatusPROpen:
		return statusLabelPROpen
	case state.WorkStatusMerged:
		return "merged"
	case state.WorkStatusBlocked:
		return statusLabelBlocked
	case state.WorkStatusRejected:
		return "rejected"
	case state.WorkStatusArchived:
		return "archived"
	}
	return string(s)
}

// recentEventsForWorkItem joins events to the work-item via the agent owning the row so the drawer can show recent activity without a new schema column. Returns nil on any query error so the drawer still renders.
func recentEventsForWorkItem(ctx context.Context, db *state.DB, workItemID string, limit int) []state.Event {
	if db == nil || workItemID == "" || limit <= 0 {
		return nil
	}
	rows, err := db.SQL().QueryContext(ctx,
		`SELECT e.id, e.agent_id, e.kind, e.payload_json, e.created_at
		 FROM events e
		 JOIN agents a ON a.id = e.agent_id
		 WHERE a.work_item_id = ?
		 ORDER BY e.id DESC
		 LIMIT ?`, workItemID, limit)
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()
	var out []state.Event
	for rows.Next() {
		var e state.Event
		var created int64
		if err := rows.Scan(&e.ID, &e.AgentID, &e.Kind, &e.PayloadJSON, &created); err != nil {
			return nil
		}
		e.CreatedAt = time.Unix(created, 0).UTC()
		out = append(out, e)
	}
	return out
}

func serveEventDrawer(w http.ResponseWriter, r *http.Request, deps Dependencies) {
	w.Header().Set("Cache-Control", noStoreCacheControl)
	if deps.Templates == nil || deps.DB == nil {
		http.NotFound(w, r)
		return
	}
	idStr := strings.TrimPrefix(r.URL.Path, "/ui/drawer/event/")
	id, err := strconv.ParseInt(idStr, strconvBase10, strconvBitSize64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), dashboardPanelTimeoutSeconds*time.Second)
	defer cancel()
	ev, err := deps.DB.GetEvent(ctx, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ev.PayloadJSON = prettyJSON(ev.PayloadJSON)
	if err := deps.Templates.Render(w, "_drawer_event", ev); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func prettyJSON(raw string) string {
	if raw == "" {
		return ""
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(raw), "", "  "); err != nil {
		return raw
	}
	return buf.String()
}

// humanRelativeShort returns a compact "5m" / "2h" / "1d" elapsed marker so the dense agents row stays under 4 chars per cell. Times within 60s read "<1m" so the operator does not see fractional minutes lying about precision.
func humanRelativeShort(now, then time.Time) string {
	d := now.Sub(then)
	if d < time.Minute {
		return "<1m"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d/time.Minute))
	}
	if d < hoursPerDay*time.Hour {
		return fmt.Sprintf("%dh", int(d/time.Hour))
	}
	return fmt.Sprintf("%dd", int(d/(hoursPerDay*time.Hour)))
}

// statusClass maps the agent state enum onto the .pill-* css classes the section uses for signal coloring. Centralised so a future state addition (e.g. agent_halt_provider_credit) lights up the css side without re-touching every template.
func statusClass(s state.AgentState) string {
	switch s {
	case state.AgentRunning, state.AgentSpawning, state.AgentPending:
		return "running"
	case state.AgentPROpen:
		return "pr-open"
	case state.AgentGatesRunning, state.AgentAwaitingMerge:
		return "gates"
	case state.AgentDone:
		return "done"
	case state.AgentCrashed, state.AgentGatesFailed:
		return "blocked"
	default:
		return "planned"
	}
}

// statusLabel keeps the row scannable. Long substrate enum tokens (eg gates_running) read as small-caps single-word verbs (eg GATES) so the operator's eye lands on signal not detail.
func statusLabel(s state.AgentState) string {
	switch s {
	case state.AgentRunning:
		return statusLabelRunning
	case state.AgentSpawning:
		return "spawn"
	case state.AgentPending:
		return "pending"
	case state.AgentPROpen:
		return statusLabelPROpen
	case state.AgentGatesRunning:
		return "gates"
	case state.AgentAwaitingMerge:
		return "waiting"
	case state.AgentDone:
		return "done"
	case state.AgentCrashed:
		return "crashed"
	case state.AgentGatesFailed:
		return "failed"
	default:
		return string(s)
	}
}

// eventVerb turns the substrate's state-machine event tokens into one short operator-readable sentence so the recent-activity panel reads as narrative not log noise. Unknown kinds fall through to template.HTMLEscapeString(e.Kind) so a future event_kind addition stays XSS-safe; the verb branches concatenate only numeric ids (%d-formatted) and static spans, while work_item_id values from PayloadJSON are routed through template.HTMLEscapeString before splicing — keep that invariant for any new payload-derived field.
//
//nolint:gosec // see godoc above — every branch returns either static HTML, %d-formatted numeric ids, HTMLEscapeString(kind), or HTMLEscapeString-wrapped payload chip values
func eventVerb(e state.Event) template.HTML {
	id := ""
	if e.AgentID.Valid {
		id = fmt.Sprintf(" <span class=\"strong\">#%d</span>", e.AgentID.Int64)
	}
	wi := workItemChip(e.PayloadJSON)
	switch e.Kind {
	case "agent.exited":
		return template.HTML("agent"+id+" <span class=\"acc\">exited</span>") + wi + exitReasonBadge(e.PayloadJSON)
	case "spawned", "spawn.started":
		return template.HTML("agent"+id+" <span class=\"acc\">spawned</span>") + wi
	case "spawn.completed":
		return template.HTML("agent"+id+" <span class=\"acc-2\">ready</span>") + wi
	case "spawn_failed", "spawn.failed":
		return template.HTML("agent"+id+" <span class=\"acc\">spawn failed</span>") + wi + exitReasonBadge(e.PayloadJSON)
	case "recovered_crashed":
		return template.HTML("agent"+id+" <span class=\"acc\">recovered crashed</span>") + wi
	case "brief.loaded":
		return template.HTML("agent"+id+" <span class=\"acc-2\">brief loaded</span>") + wi
	case "merge.completed", "merge_completed":
		return template.HTML("agent"+id+" <span class=\"acc-2\">merge completed</span>") + wi
	case "tick.started":
		return template.HTML("scheduler ticked")
	case "tick.completed":
		return template.HTML("scheduler tick completed")
	case "agent_pr_opened":
		return template.HTML("agent"+id+" <span class=\"acc-2\">opened PR</span>") + wi
	case "agent_pr_merged":
		return template.HTML("agent"+id+" <span class=\"acc-2\">PR merged</span>") + wi
	default:
		return template.HTML(template.HTMLEscapeString(e.Kind)) + wi
	}
}

// workItemChip extracts work_item_id from the event payload and returns it as an inline chip (template.HTML). The value is HTMLEscapeString'd before splicing; empty / missing / malformed payloads yield "" so the row stays clean instead of carrying a 0-byte artifact.
//
//nolint:gosec // payload value is run through template.HTMLEscapeString; the wrapping span is a static literal
func workItemChip(payload string) template.HTML {
	if payload == "" {
		return ""
	}
	var p struct {
		WorkItemID string `json:"work_item_id"`
	}
	if err := json.Unmarshal([]byte(payload), &p); err != nil || p.WorkItemID == "" {
		return ""
	}
	return template.HTML(` <span class="chip-wi">` + template.HTMLEscapeString(p.WorkItemID) + `</span>`)
}

// exitReasonBadge parses agent.exited payload + returns a colored badge as template.HTML keyed by exit_reason. The reason value is run through template.HTMLEscapeString and the color tokens are a static switch (no caller-controlled bytes splice into the output). Returns "" on payload parse failure, empty exit_reason, or a clean "completed" so the operator's eye lands on actionable exits only.
func exitReasonBadge(payload string) template.HTML {
	if payload == "" {
		return ""
	}
	var p struct {
		ExitReason string `json:"exit_reason"`
	}
	if err := json.Unmarshal([]byte(payload), &p); err != nil || p.ExitReason == "" {
		return ""
	}
	var color string
	switch p.ExitReason {
	case exitReasonCompleted:
		return ""
	case "provider_credit_exhausted":
		color = "red"
	case "provider_rate_limited", "provider_internal_error":
		color = "orange"
	case "tool_denied":
		color = "yellow"
	default:
		color = "gray"
	}
	//nolint:gosec // color is a static switch token; p.ExitReason is HTMLEscapeString'd; no caller-controlled bytes splice into the output
	return template.HTML(` <span class="badge badge-` + color + `">` + template.HTMLEscapeString(p.ExitReason) + `</span>`)
}

// relTime returns the same compact elapsed marker humanRelativeShort uses, but as a template func so the event log lines and work-item cards share one source of truth. Today + Now closures live at template-load time so test harnesses can pin the clock.
func relTimeFn(clock func() time.Time) func(time.Time) string {
	if clock == nil {
		clock = time.Now
	}
	return func(t time.Time) string {
		return humanRelativeShort(clock(), t)
	}
}

// sparkSVG renders a 24h spend histogram as a tiny inline SVG so the spend panel carries a trend signal without a charting dep. Empty / zero series collapses to a flat baseline so the metric reads "—" visually instead of confusing the operator with a flat-line-at-zero overlay.
//
//nolint:gosec // returned HTML wraps fmt.Sprintf'd static SVG with %d/%.1f-formatted numeric coords — no caller-controlled string interpolation
func sparkSVG(series []int64) template.HTML {
	if len(series) == 0 {
		return template.HTML(fmt.Sprintf(`<svg class="spark" width="%d" height="%d"></svg>`, dashboardSparkWidth, dashboardSparkHeight))
	}
	var maxv int64
	for _, v := range series {
		if v > maxv {
			maxv = v
		}
	}
	if maxv == 0 {
		return template.HTML(fmt.Sprintf(`<svg class="spark" width="%d" height="%d"><line x1="0" y1="%d" x2="%d" y2="%d" stroke="#c8c3b6"/></svg>`, dashboardSparkWidth, dashboardSparkHeight, dashboardSparkBarMaxH, dashboardSparkWidth, dashboardSparkBarMaxH))
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<svg class="spark" width="%d" height="%d" viewBox="0 0 %d %d" preserveAspectRatio="none">`, dashboardSparkWidth, dashboardSparkHeight, dashboardSparkWidth, dashboardSparkHeight)
	step := float64(dashboardSparkWidth) / float64(len(series))
	for i, v := range series {
		h := float64(v) / float64(maxv) * float64(dashboardSparkBarMaxH)
		x := float64(i) * step
		y := float64(dashboardSparkBaseline) - h
		_, _ = fmt.Fprintf(&b, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="#c2410c"/>`, x, y, step-1, h)
	}
	b.WriteString(`</svg>`)
	return template.HTML(b.String())
}
