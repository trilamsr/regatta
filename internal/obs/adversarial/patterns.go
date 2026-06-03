// Package adversarial rolls up reviewer-subagent findings into a
// bounded OTel counter so the operator surface (D-T1 dashboard) can
// trend severity, scope, and recurring pattern without the free-form
// finding text blowing the metric tag budget. Free-form text stays in
// the substrate event payload; this package emits only the bounded
// enum derived from it (spec §3, R2 mitigation).
package adversarial

import "strings"

// Severity is the closed enum that classifies a reviewer finding's
// blast radius. Adding a value requires updating AllSeverities and the
// AST-walk lint in TestAdversarial_SeverityScopeEnum_Closed.
type Severity string

// Scope is the closed enum that classifies what a reviewer finding
// applies to. Adding a value requires updating AllScopes and the
// AST-walk lint.
type Scope string

// Pattern is the closed enum of top-N recurring finding shapes. The
// final slot OtherPattern absorbs anything the registry has not yet
// learned to bucket (spec §3.2 + R9).
type Pattern string

// Severity values per spec §3.1 (5 levels). Critical/High/Medium/Low
// match the reviewer-subagent grade rubric; Nit captures hygiene-only
// findings that should not trigger alarms.
const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
	SeverityNit      Severity = "nit"
)

// Scope values per spec §3.1 (4 levels). The widening axis lets the
// dashboard distinguish a single-file lint hit from a session-wide
// review failure.
const (
	ScopeFile    Scope = "file"
	ScopePackage Scope = "package"
	ScopeRepo    Scope = "repo"
	ScopeSession Scope = "session"
)

// Pattern values per spec §3.2. The 16th slot OtherPattern is the
// catch-all bucket for unknown finding text — promoted into a named
// slot only after a quarterly review (spec §10 followup #1).
const (
	PatternMissingErrorWrap     Pattern = "missing_error_wrap"
	PatternNilMeterFallback     Pattern = "nil_meter_fallback_missing"
	PatternGodocOneLine         Pattern = "godoc_one_line_violation"
	PatternBannedPhrase         Pattern = "banned_phrase"
	PatternUnboundedTag         Pattern = "unbounded_tag"
	PatternCommentWhatNotWhy    Pattern = "comment_what_not_why"
	PatternReleaseNotesMissing  Pattern = "release_notes_missing"
	PatternAISignature          Pattern = "ai_signature"
	PatternTestGodocOverflow    Pattern = "test_godoc_overflow"
	PatternPRBodyHeredoc        Pattern = "pr_body_heredoc"
	PatternRootCauseSuppression Pattern = "root_cause_suppression"
	PatternDuplicateHelper      Pattern = "duplicate_helper"
	PatternUnsafeConcurrency    Pattern = "unsafe_concurrency"
	PatternMissingTestCoverage  Pattern = "missing_test_coverage"
	PatternSpecDeviation        Pattern = "spec_deviation"
	PatternOther                Pattern = "other"
)

// AllSeverities pins the closed severity enum so the AST-walk lint can
// reject string literals that drift outside the set.
var AllSeverities = []Severity{
	SeverityCritical, SeverityHigh, SeverityMedium, SeverityLow, SeverityNit,
}

// AllScopes pins the closed scope enum.
var AllScopes = []Scope{ScopeFile, ScopePackage, ScopeRepo, ScopeSession}

// AllPatterns pins the closed pattern enum. OtherPattern is the last
// element by convention so callers iterating for a UI legend render
// it last.
var AllPatterns = []Pattern{
	PatternMissingErrorWrap, PatternNilMeterFallback, PatternGodocOneLine,
	PatternBannedPhrase, PatternUnboundedTag, PatternCommentWhatNotWhy,
	PatternReleaseNotesMissing, PatternAISignature, PatternTestGodocOverflow,
	PatternPRBodyHeredoc, PatternRootCauseSuppression, PatternDuplicateHelper,
	PatternUnsafeConcurrency, PatternMissingTestCoverage, PatternSpecDeviation,
	PatternOther,
}

// patternKeywords maps substring tokens (lowercased) to a canonical
// pattern bucket. Order is significant — earlier entries win on
// overlap (e.g. "godoc" hits PatternGodocOneLine before PatternOther).
// New buckets are added here, not by callers, so the closed-enum
// invariant holds.
var patternKeywords = []struct {
	token  string
	bucket Pattern
}{
	{"missing error wrap", PatternMissingErrorWrap},
	{"errors.wrap", PatternMissingErrorWrap},
	{"%w", PatternMissingErrorWrap},
	{"nil meter", PatternNilMeterFallback},
	{"meter fallback", PatternNilMeterFallback},
	{"godoc", PatternGodocOneLine},
	{"one-line godoc", PatternGodocOneLine},
	{"banned phrase", PatternBannedPhrase},
	{"banned-phrase", PatternBannedPhrase},
	{"unbounded tag", PatternUnboundedTag},
	{"cardinality", PatternUnboundedTag},
	{"comment what", PatternCommentWhatNotWhy},
	{"why not what", PatternCommentWhatNotWhy},
	{"release-notes", PatternReleaseNotesMissing},
	{"release notes missing", PatternReleaseNotesMissing},
	{"ai signature", PatternAISignature},
	{"co-authored-by", PatternAISignature},
	{"test godoc", PatternTestGodocOverflow},
	{"multi-line test godoc", PatternTestGodocOverflow},
	{"heredoc", PatternPRBodyHeredoc},
	{"--body-file", PatternPRBodyHeredoc},
	{"root cause", PatternRootCauseSuppression},
	{"symptom suppression", PatternRootCauseSuppression},
	{"duplicate", PatternDuplicateHelper},
	{"copy-paste", PatternDuplicateHelper},
	{"race", PatternUnsafeConcurrency},
	{"data race", PatternUnsafeConcurrency},
	{"mutex", PatternUnsafeConcurrency},
	{"missing test", PatternMissingTestCoverage},
	{"no coverage", PatternMissingTestCoverage},
	{"spec deviation", PatternSpecDeviation},
	{"deviates from spec", PatternSpecDeviation},
}

// ClassifyPattern routes free-form reviewer-finding text into one of
// the closed Pattern enum buckets. Unknown text falls through to
// PatternOther so cardinality stays bounded even when the reviewer
// invents a new finding shape (spec §3.2 + R9).
func ClassifyPattern(findingText string) Pattern {
	needle := strings.ToLower(findingText)
	for _, kw := range patternKeywords {
		if strings.Contains(needle, kw.token) {
			return kw.bucket
		}
	}
	return PatternOther
}

// ValidSeverity reports whether s is in AllSeverities. Used by the
// AST-walk lint and the counter's pre-emit guard so a bad call site
// fails fast instead of polluting the time series.
func ValidSeverity(s Severity) bool {
	for _, v := range AllSeverities {
		if v == s {
			return true
		}
	}
	return false
}

// ValidScope reports whether s is in AllScopes.
func ValidScope(s Scope) bool {
	for _, v := range AllScopes {
		if v == s {
			return true
		}
	}
	return false
}

// ValidPattern reports whether p is in AllPatterns.
func ValidPattern(p Pattern) bool {
	for _, v := range AllPatterns {
		if v == p {
			return true
		}
	}
	return false
}
