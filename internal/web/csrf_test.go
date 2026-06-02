package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// passThrough is a 200-OK handler used as the next link in middleware
// chains; it lets tests assert the middleware did NOT short-circuit.
func passThrough() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestCSRFMiddleware_RejectsOnMismatch(t *testing.T) {
	h := CSRFMiddleware(passThrough())
	form := strings.NewReader("csrf=deadbeefdeadbeefdeadbeefdeadbeef")
	r := httptest.NewRequest(http.MethodPost, "/approve/x", form)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: "1111111111111111111111111111ffff"})

	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d want 403", w.Code)
	}
}

func TestCSRFMiddleware_ConstantTimeCompare(t *testing.T) {
	src, err := readFileForGrep("csrf.go")
	if err != nil {
		t.Fatalf("read csrf.go: %v", err)
	}
	if !strings.Contains(src, "subtle.ConstantTimeCompare") {
		t.Fatalf("csrf.go does not call subtle.ConstantTimeCompare (constant-time compare required)")
	}

	h := CSRFMiddleware(passThrough())
	for _, form := range []string{
		// bit-1 flip
		"0fffffffffffffffffffffffffffffff",
		// last-byte flip
		"ffffffffffffffffffffffffffffff0f",
	} {
		r := httptest.NewRequest(http.MethodPost, "/approve/x",
			strings.NewReader("csrf="+form))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: "ffffffffffffffffffffffffffffffff"})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatalf("mismatched form %q: status = %d want 403", form, w.Code)
		}
	}
}

func TestCSRFMiddleware_RandomizedCookieFormPairs(t *testing.T) {
	h := CSRFMiddleware(passThrough())
	rapid.Check(t, func(rp *rapid.T) {
		cookie := rapid.StringMatching(`[0-9a-f]{32}`).Draw(rp, "cookie")
		form := rapid.StringMatching(`[0-9a-f]{32}`).Draw(rp, "form")

		r := httptest.NewRequest(http.MethodPost, "/approve/x",
			strings.NewReader("csrf="+form))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: cookie})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)

		match := cookie == form
		got := w.Code
		switch {
		case match && got != http.StatusOK:
			rp.Fatalf("matching cookie==form rejected: code=%d", got)
		case !match && got != http.StatusForbidden:
			rp.Fatalf("mismatched cookie/form not rejected: code=%d", got)
		}
	})
}
