package health

import (
	"encoding/json"
	"net/http"
)

// Handler returns the W3 /healthz handler — JSON readiness envelope per spec §3.5.
// 503 only when overall status is down; degraded keeps 200 so the supervisor
// reads the `status` field.
func Handler(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
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
