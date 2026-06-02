package pricing_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/trilamsr/regatta/internal/cost/pricing"
)

// snapshotTable copies pricing.Anthropic so a test can restore it via
// t.Cleanup without leaking mutations into sibling tests.
func snapshotTable(t *testing.T) {
	t.Helper()
	saved := make(map[string]pricing.Row, len(pricing.Anthropic))
	for k, v := range pricing.Anthropic {
		saved[k] = v
	}
	t.Cleanup(func() {
		for k := range pricing.Anthropic {
			delete(pricing.Anthropic, k)
		}
		for k, v := range saved {
			pricing.Anthropic[k] = v
		}
	})
}

// writeOverride writes a 0o600 JSON file to a tmpdir + returns its path.
// 0o600 keeps the world-writable check happy; tests that need a hostile
// mode chmod afterwards.
func writeOverride(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "pricing.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write override: %v", err)
	}
	return path
}

// TestLoadOverride_EmptyPathIsNoOp pins the zero-config branch — unset
// pricing_override_path MUST NOT touch the hardcoded table.
func TestLoadOverride_EmptyPathIsNoOp(t *testing.T) {
	snapshotTable(t)
	before := pricing.Anthropic["claude-sonnet-4-7"]
	if err := pricing.LoadOverride(""); err != nil {
		t.Fatalf("LoadOverride(\"\"): %v; want nil", err)
	}
	after := pricing.Anthropic["claude-sonnet-4-7"]
	if before != after {
		t.Fatalf("LoadOverride(\"\") mutated table: before=%+v after=%+v", before, after)
	}
}

// TestLoadOverride_OverridesExistingSKU pins the merge contract: a key
// in the override file replaces the hardcoded row for that SKU only;
// other rows stay untouched.
func TestLoadOverride_OverridesExistingSKU(t *testing.T) {
	snapshotTable(t)
	path := writeOverride(t, `{
        "claude-sonnet-4-7": {
            "InputUSDPerMTok": 2.50,
            "CacheReadUSDPerMTok": 0.25,
            "CacheCreationUSDPerMTok": 3.10,
            "OutputUSDPerMTok": 12.00
        }
    }`)

	if err := pricing.LoadOverride(path); err != nil {
		t.Fatalf("LoadOverride: %v", err)
	}

	got, err := pricing.Lookup("claude-sonnet-4-7")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got.InputUSDPerMTok != 2.50 {
		t.Errorf("InputUSDPerMTok=%v; want 2.50", got.InputUSDPerMTok)
	}
	if got.OutputUSDPerMTok != 12.00 {
		t.Errorf("OutputUSDPerMTok=%v; want 12.00", got.OutputUSDPerMTok)
	}

	// Sibling row must be untouched (per-key merge, not full-table replace).
	other, err := pricing.Lookup("claude-opus-4-7")
	if err != nil {
		t.Fatalf("Lookup(opus): %v", err)
	}
	if other.InputUSDPerMTok != 15.00 {
		t.Errorf("opus mutated by sonnet override: InputUSDPerMTok=%v; want 15.00", other.InputUSDPerMTok)
	}
}

// TestLoadOverride_AddsNewSKU pins the Bedrock/Vertex use case: an
// override key that is NOT in the hardcoded table is added.
func TestLoadOverride_AddsNewSKU(t *testing.T) {
	snapshotTable(t)
	path := writeOverride(t, `{
        "bedrock-claude-sonnet-4-7": {
            "InputUSDPerMTok": 3.30,
            "CacheReadUSDPerMTok": 0.33,
            "CacheCreationUSDPerMTok": 4.10,
            "OutputUSDPerMTok": 16.50
        }
    }`)

	if err := pricing.LoadOverride(path); err != nil {
		t.Fatalf("LoadOverride: %v", err)
	}

	got, err := pricing.Lookup("bedrock-claude-sonnet-4-7")
	if err != nil {
		t.Fatalf("Lookup(bedrock): %v; want hit after override", err)
	}
	if got.InputUSDPerMTok != 3.30 {
		t.Errorf("InputUSDPerMTok=%v; want 3.30", got.InputUSDPerMTok)
	}
}

