package program

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	_ "modernc.org/sqlite"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/obstest"
	"github.com/trilamsr/regatta/internal/orchestrator"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

func newBriefTestDB(t *testing.T) *state.DB {
	t.Helper()
	db, err := state.Open(context.Background(), state.DSN(filepath.Join(t.TempDir(), "bl.db")))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// mustNewLoader wraps NewBriefLoader so tests can keep their fluent
// inline construction after the constructor switched to
// (*BriefLoader, error) for required-field validation.
func mustNewLoader(t *testing.T, cfg BriefLoaderConfig) *BriefLoader {
	t.Helper()
	l, err := NewBriefLoader(cfg)
	if err != nil {
		t.Fatalf("NewBriefLoader: %v", err)
	}
	return l
}

// captureLogs swaps slog's default to a text handler writing into a
// buffer for the duration of the test so log-asserting tests can grep
// without globally leaking handler state.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

func mustSignedBrief(t *testing.T, key []byte) (*ProgramBrief, []byte) {
	t.Helper()
	return mustSignedBriefWithOpts(t, key, "PROG-1", "m-1234567890ab",
		time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC),
		[]PlannedFeature{
			{ID: "F-1", Title: "foo", Fulfills: []string{"c1"}},
			{ID: "F-2", Title: "bar", Fulfills: []string{"c2"}},
			{ID: "F-3", Title: "baz", Fulfills: []string{"c3"}},
		},
		[]PlanCriterion{
			{ID: "c1", Text: "add foo"},
			{ID: "c2", Text: "add bar"},
			{ID: "c3", Text: "add baz"},
		})
}

func mustSignedBriefWithOpts(t *testing.T, key []byte, parentID, programID string, producedAt time.Time, features []PlannedFeature, criteria []PlanCriterion) (*ProgramBrief, []byte) {
	t.Helper()
	b := &ProgramBrief{
		SchemaVersion:    1,
		ProgramID:        programID,
		ParentWorkItemID: parentID,
		ParentCriteria:   criteria,
		PlannerModelID:   "claude-test",
		Features:         features,
		ProducedAt:       producedAt,
	}
	signed, err := b.Sign(key, "key-1")
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	raw, err := json.Marshal(signed)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	return signed, raw
}

// seedParent inserts a parent program row so the BriefLoader's
// parent-FK preflight finds it.
func seedParent(t *testing.T, db *state.DB, id string, at time.Time) {
	t.Helper()
	parent := state.WorkItem{ID: id, Kind: state.KindProgram, Title: "p",
		Lane: "server", Status: state.WorkStatusPlanned}
	if err := db.UpsertWorkItem(context.Background(), parent, state.SourceAdapter, at); err != nil {
		t.Fatal(err)
	}
}

func TestLoadAndVerifyBrief_Valid(t *testing.T) {
	key := []byte("test-key-32-bytes-aaaaaaaaaaaaaaa")
	_, raw := mustSignedBrief(t, key)
	fsys := fstest.MapFS{"PROG-1.json": &fstest.MapFile{Data: raw}}

	got, err := LoadAndVerifyBrief(fsys, "PROG-1.json", map[string][]byte{"key-1": key})
	if err != nil {
		t.Fatalf("LoadAndVerifyBrief: %v", err)
	}
	if got.ParentWorkItemID != "PROG-1" {
		t.Fatalf("ParentWorkItemID=%q want PROG-1", got.ParentWorkItemID)
	}
}

func TestLoadAndVerifyBrief_TamperedHMAC(t *testing.T) {
	key := []byte("test-key-32-bytes-aaaaaaaaaaaaaaa")
	_, raw := mustSignedBrief(t, key)
	tampered := append([]byte{}, raw...)
	for i, b := range tampered {
		if b == 'f' { // first 'f' in "foo" feature title — guaranteed present.
			tampered[i] = 'x'
			break
		}
	}
	fsys := fstest.MapFS{"PROG-1.json": &fstest.MapFile{Data: tampered}}

	_, err := LoadAndVerifyBrief(fsys, "PROG-1.json", map[string][]byte{"key-1": key})
	if !errors.Is(err, orchestrator.ErrHMACInvalid) {
		t.Fatalf("err=%v want ErrHMACInvalid", err)
	}
}

func TestLoadAndVerifyBrief_KIDMismatchFails(t *testing.T) {
	key := []byte("test-key-32-bytes-aaaaaaaaaaaaaaa")
	signed, _ := mustSignedBrief(t, key)
	signed.Signature.KeyID = "key-2"
	raw, err := json.Marshal(signed)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	fsys := fstest.MapFS{"PROG-1.json": &fstest.MapFile{Data: raw}}

	_, err = LoadAndVerifyBrief(fsys, "PROG-1.json", map[string][]byte{"key-1": key, "key-2": key})
	if !errors.Is(err, orchestrator.ErrHMACInvalid) {
		t.Fatalf("err=%v want ErrHMACInvalid (kid bound into MAC; relabel must fail)", err)
	}
}

// mustSignedBriefV2 returns a v2 brief whose features carry edges +
// outputs_schema, signed with key under kid "key-1".
func mustSignedBriefV2(t *testing.T, key []byte) ([]byte, string) {
	t.Helper()
	parent := "PROG-V2"
	v2 := &ProgramBriefV2{
		ProgramBrief: ProgramBrief{
			SchemaVersion:    2,
			ProgramID:        "m-cccccccccccc",
			ParentWorkItemID: parent,
			ParentCriteria:   []PlanCriterion{{ID: "c1", Text: "ship"}},
			PlannerModelID:   "test:model",
			ProducedAt:       time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC),
		},
		FeaturesV2: []PlannedFeatureV2{
			{
				PlannedFeature: PlannedFeature{ID: "F-A", Title: "scan", Fulfills: []string{"c1"}},
				OutputsSchema: &OutputsSchema{
					Type:       "object",
					Properties: map[string]*OutputsSchema{"severity": {Type: "string"}},
				},
				Edges:       []Edge{{From: "F-A", To: "F-B", Predicate: `out.severity == "high"`, OnSkip: SkipCascade}},
				DefaultNext: strPtr("F-B"),
			},
			{PlannedFeature: PlannedFeature{ID: "F-B", Title: "remediate"}},
		},
	}
	// Sign via the v1 Sign helper on the embedded ProgramBrief so HMAC
	// covers the canonical-JSON form. We marshal the full v2 brief
	// (which includes the v2 features array) and sign that canonical
	// payload through schemas.Sign directly.
	raw, err := json.Marshal(v2)
	if err != nil {
		t.Fatalf("marshal v2: %v", err)
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatalf("unmarshal generic: %v", err)
	}
	sig, err := schemas.Sign(generic, key, "key-1")
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	v2.Signature = sig
	signed, err := json.Marshal(v2)
	if err != nil {
		t.Fatalf("marshal signed v2: %v", err)
	}
	return signed, parent
}

