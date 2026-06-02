package web

import (
	"crypto/rand"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/canon"
)

// testKeyring builds a deterministic canon.Keyring + reviewer + helper
// that mints valid wire tokens. Lifted out so every auth test sees an
// identical fixture surface.
func testKeyring(t *testing.T) (canon.Keyring, string, string, []byte) {
	t.Helper()
	kid := "kid-test"
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("seed key: %v", err)
	}
	kr := canon.MapKeyring{kid: key}
	return kr, kid, "alice@co", key
}

func mintWire(t *testing.T, kr canon.Keyring, kid, reviewer, aid, wi string, window time.Time) string {
	t.Helper()
	wire, _, err := canon.MintToken(kr, kid, canon.TokenPayload{
		WI:       wi,
		AID:      aid,
		Reviewer: reviewer,
		Window:   window.Unix(),
	}, rand.Reader)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	return wire
}

func TestPrincipalFromRequest_HappyPath(t *testing.T) {
	kr, kid, reviewer, _ := testKeyring(t)
	now := time.Unix(1_700_000_000, 0)
	wire := mintWire(t, kr, kid, reviewer, "01H8AID", "wi-1", now.Add(5*time.Minute))

	r := httptest.NewRequest(http.MethodGet, "/approve/01H8AID", nil)
	r.AddCookie(&http.Cookie{Name: ApprovalTokenCookieName, Value: wire}) //nolint:gosec // G124: test fixture replays server-set cookie; production attrs validated by TestRedeemHandler_HappyPathSetsCookiesAnd303

	p, payload, err := PrincipalFromRequest(r, kr, now)
	if err != nil {
		t.Fatalf("PrincipalFromRequest err: %v", err)
	}
	if p.ID != reviewer {
		t.Fatalf("Principal.ID = %q want %q", p.ID, reviewer)
	}
	if p.Tenant != "default" {
		t.Fatalf("Principal.Tenant = %q want default", p.Tenant)
	}
	if p.Roles != nil {
		t.Fatalf("Principal.Roles = %v want nil", p.Roles)
	}
	if payload.AID != "01H8AID" {
		t.Fatalf("payload.AID = %q", payload.AID)
	}
}

func TestPrincipalFromRequest_MissingCookieReturnsErrCookieMissing(t *testing.T) {
	kr, _, _, _ := testKeyring(t)
	r := httptest.NewRequest(http.MethodGet, "/approve/x", nil)
	_, _, err := PrincipalFromRequest(r, kr, time.Now())
	if !errors.Is(err, ErrCookieMissing) {
		t.Fatalf("err = %v want ErrCookieMissing", err)
	}
}

func TestPrincipalFromRequest_ExpiredTokenReturnsErrTokenExpired(t *testing.T) {
	kr, kid, reviewer, _ := testKeyring(t)
	past := time.Unix(1_700_000_000, 0)
	wire := mintWire(t, kr, kid, reviewer, "01H8AID", "wi-1", past.Add(-time.Minute))

	r := httptest.NewRequest(http.MethodGet, "/approve/01H8AID", nil)
	r.AddCookie(&http.Cookie{Name: ApprovalTokenCookieName, Value: wire}) //nolint:gosec // G124: test fixture replays server-set cookie

	_, _, err := PrincipalFromRequest(r, kr, past)
	if !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("err = %v want ErrTokenExpired", err)
	}
}

