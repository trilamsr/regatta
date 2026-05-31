package program

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	_ "modernc.org/sqlite"

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

// fakeClock is a goroutine-safe deterministic clock for the tests
// below. The orchestrator/clock package was removed in commit ca58d36;
// the project standard is `func() time.Time` injected into *state.DB
// via SetClock. This struct just bundles Now/Advance so the test
// reads naturally.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock(t time.Time) *fakeClock      { return &fakeClock{t: t} }
func (f *fakeClock) Now() time.Time            { f.mu.Lock(); defer f.mu.Unlock(); return f.t }
func (f *fakeClock) NowFunc() func() time.Time { return f.Now }
func (f *fakeClock) Advance(d time.Duration)   { f.mu.Lock(); defer f.mu.Unlock(); f.t = f.t.Add(d) }

func mustSignedBrief(t *testing.T, key []byte) (*ProgramBrief, []byte) {
	t.Helper()
	b := &ProgramBrief{
		SchemaVersion:    1,
		ProgramID:        "m-1234567890ab",
		ParentWorkItemID: "PROG-1",
		ParentCriteria: []PlanCriterion{
			{ID: "c1", Text: "add foo"},
			{ID: "c2", Text: "add bar"},
			{ID: "c3", Text: "add baz"},
		},
		PlannerModelID: "claude-test",
		Features: []PlannedFeature{
			{ID: "F-1", Title: "foo", Fulfills: []string{"c1"}},
			{ID: "F-2", Title: "bar", Fulfills: []string{"c2"}},
			{ID: "F-3", Title: "baz", Fulfills: []string{"c3"}},
		},
		ProducedAt: time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC),
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

func TestBriefLoaderSync_UpsertsThreeChildren(t *testing.T) {
	db := newBriefTestDB(t)
	key := []byte("test-key-32-bytes-aaaaaaaaaaaaaaa")
	_, raw := mustSignedBrief(t, key)
	fsys := fstest.MapFS{"PROG-1.json": &fstest.MapFile{Data: raw}}

	clk := newFakeClock(time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC))

	// Seed the parent program row so cascade lookups would work.
	// UpsertWorkItem stamps last_seen_at from db.now(); install the
	// fake clock first so the seed row's timestamp is deterministic.
	db.SetClock(clk.NowFunc())
	parent := state.WorkItem{ID: "PROG-1", Kind: state.KindProgram, Title: "p",
		Lane: "server", Status: state.WorkStatusPlanned}
	if err := db.UpsertWorkItem(context.Background(), parent, state.SourceAdapter); err != nil {
		t.Fatal(err)
	}

	loader := NewBriefLoader(fsys, db, clk.NowFunc(), map[string][]byte{"key-1": key})

	if err := loader.Sync(context.Background(), clk.Now()); err != nil {
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

	clk := newFakeClock(time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC))
	loader := NewBriefLoader(fsys, db, clk.NowFunc(), map[string][]byte{"key-1": key})
	if err := loader.Sync(context.Background(), clk.Now()); err != nil {
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

	clk := newFakeClock(time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC))
	loader := NewBriefLoader(files, db, clk.NowFunc(), map[string][]byte{"key-1": key})
	if err := loader.Sync(context.Background(), clk.Now()); err != nil {
		t.Fatal(err)
	}

	// Next tick — brief gone.
	clk.Advance(1 * time.Second)
	delete(files, "PROG-1.json")
	if err := loader.Sync(context.Background(), clk.Now()); err != nil {
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
