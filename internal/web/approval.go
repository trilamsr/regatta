// approval.go mounts the operator-facing approval flow (spec §3.6.1): render
// page, submit decision, stream the full diff. The HMAC token rides in the
// per-ULID cookie set by RedeemHandler; these handlers never see it in a path.
package web

import (
	"errors"
	"net/http"
	"time"

	"github.com/trilamsr/regatta/internal/gates/approval"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// sentinelTokenInvalid is the operator-facing slug for any unrecognised /
// AID-mismatched / missing-record approval request; the catch-all of the
// approval sentinel set (auth.go::sentinelSlug owns the specific ones).
const sentinelTokenInvalid = "token_invalid"

// approvalPageView is the approval.tmpl data contract: work_item context plus
// the byte-clamped inline diff and an overflow flag the template turns into a
// "show full diff" link.
type approvalPageView struct {
	AID        string
	WorkItemID string
	GateName   string
	RequestedBy string
	Reviewer   string
	Status     string
	Diff       string
	Overflowed bool
	CSRFToken  string
}

// approvalDecidedView echoes the principal's vote back and names the peers
// whose votes the gate still awaits (spec §3.4 I6).
type approvalDecidedView struct {
	AID       string
	Reviewer  string
	Decision  string
	DecidedAt time.Time
	Terminal  bool
	Pending   []string
}

// approvalErrorView renders a typed sentinel so the operator sees why the
// click failed rather than a bare 4xx.
type approvalErrorView struct {
	Sentinel string
}

// RegisterApprovalRoutes mounts the /approve/{aid}* sub-routes onto mux; wired
// in as Dependencies.RouteRegistrar so the route table stays in one place and
// the pre-T6 nil path keeps compiling.
func RegisterApprovalRoutes(mux *http.ServeMux, deps Dependencies) {
	mux.Handle("GET /approve/{aid}", approvalPageHandler(deps))
	mux.Handle("POST /approve/{aid}/decide",
		OriginCheck(deps.Config.PublicHost, CSRFMiddleware(approvalDecideHandler(deps))))
	mux.Handle("GET /approve/{aid}/diff", approvalDiffOverflowHandler(deps))
}

// approvalPageHandler renders approval.tmpl for an authenticated reviewer; a
// missing/invalid cookie renders approval_error.tmpl with the typed sentinel.
func approvalPageHandler(deps Dependencies) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store, no-cache")
		w.Header().Set("Referrer-Policy", "no-referrer")

		principal, payload, err := PrincipalFromRequest(r, deps.Keyring, clockNow(deps))
		if err != nil {
			renderApprovalError(w, deps, approvalAuthSentinel(err), approvalAuthStatus(err))
			return
		}
		aid := r.PathValue("aid")
		if payload.AID != aid {
			renderApprovalError(w, deps, sentinelTokenInvalid, http.StatusBadRequest)
			return
		}

		a, err := deps.DB.GetApproval(r.Context(), aid)
		if err != nil {
			renderApprovalError(w, deps, sentinelTokenInvalid, http.StatusNotFound)
			return
		}

		diff, overflowed := loadInlineDiff(r, deps, a.WorkItemID)
		view := approvalPageView{
			AID:         a.ID,
			WorkItemID:  a.WorkItemID,
			GateName:    a.GateName,
			RequestedBy: a.RequestedBy,
			Reviewer:    principal.ID,
			Status:      a.Status,
			Diff:        diff,
			Overflowed:  overflowed,
			CSRFToken:   CSRFTokenFromRequest(r),
		}
		if err := deps.Templates.Render(w, "approval.tmpl", view); err != nil {
			writeInternalError(w, nil, "web.approval_render", err)
		}
	})
}

// approvalDecideHandler validates the cookie + form decision, calls the shared
// approval.DecideTx write path, and renders approval_decided.tmpl.
func approvalDecideHandler(deps Dependencies) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")

		principal, payload, err := PrincipalFromRequest(r, deps.Keyring, clockNow(deps))
		if err != nil {
			renderApprovalError(w, deps, approvalAuthSentinel(err), approvalAuthStatus(err))
			return
		}
		aid := r.PathValue("aid")
		if payload.AID != aid {
			renderApprovalError(w, deps, sentinelTokenInvalid, http.StatusBadRequest)
			return
		}

		decision := approval.DecisionAllow
		if r.PostFormValue("decision") == "deny" {
			decision = approval.DecisionDeny
		}
		reason := r.PostFormValue("reason")

		result, _, err := approval.DecideTx(
			r.Context(), deps.DB, payload, principal.ID, decision, reason, func() time.Time { return clockNow(deps) })
		if err != nil {
			renderApprovalError(w, deps, decideSentinel(err), decideStatus(err))
			return
		}

		view := approvalDecidedView{
			AID:       aid,
			Reviewer:  principal.ID,
			Decision:  decision,
			DecidedAt: clockNow(deps),
			Terminal:  result.Terminal,
			Pending:   pendingReviewers(r, deps, aid, result.DecidedBy),
		}
		if err := deps.Templates.Render(w, "approval_decided.tmpl", view); err != nil {
			writeInternalError(w, nil, "web.approval_decided_render", err)
		}
	})
}

