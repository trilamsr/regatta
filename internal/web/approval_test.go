package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// newApprovalTestHandler mounts RegisterApprovalRoutes on a bare mux with a
// minimal Dependencies set so handler tests exercise the real route table.
func newApprovalTestHandler(t *testing.T, deps Dependencies) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	RegisterApprovalRoutes(mux, deps)
	return mux
}

// TestApprovalPageHandler_NoCookieReturnsErrorPage asserts GET /approve/{aid} with no token cookie returns 401 + token_invalid (MAY-116).
func TestApprovalPageHandler_NoCookieReturnsErrorPage(t *testing.T) {
	kr, _, _, _ := testKeyring(t)
	tmpls, err := LoadTemplates(AssetsFS())
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	deps := Dependencies{
		Keyring:   kr,
		Templates: tmpls,
		Clock:     func() time.Time { return time.Unix(1_700_000_000, 0) },
	}
	h := newApprovalTestHandler(t, deps)

	r := httptest.NewRequest(http.MethodGet, "/approve/01H8AID", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d want %d", w.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(w.Body.String(), "token_invalid") {
		t.Fatalf("body missing token_invalid sentinel: %q", w.Body.String())
	}
}

// newApprovalDBHarness seeds a real sqlite DB with one work item + pending
// approval + journaled output so handler tests exercise the live read/write
// paths, and returns deps plus a token-minting helper bound to the keyring.
func newApprovalDBHarness(t *testing.T, output string) (Dependencies, func(reviewer string) string, string, string) {
	t.Helper()
	now := time.Unix(1_700_000_000, 0).UTC()
	clock := func() time.Time { return now }
	dbPath := filepath.Join(t.TempDir(), "approval.db")
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), clock)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	const wiID, aid = "F-1", "a-ui000000001"
	if err := db.UpsertWorkItem(context.Background(), state.WorkItem{
		ID: wiID, Kind: state.KindFeature, Title: "F-1", Lane: "server", Status: state.WorkStatusPlanned,
	}, state.SourceBrief, now); err != nil {
		t.Fatalf("UpsertWorkItem: %v", err)
	}
	if err := db.CreateApproval(context.Background(), state.Approval{
		ID: aid, WorkItemID: wiID, GateName: "ship-gate", RequestedAt: now, RequestedBy: "system",
		ReviewerSetSnapshot: state.ReviewerSet{Reviewers: []string{"alice", "bob"}, Quorum: 2},
		Quorum:              2, Status: state.ApprovalStatusPending, TimeoutAt: now.Add(time.Hour), OnTimeout: "fail",
	}); err != nil {
		t.Fatalf("CreateApproval: %v", err)
	}
	if output != "" {
		if _, err := db.AppendOutput(context.Background(), wiID, []byte(output)); err != nil {
			t.Fatalf("AppendOutput: %v", err)
		}
	}

	kr, kid, _, _ := testKeyring(t)
	tmpls, err := LoadTemplates(AssetsFS())
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	deps := Dependencies{
		DB: db, Keyring: kr, Templates: tmpls, Clock: clock,
		Config: Config{PublicHost: "regatta.host"},
	}
	mint := func(reviewer string) string {
		return mintWire(t, kr, kid, reviewer, aid, wiID, now.Add(time.Hour))
	}
	return deps, mint, aid, wiID
}

// TestApprovalPageHandler_RendersDiffAndOverflow asserts the page renders the clamped diff body + a full-diff link when output exceeds MaxDiffBytes (MAY-116).
func TestApprovalPageHandler_RendersDiffAndOverflow(t *testing.T) {
	big := `{"d":"` + strings.Repeat("x", 12*1024) + `"}`
	deps, mint, aid, _ := newApprovalDBHarness(t, big)
	h := newApprovalTestHandler(t, deps)

	r := httptest.NewRequest(http.MethodGet, "/approve/"+aid, nil)
	r.AddCookie(&http.Cookie{Name: ApprovalTokenCookieName, Value: mint("alice")}) //nolint:gosec // test fixture replays server-set cookie
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%q", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "/approve/"+aid+"/diff") {
		t.Fatalf("overflow link missing for >8KiB diff")
	}
}

// TestApprovalDecideHandler_HappyRendersDecided asserts a CSRF+Origin-valid allow vote calls DecideTx and renders the decided page echoing the reviewer (MAY-116).
func TestApprovalDecideHandler_HappyRendersDecided(t *testing.T) {
	deps, mint, aid, _ := newApprovalDBHarness(t, "")
	h := newApprovalTestHandler(t, deps)

	form := url.Values{"decision": {"allow"}, "reason": {"lgtm"}, "csrf": {"tok"}}
	r := httptest.NewRequest(http.MethodPost, "/approve/"+aid+"/decide", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "https://regatta.host")
	r.AddCookie(&http.Cookie{Name: ApprovalTokenCookieName, Value: mint("alice")}) //nolint:gosec // test fixture
	r.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: "tok"})                     //nolint:gosec // test fixture
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%q", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "alice") {
		t.Fatalf("decided page does not echo reviewer: %q", w.Body.String())
	}
}

