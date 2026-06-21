package spawner

import (
	"strings"
	"testing"
)

// TestDefaultPromptBuilderInjectsSafetyAllowlist asserts the resolved deny+allow lists render into the brief so an agent's force-with-lease on its own branch binds while force on main stays denied (MAY-97, MAY-258).
func TestDefaultPromptBuilderInjectsSafetyAllowlist(t *testing.T) {
	prompt := defaultPromptBuilder(Request{
		AgentID:                  3,
		WorkItemID:               "WORK-S",
		Lane:                     "server",
		DestructiveOpsDeny:       []string{"rm -rf /", "git push --force"},
		AgentDestructiveOpsAllow: []string{"git push --force-with-lease origin regatta/agent-"},
	})
	for _, want := range []string{
		"git push --force-with-lease origin regatta/agent-",
		"git push --force",
		"rm -rf /",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("brief missing concrete safety entry %q\n%s", want, prompt)
		}
	}
	permitted := "git push --force-with-lease origin regatta/agent-3"
	pi := strings.Index(prompt, permitted)
	if pi < 0 {
		t.Fatalf("brief does not render a permitted agent-branch force-with-lease example\n%s", prompt)
	}
	if di := strings.Index(prompt, "git push --force origin main"); di < 0 {
		t.Fatalf("brief does not render the denied force-on-main example\n%s", prompt)
	}
}

// TestDefaultPromptBuilderOmitsSafetySectionWhenUnset asserts no allowlist section renders when no lists are configured (config-absent default).
func TestDefaultPromptBuilderOmitsSafetySectionWhenUnset(t *testing.T) {
	prompt := defaultPromptBuilder(Request{AgentID: 1, WorkItemID: "WORK-N", Lane: "server"})
	if strings.Contains(prompt, "Destructive-op policy") {
		t.Fatalf("brief rendered safety section with no lists configured\n%s", prompt)
	}
}
