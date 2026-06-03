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
	"os"
	"strings"
	"time"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/strutil"
)

// AnthropicPlanner is a ModelClient that calls Anthropic's
// Messages API with a tool_use schema corresponding to ProgramBrief.
type AnthropicPlanner struct {
	APIKey   string
	Model    string        // e.g. "claude-opus-4-7"
	BaseURL  string        // defaults to https://api.anthropic.com
	Version  string        // anthropic-version header; defaults to "2023-06-01"
	Timeout  time.Duration // defaults to 120s
	Prompt   string        // system prompt; defaults to defaultPlannerPrompt
	HTTPClient *http.Client
}

// NewAnthropicPlanner reads ANTHROPIC_API_KEY from the environment
// and returns a configured client. The model id is required so
// callers explicitly choose Opus vs Sonnet vs Haiku (price/quality
// is operator-visible, never hidden).
func NewAnthropicPlanner(model string) (*AnthropicPlanner, error) {
	key := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY"))
	if key == "" {
		return nil, errors.New("ANTHROPIC_API_KEY is empty")
	}
	if model == "" {
		return nil, errors.New("model is required (e.g. claude-opus-4-7)")
	}
	return &AnthropicPlanner{
		APIKey:  key,
		Model:   model,
		BaseURL: "https://api.anthropic.com",
		Version: "2023-06-01",
		Timeout: 120 * time.Second,
		Prompt:  defaultPlannerPrompt,
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
// which gives us server-enforced JSON Schema output.
func (a *AnthropicPlanner) Plan(ctx context.Context, parent schemas.WorkItem) (*ProgramBrief, error) {
	body, err := a.buildRequest(parent)
	if err != nil {
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

	client := a.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: a.Timeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("anthropic: http: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("anthropic: read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic: status %d: %s", resp.StatusCode, strutil.Truncate(string(raw), 500))
	}
	return a.parseResponse(raw)
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
