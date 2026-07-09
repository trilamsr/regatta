// Anthropic-direct ModelClient implementation for the planner.
//
// This is the ONE provider adapter shipped at MVP-1 to validate
// the planner pipeline. The provider-abstraction layer
// (docs/design.md §Alternatives (e)) is deferred until a second
// paying customer requests a non-Claude provider; until then, the
// ModelClient interface is the contract and this is its single
// implementation.
//
// Scope: stdlib HTTP only -- no SDK dep. The Anthropic API surface
// used here is minimal (Messages API + tool_use + JSON Schema
// input_schema) so the cost of staying SDK-free is low.

package program

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/secrets"
	"github.com/trilamsr/regatta/internal/strutil"
)

// AnthropicPlanner is a ModelClient that calls Anthropic's
// Messages API with a tool_use schema corresponding to ProgramBrief.
//
// Deadline is caller-owned: Plan uses req.WithContext(ctx); callers
// wanting a backstop wrap ctx with context.WithTimeout.
type AnthropicPlanner struct {
	APIKey     string
	Model      string // e.g. "claude-opus-4-7"
	BaseURL    string // defaults to https://api.anthropic.com
	Version    string // anthropic-version header; defaults to "2023-06-01"
	Prompt     string // system prompt; defaults to defaultPlannerPrompt
	HTTPClient *http.Client
	// Timeout is retained for backward-compatible struct literals but
	// is no longer honored: Plan reads ctx.Deadline() only. Remove
	// callers assigning this and drop the field on the next major.
	Timeout time.Duration
	// RetryBase is the exponential-backoff floor between 429/503 retries.
	// Defaults to 1s in NewAnthropicPlanner; tests inject microseconds
	// to keep the suite deterministic without time.Sleep drift.
	RetryBase time.Duration
}

// maxAnthropicRetries caps retry attempts against Anthropic 429/503
// responses. Matches the 5-attempt precedent set by internal/cost/reconcile.
const maxAnthropicRetries = 5

// NewAnthropicPlanner resolves ANTHROPIC_API_KEY via the secrets
// Fetcher chain (keychain → legacy env → canonical env) and returns a
// configured client. The model id is required so callers explicitly
// choose Opus vs Sonnet vs Haiku (price/quality is operator-visible,
// never hidden).
func NewAnthropicPlanner(model string) (*AnthropicPlanner, error) {
	ctx := context.Background()
	var key string
	if v, err := secrets.Default(ctx).Get(ctx, secrets.KeyAnthropic); err == nil {
		key = strings.TrimSpace(string(v.Bytes()))
	}
	if key == "" {
		return nil, errors.New("ANTHROPIC_API_KEY is empty")
	}
	if model == "" {
		return nil, errors.New("model is required (e.g. claude-opus-4-7)")
	}
	return &AnthropicPlanner{
		APIKey:    key,
		Model:     model,
		BaseURL:   "https://api.anthropic.com",
		Version:   "2023-06-01",
		Prompt:    defaultPlannerPrompt,
		RetryBase: time.Second,
	}, nil
}

// ModelID implements ModelClient. Provider-qualified id stamped into
// ProgramBrief.PlannerModelID for audit.
func (a *AnthropicPlanner) ModelID() string {
	return "anthropic:" + a.Model
}

