package l4

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestL4Adapter_SystemPromptMarkedEphemeralCache asserts cache_control=ephemeral on system block + uncached dynamic user content (#852).
func TestL4Adapter_SystemPromptMarkedEphemeralCache(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		_, _ = w.Write([]byte(`{
			"content": [{"type":"text","text":"{\"verdict\":\"pass\",\"findings\":[]}"}],
			"stop_reason": "end_turn",
			"usage": {"input_tokens": 100, "output_tokens": 5}
		}`))
	}))
	t.Cleanup(srv.Close)

	a := &AnthropicAdapter{
		APIKey:  "k",
		BaseURL: srv.URL,
		Version: "2023-06-01",
		Client:  srv.Client(),
	}
	_, err := a.Invoke()(context.Background(), InvokeRequest{
		Model: "m",
		Input: Input{
			PRSHA: "deadbeef", BaseSHA: "base", RepoRoot: "/r",
			Diff: "diff --git a/x b/x\n+y\n", Spec: "spec", Scorecard: "score",
		},
		MaxChars: DefaultMaxDiffChars,
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}

	system, ok := got["system"].([]any)
	if !ok {
		t.Fatalf("payload.system: want []any block array, got %T (%v)", got["system"], got["system"])
	}
	if len(system) == 0 {
		t.Fatalf("payload.system: empty, want >=1 block")
	}
	block, ok := system[0].(map[string]any)
	if !ok {
		t.Fatalf("payload.system[0]: want map, got %T", system[0])
	}
	if block["type"] != "text" {
		t.Errorf("payload.system[0].type: got %v, want text", block["type"])
	}
	staticText, _ := block["text"].(string)
	if !strings.Contains(staticText, "adversarial code reviewer") {
		t.Errorf("payload.system[0].text: missing static reviewer preamble; got %q...", trunc(staticText))
	}
	if !strings.Contains(staticText, "Hunt list") {
		t.Errorf("payload.system[0].text: missing static hunt list section")
	}
	if !strings.Contains(staticText, "Output schema") {
		t.Errorf("payload.system[0].text: missing static output schema section")
	}
	cc, ok := block["cache_control"].(map[string]any)
	if !ok {
		t.Fatalf("payload.system[0].cache_control: want map, got %T (%v)", block["cache_control"], block["cache_control"])
	}
	if cc["type"] != "ephemeral" {
		t.Errorf("payload.system[0].cache_control.type: got %v, want ephemeral", cc["type"])
	}

	// Dynamic per-PR fields must NOT leak into the cached system block —
	// otherwise every PR busts the cache.
	for _, dyn := range []string{"deadbeef", "base", "/r", "diff --git", "+y"} {
		if strings.Contains(staticText, dyn) {
			t.Errorf("payload.system[0].text contains dynamic field %q (cache-busting)", dyn)
		}
	}

	messages, ok := got["messages"].([]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("payload.messages: want 1 message, got %v", got["messages"])
	}
	msg, _ := messages[0].(map[string]any)
	if msg["role"] != "user" {
		t.Errorf("payload.messages[0].role: got %v, want user", msg["role"])
	}
	// Dynamic content (diff + scorecard + spec + PR-SHAs) must reach the
	// model via the user message — uncached on purpose.
	switch content := msg["content"].(type) {
	case string:
		for _, dyn := range []string{"deadbeef", "diff --git"} {
			if !strings.Contains(content, dyn) {
				t.Errorf("payload.messages[0].content missing dynamic field %q", dyn)
			}
		}
		if strings.Contains(content, "Hunt list") {
			t.Errorf("payload.messages[0].content duplicates static Hunt list section (cache-bust risk)")
		}
	case []any:
		joined := ""
		for _, b := range content {
			if m, ok := b.(map[string]any); ok {
				if cc, ok := m["cache_control"]; ok {
					t.Errorf("payload.messages[0].content[*].cache_control set on dynamic block: %v", cc)
				}
				if s, ok := m["text"].(string); ok {
					joined += s
				}
			}
		}
		for _, dyn := range []string{"deadbeef", "diff --git"} {
			if !strings.Contains(joined, dyn) {
				t.Errorf("payload.messages[0].content missing dynamic field %q", dyn)
			}
		}
	default:
		t.Fatalf("payload.messages[0].content: unexpected type %T", content)
	}
}

// TestL4Adapter_CacheTokensSurfaced asserts cache_read + cache_creation tokens land on InvokeResponse for hit-ratio measurement (#852).
func TestL4Adapter_CacheTokensSurfaced(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{
			"content": [{"type":"text","text":"{\"verdict\":\"pass\",\"findings\":[]}"}],
			"stop_reason": "end_turn",
			"usage": {
				"input_tokens": 20,
				"output_tokens": 10,
				"cache_read_input_tokens": 800,
				"cache_creation_input_tokens": 0
			}
		}`))
	}))
	t.Cleanup(srv.Close)

	a := &AnthropicAdapter{APIKey: "k", BaseURL: srv.URL, Client: srv.Client()}
	resp, err := a.Invoke()(context.Background(), InvokeRequest{
		Model:    "m",
		Input:    Input{Diff: "d"},
		MaxChars: DefaultMaxDiffChars,
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if resp.TokensCacheRead != 800 {
		t.Errorf("TokensCacheRead: got %d, want 800", resp.TokensCacheRead)
	}
	if resp.TokensCacheWrite != 0 {
		t.Errorf("TokensCacheWrite: got %d, want 0", resp.TokensCacheWrite)
	}
	if resp.TokensIn != 20 {
		t.Errorf("TokensIn: got %d, want 20", resp.TokensIn)
	}
}

// TestL4Adapter_NoMarker_FallsBackToUncached asserts a hot-reloaded template missing the marker degrades to a single uncached user message (#852).
func TestL4Adapter_NoMarker_FallsBackToUncached(t *testing.T) {
	prev := active.Load()
	t.Cleanup(func() { active.Store(prev) })
	flat, err := parseSlot("flat template no marker {{ .PRSHA }}")
	if err != nil {
		t.Fatalf("parseSlot: %v", err)
	}
	active.Store(flat)

	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"{\"verdict\":\"pass\",\"findings\":[]}"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	t.Cleanup(srv.Close)

	a := &AnthropicAdapter{APIKey: "k", BaseURL: srv.URL, Client: srv.Client()}
	if _, err := a.Invoke()(context.Background(), InvokeRequest{Model: "m", Input: Input{PRSHA: "z"}}); err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if _, hasSystem := got["system"]; hasSystem {
		t.Errorf("payload.system must be absent when template lacks cache marker (fallback path)")
	}
	msgs, _ := got["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("messages: want 1, got %d", len(msgs))
	}
	content, _ := msgs[0].(map[string]any)["content"].(string)
	if !strings.Contains(content, "z") {
		t.Errorf("dynamic content missing PRSHA: %q", content)
	}
}

func trunc(s string) string {
	const n = 120
	if len(s) <= n {
		return s
	}
	return s[:n]
}
