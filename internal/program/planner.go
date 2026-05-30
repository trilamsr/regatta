// Planner is the one-shot decomposition step for a Program.
//
// The planner is NOT a long-lived agent session. It is exactly one
// model call: read a parent WorkItem (kind: program), emit a
// ProgramBrief (child WorkItems with fulfills coverage), validate the
// coverage invariant, sign, return. There is no planner state to
// recover from on crash; if the call fails, re-run from scratch.
//
// See docs/design.md §Programs §Planner contract.

package programs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/trilamsr/regatta/contracts/schemas"
)

// ProgramBrief is the Go form of schemas/features.schema.json.
type ProgramBrief struct {
	SchemaVersion    int                  `json:"schema_version"`
	ProgramID        string               `json:"program_id"`
	ParentWorkItemID string               `json:"parent_work_item_id"`
	ParentCriteria   []PlanCriterion      `json:"parent_criteria"`
	PlannerModelID   string               `json:"planner_model_id"`
	Features         []PlannedFeature     `json:"features"`
	ProducedAt       time.Time            `json:"produced_at"`
	Signature        schemas.SignatureBlock `json:"signature"`
}

// PlanCriterion is one verbatim copy of the parent WorkItem's
// acceptance criterion at plan-time.
type PlanCriterion struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// PlannedFeature is one child node in the planner's emitted DAG.
type PlannedFeature struct {
	ID                  string   `json:"id"`
	Title               string   `json:"title"`
	Description         string   `json:"description,omitempty"`
	Fulfills            []string `json:"fulfills"`
	DependsOnFeatures   []string `json:"depends_on_features"`
	EstimatedComplexity string   `json:"estimated_complexity,omitempty"`
}

// Sentinel errors and validation regexps for ProgramBrief.
var (
	featureIDRe         = regexp.MustCompile(`^F-[A-Z0-9_-]{1,32}$`)
	allowedComplexities = map[string]bool{
		"": true, "trivial": true, "small": true, "medium": true, "large": true,
	}

	ErrCoverageIncomplete    = errors.New("planner: parent criteria not fully covered by features.fulfills")
	ErrCoverageOverlap       = errors.New("planner: features.fulfills overlap; criterion claimed by multiple features")
	ErrCoveragePhantom       = errors.New("planner: features.fulfills contains criterion not in parent")
	ErrFeatureCycle          = errors.New("planner: feature depends_on_features contains a cycle")
	ErrFeatureUnknownDep     = errors.New("planner: feature depends_on_features references unknown feature")
	ErrPlanSchemaInvalid     = errors.New("planner: feature plan schema invalid")
)

// Validate runs every invariant on a freshly-produced ProgramBrief
// EXCEPT signature verification (which is a separate step that
// needs the keyring). Run this before signing AND after loading.
func (p *ProgramBrief) Validate() error {
	if p.SchemaVersion != 1 {
		return fmt.Errorf("%w: schema_version must be 1, got %d", ErrPlanSchemaInvalid, p.SchemaVersion)
	}
	if !programIDRe.MatchString(p.ProgramID) {
		return fmt.Errorf("%w: bad program_id %q", ErrPlanSchemaInvalid, p.ProgramID)
	}
	if p.ParentWorkItemID == "" {
		return fmt.Errorf("%w: parent_work_item_id required", ErrPlanSchemaInvalid)
	}
	if len(p.ParentCriteria) == 0 {
		return fmt.Errorf("%w: parent_criteria must not be empty", ErrPlanSchemaInvalid)
	}
	if len(p.Features) == 0 {
		return fmt.Errorf("%w: features must not be empty", ErrPlanSchemaInvalid)
	}

	// Feature IDs unique, regex-shape.
	seenFeat := map[string]bool{}
	for _, f := range p.Features {
		if !featureIDRe.MatchString(f.ID) {
			return fmt.Errorf("%w: bad feature id %q", ErrPlanSchemaInvalid, f.ID)
		}
		if seenFeat[f.ID] {
			return fmt.Errorf("%w: duplicate feature id %q", ErrPlanSchemaInvalid, f.ID)
		}
		seenFeat[f.ID] = true
		if f.Title == "" {
			return fmt.Errorf("%w: feature %s missing title", ErrPlanSchemaInvalid, f.ID)
		}
		if len(f.Fulfills) == 0 {
			return fmt.Errorf("%w: feature %s has empty fulfills", ErrPlanSchemaInvalid, f.ID)
		}
		if !allowedComplexities[f.EstimatedComplexity] {
			return fmt.Errorf("%w: feature %s estimated_complexity %q invalid", ErrPlanSchemaInvalid, f.ID, f.EstimatedComplexity)
		}
	}

	if err := p.checkCoverage(); err != nil {
		return err
	}
	return p.checkDAG()
}

// checkCoverage enforces the partition invariant:
//
//	⋃ features[*].fulfills == parent_criteria[*].id
//	pairwise disjoint
func (p *ProgramBrief) checkCoverage() error {
	parent := make(map[string]bool, len(p.ParentCriteria))
	for _, c := range p.ParentCriteria {
		parent[c.ID] = true
	}

	claimedBy := map[string]string{} // criterion id → feature id
	for _, f := range p.Features {
		for _, cid := range f.Fulfills {
			if !parent[cid] {
				return fmt.Errorf("%w: feature %s claims unknown criterion %q", ErrCoveragePhantom, f.ID, cid)
			}
			if prev, exists := claimedBy[cid]; exists {
				return fmt.Errorf("%w: criterion %q claimed by both %s and %s", ErrCoverageOverlap, cid, prev, f.ID)
			}
			claimedBy[cid] = f.ID
		}
	}

	var unclaimed []string
	for cid := range parent {
		if _, ok := claimedBy[cid]; !ok {
			unclaimed = append(unclaimed, cid)
		}
	}
	if len(unclaimed) > 0 {
		return fmt.Errorf("%w: %v", ErrCoverageIncomplete, unclaimed)
	}
	return nil
}

