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
	"fmt"
	"time"
)

// ErrPricingMissing is returned by Lookup when the model SKU is unknown or
// retired. Callers MUST hard-fail; silent-zero is the Portkey trap.
var ErrPricingMissing = errors.New("pricing missing for model")

// ErrPricingZeroRow is returned by Validate when an active row in an
// in-tree pricing table carries a non-positive rate. Spec §3.8 + §7 B7:
// silent-zero is the Portkey trap, so the boot validator MUST hard-fail
// before any Lookup can return the bad row. Distinct from
// ErrOverrideInvalid — that sentinel guards operator-supplied overrides;
// this one guards the in-tree source-of-truth tables (Anthropic, Bedrock,
// Vertex) which are the rollback target named in the cost-governor
// runbook §"Pricing-table rollback".
var ErrPricingZeroRow = errors.New("pricing table has zero-rate active row")

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

// Validate scans an in-tree pricing table for the Portkey-trap invariant
// (no active SKU may carry a non-positive rate) and returns an
// ErrPricingZeroRow-wrapped error on the first violation. The provider
// label is folded into the error message so operators reading the boot
// log can jump to the right table without re-deriving which file failed.
//
// Retired rows are exempt — operators may pin historical snapshots for
// replay or backfill recipes. Active = RetiredAfter.IsZero() OR in the
// future at validation time.
//
// Called from init() against each per-provider table so a bad rebase /
// merge that lands a zero rate panics the process before the first
// Lookup. The runbook "Pricing-table rollback" procedure assumes this
// guarantee; the fixture at testdata/anthropic_bad_zero_row.go is the
// falsifier.
func Validate(provider string, table map[string]Row) error {
	for model, row := range table {
		if !row.RetiredAfter.IsZero() && now().After(row.RetiredAfter) {
			continue
		}
		if row.InputUSDPerMTok <= 0 {
			return fmt.Errorf("%w: %s.%s InputUSDPerMTok=%v", ErrPricingZeroRow, provider, model, row.InputUSDPerMTok)
		}
		if row.CacheReadUSDPerMTok <= 0 {
			return fmt.Errorf("%w: %s.%s CacheReadUSDPerMTok=%v", ErrPricingZeroRow, provider, model, row.CacheReadUSDPerMTok)
		}
		if row.CacheCreationUSDPerMTok <= 0 {
			return fmt.Errorf("%w: %s.%s CacheCreationUSDPerMTok=%v", ErrPricingZeroRow, provider, model, row.CacheCreationUSDPerMTok)
		}
		if row.OutputUSDPerMTok <= 0 {
			return fmt.Errorf("%w: %s.%s OutputUSDPerMTok=%v", ErrPricingZeroRow, provider, model, row.OutputUSDPerMTok)
		}
	}
	return nil
}

// init runs the boot validator against each in-tree pricing table.
// Panic is the correct response: a zero-rate active row is a Portkey
// trap (§3.1) and silent-zero accounting is worse than a fail-fast crash
// the operator can grep from the boot log. Override-file rows go through
// validateRow + ErrOverrideInvalid in LoadOverride.
func init() {
	if err := Validate("anthropic", Anthropic); err != nil {
		panic(err)
	}
	if err := Validate("bedrock", Bedrock); err != nil {
		panic(err)
	}
	if err := Validate("vertex", Vertex); err != nil {
		panic(err)
	}
}
