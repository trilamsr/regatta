package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newApprovalTestHandler mounts RegisterApprovalRoutes on a bare mux with a
// minimal Dependencies set so handler tests exercise the real route table.
func newApprovalTestHandler(t *testing.T, deps Dependencies) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	RegisterApprovalRoutes(mux, deps)
	return mux
}

// TestApprovalPageHandler_NoCookieReturnsErrorPage asserts GET /approve/{aid} with no token cookie returns 401 + token_invalid (MAY-116).
func TestApprovalPageHandler_NoCookieReturnsErrorPage(t *testing.T) {
	kr, _, _, _ := testKeyring(t)
	tmpls, err := LoadTemplates(AssetsFS())
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	deps := Dependencies{
		Keyring:   kr,
		Templates: tmpls,
		Clock:     func() time.Time { return time.Unix(1_700_000_000, 0) },
	}
	h := newApprovalTestHandler(t, deps)

	r := httptest.NewRequest(http.MethodGet, "/approve/01H8AID", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d want %d", w.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(w.Body.String(), "token_invalid") {
		t.Fatalf("body missing token_invalid sentinel: %q", w.Body.String())
	}
}
