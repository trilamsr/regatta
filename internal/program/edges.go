// Outcome-conditional DAG types for MVP-2 W1. v2 wire format adds
// first-class edges with optional CEL predicates over upstream output
// JSON; v1 briefs stay wire-compatible (LowerV1ToV2 emits one
// unconditional Edge per depends_on_features entry). Spec §3.2.

package program

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/orchestrator"
)

// FeatureID — wire-format planned-feature ID (regex shape in
// planner.go::featureIDRe). Aliased to string so existing v1 fields
// compose without conversion churn.
type FeatureID = string

// SkipMode controls what happens to a target node when its inbound
// edge evaluates false (or upstream is skipped).
type SkipMode string

const (
	// SkipCascade — target also skips (default).
	SkipCascade SkipMode = "cascade"
	// SkipIgnore — drop the inbound; target spawns iff ≥1 inbound fired.
	SkipIgnore SkipMode = "ignore"
	// SkipDefault — route through the originating feature's default_next.
	SkipDefault SkipMode = "default"
)

// Edge is one directed link in the program DAG. Empty Predicate is
// unconditional; non-empty is a CEL expression over the upstream's
// output JSON under variable `out`, typed to OutputsSchema.
type Edge struct {
	From      FeatureID `json:"from"`
	To        FeatureID `json:"to"`
	Predicate string    `json:"predicate,omitempty"`
	OnSkip    SkipMode  `json:"on_skip,omitempty"`
}

// Validate enforces edge-local shape (non-empty endpoints, no self-
// loops). Cross-edge invariants (unknown target, missing default_next
// on conditional fan-out) live on the containing brief.
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

// OutputsSchema is the JSON-Schema subset declared per feature; CEL
// predicates type-check against this at ingest. Type strings: object,
// string, int, double, bool, list. Recursive shape so nested objects
// and typed lists participate in CEL type-checking.
type OutputsSchema struct {
	Type       string                    `json:"type"`
	Properties map[string]*OutputsSchema `json:"properties,omitempty"`
	Items      *OutputsSchema            `json:"items,omitempty"`
	Enum       []any                     `json:"enum,omitempty"`
}

// PlannedFeatureV2 extends PlannedFeature with edges, default_next,
// outputs_schema. Embedded v1 keeps existing accessors unchanged.
type PlannedFeatureV2 struct {
	PlannedFeature
	Edges         []Edge         `json:"edges,omitempty"`
	DefaultNext   *FeatureID     `json:"default_next,omitempty"`
	OutputsSchema *OutputsSchema `json:"outputs_schema,omitempty"`
}

// ProgramBriefV2 is the v2 wire format. Embedding ProgramBrief carries
// every v1 field; FeaturesV2 carries `json:"features"` at the outer
// level so it wins over the embedded slice under Go's promotion rules
// (LowerV1ToV2 also nils the embedded slice so a single Features array
// round-trips).
type ProgramBriefV2 struct {
	ProgramBrief
	FeaturesV2 []PlannedFeatureV2 `json:"features"`
}

// IsV2Brief sniffs schema_version. Streaming decoder so a malformed
// tail (truncated JSON after schema_version) does not change the
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

// VerifySignatureV2 checks the v2 HMAC against the full v2 JSON. v1
// VerifySignature would skip Edges/OutputsSchema/DefaultNext and miss
// tampering of v2-only payload; marshalling the outer struct ensures
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

// LowerV1ToV2 lifts a v1 brief into the equivalent v2 view by emitting
// one unconditional outgoing Edge per depends_on_features entry.
// Behaviour-lossless: same feature set, same dependency closure.
// Edges live on the upstream because ValidateV2 requires edge.From ==
// owning feature ID. SchemaVersion=2 on return; embedded v1 Features
// slice cleared so JSON output carries the v2 "features" array only.
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
