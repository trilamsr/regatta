package alarmwebhook

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestWebhook_RequiresAuthHeader asserts a POST without X-Webhook-Token returns 401, even when WEBHOOK_AUTH_TOKEN is set server-side (R-MEGA-3 LIVE-12).
func TestWebhook_RequiresAuthHeader(t *testing.T) {
	t.Setenv(WebhookAuthEnv, "expected-token")
	h := &Handler{Client: &fakeGitHub{}}
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader("{}"))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("no-header status=%d want 401; body=%q", rr.Code, rr.Body.String())
	}
}

// TestWebhook_AcceptsWithValidToken asserts a matching token reaches the body-parse path (R-MEGA-3 LIVE-12).
func TestWebhook_AcceptsWithValidToken(t *testing.T) {
	t.Setenv(WebhookAuthEnv, "good-token")
	h := &Handler{Client: &fakeGitHub{}}
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader("{not-json"))
	req.Header.Set(WebhookAuthHeader, "good-token")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	// Valid auth → body-parse fires → 400 on malformed JSON. Anything
	// other than 400 means auth was wrongly enforced or wrongly skipped.
	if rr.Code != http.StatusBadRequest {
		t.Errorf("valid-token status=%d want 400 (malformed body); body=%q", rr.Code, rr.Body.String())
	}
}

// TestWebhook_RejectsWrongToken asserts a token mismatch fails closed (R-MEGA-3 LIVE-12).
func TestWebhook_RejectsWrongToken(t *testing.T) {
	t.Setenv(WebhookAuthEnv, "secret-token")
	h := &Handler{Client: &fakeGitHub{}}
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader("{}"))
	req.Header.Set(WebhookAuthHeader, "wrong")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status=%d want 401", rr.Code)
	}
}

// TestWebhook_FailsClosedWhenEnvUnset asserts the gate denies-all when WEBHOOK_AUTH_TOKEN is unset (R-MEGA-3 LIVE-12 fail-closed default).
func TestWebhook_FailsClosedWhenEnvUnset(t *testing.T) {
	t.Setenv(WebhookAuthEnv, "")
	h := &Handler{Client: &fakeGitHub{}}
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader("{}"))
	req.Header.Set(WebhookAuthHeader, "anything")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status=%d want 401", rr.Code)
	}
}
