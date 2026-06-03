package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// readFileForGrep loads a sibling .go file for source-pin assertions
// (e.g. TestCSRFMiddleware_ConstantTimeCompare proves the impl uses
// subtle.ConstantTimeCompare by literal text match).
func readFileForGrep(name string) (string, error) {
	b, err := os.ReadFile(name)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func TestOriginCheck_RejectsForeignOrigin(t *testing.T) {
	h := OriginCheck("", passThrough())
	r := httptest.NewRequest(http.MethodPost, "/approve/x", nil)
	r.Host = "regatta.local"
	r.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("foreign Origin: status = %d want 403", w.Code)
	}
}

func TestOriginCheck_RejectsMissingOriginOnPost(t *testing.T) {
	h := OriginCheck("", passThrough())
	r := httptest.NewRequest(http.MethodPost, "/approve/x", nil)
	r.Host = "regatta.local"
	// no Origin header
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("missing Origin POST: status = %d want 403", w.Code)
	}
}

func TestOriginCheck_GETUnaffected(t *testing.T) {
	h := OriginCheck("", passThrough())
	// GET with missing Origin → must pass.
	r := httptest.NewRequest(http.MethodGet, "/approve/x", nil)
	r.Host = "regatta.local"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("GET no Origin: status = %d want 200", w.Code)
	}

	// GET with foreign Origin → still passes (POST-only gate).
	r2 := httptest.NewRequest(http.MethodGet, "/approve/x", nil)
	r2.Host = "regatta.local"
	r2.Header.Set("Origin", "https://evil.example")
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, r2)
	if w2.Code != http.StatusOK {
		t.Fatalf("GET foreign Origin: status = %d want 200", w2.Code)
	}
}

func TestOriginCheck_PublicHostOverride(t *testing.T) {
	h := OriginCheck("public.example", passThrough())
	r := httptest.NewRequest(http.MethodPost, "/approve/x", nil)
	r.Host = "internal-pod-7" // reverse-proxy inner host
	r.Header.Set("Origin", "https://public.example")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("public-host match: status = %d want 200", w.Code)
	}
}

// TestOrigin_PublicHostOverride_AllowsExternalHostname asserts #304 reverse-proxy host override.
func TestOrigin_PublicHostOverride_AllowsExternalHostname(t *testing.T) {
	h := OriginCheck("regatta.example.com", passThrough())
	r := httptest.NewRequest(http.MethodPost, "/approve/x", nil)
	r.Host = "pod-inner-1.svc:8080"
	r.Header.Set("Origin", "https://regatta.example.com")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("reverse-proxy public-host: status = %d want 200", w.Code)
	}
}

func TestOriginCheck_AcceptsMatchingOrigin(t *testing.T) {
	h := OriginCheck("", passThrough())
	r := httptest.NewRequest(http.MethodPost, "/approve/x", nil)
	r.Host = "regatta.local"
	r.Header.Set("Origin", "https://regatta.local")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("matching Origin: status = %d want 200", w.Code)
	}
}
