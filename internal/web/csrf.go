// csrf.go owns the double-submit-cookie middleware (spec §3.6.2); state-free, subtle.ConstantTimeCompare.
package web

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
)

// CSRFMiddleware enforces the double-submit cookie check on POST via subtle.ConstantTimeCompare.
func CSRFMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			next.ServeHTTP(w, r)
			return
		}
		c, err := r.Cookie(CSRFCookieName)
		if err != nil || c.Value == "" {
			http.Error(w, "csrf", http.StatusForbidden)
			return
		}
		// ParseForm is idempotent + cheap; required so PostFormValue
		// can read application/x-www-form-urlencoded bodies.
		if err := r.ParseForm(); err != nil {
			http.Error(w, "csrf", http.StatusForbidden)
			return
		}
		form := r.PostFormValue("csrf")
		if subtle.ConstantTimeCompare([]byte(form), []byte(c.Value)) != 1 {
			http.Error(w, "csrf", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// CSRFTokenFromRequest reads the CSRF cookie at render time so templates can echo it into the hidden form input.
func CSRFTokenFromRequest(r *http.Request) string {
	c, err := r.Cookie(CSRFCookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

// newCSRFValue mints a 32-hex-char (16-byte) CSRF cookie value from
// crypto/rand. math/rand is forbidden by repo lint TestNoMathRandImport
// for security primitives.
func newCSRFValue() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
