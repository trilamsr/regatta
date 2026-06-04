package alarmwebhook

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/ghclient"
)

// fakeGitHub records every call and serves canned ListOpenIssuesByLabel
// responses. Tests mutate Existing before posting; the handler reads
// it under the mu lock.
type fakeGitHub struct {
	mu          sync.Mutex
	Existing    map[string][]ghclient.Issue
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

func (f *fakeGitHub) ListOpenIssuesByLabel(_ context.Context, label, alertname string) ([]ghclient.Issue, error) {
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

func (f *fakeGitHub) ListIssuesByLabelPaginated(_ context.Context, _ string, _ ghclient.ListIssuesOpts) ([]ghclient.Issue, error) {
	return nil, nil
}

func (f *fakeGitHub) GetIssue(_ context.Context, _ int) (ghclient.Issue, error) {
	return ghclient.Issue{}, nil
}

func (f *fakeGitHub) EditIssueBody(_ context.Context, _ int, _ string) error { return nil }

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
		Existing: map[string][]ghclient.Issue{
			"HighErrorRate": {{Number: 42, Title: "[obs-alert] HighErrorRate firing (critical)"}},
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

// TestHandler_PayloadTooLarge_Returns500_NotFromMaxBytes asserts oversize body yields 500 so AlertManager retries instead of dropping silently.
func TestHandler_PayloadTooLarge_Returns500_NotFromMaxBytes(t *testing.T) {
	fake := &fakeGitHub{}
	h := &Handler{Client: fake}
	big := make([]byte, MaxBodyBytes+1024)
	for i := range big {
		big[i] = 'a'
	}
	rr := post(t, h, big)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d want 500; AM treats 4xx as permanent and would drop the alert", rr.Code)
	}
}

// TestHandler_MalformedJSON_Returns400 asserts an invalid JSON body within the size cap returns 400 (truly malformed; retry would not help).
func TestHandler_MalformedJSON_Returns400(t *testing.T) {
	fake := &fakeGitHub{}
	h := &Handler{Client: fake}
	rr := post(t, h, []byte("{not-json-but-within-cap}"))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", rr.Code)
	}
}

// TestHandler_4MiBExactBoundary_Succeeds asserts a body exactly at MaxBodyBytes parses to 2xx (boundary check after limit raise).
func TestHandler_4MiBExactBoundary_Succeeds(t *testing.T) {
	if MaxBodyBytes != 4<<20 {
		t.Fatalf("MaxBodyBytes: got %d want %d (4 MiB)", MaxBodyBytes, 4<<20)
	}
	fake := &fakeGitHub{}
	h := &Handler{Client: fake}
	// Build a valid AlertManager payload, then pad annotations to exact 4 MiB.
	prefix := `{"status":"firing","alerts":[{"status":"firing","labels":{"alertname":"X","severity":"critical"},"annotations":{"summary":"`
	suffix := `"}}]}`
	pad := MaxBodyBytes - len(prefix) - len(suffix)
	if pad <= 0 {
		t.Fatalf("payload skeleton too large: %d > %d", len(prefix)+len(suffix), MaxBodyBytes)
	}
	body := make([]byte, 0, MaxBodyBytes)
	body = append(body, prefix...)
	for i := 0; i < pad; i++ {
		body = append(body, 'a')
	}
	body = append(body, suffix...)
	if len(body) != MaxBodyBytes {
		t.Fatalf("body length: got %d want %d", len(body), MaxBodyBytes)
	}
	rr := post(t, h, body)
	if rr.Code >= 400 {
		t.Fatalf("status: got %d want 2xx for body == MaxBodyBytes; body=%s", rr.Code, rr.Body.String())
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

// TestAlarmWebhook_ConcurrentStormProducesOneIssue asserts a burst of 20 simultaneous firings of one alertname yields exactly one CreateIssue + 19 comments.
func TestAlarmWebhook_ConcurrentStormProducesOneIssue(t *testing.T) {
	fake := &fakeGitHub{}
	h := &Handler{Client: fake}
	body := loadSample(t)

	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			rr := post(t, h, body)
			if rr.Code != http.StatusAccepted {
				t.Errorf("status %d", rr.Code)
			}
		}()
	}
	wg.Wait()

	if len(fake.Created) != 1 {
		t.Fatalf("creates: got %d want 1; storm must collapse to one issue", len(fake.Created))
	}
	if len(fake.Comments) != n-1 {
		t.Fatalf("comments: got %d want %d", len(fake.Comments), n-1)
	}
}

// TestAlarmWebhook_DedupCacheShortCircuits asserts back-to-back firings of the same alertname only hit the GH search API once thanks to the 60s cache.
func TestAlarmWebhook_DedupCacheShortCircuits(t *testing.T) {
	fake := &fakeGitHub{
		Existing: map[string][]ghclient.Issue{
			"HighErrorRate": {{Number: 7, Title: "[obs-alert] HighErrorRate firing (critical)"}},
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

// TestHandler_MutexGC_EvictsStaleAlertnames asserts entries idle past the TTL are dropped by reapStale.
func TestHandler_MutexGC_EvictsStaleAlertnames(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	h := &Handler{Client: &fakeGitHub{}, Now: clock.Now}
	const n = 1000
	for i := 0; i < n; i++ {
		h.lockAlertname(alertNameFor(i))
	}
	if got := countPerAlertname(h); got != n {
		t.Fatalf("setup: perAlertname count got %d want %d", got, n)
	}
	clock.advance(25 * time.Hour)
	h.reapStale(clock.Now())
	if got := countPerAlertname(h); got != 0 {
		t.Fatalf("after reap: perAlertname count got %d want 0", got)
	}
}

// TestHandler_MutexGC_PreservesActiveAlertnames asserts a freshly-touched alertname survives a reap pass.
func TestHandler_MutexGC_PreservesActiveAlertnames(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	h := &Handler{Client: &fakeGitHub{}, Now: clock.Now}
	for i := 0; i < 10; i++ {
		h.lockAlertname(alertNameFor(i))
	}
	clock.advance(25 * time.Hour)
	// Touch one alertname so its lastSeenAt advances to "now".
	h.lockAlertname(alertNameFor(3))
	h.reapStale(clock.Now())
	if got := countPerAlertname(h); got != 1 {
		t.Fatalf("after reap: perAlertname count got %d want 1", got)
	}
	if _, ok := h.perAlertname.Load(alertNameFor(3)); !ok {
		t.Fatalf("active alertname %q must survive reap", alertNameFor(3))
	}
}

// TestHandler_MutexGC_SkipsHeldEntries asserts a mutex currently locked is not evicted even if its timestamp is stale.
func TestHandler_MutexGC_SkipsHeldEntries(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	h := &Handler{Client: &fakeGitHub{}, Now: clock.Now}
	lock := h.lockAlertname("Held")
	lock.mu.Lock()
	defer lock.mu.Unlock()
	clock.advance(25 * time.Hour)
	h.reapStale(clock.Now())
	if _, ok := h.perAlertname.Load("Held"); !ok {
		t.Fatalf("held alertname must not be evicted while in use")
	}
}

// TestHandler_MutexGC_RaceWithConcurrentReceive asserts reaper running while POST handlers fire on the same alertname is race-free.
func TestHandler_MutexGC_RaceWithConcurrentReceive(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	fake := &fakeGitHub{}
	h := &Handler{Client: fake, Now: clock.Now}
	body := loadSample(t)

	stop := make(chan struct{})
	var reaperWG sync.WaitGroup
	reaperWG.Add(1)
	go func() {
		defer reaperWG.Done()
		for {
			select {
			case <-stop:
				return
			default:
				h.reapStale(clock.Now())
			}
		}
	}()

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			rr := post(t, h, body)
			if rr.Code != http.StatusAccepted {
				t.Errorf("status %d", rr.Code)
			}
		}()
	}
	wg.Wait()
	close(stop)
	reaperWG.Wait()

	// Dedup contract must still hold: storm collapses to one CreateIssue.
	if len(fake.Created) != 1 {
		t.Fatalf("creates: got %d want 1", len(fake.Created))
	}
}

// TestHandler_MutexGC_BackgroundReaperStops asserts startReaper's returned stop func halts the goroutine cleanly.
func TestHandler_MutexGC_BackgroundReaperStops(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	h := &Handler{Client: &fakeGitHub{}, Now: clock.Now}
	stop := h.startReaper(1 * time.Millisecond)
	if stop == nil {
		t.Fatal("startReaper must return non-nil stop func")
	}
	// Drive a few ticks then stop; a leaked goroutine would show up under -race or via go.uber.org/goleak in CI.
	time.Sleep(10 * time.Millisecond)
	stop()
}

func alertNameFor(i int) string {
	return "alert-" + strconv.Itoa(i)
}

func countPerAlertname(h *Handler) int {
	n := 0
	h.perAlertname.Range(func(_, _ any) bool {
		n++
		return true
	})
	return n
}

