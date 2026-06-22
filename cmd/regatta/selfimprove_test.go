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
