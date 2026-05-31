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
// Property type strings are CEL primitives: string, int, double, bool,
// list, map.
type OutputsSchema struct {
	Type       string            `json:"type"`
	Properties map[string]string `json:"properties,omitempty"`
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

// LowerV1ToV2 lifts a v1 ProgramBrief into the equivalent v2 view by
// emitting one unconditional Edge per depends_on_features entry. The
// transform is lossless for behaviour: same feature set, same
// dependency closure (every v1 dep becomes exactly one v2 edge with
// the dependent as To and the dependency as From).
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
	for _, f := range brief.Features {
		fv2 := PlannedFeatureV2{PlannedFeature: f}
		for _, dep := range f.DependsOnFeatures {
			fv2.Edges = append(fv2.Edges, Edge{From: dep, To: f.ID})
		}
		out.FeaturesV2 = append(out.FeaturesV2, fv2)
	}
	return out
}
