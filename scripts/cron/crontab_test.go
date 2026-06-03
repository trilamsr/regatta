// Package cron carries one regression test for the shipped crontab —
// the self-improve scan entry MUST default to dry-run (no --apply) so
// a first-deploy run never spam-files GH issues from a noisy ruleset
// (#646). The bare `regatta self-improve scan` invocation prints
// findings without writing GH.
package cron

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestCronEntry_NoApplyFlagDefault asserts the shipped crontab self-improve line never carries --apply (#646).
func TestCronEntry_NoApplyFlagDefault(t *testing.T) {
	raw, err := os.ReadFile("regatta.crontab")
	if err != nil {
		t.Fatalf("read regatta.crontab: %v", err)
	}
	// Look for the active (non-comment) self-improve scan line.
	re := regexp.MustCompile(`(?m)^[^#].*regatta self-improve scan.*$`)
	matches := re.FindAllString(string(raw), -1)
	if len(matches) == 0 {
		t.Fatal("crontab missing self-improve scan entry")
	}
	for _, line := range matches {
		if strings.Contains(line, "--apply") {
			t.Fatalf("crontab self-improve entry must default to dry-run; found --apply in: %q", line)
		}
	}
}
