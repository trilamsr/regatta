package spawner

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

// fakeStarter records every starter call and returns a synthetic
// *exec.Cmd whose Process points at the test binary's own pid (so
// pidAlive returns true).
type fakeStarter struct {
	mu      sync.Mutex
	calls   []starterCall
	failNow atomic.Bool
}

type starterCall struct {
	name string
	args []string
	dir  string
	stdin string
}

func (f *fakeStarter) start(ctx context.Context, name string, args []string, stdin io.Reader, dir string) (*exec.Cmd, error) {
	if f.failNow.Load() {
		return nil, errors.New("synthetic start failure")
	}
	buf, _ := io.ReadAll(stdin)
	f.mu.Lock()
	f.calls = append(f.calls, starterCall{name: name, args: append([]string(nil), args...), dir: dir, stdin: string(buf)})
	f.mu.Unlock()

	// Use `true` (POSIX) as the surrogate process. It exits cleanly
	// so the test does not leak; CommandContext binds it to ctx.
	cmd := exec.CommandContext(ctx, "true")
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

func newClaudeHarness(t *testing.T) (*ClaudeSpawner, *fakeStarter, string) {
	t.Helper()
	dir := t.TempDir()
	fakeGit := &fakeRunner{}
	wm, err := NewWorktreeManager(WorktreeManagerConfig{RepoRoot: dir, Runner: fakeGit.run})
	if err != nil {
		t.Fatalf("wm: %v", err)
	}
	cs, err := NewClaudeSpawner(wm, ClaudeSpawnerConfig{})
	if err != nil {
		t.Fatalf("claude: %v", err)
	}
	fs := &fakeStarter{}
	cs.SetStarter(fs.start)
	return cs, fs, dir
}

func TestClaudeSpawnCreatesWorktreeAndStartsBinary(t *testing.T) {
	cs, fs, _ := newClaudeHarness(t)
	res, err := cs.Spawn(context.Background(), Request{AgentID: 1, WorkItemID: "WORK-1", Lane: "server"})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if res.PID <= 0 {
		t.Fatalf("expected positive pid, got %d", res.PID)
	}
	if res.SessionID == "" {
		t.Fatalf("expected session id, got empty")
	}
	if got := len(fs.calls); got != 1 {
		t.Fatalf("starter calls=%d, want 1", got)
	}
	if !filepath.IsAbs(fs.calls[0].dir) {
		t.Fatalf("starter dir not absolute: %s", fs.calls[0].dir)
	}
	if fs.calls[0].stdin == "" {
		t.Fatal("starter did not receive a prompt on stdin")
	}
}

func TestClaudeSpawnRollsBackWorktreeOnStartFailure(t *testing.T) {
	cs, fs, _ := newClaudeHarness(t)
	fs.failNow.Store(true)
	_, err := cs.Spawn(context.Background(), Request{AgentID: 1, WorkItemID: "WORK-1"})
	if err == nil {
		t.Fatal("expected spawn error")
	}
	if _, statErr := os.Stat(cs.wm.PathFor(1)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("worktree leaked after start failure: %v", statErr)
	}
}

func TestClaudeSpawnAssignsUniquePerAgentWorktrees(t *testing.T) {
	cs, _, _ := newClaudeHarness(t)
	a, err := cs.Spawn(context.Background(), Request{AgentID: 1})
	if err != nil {
		t.Fatalf("spawn 1: %v", err)
	}
	b, err := cs.Spawn(context.Background(), Request{AgentID: 2})
	if err != nil {
		t.Fatalf("spawn 2: %v", err)
	}
	if a.PID == b.PID {
		t.Fatalf("pids collided: %d", a.PID)
	}
	if cs.wm.PathFor(1) == cs.wm.PathFor(2) {
		t.Fatalf("worktree paths collided: %s", cs.wm.PathFor(1))
	}
}

func TestClaudeChildrenAndForget(t *testing.T) {
	cs, _, _ := newClaudeHarness(t)
	_, err := cs.Spawn(context.Background(), Request{AgentID: 7})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if _, ok := cs.Children()[7]; !ok {
		t.Fatal("child not recorded")
	}
	cs.Forget(7)
	if _, ok := cs.Children()[7]; ok {
		t.Fatal("Forget did not remove the child")
	}
}

// TestClaudeSpawnKillRace hammers Spawn and KillAgent across the
// same agent IDs from many goroutines. With -race the test fails if
// either method touches s.children without the mutex; functionally
// it asserts no goroutine panics and the final children map shape
// is internally consistent.
func TestClaudeSpawnKillRace(t *testing.T) {
	cs, _, _ := newClaudeHarness(t)

	const N = 32
	var wg sync.WaitGroup
	wg.Add(N * 2)
	for i := 0; i < N; i++ {
		id := int64(i + 1)
		go func() {
			defer wg.Done()
			_, _ = cs.Spawn(context.Background(), Request{AgentID: id})
		}()
		go func() {
			defer wg.Done()
			_, _ = cs.KillAgent(id)
		}()
	}
	wg.Wait()

	// Every recorded child must have a non-nil cmd; the map itself
	// must be safe to range.
	for id, cmd := range cs.Children() {
		if cmd == nil {
			t.Fatalf("agent %d has nil cmd in children map", id)
		}
	}
}

// opaqueWrap hides the wrapped error's message but preserves the
// errors.Is chain, so KillAgent must classify via the sentinel.
type opaqueWrap struct{ err error }

func (e *opaqueWrap) Error() string { return "kill failed: opaque" }
func (e *opaqueWrap) Unwrap() error { return e.err }

func TestKillAgent_AlreadyFinished_RecognizesErrProcessDone(t *testing.T) {
	cs, _, _ := newClaudeHarness(t)
	cs.killer = func(*exec.Cmd) error { return &opaqueWrap{err: os.ErrProcessDone} }
	cs.mu.Lock()
	cs.children[99] = &exec.Cmd{Process: &os.Process{Pid: 1}}
	cs.mu.Unlock()

	signaled, err := cs.KillAgent(99)
	if err != nil {
		t.Fatalf("KillAgent: %v", err)
	}
	if !signaled {
		t.Fatal("expected signaled=true for ErrProcessDone")
	}
}

func TestDefaultPromptBuilderCarriesIdentifiers(t *testing.T) {
	prompt := defaultPromptBuilder(Request{AgentID: 42, WorkItemID: "WORK-X", Lane: "server"})
	for _, want := range []string{"42", "WORK-X", "server"} {
		if !contains(prompt, want) {
			t.Fatalf("prompt missing %q: %q", want, prompt)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
