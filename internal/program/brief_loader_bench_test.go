package program

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	_ "modernc.org/sqlite"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// silenceSlog redirects slog to io.Discard for the duration of a bench
// so brief.rejected / brief.tombstoned chatter does not dominate the
// per-op cost or flood the operator's terminal.
func silenceSlog(b *testing.B) {
	b.Helper()
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Cleanup(func() { slog.SetDefault(prev) })
}

func newBriefBenchDB(b *testing.B) *state.DB {
	b.Helper()
	db, err := state.Open(context.Background(), state.DSN(filepath.Join(b.TempDir(), "bl.db")))
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	b.Cleanup(func() { _ = db.Close() })
	return db
}

// seedBriefCorpus writes n signed briefs onto a fstest.MapFS, each
// targeting a distinct parent program row that we also pre-insert into
// state. Feature IDs are namespaced by brief index so the
// cross-brief-collision rejection path is never exercised — the bench
// times the success path Sync's wired for.
func seedBriefCorpus(b *testing.B, db *state.DB, n int, key []byte) fstest.MapFS {
	b.Helper()
	fsys := fstest.MapFS{}
	at := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	ctx := context.Background()

	for i := 0; i < n; i++ {
		parentID := fmt.Sprintf("PROG-%04d", i)
		programID := fmt.Sprintf("m-%012x", i+1)
		brief := &ProgramBrief{
			SchemaVersion:    1,
			ProgramID:        programID,
			ParentWorkItemID: parentID,
			ParentCriteria: []PlanCriterion{
				{ID: fmt.Sprintf("c%04d", i), Text: "criterion"},
			},
			PlannerModelID: "claude-bench",
			ProducedAt:     at,
			Features: []PlannedFeature{
				{ID: fmt.Sprintf("F-%04d-A", i), Title: "a",
					Fulfills: []string{fmt.Sprintf("c%04d", i)}},
			},
		}
		signed, err := brief.Sign(key, "key-1")
		if err != nil {
			b.Fatalf("Sign: %v", err)
		}
		raw, err := json.Marshal(signed)
		if err != nil {
			b.Fatalf("Marshal: %v", err)
		}
		fsys[fmt.Sprintf("%s.json", parentID)] = &fstest.MapFile{Data: raw}

		parent := state.WorkItem{
			ID: parentID, Kind: state.KindProgram, Title: parentID,
			Lane: "server", Status: state.WorkStatusPlanned,
		}
		if err := db.UpsertWorkItem(ctx, parent, state.SourceAdapter, at.Add(-time.Hour)); err != nil {
			b.Fatalf("seed parent %s: %v", parentID, err)
		}
	}
	return fsys
}

// BenchmarkBriefLoaderSync times an end-to-end BriefLoader.Sync over a
// fixed-size brief corpus. The fixture is a fstest.MapFS — no on-disk
// I/O — so the cost we see is signature verify + Validate + per-feature
// CycleCheck + UpsertWorkItem + the tombstone sweep. Each iteration
// bumps pollStartedAt so the watermark check stays well-defined across
// repeats (later iterations exercise the stale_produced_at no-op path —
// the operator-visible cost of polling when no brief changed).
func BenchmarkBriefLoaderSync(b *testing.B) {
	key := []byte("test-key-32-bytes-aaaaaaaaaaaaaaa")
	sizes := []int{10, 100}
	for _, n := range sizes {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			silenceSlog(b)
			db := newBriefBenchDB(b)
			fsys := seedBriefCorpus(b, db, n, key)
			loader, err := NewBriefLoader(BriefLoaderConfig{FS: fsys, DB: db, Keyring: map[string][]byte{"key-1": key}})
			if err != nil {
				b.Fatalf("NewBriefLoader: %v", err)
			}
			base := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC).Add(time.Hour)
			ctx := context.Background()

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := loader.Sync(ctx, base.Add(time.Duration(i)*time.Second)); err != nil {
					b.Fatalf("Sync: %v", err)
				}
			}
		})
	}
}
