// program subcommand tree (plan, verify-handoff) plus the parent
// WorkItem loader and atomic brief-writer that only the plan path
// needs.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/orchestrator"
	"github.com/trilamsr/regatta/internal/orchestrator/adapter"
	"github.com/trilamsr/regatta/internal/program"
)

// runProgram dispatches the `program ...` subcommand tree.
func runProgram(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "regatta program: expected sub-subcommand (verify-handoff)")
		return 2
	}
	switch args[0] {
	case "plan":
		return runProgramPlan(args[1:])
	case "verify-handoff":
		return runProgramVerifyHandoff(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "regatta program: unknown subcommand %q\n", args[0])
		return 2
	}
}

// runProgramPlan: read a parent WorkItem from disk (.md or .json),
// invoke the selected planner (anthropic|stub), validate + sign the
// resulting ProgramBrief, then either write atomically to
// <write-dir>/<program_id>.json (when -write) or emit pretty JSON to
// stdout. Source adapters are deferred -- for v1, operators
// hand-author the parent WorkItem or feed it from an adapter dump.
func runProgramPlan(args []string) int {
	fs := flag.NewFlagSet("program plan", flag.ExitOnError)
	model := fs.String("model", "claude-opus-4-7", "Claude model id (anthropic planner only)")
	keyEnv := fs.String("hmac-key-env", "", "Env var holding HMAC key (required)")
	keyID := fs.String("hmac-key-id", "k1", "key_id to stamp into signature")
	plannerName := fs.String("planner", "anthropic",
		"planner implementation: 'anthropic' (default) or 'stub' (offline deterministic, for tests)")
	writeFlag := fs.Bool("write", false,
		"write signed brief to <write-dir>/<program_id>.json atomically; default stdout")
	writeDir := fs.String("write-dir", "",
		"directory to write brief into when -write is set; defaults to ./.regatta/programs")
	force := fs.Bool("force", false,
		"overwrite existing brief at the target path")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(fs.Output(), "Usage: regatta program plan <work-item.{md,json}>")
		fs.PrintDefaults()
	}
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}
	if *keyEnv == "" {
		fmt.Fprintln(os.Stderr, "regatta program plan: -hmac-key-env is required")
		return 2
	}
	key := os.Getenv(*keyEnv)
	if key == "" {
		fmt.Fprintf(os.Stderr, "regatta program plan: $%s is empty\n", *keyEnv)
		return 2
	}

	parent, err := loadParentWorkItem(fs.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, "regatta program plan:", err)
		return 2
	}
	if parent.ID == "" || len(parent.AcceptanceCriteria) == 0 {
		fmt.Fprintln(os.Stderr, "regatta program plan: work-item must have id and acceptance_criteria")
		return 2
	}
	if parent.Kind != schemas.KindProgram {
		fmt.Fprintf(os.Stderr, "regatta program plan: work-item kind must be %q, got %q\n", schemas.KindProgram, parent.Kind)
		return 2
	}

	var client program.ModelClient
	switch *plannerName {
	case "", "anthropic":
		c, err := program.NewAnthropicPlanner(*model)
		if err != nil {
			fmt.Fprintln(os.Stderr, "regatta program plan:", err)
			return 2
		}
		client = c
	case "stub":
		client = program.NewStubPlanner()
	default:
		fmt.Fprintf(os.Stderr, "regatta program plan: unknown -planner %q (want anthropic|stub)\n", *plannerName)
		return 2
	}

	plan, err := program.Run(context.Background(), program.PlannerOptions{
		Client:    client,
		HMACKey:   []byte(key),
		HMACKeyID: *keyID,
	}, parent)
	if err != nil {
		fmt.Fprintln(os.Stderr, "regatta program plan:", err)
		return 1
	}

	if *writeFlag {
		target := *writeDir
		if target == "" {
			target = filepath.Join(".regatta", "programs")
		}
		if err := os.MkdirAll(target, 0o750); err != nil {
			fmt.Fprintln(os.Stderr, "regatta program plan:", err)
			return 1
		}
		raw, err := json.MarshalIndent(plan, "", "  ")
		if err != nil {
			fmt.Fprintln(os.Stderr, "regatta program plan: marshal:", err)
			return 1
		}
		briefPath := filepath.Join(target, plan.ProgramID+".json")
		if err := atomicWriteBrief(briefPath, raw, *force); err != nil {
			fmt.Fprintln(os.Stderr, "regatta program plan:", err)
			if errors.Is(err, orchestrator.ErrTargetExists) {
				return 2
			}
			return 1
		}
		return 0
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(plan)
	return 0
}

