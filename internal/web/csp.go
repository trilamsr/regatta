package web

import "net/http"

// CSPHeader pins spec §3.7 verbatim; the literal lives outside CSPMiddleware
// so other handlers can assert byte-equality without spawning the middleware.
// No `unsafe-inline`, no `unsafe-eval`, no third-party origin (R3: bearer-token
// leak defense + supply-chain anchor). The single sha256 in `style-src` covers
// the deterministic `<style>` block htmx 2.0.4 injects at boot for indicator
// classes (closes the 88-per-page console-violation churn from MAY-57); every
// other inline style stays banned — template files are gated by
// TestTemplates_NoInlineStyleAttrs to keep additions out.
const CSPHeader = "default-src 'self'; script-src 'self'; style-src 'self' 'sha256-bsV5JivYxvGywDAZ22EZJKBFip65Ng9xoJVLbBg7bdo='; img-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'"

// referrerPolicy + nosniff + frameOptions are spec §3.7 belt-and-suspenders
// alongside CSP; the constants live here so a future header reorg edits one
// file.
const (
	referrerPolicy = "no-referrer"
	nosniff        = "nosniff"
	frameOptions   = "DENY"
)

// CSPMiddleware stamps spec §3.7 headers on every response before delegating
// to next. The header set is fixed at boot — no per-request branching — so
// the middleware adds four header writes and one delegation per request.
func CSPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hdr := w.Header()
		hdr.Set("Content-Security-Policy", CSPHeader)
		hdr.Set("Referrer-Policy", referrerPolicy)
		hdr.Set("X-Content-Type-Options", nosniff)
		hdr.Set("X-Frame-Options", frameOptions)
		next.ServeHTTP(w, r)
	})
}