func TestLoadAndVerifyBrief_V2_RoutesThroughValidateV2(t *testing.T) {
	key := []byte("test-key-32-bytes-aaaaaaaaaaaaaaa")
	raw, _ := mustSignedBriefV2(t, key)
	fsys := fstest.MapFS{"PROG-V2.json": &fstest.MapFile{Data: raw}}

	got, err := LoadAndVerifyBriefV2(fsys, "PROG-V2.json", map[string][]byte{"key-1": key})
	if err != nil {
		t.Fatalf("LoadAndVerifyBriefV2: %v", err)
	}
	if got.SchemaVersion != 2 {
		t.Fatalf("SchemaVersion=%d want 2", got.SchemaVersion)
	}
	if len(got.FeaturesV2) != 2 {
		t.Fatalf("FeaturesV2 len=%d want 2", len(got.FeaturesV2))
	}
	if got.FeaturesV2[0].Edges[0].Predicate == "" {
		t.Fatalf("predicated edge lost in roundtrip")
	}
}

func TestLoadAndVerifyBrief_V2_RejectsBadPredicate(t *testing.T) {
	key := []byte("test-key-32-bytes-aaaaaaaaaaaaaaa")
	// Build a v2 brief whose predicate references an undeclared field.
	v2 := &ProgramBriefV2{
		ProgramBrief: ProgramBrief{
			SchemaVersion:    2,
			ProgramID:        "m-dddddddddddd",
			ParentWorkItemID: "PROG-V2-BAD",
			ParentCriteria:   []PlanCriterion{{ID: "c1", Text: "ship"}},
			PlannerModelID:   "test:model",
		},
		FeaturesV2: []PlannedFeatureV2{
			{
				PlannedFeature: PlannedFeature{ID: "F-A", Title: "scan", Fulfills: []string{"c1"}},
				OutputsSchema: &OutputsSchema{
					Type:       "object",
					Properties: map[string]*OutputsSchema{"severity": {Type: "string"}},
				},
				Edges:       []Edge{{From: "F-A", To: "F-B", Predicate: `out.missing == "x"`, OnSkip: SkipCascade}},
				DefaultNext: strPtr("F-B"),
			},
			{PlannedFeature: PlannedFeature{ID: "F-B", Title: "remediate"}},
		},
	}
	raw, err := json.Marshal(v2)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	sig, err := schemas.Sign(generic, key, "key-1")
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	v2.Signature = sig
	signed, _ := json.Marshal(v2)
	fsys := fstest.MapFS{"PROG-V2-BAD.json": &fstest.MapFile{Data: signed}}

	_, err = LoadAndVerifyBriefV2(fsys, "PROG-V2-BAD.json", map[string][]byte{"key-1": key})
	if !errors.Is(err, orchestrator.ErrPredicateUnknownField) {
		t.Fatalf("err=%v want ErrPredicateUnknownField", err)
	}
}

func TestLoadAndVerifyBrief_OversizedRejected(t *testing.T) {
	key := []byte("test-key-32-bytes-aaaaaaaaaaaaaaa")
	pad := strings.Repeat("A", maxBriefSize+1)
	_, raw := mustSignedBriefWithOpts(t, key, "PROG-1", "m-1234567890ab",
		time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC),
		[]PlannedFeature{
			{ID: "F-1", Title: pad, Fulfills: []string{"c1"}},
		},
		[]PlanCriterion{{ID: "c1", Text: "add"}})
	fsys := fstest.MapFS{"PROG-1.json": &fstest.MapFile{Data: raw}}

	_, err := LoadAndVerifyBrief(fsys, "PROG-1.json", map[string][]byte{"key-1": key})
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("err=%v want size-cap rejection", err)
	}
}

func TestBriefLoaderSync_UpsertsThreeChildren(t *testing.T) {
	db := newBriefTestDB(t)
	key := []byte("test-key-32-bytes-aaaaaaaaaaaaaaa")
	_, raw := mustSignedBrief(t, key)
	fsys := fstest.MapFS{"PROG-1.json": &fstest.MapFile{Data: raw}}

	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	seedParent(t, db, "PROG-1", now)

	loader := mustNewLoader(t, BriefLoaderConfig{FS: fsys, DB: db, Keyring: map[string][]byte{"key-1": key}})

	if err := loader.Sync(context.Background(), now); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	children, err := db.ListByParent(context.Background(), "PROG-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 3 {
		t.Fatalf("children=%d want 3", len(children))
	}
	for _, c := range children {
		if c.Source != state.SourceBrief {
			t.Fatalf("child %s source=%s want brief", c.ID, c.Source)
		}
	}
}

