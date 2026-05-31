package scheduler

import (
	"context"
	"errors"
	"sync"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// wrappingDB is Seam-2: proxies *state.DB, lets tests drop MarkEdgeFired
// writes mid-tick to model a crash between sibling commits.
type wrappingDB struct {
	*state.DB

	mu                 sync.Mutex
	markEdgeFiredHook  func(edgeID int64, fired, contentSHA string) (skip bool, err error)
	markEdgeFiredCalls []markedEdge
	defaultIDs         map[int64]bool
}

type markedEdge struct {
	edgeID int64
	fired  string
	sha    string
}

func newWrappingDB(realDB *state.DB) *wrappingDB {
	return &wrappingDB{DB: realDB, defaultIDs: map[int64]bool{}}
}

func (w *wrappingDB) MarkEdgeFired(ctx context.Context, edgeID int64, fired, contentSHA string) error {
	w.mu.Lock()
	hook := w.markEdgeFiredHook
	w.mu.Unlock()

	if hook != nil {
		skip, err := hook(edgeID, fired, contentSHA)
		if skip {
			return err
		}
	}
	if err := w.DB.MarkEdgeFired(ctx, edgeID, fired, contentSHA); err != nil {
		return err
	}
	w.mu.Lock()
	w.markEdgeFiredCalls = append(w.markEdgeFiredCalls, markedEdge{edgeID, fired, contentSHA})
	w.mu.Unlock()
	return nil
}

func (w *wrappingDB) defaultFireCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := 0
	for _, m := range w.markEdgeFiredCalls {
		if m.fired == "true" && w.defaultIDs[m.edgeID] {
			n++
		}
	}
	return n
}

var errSimulatedCrash = errors.New("simulated crash")

// failAfterNMarkHook lets the first n MarkEdgeFired calls reach the DB,
// then returns errSimulatedCrash without writing. State is per-hook;
// reassign between ticks to reset.
func failAfterNMarkHook(n int) func(int64, string, string) (bool, error) {
	var seen int
	var mu sync.Mutex
	return func(edgeID int64, fired, contentSHA string) (bool, error) {
		mu.Lock()
		defer mu.Unlock()
		if seen < n {
			seen++
			return false, nil
		}
		return true, errSimulatedCrash
	}
}

func passThroughHook() func(int64, string, string) (bool, error) {
	return func(int64, string, string) (bool, error) { return false, nil }
}
