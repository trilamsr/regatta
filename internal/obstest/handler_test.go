package obstest_test

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/obstest"
)

func TestHandler_RecordsCapturesEveryEmission(t *testing.T) {
	h := obstest.New()
	log := slog.New(h)

	log.Info("first")
	log.Info("second")

	got := h.Records()
	if len(got) != 2 {
		t.Fatalf("len(Records)=%d; want 2", len(got))
	}
	if got[0].Message != "first" || got[1].Message != "second" {
		t.Fatalf("messages=%q,%q; want first,second", got[0].Message, got[1].Message)
	}
}

func TestHandler_RecordsReturnsCopy(t *testing.T) {
	h := obstest.New()
	slog.New(h).Info("one")

	snap := h.Records()
	snap[0].Message = "mutated"

	if h.Records()[0].Message != "one" {
		t.Fatalf("caller mutation leaked into handler state")
	}
}

func TestHandler_FindEventMatchesByEventName(t *testing.T) {
	h := obstest.New()
	log := slog.New(h)
	log.Info(string(obs.EventTickStarted))
	log.Info(string(obs.EventTickCompleted))

	rec, ok := h.FindEvent(obs.EventTickCompleted)
	if !ok {
		t.Fatalf("FindEvent(%q) not found", obs.EventTickCompleted)
	}
	if rec.Message != string(obs.EventTickCompleted) {
		t.Fatalf("rec.Message=%q; want %q", rec.Message, obs.EventTickCompleted)
	}
}

func TestHandler_FindEventReturnsFalseWhenAbsent(t *testing.T) {
	h := obstest.New()
	slog.New(h).Info("something.else")

	if _, ok := h.FindEvent(obs.EventTickStarted); ok {
		t.Fatalf("FindEvent unexpectedly found event")
	}
}

func TestHandler_Messages(t *testing.T) {
	h := obstest.New()
	log := slog.New(h)
	log.Info("a")
	log.Info("b")
	log.Info("c")

	got := h.Messages()
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("len(Messages)=%d; want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Messages[%d]=%q; want %q", i, got[i], want[i])
		}
	}
}

func TestHandler_EnabledIsAlwaysTrue(t *testing.T) {
	h := obstest.New()
	if !h.Enabled(context.Background(), slog.LevelDebug) {
		t.Fatalf("Enabled(debug)=false; want true")
	}
}

func TestHandler_ConcurrentHandleIsRaceFree(t *testing.T) {
	h := obstest.New()
	log := slog.New(h)

	const workers = 16
	const perWorker = 64
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < perWorker; j++ {
				log.Info("concurrent")
			}
		}()
	}
	wg.Wait()

	if got := len(h.Records()); got != workers*perWorker {
		t.Fatalf("len(Records)=%d; want %d", got, workers*perWorker)
	}
}

func TestHandler_InlineAttrsPreserved(t *testing.T) {
	h := obstest.New()
	slog.New(h).Info("evt", "k", "v")

	recs := h.Records()
	if len(recs) != 1 {
		t.Fatalf("len(Records)=%d; want 1", len(recs))
	}
	var sawK bool
	recs[0].Attrs(func(a slog.Attr) bool {
		if a.Key == "k" {
			sawK = a.Value.String() == "v"
		}
		return true
	})
	if !sawK {
		t.Fatalf("inline attr lost; want k=v")
	}
}

func TestHandler_WithGroupRecordsCaptured(t *testing.T) {
	h := obstest.New()
	slog.New(h).WithGroup("g").Info("evt")

	recs := h.Records()
	if len(recs) != 1 {
		t.Fatalf("len(Records)=%d; want 1", len(recs))
	}
	if recs[0].Message != "evt" {
		t.Fatalf("Message=%q; want evt", recs[0].Message)
	}
}
