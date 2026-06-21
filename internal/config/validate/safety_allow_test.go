package validate

import "testing"

// TestSafetyAllowOverridesDenyForAgentBranch asserts agent-branch force-with-lease is allowed while force-push on main stays denied (MAY-97).
func TestSafetyAllowOverridesDenyForAgentBranch(t *testing.T) {
	s := &Safety{
		DestructiveOpsDeny:       []string{"rm -rf /", "git push --force"},
		AgentDestructiveOpsAllow: []string{"git push --force-with-lease origin regatta/agent-"},
	}
	cases := []struct {
		name string
		cmd  string
		want bool // true ⇒ allowed
	}{
		{"force-with-lease on agent branch allowed", "git push --force-with-lease origin regatta/agent-3", true},
		{"force-push on main denied", "git push --force origin main", false},
		{"plain rm -rf denied", "rm -rf /", false},
		{"unrelated op allowed", "git status", true},
		{"force-with-lease on main not allowlisted, still denied", "git push --force-with-lease origin main", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := s.IsDestructiveOpAllowed(tc.cmd); got != tc.want {
				t.Fatalf("IsDestructiveOpAllowed(%q) = %v, want %v", tc.cmd, got, tc.want)
			}
		})
	}
}

// TestSafetyAllowDefaultsFromSchema asserts the CUE default allowlist surfaces in the decoded Config so agent-branch force-with-lease works out of the box (MAY-97).
func TestSafetyAllowDefaultsFromSchema(t *testing.T) {
	cfg, err := LoadConfig([]byte(minimalValid))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Safety == nil {
		t.Fatal("Safety nil")
	}
	if !cfg.Safety.IsDestructiveOpAllowed("git push --force-with-lease origin regatta/agent-7") {
		t.Fatalf("default allowlist did not permit agent-branch force-with-lease; allow=%v deny=%v",
			cfg.Safety.AgentDestructiveOpsAllow, cfg.Safety.DestructiveOpsDeny)
	}
}

// TestSafetyNilReceiverAllows asserts a nil Safety never blocks (config-absent default-permit).
func TestSafetyNilReceiverAllows(t *testing.T) {
	var s *Safety
	if !s.IsDestructiveOpAllowed("git push --force origin main") {
		t.Fatal("nil Safety should permit (no deny list configured)")
	}
}