func TestBriefLoaderSync_SkipsTmpFiles(t *testing.T) {
	db := newBriefTestDB(t)
	key := []byte("test-key-32-bytes-aaaaaaaaaaaaaaa")
	_, raw := mustSignedBrief(t, key)
	fsys := fstest.MapFS{"PROG-1.json.tmp": &fstest.MapFile{Data: raw}}

	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	loader := mustNewLoader(t, BriefLoaderConfig{FS: fsys, DB: db, Keyring: map[string][]byte{"key-1": key}})
	if err := loader.Sync(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	children, _ := db.ListByParent(context.Background(), "PROG-1")
	if len(children) != 0 {
		t.Fatalf("children=%d want 0 (.tmp must be skipped)", len(children))
	}
}

func TestBriefLoaderSync_TombstonesMissingBrief(t *testing.T) {
	db := newBriefTestDB(t)
	key := []byte("test-key-32-bytes-aaaaaaaaaaaaaaa")
	_, raw := mustSignedBrief(t, key)
	files := fstest.MapFS{"PROG-1.json": &fstest.MapFile{Data: raw}}

	t0 := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	seedParent(t, db, "PROG-1", t0)

	loader := mustNewLoader(t, BriefLoaderConfig{FS: files, DB: db, Keyring: map[string][]byte{"key-1": key}})
	if err := loader.Sync(context.Background(), t0); err != nil {
		t.Fatal(err)
	}

	// Next tick — brief gone.
	t1 := t0.Add(1 * time.Second)
	delete(files, "PROG-1.json")
	if err := loader.Sync(context.Background(), t1); err != nil {
		t.Fatal(err)
	}

	children, _ := db.ListByParent(context.Background(), "PROG-1")
	if len(children) != 3 {
		t.Fatalf("children=%d want 3 rows (tombstoned but preserved)", len(children))
	}
	for _, c := range children {
		if c.Status != state.WorkStatusArchived {
			t.Fatalf("child %s status=%s want archived", c.ID, c.Status)
		}
	}
}

func TestBriefLoaderSync_TombstoneLogsCutoff(t *testing.T) {
	db := newBriefTestDB(t)
	key := []byte("test-key-32-bytes-aaaaaaaaaaaaaaa")
	_, raw := mustSignedBrief(t, key)
	files := fstest.MapFS{"PROG-1.json": &fstest.MapFile{Data: raw}}

	t0 := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	seedParent(t, db, "PROG-1", t0)
	loader := mustNewLoader(t, BriefLoaderConfig{FS: files, DB: db, Keyring: map[string][]byte{"key-1": key}})
	if err := loader.Sync(context.Background(), t0); err != nil {
		t.Fatal(err)
	}

	logs := captureLogs(t)
	t1 := t0.Add(time.Second)
	delete(files, "PROG-1.json")
	if err := loader.Sync(context.Background(), t1); err != nil {
		t.Fatal(err)
	}
	out := logs.String()
	if !strings.Contains(out, "brief.tombstoned") {
		t.Fatalf("missing brief.tombstoned in logs: %s", out)
	}
	if !strings.Contains(out, "cutoff=") {
		t.Fatalf("log field should be 'cutoff' (mirrors A5): %s", out)
	}
}

func TestBriefLoaderSync_CrossBriefFeatureIDCollision(t *testing.T) {
	db := newBriefTestDB(t)
	key := []byte("test-key-32-bytes-aaaaaaaaaaaaaaa")
	t0 := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)

	// Two briefs, both define feature F-1 (different parents).
	_, rawA := mustSignedBriefWithOpts(t, key, "PROG-A", "m-aaaaaaaaaaaa", t0,
		[]PlannedFeature{{ID: "F-1", Title: "from-A", Fulfills: []string{"c1"}}},
		[]PlanCriterion{{ID: "c1", Text: "a"}})
	_, rawB := mustSignedBriefWithOpts(t, key, "PROG-B", "m-bbbbbbbbbbbb", t0,
		[]PlannedFeature{{ID: "F-1", Title: "from-B", Fulfills: []string{"c1"}}},
		[]PlanCriterion{{ID: "c1", Text: "b"}})

	seedParent(t, db, "PROG-A", t0)
	seedParent(t, db, "PROG-B", t0)

	// Filenames chosen so sort.Strings places A first.
	fsys := fstest.MapFS{
		"a.json": &fstest.MapFile{Data: rawA},
		"b.json": &fstest.MapFile{Data: rawB},
	}

	logs := captureLogs(t)
	loader := mustNewLoader(t, BriefLoaderConfig{FS: fsys, DB: db, Keyring: map[string][]byte{"key-1": key}})
	if err := loader.Sync(context.Background(), t0); err != nil {
		t.Fatal(err)
	}

	got, err := db.GetWorkItem(context.Background(), "F-1")
	if err != nil {
		t.Fatalf("GetWorkItem F-1: %v", err)
	}
	if got.ParentProgramID != "PROG-A" {
		t.Fatalf("F-1.parent=%q want PROG-A (first-wins)", got.ParentProgramID)
	}
	if got.Title != "from-A" {
		t.Fatalf("F-1.title=%q want from-A (first-wins)", got.Title)
	}
	out := logs.String()
	if !strings.Contains(out, "feature_id_collision") {
		t.Fatalf("missing feature_id_collision warn: %s", out)
	}
	if !strings.Contains(out, "first_seen_in=a.json") {
		t.Fatalf("missing first_seen_in=a.json: %s", out)
	}
}

func TestBriefLoaderSync_RejectsUnknownParent(t *testing.T) {
	db := newBriefTestDB(t)
	key := []byte("test-key-32-bytes-aaaaaaaaaaaaaaa")
	_, raw := mustSignedBrief(t, key)
	fsys := fstest.MapFS{"PROG-1.json": &fstest.MapFile{Data: raw}}

	logs := captureLogs(t)
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	loader := mustNewLoader(t, BriefLoaderConfig{FS: fsys, DB: db, Keyring: map[string][]byte{"key-1": key}})
	if err := loader.Sync(context.Background(), now); err != nil {
		t.Fatal(err)
	}

	children, _ := db.ListByParent(context.Background(), "PROG-1")
	if len(children) != 0 {
		t.Fatalf("children=%d want 0 (unknown parent must reject brief)", len(children))
	}
	out := logs.String()
	if !strings.Contains(out, "unknown_parent_program") {
		t.Fatalf("missing unknown_parent_program warn: %s", out)
	}
}

