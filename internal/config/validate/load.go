// Package validate loads and CUE-validates regatta.yaml against
// schemas/regatta.v1.cue. Exit point for `regatta validate-config`.
package validate

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	cueerrors "cuelang.org/go/cue/errors"
	"cuelang.org/go/encoding/yaml"

	"github.com/trilamsr/regatta/contracts/schemas"
)

// LoadFile reads path and runs LoadBytes on its contents.
func LoadFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	return LoadBytes(data)
}

// LoadBytes validates YAML bytes against the embedded regatta.v1 CUE
// schema. Returns nil on full concrete validity. On failure the
// returned error enumerates every divergence: CUE's native error
// renders elide siblings as "(and N more errors)"; LoadBytes expands
// them via cue/errors.Details so the caller sees every offending field.
func LoadBytes(data []byte) error {
	if len(data) == 0 {
		return errors.New("regatta.yaml is empty")
	}

	ctx := cuecontext.New()

	schema := ctx.CompileString(schemas.RegattaV1CUE, cue.Filename("regatta.v1.cue"))
	if err := schema.Err(); err != nil {
		return fmt.Errorf("schema compile: %s", cueDetails(err))
	}

	cfgFile, err := yaml.Extract("regatta.yaml", data)
	if err != nil {
		return fmt.Errorf("yaml parse: %s", cueDetails(err))
	}
	cfg := ctx.BuildFile(cfgFile)
	if err := cfg.Err(); err != nil {
		return fmt.Errorf("yaml build: %s", cueDetails(err))
	}

	unified := schema.Unify(cfg)
	if err := unified.Validate(cue.Concrete(true), cue.All()); err != nil {
		return errors.New(cueDetails(err))
	}
	return nil
}

// cueDetails renders a CUE error with every sibling fault enumerated.
// errors.Details prints one fault per line with position info; we trim
// trailing whitespace to keep the message tidy when wrapped in another
// error.
func cueDetails(err error) string {
	return strings.TrimRight(cueerrors.Details(err, nil), "\n")
}

// Prompts mirrors the `prompts` block in regatta.yaml. Only the SHA
// pins land in Go for now; planner_path stays implicit (the
// orchestrator resolves it from cmd-line flags or the embedded
// default). Schema authority is contracts/schemas/regatta.v1.cue
// §Prompts.
type Prompts struct {
	PlannerSHA      string `yaml:"planner_sha,omitempty" json:"planner_sha,omitempty"`
	SecurityGateSHA string `yaml:"security_gate_sha,omitempty" json:"security_gate_sha,omitempty"`
	AgentBriefSHA   string `yaml:"agent_brief_sha,omitempty" json:"agent_brief_sha,omitempty"`
}

// Config is the Go form of a validated regatta.yaml. Only the fields
// callers reach into are surfaced; the rest lives in the YAML and is
// validated by LoadBytes against the CUE schema. New fields go in
// alongside their CUE peer in contracts/schemas/regatta.v1.cue.
type Config struct {
	Prompts *Prompts `yaml:"prompts,omitempty" json:"prompts,omitempty"`
}

// PlannerPromptSHA returns the operator-pinned hex-encoded sha256 of
// the planner system prompt. Empty when unpinned. Nil-safe at every
// level so callers don't have to chain nil guards: a fresh Config{}
// or a nil receiver both yield "" without panicking. Used by
// cmd/regatta to feed program.LoadPlannerPrompt.
func (c *Config) PlannerPromptSHA() string {
	if c == nil || c.Prompts == nil {
		return ""
	}
	return c.Prompts.PlannerSHA
}
