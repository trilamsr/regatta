// Package validatespec validates emitted WorkItem JSON documents against
// schemas/work_item.schema.json.
//
// The validator is used by operators before feeding a SpecAdapter's output
// into the orchestrator, and by adapter implementors to certify wire-format
// compliance. It is intentionally schema-only: dependency cycles, ID
// uniqueness across a batch, and lane semantics are orchestrator concerns,
// not WorkItem-shape concerns.
package validatespec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"

	"github.com/trilamsr/regatta/schemas"
)

// Result is the outcome of validating one document. A document may be a
// single WorkItem object or a JSON array of WorkItems; ItemResults is
// non-empty either way (one entry for the singleton, one per array element).
type Result struct {
	OK          bool         `json:"ok"`
	ItemResults []ItemResult `json:"item_results"`
}

// ItemResult reports validation outcome for one WorkItem.
type ItemResult struct {
	Index    int      `json:"index"`           // zero for singleton input; array index otherwise
	ID       string   `json:"id,omitempty"`    // WorkItem.id if extractable, else empty
	OK       bool     `json:"ok"`
	Failures []string `json:"failures,omitempty"`
}

var compiledSchema = mustCompileSchema()

func mustCompileSchema() *jsonschema.Schema {
	c := jsonschema.NewCompiler()
	c.Draft = jsonschema.Draft2020
	if err := c.AddResource("work_item.schema.json", strings.NewReader(schemas.WorkItemSchemaJSON)); err != nil {
		panic(fmt.Errorf("validatespec: add resource: %w", err))
	}
	sch, err := c.Compile("work_item.schema.json")
	if err != nil {
		panic(fmt.Errorf("validatespec: compile embedded WorkItem schema: %w", err))
	}
	return sch
}

// Validate reads input from r and delegates to ValidateBytes. Convenience
// wrapper for stream consumers (HTTP bodies, stdin, adapter pipes).
func Validate(r io.Reader) (Result, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return Result{}, fmt.Errorf("validatespec: read input: %w", err)
	}
	return ValidateBytes(b)
}

// ValidateBytes parses input as JSON and validates against the WorkItem
// schema. Input may be a single object or an array of objects.
func ValidateBytes(input []byte) (Result, error) {
	trimmed := bytes.TrimSpace(input)
	if len(trimmed) == 0 {
		return Result{}, fmt.Errorf("validatespec: empty input")
	}

	var raw any
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return Result{}, fmt.Errorf("validatespec: parse JSON: %w", err)
	}

	switch v := raw.(type) {
	case map[string]any:
		return Result{ItemResults: []ItemResult{validateOne(0, v)}}.finalize(), nil
	case []any:
		items := make([]ItemResult, 0, len(v))
		for i, elem := range v {
			obj, ok := elem.(map[string]any)
			if !ok {
				items = append(items, ItemResult{
					Index:    i,
					OK:       false,
					Failures: []string{fmt.Sprintf("array element %d is not a JSON object", i)},
				})
				continue
			}
			items = append(items, validateOne(i, obj))
		}
		return Result{ItemResults: items}.finalize(), nil
	default:
		return Result{}, fmt.Errorf("validatespec: input must be a JSON object or array, got %T", raw)
	}
}

func validateOne(idx int, obj map[string]any) ItemResult {
	out := ItemResult{Index: idx, OK: true}
	if id, ok := obj["id"].(string); ok {
		out.ID = id
	}
	if err := compiledSchema.Validate(obj); err != nil {
		out.OK = false
		out.Failures = flattenValidationErrors(err)
	}
	return out
}

func flattenValidationErrors(err error) []string {
	ve, ok := err.(*jsonschema.ValidationError)
	if !ok {
		return []string{err.Error()}
	}
	var out []string
	var walk func(*jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		// Leaf errors (no nested Causes) carry the actionable message.
		if len(e.Causes) == 0 {
			loc := e.InstanceLocation
			if loc == "" {
				loc = "(root)"
			}
			out = append(out, fmt.Sprintf("%s: %s", loc, e.Message))
			return
		}
		for _, c := range e.Causes {
			walk(c)
		}
	}
	walk(ve)
	if len(out) == 0 {
		// Schema rejected without leaf causes; surface the top message.
		out = append(out, ve.Message)
	}
	return out
}

func (r Result) finalize() Result {
	r.OK = true
	for _, it := range r.ItemResults {
		if !it.OK {
			r.OK = false
			break
		}
	}
	return r
}
