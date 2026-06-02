package l4

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

// Adapter posts Messages API payload and parses findings round-trip.
func TestAnthropicAdapter_RoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Errorf("api key header: got %q, want test-key", got)
		}
		if got := r.Header.Get("anthropic-version"); got == "" {
			t.Errorf("anthropic-version header missing")
		}
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path: got %s, want /v1/messages", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		if req["model"] != "claude-sonnet-4-6" {
			t.Errorf("model: got %v, want claude-sonnet-4-6", req["model"])
		}
		_, _ = w.Write([]byte(`{
			"content": [{"type":"text","text":"{\"verdict\":\"fail\",\"findings\":[{\"id\":\"L4-SEC-X\",\"severity\":\"critical\",\"claim\":\"c\"}]}"}],
			"stop_reason": "end_turn",
			"usage": {"input_tokens": 100, "output_tokens": 50}
		}`))
	}))
	t.Cleanup(srv.Close)

	a := &AnthropicAdapter{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Version: "2023-06-01",
		Client:  srv.Client(),
	}
	resp, err := a.Invoke()(context.Background(), InvokeRequest{
		Model:    "claude-sonnet-4-6",
		Input:    Input{PRSHA: "deadbeef", Diff: "diff body"},
		GateID:   "l4_adversarial",
		MaxChars: DefaultMaxDiffChars,
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if len(resp.Findings) != 1 {
		t.Fatalf("findings: got %d, want 1", len(resp.Findings))
	}
	if resp.Findings[0].Severity != schemas.FindingCritical {
		t.Fatalf("severity: got %s, want critical", resp.Findings[0].Severity)
	}
	if resp.TokensIn != 100 || resp.TokensOut != 50 {
		t.Fatalf("tokens: in=%d out=%d, want 100/50", resp.TokensIn, resp.TokensOut)
	}
	if resp.PromptSHA == "" {
		t.Fatalf("prompt_sha must be stamped")
	}
}

// Non-2xx HTTP surfaces as adapter error (no silent pass).
func TestAnthropicAdapter_HTTPErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"overloaded"}`))
	}))
	t.Cleanup(srv.Close)

	a := &AnthropicAdapter{APIKey: "k", BaseURL: srv.URL, Client: srv.Client()}
	_, err := a.Invoke()(context.Background(), InvokeRequest{Model: "m"})
	if err == nil {
		t.Fatalf("want err on 5xx")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("err should mention status code: %v", err)
	}
}

// Refusal-style response (no JSON in text) returns an adapter error.
func TestAnthropicAdapter_RefusalReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{
			"content": [{"type":"text","text":"I cannot help with that."}],
			"stop_reason": "end_turn",
			"usage": {"input_tokens": 5, "output_tokens": 5}
		}`))
	}))
	t.Cleanup(srv.Close)

	a := &AnthropicAdapter{APIKey: "k", BaseURL: srv.URL, Client: srv.Client()}
	_, err := a.Invoke()(context.Background(), InvokeRequest{Model: "m", Input: Input{Diff: "d"}})
	if err == nil {
		t.Fatalf("want err on refusal")
	}
}

// Empty API key fails at constructor (gate-load surfacing).
func TestNewAnthropicAdapter_EmptyKey(t *testing.T) {
	if _, err := NewAnthropicAdapter(""); err == nil {
		t.Fatalf("want err on empty API key")
	}
	if _, err := NewAnthropicAdapter("k"); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
}

// Transport-layer errors propagate (timeout / DNS / reset uniform).
func TestAnthropicAdapter_TransportErrorPropagates(t *testing.T) {
	a := &AnthropicAdapter{
		APIKey:  "k",
		BaseURL: "http://127.0.0.1:1", // refused
		Client:  &http.Client{Transport: brokenTransport{}},
	}
	_, err := a.Invoke()(context.Background(), InvokeRequest{Model: "m"})
	if err == nil {
		t.Fatalf("want transport err")
	}
}

type brokenTransport struct{}

func (brokenTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("transport: connection refused")
}
