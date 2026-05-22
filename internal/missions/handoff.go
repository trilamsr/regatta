// Handoff loading, verification, and coverage checks for the
// mission layer. The handoff.json schema is the only mission-
// specific inter-agent artifact (see docs/design.md §Missions).
//
// IMPORTANT: a worker's claims in Handoff.CommandsRun are
// ADVISORY. Callers MUST independently re-run each command in a
// sibling sandbox and compare (exit_code, stdout_sha256) before
// trusting any claim of success. See ReRunMismatch.

package missions

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"time"

	"github.com/trilamsr/regatta/schemas"
)

// Handoff is the Go form of schemas/handoff.schema.json.
// Field shape matches the JSON Schema 1:1; field tags use the same
// names. New fields require a schema bump in lockstep.
type Handoff struct {
	SchemaVersion       int                `json:"schema_version"`
	MissionID           string             `json:"mission_id"`
	FeatureID           string             `json:"feature_id"`
	WorkerRunID         string             `json:"worker_run_id"`
	WorkerModelID       string             `json:"worker_model_id,omitempty"`
	BaseSHA             string             `json:"base_sha"`
	HeadSHA             string             `json:"head_sha"`
	CommitSHAs          []string           `json:"commit_shas,omitempty"`
	SuccessState        string             `json:"success_state"`
	CriteriaAddressed   []string           `json:"criteria_addressed"`
	CommandsRun         []HandoffCommand   `json:"commands_run,omitempty"`
	TestsAdded          []HandoffTest      `json:"tests_added,omitempty"`
	Falsifications      []Falsification    `json:"falsifications,omitempty"`
	TrapPatternsClaimed []string           `json:"trap_patterns_claimed,omitempty"`
	DiscoveredIssues    []DiscoveredIssue  `json:"discovered_issues,omitempty"`
	UnfinishedWork      []string           `json:"unfinished_work,omitempty"`
	ReturnToOrchestrator bool              `json:"return_to_orchestrator,omitempty"`
	ProducedAt          time.Time          `json:"produced_at"`
	Signature           schemas.SignatureBlock `json:"signature"`
}

type HandoffCommand struct {
	Cmd        string `json:"cmd"`
	ExitCode   int    `json:"exit_code"`
	DurationMs int    `json:"duration_ms,omitempty"`
	StdoutSHA  string `json:"stdout_sha,omitempty"`
	LogPath    string `json:"log_path,omitempty"`
}

type HandoffTest struct {
	Name      string   `json:"name"`
	File      string   `json:"file"`
	Addresses []string `json:"addresses,omitempty"`
}

type Falsification struct {
	Hypothesis            string `json:"hypothesis"`
	MutationKind          string `json:"mutation_kind"`
	TargetInvariant       string `json:"target_invariant"`
	Citation              string `json:"citation"`
	CanaryArchetypeKilled string `json:"canary_archetype_killed,omitempty"`
	Outcome               string `json:"outcome"`
}

type DiscoveredIssue struct {
	Severity              string `json:"severity"`
	Summary               string `json:"summary"`
	SuggestsNewCriterion  bool   `json:"suggests_new_criterion,omitempty"`
}

// allowed enums kept in lockstep with handoff.schema.json.
var (
	missionIDRe   = regexp.MustCompile(`^m-[0-9a-f]{12}$`)
	shaRe         = regexp.MustCompile(`^[0-9a-f]{40}$`)
	citationRe    = regexp.MustCompile(`^(test|file|commit|run)=\S+$`)
	trapPatternRe = regexp.MustCompile(`^P([1-9]|1[0-3])$`)

	allowedSuccessStates  = map[string]bool{"success": true, "partial": true, "failure": true}
	allowedMutationKinds  = map[string]bool{
		"null": true, "empty": true, "boundary": true, "race": true, "inject": true,
		"overflow": true, "auth_bypass": true, "path_traversal": true, "ssrf": true,
		"deserialization": true, "regex_dos": true, "supply_chain": true, "other": true,
	}
	allowedOutcomes = map[string]bool{
		"disproved_worker": true, "confirmed_worker": true, "inconclusive": true,
	}
	allowedSeverities = map[string]bool{
		"info": true, "low": true, "medium": true, "high": true, "critical": true,
	}
)

