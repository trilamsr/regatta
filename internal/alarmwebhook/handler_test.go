package alarmwebhook

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
)

// fakeGitHub records every call and serves canned ListOpenIssuesByLabel
// responses. Tests mutate Existing before posting; the handler reads
// it under the mu lock.
type fakeGitHub struct {
	mu          sync.Mutex
	Existing    map[string][]Issue
	Created     []createCall
	Comments    []commentCall
	ListErr     error
	CreateErr   error
	CommentErr  error
	ListCalls   int
	CreateCalls int
}

type createCall struct {
	Title  string
	Body   string
	Labels []string
}

type commentCall struct {
	Number int
	Body   string
}

func (f *fakeGitHub) ListOpenIssuesByLabel(_ context.Context, label, alertname string) ([]Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ListCalls++
	if f.ListErr != nil {
		return nil, f.ListErr
	}
	if f.Existing == nil {
		return nil, nil
	}
	return f.Existing[alertname], nil
}

func (f *fakeGitHub) CreateIssue(_ context.Context, title, body string, labels []string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.CreateErr != nil {
		return 0, f.CreateErr
	}
	f.CreateCalls++
	f.Created = append(f.Created, createCall{Title: title, Body: body, Labels: labels})
	return 1000 + f.CreateCalls, nil
}

func (f *fakeGitHub) CommentOnIssue(_ context.Context, number int, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.CommentErr != nil {
		return f.CommentErr
	}
	f.Comments = append(f.Comments, commentCall{Number: number, Body: body})
	return nil
}

func loadSample(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/alertmanager-sample.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return data
}

func post(t *testing.T, h http.Handler, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

// TestAlarmWebhook_ParsesAlertManagerSampleJSON asserts payload decode against the literal upstream AlertManager sample fixture.
func TestAlarmWebhook_ParsesAlertManagerSampleJSON(t *testing.T) {
	data := loadSample(t)
	var p Payload
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if p.Status != "firing" {
		t.Fatalf("status: got %q want firing", p.Status)
	}
	if len(p.Alerts) != 1 {
		t.Fatalf("alerts: got %d want 1", len(p.Alerts))
	}
	if p.Alerts[0].Alertname() != "HighErrorRate" {
		t.Fatalf("alertname: got %q want HighErrorRate", p.Alerts[0].Alertname())
	}
	if p.Alerts[0].Severity() != "critical" {
		t.Fatalf("severity: got %q want critical", p.Alerts[0].Severity())
	}
}

// TestAlarmWebhook_FirstFiring_CreatesIssue asserts a fresh alertname triggers one CreateIssue with autonomous+obs-alert+severity labels.
func TestAlarmWebhook_FirstFiring_CreatesIssue(t *testing.T) {
	fake := &fakeGitHub{}
	h := &Handler{Client: fake}
	rr := post(t, h, loadSample(t))

	if rr.Code != http.StatusAccepted {
		t.Fatalf("status: got %d want 202; body=%s", rr.Code, rr.Body.String())
	}
	if len(fake.Created) != 1 {
		t.Fatalf("creates: got %d want 1", len(fake.Created))
	}
	if len(fake.Comments) != 0 {
		t.Fatalf("comments: got %d want 0", len(fake.Comments))
	}
	got := map[string]bool{}
	for _, l := range fake.Created[0].Labels {
		got[l] = true
	}
	for _, want := range []string{"autonomous", "obs-alert", "critical"} {
		if !got[want] {
			t.Fatalf("label %q missing; got %v", want, fake.Created[0].Labels)
		}
	}
}

// TestAlarmWebhook_SecondFiringSameAlertname_CommentsOnExistingIssue asserts a refiring routes to CommentOnIssue and never re-creates.
func TestAlarmWebhook_SecondFiringSameAlertname_CommentsOnExistingIssue(t *testing.T) {
	fake := &fakeGitHub{
		Existing: map[string][]Issue{
			"HighErrorRate": {{Number: 42, Title: "[obs-alert] HighErrorRate firing (critical)", State: "open"}},
		},
	}
	h := &Handler{Client: fake}
	rr := post(t, h, loadSample(t))

	if rr.Code != http.StatusAccepted {
		t.Fatalf("status: got %d want 202; body=%s", rr.Code, rr.Body.String())
	}
	if len(fake.Created) != 0 {
		t.Fatalf("creates: got %d want 0", len(fake.Created))
	}
	if len(fake.Comments) != 1 {
		t.Fatalf("comments: got %d want 1", len(fake.Comments))
	}
	if fake.Comments[0].Number != 42 {
		t.Fatalf("comment target: got #%d want #42", fake.Comments[0].Number)
	}
}

// TestAlarmWebhook_BodyContainsAllRequiredFields asserts the issue body carries alarm name, threshold, current value, dashboard URL, and replay command.
func TestAlarmWebhook_BodyContainsAllRequiredFields(t *testing.T) {
	fake := &fakeGitHub{}
	h := &Handler{Client: fake}
	rr := post(t, h, loadSample(t))
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status: got %d want 202", rr.Code)
	}
	if len(fake.Created) != 1 {
		t.Fatalf("creates: got %d want 1", len(fake.Created))
	}
	body := fake.Created[0].Body
	for _, want := range []string{
		"HighErrorRate",
		"0.05",
		"0.087",
		"https://grafana.example.com/d/regatta/orchestrator",
		"regatta replay --substrate-fixture orchestrator",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q; full body:\n%s", want, body)
		}
	}
}

