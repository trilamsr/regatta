package web

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

func loadEventsView(ctx context.Context, deps Dependencies) any {
	rows, err := deps.DB.ListEvents(ctx, dashboardEventsTailLimit)
	if err != nil || len(rows) == 0 {
		return dashboardEventsView{EmptyHint: emptyHintEvents}
	}
	return dashboardEventsView{Rows: rows}
}

func serveEventDrawer(w http.ResponseWriter, r *http.Request, deps Dependencies) {
	w.Header().Set("Cache-Control", noStoreCacheControl)
	if deps.Templates == nil || deps.DB == nil {
		writeDrawerNotFound(w)
		return
	}
	idStr := strings.TrimPrefix(r.URL.Path, "/ui/drawer/event/")
	id, err := strconv.ParseInt(idStr, strconvBase10, strconvBitSize64)
	if err != nil {
		writeDrawerNotFound(w)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), dashboardPanelTimeoutSeconds*time.Second)
	defer cancel()
	ev, err := deps.DB.GetEvent(ctx, id)
	if err != nil {
		writeDrawerNotFound(w)
		return
	}
	ev.PayloadJSON = prettyJSON(ev.PayloadJSON)
	if err := deps.Templates.Render(w, "_drawer_event", ev); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
	case string(obs.EventSpawnCompleted):
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
