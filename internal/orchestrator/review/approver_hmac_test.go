package review

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/trilamsr/regatta/contracts/schemas"
)

// withKeyring returns an Approver configured with a single-entry keyring
// — tightest possible fixture so each HMAC test pins exactly one branch.
func withKeyring(t *testing.T, base string, kr map[string][]byte) *Approver {
	t.Helper()
	a, err := New(Config{
		BaseURL:        base,
		Owner:          "acme",
		Repo:           "regatta",
		ReviewerToken:  "tok",
		ReviewerLogin:  fixtureReviewerBot,
		AuthorBotLogin: fixtureAuthorBot,
		HTTPClient:     http.DefaultClient,
		Logger:         silentLogger(),
		Keyring:        kr,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return a
}

// TestApprover_HMACValid_PostsSuccessfully pins #658 — signed verdict POSTs.
func TestApprover_HMACValid_PostsSuccessfully(t *testing.T) {
	var postCount atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/regatta/pulls/100", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"user":{"login":"regatta-bot"},"head":{"sha":"s100"}}`))
	})
	mux.HandleFunc("/repos/acme/regatta/pulls/100/reviews", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		postCount.Add(1)
		_, _ = w.Write([]byte(`{"id": 1}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	kr := map[string][]byte{"k1": []byte("secret-k1")}
	a := withKeyring(t, srv.URL, kr)
	v, err := SignVerdict(kr, "k1", Verdict{
		Outcome: schemas.VerdictPass, PRNumber: 100, HeadSHA: "s100", Model: "m", Reason: "ok",
	})
	if err != nil {
		t.Fatalf("SignVerdict: %v", err)
	}
	if err := a.Reconcile(context.Background(), v); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if postCount.Load() != 1 {
		t.Fatalf("POST count = %d, want 1", postCount.Load())
	}
}

// TestApprover_HMACMismatch_RefusesPost pins #658 — tampered verdict fails closed.
func TestApprover_HMACMismatch_RefusesPost(t *testing.T) {
	var postCount atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/regatta/pulls/101/reviews", func(_ http.ResponseWriter, _ *http.Request) {
		postCount.Add(1)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	kr := map[string][]byte{"k1": []byte("secret-k1")}
	a := withKeyring(t, srv.URL, kr)
	v, err := SignVerdict(kr, "k1", Verdict{
		Outcome: schemas.VerdictPass, PRNumber: 101, HeadSHA: "s101", Model: "m", Reason: "ok",
	})
	if err != nil {
		t.Fatalf("SignVerdict: %v", err)
	}
	v.Reason = "TAMPERED" // mutate after sign — verify must fail
	err = a.Reconcile(context.Background(), v)
	if !errors.Is(err, ErrVerdictHMACMismatch) {
		t.Fatalf("Reconcile: want ErrVerdictHMACMismatch, got %v", err)
	}
	if postCount.Load() != 0 {
		t.Fatalf("POST count = %d, want 0 (refused)", postCount.Load())
	}
}

// TestApprover_HMACUnknownKID_RefusesPost pins #658 — unknown key_id fails closed.
func TestApprover_HMACUnknownKID_RefusesPost(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)
	kr := map[string][]byte{"k1": []byte("secret-k1")}
	a := withKeyring(t, srv.URL, kr)
	v := Verdict{Outcome: schemas.VerdictPass, PRNumber: 102, HeadSHA: "s", Model: "m", Sig: []byte("x"), KeyID: "missing"}
	err := a.Reconcile(context.Background(), v)
	if !errors.Is(err, ErrVerdictHMACMismatch) {
		t.Fatalf("Reconcile: want ErrVerdictHMACMismatch, got %v", err)
	}
}

// TestApprover_HMACMissingSig_RefusesPost pins #658 — unsigned verdict against keyring-configured approver fails closed.
func TestApprover_HMACMissingSig_RefusesPost(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)
	kr := map[string][]byte{"k1": []byte("secret-k1")}
	a := withKeyring(t, srv.URL, kr)
	err := a.Reconcile(context.Background(), Verdict{Outcome: schemas.VerdictPass, PRNumber: 103, HeadSHA: "s", Model: "m"})
	if !errors.Is(err, ErrVerdictHMACMismatch) {
		t.Fatalf("Reconcile: want ErrVerdictHMACMismatch, got %v", err)
	}
}

// TestSignVerdict_Deterministic pins A-tier (h) — same input, byte-identical sig.
func TestSignVerdict_Deterministic(t *testing.T) {
	kr := map[string][]byte{"k1": []byte("secret")}
	in := Verdict{Outcome: schemas.VerdictPass, PRNumber: 7, HeadSHA: "abc", Model: "m", Reason: "ok"}
	a, _ := SignVerdict(kr, "k1", in)
	b, _ := SignVerdict(kr, "k1", in)
	if string(a.Sig) != string(b.Sig) {
		t.Fatalf("non-deterministic sig: %x vs %x", a.Sig, b.Sig)
	}
}