func TestBriefLoaderSync_StaleBriefRejected(t *testing.T) {
	db := newBriefTestDB(t)
	key := []byte("test-key-32-bytes-aaaaaaaaaaaaaaa")
	t0 := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	seedParent(t, db, "PROG-1", t0)

	_, raw := mustSignedBrief(t, key) // ProducedAt = t0
	files := fstest.MapFS{"PROG-1.json": &fstest.MapFile{Data: raw}}

	loader := mustNewLoader(t, BriefLoaderConfig{FS: files, DB: db, Keyring: map[string][]byte{"key-1": key}})
	if err := loader.Sync(context.Background(), t0); err != nil {
		t.Fatal(err)
	}

	// Replay same brief 5 min later — same ProducedAt → must be rejected.
	logs := captureLogs(t)
	t1 := t0.Add(5 * time.Minute)
	if err := loader.Sync(context.Background(), t1); err != nil {
		t.Fatal(err)
	}
	out := logs.String()
	// Either replay-defence layer is acceptable: the HMAC layer (#92)
	// catches exact-replay before the produced_at watermark fires.
	if !strings.Contains(out, "stale_produced_at") && !strings.Contains(out, "brief_already_processed") {
		t.Fatalf("expected replay rejection: %s", out)
	}
	// Children should NOT have been re-stamped on the replay — so the
	// later sweep (no actual upsert during the replay) must not tombstone them.
	children, _ := db.ListByParent(context.Background(), "PROG-1")
	if len(children) != 3 {
		t.Fatalf("children=%d want 3 (no churn from stale replay)", len(children))
	}
}

// Issue #92: cross-restart-persistent brief replay protection.
func TestBriefLoaderSync_ReplayAcrossWorkItemsWipeRejected(t *testing.T) {
	db := newBriefTestDB(t)
	key := []byte("test-key-32-bytes-aaaaaaaaaaaaaaa")
	t0 := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	seedParent(t, db, "PROG-1", t0)

	_, raw := mustSignedBrief(t, key) // ProducedAt = t0
	files := fstest.MapFS{"PROG-1.json": &fstest.MapFile{Data: raw}}

	loader := mustNewLoader(t, BriefLoaderConfig{FS: files, DB: db, Keyring: map[string][]byte{"key-1": key}})
	if err := loader.Sync(context.Background(), t0); err != nil {
		t.Fatalf("seed Sync: %v", err)
	}

	// Attack: operator deletes brief-source children (or wipes them via
	// a malformed cleanup script). Pre-fix, this drops the
	// MaxUpdatedAtForBriefChildren watermark to zero so the same brief
	// can be re-ingested on the next poll.
	if _, err := db.SQL().ExecContext(context.Background(),
		`DELETE FROM work_items WHERE source = ? AND parent_program_id = ?`,
		string(state.SourceBrief), "PROG-1"); err != nil {
		t.Fatalf("delete brief children: %v", err)
	}

	logs := captureLogs(t)
	// Simulate next-tick Sync after the wipe. Same brief on disk.
	t1 := t0.Add(5 * time.Minute)
	if err := loader.Sync(context.Background(), t1); err != nil {
		t.Fatalf("replay Sync: %v", err)
	}

	out := logs.String()
	if !strings.Contains(out, "stale_produced_at") && !strings.Contains(out, "brief_already_processed") {
		t.Fatalf("expected replay rejection after work_items wipe; logs: %s", out)
	}
	children, _ := db.ListByParent(context.Background(), "PROG-1")
	if len(children) != 0 {
		t.Fatalf("children=%d want 0 (replay must NOT re-inject after wipe)", len(children))
	}
}

// ─────────────────────────────────────────────────────────────────
// Cascade-dep tests
// ─────────────────────────────────────────────────────────────────

// seedFeature inserts a feature child via UpsertWorkItem with the
// given status, depends_on, and at-timestamp. Caller seeds the parent
// first.
func seedFeature(t *testing.T, db *state.DB, id, parent string, deps []string, status state.WorkItemStatus, at time.Time) {
	t.Helper()
	wi := state.WorkItem{
		ID:                id,
		Kind:              state.KindFeature,
		Title:             id,
		Lane:              "server",
		Status:            status,
		ParentProgramID:   parent,
		DependsOnFeatures: deps,
	}
	if err := db.UpsertWorkItem(context.Background(), wi, state.SourceBrief, at); err != nil {
		t.Fatal(err)
	}
}

// freshBriefLoader returns a loader with no fsys content (Sync just
// runs the reconciler). The reconciler is independent of any brief.
func freshBriefLoader(t *testing.T, db *state.DB) *BriefLoader {
	t.Helper()
	return mustNewLoader(t, BriefLoaderConfig{FS: fstest.MapFS{}, DB: db, Keyring: map[string][]byte{"key-1": []byte("k")}})
}

// Note for cascade-dep tests: seed timestamps must equal pollStartedAt
// so the tombstone sweep (last_seen_at < pollStartedAt) is a no-op and
// only the dependency-cascade reconciler effects show through. Sync
// runs both passes per tick; isolating the reconciler in a unit means
// keeping the sweep at no-op.
func TestCascadeDep_ArchivedDepFlagsChild(t *testing.T) {
	db := newBriefTestDB(t)
	t0 := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	seedParent(t, db, "PROG-1", t0)
	seedFeature(t, db, "F-DEP", "PROG-1", nil, state.WorkStatusArchived, t0)
	seedFeature(t, db, "F-CHILD", "PROG-1", []string{"F-DEP"}, state.WorkStatusPlanned, t0)

	loader := freshBriefLoader(t, db)
	if err := loader.Sync(context.Background(), t0); err != nil {
		t.Fatal(err)
	}

	got, err := db.GetWorkItem(context.Background(), "F-CHILD")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != state.WorkStatusArchived {
		t.Fatalf("F-CHILD.status=%s want archived", got.Status)
	}
}

