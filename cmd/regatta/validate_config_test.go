package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

// TestValidateConfig_RejectsPositionalArg asserts a stray positional arg fails loudly rather than silently passing the default file.
func TestValidateConfig_RejectsPositionalArg(t *testing.T) {
	stderr, restore := captureStderr(t)
	defer restore()

	code := runValidateConfig([]string{"/tmp/some-nonexistent-yaml-the-operator-wanted-to-pass.yaml"})

	if code == 0 {
		t.Fatalf("runValidateConfig with stray positional exit = 0; want non-zero (operator-supplied path silently dropped + default validated)")
	}
	if !strings.Contains(stderr.String(), "positional") {
		t.Errorf("stderr should name the positional drop; got %q", stderr.String())
	}
}

func captureStderr(t *testing.T) (stderr *bytes.Buffer, restore func()) {
	t.Helper()
	stderr = &bytes.Buffer{}
	origStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(stderr, r)
		close(done)
	}()
	restore = func() {
		_ = w.Close()
		<-done
		os.Stderr = origStderr
	}
	return stderr, restore
}
