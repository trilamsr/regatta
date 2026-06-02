package l4

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"strings"
	"sync/atomic"
	"text/template"
)

//go:embed prompts/adversarial.tmpl
var adversarialTmpl string

// active is the live prompt slot served by RenderPrompt + PromptSHA.
// atomic.Pointer is the hot-reload swap primitive shared with W8
// (internal/authz). Initialized at package init from the embed.FS;
// disk override + SIGHUP/fsnotify reload swap it via Reloader.
var active atomic.Pointer[promptSlot]

type promptSlot struct {
	body string
	sha  string
	tmpl *template.Template
}

func init() {
	slot, err := parseSlot(adversarialTmpl)
	if err != nil {
		// Embed bytes are compile-time checked; a parse-fail is a developer bug.
		panic(fmt.Errorf("l4 prompt: embedded template parse: %w", err))
	}
	active.Store(slot)
}

func parseSlot(body string) (*promptSlot, error) {
	t, err := template.New("adversarial").Funcs(template.FuncMap{
		"indent": indent,
	}).Parse(body)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(body))
	return &promptSlot{
		body: body,
		sha:  "sha256:" + hex.EncodeToString(sum[:]),
		tmpl: t,
	}, nil
}

// RenderPrompt returns the fully-substituted prompt text + the SHA-256
// pin of the active template body. Diff is clipped to maxChars before
// substitution so the model never sees the unclipped blob even when the
// caller forgot to apply MaxDiffChars upstream. The active slot is
// loaded atomically so a concurrent hot-reload swap is observed without
// torn reads — the SHA returned always matches the body just rendered.
func RenderPrompt(in Input, maxChars int) (string, string, error) {
	slot := active.Load()
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
	if err := slot.tmpl.Execute(&buf, view); err != nil {
		return "", "", fmt.Errorf("l4 prompt: render: %w", err)
	}
	return buf.String(), slot.sha, nil
}

// PromptSHA exposes the active template SHA so callers stamping
// telemetry (gate.go) or building prompts manually (adapter dry-runs)
// match the body the model actually saw, even after a hot-reload swap.
func PromptSHA() string { return active.Load().sha }

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
