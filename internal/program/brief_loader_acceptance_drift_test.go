package program

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// TestBriefLoaderSync_AcceptanceDriftWarnsForInflightChildren — issue #78.
func TestBriefLoaderSync_AcceptanceDriftWarnsForInflightChildren(t *testing.T) {
	db := newBriefTestDB(t)
	key := []byte("test-key-32-bytes-aaaaaaaaaaaaaaa")
	t0 := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	seedParent(t, db, "PROG-1", t0)

	feats := []PlannedFeature{{ID: "F-1", Title: "foo", Fulfills: []string{"c1"}}}
	critsOld := []PlanCriterion{{ID: "c1", Text: "add foo"}}
	_, rawOld := mustSignedBriefWithOpts(t, key, "PROG-1", "m-aaaaaaaaaaaa", t0, feats, critsOld)
	files := fstest.MapFS{"PROG-1.json": &fstest.MapFile{Data: rawOld}}

	loader := mustNewLoader(t, BriefLoaderConfig{FS: files, DB: db, Keyring: map[string][]byte{"key-1": key}})
	if err := loader.Sync(context.Background(), t0); err != nil {
		t.Fatalf("initial Sync: %v", err)
	}

	// Operator transitions F-1 past planned — agent is in-flight against the
	// snapshotted criterion text "add foo".
	if _, err := db.SQL().ExecContext(context.Background(),
		`UPDATE work_items SET status = ? WHERE id = ?`, string(state.WorkStatusRunning), "F-1"); err != nil {
		t.Fatalf("flip running: %v", err)
	}

	// Operator edits the brief: criterion text changes, ProducedAt bumps so the
	// stale-replay guard does NOT reject the re-load.
	critsNew := []PlanCriterion{{ID: "c1", Text: "add foo AND bar"}}
	_, rawNew := mustSignedBriefWithOpts(t, key, "PROG-1", "m-bbbbbbbbbbbb", t0.Add(time.Minute), feats, critsNew)
	files["PROG-1.json"] = &fstest.MapFile{Data: rawNew}

	logs := captureLogs(t)
	if err := loader.Sync(context.Background(), t0.Add(time.Minute)); err != nil {
		t.Fatalf("re-Sync: %v", err)
	}

	out := logs.String()
	if !strings.Contains(out, string(obs.EventBriefCriteriaDrift)) {
		t.Fatalf("missing %s warn for in-flight child: %s", obs.EventBriefCriteriaDrift, out)
	}
	if !strings.Contains(out, "F-1") {
		t.Fatalf("warn missing child id F-1: %s", out)
	}
	if !strings.Contains(out, "PROG-1") {
		t.Fatalf("warn missing parent id PROG-1: %s", out)
	}
}

// TestBriefLoaderSync_AcceptanceDriftQuietForPlannedChildren — staleness only matters for spawned (post-planned) children; planned rows just s
func TestBriefLoaderSync_AcceptanceDriftQuietForPlannedChildren(t *testing.T) {
	db := newBriefTestDB(t)
	key := []byte("test-key-32-bytes-aaaaaaaaaaaaaaa")
	t0 := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	seedParent(t, db, "PROG-1", t0)

	feats := []PlannedFeature{{ID: "F-1", Title: "foo", Fulfills: []string{"c1"}}}
	_, rawOld := mustSignedBriefWithOpts(t, key, "PROG-1", "m-aaaaaaaaaaaa", t0, feats,
		[]PlanCriterion{{ID: "c1", Text: "add foo"}})
	files := fstest.MapFS{"PROG-1.json": &fstest.MapFile{Data: rawOld}}

	loader := mustNewLoader(t, BriefLoaderConfig{FS: files, DB: db, Keyring: map[string][]byte{"key-1": key}})
	if err := loader.Sync(context.Background(), t0); err != nil {
		t.Fatalf("initial Sync: %v", err)
	}
	// F-1 stays planned — never spawned.

	_, rawNew := mustSignedBriefWithOpts(t, key, "PROG-1", "m-bbbbbbbbbbbb", t0.Add(time.Minute), feats,
		[]PlanCriterion{{ID: "c1", Text: "add foo AND bar"}})
	files["PROG-1.json"] = &fstest.MapFile{Data: rawNew}

	logs := captureLogs(t)
	if err := loader.Sync(context.Background(), t0.Add(time.Minute)); err != nil {
		t.Fatalf("re-Sync: %v", err)
	}
	if strings.Contains(logs.String(), string(obs.EventBriefCriteriaDrift)) {
		t.Fatalf("planned-only child must not warn: %s", logs.String())
	}
}

// TestBriefLoaderSync_AcceptanceDriftQuietWhenIdentical — re-sync of a byte-identical brief is operator no-op; warning must not fire.
func TestBriefLoaderSync_AcceptanceDriftQuietWhenIdentical(t *testing.T) {
	db := newBriefTestDB(t)
	key := []byte("test-key-32-bytes-aaaaaaaaaaaaaaa")
	t0 := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	seedParent(t, db, "PROG-1", t0)

	feats := []PlannedFeature{{ID: "F-1", Title: "foo", Fulfills: []string{"c1"}}}
	crits := []PlanCriterion{{ID: "c1", Text: "add foo"}}
	_, raw1 := mustSignedBriefWithOpts(t, key, "PROG-1", "m-aaaaaaaaaaaa", t0, feats, crits)
	files := fstest.MapFS{"PROG-1.json": &fstest.MapFile{Data: raw1}}

	loader := mustNewLoader(t, BriefLoaderConfig{FS: files, DB: db, Keyring: map[string][]byte{"key-1": key}})
	if err := loader.Sync(context.Background(), t0); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(context.Background(),
		`UPDATE work_items SET status = ? WHERE id = ?`, string(state.WorkStatusRunning), "F-1"); err != nil {
		t.Fatal(err)
	}

	// Same criteria, fresh ProducedAt (re-sign for watermark advance).
	_, raw2 := mustSignedBriefWithOpts(t, key, "PROG-1", "m-bbbbbbbbbbbb", t0.Add(time.Minute), feats, crits)
	files["PROG-1.json"] = &fstest.MapFile{Data: raw2}

	logs := captureLogs(t)
	if err := loader.Sync(context.Background(), t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logs.String(), string(obs.EventBriefCriteriaDrift)) {
		t.Fatalf("identical criteria must not warn: %s", logs.String())
	}
	// Sanity: snapshot equals new criterion text marshalling.
	got, err := db.GetWorkItem(context.Background(), "F-1")
	if err != nil {
		t.Fatal(err)
	}
	var snap []PlanCriterion
	if err := json.Unmarshal([]byte(got.AcceptanceJSON), &snap); err != nil {
		t.Fatal(err)
	}
	if len(snap) != 1 || snap[0].Text != "add foo" {
		t.Fatalf("unexpected snapshot: %+v", snap)
	}
}
