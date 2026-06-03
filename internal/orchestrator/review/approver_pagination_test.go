package review

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/trilamsr/regatta/contracts/schemas"
)

// TestLookupApprovingReview_Above30Reviews_FindsApproval pins #627 —
// approval lives on page 2; single-page logic would miss it.
func TestLookupApprovingReview_Above30Reviews_FindsApproval(t *testing.T) {
	mux := http.NewServeMux()
	var page2URL string
	mux.HandleFunc("/repos/acme/regatta/pulls/200", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"user":{"login":"regatta-bot"},"head":{"sha":"s200"}}`))
	})
	mux.HandleFunc("/repos/acme/regatta/pulls/200/reviews", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			_, _ = w.Write([]byte(`{"id": 9}`))
			return
		}
		// Page 1: 100 reviews — none from the reviewer-bot. Page 2 holds the approval.
		if r.URL.Query().Get("page") == "" || r.URL.Query().Get("page") == "1" {
			w.Header().Set("Link", fmt.Sprintf(`<%s>; rel="next"`, page2URL))
			var rows []string
			for i := 0; i < 100; i++ {
				rows = append(rows, `{"id":1,"state":"COMMENTED","user":{"login":"random"}}`)
			}
			_, _ = w.Write([]byte("[" + strings.Join(rows, ",") + "]"))
			return
		}
		// Page 2: includes the reviewer-bot APPROVED.
		_, _ = w.Write([]byte(`[{"id":42,"state":"APPROVED","commit_id":"s200","user":{"login":"regatta-reviewer-bot"}}]`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	page2URL = srv.URL + "/repos/acme/regatta/pulls/200/reviews?per_page=100&page=2"

	a := newTestApprover(t, srv.URL)
	got, err := a.lookupApprovingReview(context.Background(), 200)
	if err != nil {
		t.Fatalf("lookupApprovingReview: %v", err)
	}
	if got == nil {
		t.Fatalf("approval not found across paginated reviews")
	}
	if got.ID != 42 {
		t.Errorf("got id=%d, want 42", got.ID)
	}
}

// TestLookupApprovingReview_NoNextLink_TerminatesCleanly pins the loop
// terminator — single-page responses MUST NOT loop forever.
func TestLookupApprovingReview_NoNextLink_TerminatesCleanly(t *testing.T) {
	var calls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/regatta/pulls/201/reviews", func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`[]`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	a := newTestApprover(t, srv.URL)
	got, err := a.lookupApprovingReview(context.Background(), 201)
	if err != nil || got != nil {
		t.Fatalf("got=%v err=%v, want nil/nil", got, err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1 (no Link header should not loop)", calls.Load())
	}
}

// TestParseNextLink_ExtractsRelNext pins the header parser.
func TestParseNextLink_ExtractsRelNext(t *testing.T) {
	in := `<https://api.github.com/x?page=2>; rel="next", <https://api.github.com/x?page=5>; rel="last"`
	want := "https://api.github.com/x?page=2"
	if got := parseNextLink(in); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := parseNextLink(""); got != "" {
		t.Fatalf("empty header should return empty, got %q", got)
	}
}

// avoidUnused references schemas to silence the linter if a follow-up edit drops the verdict-typed
// arg from tests above. Keeps the import set stable across refactors.
var _ = schemas.VerdictPass
