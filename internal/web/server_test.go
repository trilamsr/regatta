package web

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// newTestHandler builds the package handler under default Dependencies for in-package tests.
func newTestHandler(t *testing.T) http.Handler {
	t.Helper()
	tmpls, err := LoadTemplates(assetsFS)
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	return NewHandler(Dependencies{Templates: tmpls})
}

// B-tier #1 — static asset path serves the embedded htmx-config.js with the immutable Cache-Control.
func TestNewHandler_ServesHealthzAndAssets(t *testing.T) {
	h := newTestHandler(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ui/static/htmx-config.js", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%q", rec.Code, rec.Body.String())
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=86400, immutable" {
		t.Errorf("Cache-Control=%q want %q", cc, "public, max-age=86400, immutable")
	}
	if !strings.Contains(rec.Body.String(), "allowEval") {
		t.Errorf("body missing allowEval=false sentinel; got %q", rec.Body.String())
	}
}

// B-tier #2 — root route renders the layout template skeleton.
func TestNewHandler_ServesLayoutTemplateOnRoot(t *testing.T) {
	h := newTestHandler(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "<title>") {
		t.Errorf("body missing <title>; got %q", body)
	}
	if !strings.Contains(body, "/ui/static/tailwind.min.css") {
		t.Errorf("body missing tailwind stylesheet ref")
	}
	if !strings.Contains(body, "/ui/static/htmx.min.js") {
		t.Errorf("body missing htmx.min.js script ref")
	}
}

// A-tier #8 — every route a NewHandler exposes carries the CSP header.
func TestNewHandler_CSPHeaderOnEveryRoute(t *testing.T) {
	h := newTestHandler(t)
	for _, path := range []string{"/", "/ui/static/htmx-config.js", "/does-not-exist"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		h.ServeHTTP(rec, req)
		if got := rec.Header().Get("Content-Security-Policy"); got != cspExpected {
			t.Errorf("path=%s CSP drift:\n got: %q\nwant: %q", path, got, cspExpected)
		}
		if got := rec.Header().Get("Referrer-Policy"); got != "no-referrer" {
			t.Errorf("path=%s Referrer-Policy=%q want no-referrer", path, got)
		}
	}
}

// A-tier #9 — no numeric literals leak outside const.go (A+ scorecard "zero magic numbers").
// Test walks the package AST so const arithmetic like `8 * 1024` does not false-positive.
func TestConstNoZeroValueMagic(t *testing.T) {
	allowed := map[string]bool{
		"const.go":    true,
		"doc.go":      true,
		"const_test.go": true,
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	fset := token.NewFileSet()
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") || allowed[path] {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok {
				return true
			}
			if lit.Kind != token.INT && lit.Kind != token.FLOAT {
				return true
			}
			// 0 and 1 are loop/index identity values, not domain magic.
			if v, err := strconv.Atoi(lit.Value); err == nil && (v == 0 || v == 1) {
				return true
			}
			t.Errorf("%s:%d: numeric literal %q outside const.go; move to internal/web/const.go", path, fset.Position(lit.Pos()).Line, lit.Value)
			return true
		})
	}
}

// Non-root paths under the catch-all surface as 404 so an operator visiting /typo
// does not get a stub rendered as a real page.
func TestNewHandler_UnknownPathReturnsNotFound(t *testing.T) {
	h := newTestHandler(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/does-not-exist", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status=%d want 404 (body=%q)", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Security-Policy"); got != cspExpected {
		t.Errorf("404 path missing CSP header; got %q", got)
	}
}

// Ensures handler does not crash with a nil Dependencies.RouteRegistrar (seam contract — T6 lands later).
func TestNewHandler_NilRouteRegistrar(t *testing.T) {
	tmpls, err := LoadTemplates(assetsFS)
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	h := NewHandler(Dependencies{Templates: tmpls, RouteRegistrar: nil})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("status=%d want 200", rec.Code)
	}
}

// TestNewHandler_FaviconServedNotFound asserts /favicon.ico does not 404 (MAY-57).
func TestNewHandler_FaviconServedNotFound(t *testing.T) {
	h := newTestHandler(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/favicon.ico", nil)
	h.ServeHTTP(rec, req)
	if rec.Code == http.StatusNotFound {
		t.Errorf("/favicon.ico returned 404; want 200 or 204 (got body=%q)", rec.Body.String())
	}
}

// TestNewHandler_FaviconSVGServed asserts /ui/static/favicon.svg serves the icon (MAY-57).
func TestNewHandler_FaviconSVGServed(t *testing.T) {
	h := newTestHandler(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ui/static/favicon.svg", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200 (body=%q)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "<svg") {
		t.Errorf("favicon.svg missing <svg root; got %q", rec.Body.String())
	}
}

// TestLayout_LinksFavicon asserts layout.tmpl emits <link rel="icon"> (MAY-57).
func TestLayout_LinksFavicon(t *testing.T) {
	h := newTestHandler(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(rec.Body.String(), `rel="icon"`) {
		t.Errorf("layout missing <link rel=\"icon\"> element; got %q", rec.Body.String())
	}
}

// TestTemplates_NoInlineStyleAttrs forbids `style="..."` in templates — CSP style-src hashes do not match attrs (MAY-57).
func TestTemplates_NoInlineStyleAttrs(t *testing.T) {
	entries, err := assetsFS.ReadDir("templates")
	if err != nil {
		t.Fatalf("read templates dir: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, err := assetsFS.ReadFile("templates/" + e.Name())
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		if strings.Contains(string(b), `style="`) {
			t.Errorf("templates/%s: inline style=\"...\" attribute violates CSP style-src; promote to dashboard.css class", e.Name())
		}
	}
}

// TestTemplates_NoHxOnHandlers forbids hx-on:* attrs — htmx new Function() fires evalDisallowedError under CSP (MAY-57).
func TestTemplates_NoHxOnHandlers(t *testing.T) {
	entries, err := assetsFS.ReadDir("templates")
	if err != nil {
		t.Fatalf("read templates dir: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, err := assetsFS.ReadFile("templates/" + e.Name())
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		if strings.Contains(string(b), "hx-on:") {
			t.Errorf("templates/%s: hx-on:* handler triggers htmx:evalDisallowedError under CSP; wire via dashboard JS", e.Name())
		}
	}
}

// helper to drain bodies during table-driven asserts.
var _ = io.Discard
