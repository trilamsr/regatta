package edgeagg

import (
	"database/sql"
	"testing"

	// Blank import so this test binary registers rapid's -rapid.checks
	// flag — `make property-test` walks ./internal/orchestrator/state/...
	// and the dbtest/substrate siblings already do this for the same
	// reason.
	_ "pgregory.net/rapid"
)

// TestBuildAggregate converts raw SQL scan outputs into EdgeFromAggregate (#795 T2).
func TestBuildAggregate(t *testing.T) {
	cases := []struct {
		name            string
		nonDefaultCount int
		anyTrue         sql.NullInt64
		anyPending      sql.NullInt64
		defaultCount    int
		defID           sql.NullInt64
		defFired        sql.NullString
		defProgram      sql.NullString
		want            EdgeFromAggregate
	}{
		{
			name:            "all-null-empty-from",
			nonDefaultCount: 0,
			defaultCount:    0,
			want:            EdgeFromAggregate{},
		},
		{
			name:            "non-default-all-false",
			nonDefaultCount: 2,
			anyTrue:         sql.NullInt64{Int64: 0, Valid: true},
			anyPending:      sql.NullInt64{Int64: 0, Valid: true},
			defaultCount:    1,
			defID:           sql.NullInt64{Int64: 42, Valid: true},
			defFired:        sql.NullString{String: "pending", Valid: true},
			defProgram:      sql.NullString{String: "m-1", Valid: true},
			want: EdgeFromAggregate{
				NonDefaultCount:  2,
				DefaultCount:     1,
				DefaultEdgeID:    42,
				DefaultFired:     "pending",
				DefaultProgramID: "m-1",
			},
		},
		{
			name:            "any-true-blocks",
			nonDefaultCount: 2,
			anyTrue:         sql.NullInt64{Int64: 1, Valid: true},
			anyPending:      sql.NullInt64{Int64: 0, Valid: true},
			defaultCount:    1,
			defID:           sql.NullInt64{Int64: 7, Valid: true},
			defFired:        sql.NullString{String: "pending", Valid: true},
			defProgram:      sql.NullString{String: "m-1", Valid: true},
			want: EdgeFromAggregate{
				NonDefaultCount:   2,
				AnyNonDefaultTrue: true,
				DefaultCount:      1,
				DefaultEdgeID:     7,
				DefaultFired:      "pending",
				DefaultProgramID:  "m-1",
			},
		},
		{
			name:            "any-pending-blocks",
			nonDefaultCount: 3,
			anyTrue:         sql.NullInt64{Int64: 0, Valid: true},
			anyPending:      sql.NullInt64{Int64: 1, Valid: true},
			defaultCount:    0,
			want: EdgeFromAggregate{
				NonDefaultCount:      3,
				AnyNonDefaultPending: true,
			},
		},
		{
			name:            "no-default-row-program-still-set",
			nonDefaultCount: 1,
			anyTrue:         sql.NullInt64{Int64: 0, Valid: true},
			anyPending:      sql.NullInt64{Int64: 0, Valid: true},
			defaultCount:    0,
			defProgram:      sql.NullString{String: "m-1", Valid: true},
			want: EdgeFromAggregate{
				NonDefaultCount:  1,
				DefaultProgramID: "m-1",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := BuildAggregate(tc.nonDefaultCount, tc.anyTrue, tc.anyPending, tc.defaultCount, tc.defID, tc.defFired, tc.defProgram)
			if got != tc.want {
				t.Errorf("got=%+v want=%+v", got, tc.want)
			}
		})
	}
}

// TestBoolToInt maps true to 1 and false to 0 (#795 T2).
func TestBoolToInt(t *testing.T) {
	if BoolToInt(true) != 1 {
		t.Errorf("BoolToInt(true)=%d want 1", BoolToInt(true))
	}
	if BoolToInt(false) != 0 {
		t.Errorf("BoolToInt(false)=%d want 0", BoolToInt(false))
	}
}
