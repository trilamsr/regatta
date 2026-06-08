package state

import (
	"testing"
)

// TestSubstrate_AcceptsToolCallKind pins migration 0021 widens the substrate_events kind CHECK to include 'tool_call' (#operator-console-S0).
func TestSubstrate_AcceptsToolCallKind(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)

	// Raw insert bypasses the Go-side payload validator — this test
	// pins the SQL CHECK only. Producer-side validation has its own
	// test in internal/orchestrator/state/substrate.
	_, err := db.SQL().Exec(`
		INSERT INTO substrate_events (
			id, run_id, tenant_id, kind, payload_json, written_by, written_at,
			nonce, sig_alg, sig_key_id, sig_mac
		) VALUES ('e1', 'run-1', 'default', 'tool_call', '{}', 'spawner.v1', 0,
		          'n1', 'hmac-sha256', 'k1', 'mac1')
	`)
	if err != nil {
		t.Errorf("substrate_events rejected tool_call kind: %v", err)
	}
}

// TestSubstrate_RejectsUnknownKind pins the kind CHECK still fails closed for unrecognised kinds (#operator-console-S0).
func TestSubstrate_RejectsUnknownKind(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)

	_, err := db.SQL().Exec(`
		INSERT INTO substrate_events (
			id, run_id, tenant_id, kind, payload_json, written_by, written_at,
			nonce, sig_alg, sig_key_id, sig_mac
		) VALUES ('e1', 'run-1', 'default', 'totally_made_up_kind', '{}', 'test.v1', 0,
		          'n1', 'hmac-sha256', 'k1', 'mac1')
	`)
	if err == nil {
		t.Error("expected CHECK to reject unknown kind, got nil")
	}
}
