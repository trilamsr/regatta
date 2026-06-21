package linear

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/trilamsr/regatta/contracts/schemas"
)

// stubServer returns an httptest.Server answering the Linear GraphQL POST
// with the canned bodies, one per List page, in call order.
func stubServer(t *testing.T, pages []string, capture *[]string) *httptest.Server {
	t.Helper()
	idx := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if capture != nil {
			*capture = append(*capture, string(body))
		}
		if r.Header.Get("Authorization") == "" {
			t.Errorf("missing Authorization header")
		}
		if idx >= len(pages) {
			t.Fatalf("server called %d times, only %d pages stubbed", idx+1, len(pages))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, pages[idx])
		idx++
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newTestAdapter(t *testing.T, endpoint string) schemas.SpecAdapter {
	t.Helper()
	a, err := NewLinearCatalog(LinearCatalogConfig{
		APIKey:   "lin_api_test",
		Team:     "MAY",
		States:   []string{"Todo", "In Progress"},
		Endpoint: endpoint,
	})
	if err != nil {
		t.Fatalf("NewLinearCatalog: %v", err)
	}
	return a
}

// TestList_MapsFields asserts a single Linear issue maps onto WorkItem
// (ID prefix, status map, SHA opaque token, identifier→LinkedArtifact) (MAY-91).
func TestList_MapsFields(t *testing.T) {
	page := `{"data":{"issues":{"pageInfo":{"hasNextPage":false,"endCursor":null},"nodes":[
		{"id":"uuid-1","identifier":"MAY-7","title":"Wire the thing",
		 "description":"Do it.\n\n## Acceptance criteria\n\n- [ ] one\n- [ ] two\n",
		 "updatedAt":"2026-06-20T10:00:00.000Z","state":{"name":"In Progress"}}
	]}}}`
	srv := stubServer(t, []string{page}, nil)
	a := newTestAdapter(t, srv.URL)

	items, err := a.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d", len(items))
	}
	got := items[0]
	if got.ID != "linear:uuid-1" {
		t.Errorf("ID = %q, want linear:uuid-1", got.ID)
	}
	if got.Title != "Wire the thing" {
		t.Errorf("Title = %q", got.Title)
	}
	if got.Status != schemas.StatusInProgress {
		t.Errorf("Status = %q, want in_progress", got.Status)
	}
	if got.LinkedArtifact != "MAY-7" {
		t.Errorf("LinkedArtifact = %q, want MAY-7", got.LinkedArtifact)
	}
	if got.Source.SHA != "2026-06-20T10:00:00.000Z" {
		t.Errorf("Source.SHA = %q, want the updatedAt token", got.Source.SHA)
	}
	if got.Source.Kind != "issue" {
		t.Errorf("Source.Kind = %q, want issue", got.Source.Kind)
	}
	if len(got.AcceptanceCriteria) != 2 {
		t.Errorf("AcceptanceCriteria = %d, want 2 (parseCriteria reuse)", len(got.AcceptanceCriteria))
	}
}

// TestList_StatusMap asserts every Linear state name maps to the right regatta Status (MAY-91).
func TestList_StatusMap(t *testing.T) {
	cases := map[string]schemas.Status{
		"Backlog":     schemas.StatusPlanned,
		"Todo":        schemas.StatusPlanned,
		"In Progress": schemas.StatusInProgress,
		"Done":        schemas.StatusDone,
		"Canceled":    schemas.StatusClosedResolved,
	}
	for stateName, want := range cases {
		page := `{"data":{"issues":{"pageInfo":{"hasNextPage":false},"nodes":[
			{"id":"u","identifier":"MAY-1","title":"t","description":"## Acceptance criteria\n- [ ] x\n",
			 "updatedAt":"2026-06-20T00:00:00.000Z","state":{"name":"` + stateName + `"}}
		]}}}`
		srv := stubServer(t, []string{page}, nil)
		a := newTestAdapter(t, srv.URL)
		items, err := a.List(context.Background())
		if err != nil {
			t.Fatalf("state %q: List: %v", stateName, err)
		}
		if items[0].Status != want {
			t.Errorf("state %q → %q, want %q", stateName, items[0].Status, want)
		}
	}
}