func TestCascadeDep_LiveDepLeavesChildAlone(t *testing.T) {
	db := newBriefTestDB(t)
	t0 := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	seedParent(t, db, "PROG-1", t0)
	seedFeature(t, db, "F-DEP", "PROG-1", nil, state.WorkStatusPlanned, t0)
	seedFeature(t, db, "F-CHILD", "PROG-1", []string{"F-DEP"}, state.WorkStatusPlanned, t0)

	loader := freshBriefLoader(t, db)
	if err := loader.Sync(context.Background(), t0); err != nil {
		t.Fatal(err)
	}

	got, _ := db.GetWorkItem(context.Background(), "F-CHILD")
	if got.Status != state.WorkStatusPlanned {
		t.Fatalf("F-CHILD.status=%s want planned (dep alive)", got.Status)
	}
}

func TestCascadeDep_DepNotFound(t *testing.T) {
	db := newBriefTestDB(t)
	t0 := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	seedParent(t, db, "PROG-1", t0)
	seedFeature(t, db, "F-CHILD", "PROG-1", []string{"F-PHANTOM"}, state.WorkStatusPlanned, t0)

	loader := freshBriefLoader(t, db)
	if err := loader.Sync(context.Background(), t0); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	got, _ := db.GetWorkItem(context.Background(), "F-CHILD")
	if got.Status != state.WorkStatusPlanned {
		t.Fatalf("F-CHILD.status=%s want planned (phantom dep is no-op)", got.Status)
	}
}

// TestCascadeDep_MultiHopWithinOneSync asserts fixed-point cascade converges C→B→A archive chain in one Sync despite reverse insertion order.
func TestCascadeDep_MultiHopWithinOneSync(t *testing.T) {
	db := newBriefTestDB(t)
	t0 := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	seedParent(t, db, "PROG-1", t0)
	seedFeature(t, db, "F-C", "PROG-1", []string{"F-B"}, state.WorkStatusPlanned, t0)
	seedFeature(t, db, "F-B", "PROG-1", []string{"F-A"}, state.WorkStatusPlanned, t0)
	seedFeature(t, db, "F-A", "PROG-1", nil, state.WorkStatusArchived, t0)

	loader := freshBriefLoader(t, db)
	if err := loader.Sync(context.Background(), t0); err != nil {
		t.Fatal(err)
	}

	gotB, _ := db.GetWorkItem(context.Background(), "F-B")
	if gotB.Status != state.WorkStatusArchived {
		t.Fatalf("F-B.status=%s want archived (depends on archived F-A)", gotB.Status)
	}
	gotC, _ := db.GetWorkItem(context.Background(), "F-C")
	if gotC.Status != state.WorkStatusArchived {
		t.Fatalf("F-C.status=%s want archived (multi-hop convergence)", gotC.Status)
	}
}

// ─────────────────────────────────────────────────────────────────
// W5 — v2 brief Sync materialises edges + exposes OutputsSchemas
// ─────────────────────────────────────────────────────────────────

// mustSignedBriefV2WithOpts builds + signs a v2 brief with caller-
// supplied parent + program IDs so cross-brief tests can stand up
// multiple briefs in one fsys.
func mustSignedBriefV2WithOpts(t *testing.T, key []byte, parentID, programID string, features []PlannedFeatureV2, producedAt time.Time) []byte {
	t.Helper()
	v2 := &ProgramBriefV2{
		ProgramBrief: ProgramBrief{
			SchemaVersion:    2,
			ProgramID:        programID,
			ParentWorkItemID: parentID,
			ParentCriteria:   []PlanCriterion{{ID: "c1", Text: "ship"}},
			PlannerModelID:   "test:model",
			ProducedAt:       producedAt,
		},
		FeaturesV2: features,
	}
	raw, err := json.Marshal(v2)
	if err != nil {
		t.Fatalf("marshal v2: %v", err)
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatalf("unmarshal generic: %v", err)
	}
	sig, err := schemas.Sign(generic, key, "key-1")
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	v2.Signature = sig
	signed, err := json.Marshal(v2)
	if err != nil {
		t.Fatalf("marshal signed v2: %v", err)
	}
	return signed
}

func TestBriefLoaderSync_V2BriefUpsertsEdges(t *testing.T) {
	db := newBriefTestDB(t)
	key := []byte("test-key-32-bytes-aaaaaaaaaaaaaaa")
	t0 := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	seedParent(t, db, "PROG-V2", t0)

	// Topology: F-A has a predicated edge to F-B and a default to F-C.
	// CheckReachability is satisfied because F-A's outgoing edge to F-B
	// transitively reaches F-C via F-B -> F-C.
	raw := mustSignedBriefV2WithOpts(t, key, "PROG-V2", "m-cccccccccccc",
		[]PlannedFeatureV2{
			{
				PlannedFeature: PlannedFeature{ID: "F-A", Title: "scan", Fulfills: []string{"c1"}},
				OutputsSchema: &OutputsSchema{
					Type:       "object",
					Properties: map[string]*OutputsSchema{"severity": {Type: "string"}},
				},
				Edges:       []Edge{{From: "F-A", To: "F-B", Predicate: `out.severity == "high"`, OnSkip: SkipCascade}},
				DefaultNext: strPtr("F-C"),
			},
			{
				PlannedFeature: PlannedFeature{ID: "F-B", Title: "remediate"},
				Edges:          []Edge{{From: "F-B", To: "F-C"}},
			},
			{PlannedFeature: PlannedFeature{ID: "F-C", Title: "report"}},
		}, t0)

	fsys := fstest.MapFS{"PROG-V2.json": &fstest.MapFile{Data: raw}}
	loader := mustNewLoader(t, BriefLoaderConfig{FS: fsys, DB: db, Keyring: map[string][]byte{"key-1": key}, Evaluator: NewEdgeEvaluator()})
	if err := loader.Sync(context.Background(), t0); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	edges, err := db.ListEdgesFrom(context.Background(), "F-A")
	if err != nil {
		t.Fatalf("ListEdgesFrom: %v", err)
	}
	if len(edges) != 2 {
		t.Fatalf("edges from F-A=%d want 2 (predicated + default): %+v", len(edges), edges)
	}
	var predicated, def *state.EdgeRow
	for i := range edges {
		e := edges[i]
		switch {
		case e.IsDefault:
			def = &edges[i]
		case e.PredicateCEL != "":
			predicated = &edges[i]
		}
	}
	if predicated == nil {
		t.Fatalf("predicated edge missing: %+v", edges)
	}
	if predicated.ToID != "F-B" || predicated.PredicateCEL != `out.severity == "high"` {
		t.Fatalf("predicated edge wrong: %+v", predicated)
	}
	if predicated.OnSkip != string(SkipCascade) {
		t.Fatalf("OnSkip=%q want cascade", predicated.OnSkip)
	}
	if def == nil {
		t.Fatalf("default edge missing: %+v", edges)
	}
	if def.ToID != "F-C" || !def.IsDefault {
		t.Fatalf("default edge wrong: %+v", def)
	}
	// F-B -> F-C unconditional should also be persisted.
	fromB, err := db.ListEdgesFrom(context.Background(), "F-B")
	if err != nil {
		t.Fatalf("ListEdgesFrom F-B: %v", err)
	}
	if len(fromB) != 1 || fromB[0].ToID != "F-C" {
		t.Fatalf("F-B edges=%+v want one edge to F-C", fromB)
	}
}