// TestLoadOverride_MalformedJSON_HardFails pins the malformed-input
// branch — a parse error MUST surface, not silently drop the override.
func TestLoadOverride_MalformedJSON_HardFails(t *testing.T) {
	snapshotTable(t)
	path := writeOverride(t, `{not valid json`)
	err := pricing.LoadOverride(path)
	if err == nil {
		t.Fatalf("LoadOverride(malformed): nil; want parse error")
	}
}

// TestLoadOverride_UnknownField_HardFails pins the DisallowUnknownFields
// invariant — a JSON key that does NOT (even case-insensitively) match
// any Row field MUST surface, not silently no-op. Without strict-decode,
// operator typos like adding an "AmazonBedrockRate" key are lost.
func TestLoadOverride_UnknownField_HardFails(t *testing.T) {
	snapshotTable(t)
	path := writeOverride(t, `{
        "claude-sonnet-4-7": {
            "InputUSDPerMTok": 2.50,
            "CacheReadUSDPerMTok": 0.25,
            "CacheCreationUSDPerMTok": 3.10,
            "OutputUSDPerMTok": 12.00,
            "AmazonBedrockRate": 999.99
        }
    }`)
	err := pricing.LoadOverride(path)
	if err == nil {
		t.Fatalf("LoadOverride(unknown-field): nil; want strict-decoder error")
	}
}

// TestLoadOverride_NonPositiveRate_Rejected pins the B7 Portkey-trap
// extension to overrides — zero or negative rates MUST hard-fail, never
// silently price a real call at $0.
func TestLoadOverride_NonPositiveRate_Rejected(t *testing.T) {
	snapshotTable(t)
	path := writeOverride(t, `{
        "claude-sonnet-4-7": {
            "InputUSDPerMTok": 0,
            "CacheReadUSDPerMTok": 0.25,
            "CacheCreationUSDPerMTok": 3.10,
            "OutputUSDPerMTok": 12.00
        }
    }`)
	err := pricing.LoadOverride(path)
	if err == nil {
		t.Fatalf("LoadOverride(zero-rate): nil; want rejection")
	}
	if !errors.Is(err, pricing.ErrOverrideInvalid) {
		t.Fatalf("err=%v; want ErrOverrideInvalid", err)
	}
}

// TestLoadOverride_MissingFile_HardFails pins the operator-typo branch:
// if pricing_override_path points at nothing, fail loud, not silent.
func TestLoadOverride_MissingFile_HardFails(t *testing.T) {
	snapshotTable(t)
	err := pricing.LoadOverride(filepath.Join(t.TempDir(), "nope.json"))
	if err == nil {
		t.Fatalf("LoadOverride(missing): nil; want filesystem error")
	}
}

// TestLoadOverride_WorldWritable_Rejected pins R14 (override-tampering
// surface): a 0o666 override file MUST be rejected. Unix-only; Windows
// permission bits don't map cleanly to POSIX modes.
func TestLoadOverride_WorldWritable_Rejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes — skipped on Windows")
	}
	snapshotTable(t)
	path := writeOverride(t, `{
        "claude-sonnet-4-7": {
            "InputUSDPerMTok": 2.50,
            "CacheReadUSDPerMTok": 0.25,
            "CacheCreationUSDPerMTok": 3.10,
            "OutputUSDPerMTok": 12.00
        }
    }`)
	if err := os.Chmod(path, 0o666); err != nil { //nolint:gosec // G302: hostile mode is the test fixture — the assertion is that LoadOverride REJECTS it.
		t.Fatalf("chmod 0666: %v", err)
	}
	err := pricing.LoadOverride(path)
	if err == nil {
		t.Fatalf("LoadOverride(world-writable): nil; want ErrOverrideUnsafe")
	}
	if !errors.Is(err, pricing.ErrOverrideUnsafe) {
		t.Fatalf("err=%v; want ErrOverrideUnsafe", err)
	}
}