// TestList_Paginates asserts the adapter follows pageInfo.endCursor across pages (MAY-91).
func TestList_Paginates(t *testing.T) {
	page1 := `{"data":{"issues":{"pageInfo":{"hasNextPage":true,"endCursor":"cur-1"},"nodes":[
		{"id":"u1","identifier":"MAY-1","title":"a","description":"## Acceptance criteria\n- [ ] x\n","updatedAt":"2026-06-20T00:00:00.000Z","state":{"name":"Todo"}}
	]}}}`
	page2 := `{"data":{"issues":{"pageInfo":{"hasNextPage":false},"nodes":[
		{"id":"u2","identifier":"MAY-2","title":"b","description":"## Acceptance criteria\n- [ ] y\n","updatedAt":"2026-06-20T00:00:00.000Z","state":{"name":"Done"}}
	]}}}`
	var sent []string
	srv := stubServer(t, []string{page1, page2}, &sent)
	a := newTestAdapter(t, srv.URL)

	items, err := a.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("want 2 items across 2 pages, got %d", len(items))
	}
	if len(sent) != 2 {
		t.Fatalf("want 2 POSTs, got %d", len(sent))
	}
	if !strings.Contains(sent[1], "cur-1") {
		t.Errorf("second request did not carry endCursor cur-1: %s", sent[1])
	}
}

// TestList_RateLimitedHTTP429 asserts HTTP 429 surfaces ErrRateLimited (MAY-91).
func TestList_RateLimitedHTTP429(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-ratelimit-requests-reset", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)
	a := newTestAdapter(t, srv.URL)
	_, err := a.List(context.Background())
	if !errors.Is(err, schemas.ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
}

// TestList_RateLimitedGraphQL asserts a RATELIMITED GraphQL error code surfaces ErrRateLimited (MAY-91).
func TestList_RateLimitedGraphQL(t *testing.T) {
	page := `{"errors":[{"message":"rate limited","extensions":{"code":"RATELIMITED"}}]}`
	srv := stubServer(t, []string{page}, nil)
	a := newTestAdapter(t, srv.URL)
	_, err := a.List(context.Background())
	if !errors.Is(err, schemas.ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
}

// TestCapabilities asserts the Linear feature-flag floor (MAY-91).
func TestCapabilities(t *testing.T) {
	a := newTestAdapter(t, "http://unused")
	c := a.Capabilities()
	if c.Webhook || c.BulkUpdate {
		t.Errorf("Webhook/BulkUpdate must be false, got %v/%v", c.Webhook, c.BulkUpdate)
	}
	if c.MinPollInterval <= 0 {
		t.Errorf("MinPollInterval must be positive, got %v", c.MinPollInterval)
	}
	if len(c.SupportedStatuses) == 0 {
		t.Errorf("SupportedStatuses must be non-empty")
	}
}

// TestUpdateStatus_Unsupported asserts the read-adapter rejects writes (MAY-91).
func TestUpdateStatus_Unsupported(t *testing.T) {
	a := newTestAdapter(t, "http://unused")
	err := a.UpdateStatus(context.Background(), "linear:u", schemas.StatusDone, "cite")
	if !errors.Is(err, schemas.ErrAdapterUnsupported) {
		t.Fatalf("err = %v, want ErrAdapterUnsupported", err)
	}
}

// TestNewLinearCatalog_RequiresKeyAndTeam asserts fail-closed on missing config (MAY-91).
func TestNewLinearCatalog_RequiresKeyAndTeam(t *testing.T) {
	if _, err := NewLinearCatalog(LinearCatalogConfig{Team: "MAY"}); err == nil {
		t.Error("missing APIKey must error")
	}
	if _, err := NewLinearCatalog(LinearCatalogConfig{APIKey: "k"}); err == nil {
		t.Error("missing Team must error")
	}
}

// helper kept to assert the request body carries the team+state filter (MAY-91).
func TestList_RequestCarriesFilter(t *testing.T) {
	page := `{"data":{"issues":{"pageInfo":{"hasNextPage":false},"nodes":[]}}}`
	var sent []string
	srv := stubServer(t, []string{page}, &sent)
	a := newTestAdapter(t, srv.URL)
	if _, err := a.List(context.Background()); err != nil {
		t.Fatalf("List: %v", err)
	}
	var req struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	if err := json.Unmarshal([]byte(sent[0]), &req); err != nil {
		t.Fatalf("request body not JSON: %v", err)
	}
	if req.Variables["team"] != "MAY" {
		t.Errorf("team variable = %v, want MAY", req.Variables["team"])
	}
}
