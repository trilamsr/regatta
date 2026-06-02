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

// seedBriefCorpus writes n signed briefs onto a fstest.MapFS. featPerBrief
// features per brief surface the batched-UPSERT win (issue #89): one
// feature per brief lands one Batch call of size 1 — equivalent to a
// single UpsertWorkItem — so the perf delta only shows up when briefs
// carry realistic multi-feature payloads.
func seedBriefCorpus(b *testing.B, db *state.DB, n, featPerBrief int, key []byte) fstest.MapFS {
	b.Helper()
	fsys := fstest.MapFS{}
	at := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	ctx := context.Background()

	for i := 0; i < n; i++ {
		parentID := fmt.Sprintf("PROG-%04d", i)
		programID := fmt.Sprintf("m-%012x", i+1)
		feats := make([]PlannedFeature, featPerBrief)
		crits := make([]PlanCriterion, featPerBrief)
		for j := 0; j < featPerBrief; j++ {
			cid := fmt.Sprintf("c%04d-%02d", i, j)
			crits[j] = PlanCriterion{ID: cid, Text: "criterion"}
			feats[j] = PlannedFeature{
				ID:       fmt.Sprintf("F-%04d-%02d", i, j),
				Title:    fmt.Sprintf("feat-%d-%d", i, j),
				Fulfills: []string{cid},
			}
		}
		brief := &ProgramBrief{
			SchemaVersion:    1,
			ProgramID:        programID,
			ParentWorkItemID: parentID,
			ParentCriteria:   crits,
			PlannerModelID:   "claude-bench",
			ProducedAt:       at,
			Features:         feats,
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

// BenchmarkBriefLoaderSync times end-to-end BriefLoader.Sync. Two shapes surface the batched-UPSERT delta (issue #89): N briefs × 1 feature (B
func BenchmarkBriefLoaderSync(b *testing.B) {
	key := []byte("test-key-32-bytes-aaaaaaaaaaaaaaa")
	shapes := []struct {
		n, featPerBrief int
	}{
		{10, 1},
		{100, 1},
		{10, 10},
		{100, 10},
	}
	for _, s := range shapes {
		name := fmt.Sprintf("N=%d_F=%d", s.n, s.featPerBrief)
		b.Run(name, func(b *testing.B) {
			silenceSlog(b)
			db := newBriefBenchDB(b)
			fsys := seedBriefCorpus(b, db, s.n, s.featPerBrief, key)
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
