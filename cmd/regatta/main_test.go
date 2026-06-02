package main

import "testing"

// TestMain_DispatchTableCoversAllCommands locks the dispatch table: each
// entry has a non-empty name, a non-nil run func, and no duplicate
// names. Drops a class of regression where moving a run* into its own
// file deletes the table row but main still compiles.
func TestMain_DispatchTableCoversAllCommands(t *testing.T) {
	want := []string{
		"l0",
		"l0-refs",
		"l0-merge",
		subcmdVerifyRepoConfig,
		subcmdProgram,
		subcmdServe,
		subcmdValidateConfig,
		subcmdInit,
		subcmdApproval,
		subcmdKeys,
		subcmdDigest,
	}
	if len(subcommands) != len(want) {
		t.Fatalf("subcommand count: got %d want %d", len(subcommands), len(want))
	}
	seen := make(map[string]bool, len(subcommands))
	for i, sc := range subcommands {
		if sc.name == "" {
			t.Errorf("subcommands[%d]: empty name", i)
		}
		if sc.run == nil {
			t.Errorf("subcommands[%d] (%q): nil run", i, sc.name)
		}
		if seen[sc.name] {
			t.Errorf("subcommands[%d]: duplicate name %q", i, sc.name)
		}
		seen[sc.name] = true
	}
	for _, name := range want {
		if !seen[name] {
			t.Errorf("missing subcommand entry: %q", name)
		}
	}
}
