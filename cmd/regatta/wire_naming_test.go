package main

import (
	"os"
	"strings"
	"testing"
)

// TestServeWireNamingConvention asserts subsystem wiring files use the wire_ prefix (#738).
func TestServeWireNamingConvention(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read cmd/regatta: %v", err)
	}
	var offenders []string
	for _, e := range entries {
		n := e.Name()
		if !strings.HasPrefix(n, "serve_") || !strings.HasSuffix(n, ".go") {
			continue
		}
		if strings.HasSuffix(n, "_test.go") {
			continue
		}
		offenders = append(offenders, n)
	}
	if len(offenders) > 0 {
		t.Fatalf("subsystem wiring files must use wire_ prefix; offenders: %v", offenders)
	}
}
