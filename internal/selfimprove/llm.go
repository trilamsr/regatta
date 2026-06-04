package selfimprove

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// MaxLLMTokens caps every nightly call hard per spec §7.4 + adversarial
// finding "LLM hallucinating heuristic" — the cap is enforced inside
// LLMScanner.Scan regardless of model selection so a Haiku→Sonnet
// upgrade cannot silently 10x the bill.
const MaxLLMTokens = 4000

// LLMComplete is the function shape the Anthropic API call satisfies.
// Tests pass a closure; production wiring lives behind a build tag so
// non-LLM builds (CI, hermetic test envs) stay zero-dep.
type LLMComplete func(ctx context.Context, prompt string, maxTokens int) (string, error)

// LLMScanner runs the nightly path: build the digest prompt, call
// Haiku with a hard token cap, write the YAML proposal to disk for
// operator review. Per spec §3.1 + §7.2 it NEVER files issues — that
// invariant is guarded by TestLLMScanner_NeverAutoFiles_OnlyWritesYAML.
type LLMScanner struct {
	Client    LLMComplete
	OutputDir string
	Clock     func() time.Time
}

// Scan reads the digest, prompts the LLM under a hard MaxLLMTokens
// cap, and writes the parsed YAML proposal to
// `<OutputDir>/proposed-rules-<date>.yaml`. Returns the written path
// so the cron wrapper can surface it to the operator.
func (s *LLMScanner) Scan(ctx context.Context, digest string, promptTemplate string) (string, error) {
	if s.Client == nil {
		return "", errors.New("selfimprove: LLM client not configured")
	}
	if s.OutputDir == "" {
		return "", errors.New("selfimprove: output dir not set")
	}
	clock := s.Clock
	if clock == nil {
		clock = time.Now
	}

	prompt := fmt.Sprintf("%s\n\nDIGEST:\n%s\n", promptTemplate, digest)
	raw, err := s.Client(ctx, prompt, MaxLLMTokens)
	if err != nil {
		return "", fmt.Errorf("selfimprove: llm complete: %w", err)
	}

	if err := os.MkdirAll(s.OutputDir, 0o750); err != nil {
		return "", fmt.Errorf("selfimprove: mkdir: %w", err)
	}
	path := filepath.Join(s.OutputDir,
		fmt.Sprintf("proposed-rules-%s.yaml", clock().UTC().Format("2006-01-02")))
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		return "", fmt.Errorf("selfimprove: write proposal: %w", err)
	}
	return path, nil
}
