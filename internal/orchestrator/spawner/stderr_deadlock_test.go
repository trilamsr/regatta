package spawner

import (
	"context"
	"io"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"
)

// TestExecStarter_ChildExitsWhenStderrSinkBlocks asserts execStarter decouples the child from os.Stderr backpressure — a child that writes ~256KB to stderr must exit even when the ultimate *os.File stderr sink has a wedged reader (#1361, R-MEGA-2 C7).
func TestExecStarter_ChildExitsWhenStderrSinkBlocks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh-based stderr burst test not portable to windows")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh on PATH")
	}

	// A pipe whose read end we never drain — its ~64KB kernel buffer
	// fills after the first burst, wedging any write on the write end.
	// Direct FD inheritance (cmd.Stderr = pipeWriter) forwards this
	// wedge into the child process; a pipe+goroutine seam in
	// execStarter should absorb it.
	pipeR, pipeW, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = pipeR.Close()
		_ = pipeW.Close()
	})

	orig := execStarterStderr
	execStarterStderr = pipeW
	t.Cleanup(func() { execStarterStderr = orig })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Emit ~256KB to stderr: exceeds a typical 64KB pipe kernel
	// buffer several times over so a drain goroutine reading from
	// StderrPipe absorbs the burst by itself (all bytes discarded to
	// the wedged sink at write time, but the child's write side stays
	// open because our goroutine keeps read-draining).
	cmd, err := execStarter(ctx, "/bin/sh", []string{"-c", "yes stderrfill | head -c 262144 1>&2"}, nil, io.Discard, t.TempDir())
	if err != nil {
		t.Fatalf("execStarter: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case werr := <-done:
		if werr != nil {
			t.Fatalf("child exited with error: %v", werr)
		}
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		<-done
		t.Fatal("child deadlocked on stderr write — execStarter did not decouple child from blocked os.Stderr sink (#1361)")
	}
}
