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
	stdout, stderr, restore := captureOutput(t)
	defer restore()
	code := runSelfImproveScan([]string{"--since=7d"})
	_ = stderr
	if code != 0 {
		t.Fatalf("runSelfImproveScan --since=7d exit=%d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "dry-run scan") {
		t.Errorf("dry-run output missing; got %q", stdout.String())
	}
}

func captureOutput(t *testing.T) (stdout, stderr *bytes.Buffer, restore func()) {
	t.Helper()
	stdout = &bytes.Buffer{}
	stderr = &bytes.Buffer{}
	origStdout, origStderr := os.Stdout, os.Stderr
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout, os.Stderr = wOut, wErr
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(stdout, rOut)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(stderr, rErr)
		done <- struct{}{}
	}()
	restore = func() {
		_ = wOut.Close()
		_ = wErr.Close()
		<-done
		<-done
		os.Stdout, os.Stderr = origStdout, origStderr
	}
	return stdout, stderr, restore
}