// approvalDiffOverflowHandler streams the full latest-attempt output for the
// approval's work_item with a 1 MiB hard cap, chunked so a large diff does not
// buffer in memory (spec §3.3 row 4).
func approvalDiffOverflowHandler(deps Dependencies) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")

		_, payload, err := PrincipalFromRequest(r, deps.Keyring, clockNow(deps))
		if err != nil {
			renderApprovalError(w, deps, approvalAuthSentinel(err), approvalAuthStatus(err))
			return
		}
		aid := r.PathValue("aid")
		if payload.AID != aid {
			renderApprovalError(w, deps, sentinelTokenInvalid, http.StatusBadRequest)
			return
		}

		a, err := deps.DB.GetApproval(r.Context(), aid)
		if err != nil {
			renderApprovalError(w, deps, sentinelTokenInvalid, http.StatusNotFound)
			return
		}
		entry, err := deps.DB.GetLatestOutput(r.Context(), a.WorkItemID)
		if err != nil {
			renderApprovalError(w, deps, sentinelTokenInvalid, http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		streamCapped(w, []byte(entry.OutputJSON), MaxFullDiffBytes)
	})
}

// streamCapped writes body to w in chunks, stopping at limit bytes. Chunking
// keeps a large diff out of a single buffer; the limit bounds total bytes sent.
func streamCapped(w http.ResponseWriter, body []byte, limit int) {
	if len(body) > limit {
		body = body[:limit]
	}
	flusher, _ := w.(http.Flusher)
	for off := 0; off < len(body); off += fullDiffChunkBytes {
		end := off + fullDiffChunkBytes
		if end > len(body) {
			end = len(body)
		}
		if _, err := w.Write(body[off:end]); err != nil {
			return
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
}

// loadInlineDiff fetches the latest work_item output and clamps it for inline
// render; a missing journal entry renders an empty (non-overflowing) diff
// since the inline view is advisory.
func loadInlineDiff(r *http.Request, deps Dependencies, workItemID string) (string, bool) {
	entry, err := deps.DB.GetLatestOutput(r.Context(), workItemID)
	if err != nil {
		return "", false
	}
	clamped, overflowed := ClampDiff([]byte(entry.OutputJSON))
	return string(clamped), overflowed
}

// pendingReviewers names the snapshot reviewers who have not yet voted, so the
// decided page can tell the operator who the gate still awaits (spec §3.4 I6).
func pendingReviewers(r *http.Request, deps Dependencies, aid string, decidedBy []string) []string {
	a, err := deps.DB.GetApproval(r.Context(), aid)
	if err != nil {
		return nil
	}
	voted := make(map[string]struct{}, len(decidedBy))
	for _, v := range decidedBy {
		voted[v] = struct{}{}
	}
	var pending []string
	for _, rev := range a.ReviewerSetSnapshot.Reviewers {
		if _, ok := voted[rev]; !ok {
			pending = append(pending, rev)
		}
	}
	return pending
}

// renderApprovalError renders approval_error.tmpl with the typed sentinel at
// the given status; a nil template falls back to the plain-text sentinel via
// writeSentinelError (the shared redeem-path helper). Render buffers internally
// and only writes on success, so a parse-time failure here leaves the response
// untouched and the operator still sees the status code.
func renderApprovalError(w http.ResponseWriter, deps Dependencies, sentinel string, code int) {
	if deps.Templates == nil {
		writeSentinelError(w, sentinel, code)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	_ = deps.Templates.Render(w, "approval_error.tmpl", approvalErrorView{Sentinel: sentinel})
}

// approvalAuthSentinel/approvalAuthStatus map a PrincipalFromRequest error to
// the operator-facing slug + status. A missing cookie is the no-link-redeemed
// case and surfaces as 401 token_invalid (brief MAY-116); other token failures
// keep their canon-derived sentinel + 4xx status.
func approvalAuthSentinel(err error) string {
	if errors.Is(err, ErrCookieMissing) {
		return sentinelTokenInvalid
	}
	return sentinelSlug(err)
}

func approvalAuthStatus(err error) int {
	if errors.Is(err, ErrCookieMissing) {
		return http.StatusUnauthorized
	}
	return errStatus(err)
}

// clockNow reads the injected clock, defaulting to time.Now when unset.
func clockNow(deps Dependencies) time.Time {
	if deps.Clock != nil {
		return deps.Clock()
	}
	return time.Now()
}

// decideSentinel maps a DecideTx error to its operator-facing slug.
func decideSentinel(err error) string {
	switch {
	case errors.Is(err, state.ErrTokenReplay), errors.Is(err, approval.ErrDoubleVote):
		return "token_replay"
	case errors.Is(err, approval.ErrNotReviewer):
		return "not_reviewer"
	case errors.Is(err, approval.ErrSelfReview):
		return "self_review"
	default:
		return sentinelTokenInvalid
	}
}

// decideStatus maps a DecideTx error to an HTTP status; replay/double-vote is
// 409 (spec §3.6.1 sentinel folding), policy rejections are 403.
func decideStatus(err error) int {
	switch {
	case errors.Is(err, state.ErrTokenReplay), errors.Is(err, approval.ErrDoubleVote):
		return http.StatusConflict
	case errors.Is(err, approval.ErrNotReviewer), errors.Is(err, approval.ErrSelfReview):
		return http.StatusForbidden
	default:
		return http.StatusBadRequest
	}
}
