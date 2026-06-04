package main

import (
	"bytes"
	"os"
	"testing"
)

// TestServeFileSize asserts cmd/regatta/serve.go stays under the god-file threshold (audit Wave D).
func TestServeFileSize(t *testing.T) {
	const limit = 400
	data, err := os.ReadFile("serve.go")
	if err != nil {
		t.Fatalf("read serve.go: %v", err)
	}
	lines := bytes.Count(data, []byte{'\n'})
	if !bytes.HasSuffix(data, []byte{'\n'}) {
		lines++
	}
	if lines >= limit {
		t.Fatalf("cmd/regatta/serve.go = %d lines; want < %d. Extract subsystem wiring into wire_*.go siblings.", lines, limit)
	}
}
