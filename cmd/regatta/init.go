package main

import (
	"embed"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

//go:embed init_assets/regatta.yaml init_assets/sample.diff
var initAssets embed.FS

// runInit is the CLI entry point. It dispatches to runInitWithIO so
// tests can capture output without touching real stdout/stderr.
func runInit(args []string) int {
	return runInitWithIO(args, os.Stdout, os.Stderr)
}

func runInitWithIO(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	force := fs.Bool("force", false, "overwrite existing regatta.yaml / .regatta/sample.diff")
	jsonOut := fs.Bool("json", false, "emit JSON envelope instead of friendly prose")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	yamlBytes, err := initAssets.ReadFile("init_assets/regatta.yaml")
	if err != nil {
		fmt.Fprintf(stderr, "regatta init: internal: read embedded yaml: %v\n", err)
		return 1
	}
	diffBytes, err := initAssets.ReadFile("init_assets/sample.diff")
	if err != nil {
		fmt.Fprintf(stderr, "regatta init: internal: read embedded sample.diff: %v\n", err)
		return 1
	}

	if err := os.WriteFile("regatta.yaml", yamlBytes, 0o644); err != nil {
		fmt.Fprintf(stderr, "regatta init: write regatta.yaml: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "+ wrote regatta.yaml         (your config; L0 gate enabled)")

	if err := os.MkdirAll(".regatta", 0o755); err != nil {
		fmt.Fprintf(stderr, "regatta init: mkdir .regatta: %v\n", err)
		return 1
	}
	diffPath := filepath.Join(".regatta", "sample.diff")
	if err := os.WriteFile(diffPath, diffBytes, 0o644); err != nil {
		fmt.Fprintf(stderr, "regatta init: write %s: %v\n", diffPath, err)
		return 1
	}
	fmt.Fprintln(stdout, "+ wrote .regatta/sample.diff (a demo attack against MILESTONES.md)")

	_ = force
	_ = jsonOut
	return 0
}
