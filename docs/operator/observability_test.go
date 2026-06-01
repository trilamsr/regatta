// Package operator hosts gate tests for the operator-facing
// documentation under docs/operator/. Tests-only package — there is no
// production Go code here; the tests assert that the markdown files
// satisfy contracts that would otherwise be silent doc-drift bugs.
//
// Coverage: observability.md must document every OTel env var the
// W6 SDK setup reads, plus every relative link must resolve. Both
// gates fail loudly if the doc and the spec diverge.
package operator

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const observabilityDoc = "observability.md"

// observabilityEnvVars is the env-var contract from spec
// docs/engineer/specs/2026-05-31-mvp-3-w6-otel-backbone.md §3.6. Every
// entry must appear verbatim in observability.md or operators reading
// the doc will not know the knob exists. Adding a new env var to setup.go
// requires adding it here AND to the doc; the test fails loudly when
// either side drifts.
var observabilityEnvVars = []string{
	"OTEL_EXPORTER_OTLP_ENDPOINT",
	"OTEL_EXPORTER_OTLP_HEADERS",
	"OTEL_SERVICE_NAME",
	"OTEL_TRACES_SAMPLER",
	"OTEL_RESOURCE_ATTRIBUTES",
	"--otel-dev-stdout",
}

// TestObservabilityDoc_DocumentsAllEnvVars pins spec §6 T7 — every env
// var named in §3.6 appears in observability.md. Operator-doc completeness
// is a falsifiable contract; this is its gate.
func TestObservabilityDoc_DocumentsAllEnvVars(t *testing.T) {
	body := readDoc(t, observabilityDoc)
	for _, name := range observabilityEnvVars {
		if !strings.Contains(body, name) {
			t.Errorf("observability.md is missing env var %q (spec §3.6 requires it)", name)
		}
	}
}

// TestObservabilityDoc_LinksValid pins spec §6 T7 — every relative .md
// link in observability.md resolves to an on-disk file. External http(s)
// links and pure anchors are out of scope (the repo-wide doc-check.sh
// gate runs the same rule across all markdown; this test localises the
// invariant to the operator doc so a doc-only edit fails the package's
// own go-test gate without waiting for doc-check.sh).
func TestObservabilityDoc_LinksValid(t *testing.T) {
	docPath := docFullPath(t, observabilityDoc)
	body := readDoc(t, observabilityDoc)
	docDir := filepath.Dir(docPath)

	// Same regex shape as scripts/doc-check.sh: [text](target). We do
	// not strip fenced-code-block links here because operator doc is
	// prose; if a fenced link appears the test still catches drift.
	re := regexp.MustCompile(`\[[^\]]*\]\(([^)]+)\)`)
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		target := strings.TrimSpace(m[1])
		if target == "" {
			continue
		}
		if u, err := url.Parse(target); err == nil && u.Scheme != "" {
			continue
		}
		if strings.HasPrefix(target, "#") {
			continue
		}
		// Strip anchor; we resolve files, not fragments.
		if i := strings.Index(target, "#"); i >= 0 {
			target = target[:i]
		}
		if !strings.HasSuffix(target, ".md") {
			continue
		}
		resolved := filepath.Join(docDir, target)
		if _, err := os.Stat(resolved); err != nil {
			t.Errorf("dead link in observability.md: %q (resolved %s): %v", target, resolved, err)
		}
	}
}

// TestObservabilityDoc_NoSensitivePayloadKey pins spec §9 R7 — the doc
// must NOT teach operators how to enable gen_ai.{input,output}.messages.
// W6 keeps these attribute keys off by design; documenting an opt-in
// path here would invite the sensitive-payload regression the runtime
// gate (TestGenAI_SensitivePayloadNotEmitted) already blocks.
func TestObservabilityDoc_NoSensitivePayloadKey(t *testing.T) {
	body := readDoc(t, observabilityDoc)
	for _, forbidden := range []string{"gen_ai.input.messages", "gen_ai.output.messages"} {
		// The doc IS allowed to NAME these attrs while explaining why
		// they are off — what it must not do is teach an opt-in. We
		// flag any occurrence not adjacent to "off", "disabled", or
		// "not emitted" wording. Cheaper rule: forbid the bare attr
		// key unless the line also mentions "off" or "deferred" or
		// "not" — the doc's policy section uses one of those.
		for _, line := range strings.Split(body, "\n") {
			if !strings.Contains(line, forbidden) {
				continue
			}
			lower := strings.ToLower(line)
			if strings.Contains(lower, "off") ||
				strings.Contains(lower, "deferred") ||
				strings.Contains(lower, "not ") ||
				strings.Contains(lower, "never") ||
				strings.Contains(lower, "forbidden") {
				continue
			}
			t.Errorf("observability.md mentions %q without policy wording (R7 sensitive-payload regression)", forbidden)
		}
	}
}

func docFullPath(t *testing.T, name string) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Join(wd, name)
}

func readDoc(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(docFullPath(t, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}
