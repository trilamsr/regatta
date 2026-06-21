package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// htmxInlineStyleHash is the sha256 (base64) of the inline `<style>` block htmx
// 2.0.4 injects at boot (indicatorClass + requestClass rules); CSP `style-src`
// must allowlist it or every page load logs a violation per `feedback_root_cause`.
const htmxInlineStyleHash = "'sha256-bsV5JivYxvGywDAZ22EZJKBFip65Ng9xoJVLbBg7bdo='"

const cspExpected = "default-src 'self'; script-src 'self'; style-src 'self' " + htmxInlineStyleHash + "; img-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'"

// B-tier #3.
func TestCSPMiddleware_SetsAllSpecHeadersByteEqual(t *testing.T) {
	h := CSPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Security-Policy"); got != cspExpected {
		t.Errorf("CSP header drift:\n got: %q\nwant: %q", got, cspExpected)
	}
	for _, c := range []struct {
		header, want string
	}{
		{"Referrer-Policy", "no-referrer"},
		{"X-Content-Type-Options", "nosniff"},
		{"X-Frame-Options", "DENY"},
	} {
		if got := rec.Header().Get(c.header); got != c.want {
			t.Errorf("%s=%q want %q", c.header, got, c.want)
		}
	}
}

// B-tier #4 — no third-party origins in CSP (`://` absent rules out URLs).
func TestCSPMiddleware_NoThirdPartyOrigins(t *testing.T) {
	if strings.Contains(CSPHeader, "://") {
		t.Errorf("CSPHeader contains a URL (third-party origin); got %q", CSPHeader)
	}
}

// TestCSPHeader_AllowsHtmxInlineStyleHash asserts the htmx 2.0.4 boot `<style>` sha256 is allowlisted (MAY-57).
func TestCSPHeader_AllowsHtmxInlineStyleHash(t *testing.T) {
	if !strings.Contains(CSPHeader, htmxInlineStyleHash) {
		t.Errorf("CSPHeader missing htmx inline-style hash %s; got %q", htmxInlineStyleHash, CSPHeader)
	}
}

// TestCSPHeader_NoUnsafeInline pins style-src to hash-only (no 'unsafe-inline' / 'unsafe-eval') per MAY-57.
func TestCSPHeader_NoUnsafeInline(t *testing.T) {
	if strings.Contains(CSPHeader, "'unsafe-inline'") {
		t.Errorf("CSPHeader leaked 'unsafe-inline'; got %q", CSPHeader)
	}
	if strings.Contains(CSPHeader, "'unsafe-eval'") {
		t.Errorf("CSPHeader leaked 'unsafe-eval'; got %q", CSPHeader)
	}
}
