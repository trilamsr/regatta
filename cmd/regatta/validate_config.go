// validate-config: CUE-validates a regatta.yaml; a thin operator-side
// shim around internal/config/validate.
package main

import (
	"flag"
	"fmt"
	"os"

	validateconfig "github.com/trilamsr/regatta/internal/config/validate"
)

func runValidateConfig(args []string) int {
	fs := flag.NewFlagSet(subcmdValidateConfig, flag.ExitOnError)
	cfgPath := fs.String("config", "regatta.yaml", "Path to regatta.yaml")
	_ = fs.Parse(args)

	if err := validateconfig.LoadFile(*cfgPath); err != nil {
		fmt.Fprintf(os.Stderr, "validate-config: FAIL\n%s\n", err)
		return 1
	}
	fmt.Printf("validate-config: PASS - %s validates against regatta.v1\n", *cfgPath)
	return 0
}
