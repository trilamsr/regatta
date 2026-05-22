package missions

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/trilamsr/regatta/schemas"
)

// stubAnthropicServer returns a fake Anthropic Messages API.
func stubAnthropicServer(t *testing.T, respJSON string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") == "" {
			http.Error(w, "missing x-api-key", http.StatusUnauthorized)
			return
		}
		if r.Header.Get("anthropic-version") == "" {
			http.Error(w, "missing anthropic-version", http.StatusBadRequest)
			return
		}
		// Quick sanity: request body must be JSON and contain our tool name.
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "emit_feature_plan") {
			http.Error(w, "missing tool", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(respJSON))
	}))
}

func TestAnthropicPlanner_HappyPath(t *testing.T) {
	respBody := `{
	  "stop_reason": "tool_use",
	  "content": [
	    {"type": "text", "text": "I'll decompose this."},
	    {"type": "tool_use", "name": "emit_feature_plan",
	     "input": {"features": [
	       {"id": "F-A", "title": "do A", "fulfills": ["AC-1"], "depends_on_features": []},
	       {"id": "F-B", "title": "do B", "fulfills": ["AC-2"], "depends_on_features": ["F-A"]}
	     ]}}
	  ]
	}`
	server := stubAnthropicServer(t, respBody)
	defer server.Close()

	a := &AnthropicPlanner{
		APIKey:  "test-key",
		Model:   "claude-opus-4-7",
		BaseURL: server.URL,
		Version: "2023-06-01",
		Prompt:  "test",
	}
	got, err := a.Plan(context.Background(), schemas.WorkItem{
		ID: "RFC-1",
		AcceptanceCriteria: []schemas.Criterion{
			{ID: "AC-1", Text: "do A"},
			{ID: "AC-2", Text: "do B"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(got.Features) != 2 || got.Features[0].ID != "F-A" {
		t.Fatalf("bad plan: %+v", got.Features)
	}
}

func TestAnthropicPlanner_NoToolUse(t *testing.T) {
	respBody := `{"stop_reason": "end_turn", "content": [{"type": "text", "text": "I refuse to plan"}]}`
	server := stubAnthropicServer(t, respBody)
	defer server.Close()

	a := &AnthropicPlanner{APIKey: "x", Model: "x", BaseURL: server.URL, Version: "v"}
	_, err := a.Plan(context.Background(), schemas.WorkItem{})
	if err == nil {
		t.Fatal("expected error when model returned no tool_use")
	}
	if !strings.Contains(err.Error(), "no tool_use") {
		t.Fatalf("error not informative: %v", err)
	}
}

func TestAnthropicPlanner_UpstreamFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"type":"error","error":{"message":"overloaded"}}`, http.StatusServiceUnavailable)
	}))
	defer server.Close()

	a := &AnthropicPlanner{APIKey: "x", Model: "x", BaseURL: server.URL, Version: "v"}
	_, err := a.Plan(context.Background(), schemas.WorkItem{})
	if err == nil {
		t.Fatal("expected error on 503")
	}
}

func TestAnthropicPlanner_ModelIDStamped(t *testing.T) {
	a := &AnthropicPlanner{Model: "claude-sonnet-4-6"}
	if got, want := a.ModelID(), "anthropic:claude-sonnet-4-6"; got != want {
		t.Fatalf("ModelID=%q, want %q", got, want)
	}
}

func TestAnthropicPlanner_NewRequiresEnv(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	if _, err := NewAnthropicPlanner("claude-opus-4-7"); err == nil {
		t.Fatal("expected error on empty env")
	}
	t.Setenv("ANTHROPIC_API_KEY", "k")
	if _, err := NewAnthropicPlanner(""); err == nil {
		t.Fatal("expected error on empty model")
	}
	t.Setenv("ANTHROPIC_API_KEY", "k")
	a, err := NewAnthropicPlanner("claude-opus-4-7")
	if err != nil {
		t.Fatal(err)
	}
	if a.Prompt == "" || a.Timeout == 0 {
		t.Fatal("defaults not applied")
	}
}

// End-to-end: stub Anthropic + planner pipeline + signature verify.
func TestPlannerWithAnthropicAdapter_EndToEnd(t *testing.T) {
	respBody := `{
	  "stop_reason": "tool_use",
	  "content": [
	    {"type": "tool_use", "name": "emit_feature_plan",
	     "input": {"features": [
	       {"id": "F-A", "title": "do A", "fulfills": ["AC-1"], "depends_on_features": []},
	       {"id": "F-B", "title": "do B", "fulfills": ["AC-2"], "depends_on_features": ["F-A"]}
	     ]}}
	  ]
	}`
	server := stubAnthropicServer(t, respBody)
	defer server.Close()

	client := &AnthropicPlanner{
		APIKey: "x", Model: "claude-opus-4-7", BaseURL: server.URL, Version: "v", Prompt: "test",
	}
	key := []byte("regatta-e2e-test-key-32-bytes-pad")
	plan, err := Run(context.Background(), PlannerOptions{
		Client:    client,
		HMACKey:   key,
		HMACKeyID: "k1",
	}, schemas.WorkItem{
		ID: "RFC-AUTH",
		AcceptanceCriteria: []schemas.Criterion{
			{ID: "AC-1", Text: "do A"},
			{ID: "AC-2", Text: "do B"},
		},
	})
	if err != nil {
		t.Fatalf("end-to-end run: %v", err)
	}
	if plan.MissionID == "" || plan.PlannerModelID != "anthropic:claude-opus-4-7" {
		t.Fatalf("planner-fixed fields missing: %+v", plan)
	}
	// Verify signature with the same key.
	if err := plan.VerifySignature(map[string][]byte{"k1": key}); err != nil {
		t.Fatalf("verify: %v", err)
	}

	// And re-marshal the plan to JSON to confirm the wire shape is
	// what features.schema.json expects (no unexpected fields).
	raw, _ := json.Marshal(plan)
	if !strings.Contains(string(raw), `"parent_criteria"`) {
		t.Fatal("parent_criteria not stamped into wire form")
	}
}
