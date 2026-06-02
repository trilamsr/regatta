package adaptersync_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/orchestrator/adaptersync"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// silenceSlogBench discards slog output so adapter.tombstoned chatter does
// not dominate per-op cost.
func silenceSlogBench(b *testing.B) {
	b.Helper()
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Cleanup(func() { slog.SetDefault(prev) })
}

// BenchmarkAdapterSync_NItems times AdapterSync.Sync at varying corpus sizes.
func BenchmarkAdapterSync_NItems(b *testing.B) {
	for _, n := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			silenceSlogBench(b)
			db, err := state.Open(context.Background(), state.DSN(filepath.Join(b.TempDir(), "as.db")))
			if err != nil {
				b.Fatalf("Open: %v", err)
			}
			b.Cleanup(func() { _ = db.Close() })

			items := make([]schemas.WorkItem, n)
			for i := range items {
				items[i] = schemas.WorkItem{
					ID:     schemas.WorkItemID(fmt.Sprintf("ITEM-%05d", i)),
					Kind:   schemas.KindFeature,
					Title:  fmt.Sprintf("item-%d", i),
					Lane:   "server",
					Status: schemas.StatusPlanned,
				}
			}
			adapter := &stubAdapter{items: items}
			syncer, err := adaptersync.New(adaptersync.Config{Adapter: adapter, DB: db})
			if err != nil {
				b.Fatalf("New: %v", err)
			}
			base := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
			ctx := context.Background()

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := syncer.Sync(ctx, base.Add(time.Duration(i)*time.Second)); err != nil {
					b.Fatalf("Sync: %v", err)
				}
			}
		})
	}
}