// Errors callers should be able to match against.
var (
	ErrSchemaInvalid     = errors.New("handoff: schema invalid")
	ErrCoverageViolation = errors.New("handoff: criteria_addressed not a subset of feature acceptance criteria")
	ErrFalsificationMissingForPattern = errors.New("handoff: trap_pattern claimed without backing falsification")
)

// LoadAndValidate reads a handoff JSON file, structurally
// validates it against the schema's invariants, and returns the
// parsed Handoff. It does NOT verify the signature — that is a
// separate step that takes a keyring (see VerifySignature).
func LoadAndValidate(path string) (*Handoff, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("handoff: read %s: %w", path, err)
	}
	return ParseAndValidate(data)
}

// ParseAndValidate is the in-memory form of LoadAndValidate.
func ParseAndValidate(data []byte) (*Handoff, error) {
	var h Handoff
	dec := json.NewDecoder(bytesReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&h); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSchemaInvalid, err)
	}
	if err := h.validate(); err != nil {
		return nil, err
	}
	return &h, nil
}

func bytesReader(b []byte) *fixedReader { return &fixedReader{b: b} }

// fixedReader is a trivial io.Reader over a byte slice without
// pulling in bytes.NewReader (keeps the import surface tight).
type fixedReader struct {
	b []byte
	i int
}

func (r *fixedReader) Read(p []byte) (int, error) {
	if r.i >= len(r.b) {
		return 0, errEOF
	}
	n := copy(p, r.b[r.i:])
	r.i += n
	return n, nil
}

var errEOF = errors.New("EOF")

func (h *Handoff) validate() error {
	if h.SchemaVersion != 1 {
		return fmt.Errorf("%w: schema_version must be 1, got %d", ErrSchemaInvalid, h.SchemaVersion)
	}
	if !missionIDRe.MatchString(h.MissionID) {
		return fmt.Errorf("%w: mission_id %q does not match ^m-[0-9a-f]{12}$", ErrSchemaInvalid, h.MissionID)
	}
	if h.FeatureID == "" {
		return fmt.Errorf("%w: feature_id is required", ErrSchemaInvalid)
	}
	if !shaRe.MatchString(h.BaseSHA) {
		return fmt.Errorf("%w: base_sha %q is not a 40-char hex", ErrSchemaInvalid, h.BaseSHA)
	}
	if !shaRe.MatchString(h.HeadSHA) {
		return fmt.Errorf("%w: head_sha %q is not a 40-char hex", ErrSchemaInvalid, h.HeadSHA)
	}
	for _, c := range h.CommitSHAs {
		if !shaRe.MatchString(c) {
			return fmt.Errorf("%w: commit_shas contains non-SHA %q", ErrSchemaInvalid, c)
		}
	}
	if !allowedSuccessStates[h.SuccessState] {
		return fmt.Errorf("%w: success_state %q not in {success,partial,failure}", ErrSchemaInvalid, h.SuccessState)
	}
	if h.WorkerRunID == "" {
		return fmt.Errorf("%w: worker_run_id is required", ErrSchemaInvalid)
	}

	// Falsifications carry citation and enum discipline.
	for i, f := range h.Falsifications {
		if !allowedMutationKinds[f.MutationKind] {
			return fmt.Errorf("%w: falsifications[%d].mutation_kind %q invalid", ErrSchemaInvalid, i, f.MutationKind)
		}
		if !allowedOutcomes[f.Outcome] {
			return fmt.Errorf("%w: falsifications[%d].outcome %q invalid", ErrSchemaInvalid, i, f.Outcome)
		}
		if !citationRe.MatchString(f.Citation) {
			return fmt.Errorf("%w: falsifications[%d].citation %q must match ^(test|file|commit|run)=\\S+$", ErrSchemaInvalid, i, f.Citation)
		}
		if f.Hypothesis == "" || f.TargetInvariant == "" {
			return fmt.Errorf("%w: falsifications[%d] missing hypothesis or target_invariant", ErrSchemaInvalid, i)
		}
	}

	// Every claimed trap pattern must have at least one falsification
	// with a non-inconclusive outcome. Plain counts don't satisfy the
	// schema — the discipline is "citation OR it didn't happen."
	for _, p := range h.TrapPatternsClaimed {
		if !trapPatternRe.MatchString(p) {
			return fmt.Errorf("%w: trap_patterns_claimed contains invalid %q", ErrSchemaInvalid, p)
		}
		found := false
		for _, f := range h.Falsifications {
			if f.Outcome == "inconclusive" {
				continue
			}
			// Cheap heuristic: target_invariant or hypothesis mentions the pattern.
			// Strict mapping would require an explicit field; for v1 we settle for
			// "at least one non-inconclusive falsification exists per claim."
			found = true
			break
		}
		if !found {
			return fmt.Errorf("%w: pattern %s claimed but no backing non-inconclusive falsification", ErrFalsificationMissingForPattern, p)
		}
	}

	for _, d := range h.DiscoveredIssues {
		if !allowedSeverities[d.Severity] {
			return fmt.Errorf("%w: discovered_issues severity %q invalid", ErrSchemaInvalid, d.Severity)
		}
	}

	return nil
}

