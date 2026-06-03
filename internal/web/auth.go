// Package web hosts the operator UI HTTP surface (spec §3.6); auth.go owns Principal + sentinels + cookie-bound HMAC.
package web

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/trilamsr/regatta/internal/canon/approvaltoken"
)

// Principal is the identity contract consumed by approval handlers.
type Principal struct {
	ID     string
	Tenant string
	Roles  []string
}

// Sentinel errors. Package-level vars so callers can errors.Is-match
// without string compare. The sentinel name == operator-facing slug
// rendered by T6's approval_error.tmpl.
var (
	// ErrCookieMissing signals no regatta_approval_token cookie on the request.
	ErrCookieMissing = errors.New("web: approval token cookie missing")
	// ErrTokenInvalid wraps approvaltoken.ErrTokenInvalid (framing / b64 / kid scan).
	ErrTokenInvalid = errors.New("web: approval token invalid")
	// ErrTokenExpired wraps approvaltoken.ErrTokenExpired (now >= window).
	ErrTokenExpired = errors.New("web: approval token expired")
	// ErrTokenReplay signals reuse of a consumed token (decide path sentinel).
	ErrTokenReplay = errors.New("web: approval token already consumed")
	// ErrUnknownKey wraps approvaltoken.ErrUnknownKeyID (kid not in keyring).
	ErrUnknownKey = errors.New("web: approval token unknown key_id")
	// ErrNotReviewer wraps approval.ErrNotReviewer (reviewer not in approval snapshot).
	ErrNotReviewer = errors.New("web: reviewer not in approval snapshot")
	// ErrSelfReview wraps approval.ErrSelfReview (reviewer == requested_by under prevent_self_review).
	ErrSelfReview = errors.New("web: self-review blocked by policy")
)

// reviewerHintCookieName retains its server-set lifecycle for operator
// UX (browser dev-tools surface a human-readable reviewer slug next to
// the opaque HMAC token). The cookie is NOT load-bearing for
// verification — approvaltoken.VerifyToken derives reviewer from the signed
// claim (issue #305). Removing the cookie would not weaken auth; it is
// kept solely so operators can eyeball "who is this token for?" without
// decoding the wire bytes.
const reviewerHintCookieName = "regatta_reviewer_hint"

// PrincipalFromRequest authenticates the cookie-bound approval token via approvaltoken.VerifyToken and returns the Principal claim derived from the HMAC-signed reviewer field.
func PrincipalFromRequest(r *http.Request, kr approvaltoken.Keyring, now time.Time) (Principal, approvaltoken.TokenPayload, error) {
	c, err := r.Cookie(ApprovalTokenCookieName)
	if err != nil || c.Value == "" {
		return Principal{}, approvaltoken.TokenPayload{}, ErrCookieMissing
	}
	// Empty expectReviewer → derive identity from the signed claim
	// (issue #305). HMAC compare inside approvaltoken.VerifyToken runs first,
	// so a forged token still fails before the derivation branch.
	payload, verr := approvaltoken.VerifyToken(kr, c.Value, "", now)
	if verr != nil {
		return Principal{}, approvaltoken.TokenPayload{}, mapCanonErr(verr)
	}
	return Principal{ID: payload.Reviewer, Tenant: "default"}, payload, nil
}

// mapCanonErr folds canon sentinels into the web-layer sentinel set so
// errors.Is-match works against the web sentinels without leaking
// canon's internal envelope-vs-mismatch distinction.
func mapCanonErr(err error) error {
	switch {
	case errors.Is(err, approvaltoken.ErrTokenExpired):
		return fmt.Errorf("%w: %w", ErrTokenExpired, err)
	case errors.Is(err, approvaltoken.ErrUnknownKeyID):
		return fmt.Errorf("%w: %w", ErrUnknownKey, err)
	case errors.Is(err, approvaltoken.ErrTokenInvalid), errors.Is(err, approvaltoken.ErrUnverifiable):
		return fmt.Errorf("%w: %w", ErrTokenInvalid, err)
	default:
		return fmt.Errorf("%w: %w", ErrTokenInvalid, err)
	}
}

// RedeemHandler verifies ?t=<wire> (the reviewer is derived from the signed claim per issue #305), sets HMAC + CSRF cookies (spec §3.6.1), and 303s to /approve/<aid>.
func RedeemHandler(deps Dependencies) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wire := r.URL.Query().Get("t")
		if wire == "" {
			writeSentinelError(w, "token_invalid", http.StatusBadRequest)
			return
		}
		now := time.Now()
		if deps.Clock != nil {
			now = deps.Clock()
		}
		// Empty expectReviewer → derive identity from claim (issue #305).
		// HMAC verify inside approvaltoken.VerifyToken precedes the derivation
		// branch, so a forged wire still trips ErrUnverifiable.
		payload, err := approvaltoken.VerifyToken(deps.Keyring, wire, "", now)
		if err != nil {
			writeSentinelError(w, sentinelSlug(mapCanonErr(err)), errStatus(err))
			return
		}

		path := "/approve/" + payload.AID
		maxAge := int(deps.Config.DecisionWindow.Seconds())
		http.SetCookie(w, &http.Cookie{
			Name:     ApprovalTokenCookieName,
			Value:    wire,
			Path:     path,
			MaxAge:   maxAge,
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteStrictMode,
		})
		http.SetCookie(w, &http.Cookie{
			Name:     reviewerHintCookieName,
			Value:    payload.Reviewer,
			Path:     path,
			MaxAge:   maxAge,
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteStrictMode,
		})

		csrf, err := newCSRFValue()
		if err != nil {
			http.Error(w, "csrf mint failure", http.StatusInternalServerError)
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     CSRFCookieName,
			Value:    csrf,
			Path:     path,
			MaxAge:   maxAge,
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteStrictMode,
		})

		w.Header().Set("Cache-Control", "no-store, no-cache")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Location", path)
		w.WriteHeader(http.StatusSeeOther)
	})
}

// writeSentinelError emits a tiny text body containing the sentinel
// slug so curl + integration tests can grep for the exact token.
// T6 renders operator-facing HTML via approval_error.tmpl; this is
// the pre-cookie redeem-failure fallback.
func writeSentinelError(w http.ResponseWriter, slug string, code int) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(slug + "\n"))
}

// sentinelSlug maps a wrapped sentinel error to its operator-facing slug.
func sentinelSlug(err error) string {
	switch {
	case errors.Is(err, ErrTokenExpired):
		return "token_expired"
	case errors.Is(err, ErrUnknownKey):
		return "unknown_key"
	case errors.Is(err, ErrTokenReplay):
		return "token_replay"
	case errors.Is(err, ErrNotReviewer):
		return "not_reviewer"
	case errors.Is(err, ErrSelfReview):
		return "self_review"
	default:
		return "token_invalid"
	}
}

// errStatus picks an HTTP status proportional to the sentinel; 4xx
// covers every operator-actionable failure so a reverse-proxy alerting
// on 5xx does not flap on routine wrong-token clicks.
func errStatus(err error) int {
	switch {
	case errors.Is(err, approvaltoken.ErrTokenExpired):
		return http.StatusGone
	case errors.Is(err, approvaltoken.ErrUnknownKeyID):
		return http.StatusForbidden
	default:
		return http.StatusBadRequest
	}
}
