package pricing

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
)

// ErrOverrideInvalid is returned when an override row violates the
// Portkey-trap invariant (non-positive rate). Callers MUST hard-fail.
var ErrOverrideInvalid = errors.New("pricing override row invalid")

// ErrOverrideUnsafe is returned when the override file's POSIX mode is
// world-writable. Spec §10 S2 closes R14 (override-tampering) by
// rejecting at load time rather than trusting the path string.
var ErrOverrideUnsafe = errors.New("pricing override file is world-writable")

// LoadOverride merges a JSON map of model→Row into the package
// Anthropic table at process boot. Empty path is a no-op (the default
// regatta.yaml carries no override). Per-key merge — each model in the
// override replaces the hardcoded row entirely; sibling rows untouched.
// Adds new SKUs (Bedrock/Vertex/marketplace) the hardcoded table does
// not list.
//
// Hard-fails on: missing file, malformed JSON, unknown Row field,
// non-positive rate, world-writable file mode. Spec §10 S2 + §3.8.
// Refresh-via-PR remains the default; this surface is the escape hatch
// for operators who need rates the upstream-mirror table cannot carry.
func LoadOverride(path string) error {
	if path == "" {
		return nil
	}

	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("pricing override: stat %s: %w", path, err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o002 != 0 {
		return fmt.Errorf("%w: %s (mode %v)", ErrOverrideUnsafe, path, info.Mode().Perm())
	}

	data, err := os.ReadFile(path) //nolint:gosec // G304: path is the operator-supplied safety.cost.pricing_override_path; the world-writable check above is the trust boundary, not the variable taint.
	if err != nil {
		return fmt.Errorf("pricing override: read %s: %w", path, err)
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var overrides map[string]Row
	if err := dec.Decode(&overrides); err != nil {
		return fmt.Errorf("pricing override: parse %s: %w", path, err)
	}

	for model, row := range overrides {
		if err := validateRow(model, row); err != nil {
			return err
		}
		Anthropic[model] = row
	}
	return nil
}

// validateRow enforces the Portkey-trap invariant for override rows.
// Shares rowFieldNonPositive with Validate + Lookup so a future field
// addition cannot drift between in-tree and operator-supplied paths.
// Retired rows are exempt — operators may load a historical snapshot
// for replay.
func validateRow(model string, row Row) error {
	if !row.RetiredAfter.IsZero() {
		return nil
	}
	if field, value, ok := rowFieldNonPositive(row); ok {
		return fmt.Errorf("%w: %s %s=%v", ErrOverrideInvalid, model, field, value)
	}
	return nil
}
