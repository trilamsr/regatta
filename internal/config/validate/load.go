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
