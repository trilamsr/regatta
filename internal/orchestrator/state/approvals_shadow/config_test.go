package approvals_shadow

import "testing"

// TestShadowMode_Constants pins the per-process tri-state token strings (#795 T5).
func TestShadowMode_Constants(t *testing.T) {
	if ShadowModeOff != "off" {
		t.Fatalf("ShadowModeOff=%q want off", ShadowModeOff)
	}
	if ShadowModeOn != "on" {
		t.Fatalf("ShadowModeOn=%q want on", ShadowModeOn)
	}
}

// TestReadMode_Constants pins the Phase-C read-path token strings (#795 T5).
func TestReadMode_Constants(t *testing.T) {
	if ReadModeLegacy != "legacy" {
		t.Fatalf("ReadModeLegacy=%q want legacy", ReadModeLegacy)
	}
	if ReadModeSubstrateFirst != "substrate_first" {
		t.Fatalf("ReadModeSubstrateFirst=%q want substrate_first", ReadModeSubstrateFirst)
	}
}

// TestShadowWriteConfig_ZeroValue asserts the zero config opts out of shadow + reads legacy (#795 T5).
func TestShadowWriteConfig_ZeroValue(t *testing.T) {
	var cfg ShadowWriteConfig
	if cfg.Mode != "" {
		t.Fatalf("zero Mode=%q want empty (≠ on)", cfg.Mode)
	}
	if cfg.ReadMode != "" {
		t.Fatalf("zero ReadMode=%q want empty (≠ substrate_first)", cfg.ReadMode)
	}
	if cfg.Mode == ShadowModeOn {
		t.Fatalf("zero-value config must not be ShadowModeOn")
	}
	if cfg.ReadMode == ReadModeSubstrateFirst {
		t.Fatalf("zero-value config must not be ReadModeSubstrateFirst")
	}
}

// TestSystemActor_Constant pins the sweeper placeholder (#795 T5).
func TestSystemActor_Constant(t *testing.T) {
	if SystemActor != "system" {
		t.Fatalf("SystemActor=%q want system", SystemActor)
	}
}
