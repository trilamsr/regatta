package spawner

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// moduleRoot walks up from this test's source file to the directory holding
// go.mod so the integration harness can invoke the real gate script regardless
// of the package's working directory.
func moduleRoot(t *testing.T) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(self)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found walking up from test source")
		}
		dir = parent
	}
}

// runReviewerVerdict runs the real gate against body on a load-bearing PR and
// returns its exit code so the harness can assert pass/fail end-to-end.
func runReviewerVerdict(t *testing.T, root, body string) int {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash unavailable")
	}
	gate := filepath.Join(root, "scripts", "check-reviewer-verdict.sh")
	if _, err := os.Stat(gate); err != nil {
		t.Skipf("gate script absent: %v", err)
	}
	f := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(f, []byte(body), 0o644); err != nil {
		t.Fatalf("write body: %v", err)
	}
	cmd := exec.Command("bash", gate, "--body-file", f, "--load-bearing")
	cmd.Dir = root
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode()
		}
		t.Fatalf("run gate: %v", err)
	}
	return 0
}

// reviewerTokenContractTaught reports whether prompt still teaches the strict
// single-token Reviewer-recommendation shape the gate enforces. A false here
// means the prompt drifted away from the gate contract, which would make the
// end-to-end assertions below vacuous.
func reviewerTokenContractTaught(prompt string) bool {
	return strings.Contains(prompt, "`Reviewer-recommendation:` MUST be ONE of `APPROVE` | `REVISE` | `BLOCK` ALONE on the line.") &&
		strings.Contains(prompt, "NEVER append justification")
}

// TestReviewerVerdictGateMatchesPromptBuilderContract asserts the real prompt builder and real verdict gate agree end-to-end on token shape (MAY-95).
func TestReviewerVerdictGateMatchesPromptBuilderContract(t *testing.T) {
	root := moduleRoot(t)
	prompt := defaultPromptBuilder(Request{AgentID: 9, WorkItemID: "WORK-GATE", Lane: "server"})
	if !reviewerTokenContractTaught(prompt) {
		t.Fatal("defaultPromptBuilder no longer teaches the strict Reviewer-recommendation token shape; gate contract drifted")
	}

	const footer = "Reviewer-agent-id: cavecrew-reviewer-gateharness\n"

	conforming := "## Summary\n\nChanges internal/orchestrator/scheduler/scheduler.go\n\n" +
		footer + "Reviewer-recommendation: APPROVE\n\n```release-notes\n[FEAT] thing\n```\n"
	if got := runReviewerVerdict(t, root, conforming); got != 0 {
		t.Fatalf("contract-conforming body rejected by gate: exit %d", got)
	}

	violating := "## Summary\n\nChanges internal/orchestrator/scheduler/scheduler.go\n\n" +
		footer + "Reviewer-recommendation: APPROVE (pending CI)\n\n```release-notes\n[FEAT] thing\n```\n"
	if got := runReviewerVerdict(t, root, violating); got == 0 {
		t.Fatal("contract-violating body (justification appended) accepted by gate; prompt teaches a shape the gate would reject")
	}
}
