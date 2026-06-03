package review

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/trilamsr/regatta/contracts/schemas"
)

// fixtureBot is the canonical reviewer-bot login used in tests; spec §3
// two-identity model gates author == GH_USER_BOT, not the reviewer login.
const (
	fixtureAuthorBot   = "regatta-bot"
	fixtureReviewerBot = "regatta-reviewer-bot"
)

// newFakeGH spins up an httptest server with a router; tests register
// per-route handlers so each test owns its response matrix without
// cross-test mutation.
func newFakeGH(t *testing.T, h http.Handler) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

// silentLogger returns a slog.Logger that discards output so tests stay
// quiet; specific tests that need log assertions construct their own.
func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newTestApprover wires an Approver against a fake GH base URL with the
// fixture bot identities; tests override only the http.Handler.
func newTestApprover(t *testing.T, baseURL string) *Approver {
	t.Helper()
	a, err := New(Config{
		BaseURL:        baseURL,
		Owner:          "acme",
		Repo:           "regatta",
		ReviewerToken:  "tok-reviewer",
		ReviewerLogin:  fixtureReviewerBot,
		AuthorBotLogin: fixtureAuthorBot,
		HTTPClient:     http.DefaultClient,
		Logger:         silentLogger(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return a
}

// TestApprover_HappyPath_PostsApproveReview pins B-tier (a) — pass verdict + bot-authored PR posts APPROVE.
func TestApprover_HappyPath_PostsApproveReview(t *testing.T) {
	var posted struct {
		Event    string `json:"event"`
		Body     string `json:"body"`
		CommitID string `json:"commit_id"`
	}
	var postCount atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/regatta/pulls/42", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"user":{"login":"regatta-bot"},"head":{"sha":"abc1234"}}`))
	})
	mux.HandleFunc("/repos/acme/regatta/pulls/42/reviews", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		postCount.Add(1)
		_ = json.NewDecoder(r.Body).Decode(&posted)
		_, _ = w.Write([]byte(`{"id": 98765432}`))
	})
	srv := newFakeGH(t, mux)
	a := newTestApprover(t, srv.URL)

	v := Verdict{
		Outcome:  schemas.VerdictPass,
		PRNumber: 42,
		HeadSHA:  "abc1234",
		Model:    "claude-opus-4-7",
		Reason:   "all criteria pass",
	}
	if err := a.Reconcile(context.Background(), v); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if got := postCount.Load(); got != 1 {
		t.Fatalf("POST count = %d, want 1", got)
	}
	if posted.Event != "APPROVE" {
		t.Errorf("event = %q, want APPROVE", posted.Event)
	}
	if !strings.Contains(posted.Body, "pass") || !strings.Contains(posted.Body, "abc1234") {
		t.Errorf("body missing verdict/SHA: %q", posted.Body)
	}
	if posted.CommitID != "abc1234" {
		t.Errorf("commit_id = %q, want abc1234", posted.CommitID)
	}
}

// TestApprover_NonBotAuthor_Refuses pins B-tier (e) — human PRs short-circuit before HTTP call.
func TestApprover_NonBotAuthor_Refuses(t *testing.T) {
	var postCount atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/regatta/pulls/7", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"user":{"login":"alice"},"head":{"sha":"deadbee"}}`))
	})
	mux.HandleFunc("/repos/acme/regatta/pulls/7/reviews", func(_ http.ResponseWriter, _ *http.Request) {
		postCount.Add(1)
	})
	srv := newFakeGH(t, mux)
	a := newTestApprover(t, srv.URL)

	v := Verdict{Outcome: schemas.VerdictPass, PRNumber: 7, HeadSHA: "deadbee", Model: "claude-opus-4-7"}
	if err := a.Reconcile(context.Background(), v); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if got := postCount.Load(); got != 0 {
		t.Fatalf("POST count = %d, want 0 (human-authored skip)", got)
	}
}

