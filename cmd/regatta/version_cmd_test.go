package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"testing"
)

// TestVersion_JSONFlag asserts `regatta version --json` emits a parseable JSON envelope with version/commit/date fields (R-MEGA-3 LIVE-16).
func TestVersion_JSONFlag(t *testing.T) {
	out := captureStdoutVersion(t, func() {
		emitVersion([]string{"--json"})
	})
	var rep versionReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("emit was not JSON: %v; output=%q", err, out)
	}
	if rep.Version == "" {
		t.Errorf("version field empty: %+v", rep)
	}
}

// TestVersion_TextDefault asserts the bare `regatta version` invocation keeps the legacy text form.
func TestVersion_TextDefault(t *testing.T) {
	out := captureStdoutVersion(t, func() {
		emitVersion(nil)
	})
	if len(out) == 0 || out[0] == '{' {
		t.Errorf("default emit should be text not JSON; got %q", out)
	}
}

func captureStdoutVersion(t *testing.T, fn func()) string {
	t.Helper()
	r, w, _ := os.Pipe()
	saved := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = saved }()

	done := make(chan struct{})
	var buf bytes.Buffer
	go func() {
		_, _ = io.Copy(&buf, r)
		close(done)
	}()
	fn()
	_ = w.Close()
	<-done
	return buf.String()
}
