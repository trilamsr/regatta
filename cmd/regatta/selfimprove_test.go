package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

// TestSelfImproveScan_DryRunAcceptsSevenDays asserts the full --since=7d CLI invocation no longer errors.
func TestSelfImproveScan_DryRunAcceptsSevenDays(t *testing.T) {
	stdout, restore := captureStdoutToBuf(t)
	code := runSelfImproveScan([]string{"--since=7d"})
	restore()
	if code != 0 {
		t.Fatalf("runSelfImproveScan --since=7d exit=%d", code)
	}
	if !strings.Contains(stdout.String(), "dry-run scan") {
		t.Errorf("dry-run output missing; got %q", stdout.String())
	}
}

func captureStdoutToBuf(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	buf := &bytes.Buffer{}
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(buf, r)
		close(done)
	}()
	return buf, func() {
		_ = w.Close()
		<-done
		os.Stdout = orig
		_ = r.Close()
	}
}

// TestSelfImproveScan_NegativeSinceRejected asserts --since <= 0 exits non-zero (R15-Bug-2; mirror of R14 events tail fix).
func TestSelfImproveScan_NegativeSinceRejected(t *testing.T) {
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	code := runSelfImproveScan([]string{"--since", "-1h"})
	_ = w.Close()
	os.Stderr = old
	b := make([]byte, 4096)
	n, _ := r.Read(b)
	stderr := string(b[:n])
	if code == 0 {
		t.Fatalf("exit=0 want non-zero for --since=-1h; stderr=%q", stderr)
	}
	if !strings.Contains(stderr, "must be > 0") {
		t.Errorf("stderr missing > 0 hint: %q", stderr)
	}
}
