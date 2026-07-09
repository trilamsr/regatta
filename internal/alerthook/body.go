package alerthook

import (
	"fmt"
	"strings"
)

// unsetMarker renders for any annotation the AlertManager payload
// omits. The literal "(unset)" surfaces in the issue body so an
// on-call reader immediately sees which fields the alert source
// failed to populate.
const unsetMarker = "(unset)"

// renderIssueBody composes the GH issue markdown body. The shape is
// fixed by PHASE-AUTONOMY-W1 acceptance criteria c3: alarm name, SLO
// threshold, current metric value, dashboard URL, and a regatta-replay
// reproduce command. Missing fields render as "(unset)" rather than
// blanks so a downstream on-call reader can immediately tell what
// AlertManager omitted.
func renderIssueBody(p Payload, a Alert) string {
	annot := a.Annotations
	if annot == nil {
		annot = map[string]string{}
	}
	common := p.CommonAnnotations
	if common == nil {
		common = map[string]string{}
	}
	pick := func(key string) string {
		if v := annot[key]; v != "" {
			return v
		}
		if v := common[key]; v != "" {
			return v
		}
		return unsetMarker
	}

	var b strings.Builder
	fmt.Fprintf(&b, "## Alert: %s\n\n", a.Alertname())
	fmt.Fprintf(&b, "- Severity: `%s`\n", a.Severity())
	fmt.Fprintf(&b, "- Status: `%s`\n", a.Status)
	fmt.Fprintf(&b, "- Started: `%s`\n\n", a.StartsAt)

	fmt.Fprintf(&b, "### Summary\n\n%s\n\n", pick("summary"))
	fmt.Fprintf(&b, "### Description\n\n%s\n\n", pick("description"))

	fmt.Fprintf(&b, "### SLO breach\n\n")
	fmt.Fprintf(&b, "- Threshold: `%s`\n", pick("threshold"))
	fmt.Fprintf(&b, "- Current value: `%s`\n\n", pick("current_value"))

	fmt.Fprintf(&b, "### Dashboard\n\n%s\n\n", pick("dashboard_url"))

	fmt.Fprintf(&b, "### Replay command\n\n```sh\n%s\n```\n\n", pick("replay_command"))

	if runbook := pick("runbook_url"); runbook != unsetMarker {
		fmt.Fprintf(&b, "### Runbook\n\n%s\n\n", runbook)
	}

	fmt.Fprintf(&b, "### Source\n\n- Generator: %s\n- AlertManager: %s\n",
		nonEmpty(a.GeneratorURL, unsetMarker),
		nonEmpty(p.ExternalURL, unsetMarker),
	)
	return b.String()
}

func nonEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// renderCommentBody is the dedup-path counterpart: shorter, no header
// duplication, but every load-bearing field still surfaces so the
// comment-thread reader does not have to scroll back to the parent
// issue to see what the new firing changed.
func renderCommentBody(p Payload, a Alert) string {
	annot := a.Annotations
	if annot == nil {
		annot = map[string]string{}
	}
	common := p.CommonAnnotations
	if common == nil {
		common = map[string]string{}
	}
	pick := func(key string) string {
		if v := annot[key]; v != "" {
			return v
		}
		if v := common[key]; v != "" {
			return v
		}
		return unsetMarker
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Refiring at `%s` — status `%s`.\n\n", a.StartsAt, a.Status)
	fmt.Fprintf(&b, "- Current value: `%s` (threshold `%s`)\n", pick("current_value"), pick("threshold"))
	fmt.Fprintf(&b, "- Dashboard: %s\n", pick("dashboard_url"))
	fmt.Fprintf(&b, "- Replay: `%s`\n", pick("replay_command"))
	return b.String()
}
