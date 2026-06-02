package reconcile

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// adminKeyFixture is a sentinel admin-key value tests use. The R15
// log-leak test searches every captured log for this exact string to
// prove the key never appears in any record.
const adminKeyFixture = "sk-ant-admin-fixture-DO-NOT-LEAK"

// TestClient_FetchCost_SendsExpectedHeadersAndQuery pins spec §3.4 cost-report wire shape.
func TestClient_FetchCost_SendsExpectedHeadersAndQuery(t *testing.T) {
	t.Setenv(adminKeyEnv, adminKeyFixture)

	var seen struct {
		path  string
		query string
		hdrs  http.Header
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.path = r.URL.Path
		seen.query = r.URL.RawQuery
		seen.hdrs = r.Header.Clone()
		_, _ = w.Write(mustReadTestdata(t, "anthropic_cost_2026_06_01_01h.json"))
	}))
	t.Cleanup(srv.Close)

	c := NewClient(ClientConfig{
		BaseURL:         srv.URL,
		HTTPClient:      srv.Client(),
		UsageAPIKeyEnv:  adminKeyEnv,
		UserAgent:       "regatta/test (https://github.com/maydow/regatta)",
		AnthropicVerHdr: "2023-06-01",
	})
	start := time.Date(2026, 6, 1, 1, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 1, 2, 0, 0, 0, time.UTC)
	_, _, err := c.FetchCost(context.Background(), start, end, time.Hour)
	if err != nil {
		t.Fatalf("FetchCost: %v", err)
	}

	if !strings.HasSuffix(seen.path, "/v1/organizations/cost_report/messages") {
		t.Errorf("path=%q want suffix /v1/organizations/cost_report/messages", seen.path)
	}
	for _, want := range []string{
		"starting_at=2026-06-01T01%3A00%3A00Z",
		"ending_at=2026-06-01T02%3A00%3A00Z",
		"bucket_width=1h",
		"group_by%5B%5D=model",
	} {
		if !strings.Contains(seen.query, want) {
			t.Errorf("query=%q missing %q", seen.query, want)
		}
	}
	if got := seen.hdrs.Get("anthropic-version"); got != "2023-06-01" {
		t.Errorf("anthropic-version=%q want 2023-06-01", got)
	}
	if got := seen.hdrs.Get("x-api-key"); got != adminKeyFixture {
		t.Errorf("x-api-key=%q want fixture", got)
	}
	if !strings.HasPrefix(seen.hdrs.Get("User-Agent"), "regatta/") {
		t.Errorf("User-Agent=%q want regatta/ prefix", seen.hdrs.Get("User-Agent"))
	}
}

// TestClient_FetchCost_404ReturnsErrCostAPIUnavailable pins spec §3.4 fallback signal.
func TestClient_FetchCost_404ReturnsErrCostAPIUnavailable(t *testing.T) {
	t.Setenv(adminKeyEnv, adminKeyFixture)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	c := NewClient(ClientConfig{
		BaseURL:        srv.URL,
		HTTPClient:     srv.Client(),
		UsageAPIKeyEnv: adminKeyEnv,
	})
	_, _, err := c.FetchCost(context.Background(),
		time.Now().Add(-time.Hour), time.Now(), time.Hour)
	if !errors.Is(err, ErrCostAPIUnavailable) {
		t.Fatalf("err=%v want ErrCostAPIUnavailable", err)
	}
}

// TestClient_429ReturnsErrRateLimitedWithRetryAfter pins R3 + A3 retry-after parsing.
func TestClient_429ReturnsErrRateLimitedWithRetryAfter(t *testing.T) {
	t.Setenv(adminKeyEnv, adminKeyFixture)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("retry-after", "12")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)

	c := NewClient(ClientConfig{
		BaseURL:        srv.URL,
		HTTPClient:     srv.Client(),
		UsageAPIKeyEnv: adminKeyEnv,
	})
	_, _, err := c.FetchCost(context.Background(),
		time.Now().Add(-time.Hour), time.Now(), time.Hour)
	var rl *RateLimitedError
	if !errors.As(err, &rl) {
		t.Fatalf("err=%v want *RateLimitedError", err)
	}
	if rl.RetryAfter != 12*time.Second {
		t.Errorf("RetryAfter=%v want 12s", rl.RetryAfter)
	}
}

// TestClient_NeverLogsKeyValue pins R15 — admin key absent from every error string.
func TestClient_NeverLogsKeyValue(t *testing.T) {
	t.Setenv(adminKeyEnv, adminKeyFixture)

	mkSrv := func(status int, hdrs map[string]string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			for k, v := range hdrs {
				w.Header().Set(k, v)
			}
			w.WriteHeader(status)
		}))
	}
	cases := []struct {
		name   string
		status int
		hdrs   map[string]string
	}{
		{"401", 401, nil},
		{"403", 403, nil},
		{"404", 404, nil},
		{"429", 429, map[string]string{"retry-after": "1"}},
		{"500", 500, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := mkSrv(c.status, c.hdrs)
			t.Cleanup(srv.Close)
			client := NewClient(ClientConfig{
				BaseURL:        srv.URL,
				HTTPClient:     srv.Client(),
				UsageAPIKeyEnv: adminKeyEnv,
			})
			_, _, err := client.FetchCost(context.Background(),
				time.Now().Add(-time.Hour), time.Now(), time.Hour)
			if err == nil {
				t.Fatalf("expected error on status %d", c.status)
			}
			if strings.Contains(err.Error(), adminKeyFixture) {
				t.Errorf("err msg leaks admin key: %q", err.Error())
			}
		})
	}
}

// TestClient_FetchUsage_DecodesUsageBuckets pins the Usage API fallback decoder shape.
func TestClient_FetchUsage_DecodesUsageBuckets(t *testing.T) {
	t.Setenv(adminKeyEnv, adminKeyFixture)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v1/organizations/usage_report/messages") {
			http.Error(w, "wrong path", 400)
			return
		}
		_, _ = w.Write(mustReadTestdata(t, "anthropic_usage_2026_06_01_01h.json"))
	}))
	t.Cleanup(srv.Close)

	c := NewClient(ClientConfig{
		BaseURL:        srv.URL,
		HTTPClient:     srv.Client(),
		UsageAPIKeyEnv: adminKeyEnv,
	})
	resp, _, err := c.FetchUsage(context.Background(),
		time.Date(2026, 6, 1, 1, 0, 0, 0, time.UTC),
		time.Date(2026, 6, 1, 2, 0, 0, 0, time.UTC), time.Hour)
	if err != nil {
		t.Fatalf("FetchUsage: %v", err)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("len(resp.Data)=%d want 2", len(resp.Data))
	}
	if resp.Data[0].Model != "claude-sonnet-4-7" {
		t.Errorf("Data[0].Model=%q want claude-sonnet-4-7", resp.Data[0].Model)
	}
	if resp.Data[0].UncachedInputTokens != 1000000 {
		t.Errorf("Data[0].UncachedInputTokens=%d want 1000000", resp.Data[0].UncachedInputTokens)
	}
}

func mustReadTestdata(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read testdata %s: %v", name, err)
	}
	return b
}
