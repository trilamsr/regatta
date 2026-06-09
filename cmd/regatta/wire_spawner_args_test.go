package main

import "testing"

// TestDefaultClaudeArgs asserts the headless flag set is non-empty so #1085's silent-stdout regression cannot return.
func TestDefaultClaudeArgs(t *testing.T) {
	args := defaultClaudeArgs()
	if len(args) == 0 {
		t.Fatalf("defaultClaudeArgs() returned empty; agents will spawn in TUI mode and emit no stdout (#1085)")
	}
	wants := map[string]bool{
		"--print":                       false,
		"--output-format=stream-json":   false,
		"--verbose":                     false,
	}
	for _, a := range args {
		if _, ok := wants[a]; ok {
			wants[a] = true
		}
	}
	for flag, present := range wants {
		if !present {
			t.Fatalf("defaultClaudeArgs() missing %q (needed for ParseStream + operator log visibility)", flag)
		}
	}
}
