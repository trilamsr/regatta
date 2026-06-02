package state

import (
	"context"
	"testing"
	"time"
)

func seedEdgeWorkItem(t *testing.T, db *DB, id string) {
	t.Helper()
	item := WorkItem{ID: id, Kind: KindFeature, Title: id, Lane: "server", Status: WorkStatusPlanned}
	seedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := db.UpsertWorkItem(context.Background(), item, SourceBrief, seedAt); err != nil {
		t.Fatalf("seedEdgeWorkItem(%s): %v", id, err)
	}
}

func TestUpsertEdges_InsertThenUpdatePreservesFired(t *testing.T) {
	t0 := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	db := fixedClockDB(t, t0)
	ctx := context.Background()
	seedEdgeWorkItem(t, db, "F-1")
	seedEdgeWorkItem(t, db, "F-2")

	rows := []EdgeRow{{
		ProgramID:    "m-aaaa",
		FromID:       "F-1",
		ToID:         "F-2",
		PredicateCEL: `out.severity == "high"`,
		OnSkip:       "cascade",
	}}
	if err := db.UpsertEdges(ctx, "m-aaaa", rows); err != nil {
		t.Fatalf("UpsertEdges: %v", err)
	}

	got, err := db.ListEdgesFrom(ctx, "F-1")
	if err != nil {
		t.Fatalf("ListEdgesFrom: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got)=%d want 1", len(got))
	}
	if got[0].PredicateCEL != `out.severity == "high"` {
		t.Fatalf("PredicateCEL=%q", got[0].PredicateCEL)
	}
	if got[0].Fired != "pending" {
		t.Fatalf("Fired=%q want pending", got[0].Fired)
	}
	if !got[0].CreatedAt.Equal(t0) {
		t.Fatalf("CreatedAt=%v want %v", got[0].CreatedAt, t0)
	}

	// Simulate evaluation between upserts: re-plan must preserve fired.
	if err := db.MarkEdgeFired(ctx, got[0].ID, "true", "sha-aa"); err != nil {
		t.Fatalf("MarkEdgeFired: %v", err)
	}

	rows[0].PredicateCEL = `out.severity == "critical"`
	if err := db.UpsertEdges(ctx, "m-aaaa", rows); err != nil {
		t.Fatalf("UpsertEdges #2: %v", err)
	}
	got, _ = db.ListEdgesFrom(ctx, "F-1")
	if len(got) != 1 {
		t.Fatalf("after re-upsert len=%d want 1 (must remain idempotent)", len(got))
	}
	if got[0].PredicateCEL != `out.severity == "critical"` {
		t.Fatalf("predicate not refreshed: %q", got[0].PredicateCEL)
	}
	if got[0].Fired != "true" || got[0].FiredAgainst != "sha-aa" {
		t.Fatalf("re-upsert silently flipped fired: %+v", got[0])
	}
}

func TestUpsertEdges_Empty(t *testing.T) {
	db := newTestDB(t)
	if err := db.UpsertEdges(context.Background(), "m-x", nil); err != nil {
		t.Fatalf("UpsertEdges(nil): %v", err)
	}
}

func TestUpsertEdges_IsDefaultRoundTrip(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	seedEdgeWorkItem(t, db, "F-A")
	seedEdgeWorkItem(t, db, "F-B")
	if err := db.UpsertEdges(ctx, "m-1", []EdgeRow{{
		ProgramID: "m-1", FromID: "F-A", ToID: "F-B",
		IsDefault: true, OnSkip: "ignore",
	}}); err != nil {
		t.Fatalf("UpsertEdges: %v", err)
	}
	got, _ := db.ListEdgesFrom(ctx, "F-A")
	if len(got) != 1 || !got[0].IsDefault || got[0].OnSkip != "ignore" {
		t.Fatalf("got %+v", got)
	}
}

