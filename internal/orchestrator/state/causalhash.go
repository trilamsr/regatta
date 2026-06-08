package state

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/trilamsr/regatta/internal/canon"
)

// CausalInputs are the bytes a regatta dispatch is deterministic over; Versions is sorted by canon.Marshal so map iteration order never leaks into Hash(). Spec §3.2 + §3.8.
type CausalInputs struct {
	SpecHash           string            `json:"spec_hash"`
	ModelHash          string            `json:"model_hash"`
	PromptTemplateHash string            `json:"prompt_template_hash"`
	ToolImplHash       string            `json:"tool_impl_hash"`
	Seed               string            `json:"seed"`
	Versions           map[string]string `json:"versions"`
}

// Hash returns lowercase-hex sha256 of canon.Marshal(c).
func (c CausalInputs) Hash() (string, error) {
	b, err := canon.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("canon marshal: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
