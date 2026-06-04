package program

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/trilamsr/regatta/internal/orchestrator/scheduler"
	"github.com/trilamsr/regatta/internal/orchestrator/spawner"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// TestEndToEnd_V2_ConditionalDAG asserts the conditional-DAG pipeline (Sync → Tick → Complete) propagates predicate fires and default fallback via row/status/journal shape.
func TestEndToEnd_V2_ConditionalDAG(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test; skip with -short")
	}
	ctx := context.Background()
	db := newBriefTestDB(t)

	key := []byte("test-key-32-bytes-aaaaaaaaaaaaaaa")
	keyring := map[string][]byte{"key-1": key}
	t0 := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	seedParent(t, db, "PROG-2", t0)

	raw := mustSignedBriefV2WithOpts(t, key, "PROG-2", "m-eeeeeeeeeeee",
		[]PlannedFeatureV2{
			{
				PlannedFeature: PlannedFeature{
					ID:       "F-SCAN",
					Title:    "scan",
					Fulfills: []string{"c1"},
				},
				OutputsSchema: &OutputsSchema{
					Type: "object",
					Properties: map[string]*OutputsSchema{
						"severity": {Type: "string"},
					},
				},
				Edges: []Edge{
					{From: "F-SCAN", To: "F-DEEP", Predicate: `out.severity == "high"`, OnSkip: SkipCascade},
				},
				DefaultNext: strPtr("F-QUICK"),
			},
			{
				PlannedFeature: PlannedFeature{ID: "F-DEEP", Title: "deep-remediate", Fulfills: []string{"c2"}},
				Edges:          []Edge{{From: "F-DEEP", To: "F-QUICK"}},
			},
			{PlannedFeature: PlannedFeature{ID: "F-QUICK", Title: "fast-path", Fulfills: []string{"c3"}}},
		}, t0)

	briefDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(briefDir, "PROG-2.json"), raw, 0o600); err != nil {
		t.Fatalf("write brief: %v", err)
	}

	evaluator := NewEdgeEvaluator()
	loader, err := NewBriefLoader(BriefLoaderConfig{FS: os.DirFS(briefDir), DB: db, Keyring: keyring, Evaluator: evaluator})
	if err != nil {
		t.Fatalf("NewBriefLoader: %v", err)
	}
	if err := loader.Sync(ctx, t0); err != nil {
		t.Fatalf("BriefLoader.Sync: %v", err)
	}

	scanFrom, err := db.ListEdgesFrom(ctx, "F-SCAN")
	if err != nil {
		t.Fatalf("ListEdgesFrom F-SCAN: %v", err)
	}
	if len(scanFrom) != 2 {
		t.Fatalf("F-SCAN edges=%d want 2 (1 predicated + 1 default): %+v", len(scanFrom), scanFrom)
	}
	deepFrom, err := db.ListEdgesFrom(ctx, "F-DEEP")
	if err != nil {
		t.Fatalf("ListEdgesFrom F-DEEP: %v", err)
	}
	if len(deepFrom) != 1 {
		t.Fatalf("F-DEEP edges=%d want 1 (chain to F-QUICK): %+v", len(deepFrom), deepFrom)
	}

	sched := scheduler.New(db, scheduler.Config{
		LockTTL:        time.Minute,
		Evaluator:      adaptEvaluator{evaluator},
		OutputsSchemas: resolverFor(loader),
	})
	stub := spawner.New(spawner.Config{DB: db})

	// Drive the pipeline. Each tick: scheduler reserves spawnable
	// work_items, then we run Complete on every reserved agent's
	// work_item with the appropriate payload. F-SCAN ships severity=
	// high; downstream features ship {}. Loop bounded so a wedged
	// pipeline cannot hang the test runner.
	const maxTicks = 6
	for i := 0; i < maxTicks; i++ {
		reserved, err := sched.Tick(ctx)
		if err != nil {
			t.Fatalf("tick %d: %v", i, err)
		}
		for _, agentID := range reserved {
			a, err := db.GetAgent(ctx, agentID)
			if err != nil {
				t.Fatalf("get agent %d: %v", agentID, err)
			}
			if _, err := stub.Spawn(ctx, spawner.Request{
				AgentID: a.ID, WorkItemID: a.WorkItemID, Lane: a.Lane,
			}); err != nil {
				t.Fatalf("spawn %s: %v", a.WorkItemID, err)
			}
			payload := json.RawMessage(`{}`)
			if a.WorkItemID == "F-SCAN" {
				payload = json.RawMessage(`{"severity":"high"}`)
			}
			if err := stub.Complete(ctx, a.WorkItemID, payload); err != nil {
				t.Fatalf("complete %s: %v", a.WorkItemID, err)
			}
		}
	}

	scan, err := db.GetWorkItem(ctx, "F-SCAN")
	if err != nil || scan.Status != state.WorkStatusMerged {
		t.Fatalf("F-SCAN status=%s err=%v want merged", scan.Status, err)
	}
	deep, err := db.GetWorkItem(ctx, "F-DEEP")
	if err != nil || deep.Status != state.WorkStatusMerged {
		t.Fatalf("F-DEEP status=%s err=%v want merged (predicate severity==high fires)", deep.Status, err)
	}
	quick, err := db.GetWorkItem(ctx, "F-QUICK")
	if err != nil || quick.Status != state.WorkStatusMerged {
		t.Fatalf("F-QUICK status=%s err=%v want merged (transit edge F-DEEP -> F-QUICK fires after F-DEEP merges)", quick.Status, err)
	}

	scanEdges, err := db.ListEdgesFrom(ctx, "F-SCAN")
	if err != nil {
		t.Fatalf("ListEdgesFrom F-SCAN: %v", err)
	}
	var firedDeep bool
	for _, e := range scanEdges {
		if e.ToID == "F-DEEP" && !e.IsDefault && e.Fired == "true" {
			firedDeep = true
			if e.FiredAgainst == "" {
				t.Fatalf("F-SCAN -> F-DEEP fired=true but fired_against (journal sha) empty: %+v", e)
			}
		}
	}
	if !firedDeep {
		t.Fatalf("F-SCAN -> F-DEEP predicated edge must be fired=true (severity=high satisfies predicate): %+v", scanEdges)
	}
	deepEdges, err := db.ListEdgesFrom(ctx, "F-DEEP")
	if err != nil {
		t.Fatalf("ListEdgesFrom F-DEEP: %v", err)
	}
	if len(deepEdges) != 1 || deepEdges[0].Fired != "true" {
		t.Fatalf("F-DEEP -> F-QUICK transit edge must be fired=true after F-DEEP merge: %+v", deepEdges)
	}

	journal, err := db.GetLatestOutput(ctx, "F-SCAN")
	if err != nil {
		t.Fatalf("GetLatestOutput F-SCAN: %v", err)
	}
	if journal.ContentSHA == "" {
		t.Fatalf("F-SCAN journal content_sha empty")
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(journal.OutputJSON), &payload); err != nil {
		t.Fatalf("journal output JSON not parseable: %v", err)
	}
	if payload["severity"] != "high" {
		t.Fatalf("F-SCAN journal severity=%v want high", payload["severity"])
	}
}

// adaptEvaluator boxes a *program.EdgeEvaluator into the scheduler-
// side interface. Mirrors cmd/regatta/main.go schedulerEvaluator so
// this test exercises the same adapter shape production wires.
type adaptEvaluator struct{ ev *EdgeEvaluator }

func (a adaptEvaluator) Eval(ctx context.Context, edge state.EdgeRow, schema any, journal state.OutputJournalEntry) (bool, string, error) {
	sch, _ := schema.(*OutputsSchema)
	return a.ev.Eval(ctx, edge, sch, journal)
}

// resolverFor closes over BriefLoader's OutputsSchemaForFeature and
// boxes the typed return into the scheduler's `any`-typed seam. Same
// shape as cmd/regatta/main.go outputsSchemaResolverFor.
func resolverFor(loader *BriefLoader) scheduler.OutputsSchemaResolver {
	return func(featureID string) (any, bool) {
		sch, ok := loader.OutputsSchemaForFeature(featureID)
		if !ok {
			return nil, false
		}
		return sch, true
	}
}