// checkDAG rejects unknown deps and cycles in depends_on_features.
func (p *ProgramBrief) checkDAG() error {
	known := make(map[string]bool, len(p.Features))
	for _, f := range p.Features {
		known[f.ID] = true
	}
	adj := make(map[string][]string, len(p.Features))
	for _, f := range p.Features {
		for _, dep := range f.DependsOnFeatures {
			if !known[dep] {
				return fmt.Errorf("%w: %s -> %s", ErrFeatureUnknownDep, f.ID, dep)
			}
			adj[f.ID] = append(adj[f.ID], dep)
		}
	}
	// 3-state DFS cycle detection (white=0, gray=1, black=2)
	state := make(map[string]int, len(p.Features))
	var dfs func(string) error
	dfs = func(node string) error {
		switch state[node] {
		case 1:
			return fmt.Errorf("%w: at %s", ErrFeatureCycle, node)
		case 2:
			return nil
		}
		state[node] = 1
		for _, dep := range adj[node] {
			if err := dfs(dep); err != nil {
				return err
			}
		}
		state[node] = 2
		return nil
	}
	for _, f := range p.Features {
		if state[f.ID] == 0 {
			if err := dfs(f.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// Sign returns a signed copy of p. Validate() is called first; an
// invalid plan never gets a signature (the signature attests to a
// well-formed payload, not arbitrary bytes).
func (p *ProgramBrief) Sign(key []byte, keyID string) (*ProgramBrief, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, err
	}
	sig, err := schemas.Sign(generic, key, keyID)
	if err != nil {
		return nil, err
	}
	out := *p
	out.Signature = sig
	return &out, nil
}

// VerifySignature checks the plan's HMAC under keyring.
func (p *ProgramBrief) VerifySignature(keyring map[string][]byte) error {
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return err
	}
	return schemas.Verify(generic, keyring)
}

// ─────────────────────────────────────────────────────────────────
// Planner pipeline
// ─────────────────────────────────────────────────────────────────

// ModelClient is the minimal contract the planner needs from a
// model-provider. Provider-agnostic by design -- the planner does
// not know it's calling Anthropic.
type ModelClient interface {
	// Plan submits the planner prompt + parent WorkItem context and
	// returns the model's structured ProgramBrief response.
	// Implementations parse the model output into the schema; the
	// returned plan is NOT validated yet (caller calls Validate +
	// Sign).
	Plan(ctx context.Context, parent schemas.WorkItem) (*ProgramBrief, error)

	// ModelID returns the provider-qualified id, e.g.
	// "anthropic:claude-opus-4-7", to stamp into planner_model_id.
	ModelID() string
}

// PlannerOptions configures one planner invocation.
type PlannerOptions struct {
	Client       ModelClient
	HMACKey      []byte
	HMACKeyID    string
}

// Run executes the one-shot planner pipeline:
//
//  1. Call the model with the parent WorkItem context.
//  2. Fill in the planner-fixed fields (program_id, parent_*,
//     planner_model_id, produced_at).
//  3. Validate (coverage + DAG + schema invariants).
//  4. Sign with the shared HMAC key.
//
// On any failure, returns the partial plan (for forensic dumping)
// alongside the error.
func Run(ctx context.Context, opts PlannerOptions, parent schemas.WorkItem) (*ProgramBrief, error) {
	if opts.Client == nil {
		return nil, errors.New("planner: nil ModelClient")
	}
	if len(opts.HMACKey) == 0 {
		return nil, errors.New("planner: HMAC key required")
	}
	plan, err := opts.Client.Plan(ctx, parent)
	if err != nil {
		return nil, fmt.Errorf("planner: model call: %w", err)
	}
	if plan == nil {
		return nil, errors.New("planner: model returned nil plan")
	}

	// Planner-owned fields (the model proposes features; the
	// orchestrator stamps identity + provenance).
	plan.SchemaVersion = 1
	plan.ProgramID = newProgramID()
	plan.ParentWorkItemID = string(parent.ID)
	plan.ParentCriteria = copyCriteria(parent.AcceptanceCriteria)
	plan.PlannerModelID = opts.Client.ModelID()
	plan.ProducedAt = time.Now().UTC()

	signed, err := plan.Sign(opts.HMACKey, opts.HMACKeyID)
	if err != nil {
		return plan, fmt.Errorf("planner: sign: %w", err)
	}
	return signed, nil
}

func copyCriteria(crit []schemas.Criterion) []PlanCriterion {
	out := make([]PlanCriterion, len(crit))
	for i, c := range crit {
		out[i] = PlanCriterion{ID: c.ID, Text: c.Text}
	}
	return out
}

// newProgramID returns a 12-hex-char program ID. 48 bits of entropy
// is enough for human-scale uniqueness; program state lives in
// sqlite with a UNIQUE constraint as the real durability anchor.
// The "m-" prefix is wire format -- handoff.schema.json pins it.
func newProgramID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return "m-" + hex.EncodeToString(b[:])
}
