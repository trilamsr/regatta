//go:build docker

package docker_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestComposeAlertmanagerRoundTrip posts a synthetic alert to Alertmanager v2, asserts 2xx, then polls GET /api/v2/alerts until the alert is visible (R-MEGA-3 INT-1 #1363).
func TestComposeAlertmanagerRoundTrip(t *testing.T) {
	requireDocker(t)

	repoRoot := repoRootFromCWD(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	out, upErr := compose(ctx, repoRoot, "up", "-d", "--wait").CombinedOutput()
	if err := composeUpErrorWithLogs(upErr, out, composeLogFetcher(ctx, repoRoot), coreServices); err != nil {
		t.Fatalf("compose up: %v", err)
	}
	t.Cleanup(func() {
		downCtx, c := context.WithTimeout(context.Background(), 60*time.Second)
		defer c()
		_ = compose(downCtx, repoRoot, "down", "-v").Run()
	})
	assertServicesHealthy(ctx, t, repoRoot, []string{"regatta", "alertmanager"})

	base := "http://127.0.0.1:9093"
	stamp := fmt.Sprintf("rt-%d", time.Now().UnixNano())
	alert := []map[string]any{{
		"labels": map[string]string{
			"alertname": "RegattaRoundTripSynthetic",
			"severity":  "info",
			"stamp":     stamp,
		},
		"annotations": map[string]string{
			"summary": "synthetic round-trip probe (INT-1)",
		},
		"startsAt": time.Now().UTC().Format(time.RFC3339),
	}}
	body, err := json.Marshal(alert)
	if err != nil {
		t.Fatalf("marshal alert: %v", err)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/v2/alerts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	httpClient := &http.Client{Timeout: 10 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/v2/alerts: %v", err)
	}
	respBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("POST /api/v2/alerts status=%d body=%s", resp.StatusCode, respBody)
	}

	// Alertmanager coalesces new alerts into the active-alerts set on the
	// next dispatch tick (default group_interval); poll rather than assume
	// synchronous visibility.
	deadline := time.Now().Add(30 * time.Second)
	var lastBody []byte
	for {
		getReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v2/alerts", nil)
		getResp, err := httpClient.Do(getReq)
		if err == nil {
			lastBody, _ = io.ReadAll(getResp.Body)
			getResp.Body.Close()
			if getResp.StatusCode == http.StatusOK && strings.Contains(string(lastBody), stamp) {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("posted alert stamp=%s not visible in GET /api/v2/alerts within 30s; last body=%s", stamp, lastBody)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("ctx canceled polling alertmanager: %v", ctx.Err())
		case <-time.After(2 * time.Second):
		}
	}
}

// TestComposePrometheusOTLPSample asserts a regatta OTLP metric series appears in Prometheus after boot, proving the OTLP push path (#519) is live (R-MEGA-3 INT-1 #1363).
func TestComposePrometheusOTLPSample(t *testing.T) {
	requireDocker(t)

	repoRoot := repoRootFromCWD(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	out, upErr := compose(ctx, repoRoot, "up", "-d", "--wait").CombinedOutput()
	if err := composeUpErrorWithLogs(upErr, out, composeLogFetcher(ctx, repoRoot), coreServices); err != nil {
		t.Fatalf("compose up: %v", err)
	}
	t.Cleanup(func() {
		downCtx, c := context.WithTimeout(context.Background(), 60*time.Second)
		defer c()
		_ = compose(downCtx, repoRoot, "down", "-v").Run()
	})
	assertServicesHealthy(ctx, t, repoRoot, []string{"regatta", "prometheus"})

	// The scheduler ticks every 5s (cmd/regatta/wire_flags.go TickDur
	// default) and emits regatta.scheduler.tick.latency_ms via OTLP.
	// Prometheus's OTLP receiver translates dots to underscores, so the
	// histogram surfaces as regatta_scheduler_tick_latency_ms_count. The
	// SDK's default periodic-export interval is 60s; combined with the
	// 15s Prom scrape interval and pre-wait boot latency, allow 120s.
	base := "http://127.0.0.1:9090"
	query := "regatta_scheduler_tick_latency_ms_count"
	deadline := time.Now().Add(120 * time.Second)
	httpClient := &http.Client{Timeout: 10 * time.Second}
	var lastBody []byte
	for {
		u := base + "/api/v1/query?query=" + query
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		resp, err := httpClient.Do(req)
		if err == nil {
			lastBody, _ = io.ReadAll(resp.Body)
			resp.Body.Close()
			var parsed struct {
				Status string `json:"status"`
				Data   struct {
					ResultType string           `json:"resultType"`
					Result     []map[string]any `json:"result"`
				} `json:"data"`
			}
			if resp.StatusCode == http.StatusOK && json.Unmarshal(lastBody, &parsed) == nil &&
				parsed.Status == "success" && len(parsed.Data.Result) > 0 {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("prometheus query %q returned no result within 120s; last body=%s", query, lastBody)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("ctx canceled polling prometheus: %v", ctx.Err())
		case <-time.After(3 * time.Second):
		}
	}
}
