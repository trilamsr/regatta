// Outcome-conditional DAG types for MVP-2 W1.
//
// Wire-format v2 introduces first-class edges with optional CEL
// predicates over upstream output JSON. v1 briefs remain wire-compatible:
// LowerV1ToV2 produces an equivalent v2 view by emitting one
// unconditional Edge per v1 depends_on_features entry. See spec §3.2.

package program

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/orchestrator"
)

// FeatureID is the wire-format ID for a planned feature (regex shape
// pinned by featureIDRe in planner.go). Aliased to string so existing
// v1 fields (PlannedFeature.ID, DependsOnFeatures) compose without
// conversion churn.
type FeatureID = string

// SkipMode controls what happens to a target node when its inbound
// edge evaluates false (or the upstream is skipped).
type SkipMode string

const (
	// SkipCascade propagates the skip: target gets skipped (default).
	SkipCascade SkipMode = "cascade"
	// SkipIgnore drops the inbound: target spawns iff ≥1 inbound fired.
	SkipIgnore SkipMode = "ignore"
	// SkipDefault routes through the originating feature's default_next.
	SkipDefault SkipMode = "default"
)

// Edge is one directed link in the program DAG. Empty Predicate ==
// unconditional. Non-empty Predicate is a CEL expression evaluated
// against the upstream node's output JSON under variable `out`, typed
// to the upstream's OutputsSchema.
type Edge struct {
	From      FeatureID `json:"from"`
	To        FeatureID `json:"to"`
	Predicate string    `json:"predicate,omitempty"`
	OnSkip    SkipMode  `json:"on_skip,omitempty"`
}

// Validate enforces edge-local shape invariants: non-empty endpoints
// and no self-loops. Cross-edge invariants (unknown target across the
// brief, missing default_next on conditional fan-out) live on the
// containing brief.
func (e Edge) Validate() error {
	if e.From == "" {
		return fmt.Errorf("%w: edge missing from", orchestrator.ErrEdgeUnknownTarget)
	}
	if e.To == "" {
		return fmt.Errorf("%w: edge missing to", orchestrator.ErrEdgeUnknownTarget)
	}
	if e.From == e.To {
		return fmt.Errorf("%w: self-loop %s -> %s", orchestrator.ErrEdgeUnknownTarget, e.From, e.To)
	}
	return nil
}

// OutputsSchema is the JSON-Schema subset declared per feature for its
// produced output. CEL predicates type-check against this at ingest.
//
// Type strings: object, string, int, double, bool, list. Object types
// declare per-property schemas via Properties; list types declare an
// element schema via Items. Enum constrains a scalar's value set;
// ValidateV2 (W2-A) uses it for forward-compatibility and the
// evaluator (W3) honours it at runtime.
//
// Recursive shape is required so nested objects and typed lists
// participate in CEL type-checking. W1-D shipped Properties as
// map[string]string; W2-A widens it to map[string]*OutputsSchema
// before any external caller depends on the narrower form.
type OutputsSchema struct {
	Type       string                    `json:"type"`
	Properties map[string]*OutputsSchema `json:"properties,omitempty"`
	Items      *OutputsSchema            `json:"items,omitempty"`
	Enum       []any                     `json:"enum,omitempty"`
}

// PlannedFeatureV2 extends PlannedFeature with edges, default_next,
// and outputs_schema. The embedded v1 struct keeps existing accessors
// (ID, Title, Fulfills, DependsOnFeatures) working unchanged.
type PlannedFeatureV2 struct {
	PlannedFeature
	Edges         []Edge         `json:"edges,omitempty"`
	DefaultNext   *FeatureID     `json:"default_next,omitempty"`
	OutputsSchema *OutputsSchema `json:"outputs_schema,omitempty"`
}

// ProgramBriefV2 is the v2 wire format.
//
// Embedding ProgramBrief carries every v1 field (ProgramID, ParentCriteria,
// Signature, …). FeaturesV2 carries the json:"features" tag at the
// outer level so it wins over the embedded ProgramBrief.Features under
// Go's JSON field-promotion rules; LowerV1ToV2 also nils the embedded
// slice for hygiene so a single Features array round-trips.
type ProgramBriefV2 struct {
	ProgramBrief
	FeaturesV2 []PlannedFeatureV2 `json:"features"`
}

// IsV2Brief reports whether raw is a v2 ProgramBrief by sniffing the
// schema_version field. Panic-safe on malformed input: any decode
// error returns false rather than propagating.
//
// Uses a streaming decoder rather than full unmarshal so a malformed
// tail (e.g. truncated JSON after schema_version) does not change the
// answer for the prefix that already parsed.
func IsV2Brief(raw []byte) bool {
	if len(raw) == 0 {
		return false
	}
	var probe struct {
		SchemaVersion int `json:"schema_version"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&probe); err != nil {
		return false
	}
	return probe.SchemaVersion == 2
}

// VerifySignatureV2 checks the v2 brief's HMAC under keyring. The
// canonical body is the full v2 JSON (features array carries Edges +
// OutputsSchema + DefaultNext), so v1 VerifySignature on the embedded
// struct would skip those fields and fail to detect tampering of v2-
// only payload. This method marshals the outer ProgramBriefV2 so
// every signed byte rides through schemas.Verify.
func (p *ProgramBriefV2) VerifySignatureV2(keyring map[string][]byte) error {
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

// LowerV1ToV2 lifts a v1 ProgramBrief into the equivalent v2 view by
// emitting one unconditional outgoing Edge per depends_on_features
// entry. The transform is lossless for behaviour: same feature set,
// same dependency closure (every v1 dep `F-B depends_on F-A` becomes
// an outgoing edge `F-A -> F-B` on F-A's Edges slice).
//
// Edges live on the upstream (source) feature because ValidateV2
// requires edge.From == owning feature ID — outgoing semantics.
//
// The returned brief has SchemaVersion=2; the embedded v1 Features
// slice is cleared so JSON output carries the v2 "features" array
// only.
func LowerV1ToV2(brief *ProgramBrief) *ProgramBriefV2 {
	if brief == nil {
		return nil
	}
	out := &ProgramBriefV2{ProgramBrief: *brief}
	out.SchemaVersion = 2
	out.Features = nil

	out.FeaturesV2 = make([]PlannedFeatureV2, 0, len(brief.Features))
	byID := make(map[string]int, len(brief.Features))
	for i, f := range brief.Features {
		out.FeaturesV2 = append(out.FeaturesV2, PlannedFeatureV2{PlannedFeature: f})
		byID[f.ID] = i
	}
	for _, f := range brief.Features {
		for _, dep := range f.DependsOnFeatures {
			idx, ok := byID[dep]
			if !ok {
				continue
			}
			out.FeaturesV2[idx].Edges = append(out.FeaturesV2[idx].Edges,
				Edge{From: dep, To: f.ID})
		}
	}
	return out
}