// TestApprover_VerdictFlip_DismissesThenRequestsChanges pins spec §5 — flip dismisses prior approval BEFORE posting REQUEST_CHANGES.
func TestApprover_VerdictFlip_DismissesThenRequestsChanges(t *testing.T) {
	var order []string
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/regatta/pulls/13", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"user":{"login":"regatta-bot"},"head":{"sha":"f00"}}`))
	})
	mux.HandleFunc("/repos/acme/regatta/pulls/13/reviews", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`[{"id": 555, "state":"APPROVED", "user":{"login":"regatta-reviewer-bot"}}]`))
			return
		}
		order = append(order, "POST_REVIEW")
		var body struct {
			Event string `json:"event"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Event != "REQUEST_CHANGES" {
			t.Errorf("posted event = %q, want REQUEST_CHANGES", body.Event)
		}
		_, _ = w.Write([]byte(`{"id": 999}`))
	})
	mux.HandleFunc("/repos/acme/regatta/pulls/13/reviews/555/dismissals", func(w http.ResponseWriter, _ *http.Request) {
		order = append(order, "DISMISS")
		_, _ = w.Write([]byte(`{}`))
	})
	srv := newFakeGH(t, mux)
	a := newTestApprover(t, srv.URL)

	v := Verdict{Outcome: schemas.VerdictFail, PRNumber: 13, HeadSHA: "f00", Model: "claude-opus-4-7", Reason: "L0 gate failed"}
	if err := a.Reconcile(context.Background(), v); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(order) != 2 || order[0] != "DISMISS" || order[1] != "POST_REVIEW" {
		t.Fatalf("call order = %v, want [DISMISS POST_REVIEW]", order)
	}
}

// TestApprover_DismissalFails_StillPostsRequestChanges pins spec §7 R12 — 404/422 on dismiss does not block REQUEST_CHANGES.
func TestApprover_DismissalFails_StillPostsRequestChanges(t *testing.T) {
	var postedReview bool
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/regatta/pulls/8", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"user":{"login":"regatta-bot"},"head":{"sha":"sha8"}}`))
	})
	mux.HandleFunc("/repos/acme/regatta/pulls/8/reviews", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`[{"id": 1, "state":"APPROVED", "user":{"login":"regatta-reviewer-bot"}}]`))
			return
		}
		postedReview = true
		_, _ = w.Write([]byte(`{"id": 2}`))
	})
	mux.HandleFunc("/repos/acme/regatta/pulls/8/reviews/1/dismissals", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
	})
	srv := newFakeGH(t, mux)
	a := newTestApprover(t, srv.URL)

	v := Verdict{Outcome: schemas.VerdictFail, PRNumber: 8, HeadSHA: "sha8", Model: "m"}
	if err := a.Reconcile(context.Background(), v); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if !postedReview {
		t.Fatalf("REQUEST_CHANGES was not POSTed despite dismissal failure")
	}
}

// TestApprover_SelfApproveAttempt_Rejected pins spec §7 R6 — 422 self-approve fails closed without retry.
func TestApprover_SelfApproveAttempt_Rejected(t *testing.T) {
	var postCount atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/regatta/pulls/9", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"user":{"login":"regatta-bot"},"head":{"sha":"s9"}}`))
	})
	mux.HandleFunc("/repos/acme/regatta/pulls/9/reviews", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		postCount.Add(1)
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"Can not approve your own pull request"}`))
	})
	srv := newFakeGH(t, mux)
	a := newTestApprover(t, srv.URL)

	v := Verdict{Outcome: schemas.VerdictPass, PRNumber: 9, HeadSHA: "s9", Model: "m"}
	err := a.Reconcile(context.Background(), v)
	if err == nil {
		t.Fatalf("Reconcile: want error on self-approve 422, got nil")
	}
	if postCount.Load() != 1 {
		t.Fatalf("expected exactly one POST attempt, got %d", postCount.Load())
	}
}

// TestApprover_TokenScopeInsufficient_403Surfaces pins matrix row "Token scope insufficient" — 403 surfaces error.
func TestApprover_TokenScopeInsufficient_403Surfaces(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/regatta/pulls/10", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"user":{"login":"regatta-bot"},"head":{"sha":"s10"}}`))
	})
	mux.HandleFunc("/repos/acme/regatta/pulls/10/reviews", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		w.WriteHeader(http.StatusForbidden)
	})
	srv := newFakeGH(t, mux)
	a := newTestApprover(t, srv.URL)

	err := a.Reconcile(context.Background(), Verdict{Outcome: schemas.VerdictPass, PRNumber: 10, HeadSHA: "s10", Model: "m"})
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("Reconcile: want 403 error, got %v", err)
	}
}

// TestApprover_PRGone_404TreatsAsNoop pins spec §7 R12 — PR closed mid-review is soft-success.
func TestApprover_PRGone_404TreatsAsNoop(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/regatta/pulls/11", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	srv := newFakeGH(t, mux)
	a := newTestApprover(t, srv.URL)

	if err := a.Reconcile(context.Background(), Verdict{Outcome: schemas.VerdictPass, PRNumber: 11, HeadSHA: "s11", Model: "m"}); err != nil {
		t.Fatalf("Reconcile: want soft-success on 404, got %v", err)
	}
}

