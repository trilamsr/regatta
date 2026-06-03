package health

import (
	"encoding/json"
	"net/http"
	"strings"
)

// legacyBody is the W7.0 contract preserved for callers that do not
// negotiate JSON — `GET /healthz` without `Accept: application/json`
// keeps returning `ok\n` with zero DB queries.
const legacyBody = "ok\n"

// Handler returns the W3 /healthz handler — content-negotiates between
// the legacy literal-ok contract and the JSON readiness envelope.
// 503 only when overall status is down; degraded keeps 200 so the
// supervisor reads the `status` field (per spec §3.5).
func Handler(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !wantsJSON(r) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(legacyBody))
			return
		}
		res := Probe(r.Context(), deps)
		w.Header().Set("Content-Type", "application/json")
		if res.Status == StatusDown {
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			w.WriteHeader(http.StatusOK)
		}
		_ = json.NewEncoder(w).Encode(res)
	}
}

// wantsJSON honours Accept: application/json (also matches `*/*` JSON-style probes).
func wantsJSON(r *http.Request) bool {
	a := r.Header.Get("Accept")
	return strings.Contains(a, "application/json")
}
