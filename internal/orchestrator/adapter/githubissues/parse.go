// Package githubissues implements the schemas.SpecAdapter contract over
// GitHub Issues labelled `autonomous`. Per MVR-1-T4 spec the adapter
// consumes one discriminator label, parses an HTML-comment YAML metadata
// block plus a `## Acceptance criteria` H2 section, and dedups via a
// `<!-- regatta-dedup-key: <hex> -->` body marker. Projection is
// deterministic; LLM inference is forbidden on this path (spec §6.2).
package githubissues

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"
	"gopkg.in/yaml.v3"

	"github.com/trilamsr/regatta/contracts/schemas"
)

// SkipReason enumerates the closed set of projection-skip causes per
// spec §7.6; written into the WARN payload so operator-console can route.
type SkipReason string

// SkipReason values are the spec §7.6 closed enum.
const (
	ReasonBadIDPrefix          SkipReason = "bad_id_prefix"
	ReasonDupIDPrefix          SkipReason = "dup_id_prefix"
	ReasonDupAcceptanceSection SkipReason = "dup_acceptance_section"
	ReasonBadMetadataYAML      SkipReason = "bad_metadata_yaml"
	ReasonBackfillFailed       SkipReason = "body_marker_backfill_failed"
)

// AcceptanceHeading is the canonical H2 the parser anchors on; matches
// markdown_catalog so operators port templates without rewriting.
const AcceptanceHeading = "## Acceptance criteria"

// AutonomousLabel is the single discriminator the adapter consumes; the
// existing alarmwebhook + (post-F4) selfimprove producers tag with it.
const AutonomousLabel = "autonomous"

// DedupMarkerPrefix is the body line the adapter back-fills on first
// sighting (spec §4.1); same convention as selfimprove dedup but
// HTML-comment-wrapped so GH renders invisibly.
const DedupMarkerPrefix = "<!-- regatta-dedup-key: "

// DedupMarkerSuffix closes the body marker.
const DedupMarkerSuffix = " -->"

var (
	idPrefixRE      = regexp.MustCompile(`^([A-Z][A-Z0-9_-]{1,40}):\s`)
	criterionRE     = regexp.MustCompile(`^\s*-\s*\[(planned|in_progress|done|closed)\]\s+([^\s:]+):\s*(.+?)\s*$`)
	criteriaHeadRE  = regexp.MustCompile(`(?i)^##\s+acceptance\s+criteria\s*$`)
	dedupMarkerLine = regexp.MustCompile(`(?m)^\s*<!--\s*regatta-dedup-key:\s*([0-9a-fA-F]+)\s*-->\s*$`)
	metadataBlockRE = regexp.MustCompile(`(?s)<!--regatta\s*\n(.*?)\n\s*-->`)
)

// projection is the per-issue extraction result; callers convert it into
// a schemas.WorkItem at adapter boundary.
type projection struct {
	ID                 string
	Title              string
	Body               string
	Lane               string
	Dependencies       []string
	LinkedArtifact     string
	AcceptanceCriteria []schemas.Criterion
	DedupKey           string
}

// parseIssueBody normalizes body bytes (CRLF→LF + NFC), extracts the
// metadata block, dedup marker, and acceptance criteria. Returns a
// SkipReason when projection cannot proceed; callers WARN-log the
// payload per spec §7.6.
func parseIssueBody(rawBody string) (projection, SkipReason, error) {
	body := normalize(rawBody)
	var p projection

	// Metadata block — optional. Reject malformed YAML loud.
	if loc := metadataBlockRE.FindStringSubmatchIndex(body); loc != nil {
		raw := body[loc[2]:loc[3]]
		var md map[string]string
		if err := yaml.Unmarshal([]byte(raw), &md); err != nil {
			return projection{}, ReasonBadMetadataYAML, fmt.Errorf("metadata yaml: %w", err)
		}
		p.Lane = md["lane"]
		p.LinkedArtifact = md["linked_artifact"]
		if deps := md["dependencies"]; deps != "" {
			for _, d := range strings.FieldsFunc(deps, func(r rune) bool { return r == ',' || r == ' ' }) {
				if d = strings.TrimSpace(d); d != "" {
					p.Dependencies = append(p.Dependencies, d)
				}
			}
		}
	}

	// Dedup marker — optional; back-fill responsibility is the adapter's.
	if m := dedupMarkerLine.FindStringSubmatch(body); m != nil {
		p.DedupKey = m[1]
	}

	// Acceptance section — two H2s is ambiguous, fail closed.
	criteria, reason, err := parseCriteria(body)
	if err != nil {
		return projection{}, reason, err
	}
	p.AcceptanceCriteria = criteria

	p.Body = stripDedupMarker(stripAcceptanceSection(body))
	return p, "", nil
}

