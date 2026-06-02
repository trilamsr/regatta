package l4

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Anthropic Messages API integration. Stdlib HTTP only — no SDK dep,
// same pattern as internal/program/provider_anthropic.go. The SDK was
// considered and rejected: the surface used here (POST /v1/messages
// + text-content response) is ~30 LOC and pulling the SDK would add
// a transitive dep for one call site.
//
// Auth: ANTHROPIC_API_KEY env or explicit field. Caller chooses model
// per spec §3.6 yaml > env > default resolution (handled in config.go).

const (
	// AnthropicDefaultBaseURL is the Messages API base URL.
	AnthropicDefaultBaseURL = "https://api.anthropic.com"

	// AnthropicDefaultVersion pins the API version header.
	// Bump deliberately when Anthropic ships a breaking schema change.
	AnthropicDefaultVersion = "2023-06-01"

	// AnthropicDefaultTimeout caps any single Invoke call. The L4
	// gate sits on the critical path of every PR merge, so a stuck
	// provider must not block the orchestrator indefinitely.
	AnthropicDefaultTimeout = 120 * time.Second

	// anthropicMaxTokens caps response length. Findings are short
	// JSON; 4096 leaves headroom for ~30 findings at ~120 tokens each.
	anthropicMaxTokens = 4096
)

// AnthropicAdapter implements the L4 Invoker against Anthropic's
// Messages API. Zero value is not usable; build via NewAnthropicAdapter
// or set APIKey + BaseURL + Version explicitly.
type AnthropicAdapter struct {
	APIKey  string
	BaseURL string
	Version string
	Timeout time.Duration
	Client  *http.Client
}

// NewAnthropicAdapter reads ANTHROPIC_API_KEY from env when apiKey is
// empty, then sets the spec-defaults for BaseURL + Version + Timeout.
// Empty key after env-fallback returns an error so config-load surfaces
// the misconfiguration rather than the first PR run.
func NewAnthropicAdapter(apiKey string) (*AnthropicAdapter, error) {
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY"))
	}
	if apiKey == "" {
		return nil, errors.New("l4 adapter: ANTHROPIC_API_KEY is empty")
	}
	return &AnthropicAdapter{
		APIKey:  apiKey,
		BaseURL: AnthropicDefaultBaseURL,
		Version: AnthropicDefaultVersion,
		Timeout: AnthropicDefaultTimeout,
	}, nil
}

// Invoke returns the Invoker closure the gate calls. Closure form so
// tests can swap the underlying transport without exporting an
// internal HTTP method.
func (a *AnthropicAdapter) Invoke() Invoker {
	return func(ctx context.Context, req InvokeRequest) (InvokeResponse, error) {
		return a.do(ctx, req)
	}
}

func (a *AnthropicAdapter) do(ctx context.Context, req InvokeRequest) (InvokeResponse, error) {
	prompt, sha, err := RenderPrompt(req.Input, req.MaxChars)
	if err != nil {
		return InvokeResponse{}, err
	}

	payload, err := json.Marshal(buildAnthropicPayload(req.Model, prompt))
	if err != nil {
		return InvokeResponse{}, fmt.Errorf("l4 adapter: marshal payload: %w", err)
	}

	baseURL := a.BaseURL
	if baseURL == "" {
		baseURL = AnthropicDefaultBaseURL
	}
	version := a.Version
	if version == "" {
		version = AnthropicDefaultVersion
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		baseURL+"/v1/messages", bytes.NewReader(payload))
	if err != nil {
		return InvokeResponse{}, fmt.Errorf("l4 adapter: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", a.APIKey)
	httpReq.Header.Set("anthropic-version", version)

	client := a.Client
	if client == nil {
		timeout := a.Timeout
		if timeout <= 0 {
			timeout = AnthropicDefaultTimeout
		}
		client = &http.Client{Timeout: timeout}
	}

	httpResp, err := client.Do(httpReq)
	if err != nil {
		return InvokeResponse{}, fmt.Errorf("l4 adapter: http: %w", err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	raw, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return InvokeResponse{}, fmt.Errorf("l4 adapter: read body: %w", err)
	}
	if httpResp.StatusCode != http.StatusOK {
		return InvokeResponse{}, fmt.Errorf("l4 adapter: status %d: %s",
			httpResp.StatusCode, snippet(raw, 500))
	}

	textBody, inTok, outTok, err := extractAnthropicText(raw)
	if err != nil {
		return InvokeResponse{}, err
	}
	env, err := ParseEnvelope([]byte(textBody))
	if err != nil {
		return InvokeResponse{}, fmt.Errorf("l4 adapter: %w", err)
	}
	return InvokeResponse{
		Findings:  env.Findings,
		PromptSHA: sha,
		TokensIn:  inTok,
		TokensOut: outTok,
	}, nil
}

// buildAnthropicPayload returns the Messages API request shape. The
// prompt is sent as one user message; the JSON-output contract is
// enforced via the prompt template's "## Output schema" section.
func buildAnthropicPayload(model, prompt string) map[string]any {
	return map[string]any{
		"model":       model,
		"max_tokens":  anthropicMaxTokens,
		"temperature": 0.2,
		"messages": []map[string]any{
			{"role": "user", "content": prompt},
		},
	}
}

// extractAnthropicText pulls the first text-content block out of an
// Anthropic Messages response + the token usage counts. The L4 prompt
// asks for one JSON envelope as the entire response so multi-block
// responses are degenerate; we still scan all blocks and concatenate
// to stay robust against future model behaviour drift.
func extractAnthropicText(raw []byte) (string, int64, int64, error) {
	var msg struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
		Usage      struct {
			InputTokens  int64 `json:"input_tokens"`
			OutputTokens int64 `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &msg); err != nil {
		return "", 0, 0, fmt.Errorf("l4 adapter: parse response: %w", err)
	}
	var sb strings.Builder
	for _, c := range msg.Content {
		if c.Type == "text" {
			sb.WriteString(c.Text)
		}
	}
	if sb.Len() == 0 {
		return "", msg.Usage.InputTokens, msg.Usage.OutputTokens,
			fmt.Errorf("l4 adapter: empty text content (stop_reason=%s)", msg.StopReason)
	}
	return sb.String(), msg.Usage.InputTokens, msg.Usage.OutputTokens, nil
}

func snippet(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}
