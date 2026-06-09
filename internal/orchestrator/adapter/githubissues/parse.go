// Package githubissues implements schemas.SpecAdapter over GitHub Issues labelled `autonomous` (MVR-1-T4); deterministic projection, LLM inference forbidden (spec §6.2).
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

// SkipReason is the spec §7.6 closed-enum projection-skip cause; values below are the WARN payload tokens.
type SkipReason string

// ReasonBadIDPrefix and siblings are the spec §7.6 SkipReason values.
const (
	ReasonBadIDPrefix          SkipReason = "bad_id_prefix"
	ReasonDupIDPrefix          SkipReason = "dup_id_prefix"
	ReasonDupAcceptanceSection SkipReason = "dup_acceptance_section"
	ReasonBadMetadataYAML      SkipReason = "bad_metadata_yaml"
	ReasonBackfillFailed       SkipReason = "body_marker_backfill_failed"
)

// AcceptanceHeading, AutonomousLabel, and the DedupMarker{Prefix,Suffix} pin the adapter's spec §2.3/§4.1 anchors.
const (
	AcceptanceHeading = "## Acceptance criteria"
	AutonomousLabel   = "autonomous"
	DedupMarkerPrefix = "<!-- regatta-dedup-key: "
	DedupMarkerSuffix = " -->"
)

var (
	idPrefixRE      = regexp.MustCompile(`^([A-Z][A-Z0-9_-]{1,40}):\s`)
	criterionRE     = regexp.MustCompile(`^\s*-\s*\[(planned|in_progress|done|closed)\]\s+([^\s:]+):\s*(.+?)\s*$`)
	criteriaHeadRE  = regexp.MustCompile(`(?i)^##\s+acceptance\s+criteria\s*$`)
	dedupMarkerLine = regexp.MustCompile(`(?m)^\s*<!--\s*regatta-dedup-key:\s*([0-9a-fA-F]+)\s*-->\s*$`)
	metadataBlockRE = regexp.MustCompile(`(?s)<!--regatta\s*\n(.*?)\n\s*-->`)
)

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

// parseIssueBody normalizes body bytes (CRLF→LF + NFC) and extracts metadata, dedup marker, and acceptance criteria; SkipReason on projection failure (spec §7.6). When the body has no `lane:` metadata, defaultLane (when non-empty) backfills Lane so adaptersync stops emitting `empty_lane` WARN on operator-filed issues (#1117, mirrors the scheduler default added in #1048).
func parseIssueBody(rawBody, defaultLane string) (projection, SkipReason, error) {
	body := normalize(rawBody)
	var p projection

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
	if p.Lane == "" && defaultLane != "" {
		p.Lane = defaultLane
	}

	if m := dedupMarkerLine.FindStringSubmatch(body); m != nil {
		p.DedupKey = m[1]
	}

	criteria, reason, err := parseCriteria(body)
	if err != nil {
		return projection{}, reason, err
	}
	p.AcceptanceCriteria = criteria

	p.Body = stripDedupMarker(stripAcceptanceSection(body))
	return p, "", nil
}

// parseCriteria extracts `## Acceptance criteria` bullets; dup H2 ⇒ dup_acceptance_section, empty section soft-fails (spec §2.3).
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

// extractIDFromTitle returns the leading `^[A-Z][A-Z0-9_-]{1,40}:` prefix + stripped title (spec §2.4); ok=false on observability-shaped titles.
func extractIDFromTitle(title string) (id, stripped string, ok bool) {
	m := idPrefixRE.FindStringSubmatchIndex(title)
	if m == nil {
		return "", "", false
	}
	id = title[m[2]:m[3]]
	stripped = strings.TrimSpace(title[m[1]:])
	return id, stripped, true
}

// computeDedupKey is sha256_hex(`<owner>/<repo>:<number>:<body_sha>`); body_sha elides the dedup-marker line so back-fill stays idempotent.
func computeDedupKey(owner, repo string, number int, body string) string {
	stripped := stripDedupMarker(normalize(body))
	bodySum := sha256.Sum256([]byte(stripped))
	composite := fmt.Sprintf("%s/%s:%d:%s", owner, repo, number, hex.EncodeToString(bodySum[:]))
	full := sha256.Sum256([]byte(composite))
	return hex.EncodeToString(full[:])
}

// bodySourceSHA is the SourceRef.SHA L0 uses to anchor criterion-text immutability across the adapter boundary.
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

// withBackfilledMarker appends the dedup-key marker to body; idempotent on already-marked bodies.
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
