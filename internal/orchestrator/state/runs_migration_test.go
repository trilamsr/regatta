package state

import (
	"context"
	"testing"
)

// TestRunsMigration_RoundTrip asserts migration 0018 lets a full runs row insert + select (#operator-console-S0).
func TestRunsMigration_RoundTrip(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)

	_, err := db.SQL().Exec(`
		INSERT INTO runs (
			id, started_at, finished_at, status,
			spec_hash, model_hash, prompt_template_hash, tool_impl_hash,
			seed, versions_json, causal_hash, rerun_of, trace_id,
			declared_effect_class
		) VALUES (
			'run-test-1', 1717000000, NULL, 'running',
			'sh', 'mh', 'pth', 'tih',
			'seed-x', '{"go":"1.25.0"}', 'ch-x', NULL, '00000000000000000000000000000000',
			'filesystem-write+gh-mutation'
		)`)
	if err != nil {
		t.Fatalf("insert into runs: %v", err)
	}

	var (
		id, status, specHash, modelHash, ptHash, tiHash, seed, versionsJSON, causalHash, traceID, declEC string
		startedAt                                                                                        int64
		finishedAt                                                                                       any
		rerunOf                                                                                          any
	)
	err = db.SQL().QueryRow(`
		SELECT id, started_at, finished_at, status, spec_hash, model_hash,
		       prompt_template_hash, tool_impl_hash, seed, versions_json,
		       causal_hash, rerun_of, trace_id, declared_effect_class
		FROM runs WHERE id = 'run-test-1'
	`).Scan(&id, &startedAt, &finishedAt, &status, &specHash, &modelHash,
		&ptHash, &tiHash, &seed, &versionsJSON, &causalHash, &rerunOf, &traceID, &declEC)
	if err != nil {
		t.Fatalf("select: %v", err)
	}

	if id != "run-test-1" || startedAt != 1717000000 || status != "running" ||
		specHash != "sh" || modelHash != "mh" || ptHash != "pth" || tiHash != "tih" ||
		seed != "seed-x" || versionsJSON != `{"go":"1.25.0"}` || causalHash != "ch-x" ||
		traceID != "00000000000000000000000000000000" || declEC != "filesystem-write+gh-mutation" {
		t.Errorf("round-trip mismatch: id=%q started=%d status=%q dec=%q",
			id, startedAt, status, declEC)
	}
	_ = finishedAt
	_ = rerunOf
}

// TestRunsMigration_IndexesExist asserts the three runs indexes ship with migration 0018 (#operator-console-S0).
func TestRunsMigration_IndexesExist(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)

	rows, err := db.SQL().QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type='index' AND tbl_name='runs'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	want := map[string]bool{"idx_runs_started": false, "idx_runs_causal_hash": false, "idx_runs_rerun_of": false}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		if _, ok := want[name]; ok {
			want[name] = true
		}
	}
	for n, found := range want {
		if !found {
			t.Errorf("index %s missing", n)
		}
	}
}
