package obs

import "testing"

// TestAllAttrKeys_IncludesKeyExitReason asserts AllAttrKeys exposes KeyExitReason so schema validation accepts the agent.exited bucket attr (R-MEGA-3 LIVE-14).
func TestAllAttrKeys_IncludesKeyExitReason(t *testing.T) {
	for _, k := range AllAttrKeys() {
		if k == KeyExitReason {
			return
		}
	}
	t.Fatalf("KeyExitReason missing from AllAttrKeys; defined but not enumerated")
}
