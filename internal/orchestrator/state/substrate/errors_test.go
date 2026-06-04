package substrate_test

import (
	"errors"
	"testing"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// TestErrUnverifiable_WrapsSchemasSentinel asserts substrate.ErrUnverifiable wraps schemas.ErrUnverifiable so errors.Is bridges the package boundary (#715).
func TestErrUnverifiable_WrapsSchemasSentinel(t *testing.T) {
	if !errors.Is(substrate.ErrUnverifiable, schemas.ErrUnverifiable) {
		t.Fatalf("errors.Is(substrate.ErrUnverifiable, schemas.ErrUnverifiable) = false, want true: docstring claims wrap but %v does not unwrap to %v", substrate.ErrUnverifiable, schemas.ErrUnverifiable)
	}
}
