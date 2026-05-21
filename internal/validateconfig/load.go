// Package validateconfig loads and CUE-validates regatta.yaml against
// schemas/regatta.v1.cue. Exit point for `regatta validate-config`.
package validateconfig

import (
	"fmt"
	"os"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/encoding/yaml"

	"github.com/trilamsr/regatta/schemas"
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
// schema. Returns nil on full concrete validity; otherwise an error
// describing the first divergence (CUE surfaces multi-error chains by
// default).
func LoadBytes(data []byte) error {
	ctx := cuecontext.New()

	schema := ctx.CompileString(schemas.RegattaV1CUE, cue.Filename("regatta.v1.cue"))
	if err := schema.Err(); err != nil {
		return fmt.Errorf("schema compile: %w", err)
	}

	cfgFile, err := yaml.Extract("regatta.yaml", data)
	if err != nil {
		return fmt.Errorf("yaml parse: %w", err)
	}
	cfg := ctx.BuildFile(cfgFile)
	if err := cfg.Err(); err != nil {
		return fmt.Errorf("yaml build: %w", err)
	}

	unified := schema.Unify(cfg)
	if err := unified.Err(); err != nil {
		return err
	}
	if err := unified.Validate(cue.Concrete(true), cue.All()); err != nil {
		return err
	}
	return nil
}
