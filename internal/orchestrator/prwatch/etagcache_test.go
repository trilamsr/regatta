package prwatch

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

// fallbackStub is a hermetic PRLister captured calls fall back to when
// the ETag HTTP path declines (rate-limit, 5xx). Keyed by branch like
// the production GHCLILister.
type fallbackStub struct {
	calls    int32
	byBranch map[string][]PullRequest
	err      error
}

func (f *fallbackStub) ListOpenByHead(_ context.Context, branch, _ string) ([]PullRequest, error) {
	atomic.AddInt32(&f.calls, 1)
	if f.err != nil {
		return nil, f.err
	}
	return append([]PullRequest(nil), f.byBranch[branch]...), nil
}

// TestETagLister_200_RefreshesCache decodes REST body + seeds cache (AC4).
func TestETagLister_200_RefreshesCache(t *testing.T) {
	hits := int32(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("ETag", `W/"v1"`)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{"number":42,"head":{"sha":"deadbeef","ref":"regatta/agent-7","user":{"login":"me"}},"state":"open","title":"[agent-7] x","mergeable_state":"clean"}]`))
	}))
	t.Cleanup(srv.Close)

	fb := &fallbackStub{}
	el := NewETagLister(ETagListerConfig{
		Owner:    "trilamsr",
		Repo:     "regatta",
		Token:    "tok",
		BaseURL:  srv.URL,
		Fallback: fb,
		Client:   srv.Client(),
	})

	prs, err := el.ListOpenByHead(context.Background(), "regatta/agent-7", "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(prs) != 1 || prs[0].Number != 42 || prs[0].HeadRefOid != "deadbeef" ||
		prs[0].HeadRefName != "regatta/agent-7" || prs[0].AuthorLogin != "me" ||
		prs[0].MergeStateStatus != "CLEAN" {
		t.Fatalf("decode wrong: %+v", prs)
	}
	if atomic.LoadInt32(&hits) != 1 {
		t.Fatalf("want 1 http hit, got %d", hits)
	}
	if atomic.LoadInt32(&fb.calls) != 0 {
		t.Fatalf("fallback should NOT fire on 200; calls=%d", fb.calls)
	}
}

// TestETagLister_304_UsesCache sends If-None-Match + serves cache on 304 (AC3).
func TestETagLister_304_UsesCache(t *testing.T) {
	var ifNoneMatch atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hdr := r.Header.Get("If-None-Match")
		if hdr != "" {
			ifNoneMatch.Store(hdr)
			w.Header().Set("ETag", `W/"v1"`)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `W/"v1"`)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{"number":42,"head":{"sha":"deadbeef","ref":"regatta/agent-7","user":{"login":"me"}},"state":"open","title":"[agent-7] x","mergeable_state":"clean"}]`))
	}))
	t.Cleanup(srv.Close)

	fb := &fallbackStub{}
	el := NewETagLister(ETagListerConfig{
		Owner:    "trilamsr",
		Repo:     "regatta",
		Token:    "tok",
		BaseURL:  srv.URL,
		Fallback: fb,
		Client:   srv.Client(),
	})

	// Prime cache.
	if _, err := el.ListOpenByHead(context.Background(), "regatta/agent-7", ""); err != nil {
		t.Fatalf("prime: %v", err)
	}
	// Second call: server returns 304, lister must serve cached snapshot.
	prs, err := el.ListOpenByHead(context.Background(), "regatta/agent-7", "")
	if err != nil {
		t.Fatalf("304: %v", err)
	}
	if len(prs) != 1 || prs[0].HeadRefOid != "deadbeef" {
		t.Fatalf("304 cache miss: %+v", prs)
	}
	got, _ := ifNoneMatch.Load().(string)
	if got != `W/"v1"` {
		t.Fatalf(`want If-None-Match=W/"v1", got %q`, got)
	}
	if atomic.LoadInt32(&fb.calls) != 0 {
		t.Fatalf("fallback should NOT fire on 304; calls=%d", fb.calls)
	}
}

// TestETagLister_SecondaryRateLimit_FallsBack routes 403+rate-limit to fallback (AC5).
func TestETagLister_SecondaryRateLimit_FallsBack(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Mimic GH's secondary-rate-limit response shape.
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusForbidden)
		_, _ = fmt.Fprintln(w, `{"message":"You have exceeded a secondary rate limit"}`)
	}))
	t.Cleanup(srv.Close)

	fb := &fallbackStub{
		byBranch: map[string][]PullRequest{
			"regatta/agent-7": {{Number: 42, HeadRefOid: "fbsha", State: "OPEN", HeadRefName: "regatta/agent-7"}},
		},
	}
	el := NewETagLister(ETagListerConfig{
		Owner:    "trilamsr",
		Repo:     "regatta",
		Token:    "tok",
		BaseURL:  srv.URL,
		Fallback: fb,
		Client:   srv.Client(),
	})

	prs, err := el.ListOpenByHead(context.Background(), "regatta/agent-7", "")
	if err != nil {
		t.Fatalf("fallback list: %v", err)
	}
	if atomic.LoadInt32(&fb.calls) != 1 {
		t.Fatalf("want fallback fired exactly once, got %d", fb.calls)
	}
	if len(prs) != 1 || prs[0].HeadRefOid != "fbsha" {
		t.Fatalf("want fallback PR returned, got %+v", prs)
	}
}

