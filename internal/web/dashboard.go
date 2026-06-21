package web

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"time"

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
	Rows      []dashboardAgentRow
	EmptyHint string
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
	Buckets   []dashboardBucket
	EmptyHint string
}

type dashboardEventsView struct {
	Rows      []state.Event
	EmptyHint string
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

type dashboardPipelineStage struct {
	Slug  string
	Label string
	Owner string
	Count int
}

type dashboardPipelineView struct {
	Stages []dashboardPipelineStage
}

type dashboardPipelineItem struct {
	WorkItemID string
	AgentID    int64
	Lane       string
	Age        string
}

type dashboardPipelineDrawer struct {
	Slug  string
	Label string
	Owner string
	Items []dashboardPipelineItem
	// Err mirrors dashboardSpendView.Err: a non-empty value flips the drawer to a
	// degraded block so a DB-unavailable stage reads distinct from a genuinely-empty one.
	Err string
}

const (
	pipelineStageQueued   = "queued"
	pipelineStageReady    = "ready"
	pipelineStageSpawning = "spawning"
	pipelineStageRunning  = "running"
	pipelineStagePROpen   = "pr_open"
	pipelineStageDone     = "done"
)

type dashboardWorkItemDetail struct {
	state.WorkItem
	Acceptance   string
	StatusLabel  string
	RecentEvents []state.Event
	// IssueURL is empty unless web.Config.GitHubRepo is wired. When set, the drawer
	// renders a direct link to the upstream issue so the operator can verify context
	// without a separate search step.
	IssueURL string
	// StatusFlow is the 4-step pill row (pending → spawning → running → done/crashed)
	// derived from the owning agent's state. Empty when no agent exists for the item.
	StatusFlow []workItemFlowStep
	// BodyPreview is the first bodyPreviewMaxRunes runes of the upstream issue body.
	// Sourced from AcceptanceJSON until #1092 lands a dedicated body column.
	BodyPreview string
}

// workItemFlowStep renders one cell of the 4-step status pill row. Active marks the
// cell that matches the owning agent's current AgentState.
type workItemFlowStep struct {
	Label  string
	Active bool
}

// workItemFlowLabels are the canonical drawer pill labels. The final cell flips to
// "crashed" when the agent terminated in AgentCrashed.
var workItemFlowLabels = [workItemFlowStepCount]string{statusLabelPending, statusLabelSpawning, statusLabelRunning, statusLabelDone}

// statusLabelRunning / statusLabelPROpen / statusLabelBlocked / statusLabelDone are the operator-facing display tokens shared between work-item status, agent state, and flow-panel buckets so a future palette swap edits one source.
const (
	statusLabelRunning  = "running"
	statusLabelPROpen   = "PR open"
	statusLabelBlocked  = "blocked"
	statusLabelDone     = "done"
	statusLabelPending  = "pending"
	statusLabelSpawning = "spawning"
	bucketLabelRunning  = "Running"
)

const (
	healthGreen         = "green"
	healthAmber         = "amber"
	healthRed           = "red"
	exitReasonCompleted = "completed"
)

// emptyHint* constants pin the operator-facing copy for blank dashboard panels so a future palette / cadence change edits one source instead of N templates. WHY: a blank "loading…" or empty div leaves the operator guessing whether the scheduler is wedged or simply idle.
const (
	emptyHintAgents    = "No agents in flight. Scheduler ticks every 5s."
	emptyHintWorkItems = "No work-items found. Adapter polls every 30s; check spec_adapter.selector in regatta.yaml."
	emptyHintEvents    = "No events in last 24h."
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
	mux.HandleFunc("/ui/panels/pipeline", func(w http.ResponseWriter, r *http.Request) {
		serveDashboardPanel(w, r, deps, "_pipeline", loadPipelineView)
	})
	mux.HandleFunc("/ui/panels/health", func(w http.ResponseWriter, r *http.Request) {
		serveHealthPanel(w, r, deps)
	})
	mux.HandleFunc("/ui/drawer/pipeline/", func(w http.ResponseWriter, r *http.Request) {
		servePipelineDrawer(w, r, deps)
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

func scanInt(ctx context.Context, sqlDB *sql.DB, query string, args ...any) int {
	var n int
	if err := sqlDB.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		return 0
	}
	return n
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
