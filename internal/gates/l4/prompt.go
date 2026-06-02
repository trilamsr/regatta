package l4

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"strings"
	"text/template"
)

//go:embed prompts/adversarial.tmpl
var adversarialTmpl string

// promptSHA is the SHA-256 of the embedded template body, computed
// once at package init. Stamped onto every InvokeResponse.PromptSHA
// so audit replay can match an emitted GateResult to the exact prompt
// the model saw, even after the template evolves in main.
var promptSHA = func() string {
	sum := sha256.Sum256([]byte(adversarialTmpl))
	return "sha256:" + hex.EncodeToString(sum[:])
}()

// promptTemplate parses the embedded template once. Re-parsing per
// Run would burn ~50us+GC; cache locally.
var promptTemplate = template.Must(
	template.New("adversarial").Funcs(template.FuncMap{
		"indent": indent,
	}).Parse(adversarialTmpl),
)

// RenderPrompt returns the fully-substituted prompt text + the
// SHA-256 pin of the embedded template body. Diff is clipped to
// maxChars before substitution so the model never sees the unclipped
// blob even if the caller forgot to apply MaxDiffChars upstream.
func RenderPrompt(in Input, maxChars int) (string, string, error) {
	diff := in.Diff
	if maxChars > 0 && len(diff) > maxChars {
		diff = diff[:maxChars]
	}
	view := struct {
		PRSHA, BaseSHA, RepoRoot, Diff, Spec, Scorecard string
		MaxDiffChars                                    int
	}{
		PRSHA:        in.PRSHA,
		BaseSHA:      in.BaseSHA,
		RepoRoot:     in.RepoRoot,
		Diff:         diff,
		Spec:         in.Spec,
		Scorecard:    in.Scorecard,
		MaxDiffChars: maxChars,
	}
	var buf bytes.Buffer
	if err := promptTemplate.Execute(&buf, view); err != nil {
		return "", "", fmt.Errorf("l4 prompt: render: %w", err)
	}
	return buf.String(), promptSHA, nil
}

// PromptSHA exposes the pinned template SHA so callers that build
// prompts manually (e.g. adapter dry-runs) can stamp the same value
// without re-rendering.
func PromptSHA() string { return promptSHA }

// indent prefixes every line of s with n spaces. Used by the
// adversarial template to nest the diff + spec + scorecard blocks
// under their section headers.
func indent(n int, s string) string {
	pad := strings.Repeat(" ", n)
	if s == "" {
		return pad
	}
	return pad + strings.ReplaceAll(s, "\n", "\n"+pad)
}