func TestBriefLoaderSync_V2BriefStoresOutputsSchemas(t *testing.T) {
	db := newBriefTestDB(t)
	key := []byte("test-key-32-bytes-aaaaaaaaaaaaaaa")
	t0 := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	seedParent(t, db, "PROG-V2", t0)

	raw := mustSignedBriefV2WithOpts(t, key, "PROG-V2", "m-cccccccccccc",
		[]PlannedFeatureV2{
			{
				PlannedFeature: PlannedFeature{ID: "F-A", Title: "scan", Fulfills: []string{"c1"}},
				OutputsSchema: &OutputsSchema{
					Type:       "object",
					Properties: map[string]*OutputsSchema{"severity": {Type: "string"}},
				},
				Edges:       []Edge{{From: "F-A", To: "F-B", Predicate: `out.severity == "high"`, OnSkip: SkipCascade}},
				DefaultNext: strPtr("F-B"),
			},
			{PlannedFeature: PlannedFeature{ID: "F-B", Title: "remediate"}},
		}, t0)
	fsys := fstest.MapFS{"PROG-V2.json": &fstest.MapFile{Data: raw}}

	loader := mustNewLoader(t, BriefLoaderConfig{FS: fsys, DB: db, Keyring: map[string][]byte{"key-1": key}, Evaluator: NewEdgeEvaluator()})
	if err := loader.Sync(context.Background(), t0); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	sch, ok := loader.OutputsSchemaForFeature("F-A")
	if !ok {
		t.Fatalf("F-A schema missing after sync")
	}
	if sch == nil || sch.Type != "object" {
		t.Fatalf("F-A schema wrong: %+v", sch)
	}
	if sch.Properties["severity"] == nil || sch.Properties["severity"].Type != "string" {
		t.Fatalf("F-A.severity property lost: %+v", sch.Properties)
	}
	if _, ok := loader.OutputsSchemaForFeature("F-B"); ok {
		t.Fatalf("F-B has no schema declared, lookup must miss")
	}
	if _, ok := loader.OutputsSchemaForFeature("F-UNKNOWN"); ok {
		t.Fatalf("unknown feature must miss")
	}
}

func TestBriefLoaderSync_V2BriefRejectionDoesNotWriteEdges(t *testing.T) {
	db := newBriefTestDB(t)
	key := []byte("test-key-32-bytes-aaaaaaaaaaaaaaa")
	t0 := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	seedParent(t, db, "PROG-V2-BAD", t0)

	// Predicate references undeclared field — ValidateV2 (via the
	// LoadAndVerifyBrief path) rejects with ErrPredicateUnknownField.
	raw := mustSignedBriefV2WithOpts(t, key, "PROG-V2-BAD", "m-dddddddddddd",
		[]PlannedFeatureV2{
			{
				PlannedFeature: PlannedFeature{ID: "F-A", Title: "scan", Fulfills: []string{"c1"}},
				OutputsSchema: &OutputsSchema{
					Type:       "object",
					Properties: map[string]*OutputsSchema{"severity": {Type: "string"}},
				},
				Edges:       []Edge{{From: "F-A", To: "F-B", Predicate: `out.missing == "x"`, OnSkip: SkipCascade}},
				DefaultNext: strPtr("F-B"),
			},
			{PlannedFeature: PlannedFeature{ID: "F-B", Title: "remediate"}},
		}, t0)
	fsys := fstest.MapFS{"PROG-V2-BAD.json": &fstest.MapFile{Data: raw}}

	logs := captureLogs(t)
	loader := mustNewLoader(t, BriefLoaderConfig{FS: fsys, DB: db, Keyring: map[string][]byte{"key-1": key}, Evaluator: NewEdgeEvaluator()})
	if err := loader.Sync(context.Background(), t0); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	edges, err := db.ListEdgesFrom(context.Background(), "F-A")
	if err != nil {
		t.Fatalf("ListEdgesFrom: %v", err)
	}
	if len(edges) != 0 {
		t.Fatalf("edges=%d want 0 (rejected brief must not write edges)", len(edges))
	}
	if _, ok := loader.OutputsSchemaForFeature("F-A"); ok {
		t.Fatalf("rejected brief must not expose schema")
	}
	out := logs.String()
	if !strings.Contains(out, "brief.rejected") {
		t.Fatalf("missing brief.rejected warn: %s", out)
	}
}