func TestMarkEdgeFired_PendingToTrue(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	seedEdgeWorkItem(t, db, "F-1")
	seedEdgeWorkItem(t, db, "F-2")
	if err := db.UpsertEdges(ctx, "m-aaaa", []EdgeRow{{
		ProgramID: "m-aaaa", FromID: "F-1", ToID: "F-2",
		PredicateCEL: "true", OnSkip: "cascade",
	}}); err != nil {
		t.Fatal(err)
	}
	edges, _ := db.ListEdgesFrom(ctx, "F-1")

	if err := db.MarkEdgeFired(ctx, edges[0].ID, "true", "sha-deadbeef"); err != nil {
		t.Fatalf("MarkEdgeFired: %v", err)
	}
	edges, _ = db.ListEdgesFrom(ctx, "F-1")
	if edges[0].Fired != "true" {
		t.Fatalf("Fired=%q want true", edges[0].Fired)
	}
	if edges[0].FiredAgainst != "sha-deadbeef" {
		t.Fatalf("FiredAgainst=%q want sha-deadbeef", edges[0].FiredAgainst)
	}
	if edges[0].EvaluatedAt.IsZero() {
		t.Fatal("EvaluatedAt zero after MarkEdgeFired")
	}
}

func TestMarkEdgeFired_NotFound(t *testing.T) {
	db := newTestDB(t)
	err := db.MarkEdgeFired(context.Background(), 9999, "true", "sha")
	if err == nil {
		t.Fatal("MarkEdgeFired(non-existent) returned nil; want error")
	}
}

