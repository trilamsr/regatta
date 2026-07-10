package state

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/trilamsr/regatta/internal/canon"
)

// CausalInputs is the bytes a regatta dispatch is deterministic over;
// Versions is canon-Marshal-sorted so map iteration order never leaks
// into Hash() (spec §3.2 + §3.8).
type CausalInputs struct {
	SpecHash           string            `json:"spec_hash"`
	ModelHash          string            `json:"model_hash"`
	PromptTemplateHash string            `json:"prompt_template_hash"`
	ToolImplHash       string            `json:"tool_impl_hash"`
	Seed               string            `json:"seed"`
	Versions           map[string]string `json:"versions"`
}

// Hash returns the lowercase-hex sha256 of the canon.Marshal form so
// two dispatches with equivalent CausalInputs produce byte-identical
// digests regardless of map iteration order.
func (c CausalInputs) Hash() (string, error) {
	b, err := canon.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("canon marshal: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
