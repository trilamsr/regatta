// Package pricing holds the hardcoded per-model USD rate tables
// (Anthropic + AWS Bedrock + GCP Vertex) and Lookup. Pricing is
// critical-path data that MUST be hermetic and reviewable in the diff;
// override-file surface is deferred to #239. SKU namespace: native
// Anthropic uses bare keys (`claude-opus-4-7`); Bedrock and Vertex use
// dotted provider prefix (`bedrock.claude-opus-4-7`) so operator
// config picks the tier without changing model-id. Lookup hard-fails
// on unknown OR retired SKUs — silent-zero is the Portkey trap (§3.1).
package pricing

import (
	"errors"
	"fmt"
	"time"
)

// ErrPricingMissing — model SKU unknown OR retired. Callers MUST hard-
// fail; silent-zero is the Portkey trap.
var ErrPricingMissing = errors.New("pricing missing for model")

// ErrPricingZeroRow — an active in-tree row carries a non-positive
// rate. Spec §3.8 + §7 B7: boot validator MUST hard-fail before any
// Lookup returns the bad row. Distinct from ErrOverrideInvalid which
// guards operator-supplied overrides; this guards the in-tree tables
// (Anthropic/Bedrock/Vertex) named in the cost-governor runbook.
var ErrPricingZeroRow = errors.New("pricing table has zero-rate active row")

// Row holds per-million-token USD rates plus a sunset marker. Zero
// RetiredAfter means the SKU is still active.
type Row struct {
	InputUSDPerMTok         float64
	CacheReadUSDPerMTok     float64
	CacheCreationUSDPerMTok float64
	OutputUSDPerMTok        float64
	RetiredAfter            time.Time
}

// now is a seam so tests pin "current time" for retired-SKU coverage
// without leaking time.Now into pure-function callers.
var now = time.Now

// Lookup returns the priced row for model. ErrPricingMissing when
// unknown OR retired (RetiredAfter non-zero and past). ErrPricingZeroRow
// when an active row carries a non-positive rate — boot Validate
// catches in-tree tables, but a mid-process mutation (test code, future
// LoadOverride re-entry) would otherwise trip the Portkey trap (#447).
// Resolution order: Anthropic → Bedrock → Vertex.
func Lookup(model string) (Row, error) {
	row, ok := lookupRaw(model)
	if !ok {
		return Row{}, ErrPricingMissing
	}
	if !row.RetiredAfter.IsZero() && now().After(row.RetiredAfter) {
		return Row{}, ErrPricingMissing
	}
	if field, value, ok := rowFieldNonPositive(row); ok {
		return Row{}, fmt.Errorf("%w: %s %s=%v", ErrPricingZeroRow, model, field, value)
	}
	return row, nil
}

// lookupRaw is private so the search order is not part of caller API —
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

// Catalog returns a fresh merged view of every active per-provider
// table. Callers ranging over all priced SKUs (drift checker, future
// admin-CLI list) MUST use this rather than reading Anthropic
// directly. Mutating the returned map does not affect source tables.
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

// Validate scans an in-tree pricing table for the Portkey-trap
// invariant (no active SKU may carry a non-positive rate; table non-
// empty) and returns ErrPricingZeroRow-wrapped on first violation.
// Empty / nil tables are rejected (#445): a rebase truncating
// anthropic.go to an empty map would otherwise zero every routed
// call's spend.
//
// Retired rows are exempt — operators may pin historical snapshots
// for replay or backfill recipes. Called from init() so a bad merge
// panics before the first Lookup.
func Validate(provider string, table map[string]Row) error {
	if len(table) == 0 {
		return fmt.Errorf("%w: %s table is empty", ErrPricingZeroRow, provider)
	}
	for model, row := range table {
		if !row.RetiredAfter.IsZero() && now().After(row.RetiredAfter) {
			continue
		}
		if field, value, ok := rowFieldNonPositive(row); ok {
			return fmt.Errorf("%w: %s.%s %s=%v", ErrPricingZeroRow, provider, model, field, value)
		}
	}
	return nil
}

// rowFieldNonPositive is the single source of truth for the Portkey-
// trap field scan — Validate, validateRow (override path), and Lookup
// all share it so a future field addition cannot leave one call site
// checking three rates and another checking four (drift reviewer
// flagged in #447).
func rowFieldNonPositive(row Row) (field string, value float64, ok bool) {
	if row.InputUSDPerMTok <= 0 {
		return "InputUSDPerMTok", row.InputUSDPerMTok, true
	}
	if row.CacheReadUSDPerMTok <= 0 {
		return "CacheReadUSDPerMTok", row.CacheReadUSDPerMTok, true
	}
	if row.CacheCreationUSDPerMTok <= 0 {
		return "CacheCreationUSDPerMTok", row.CacheCreationUSDPerMTok, true
	}
	if row.OutputUSDPerMTok <= 0 {
		return "OutputUSDPerMTok", row.OutputUSDPerMTok, true
	}
	return "", 0, false
}

// init panics on a zero-rate active row — silent-zero accounting is
// worse than a fail-fast crash the operator can grep from the boot
// log. Override-file rows go through validateRow in LoadOverride.
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
