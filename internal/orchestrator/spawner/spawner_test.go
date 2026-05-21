package spawner

import (
	"context"
	"sync"
	"testing"
)

func TestStubReturnsUniqueIdentity(t *testing.T) {
	s := NewStub()
	ctx := context.Background()
	a, err := s.Spawn(ctx, Request{AgentID: 1, WorkItemID: "WORK-1", Lane: "server"})
	if err != nil {
		t.Fatalf("spawn a: %v", err)
	}
	b, err := s.Spawn(ctx, Request{AgentID: 2, WorkItemID: "WORK-2", Lane: "server"})
	if err != nil {
		t.Fatalf("spawn b: %v", err)
	}
	if a.PID == b.PID || a.SessionID == b.SessionID {
		t.Fatalf("stub returned colliding identity: %+v vs %+v", a, b)
	}
	if a.PID >= 0 || b.PID >= 0 {
		t.Fatalf("stub PIDs must be negative; got %d, %d", a.PID, b.PID)
	}
}

func TestStubRecordsCalls(t *testing.T) {
	s := NewStub()
	_, _ = s.Spawn(context.Background(), Request{AgentID: 7, WorkItemID: "WORK-7"})
	calls := s.Calls()
	if len(calls) != 1 || calls[0].AgentID != 7 {
		t.Fatalf("unexpected calls log: %+v", calls)
	}
}

func TestStubConcurrentSafe(t *testing.T) {
	s := NewStub()
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _ = s.Spawn(context.Background(), Request{AgentID: int64(i)})
		}(i)
	}
	wg.Wait()
	if got := len(s.Calls()); got != 32 {
		t.Fatalf("got %d calls, want 32", got)
	}
}