func TestRedeemHandler_HappyPathSetsCookiesAnd303(t *testing.T) {
	kr, kid, reviewer, _ := testKeyring(t)
	now := time.Unix(1_700_000_000, 0)
	aid := "01H8AID"
	wire := mintWire(t, kr, kid, reviewer, aid, "wi-1", now.Add(15*time.Minute))

	deps := Dependencies{
		Keyring: kr,
		Clock:   func() time.Time { return now },
		Config:  Config{DecisionWindow: 15 * time.Minute},
	}
	h := RedeemHandler(deps)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/approve/redeem?t="+wire+"&r="+reviewer, nil)
	h.ServeHTTP(w, r)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d want 303", w.Code)
	}
	loc := w.Header().Get("Location")
	if loc != "/approve/"+aid {
		t.Fatalf("Location = %q want /approve/%s", loc, aid)
	}

	setCookies := w.Result().Cookies()
	var authC, csrfC *http.Cookie
	for _, c := range setCookies {
		switch c.Name {
		case ApprovalTokenCookieName:
			authC = c
		case CSRFCookieName:
			csrfC = c
		}
	}
	if authC == nil {
		t.Fatalf("missing %s cookie; got %+v", ApprovalTokenCookieName, setCookies)
	}
	if csrfC == nil {
		t.Fatalf("missing %s cookie; got %+v", CSRFCookieName, setCookies)
	}
	if !authC.HttpOnly || !authC.Secure || authC.SameSite != http.SameSiteStrictMode {
		t.Fatalf("auth cookie attrs wrong: %+v", authC)
	}
	if authC.Path != "/approve/"+aid {
		t.Fatalf("auth cookie Path = %q want /approve/%s", authC.Path, aid)
	}
	if authC.MaxAge != int((15 * time.Minute).Seconds()) {
		t.Fatalf("auth cookie MaxAge = %d want %d", authC.MaxAge, int((15 * time.Minute).Seconds()))
	}
	if !csrfC.HttpOnly || !csrfC.Secure || csrfC.SameSite != http.SameSiteStrictMode {
		t.Fatalf("csrf cookie attrs wrong: %+v", csrfC)
	}
	if csrfC.Path != "/approve/"+aid {
		t.Fatalf("csrf cookie Path = %q want /approve/%s", csrfC.Path, aid)
	}
	if len(csrfC.Value) != 32 {
		t.Fatalf("csrf value len = %d want 32 (hex of 16 bytes)", len(csrfC.Value))
	}
}

// TestRedeemHandler_NoRParam_DerivesReviewerFromClaim asserts the cookie-bound flow no longer requires `?r=<reviewer>` — the signed claim is the source of identity (issue #305).
func TestRedeemHandler_NoRParam_DerivesReviewerFromClaim(t *testing.T) {
	kr, kid, reviewer, _ := testKeyring(t)
	now := time.Unix(1_700_000_000, 0)
	aid := "01H8AID"
	wire := mintWire(t, kr, kid, reviewer, aid, "wi-1", now.Add(15*time.Minute))

	deps := Dependencies{
		Keyring: kr,
		Clock:   func() time.Time { return now },
		Config:  Config{DecisionWindow: 15 * time.Minute},
	}
	h := RedeemHandler(deps)

	w := httptest.NewRecorder()
	// No &r= query param — handler must derive reviewer from claim.
	r := httptest.NewRequest(http.MethodGet, "/approve/redeem?t="+wire, nil)
	h.ServeHTTP(w, r)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d want 303 (body=%q)", w.Code, w.Body.String())
	}
	loc := w.Header().Get("Location")
	if loc != "/approve/"+aid {
		t.Fatalf("Location = %q want /approve/%s", loc, aid)
	}
	// Hint cookie must still be set from claim (operator UX, not auth).
	var hintC *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == reviewerHintCookieName {
			hintC = c
		}
	}
	if hintC == nil || hintC.Value != reviewer {
		t.Fatalf("reviewer-hint cookie = %+v want value=%q", hintC, reviewer)
	}
}

func TestRedeemHandler_TokenInvalidReturnsTypedSentinel(t *testing.T) {
	kr, _, _, _ := testKeyring(t)
	deps := Dependencies{
		Keyring: kr,
		Clock:   time.Now,
		Config:  Config{DecisionWindow: 5 * time.Minute},
	}
	h := RedeemHandler(deps)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/approve/redeem?t=not-a-valid-token&r=alice@co", nil)
	h.ServeHTTP(w, r)

	if w.Code < 400 || w.Code >= 500 {
		t.Fatalf("status = %d want 4xx", w.Code)
	}
	if !strings.Contains(w.Body.String(), "token_invalid") {
		t.Fatalf("body missing token_invalid sentinel string: %q", w.Body.String())
	}
}