// VerifySignature checks the HMAC over the canonical payload. It
// re-serializes the handoff through a generic map to use the
// shared schemas.Verify implementation — the canonical form is the
// same one Sign produced.
func (h *Handoff) VerifySignature(keyring map[string][]byte) error {
	raw, err := json.Marshal(h)
	if err != nil {
		return err
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return err
	}
	return schemas.Verify(generic, keyring)
}

// CoverageCheck enforces that h.CriteriaAddressed ⊆ featureCriteria.
// The feature's acceptance-criteria IDs come from the parent
// WorkItem; the caller loads them from the spec adapter.
func (h *Handoff) CoverageCheck(featureCriteria []string) error {
	want := make(map[string]bool, len(featureCriteria))
	for _, c := range featureCriteria {
		want[c] = true
	}
	for _, c := range h.CriteriaAddressed {
		if !want[c] {
			return fmt.Errorf("%w: criteria_addressed has %q not in feature criteria", ErrCoverageViolation, c)
		}
	}
	return nil
}

// ReRunResult is the orchestrator's independent re-execution of one
// of the worker's claimed commands. The orchestrator (NOT the
// worker) populates this and HMAC-signs it. A mismatch with
// h.CommandsRun is a hard fail.
type ReRunResult struct {
	Cmd       string
	ExitCode  int
	StdoutSHA string
}

// ReRunMismatch returns the indices in h.CommandsRun whose
// (exit_code, stdout_sha) the orchestrator's independent re-runs
// did not reproduce. Empty slice = no mismatch.
//
// This is THE check that defeats incident #1 (Replit-class
// "agent self-reports green while real run is red").
func (h *Handoff) ReRunMismatch(orchestratorRuns []ReRunResult) []int {
	var bad []int
	if len(orchestratorRuns) != len(h.CommandsRun) {
		// Differing length is itself a mismatch — orchestrator ran
		// a different command set than the worker claimed.
		for i := range h.CommandsRun {
			bad = append(bad, i)
		}
		return bad
	}
	for i, claimed := range h.CommandsRun {
		actual := orchestratorRuns[i]
		if actual.Cmd != claimed.Cmd ||
			actual.ExitCode != claimed.ExitCode ||
			(claimed.StdoutSHA != "" && actual.StdoutSHA != claimed.StdoutSHA) {
			bad = append(bad, i)
		}
	}
	return bad
}