// TestApprovalDecideHandler_RejectsMismatchedCSRF asserts a POST whose csrf form field differs from the CSRF cookie is rejected 403 by CSRFMiddleware (MAY-116).
func TestApprovalDecideHandler_RejectsMismatchedCSRF(t *testing.T) {
	deps, mint, aid, _ := newApprovalDBHarness(t, "")
	h := newApprovalTestHandler(t, deps)

	form := url.Values{"decision": {"allow"}, "csrf": {"wrong"}}
	r := httptest.NewRequest(http.MethodPost, "/approve/"+aid+"/decide", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "https://regatta.host")
	r.AddCookie(&http.Cookie{Name: ApprovalTokenCookieName, Value: mint("alice")}) //nolint:gosec // test fixture
	r.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: "tok"})                  //nolint:gosec // test fixture
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d want 403; body=%q", w.Code, w.Body.String())
	}
}

// TestApprovalDecideHandler_RejectsForeignOrigin asserts a POST with a foreign Origin header is rejected 403 by OriginCheck (MAY-116).
func TestApprovalDecideHandler_RejectsForeignOrigin(t *testing.T) {
	deps, mint, aid, _ := newApprovalDBHarness(t, "")
	h := newApprovalTestHandler(t, deps)

	form := url.Values{"decision": {"allow"}, "csrf": {"tok"}}
	r := httptest.NewRequest(http.MethodPost, "/approve/"+aid+"/decide", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "https://evil.example")
	r.AddCookie(&http.Cookie{Name: ApprovalTokenCookieName, Value: mint("alice")}) //nolint:gosec // test fixture
	r.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: "tok"})                  //nolint:gosec // test fixture
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d want 403; body=%q", w.Code, w.Body.String())
	}
}

// TestApprovalPageHandler_RejectsAIDMismatch asserts GET /approve/{aid} with a token minted for a different aid returns 400 (MAY-116).
func TestApprovalPageHandler_RejectsAIDMismatch(t *testing.T) {
	deps, _, _, wiID := newApprovalDBHarness(t, "")
	kr, kid, _, _ := testKeyring(t)
	deps.Keyring = kr
	h := newApprovalTestHandler(t, deps)

	now := time.Unix(1_700_000_000, 0).UTC()
	foreign := mintWire(t, kr, kid, "alice", "a-other00000002", wiID, now.Add(time.Hour))
	r := httptest.NewRequest(http.MethodGet, "/approve/a-ui000000001", nil)
	r.AddCookie(&http.Cookie{Name: ApprovalTokenCookieName, Value: foreign}) //nolint:gosec // test fixture
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want 400; body=%q", w.Code, w.Body.String())
	}
}

// TestApprovalDecideHandler_RejectsAIDMismatch asserts POST /decide with a token minted for a different aid returns 400 (MAY-116).
func TestApprovalDecideHandler_RejectsAIDMismatch(t *testing.T) {
	deps, _, _, wiID := newApprovalDBHarness(t, "")
	kr, kid, _, _ := testKeyring(t)
	deps.Keyring = kr
	h := newApprovalTestHandler(t, deps)

	now := time.Unix(1_700_000_000, 0).UTC()
	foreign := mintWire(t, kr, kid, "alice", "a-other00000002", wiID, now.Add(time.Hour))
	form := url.Values{"decision": {"allow"}, "csrf": {"tok"}}
	r := httptest.NewRequest(http.MethodPost, "/approve/a-ui000000001/decide", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "https://regatta.host")
	r.AddCookie(&http.Cookie{Name: ApprovalTokenCookieName, Value: foreign}) //nolint:gosec // test fixture
	r.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: "tok"})            //nolint:gosec // test fixture
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want 400; body=%q", w.Code, w.Body.String())
	}
}

// TestApprovalDiffOverflowHandler_StreamsCapped asserts the full-diff route serves at most MaxFullDiffBytes (MAY-116).
func TestApprovalDiffOverflowHandler_StreamsCapped(t *testing.T) {
	huge := strings.Repeat("z", 2*1024*1024)
	deps, mint, aid, _ := newApprovalDBHarness(t, `{"d":"`+huge+`"}`)
	h := newApprovalTestHandler(t, deps)

	r := httptest.NewRequest(http.MethodGet, "/approve/"+aid+"/diff", nil)
	r.AddCookie(&http.Cookie{Name: ApprovalTokenCookieName, Value: mint("alice")}) //nolint:gosec // test fixture
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d want 200", w.Code)
	}
	if w.Body.Len() > MaxFullDiffBytes {
		t.Fatalf("streamed %d bytes want <= %d", w.Body.Len(), MaxFullDiffBytes)
	}
}

// TestApprovalPageHandler_EscapesDiffXSS asserts operator-controlled diff bytes are HTML-escaped in the rendered page (MAY-116, spec §8).
func TestApprovalPageHandler_EscapesDiffXSS(t *testing.T) {
	deps, mint, aid, _ := newApprovalDBHarness(t, `{"x":"<script>alert(1)</script>"}`)
	h := newApprovalTestHandler(t, deps)

	r := httptest.NewRequest(http.MethodGet, "/approve/"+aid, nil)
	r.AddCookie(&http.Cookie{Name: ApprovalTokenCookieName, Value: mint("alice")}) //nolint:gosec // test fixture
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if strings.Contains(w.Body.String(), "<script>alert(1)</script>") {
		t.Fatalf("raw <script> survived into rendered page: XSS gate breached")
	}
}
