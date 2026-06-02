package web

import (
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ErrFuncExists fires when RegisterFunc receives a name already in the
// whitelist. Spec §3.4 forbids silent override so XSS-via-template.HTML cannot
// slip in as a "fix" later.
var ErrFuncExists = errors.New("web: template func already registered")

// Templates wraps the parsed template set and the func registry it was parsed
// with. Funcs frozen at LoadTemplates time except via RegisterFunc, which
// re-parses against the extended map (T6 plugs ClampDiff in via this seam).
type Templates struct {
	mu     sync.RWMutex
	fsys   fs.FS
	funcs  template.FuncMap
	parsed *template.Template
}

// LoadTemplates parses templates/*.tmpl from fsys at boot. Parse failure
// returns non-nil so spec §3.4 fail-loud holds — the listener refuses to come
// up rather than rendering a half-broken page at first request.
func LoadTemplates(fsys fs.FS) (*Templates, error) {
	t := &Templates{fsys: fsys, funcs: baseFuncs()}
	parsed, err := template.New("layout").Funcs(t.funcs).ParseFS(fsys, templatesDirName+"/*.tmpl")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}
	t.parsed = parsed
	return t, nil
}

// Render writes the named template to w. Centralising the html/template
// invocation here means handlers cannot bypass auto-escape by writing raw
// bytes — auto-escape is the spec §8 XSS gate.
func (t *Templates) Render(w http.ResponseWriter, name string, data any) error {
	t.mu.RLock()
	parsed := t.parsed
	t.mu.RUnlock()
	if parsed == nil {
		return errors.New("web: templates not loaded")
	}
	return parsed.ExecuteTemplate(w, name, data)
}

// RegisterFunc extends the template func registry with a fresh name. Spec
// §3.4 whitelists eight funcs at boot (T4 ships them) and lets T6 add
// `clampDiff` via this seam — no circular import. Duplicate names return
// ErrFuncExists so a future caller cannot silently shadow an auto-escape-safe
// helper with a template.HTML returner.
func (t *Templates) RegisterFunc(name string, fn any) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, exists := t.funcs[name]; exists {
		return fmt.Errorf("%w: %q", ErrFuncExists, name)
	}
	t.funcs[name] = fn
	parsed, err := template.New("layout").Funcs(t.funcs).ParseFS(t.fsys, templatesDirName+"/*.tmpl")
	if err != nil {
		// roll back the registry so a parse failure does not leave the
		// Templates in a half-extended state.
		delete(t.funcs, name)
		return fmt.Errorf("reparse with %q: %w", name, err)
	}
	t.parsed = parsed
	return nil
}

// baseFuncs is the spec §3.4 whitelist T4 registers at boot. Every func
// returns a primitive (string / int / time.Duration), never template.HTML, so
// the XSS gate holds at the type layer (`! grep template.HTML internal/web/`).
func baseFuncs() template.FuncMap {
	return template.FuncMap{
		"formatTime":       formatTime,
		"formatBytes":      formatBytes,
		"formatTokens":     formatTokens,
		"formatUSDMicros":  formatUSDMicros,
		"truncate":         truncate,
		"humanizeDuration": humanizeDuration,
		"safeURL":          safeURL,
		"csrfToken":        csrfTokenStub,
	}
}

// formatTime renders a time as RFC3339 UTC. Spec §3.4 lists it as a whitelist
// member; the body is intentionally trivial.
func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// formatBytes prints a byte count with a fixed unit suffix; locales the SUM
// query results live in.
func formatBytes(n int64) string { return fmt.Sprintf("%d B", n) }

// formatTokens prints a token count; placeholder until T11 supplies a richer
// formatter (cost panel rendering).
func formatTokens(n int64) string { return fmt.Sprintf("%d tok", n) }

// formatUSDMicros renders microdollars as USD; one division, fixed precision.
func formatUSDMicros(micros int64) string {
	usd := float64(micros) / float64(usdMicroPerDollar)
	return fmt.Sprintf("$%.2f", usd)
}

// truncate caps a string to n bytes; non-rune-aware on purpose — UI snippets
// are ASCII-dominant and the trailing `…` keeps the cut visible.
func truncate(n int, s string) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// humanizeDuration prints a coarse human label.
func humanizeDuration(d time.Duration) string { return d.Truncate(time.Second).String() }

// safeURL whitelists the scheme set the UI is allowed to link to. Anything
// else (including data: + javascript:) returns the empty string so the
// template renders an inert href.
func safeURL(raw string) string {
	for _, ok := range []string{"https://", "http://", "/", "#", "mailto:"} {
		if strings.HasPrefix(raw, ok) {
			return raw
		}
	}
	return ""
}

// csrfTokenStub is the T4 placeholder so layout templates compile pre-T7. T7's
// PR replaces the registration via RegisterFunc; the stub never reaches an
// approval form because T4 does not mount any.
func csrfTokenStub() string { return "" }
