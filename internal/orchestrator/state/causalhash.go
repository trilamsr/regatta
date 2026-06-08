package state

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/trilamsr/regatta/internal/canon"
)

// CausalInputs are the bytes a regatta dispatch is deterministic over.
// Hash() is the value runs.causal_hash carries — folding by causal_hash
// surfaces every replay of the same agent over the same world. Spec
// §3.2 + §3.8 (rerun-from-hash). Versions is a sorted-key map via
// canon.Marshal so insertion order never leaks into the digest.
type CausalInputs struct {
	SpecHash           string            `json:"spec_hash"`
	ModelHash          string            `json:"model_hash"`
	PromptTemplateHash string            `json:"prompt_template_hash"`
	ToolImplHash       string            `json:"tool_impl_hash"`
	Seed               string            `json:"seed"`
	Versions           map[string]string `json:"versions"`
}

// Hash returns the lowercase-hex sha256 of canon.Marshal(c).
func (c CausalInputs) Hash() (string, error) {
	b, err := canon.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("canon marshal: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