// TestBriefLoaderSync_V2SchemaStalePurged asserts OutputsSchema for a removed feature is purged on the next Sync tick.
func TestBriefLoaderSync_V2SchemaStalePurged(t *testing.T) {
	db := newBriefTestDB(t)
	key := []byte("test-key-32-bytes-aaaaaaaaaaaaaaa")
	t0 := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	seedParent(t, db, "PROG-V2", t0)

	rawT0 := mustSignedBriefV2WithOpts(t, key, "PROG-V2", "m-cccccccccccc",
		[]PlannedFeatureV2{
			{
				PlannedFeature: PlannedFeature{ID: "F-A", Title: "scan", Fulfills: []string{"c1"}},
				OutputsSchema: &OutputsSchema{
					Type:       "object",
					Properties: map[string]*OutputsSchema{"severity": {Type: "string"}},
				},
				Edges:       []Edge{{From: "F-A", To: "F-B", Predicate: `out.severity == "high"`, OnSkip: SkipCascade}},
				DefaultNext: strPtr("F-B"),
			},
			{PlannedFeature: PlannedFeature{ID: "F-B", Title: "remediate"}},
		}, t0)
	files := fstest.MapFS{"PROG-V2.json": &fstest.MapFile{Data: rawT0}}
	loader := mustNewLoader(t, BriefLoaderConfig{FS: files, DB: db, Keyring: map[string][]byte{"key-1": key}, Evaluator: NewEdgeEvaluator()})
	if err := loader.Sync(context.Background(), t0); err != nil {
		t.Fatalf("Sync 1: %v", err)
	}
	if _, ok := loader.OutputsSchemaForFeature("F-A"); !ok {
		t.Fatalf("F-A schema must be present after first sync")
	}

	// Re-plan removes F-A entirely (kept as a degenerate single-feature
	// brief). ProducedAt must advance past the prior watermark.
	t1 := t0.Add(time.Minute)
	rawT1 := mustSignedBriefV2WithOpts(t, key, "PROG-V2", "m-cccccccccccc",
		[]PlannedFeatureV2{
			{PlannedFeature: PlannedFeature{ID: "F-B", Title: "remediate", Fulfills: []string{"c1"}}},
		}, t1)
	files["PROG-V2.json"] = &fstest.MapFile{Data: rawT1}
	if err := loader.Sync(context.Background(), t1); err != nil {
		t.Fatalf("Sync 2: %v", err)
	}
	if _, ok := loader.OutputsSchemaForFeature("F-A"); ok {
		t.Fatalf("F-A schema must be purged after re-plan dropped the feature")
	}
}

func TestCascadeDep_UpdatedAtAdvances(t *testing.T) {
	db := newBriefTestDB(t)
	t0 := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	seedParent(t, db, "PROG-1", t0)
	seedFeature(t, db, "F-DEP", "PROG-1", nil, state.WorkStatusArchived, t0)
	seedFeature(t, db, "F-CHILD", "PROG-1", []string{"F-DEP"}, state.WorkStatusPlanned, t0)

	before, _ := db.GetWorkItem(context.Background(), "F-CHILD")
	loader := freshBriefLoader(t, db)
	t1 := t0.Add(10 * time.Second)
	// Sweep no-op (last_seen_at==t0, cutoff==t1 — but the sweep WILL
	// archive things here). Reset seed timestamps to t1 so the sweep
	// is still no-op AND the cascade stamp matches t1.
	seedFeature(t, db, "F-DEP", "PROG-1", nil, state.WorkStatusArchived, t1)
	seedFeature(t, db, "F-CHILD", "PROG-1", []string{"F-DEP"}, state.WorkStatusPlanned, t1)
	if err := loader.Sync(context.Background(), t1); err != nil {
		t.Fatal(err)
	}
	after, _ := db.GetWorkItem(context.Background(), "F-CHILD")
	if !after.UpdatedAt.After(before.UpdatedAt) {
		t.Fatalf("updated_at did not advance: before=%v after=%v", before.UpdatedAt, after.UpdatedAt)
	}
	if !after.UpdatedAt.Equal(t1.UTC().Truncate(time.Second)) {
		t.Fatalf("updated_at=%v want %v (pollStartedAt)", after.UpdatedAt, t1)
	}
}

// TestBriefLoader_LoggerInjected pins spec §5.7 + Task H contract.
func TestBriefLoader_LoggerInjected(t *testing.T) {
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	h := obstest.New()
	logger := slog.New(h)

	db := newBriefTestDB(t)
	key := []byte("test-key-32-bytes-aaaaaaaaaaaaaaa")
	_, raw := mustSignedBrief(t, key)
	// Parent is intentionally NOT seeded so the brief is rejected
	// with reason=unknown_parent_program — triggers the slog.Warn
	// callsite we want to verify routes through the injected logger.
	fsys := fstest.MapFS{"PROG-1.json": &fstest.MapFile{Data: raw}}

	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	loader := mustNewLoader(t, BriefLoaderConfig{FS: fsys, DB: db, Keyring: map[string][]byte{"key-1": key}, Logger: logger})

	if err := loader.Sync(context.Background(), now); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	msgs := h.Messages()
	found := false
	for _, m := range msgs {
		if strings.Contains(m, "brief.rejected") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("injected logger received no brief.rejected record; got %v", msgs)
	}
}

// TestNewBriefLoader_Config_RequiresFS — nil FS must error at New, not nil-deref on first Sync.
func TestNewBriefLoader_Config_RequiresFS(t *testing.T) {
	db := newBriefTestDB(t)
	if _, err := NewBriefLoader(BriefLoaderConfig{DB: db, Keyring: map[string][]byte{}}); err == nil {
		t.Fatal("NewBriefLoader with nil FS must error")
	} else if !strings.Contains(err.Error(), "FS") {
		t.Fatalf("error %q missing FS mention", err.Error())
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, err := NewBriefLoader(BriefLoaderConfig{DB: db, Keyring: map[string][]byte{}, Logger: logger}); err == nil {
		t.Fatal("NewBriefLoader with nil FS (Logger set) must still error")
	}
}

// TestNewBriefLoader_Config_RequiresDB — nil DB must error at New.
func TestNewBriefLoader_Config_RequiresDB(t *testing.T) {
	if _, err := NewBriefLoader(BriefLoaderConfig{FS: fstest.MapFS{}, Keyring: map[string][]byte{}}); err == nil {
		t.Fatal("NewBriefLoader with nil DB must error")
	} else if !strings.Contains(err.Error(), "DB") {
		t.Fatalf("error %q missing DB mention", err.Error())
	}
}

// mustUnsignedBriefRaw returns a well-formed brief JSON with no signature block for require-signing tests (#1364).
func mustUnsignedBriefRaw(t *testing.T) []byte {
	t.Helper()
	b := &ProgramBrief{
		SchemaVersion:    1,
		ProgramID:        "m-1234567890ab",
		ParentWorkItemID: "PROG-1",
		ParentCriteria:   []PlanCriterion{{ID: "c1", Text: "add foo"}},
		PlannerModelID:   "claude-test",
		Features:         []PlannedFeature{{ID: "F-1", Title: "foo", Fulfills: []string{"c1"}}},
		ProducedAt:       time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC),
	}
	raw, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	return raw
}

