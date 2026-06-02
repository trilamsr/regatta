// Package pricing holds the hardcoded per-model USD rate tables (native
// Anthropic + AWS Bedrock + GCP Vertex) and the Lookup function that
// callers use to price a token-count.
//
// Pricing is critical-path data that MUST be hermetic (no boot-time network
// call) and MUST be reviewable in the diff (every change is a code-review
// event). The override-file surface is deferred per spec §10 S2 to its own
// follow-up (#239) — refresh-via-code-change matches Helicone/Portkey/LiteLLM v1.
//
// SKU namespace. Native Anthropic SKUs use bare keys (e.g.
// `claude-opus-4-7`). Bedrock and Vertex tier SKUs use a dotted provider
// prefix (e.g. `bedrock.claude-opus-4-7`, `vertex.claude-opus-4-7`) so
// the operator config can pick the priced tier without changing the
// model-id surface. Closes #240.
//
// Lookup hard-fails on unknown SKUs: silent-zero is the Portkey trap called
// out in spec §3.1. Retired SKUs (RetiredAfter non-zero AND in the past) are
// also rejected so a stale-SKU caller cannot drift past pricing changes.
package pricing

import (
	"errors"
	"time"
)

// ErrPricingMissing is returned by Lookup when the model SKU is unknown or
// retired. Callers MUST hard-fail; silent-zero is the Portkey trap.
var ErrPricingMissing = errors.New("pricing missing for model")

// Row holds per-million-token USD rates for one model SKU plus a sunset
// marker. The zero value of RetiredAfter means the SKU is still active.
type Row struct {
	InputUSDPerMTok         float64
	CacheReadUSDPerMTok     float64
	CacheCreationUSDPerMTok float64
	OutputUSDPerMTok        float64
	RetiredAfter            time.Time
}

// now is a seam so tests can pin "current time" for retired-SKU coverage
// without leaking time.Now into the pure-function callers.
var now = time.Now

// Lookup returns the priced row for a model SKU. ErrPricingMissing when the
// SKU is unknown OR retired (RetiredAfter non-zero and in the past).
//
// Resolution order: Anthropic (bare keys) → Bedrock (`bedrock.*`) → Vertex
// (`vertex.*`). The provider prefix is part of the key string; namespaces
// do not collide because bare and prefixed SKUs live in disjoint maps.
func Lookup(model string) (Row, error) {
	row, ok := lookupRaw(model)
	if !ok {
		return Row{}, ErrPricingMissing
	}
	if !row.RetiredAfter.IsZero() && now().After(row.RetiredAfter) {
		return Row{}, ErrPricingMissing
	}
	return row, nil
}

// lookupRaw consults the per-provider maps in deterministic order. Kept
// private so the search order is not part of the caller API surface;
// switching to a merged catalog later is a one-line refactor.
func lookupRaw(model string) (Row, bool) {
	if row, ok := Anthropic[model]; ok {
		return row, true
	}
	if row, ok := Bedrock[model]; ok {
		return row, true
	}
	if row, ok := Vertex[model]; ok {
		return row, true
	}
	return Row{}, false
}

// Catalog returns a merged view of every active per-provider table.
// Callers that need to range over all priced SKUs (drift checker, future
// admin-CLI list command) MUST use this rather than reading Anthropic
// directly. Returned map is a fresh allocation — mutating it does not
// affect the source-of-truth tables.
func Catalog() map[string]Row {
	out := make(map[string]Row, len(Anthropic)+len(Bedrock)+len(Vertex))
	for k, v := range Anthropic {
		out[k] = v
	}
	for k, v := range Bedrock {
		out[k] = v
	}
	for k, v := range Vertex {
		out[k] = v
	}
	return out
}