func TestListEdgesFrom_OrdersByID(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	seedEdgeWorkItem(t, db, "F-A")
	seedEdgeWorkItem(t, db, "F-B")
	seedEdgeWorkItem(t, db, "F-C")
	if err := db.UpsertEdges(ctx, "m-1", []EdgeRow{
		{ProgramID: "m-1", FromID: "F-A", ToID: "F-C", OnSkip: "cascade"},
		{ProgramID: "m-1", FromID: "F-A", ToID: "F-B", OnSkip: "cascade"},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := db.ListEdgesFrom(ctx, "F-A")
	if err != nil {
		t.Fatalf("ListEdgesFrom: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len=%d want 2", len(got))
	}
	if got[0].ID >= got[1].ID {
		t.Fatalf("not ordered by id ASC: %d, %d", got[0].ID, got[1].ID)
	}
	if got[0].ToID != "F-C" || got[1].ToID != "F-B" {
		t.Fatalf("insertion order broken: %q, %q", got[0].ToID, got[1].ToID)
	}
}

func TestListEdgesTo_FiltersToTarget(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	seedEdgeWorkItem(t, db, "F-A")
	seedEdgeWorkItem(t, db, "F-B")
	seedEdgeWorkItem(t, db, "F-C")
	if err := db.UpsertEdges(ctx, "m-1", []EdgeRow{
		{ProgramID: "m-1", FromID: "F-A", ToID: "F-C", OnSkip: "cascade"},
		{ProgramID: "m-1", FromID: "F-B", ToID: "F-C", OnSkip: "ignore"},
		{ProgramID: "m-1", FromID: "F-A", ToID: "F-B", OnSkip: "cascade"},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := db.ListEdgesTo(ctx, "F-C")
	if err != nil {
		t.Fatalf("ListEdgesTo: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len=%d want 2", len(got))
	}
	for _, e := range got {
		if e.ToID != "F-C" {
			t.Fatalf("leaked edge ToID=%q", e.ToID)
		}
	}
}

func TestListPendingEdgesFromMerged_OnlyMergedSources(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	// F-MERGED has fired-pending edge; F-RUNNING also has one but
	// must be excluded by the merged-source filter.
	merged := WorkItem{ID: "F-MERGED", Kind: KindFeature, Title: "m", Lane: "server", Status: WorkStatusMerged}
	running := WorkItem{ID: "F-RUNNING", Kind: KindFeature, Title: "r", Lane: "server", Status: WorkStatusRunning}
	tgt := WorkItem{ID: "F-TGT", Kind: KindFeature, Title: "t", Lane: "server", Status: WorkStatusPlanned}
	stamp := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, w := range []WorkItem{merged, running, tgt} {
		if err := db.UpsertWorkItem(ctx, w, SourceBrief, stamp); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.UpsertEdges(ctx, "m-1", []EdgeRow{
		{ProgramID: "m-1", FromID: "F-MERGED", ToID: "F-TGT", OnSkip: "cascade"},
		{ProgramID: "m-1", FromID: "F-RUNNING", ToID: "F-TGT", OnSkip: "cascade"},
	}); err != nil {
		t.Fatal(err)
	}

	got, err := db.ListPendingEdgesFromMerged(ctx)
	if err != nil {
		t.Fatalf("ListPendingEdgesFromMerged: %v", err)
	}
	if len(got) != 1 || got[0].FromID != "F-MERGED" {
		t.Fatalf("got %+v want exactly [F-MERGED]", got)
	}

	// After firing, it must no longer appear in the pending slice.
	if err := db.MarkEdgeFired(ctx, got[0].ID, "true", "sha"); err != nil {
		t.Fatal(err)
	}
	got, _ = db.ListPendingEdgesFromMerged(ctx)
	if len(got) != 0 {
		t.Fatalf("after fire len=%d want 0", len(got))
	}
}

// TestCountNonDefaultEdgeStates covers the aggregate that replaces the post-loop ListEdgesFrom re-read in evalPendingEdges
func TestCountNonDefaultEdgeStates(t *testing.T) {
	cases := []struct {
		name    string
		seed    []EdgeRow          // edges to upsert
		fireMap map[string]string  // ToID -> fired value to MarkEdgeFired after upsert
		want    EdgeFromAggregate
	}{
		{
			name: "all-non-default-false-with-pending-default",
			seed: []EdgeRow{
				{ProgramID: "m-1", FromID: "F-A", ToID: "F-B", OnSkip: "cascade"},
				{ProgramID: "m-1", FromID: "F-A", ToID: "F-C", OnSkip: "cascade"},
				{ProgramID: "m-1", FromID: "F-A", ToID: "F-D", IsDefault: true, OnSkip: "cascade"},
			},
			fireMap: map[string]string{"F-B": "false", "F-C": "false"},
			want: EdgeFromAggregate{
				NonDefaultCount:      2,
				AnyNonDefaultTrue:    false,
				AnyNonDefaultPending: false,
				DefaultCount:         1,
				DefaultFired:         "pending",
				DefaultProgramID:     "m-1",
			},
		},
		{
			name: "any-non-default-true-blocks-fallback",
			seed: []EdgeRow{
				{ProgramID: "m-1", FromID: "F-A", ToID: "F-B", OnSkip: "cascade"},
				{ProgramID: "m-1", FromID: "F-A", ToID: "F-C", OnSkip: "cascade"},
				{ProgramID: "m-1", FromID: "F-A", ToID: "F-D", IsDefault: true, OnSkip: "cascade"},
			},
			fireMap: map[string]string{"F-B": "true", "F-C": "false"},
			want: EdgeFromAggregate{
				NonDefaultCount:      2,
				AnyNonDefaultTrue:    true,
				AnyNonDefaultPending: false,
				DefaultCount:         1,
				DefaultFired:         "pending",
				DefaultProgramID:     "m-1",
			},
		},
		{
			name: "any-non-default-pending-blocks-fallback",
			seed: []EdgeRow{
				{ProgramID: "m-1", FromID: "F-A", ToID: "F-B", OnSkip: "cascade"},
				{ProgramID: "m-1", FromID: "F-A", ToID: "F-C", OnSkip: "cascade"},
				{ProgramID: "m-1", FromID: "F-A", ToID: "F-D", IsDefault: true, OnSkip: "cascade"},
			},
			fireMap: map[string]string{"F-B": "false"},
			want: EdgeFromAggregate{
				NonDefaultCount:      2,
				AnyNonDefaultTrue:    false,
				AnyNonDefaultPending: true,
				DefaultCount:         1,
				DefaultFired:         "pending",
				DefaultProgramID:     "m-1",
			},
		},
		{
			name: "no-default-row",
			seed: []EdgeRow{
				{ProgramID: "m-1", FromID: "F-A", ToID: "F-B", OnSkip: "cascade"},
			},
			fireMap: map[string]string{"F-B": "false"},
			want: EdgeFromAggregate{
				NonDefaultCount:      1,
				AnyNonDefaultTrue:    false,
				AnyNonDefaultPending: false,
				DefaultCount:         0,
				DefaultFired:         "",
				DefaultProgramID:     "m-1",
			},
		},
		{
			name: "multi-default-misconfig",
			seed: []EdgeRow{
				{ProgramID: "m-1", FromID: "F-A", ToID: "F-B", IsDefault: true, OnSkip: "cascade"},
				{ProgramID: "m-1", FromID: "F-A", ToID: "F-C", IsDefault: true, OnSkip: "cascade"},
				{ProgramID: "m-1", FromID: "F-A", ToID: "F-D", OnSkip: "cascade"},
			},
			fireMap: map[string]string{"F-D": "false"},
			want: EdgeFromAggregate{
				NonDefaultCount:      1,
				AnyNonDefaultTrue:    false,
				AnyNonDefaultPending: false,
				DefaultCount:         2,
				DefaultFired:         "pending",
				DefaultProgramID:     "m-1",
			},
		},
		{
			name: "empty-from-id",
			seed: nil,
			want: EdgeFromAggregate{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := newTestDB(t)
			ctx := context.Background()
			seedEdgeWorkItem(t, db, "F-A")
			for _, suf := range []string{"F-B", "F-C", "F-D"} {
				seedEdgeWorkItem(t, db, suf)
			}
			if len(tc.seed) > 0 {
				if err := db.UpsertEdges(ctx, "m-1", tc.seed); err != nil {
					t.Fatalf("UpsertEdges: %v", err)
				}
			}
			rows, _ := db.ListEdgesFrom(ctx, "F-A")
			for _, r := range rows {
				fired, ok := tc.fireMap[r.ToID]
				if !ok {
					continue
				}
				if err := db.MarkEdgeFired(ctx, r.ID, fired, "sha"); err != nil {
					t.Fatalf("MarkEdgeFired %s: %v", r.ToID, err)
				}
			}

			got, err := db.CountNonDefaultEdgeStates(ctx, "F-A")
			if err != nil {
				t.Fatalf("CountNonDefaultEdgeStates: %v", err)
			}
			// DefaultEdgeID is set by the SQL; we only need to check
			// it identifies the lexically-first default row when one
			// exists. Compare against tc.want field-by-field.
			if got.NonDefaultCount != tc.want.NonDefaultCount {
				t.Errorf("NonDefaultCount=%d want %d", got.NonDefaultCount, tc.want.NonDefaultCount)
			}
			if got.AnyNonDefaultTrue != tc.want.AnyNonDefaultTrue {
				t.Errorf("AnyNonDefaultTrue=%v want %v", got.AnyNonDefaultTrue, tc.want.AnyNonDefaultTrue)
			}
			if got.AnyNonDefaultPending != tc.want.AnyNonDefaultPending {
				t.Errorf("AnyNonDefaultPending=%v want %v", got.AnyNonDefaultPending, tc.want.AnyNonDefaultPending)
			}
			if got.DefaultCount != tc.want.DefaultCount {
				t.Errorf("DefaultCount=%d want %d", got.DefaultCount, tc.want.DefaultCount)
			}
			if got.DefaultFired != tc.want.DefaultFired {
				t.Errorf("DefaultFired=%q want %q", got.DefaultFired, tc.want.DefaultFired)
			}
			if got.DefaultProgramID != tc.want.DefaultProgramID {
				t.Errorf("DefaultProgramID=%q want %q", got.DefaultProgramID, tc.want.DefaultProgramID)
			}
			if tc.want.DefaultCount > 0 && got.DefaultEdgeID == 0 {
				t.Errorf("DefaultEdgeID=0 with DefaultCount=%d (want a real id)", got.DefaultCount)
			}
			if tc.want.DefaultCount == 0 && got.DefaultEdgeID != 0 {
				t.Errorf("DefaultEdgeID=%d with no default rows", got.DefaultEdgeID)
			}
		})
	}
}

// TestCountNonDefaultEdgeStates_MatchesListEdgesFrom is a property-style equivalence test: for every from_id seeded into a
func TestCountNonDefaultEdgeStates_MatchesListEdgesFrom(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	// Seed 8 from_ids with varying sibling shapes.
	shapes := []struct {
		from         string
		nonDefaults  int
		defaults     int
		fireFirstAs  string // "" leaves pending
	}{
		{"F-1", 0, 0, ""},
		{"F-2", 1, 0, "false"},
		{"F-3", 2, 1, "false"},
		{"F-4", 3, 1, "true"},
		{"F-5", 2, 1, ""},
		{"F-6", 0, 1, ""},
		{"F-7", 4, 2, "false"},
		{"F-8", 1, 1, "true"},
	}
	var allEdges []EdgeRow
	for _, s := range shapes {
		seedEdgeWorkItem(t, db, s.from)
		for i := 0; i < s.nonDefaults; i++ {
			to := s.from + "-T" + string(rune('A'+i))
			seedEdgeWorkItem(t, db, to)
			allEdges = append(allEdges, EdgeRow{ProgramID: "m-1", FromID: s.from, ToID: to})
		}
		for i := 0; i < s.defaults; i++ {
			to := s.from + "-D" + string(rune('A'+i))
			seedEdgeWorkItem(t, db, to)
			allEdges = append(allEdges, EdgeRow{ProgramID: "m-1", FromID: s.from, ToID: to, IsDefault: true})
		}
	}
	if len(allEdges) > 0 {
		if err := db.UpsertEdges(ctx, "m-1", allEdges); err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range shapes {
		if s.fireFirstAs == "" {
			continue
		}
		rows, _ := db.ListEdgesFrom(ctx, s.from)
		for _, r := range rows {
			if !r.IsDefault {
				if err := db.MarkEdgeFired(ctx, r.ID, s.fireFirstAs, "sha"); err != nil {
					t.Fatal(err)
				}
				break
			}
		}
	}
	for _, s := range shapes {
		rows, _ := db.ListEdgesFrom(ctx, s.from)
		var wantNonDef, wantDef int
		var wantAnyTrue, wantAnyPending bool
		var wantDefFired, wantDefProgID string
		var wantDefID int64
		for _, r := range rows {
			if r.IsDefault {
				wantDef++
				if wantDefID == 0 || r.ID < wantDefID {
					wantDefID = r.ID
					wantDefFired = r.Fired
				}
			} else {
				wantNonDef++
				if r.Fired == "true" {
					wantAnyTrue = true
				}
				if r.Fired == "pending" {
					wantAnyPending = true
				}
			}
			if wantDefProgID == "" {
				wantDefProgID = r.ProgramID
			}
		}
		got, err := db.CountNonDefaultEdgeStates(ctx, s.from)
		if err != nil {
			t.Fatalf("from=%s: %v", s.from, err)
		}
		if got.NonDefaultCount != wantNonDef ||
			got.AnyNonDefaultTrue != wantAnyTrue ||
			got.AnyNonDefaultPending != wantAnyPending ||
			got.DefaultCount != wantDef ||
			got.DefaultFired != wantDefFired ||
			got.DefaultEdgeID != wantDefID {
			t.Errorf("from=%s: got=%+v want NonDef=%d AnyTrue=%v AnyPending=%v Def=%d DefFired=%q DefID=%d",
				s.from, got, wantNonDef, wantAnyTrue, wantAnyPending, wantDef, wantDefFired, wantDefID)
		}
	}
}

func TestUpsertEdges_UpdatedAtUsesClock(t *testing.T) {
	t0 := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	t1 := t0.Add(2 * time.Minute)
	now := t0
	db := newClockedTestDB(t, &now)
	ctx := context.Background()
	seedEdgeWorkItem(t, db, "F-1")
	seedEdgeWorkItem(t, db, "F-2")

	if err := db.UpsertEdges(ctx, "m-1", []EdgeRow{{
		ProgramID: "m-1", FromID: "F-1", ToID: "F-2", OnSkip: "cascade",
	}}); err != nil {
		t.Fatal(err)
	}
	got, _ := db.ListEdgesFrom(ctx, "F-1")
	if !got[0].CreatedAt.Equal(t0) || !got[0].UpdatedAt.Equal(t0) {
		t.Fatalf("created=%v updated=%v want both %v", got[0].CreatedAt, got[0].UpdatedAt, t0)
	}

	now = t1
	if err := db.UpsertEdges(ctx, "m-1", []EdgeRow{{
		ProgramID: "m-1", FromID: "F-1", ToID: "F-2", PredicateCEL: "x", OnSkip: "ignore",
	}}); err != nil {
		t.Fatal(err)
	}
	got, _ = db.ListEdgesFrom(ctx, "F-1")
	if !got[0].CreatedAt.Equal(t0) {
		t.Fatalf("CreatedAt mutated on update: %v", got[0].CreatedAt)
	}
	if !got[0].UpdatedAt.Equal(t1) {
		t.Fatalf("UpdatedAt=%v want %v", got[0].UpdatedAt, t1)
	}
}