// Plan invokes the Anthropic Messages API with a tool_use schema
// mirroring ProgramBrief. The model is required to call the tool
// (`tool_choice: {type: "tool", name: "emit_feature_plan"}`),
// which gives us server-enforced JSON Schema output. 429 (rate limit)
// and 503 (overload) are documented Anthropic transient responses —
// retry up to maxAnthropicRetries times with exponential backoff,
// honouring Retry-After when present. Other 4xx fail immediately.
//
// Deadline is caller-owned via req.WithContext(ctx). A client-level
// Timeout would race the caller's ctx and silently pre-empt a longer
// or absent deadline; callers wanting a backstop wrap ctx with
// context.WithTimeout themselves.
func (a *AnthropicPlanner) Plan(ctx context.Context, parent schemas.WorkItem) (*ProgramBrief, error) {
	body, err := a.buildRequest(parent)
	if err != nil {
		return nil, err
	}
	client := a.HTTPClient
	if client == nil {
		client = &http.Client{}
	}
	base := a.RetryBase
	if base <= 0 {
		base = time.Second
	}

	var lastStatus int
	var lastBody []byte
	for attempt := 0; attempt < maxAnthropicRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			a.BaseURL+"/v1/messages", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", a.APIKey)
		req.Header.Set("anthropic-version", a.Version)

		status, raw, retryAfter, err := a.doOnce(client, req)
		if err != nil {
			return nil, fmt.Errorf("anthropic: http: %w", err)
		}
		if status == http.StatusOK {
			return a.parseResponse(raw)
		}
		lastStatus, lastBody = status, raw
		if status != http.StatusTooManyRequests && status != http.StatusServiceUnavailable {
			// Non-retryable 4xx/5xx — surface immediately.
			return nil, fmt.Errorf("anthropic: status %d: %s", status, strutil.Truncate(string(raw), 500))
		}
		if attempt == maxAnthropicRetries-1 {
			break
		}
		delay := backoffDelay(base, attempt)
		if retryAfter > delay {
			delay = retryAfter
		}
		if err := sleepCtx(ctx, delay); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("anthropic: status %d after %d retries: %s",
		lastStatus, maxAnthropicRetries, strutil.Truncate(string(lastBody), 500))
}

// doOnce performs a single HTTP round-trip and returns status, body,
// parsed Retry-After (zero if absent/malformed), and any transport
// error. Body is fully drained so the connection can be reused.
func (a *AnthropicPlanner) doOnce(client *http.Client, req *http.Request) (int, []byte, time.Duration, error) {
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, 0, fmt.Errorf("read body: %w", err)
	}
	return resp.StatusCode, raw, parseRetryAfter(resp.Header.Get("Retry-After")), nil
}

// parseRetryAfter accepts the Anthropic-documented seconds form. The
// HTTP-date form is rare from Anthropic and adds parser surface for
// no measured benefit; treat unparseable values as zero and fall
// through to plain exponential backoff.
func parseRetryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	return 0
}

// backoffDelay returns base<<attempt, capped so overflow cannot produce
// a negative duration.
func backoffDelay(base time.Duration, attempt int) time.Duration {
	const maxShift = 20 // base<<20 dominates any sane per-request wait
	if attempt > maxShift {
		attempt = maxShift
	}
	d := base << attempt
	if d <= 0 {
		return base
	}
	return d
}

// sleepCtx blocks for d or until ctx cancels, whichever fires first.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// buildRequest constructs the Messages API payload.
func (a *AnthropicPlanner) buildRequest(parent schemas.WorkItem) ([]byte, error) {
	type tool struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		InputSchema any    `json:"input_schema"`
	}
	type msg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}

	criteriaTxt := strings.Builder{}
	for _, c := range parent.AcceptanceCriteria {
		criteriaTxt.WriteString("- [")
		criteriaTxt.WriteString(c.ID)
		criteriaTxt.WriteString("] ")
		criteriaTxt.WriteString(c.Text)
		criteriaTxt.WriteByte('\n')
	}
	userMsg := fmt.Sprintf(`Parent WorkItem ID: %s
Title: %s

Body:
%s

Acceptance criteria (IMMUTABLE -- copy IDs verbatim into features.fulfills):
%s

Linked artifact: %s
`, parent.ID, parent.Title, parent.Body, criteriaTxt.String(), parent.LinkedArtifact)

	payload := map[string]any{
		"model":       a.Model,
		"max_tokens":  8192,
		"system":      a.Prompt,
		"temperature": 0.2,
		"tools": []tool{
			{
				Name:        "emit_feature_plan",
				Description: "Emit a ProgramBrief decomposing the parent WorkItem into child features. Every parent criterion id MUST appear in exactly one feature.fulfills (no overlap, no gaps).",
				InputSchema: featurePlanToolSchema(),
			},
		},
		"tool_choice": map[string]any{
			"type": "tool",
			"name": "emit_feature_plan",
		},
		"messages": []msg{{Role: "user", Content: userMsg}},
	}
	return json.Marshal(payload)
}

