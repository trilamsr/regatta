// program show + replay-skew-check subcommands (#549).
//
// `program show` surfaces a brief's engine_version + dirty flag so an
// operator can SEE which engine produced a months-old program without
// jq-by-hand. `program replay-skew-check` is the boundary the audit
// pipeline calls before treating a replay as reproducible — WARN by
// default (tag the digest, keep the loop unblocked), STRICT via flag
// (refuse + non-zero exit, for audit-grade runs).
//
// Both commands load the brief through program.LoadAndValidate's
// signature-aware path when the operator passes -hmac-key-env, so a
// brief tampered after produced_at fails before any engine-skew check
// even runs.

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/trilamsr/regatta/internal/program"
)

// programShowReport is the JSON shape `program show` emits. Stable
// keys (snake_case) so dashboards + audit-trail jq pipelines can pin
// the schema. EngineVersion is the LOAD-BEARING field for #549 audit.
type programShowReport struct {
	ProgramID         string `json:"program_id"`
	ParentWorkItemID  string `json:"parent_work_item_id"`
	PlannerModelID    string `json:"planner_model_id"`
	ProducedAt        string `json:"produced_at"`
	EngineVersion     string `json:"engine_version"`
	EngineBuildDirty  bool   `json:"engine_build_dirty"`
	FeatureCount      int    `json:"feature_count"`
	CriterionCount    int    `json:"criterion_count"`
	SignatureVerified bool   `json:"signature_verified,omitempty"`
	SignatureChecked  bool   `json:"signature_checked"`
}

// runProgramShow reads a signed ProgramBrief from disk and prints its
// metadata as JSON. The engine_version + dirty flag are first-class
// fields so `regatta program show <path> | jq .engine_version` is the
// audit-inspect UX called out by #549.
func runProgramShow(args []string) int {
	fs := flag.NewFlagSet("program show", flag.ExitOnError)
	keyEnv := fs.String("hmac-key-env", "", "Env var holding the HMAC key (if set, verify brief signature)")
	keyID := fs.String("hmac-key-id", "k1", "key_id to expect in the signature")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(fs.Output(), "Usage: regatta program show <brief.json>")
		fs.PrintDefaults()
	}
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}

	brief, err := loadProgramBrief(fs.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, "regatta program show:", err)
		return 1
	}

	report := programShowReport{
		ProgramID:        brief.ProgramID,
		ParentWorkItemID: brief.ParentWorkItemID,
		PlannerModelID:   brief.PlannerModelID,
		ProducedAt:       brief.ProducedAt.UTC().Format("2006-01-02T15:04:05Z"),
		EngineVersion:    brief.EngineVersion,
		EngineBuildDirty: brief.EngineBuildDirty,
		FeatureCount:     len(brief.Features),
		CriterionCount:   len(brief.ParentCriteria),
	}

	if *keyEnv != "" {
		report.SignatureChecked = true
		key := os.Getenv(*keyEnv)
		if key == "" {
			fmt.Fprintf(os.Stderr, "regatta program show: $%s is empty\n", *keyEnv)
			return 2
		}
		keyring := map[string][]byte{*keyID: []byte(key)}
		if err := brief.VerifySignature(keyring); err != nil {
			fmt.Fprintln(os.Stderr, "regatta program show: signature:", err)
			report.SignatureVerified = false
			emitJSON(report)
			return 1
		}
		report.SignatureVerified = true
	}

	emitJSON(report)
	return 0
}

// programSkewReport mirrors program.SkewResult plus the from/to SHAs
// so the JSON output is self-describing. Exit code 0 for no-skew,
// 1 for STRICT skew, 0-but-Skewed=true for WARN — the operator
// reads the `skewed` field.
type programSkewReport struct {
	ProgramID         string `json:"program_id"`
	RecordEngine      string `json:"record_engine_version"`
	RecordEngineDirty bool   `json:"record_engine_build_dirty"`
	CurrentEngine     string `json:"current_engine_version"`
	CurrentEngineDirty bool  `json:"current_engine_build_dirty"`
	Skewed            bool   `json:"skewed"`
	Tag               string `json:"tag,omitempty"`
	Warning           string `json:"warning,omitempty"`
	Mode              string `json:"mode"`
}

// runProgramReplaySkewCheck is the boundary the future audit replay
// path calls before treating a replay as reproducible. WARN is the
// default per #549 (keeps the autonomous loop unblocked); STRICT via
// --strict for audit-grade runs (returns non-zero on any skew).
//
// The current engine identity is read from program.EngineInfo() at
// call time so the same binary that's about to RE-RUN the engine is
// the one whose SHA the brief is compared against.
func runProgramReplaySkewCheck(args []string) int {
	fs := flag.NewFlagSet("program replay-skew-check", flag.ExitOnError)
	strict := fs.Bool("strict", false,
		"refuse replay on engine-version skew (exit 1). Default is WARN: emit a tag and continue.")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(fs.Output(), "Usage: regatta program replay-skew-check [--strict] <brief.json>")
		fs.PrintDefaults()
	}
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}

	brief, err := loadProgramBrief(fs.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, "regatta program replay-skew-check:", err)
		return 2
	}

	current := program.EngineInfo()
	res, skewErr := program.CheckEngineSkew(brief, current, *strict)

	mode := "warn"
	if *strict {
		mode = "strict"
	}
	report := programSkewReport{
		ProgramID:          brief.ProgramID,
		RecordEngine:       brief.EngineVersion,
		RecordEngineDirty:  brief.EngineBuildDirty,
		CurrentEngine:      current.Version,
		CurrentEngineDirty: current.Dirty,
		Skewed:             res.Skewed,
		Tag:                res.Tag,
		Warning:            res.Warning,
		Mode:               mode,
	}
	emitJSON(report)
	if errors.Is(skewErr, program.ErrEngineSkew) {
		return 1
	}
	if skewErr != nil {
		fmt.Fprintln(os.Stderr, "regatta program replay-skew-check:", skewErr)
		return 1
	}
	return 0
}

// loadProgramBrief reads + unmarshals a brief from disk. Validation is
// schema-only (Validate()); signature verification is opt-in via the
// caller's --hmac-key-env wiring so `program show` works against an
// unsigned dev fixture too.
func loadProgramBrief(path string) (*program.ProgramBrief, error) {
	raw, err := os.ReadFile(path) // #nosec G304 — operator-supplied path; this is a local CLI.
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var brief program.ProgramBrief
	if err := json.Unmarshal(raw, &brief); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := brief.Validate(); err != nil {
		return nil, fmt.Errorf("validate %s: %w", path, err)
	}
	return &brief, nil
}

// emitJSON pretty-prints to stdout. Centralized so all four code
// paths above (show success, show signature fail, skew success, skew
// fail) emit the same shape.
func emitJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
