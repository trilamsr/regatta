package web

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

func loadWorkItemsView(ctx context.Context, deps Dependencies) any {
	statuses := []state.WorkItemStatus{
		state.WorkStatusPlanned,
		state.WorkStatusRunning,
		state.WorkStatusPROpen,
		state.WorkStatusMerged,
	}
	labels := []string{"Planned", bucketLabelRunning, statusLabelPROpen, "Merged"}
	summary, err := deps.DB.SummarizeWorkItemStatuses(ctx, statuses, dashboardWorkItemSampleCount)
	if err != nil {
		summary = map[state.WorkItemStatus]state.WorkItemStatusSummary{}
	}
	// #1217 / #MAY-48: Running bucket tracks live agents — work_items.status
	// lags spawning. Sourced from the consolidated snapshot so this panel + the
	// pipeline panel + future health cells all read the same number.
	var runningAgents int
	if snap, ok := loadHealthSnapshotView(ctx, deps).(dashboardHealthSnapshot); ok {
		runningAgents = snap.AgentStateCounts[state.AgentRunning]
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
		count := sum.Count
		if s == state.WorkStatusRunning {
			count = runningAgents
		}
		buckets[i] = dashboardBucket{Label: labels[i], Count: count, Top: top}
	}
	view := dashboardWorkItemsView{Buckets: buckets}
	total := 0
	for _, b := range buckets {
		total += b.Count
	}
	if total == 0 {
		view.EmptyHint = emptyHintWorkItems
	}
	return view
}

func loadFlowView(ctx context.Context, deps Dependencies) any {
	statuses := []state.WorkItemStatus{
		state.WorkStatusPlanned,
		state.WorkStatusRunning,
		state.WorkStatusPROpen,
		state.WorkStatusMerged,
		state.WorkStatusBlocked,
	}
	labels := []string{"backlog", statusLabelSpawning, "in PR", "merged", statusLabelBlocked}
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
	var agentState state.AgentState
	if a, err := deps.DB.GetAgentByWorkItemID(ctx, wi.ID); err == nil && a != nil {
		agentState = a.State
	}
	view := dashboardWorkItemDetail{
		WorkItem:     wi,
		Acceptance:   prettyJSON(wi.AcceptanceJSON),
		StatusLabel:  workItemStatusLabel(wi.Status),
		RecentEvents: recentEventsForWorkItem(ctx, deps.DB, wi.ID, workItemDrawerEventTail),
		IssueURL:     buildIssueURL(deps.Config.GitHubRepo, wi.ID),
		StatusFlow:   buildStatusFlow(agentState),
		BodyPreview:  buildBodyPreview(wi.AcceptanceJSON, bodyPreviewMaxRunes),
	}
	if err := deps.Templates.Render(w, "_drawer_workitem", view); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

var repoSlugRE = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)

// buildIssueURL produces a github.com issue URL from a "owner/name" slug + a work-item
// id like "BUG-1058" or "1058". Returns "" when repo is empty OR the trailing token has
// no digits — both cases avoid linking to a 404. The prefix-stripping pass tolerates
// the "BUG-", "FEAT-", "CHORE-" conventions emitted by the github_issues adapter.
func buildIssueURL(repo, id string) string {
	if repo == "" || id == "" {
		return ""
	}
	// Reviewer aa2355db4abd1de00 (#1149): reject malformed owner/name slugs (path-traversal, empty halves, embedded `/`).
	if !repoSlugRE.MatchString(repo) {
		return ""
	}
	num := id
	if i := strings.LastIndex(id, "-"); i >= 0 && i < len(id)-1 {
		num = id[i+1:]
	}
	if _, err := strconv.Atoi(num); err != nil {
		return ""
	}
	return fmt.Sprintf("https://github.com/%s/issues/%s", repo, num)
}

// buildStatusFlow maps an AgentState onto the 4-step pill row. The 11-value AgentState
// enum collapses into 4 operator-visible buckets so the drawer reads as a single linear
// flow rather than a debugger view. Crashed flips the final cell label to "crashed".
// Empty state (no agent yet) returns the row with no active cell.
func buildStatusFlow(s state.AgentState) []workItemFlowStep {
	out := make([]workItemFlowStep, workItemFlowStepCount)
	for i, label := range workItemFlowLabels {
		out[i] = workItemFlowStep{Label: label}
	}
	switch s {
	case state.AgentPending:
		out[workItemFlowIdxPending].Active = true
	case state.AgentSpawning:
		out[workItemFlowIdxSpawning].Active = true
	case state.AgentRunning, state.AgentPROpen, state.AgentGatesRunning, state.AgentAwaitingMerge, state.AgentGatesFailed:
		out[workItemFlowIdxRunning].Active = true
	case state.AgentDone, state.AgentWithdrawn:
		out[workItemFlowIdxDone].Active = true
	case state.AgentCrashed, state.AgentEscalated:
		out[workItemFlowIdxDone].Label = workItemFlowLabelCrashed
		out[workItemFlowIdxDone].Active = true
	}
	return out
}

// buildBodyPreview truncates s to the first n runes — rune-safe so a multi-byte
// trailing character does not produce invalid utf-8. When s is a JSON document
// with a "body" field (the AcceptanceJSON stopgap until #1092 ships a dedicated
// Body column), extract that field instead of showing raw JSON to the operator.
// Returns "" for empty inputs to suppress an otherwise-empty drawer section.
func buildBodyPreview(s string, n int) string {
	if s == "" || n <= 0 {
		return ""
	}
	// Reviewer aa2355db4abd1de00 (#1149): unwrap JSON body field if present so
	// the operator does not see raw `{"body":"text"}` quoted blobs in the drawer.
	var doc struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal([]byte(s), &doc); err == nil && doc.Body != "" {
		s = doc.Body
	}
	runes := []rune(s)
	if len(runes) <= n {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(string(runes[:n])) + "…"
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

// recentEventsForWorkItem joins events to the work-item via the agent owning the row so the drawer can show recent activity without a new schema column. Query-level errors return nil so the drawer still renders; per-row scan errors log WARN with the work_item_id attr and skip the row so one corrupt row never collapses the whole list into a silent empty drawer (#1135).
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
			// slog.Default() goes to stderr when SetDefault is not called; deps.Logger threading is a separate refactor.
			slog.WarnContext(ctx, "dashboard.recent_events_scan_error",
				string(obs.KeyWorkItemID), workItemID,
				"err", err)
			continue
		}
		e.CreatedAt = time.Unix(created, 0).UTC()
		out = append(out, e)
	}
	return out
}