// featurePlanToolSchema is the JSON Schema the model fills in. Kept
// in sync with schemas/features.schema.json -- but trimmed to the
// fields the model is responsible for (program_id / signature /
// parent_criteria / planner_model_id / produced_at are stamped by
// the planner pipeline, not the model).
func featurePlanToolSchema() any {
	return map[string]any{
		"type":                 "object",
		"required":             []string{"features"},
		"additionalProperties": false,
		"properties": map[string]any{
			"features": map[string]any{
				"type":     "array",
				"minItems": 1,
				"items": map[string]any{
					"type":                 "object",
					"required":             []string{"id", "title", "fulfills", "depends_on_features"},
					"additionalProperties": false,
					"properties": map[string]any{
						"id": map[string]any{
							"type":    "string",
							"pattern": "^F-[A-Z0-9_-]{1,32}$",
						},
						"title":       map[string]any{"type": "string", "minLength": 1},
						"description": map[string]any{"type": "string"},
						"fulfills": map[string]any{
							"type":        "array",
							"minItems":    1,
							"uniqueItems": true,
							"items":       map[string]any{"type": "string", "minLength": 1},
						},
						"depends_on_features": map[string]any{
							"type":        "array",
							"uniqueItems": true,
							"items":       map[string]any{"type": "string", "pattern": "^F-[A-Z0-9_-]{1,32}$"},
						},
						"estimated_complexity": map[string]any{
							"enum": []string{"trivial", "small", "medium", "large"},
						},
					},
				},
			},
		},
	}
}

// parseResponse pulls the tool_use block out of an Anthropic
// Messages response and unmarshals it into a partial ProgramBrief.
// The planner pipeline fills in the rest.
func (a *AnthropicPlanner) parseResponse(raw []byte) (*ProgramBrief, error) {
	var msg struct {
		Content []struct {
			Type  string          `json:"type"`
			Name  string          `json:"name,omitempty"`
			Input json.RawMessage `json:"input,omitempty"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
	}
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil, fmt.Errorf("anthropic: parse response: %w", err)
	}
	for _, c := range msg.Content {
		if c.Type != "tool_use" || c.Name != "emit_feature_plan" {
			continue
		}
		var partial struct {
			Features []PlannedFeature `json:"features"`
		}
		if err := json.Unmarshal(c.Input, &partial); err != nil {
			return nil, fmt.Errorf("anthropic: tool_use input invalid: %w", err)
		}
		return &ProgramBrief{Features: partial.Features}, nil
	}
	return nil, fmt.Errorf("anthropic: no tool_use block in response (stop_reason=%s)", msg.StopReason)
}

// defaultPlannerPrompt is the system prompt for one-shot program
// decomposition. SHA-pin in prompts/planner.md is the eventual
// home; embedded here for v1 to keep the build hermetic.
//
// The prompt mandates:
//   - Coverage invariant (every criterion claimed exactly once).
//   - DAG of features (depends_on_features).
//   - No re-planning; one-shot only.
//   - Verbatim criterion IDs (mutation is L0's catch but the prompt
//     explicitly forbids it as the first line of defense).
const defaultPlannerPrompt = `You are Regatta's program planner.

Your one job is to decompose a parent WorkItem into a DAG of child
features that, together, fully cover the parent's acceptance
criteria.

CRITICAL RULES (violations are rejected by the orchestrator):

1. Coverage invariant. Every acceptance-criterion ID in the parent
   MUST appear in exactly one feature.fulfills entry. No gaps. No
   overlaps. Criterion IDs MUST be copied verbatim -- do not edit,
   normalize, paraphrase, or "fix" them.

2. DAG. depends_on_features must be acyclic. If feature B reads
   state that feature A writes, B depends on A.

3. Atomicity. Each feature is independently mergeable. If two
   features must land together, they are one feature.

4. Naming. Feature IDs match ^F-[A-Z0-9_-]{1,32}$.

5. You run exactly once per program. There is no "v2" -- if you are
   uncertain, prefer fewer, broader features over many speculative
   ones. The orchestrator will inject fix-features for issues it
   discovers; you do not pre-empt them.

You MUST call the emit_feature_plan tool with your output. Do not
emit free-form text. Do not negotiate. Decompose and emit.`
