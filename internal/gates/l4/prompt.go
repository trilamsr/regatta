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

// cacheBreakpointMarker splits the static cacheable prefix from the
// PR-specific dynamic suffix in adversarial.tmpl. The Anthropic adapter
// emits the prefix as a `system` block tagged with cache_control so
// repeat L4 calls hit the prompt cache (#852).
const cacheBreakpointMarker = "<<<REGATTA_L4_CACHE_BREAKPOINT>>>"

// active is the live prompt slot served by RenderPrompt + PromptSHA.
// atomic.Pointer is the hot-reload swap primitive shared with W8
// (internal/authz). Initialized at package init from the embed.FS;
// disk override + SIGHUP/fsnotify reload swap it via Reloader.
var active atomic.Pointer[promptSlot]

type promptSlot struct {
	body   string
	sha    string
	tmpl   *template.Template
	static string
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
	static := body
	if idx := strings.Index(body, cacheBreakpointMarker); idx >= 0 {
		static = strings.TrimRight(body[:idx], "\n")
	}
	return &promptSlot{
		body:   body,
		sha:    "sha256:" + hex.EncodeToString(sum[:]),
		tmpl:   t,
		static: static,
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
	out := strings.Replace(buf.String(), cacheBreakpointMarker+"\n", "", 1)
	out = strings.Replace(out, cacheBreakpointMarker, "", 1)
	return out, slot.sha, nil
}

// RenderPromptSplit returns the static cacheable prefix (system block)
// and the dynamic per-PR suffix (user message) separately so the
// Anthropic adapter can tag the prefix with cache_control=ephemeral
// for prompt-cache reuse across PRs (#852). The SHA still pins the
// full concatenated body so audit-replay stays exact.
func RenderPromptSplit(in Input, maxChars int) (string, string, string, error) {
	slot := active.Load()
	full, sha, err := RenderPrompt(in, maxChars)
	if err != nil {
		return "", "", "", err
	}
	if slot.static == slot.body || slot.static == "" {
		return "", full, sha, nil
	}
	dyn := strings.TrimPrefix(full, slot.static)
	dyn = strings.TrimLeft(dyn, "\n")
	return slot.static, dyn, sha, nil
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
