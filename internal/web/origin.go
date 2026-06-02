// origin.go owns the POST-only Origin-header check (spec §3.6.2 S2); empty Origin on POST rejects too.
package web

import (
	"net/http"
)

// OriginCheck rejects POSTs whose Origin header is missing or differs from "https://"+publicHost (fallback r.Host).
func OriginCheck(publicHost string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			next.ServeHTTP(w, r)
			return
		}
		expect := "https://"
		if publicHost != "" {
			expect += publicHost
		} else {
			expect += r.Host
		}
		got := r.Header.Get("Origin")
		if got == "" || got != expect {
			http.Error(w, "origin", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
