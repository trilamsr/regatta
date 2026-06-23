package web

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

func loadAgentsView(ctx context.Context, deps Dependencies) any {
	rows, err := deps.DB.ListAgentsByState(ctx,
		state.AgentPending, state.AgentSpawning, state.AgentRunning,
		state.AgentPROpen, state.AgentGatesRunning, state.AgentAwaitingMerge,
	)
	if err != nil || len(rows) == 0 {
		return dashboardAgentsView{EmptyHint: emptyHintAgents}
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

func serveAgentDrawer(w http.ResponseWriter, r *http.Request, deps Dependencies) {
	w.Header().Set("Cache-Control", noStoreCacheControl)
	if deps.Templates == nil || deps.DB == nil {
		writeDrawerNotFound(w)
		return
	}
	idStr := strings.TrimPrefix(r.URL.Path, "/ui/drawer/agent/")
	id, err := strconv.ParseInt(idStr, strconvBase10, strconvBitSize64)
	if err != nil {
		writeDrawerNotFound(w)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), dashboardPanelTimeoutSeconds*time.Second)
	defer cancel()
	aPtr, err := deps.DB.GetAgent(ctx, id)
	if err != nil || aPtr == nil {
		writeDrawerNotFound(w)
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

// statusClass maps the agent state enum onto the .pill-* css classes the section uses for signal coloring. Centralised so a future state addition (e.g. agent_halt_provider_credit) lights up the css side without re-touching every template.
func statusClass(s state.AgentState) string {
	switch s {
	case state.AgentRunning, state.AgentSpawning, state.AgentPending:
		return statusLabelRunning
	case state.AgentPROpen:
		return "pr-open"
	case state.AgentGatesRunning, state.AgentAwaitingMerge:
		return "gates"
	case state.AgentDone:
		return statusLabelDone
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
		return statusLabelPending
	case state.AgentPROpen:
		return statusLabelPROpen
	case state.AgentGatesRunning:
		return "gates"
	case state.AgentAwaitingMerge:
		return "waiting"
	case state.AgentDone:
		return statusLabelDone
	case state.AgentCrashed:
		return "crashed"
	case state.AgentGatesFailed:
		return "failed"
	default:
		return string(s)
	}
}