// TestApprover_RedactsRepoPrivatePaths pins A-tier (j) — paths in reasoning are stripped before POST.
func TestApprover_RedactsRepoPrivatePaths(t *testing.T) {
	var posted struct {
		Body string `json:"body"`
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/regatta/pulls/12", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"user":{"login":"regatta-bot"},"head":{"sha":"s12"}}`))
	})
	mux.HandleFunc("/repos/acme/regatta/pulls/12/reviews", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&posted)
		_, _ = w.Write([]byte(`{"id": 3}`))
	})
	srv := newFakeGH(t, mux)
	a := newTestApprover(t, srv.URL)

	v := Verdict{
		Outcome:  schemas.VerdictPass,
		PRNumber: 12,
		HeadSHA:  "s12",
		Model:    "m",
		Reason:   "looked at internal/secret/keys.go and api_key=sk-ant-fixture-DO-NOT-LEAK",
	}
	if err := a.Reconcile(context.Background(), v); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if strings.Contains(posted.Body, "internal/secret/keys.go") {
		t.Errorf("body leaked path: %q", posted.Body)
	}
	if strings.Contains(posted.Body, "sk-ant-fixture-DO-NOT-LEAK") {
		t.Errorf("body leaked secret: %q", posted.Body)
	}
}

// TestApprover_IdempotentOnExistingApproval pins spec §5 idempotency — byte-identical body short-circuits POST.
func TestApprover_IdempotentOnExistingApproval(t *testing.T) {
	var postCount atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/regatta/pulls/14", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"user":{"login":"regatta-bot"},"head":{"sha":"s14"}}`))
	})
	a := &Approver{}
	v := Verdict{Outcome: schemas.VerdictPass, PRNumber: 14, HeadSHA: "s14", Model: "claude-opus-4-7", Reason: "pass"}
	// Compute the body the Approver will post so the fake can pre-seed an
	// existing approval whose body string-matches.
	mux.HandleFunc("/repos/acme/regatta/pulls/14/reviews", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			body, _ := json.Marshal([]map[string]any{{
				"id":    100,
				"state": "APPROVED",
				"user":  map[string]any{"login": "regatta-reviewer-bot"},
				"body":  a.composeBody(v),
				"commit_id": "s14",
			}})
			_, _ = w.Write(body)
			return
		}
		postCount.Add(1)
	})
	srv := newFakeGH(t, mux)
	a2 := newTestApprover(t, srv.URL)
	*a = *a2
	if err := a.Reconcile(context.Background(), v); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if postCount.Load() != 0 {
		t.Fatalf("expected idempotent skip, got %d POSTs", postCount.Load())
	}
}

// TestApprover_RateLimit5000PerHour_RecordsMetric pins spec §7 R7 — metric is registered (read on construction).
func TestApprover_RateLimit5000PerHour_RecordsMetric(t *testing.T) {
	srv := newFakeGH(t, http.NotFoundHandler())
	a := newTestApprover(t, srv.URL)
	if a.metrics == nil {
		t.Fatalf("metrics struct not initialized")
	}
	if a.metrics.postsAttempted == nil || a.metrics.postsFailed == nil {
		t.Fatalf("metric counters not registered")
	}
}

// TestApprover_AbstainPostsComment pins A+ (n) — ABSTAIN equivalent (advisory) maps to COMMENT event.
func TestApprover_AbstainPostsComment(t *testing.T) {
	var posted struct {
		Event string `json:"event"`
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/regatta/pulls/15", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"user":{"login":"regatta-bot"},"head":{"sha":"s15"}}`))
	})
	mux.HandleFunc("/repos/acme/regatta/pulls/15/reviews", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&posted)
		_, _ = w.Write([]byte(`{"id": 1}`))
	})
	srv := newFakeGH(t, mux)
	a := newTestApprover(t, srv.URL)
	if err := a.Reconcile(context.Background(), Verdict{Outcome: schemas.VerdictAdvisory, PRNumber: 15, HeadSHA: "s15", Model: "m"}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if posted.Event != "COMMENT" {
		t.Errorf("event = %q, want COMMENT", posted.Event)
	}
}

// TestRedactor_Idempotent pins A+ (s) — redact ∘ redact == redact across representative inputs.
func TestRedactor_Idempotent(t *testing.T) {
	inputs := []string{
		"internal/secret/keys.go has issue",
		"token=sk-ant-fixture-DO-NOT-LEAK in body",
		"path/to/file.go:42 with code: `secret_key`",
		"clean text without anything sensitive",
	}
	for _, in := range inputs {
		once := redact(in)
		twice := redact(once)
		if once != twice {
			t.Errorf("not idempotent: %q\n once=%q\ntwice=%q", in, once, twice)
		}
	}
}
