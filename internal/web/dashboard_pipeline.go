package web

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/web/internalerror"
)

// pipelineStageOrder fixes display order so a future stage insertion stays one-line edit.
var pipelineStageOrder = []struct {
	Slug  string
	Label string
	Owner string
}{
	{pipelineStageQueued, pipelineStageQueued, "Adapter"},
	{pipelineStageReady, pipelineStageReady, "Scheduler"},
	{pipelineStageSpawning, pipelineStageSpawning, "Spawner"},
	{pipelineStageRunning, statusLabelRunning, "Agents"},
	{pipelineStagePROpen, "pr open", "PR-watch"},
	{pipelineStageDone, pipelineStageDone, "Merge"},
}

func loadPipelineView(ctx context.Context, deps Dependencies) any {
	view := dashboardPipelineView{Stages: make([]dashboardPipelineStage, 0, len(pipelineStageOrder))}
	if deps.DB == nil {
		return view
	}
	counts := pipelineCountsFromSnapshot(ctx, deps)
	for _, s := range pipelineStageOrder {
		view.Stages = append(view.Stages, dashboardPipelineStage{
			Slug: s.Slug, Label: s.Label, Owner: s.Owner, Count: counts[s.Slug],
		})
	}
	return view
}

// pipelineCountsFromSnapshot derives per-stage counts from the consolidated
// health snapshot (#MAY-48 AC1) so the pipeline panel + work-items Running
// bucket + future health cells all source from one GROUP BY — eliminating
// the cross-panel drift of #1217. The queued stage still issues its own
// LEFT-JOIN-shaped SELECT because it counts work_items without agents,
// not agents in a state — different cardinality.
func pipelineCountsFromSnapshot(ctx context.Context, deps Dependencies) map[string]int {
	snap, ok := loadHealthSnapshotView(ctx, deps).(dashboardHealthSnapshot)
	if !ok {
		return map[string]int{}
	}
	out := map[string]int{
		pipelineStageReady:    snap.AgentStateCounts[state.AgentPending],
		pipelineStageSpawning: snap.AgentStateCounts[state.AgentSpawning],
		pipelineStageRunning:  snap.AgentStateCounts[state.AgentRunning],
		pipelineStagePROpen:   snap.AgentStateCounts[state.AgentPROpen],
		pipelineStageDone:     snap.AgentStateCounts[state.AgentDone] + snap.AgentStateCounts[state.AgentWithdrawn],
	}
	out[pipelineStageQueued] = scanInt(ctx, deps.DB.SQL(),
		`SELECT COUNT(*) FROM work_items w
		 WHERE w.status = ? AND NOT EXISTS (SELECT 1 FROM agents a WHERE a.work_item_id = w.id)`,
		string(state.WorkStatusPlanned))
	return out
}

func servePipelineDrawer(w http.ResponseWriter, r *http.Request, deps Dependencies) {
	w.Header().Set("Cache-Control", noStoreCacheControl)
	if deps.Templates == nil || deps.DB == nil {
		writeDrawerNotFound(w)
		return
	}
	slug := strings.TrimPrefix(r.URL.Path, "/ui/drawer/pipeline/")
	var meta struct{ Slug, Label, Owner string }
	for _, s := range pipelineStageOrder {
		if s.Slug == slug {
			meta.Slug, meta.Label, meta.Owner = s.Slug, s.Label, s.Owner
			break
		}
	}
	if meta.Slug == "" {
		writeDrawerNotFound(w)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), dashboardPanelTimeoutSeconds*time.Second)
	defer cancel()
	view := dashboardPipelineDrawer{Slug: meta.Slug, Label: meta.Label, Owner: meta.Owner}
	items, err := loadPipelineStageItems(ctx, deps, slug)
	if err != nil {
		view.Err = err.Error()
	} else {
		view.Items = items
	}
	if err := deps.Templates.Render(w, "_drawer_pipeline_stage", view); err != nil {
		internalerror.Write(w, nil, "web.drawer_pipeline_render", err)
	}
}

func loadPipelineStageItems(ctx context.Context, deps Dependencies, slug string) ([]dashboardPipelineItem, error) {
	now := deps.Clock()
	if slug == pipelineStageQueued {
		rows, err := deps.DB.SQL().QueryContext(ctx,
			`SELECT w.id, w.lane, w.updated_at FROM work_items w
			 WHERE w.status = ? AND NOT EXISTS (SELECT 1 FROM agents a WHERE a.work_item_id = w.id)
			 ORDER BY w.updated_at DESC LIMIT ?`,
			string(state.WorkStatusPlanned), pipelineDrawerLimit)
		if err != nil {
			return nil, err
		}
		defer func() { _ = rows.Close() }()
		var out []dashboardPipelineItem
		for rows.Next() {
			var id, lane string
			var updated int64
			if err := rows.Scan(&id, &lane, &updated); err != nil {
				return nil, err
			}
			out = append(out, dashboardPipelineItem{
				WorkItemID: id, Lane: lane,
				Age: humanRelativeShort(now, time.Unix(updated, 0).UTC()),
			})
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return out, nil
	}
	agents, err := agentsForPipelineSlug(ctx, deps.DB, slug)
	if err != nil {
		return nil, err
	}
	out := make([]dashboardPipelineItem, 0, len(agents))
	for _, a := range agents {
		out = append(out, dashboardPipelineItem{
			WorkItemID: a.WorkItemID, AgentID: a.ID, Lane: a.Lane,
			Age: humanRelativeShort(now, a.CreatedAt),
		})
	}
	return out, nil
}

func agentsForPipelineSlug(ctx context.Context, db *state.DB, slug string) ([]state.Agent, error) {
	var states []state.AgentState
	switch slug {
	case pipelineStageReady:
		states = []state.AgentState{state.AgentPending}
	case pipelineStageSpawning:
		states = []state.AgentState{state.AgentSpawning}
	case pipelineStageRunning:
		states = []state.AgentState{state.AgentRunning}
	case pipelineStagePROpen:
		states = []state.AgentState{state.AgentPROpen}
	case pipelineStageDone:
		states = []state.AgentState{state.AgentDone, state.AgentWithdrawn}
	default:
		return nil, nil
	}
	rows, err := db.ListAgentsByState(ctx, states...)
	if err != nil {
		return nil, err
	}
	if len(rows) > pipelineDrawerLimit {
		rows = rows[:pipelineDrawerLimit]
	}
	return rows, nil
}
