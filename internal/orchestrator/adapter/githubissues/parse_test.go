package githubissues

import "testing"

// TestParseIssueBody_DefaultLaneAppliedWhenNoLabel pins #1117: when the issue body has no `lane:` metadata and the adapter is configured with a default lane, parseIssueBody surfaces that lane instead of leaving Lane empty.
func TestParseIssueBody_DefaultLaneAppliedWhenNoLabel(t *testing.T) {
	body := "BUG repro\n\n## Acceptance criteria\n- [planned] c1: x\n"
	p, reason, err := parseIssueBody(body, "server")
	if err != nil {
		t.Fatalf("parse: %v (reason=%s)", err, reason)
	}
	if reason != "" {
		t.Fatalf("unexpected skip reason: %s", reason)
	}
	if p.Lane != "server" {
		t.Fatalf("Lane = %q, want %q", p.Lane, "server")
	}
}

// TestParseIssueBody_EmptyLaneStillSkipsWhenDefaultUnset pins #1117 negative: when no body lane AND no default lane configured, Lane stays empty so adaptersync still emits the existing empty_lane WARN.
func TestParseIssueBody_EmptyLaneStillSkipsWhenDefaultUnset(t *testing.T) {
	body := "BUG repro\n\n## Acceptance criteria\n- [planned] c1: x\n"
	p, reason, err := parseIssueBody(body, "")
	if err != nil {
		t.Fatalf("parse: %v (reason=%s)", err, reason)
	}
	if reason != "" {
		t.Fatalf("unexpected skip reason: %s", reason)
	}
	if p.Lane != "" {
		t.Fatalf("Lane = %q, want empty", p.Lane)
	}
}

// TestParseIssueBody_ExplicitLaneWinsOverDefault pins #1117 precedence: an explicit lane in the body metadata block is preserved even when a default is supplied.
func TestParseIssueBody_ExplicitLaneWinsOverDefault(t *testing.T) {
	body := "<!--regatta\nlane: client\n-->\n\n## Acceptance criteria\n- [planned] c1: x\n"
	p, reason, err := parseIssueBody(body, "server")
	if err != nil {
		t.Fatalf("parse: %v (reason=%s)", err, reason)
	}
	if reason != "" {
		t.Fatalf("unexpected skip reason: %s", reason)
	}
	if p.Lane != "client" {
		t.Fatalf("Lane = %q, want %q", p.Lane, "client")
	}
}
