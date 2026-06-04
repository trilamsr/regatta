package health

import (
	"testing"
)

// TestConfigureHeartbeatPool_RejectsNilDB pins caller-injection contract (G2).
func TestConfigureHeartbeatPool_RejectsNilDB(t *testing.T) {
	if err := ConfigureHeartbeatPool(nil); err == nil {
		t.Fatal("want error on nil db, got nil")
	}
}
