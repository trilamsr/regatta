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
	fs := flag.NewFlagSet(subcmdValidateConfig, flag.ContinueOnError)
	cfgPath := fs.String("config", "regatta.yaml", "Path to regatta.yaml")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "validate-config: unexpected positional arg(s) %v — use --config <path>\n", fs.Args())
		return 2
	}

	if err := validateconfig.LoadFile(*cfgPath); err != nil {
		fmt.Fprintf(os.Stderr, "validate-config: FAIL\n%s\n", err)
		fmt.Fprintln(os.Stderr, "\nHint: run `regatta init` to scaffold a minimal valid regatta.yaml then edit from there.")
		return 1
	}
	fmt.Printf("validate-config: PASS - %s validates against regatta.v1\n", *cfgPath)
	return 0
}
