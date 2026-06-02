package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// cspExpected pins spec §3.7 byte-for-byte; drift between this literal and the production CSPHeader trips test 3.
const cspExpected = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'"

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
