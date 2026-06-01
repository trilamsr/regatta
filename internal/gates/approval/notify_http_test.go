package approval

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/canon"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// httpHarness wires a fresh state.DB + keyring + pending approval row +
// minted token so each test exercises the real DecideTx path through
// NewHTTPCallback. Mirrors decideTxHarness but adds keyring + handler.
type httpHarness struct {
	db         *state.DB
	keyring    canon.MapKeyring
	kid        string
	now        time.Time
	clock      func() time.Time
	path       string
	handler    http.Handler
	approvalID string
}

func newHTTPHarness(t *testing.T) *httpHarness {
	t.Helper()
	t0 := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return t0 }
	dbPath := filepath.Join(t.TempDir(), "notify_http.db")
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), clock)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := db.UpsertWorkItem(context.Background(), state.WorkItem{
		ID: "F-1", Kind: state.KindFeature, Title: "F-1", Lane: "server", Status: state.WorkStatusPlanned,
	}, state.SourceBrief, t0); err != nil {
		t.Fatalf("UpsertWorkItem: %v", err)
	}

	approvalID := "a-htt00000001"
	if err := db.CreateApproval(context.Background(), state.Approval{
		ID:          approvalID,
		WorkItemID:  "F-1",
		GateName:    "ship-gate",
		RequestedAt: t0,
		RequestedBy: "system",
		ReviewerSetSnapshot: state.ReviewerSet{
			Reviewers: []string{"alice"},
			Quorum:    1,
		},
		Quorum:    1,
		Status:    state.ApprovalStatusPending,
		TimeoutAt: t0.Add(time.Hour),
		OnTimeout: "fail",
	}); err != nil {
		t.Fatalf("CreateApproval: %v", err)
	}

	kr := canon.MapKeyring{"k1": []byte("test-key-do-not-use-in-prod")}
	path, handler := NewHTTPCallback(Dependencies{
		DB:      db,
		Keyring: kr,
		Clock:   clock,
	})
	return &httpHarness{
		db:         db,
		keyring:    kr,
		kid:        "k1",
		now:        t0,
		clock:      clock,
		path:       path,
		handler:    handler,
		approvalID: approvalID,
	}
}

func (h *httpHarness) mintToken(t *testing.T, reviewer string) string {
	t.Helper()
	wire, _, err := canon.MintToken(h.keyring, h.kid, canon.TokenPayload{
		KID:      h.kid,
		WI:       "F-1",
		AID:      h.approvalID,
		Reviewer: reviewer,
		JTI:      "",
		Window:   h.now.Add(time.Hour).Unix(),
	}, rand.Reader)
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}
	return wire
}

func (h *httpHarness) postDecision(t *testing.T, token, decision, reason string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{}
	form.Set("token", token)
	form.Set("reviewer", "alice")
	form.Set("decision", decision)
	if reason != "" {
		form.Set("reason", reason)
	}
	req := httptest.NewRequest(http.MethodPost, h.path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	return rec
}

// B5: happy-path POST allow → 200 + state mutated + Cache-Control: no-store.
func TestNotifyHTTP_CallbackPOSTAllow(t *testing.T) {
	h := newHTTPHarness(t)
	rec := h.postDecision(t, h.mintToken(t, "alice"), "allow", "lgtm")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control=%q want no-store", got)
	}
	var body struct {
		Status     string `json:"status"`
		ApprovalID string `json:"approval_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json decode: %v; body=%s", err, rec.Body.String())
	}
	if body.Status != "ok" {
		t.Errorf("status=%q want ok", body.Status)
	}
	if body.ApprovalID != h.approvalID {
		t.Errorf("approval_id=%q want %q", body.ApprovalID, h.approvalID)
	}
	got, err := h.db.GetApproval(context.Background(), h.approvalID)
	if err != nil {
		t.Fatalf("GetApproval: %v", err)
	}
	if got.Status != state.ApprovalStatusApproved {
		t.Errorf("denorm status=%q want approved", got.Status)
	}
}

// A6: replay → first 200, second 409 with token_replay sentinel.
func TestNotifyHTTP_CallbackReplayReturns409(t *testing.T) {
	h := newHTTPHarness(t)
	tok := h.mintToken(t, "alice")
	first := h.postDecision(t, tok, "allow", "lgtm")
	if first.Code != http.StatusOK {
		t.Fatalf("first status=%d want 200; body=%s", first.Code, first.Body.String())
	}
	second := h.postDecision(t, tok, "allow", "lgtm")
	if second.Code != http.StatusConflict {
		t.Fatalf("second status=%d want 409; body=%s", second.Code, second.Body.String())
	}
	if got := second.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control=%q want no-store on replay", got)
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(second.Body.Bytes(), &body); err != nil {
		t.Fatalf("json decode: %v; body=%s", err, second.Body.String())
	}
	if body.Error != "token_replay" {
		t.Errorf("error=%q want token_replay", body.Error)
	}
	if !errors.Is(state.ErrTokenReplay, state.ErrTokenReplay) {
		t.Fatal("sentinel sanity: state.ErrTokenReplay must remain comparable")
	}
}

// A7: non-POST methods return 405 with Allow: POST.
func TestNotifyHTTP_CallbackRejectsNonPOST(t *testing.T) {
	h := newHTTPHarness(t)
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		req := httptest.NewRequest(method, h.path, nil)
		rec := httptest.NewRecorder()
		h.handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("method=%s status=%d want 405", method, rec.Code)
		}
		if allow := rec.Header().Get("Allow"); !strings.Contains(allow, http.MethodPost) {
			t.Errorf("method=%s Allow=%q want contains POST", method, allow)
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("method=%s Cache-Control=%q want no-store", method, got)
		}
	}
}

// Path returned by NewHTTPCallback must be spec §3.3 row 8.
func TestNotifyHTTP_CallbackPathIsSpecLocked(t *testing.T) {
	h := newHTTPHarness(t)
	if h.path != "/api/approval/callback" {
		t.Errorf("path=%q want /api/approval/callback", h.path)
	}
}