// loadParentWorkItem reads a parent WorkItem from disk. JSON files are
// unmarshalled directly; .md files route through the markdown_catalog
// parser so operators can hand-author one fixture format for both
// `serve` and one-shot `program plan`.
func loadParentWorkItem(path string) (schemas.WorkItem, error) {
	raw, err := os.ReadFile(path) // #nosec G304 — operator-provided path; cmd is local-only.
	if err != nil {
		return schemas.WorkItem{}, err
	}
	if strings.EqualFold(filepath.Ext(path), ".md") {
		return adapter.ParseMarkdownItem(raw)
	}
	var parent schemas.WorkItem
	if err := json.Unmarshal(raw, &parent); err != nil {
		return schemas.WorkItem{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return parent, nil
}

// atomicWriteBrief writes data to path via temp + os.Rename. When path
// already exists, identical bytes are a no-op (idempotent reruns);
// differing bytes return ErrTargetExists unless force is true. The
// temp file lives in path's directory so the rename is intra-FS
// (atomic on POSIX).
func atomicWriteBrief(path string, data []byte, force bool) error {
	if existing, err := os.ReadFile(path); err == nil { // #nosec G304 — path is the brief target the cmd just computed from operator-supplied -write-dir + program_id; identity check is the whole point of the helper.
		if bytes.Equal(existing, data) {
			return nil
		}
		if !force {
			return fmt.Errorf("%w: %s", orchestrator.ErrTargetExists, path)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat target: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".brief-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename temp into place: %w", err)
	}
	return nil
}

func runProgramVerifyHandoff(args []string) int {
	fs := flag.NewFlagSet("program verify-handoff", flag.ExitOnError)
	keyEnv := fs.String("hmac-key-env", "", "Env var holding the HMAC key (if set, verify signature)")
	keyID := fs.String("hmac-key-id", "k1", "key_id to expect in the signature")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(fs.Output(), "Usage: regatta program verify-handoff <handoff.json>")
		fs.PrintDefaults()
	}
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}

	h, err := program.LoadAndValidate(fs.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, "regatta program verify-handoff:", err)
		return 1
	}

	report := struct {
		ProgramID         string `json:"program_id"`
		FeatureID         string `json:"feature_id"`
		WorkerRunID       string `json:"worker_run_id"`
		SuccessState      string `json:"success_state"`
		SchemaOK          bool   `json:"schema_ok"`
		SignatureVerified bool   `json:"signature_verified"`
		SignatureChecked  bool   `json:"signature_checked"`
	}{
		ProgramID:    h.ProgramID,
		FeatureID:    h.FeatureID,
		WorkerRunID:  h.WorkerRunID,
		SuccessState: h.SuccessState,
		SchemaOK:     true,
	}

	if *keyEnv != "" {
		report.SignatureChecked = true
		key := os.Getenv(*keyEnv)
		if key == "" {
			fmt.Fprintf(os.Stderr, "regatta program verify-handoff: $%s is empty\n", *keyEnv)
			return 2
		}
		keyring := map[string][]byte{*keyID: []byte(key)}
		if err := h.VerifySignature(keyring); err != nil {
			fmt.Fprintln(os.Stderr, "regatta program verify-handoff: signature:", err)
			report.SignatureVerified = false
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			_ = enc.Encode(report)
			return 1
		}
		report.SignatureVerified = true
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(report)
	return 0
}