// parseCriteria extracts `## Acceptance criteria` bullets; returns
// dup_acceptance_section when more than one H2 collides. Empty section
// is NOT an error — spec §2.3 last row (soft-fail).
func parseCriteria(body string) ([]schemas.Criterion, SkipReason, error) {
	lines := strings.Split(body, "\n")
	seenHeading := 0
	inSection := false
	seenIDs := map[string]bool{}
	var out []schemas.Criterion
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if criteriaHeadRE.MatchString(line) {
			seenHeading++
			inSection = true
			continue
		}
		if inSection && strings.HasPrefix(strings.TrimSpace(line), "## ") {
			inSection = false
		}
		if !inSection {
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		m := criterionRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		id := m[2]
		if seenIDs[id] {
			continue
		}
		seenIDs[id] = true
		out = append(out, schemas.Criterion{
			ID:    id,
			Text:  m[3],
			State: schemas.CriterionState(m[1]),
		})
	}
	if seenHeading > 1 {
		return nil, ReasonDupAcceptanceSection, fmt.Errorf("two ## Acceptance criteria sections")
	}
	return out, "", nil
}

// extractIDFromTitle returns the leading `^[A-Z][A-Z0-9_-]{1,40}:` prefix
// and the title with prefix stripped; ok=false on observability-shaped
// titles (e.g. "[self-improve] ..."), per spec §2.4.
func extractIDFromTitle(title string) (id, stripped string, ok bool) {
	m := idPrefixRE.FindStringSubmatchIndex(title)
	if m == nil {
		return "", "", false
	}
	id = title[m[2]:m[3]]
	stripped = strings.TrimSpace(title[m[1]:])
	return id, stripped, true
}

// computeDedupKey is `sha256_hex(<owner>/<repo>:<number>:<body_sha>)`
// where body_sha hashes the normalized body with the dedup-marker line
// elided so the back-fill write does not invalidate the key.
func computeDedupKey(owner, repo string, number int, body string) string {
	stripped := stripDedupMarker(normalize(body))
	bodySum := sha256.Sum256([]byte(stripped))
	composite := fmt.Sprintf("%s/%s:%d:%s", owner, repo, number, hex.EncodeToString(bodySum[:]))
	full := sha256.Sum256([]byte(composite))
	return hex.EncodeToString(full[:])
}

// bodySourceSHA is the SourceRef.SHA used by L0 to anchor criterion-text
// immutability across the adapter boundary.
func bodySourceSHA(body string) string {
	stripped := stripDedupMarker(normalize(body))
	sum := sha256.Sum256([]byte(stripped))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func normalize(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return norm.NFC.String(s)
}

func stripDedupMarker(body string) string {
	return dedupMarkerLine.ReplaceAllString(body, "")
}

func stripAcceptanceSection(body string) string {
	lines := strings.Split(body, "\n")
	var out []string
	inSection := false
	for _, line := range lines {
		if criteriaHeadRE.MatchString(strings.TrimRight(line, "\r")) {
			inSection = true
			continue
		}
		if inSection && strings.HasPrefix(strings.TrimSpace(line), "## ") {
			inSection = false
		}
		if inSection {
			continue
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// withBackfilledMarker appends the dedup-key marker to a body that lacks
// one; the call site fires gh issue edit on the result. Idempotent: a
// body that already carries the marker round-trips unchanged.
func withBackfilledMarker(body, key string) string {
	if dedupMarkerLine.MatchString(body) {
		return body
	}
	tail := DedupMarkerPrefix + key + DedupMarkerSuffix
	if strings.HasSuffix(body, "\n") {
		return body + tail + "\n"
	}
	return body + "\n\n" + tail + "\n"
}
