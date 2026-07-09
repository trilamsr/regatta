package approval

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/trilamsr/regatta/internal/canon/approvaltoken"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// callbackPath is the spec §3.3 row 8 mount point.
const callbackPath = "/api/approval/callback"

// maxCallbackBodyBytes caps form-urlencoded callback bodies so a hostile caller can't exhaust memory before parse-time rejection.
const maxCallbackBodyBytes = 1 << 20

// Dependencies bundles the side-effect handles NewHTTPCallback needs (DB, keyring, clock).
type Dependencies struct {
	DB      *state.DB
	Keyring approvaltoken.Keyring
	Clock   func() time.Time
}

// NewHTTPCallback returns the spec §3.3 row 8 path and the concrete HTTP handler for interactive notifier callbacks (e.g. Slack button POST).
func NewHTTPCallback(deps Dependencies) (string, http.Handler) {
	clock := deps.Clock
	if clock == nil {
		clock = time.Now
	}
	h := &httpCallback{db: deps.DB, keyring: deps.Keyring, clock: clock}
	return callbackPath, h
}

// httpCallback validates the HMAC token, dispatches to approval.DecideTx, and maps typed sentinels onto HTTP status per spec §3.6.
type httpCallback struct {
	db      *state.DB
	keyring approvaltoken.Keyring
	clock   func() time.Time
}

func (h *httpCallback) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// no-store on every response — operator decisions can't tolerate a stale-cache lie even on errors (spec §3.3 row 8).
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxCallbackBodyBytes)
	if err := r.ParseForm(); err != nil {
		writeJSONError(w, http.StatusBadRequest, "malformed_body")
		return
	}
	wire := r.PostFormValue("token")
	reviewer := r.PostFormValue("reviewer")
	decision := r.PostFormValue("decision")
	reason := r.PostFormValue("reason")
	if wire == "" || reviewer == "" || decision == "" {
		writeJSONError(w, http.StatusBadRequest, "missing_field")
		return
	}

	payload, err := approvaltoken.VerifyToken(h.keyring, wire, reviewer, h.clock())
	if err != nil {
		code, sentinel := classifyVerifyErr(err)
		writeJSONError(w, code, sentinel)
		return
	}

	res, finalStatus, decideErr := DecideTx(r.Context(), h.db, payload, reviewer, decision, reason, h.clock)
	if decideErr != nil {
		code, sentinel := classifyDecideErr(decideErr)
		writeJSONError(w, code, sentinel)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Status     string `json:"status"`
		ApprovalID string `json:"approval_id"`
		Terminal   bool   `json:"terminal"`
		Final      string `json:"final_status,omitempty"`
	}{Status: "ok", ApprovalID: payload.AID, Terminal: res.Terminal, Final: finalStatus})
}

// classifyVerifyErr maps canon verify-time sentinels to HTTP status codes.
func classifyVerifyErr(err error) (int, string) {
	switch {
	case errors.Is(err, approvaltoken.ErrTokenExpired):
		return http.StatusUnauthorized, "token_expired"
	case errors.Is(err, approvaltoken.ErrTokenInvalid):
		return http.StatusBadRequest, "token_invalid"
	case errors.Is(err, approvaltoken.ErrUnverifiable):
		return http.StatusUnauthorized, "token_unverifiable"
	case errors.Is(err, approvaltoken.ErrUnknownKeyID):
		return http.StatusUnauthorized, "unknown_key"
	default:
		return http.StatusUnauthorized, "token_invalid"
	}
}

// classifyDecideErr maps approval.DecideTx errors to HTTP status codes. DoubleVote folds into token_replay because the operator-facing semantic ("this vote already counted") is identical and Slack-button retries are the dominant trigger.
func classifyDecideErr(err error) (int, string) {
	switch {
	case errors.Is(err, state.ErrTokenReplay), errors.Is(err, ErrDoubleVote):
		return http.StatusConflict, "token_replay"
	case errors.Is(err, ErrNotReviewer):
		return http.StatusForbidden, "not_reviewer"
	case errors.Is(err, ErrSelfReview):
		return http.StatusForbidden, "self_review"
	default:
		return http.StatusInternalServerError, "internal_error"
	}
}

func writeJSONError(w http.ResponseWriter, status int, sentinel string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Error string `json:"error"`
	}{Error: sentinel})
}