// TestBriefLoaderSync_RequireSigning_OffAllowsUnsigned asserts flag OFF preserves warn+skip semantics on an unsigned brief (#1364).
func TestBriefLoaderSync_RequireSigning_OffAllowsUnsigned(t *testing.T) {
	db := newBriefTestDB(t)
	key := []byte("test-key-32-bytes-aaaaaaaaaaaaaaa")
	raw := mustUnsignedBriefRaw(t)
	fsys := fstest.MapFS{"PROG-1.json": &fstest.MapFile{Data: raw}}
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	seedParent(t, db, "PROG-1", now)

	loader := mustNewLoader(t, BriefLoaderConfig{FS: fsys, DB: db, Keyring: map[string][]byte{"key-1": key}, RequireSigning: false})
	if err := loader.Sync(context.Background(), now); err != nil {
		t.Fatalf("Sync with RequireSigning=false must not fail on unsigned brief: %v", err)
	}
}

// TestBriefLoaderSync_RequireSigning_OnAcceptsValidSigned asserts flag ON accepts a valid-signature brief without error (#1364).
func TestBriefLoaderSync_RequireSigning_OnAcceptsValidSigned(t *testing.T) {
	db := newBriefTestDB(t)
	key := []byte("test-key-32-bytes-aaaaaaaaaaaaaaa")
	_, raw := mustSignedBrief(t, key)
	fsys := fstest.MapFS{"PROG-1.json": &fstest.MapFile{Data: raw}}
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	seedParent(t, db, "PROG-1", now)

	loader := mustNewLoader(t, BriefLoaderConfig{FS: fsys, DB: db, Keyring: map[string][]byte{"key-1": key}, RequireSigning: true})
	if err := loader.Sync(context.Background(), now); err != nil {
		t.Fatalf("Sync with RequireSigning=true + valid signed brief must succeed: %v", err)
	}
	children, err := db.ListByParent(context.Background(), "PROG-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 3 {
		t.Fatalf("children=%d want 3", len(children))
	}
}

// TestBriefLoaderSync_RequireSigning_OnRejectsUnsigned asserts flag ON returns an error containing "signing required" on an unsigned brief (#1364).
func TestBriefLoaderSync_RequireSigning_OnRejectsUnsigned(t *testing.T) {
	db := newBriefTestDB(t)
	key := []byte("test-key-32-bytes-aaaaaaaaaaaaaaa")
	raw := mustUnsignedBriefRaw(t)
	fsys := fstest.MapFS{"PROG-1.json": &fstest.MapFile{Data: raw}}
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	seedParent(t, db, "PROG-1", now)

	loader := mustNewLoader(t, BriefLoaderConfig{FS: fsys, DB: db, Keyring: map[string][]byte{"key-1": key}, RequireSigning: true})
	err := loader.Sync(context.Background(), now)
	if err == nil {
		t.Fatal("Sync with RequireSigning=true + unsigned brief must error")
	}
	if !strings.Contains(err.Error(), "signing required") {
		t.Fatalf("error %q missing 'signing required' phrase", err.Error())
	}
}

// TestBriefLoaderSync_RequireSigning_OnRejectsTampered asserts flag ON rejects an invalid-signature (tampered) brief with the signing-required phrase (#1364).
func TestBriefLoaderSync_RequireSigning_OnRejectsTampered(t *testing.T) {
	db := newBriefTestDB(t)
	key := []byte("test-key-32-bytes-aaaaaaaaaaaaaaa")
	_, raw := mustSignedBrief(t, key)
	tampered := append([]byte{}, raw...)
	for i, b := range tampered {
		if b == 'f' {
			tampered[i] = 'x'
			break
		}
	}
	fsys := fstest.MapFS{"PROG-1.json": &fstest.MapFile{Data: tampered}}
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	seedParent(t, db, "PROG-1", now)

	loader := mustNewLoader(t, BriefLoaderConfig{FS: fsys, DB: db, Keyring: map[string][]byte{"key-1": key}, RequireSigning: true})
	err := loader.Sync(context.Background(), now)
	if err == nil {
		t.Fatal("Sync with RequireSigning=true + tampered brief must error")
	}
	if !strings.Contains(err.Error(), "signing required") {
		t.Fatalf("error %q missing 'signing required' phrase", err.Error())
	}
}

// TestNewBriefLoader_Config_RequiresKeyring — nil Keyring rejected at New, not deferred to LoadAndVerifyBrief.
func TestNewBriefLoader_Config_RequiresKeyring(t *testing.T) {
	db := newBriefTestDB(t)
	if _, err := NewBriefLoader(BriefLoaderConfig{FS: fstest.MapFS{}, DB: db}); err == nil {
		t.Fatal("NewBriefLoader with nil Keyring must error")
	} else if !strings.Contains(err.Error(), "Keyring") {
		t.Fatalf("error %q missing Keyring mention", err.Error())
	}
	if _, err := NewBriefLoader(BriefLoaderConfig{}); err == nil {
		t.Fatal("NewBriefLoader with empty Config must error")
	}
}