// TestETagLister_TitlePrefixDelegated routes title-prefix probe to fallback (AC2).
func TestETagLister_TitlePrefixDelegated(t *testing.T) {
	httpHits := int32(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&httpHits, 1)
		w.Header().Set("ETag", `W/"v1"`)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)

	fb := &fallbackStub{
		byBranch: map[string][]PullRequest{
			"regatta/agent-7": {{Number: 99, HeadRefOid: "forksha", HeadRefName: "topic"}},
		},
	}
	el := NewETagLister(ETagListerConfig{
		Owner:    "trilamsr",
		Repo:     "regatta",
		Token:    "tok",
		BaseURL:  srv.URL,
		Fallback: fb,
		Client:   srv.Client(),
	})

	prs, err := el.ListOpenByHead(context.Background(), "regatta/agent-7", "[agent-7]")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	// HTTP head-query AND fallback (title-prefix) both run.
	if atomic.LoadInt32(&httpHits) != 1 {
		t.Fatalf("want 1 http hit for head, got %d", httpHits)
	}
	if atomic.LoadInt32(&fb.calls) != 1 {
		t.Fatalf("want 1 fallback call for title-prefix, got %d", fb.calls)
	}
	// Returned set is the union — head HTTP empty + fallback PR.
	if len(prs) != 1 || prs[0].HeadRefOid != "forksha" {
		t.Fatalf("union wrong: %+v", prs)
	}
}

// TestETagLister_500_FallsBack routes transient 5xx to fallback (AC5 defensive).
func TestETagLister_500_FallsBack(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	fb := &fallbackStub{
		byBranch: map[string][]PullRequest{
			"regatta/agent-7": {{Number: 1, HeadRefOid: "x", HeadRefName: "regatta/agent-7"}},
		},
	}
	el := NewETagLister(ETagListerConfig{
		Owner:    "trilamsr",
		Repo:     "regatta",
		Token:    "tok",
		BaseURL:  srv.URL,
		Fallback: fb,
		Client:   srv.Client(),
	})

	prs, err := el.ListOpenByHead(context.Background(), "regatta/agent-7", "")
	if err != nil {
		t.Fatalf("want fallback success, got %v", err)
	}
	if atomic.LoadInt32(&fb.calls) != 1 {
		t.Fatalf("want fallback fired, got %d", fb.calls)
	}
	if len(prs) != 1 || prs[0].HeadRefOid != "x" {
		t.Fatalf("fallback PR not returned: %+v", prs)
	}
}

// TestETagLister_BranchNameEncoded percent-encodes the `/` in `regatta/agent-N` (RFC 3986).
func TestETagLister_BranchNameEncoded(t *testing.T) {
	var capturedRawQuery atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRawQuery.Store(r.URL.RawQuery)
		// Echo back the branch the server PARSED out of the head
		// param so the test asserts upstream sees the expected value.
		head := r.URL.Query().Get("head")
		_ = head // captured via RawQuery; URL.Query parses correctly either encoded or raw
		w.Header().Set("ETag", `W/"v1"`)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)

	el := NewETagLister(ETagListerConfig{
		Owner:    "trilamsr",
		Repo:     "regatta",
		Token:    "tok",
		BaseURL:  srv.URL,
		Fallback: &fallbackStub{},
		Client:   srv.Client(),
	})
	if _, err := el.ListOpenByHead(context.Background(), "regatta/agent-7", ""); err != nil {
		t.Fatalf("list: %v", err)
	}

	got, _ := capturedRawQuery.Load().(string)
	// `/` in the branch must be %2F; `:` in `owner:branch` must be %3A.
	// url.Values.Encode produces both per RFC 3986.
	if !strings.Contains(got, "head=trilamsr%3Aregatta%2Fagent-7") {
		t.Fatalf("query missing percent-encoded head; got %q", got)
	}
	// And the parsed value MUST equal the original literal so GitHub
	// receives the byte sequence the orchestrator pinned.
	parsed, err := url.ParseQuery(got)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if h := parsed.Get("head"); h != "trilamsr:regatta/agent-7" {
		t.Fatalf("parsed head=%q, want trilamsr:regatta/agent-7", h)
	}
}

// TestETagLister_ForgetEvictsCache drops cached {etag, snapshot} for branch.
func TestETagLister_ForgetEvictsCache(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("ETag", `W/"v1"`)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{"number":1,"head":{"sha":"x","ref":"regatta/agent-7"},"state":"open"}]`))
	}))
	t.Cleanup(srv.Close)

	el := NewETagLister(ETagListerConfig{
		Owner:    "trilamsr",
		Repo:     "regatta",
		Token:    "tok",
		BaseURL:  srv.URL,
		Fallback: &fallbackStub{},
		Client:   srv.Client(),
	})
	if _, err := el.ListOpenByHead(context.Background(), "regatta/agent-7", ""); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if el.CacheLen() != 1 {
		t.Fatalf("want cache=1 after seed, got %d", el.CacheLen())
	}
	el.Forget("regatta/agent-7")
	if el.CacheLen() != 0 {
		t.Fatalf("want cache=0 after Forget, got %d", el.CacheLen())
	}
	// Forget on a never-cached branch is a silent no-op.
	el.Forget("never-seen")
	if el.CacheLen() != 0 {
		t.Fatalf("want cache=0 after no-op Forget, got %d", el.CacheLen())
	}
}

// TestETagLister_FallbackError_Surfaces propagates fallback error (no silent swallow).
func TestETagLister_FallbackError_Surfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	wantErr := errors.New("gh down")
	fb := &fallbackStub{err: wantErr}
	el := NewETagLister(ETagListerConfig{
		Owner:    "trilamsr",
		Repo:     "regatta",
		Token:    "tok",
		BaseURL:  srv.URL,
		Fallback: fb,
		Client:   srv.Client(),
	})

	_, err := el.ListOpenByHead(context.Background(), "regatta/agent-7", "")
	if !errors.Is(err, wantErr) {
		t.Fatalf("want fallback err propagated, got %v", err)
	}
}