// TestAlarmWebhook_RejectsNonPOST asserts GET/PUT/DELETE return 405 without a GH API call.
func TestAlarmWebhook_RejectsNonPOST(t *testing.T) {
	fake := &fakeGitHub{}
	h := &Handler{Client: fake}
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		req := httptest.NewRequest(method, "/webhook", nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: got %d want 405", method, rr.Code)
		}
	}
	if fake.ListCalls+len(fake.Created)+len(fake.Comments) != 0 {
		t.Fatalf("non-POST should never touch GH; got %d list, %d create, %d comment",
			fake.ListCalls, len(fake.Created), len(fake.Comments))
	}
}

// TestAlarmWebhook_RejectsMalformedJSON asserts a junk body returns 400 not a panic.
func TestAlarmWebhook_RejectsMalformedJSON(t *testing.T) {
	fake := &fakeGitHub{}
	h := &Handler{Client: fake}
	rr := post(t, h, []byte("{not-json"))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400; body=%s", rr.Code, rr.Body.String())
	}
}

// TestAlarmWebhook_CapsBodySize asserts an oversized body is rejected before decoding instead of OOMing the receiver.
func TestAlarmWebhook_CapsBodySize(t *testing.T) {
	fake := &fakeGitHub{}
	h := &Handler{Client: fake}
	big := make([]byte, MaxBodyBytes+1024)
	for i := range big {
		big[i] = 'a'
	}
	rr := post(t, h, big)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400; oversized body must fail", rr.Code)
	}
}

// TestAlarmWebhook_EmptyAlertsReturns204 asserts an empty alerts slice short-circuits without a GH call.
func TestAlarmWebhook_EmptyAlertsReturns204(t *testing.T) {
	fake := &fakeGitHub{}
	h := &Handler{Client: fake}
	rr := post(t, h, []byte(`{"status":"firing","alerts":[]}`))
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status: got %d want 204", rr.Code)
	}
	if fake.ListCalls != 0 {
		t.Fatalf("empty alerts should skip GH; got %d list calls", fake.ListCalls)
	}
}

// TestAlarmWebhook_GitHubErrorReturns502 asserts a GH API failure surfaces as 502 with the body propagated for operator triage.
func TestAlarmWebhook_GitHubErrorReturns502(t *testing.T) {
	fake := &fakeGitHub{ListErr: errors.New("rate limited")}
	h := &Handler{Client: fake}
	rr := post(t, h, loadSample(t))
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("status: got %d want 502; body=%s", rr.Code, rr.Body.String())
	}
}

// TestAlarmWebhook_LabelsAreLowercaseAndDistinct asserts the three issue labels match the autonomous+obs-alert+severity contract verbatim.
func TestAlarmWebhook_LabelsAreLowercaseAndDistinct(t *testing.T) {
	fake := &fakeGitHub{}
	h := &Handler{Client: fake}
	rr := post(t, h, loadSample(t))
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status: %d", rr.Code)
	}
	got := fake.Created[0].Labels
	if len(got) != 3 {
		t.Fatalf("labels: got %d (%v) want exactly 3", len(got), got)
	}
	want := []string{"autonomous", "obs-alert", "critical"}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("label[%d]: got %q want %q", i, got[i], w)
		}
	}
}

// TestAlarmWebhook_DedupCacheShortCircuits asserts back-to-back firings of the same alertname only hit the GH search API once thanks to the 60s cache.
func TestAlarmWebhook_DedupCacheShortCircuits(t *testing.T) {
	fake := &fakeGitHub{
		Existing: map[string][]Issue{
			"HighErrorRate": {{Number: 7, Title: "[obs-alert] HighErrorRate firing (critical)", State: "open"}},
		},
	}
	h := &Handler{Client: fake}
	for i := 0; i < 5; i++ {
		rr := post(t, h, loadSample(t))
		if rr.Code != http.StatusAccepted {
			t.Fatalf("iter %d status %d", i, rr.Code)
		}
	}
	if fake.ListCalls != 1 {
		t.Fatalf("list calls: got %d want 1; cache must absorb the storm", fake.ListCalls)
	}
	if len(fake.Comments) != 5 {
		t.Fatalf("comments: got %d want 5", len(fake.Comments))
	}
	if len(fake.Created) != 0 {
		t.Fatalf("creates: got %d want 0", len(fake.Created))
	}
}

