// audit subcommand tree. Today: `audit verify --run-id R --db PATH`
// surfaces, per gate_verdict event recorded under run R, the (tool,
// version, deterministic flag, audit posture, hmac-status, schema-skew)
// tuple that operators need to answer "what was actually recorded and
// is the chain intact?".
//
// Issue #550 reframe: this command does NOT re-execute gates. It walks
// the journaled verdicts and proves the HMAC chain is intact (tamper-
// evidence). Re-execution to compare against the recorded verdict is
// only meaningful when verdict.Deterministic=true — the output marks
// each row "reproduce" or postureVerifyOnly so an auditor cannot mistake
// the latter for a re-runnable artifact.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
	"github.com/trilamsr/regatta/internal/secrets"
)

const (
	subcmdAudit = "audit"

	// postureVerifyOnly is the operator-facing posture string for
	// non-deterministic verdicts: chain proves bytes intact, gate is
	// non-replayable. Shared by every row that lands in the
	// verify-only bucket so lint sees one source of truth. Mirrors
	// substrate.GateVerdictPayload.AuditPosture() — that method is
	// the canonical producer for properly-tagged verdicts; this
	// constant covers the decode-error + unknown-legacy backfill path
	// where the payload itself never returned a posture string.
	postureVerifyOnly = "verify-only"

	// toolUnknownLegacy is the Tool / ToolVersion sentinel for pre-#550
	// verdicts decoded at audit time. Single source of truth so prod
	// fold + every test that pins the legacy-backfill path reference
	// one literal — goconst would otherwise flag the 4th occurrence.
	toolUnknownLegacy = "unknown-legacy"

	// hmacStatusChainOK is the per-row HMAC verdict when Verify
	// returns nil. Hoisted to a constant so the production fold + test
	// assertions share one literal; lint goconst rejects 3+ inline.
	hmacStatusChainOK = "chain-ok"
)

// auditDeps injects every side-effect the audit path touches so
// tests substitute a fixed clock + temp-DB DSN. Mirrors
// approvalDecideDeps so the audit subcommand follows existing wiring.
type auditDeps struct {
	Stdout io.Writer
	Stderr io.Writer
	DSN    string
}

// runAudit dispatches the `audit ...` subcommand tree.
func runAudit(args []string) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(os.Stderr, "regatta audit: expected sub-subcommand (verify)")
		return 2
	}
	switch args[0] {
	case "verify":
		return runAuditVerify(args[1:])
	default:
		_, _ = fmt.Fprintf(os.Stderr, "regatta audit: unknown subcommand %q\n", args[0])
		return 2
	}
}

// runAuditVerify is the operator-facing entry point.
func runAuditVerify(args []string) int {
	return runAuditVerifyWith(auditDeps{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		DSN:    state.DSN(defaultDBPath(args)),
	}, args)
}

// auditVerifyRow is the per-verdict shape `audit verify` emits.
// Field order is operator-stable: an auditor scrolling JSON output
// hits the load-bearing columns (gate, tool, version, posture,
// hmac-status, schema-skew) in the order they reason about them.
type auditVerifyRow struct {
	GateName        string `json:"gate_name"`
	WorkItemID      string `json:"work_item_id"`
	Pass            bool   `json:"pass"`
	Reason          string `json:"reason,omitempty"`
	Tool            string `json:"tool"`
	ToolVersion     string `json:"tool_version"`
	Deterministic   bool   `json:"deterministic"`
	AuditPosture    string `json:"audit_posture"`
	HMACStatus      string `json:"hmac_status"`
	RecordedSchema  int64  `json:"recorded_schema_version"`
	RunningSchema   int64  `json:"running_schema_version"`
	SchemaSkew      bool   `json:"schema_skew"`
	EventID         string `json:"event_id"`
	WrittenAtMillis int64  `json:"written_at_ms"`
}

// auditVerifySummary is the human-readable header `audit verify`
// prints before the per-row JSON. Operators scanning a single
// terminal pane see the (chain-ok / chain-broken, schema-skew count,
// deterministic split) at a glance.
type auditVerifySummary struct {
	RunID                string `json:"run_id"`
	Total                int    `json:"total"`
	ChainOK              int    `json:"chain_ok"`
	ChainBroken          int    `json:"chain_broken"`
	SchemaSkew           int    `json:"schema_skew"`
	Reproducible         int    `json:"reproducible"`
	VerifyOnly           int    `json:"verify_only"`
	RunningSchemaVersion int64  `json:"running_schema_version"`
}

func runAuditVerifyWith(deps auditDeps, args []string) int {
	fs := flag.NewFlagSet("audit verify", flag.ContinueOnError)
	fs.SetOutput(deps.Stderr)
	runID := fs.String("run-id", "", "Run id to verify (required)")
	_ = fs.String("db", stateDBDefaultLiteral, "Path to sqlite state DB")
	format := fs.String("format", "json", "Output format: json | table")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(deps.Stderr, "Usage: regatta audit verify --run-id <id> [--db <path>] [--format json|table]")
		_, _ = fmt.Fprintln(deps.Stderr, "Walks every gate_verdict event for the run, prints per-gate (tool, version, deterministic, hmac-status, schema-skew).")
		_, _ = fmt.Fprintln(deps.Stderr, "Exits 0 when the HMAC chain is intact across all verdicts; non-zero when any chain-broken row is observed.")
		_, _ = fmt.Fprintln(deps.Stderr, "")
		_, _ = fmt.Fprintln(deps.Stderr, "Audit posture per row:")
		_, _ = fmt.Fprintln(deps.Stderr, "  reproduce   verdict.deterministic=true — same input must yield same verdict")
		_, _ = fmt.Fprintln(deps.Stderr, "  verify-only verdict.deterministic=false — chain is tamper-evident but verdict is non-replayable")
		_, _ = fmt.Fprintln(deps.Stderr, "")
		_, _ = fmt.Fprintln(deps.Stderr, "Keyring (REGATTA_AUDIT_HMAC_KEY env): when unset the command exits non-zero")
		_, _ = fmt.Fprintln(deps.Stderr, "with every row marked chain-unverifiable; it does NOT silently skip verification.")
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *runID == "" {
		fs.Usage()
		return 2
	}

	ctx := context.Background()
	db, err := state.Open(ctx, deps.DSN)
	if err != nil {
		_, _ = fmt.Fprintf(deps.Stderr, "regatta audit verify: open db: %v\n", err)
		return 1
	}
	defer func() { _ = db.Close() }()

	events, err := substrate.Fold(ctx, db.SQL(), *runID, substrate.KindGateVerdict)
	if err != nil {
		_, _ = fmt.Fprintf(deps.Stderr, "regatta audit verify: fold: %v\n", err)
		return 1
	}

	// Keyring resolution: the audit-verify CLI accepts the same
	// keyring discipline as `program verify-handoff` (env-var key),
	// but for now the chain-only path uses the substrate's own
	// IsUnverifiable error and treats key-loading failure as
	// chain-broken on every row so the operator gets a clear signal
	// instead of a silent zero count.
	keyring, keyErr := loadAuditVerifyKeyring()

	rows := make([]auditVerifyRow, 0, len(events))
	summary := auditVerifySummary{
		RunID:                *runID,
		Total:                len(events),
		RunningSchemaVersion: state.CurrentSchemaVersion,
	}

	for _, e := range events {
		row := auditVerifyRow{
			GateName:        e.Key,
			WorkItemID:      e.WorkItemID,
			EventID:         e.ID,
			WrittenAtMillis: e.WrittenAt,
		}
		// Decode payload best-effort. A row that fails to decode is
		// flagged as chain-broken without crashing the whole audit —
		// the operator still wants to see the chain status of the
		// other rows.
		var p substrate.GateVerdictPayload
		if jerr := json.Unmarshal(e.PayloadJSON, &p); jerr == nil {
			row.Pass = p.Pass
			row.Reason = p.Reason
			row.Tool = p.Tool
			row.ToolVersion = p.ModelOrVersion
			row.Deterministic = p.Deterministic
			row.AuditPosture = p.AuditPosture()
			row.RecordedSchema = p.DBSchemaVersion
			// Legacy-row backfill: a payload that decodes but has no
			// Tool was written before issue #550. Operators surfacing
			// these in `audit verify` need to see them flagged as
			// non-deterministic (we cannot prove otherwise) with an
			// explicit "unknown-legacy" tool so the row is visibly
			// distinct from a properly-tagged verify-only verdict.
			if row.Tool == "" {
				row.Tool = toolUnknownLegacy
				row.ToolVersion = toolUnknownLegacy
				row.Deterministic = false
				row.AuditPosture = postureVerifyOnly
			}
		} else {
			// Decode-error rows are non-replayable by construction: we
			// could not even parse the recorded bytes, so Deterministic
			// must be explicit (not the implicit zero-value) so the
			// verify-only bucket counter increments and the JSON column
			// reflects the actual posture instead of relying on field
			// ordering invariants.
			row.Tool = "decode-error"
			row.Deterministic = false
			row.AuditPosture = postureVerifyOnly
		}
		row.RunningSchema = state.CurrentSchemaVersion
		row.SchemaSkew = row.RecordedSchema != 0 && row.RecordedSchema != state.CurrentSchemaVersion

		switch {
		case keyErr != nil:
			row.HMACStatus = "chain-unverifiable"
		default:
			if verr := substrate.Verify(e, keyring); verr == nil {
				row.HMACStatus = hmacStatusChainOK
				summary.ChainOK++
			} else {
				row.HMACStatus = "chain-broken"
				summary.ChainBroken++
			}
		}
		if row.SchemaSkew {
			summary.SchemaSkew++
		}
		if row.Deterministic {
			summary.Reproducible++
		} else {
			summary.VerifyOnly++
		}
		rows = append(rows, row)
	}

	// Stable order so an auditor diffing two runs sees structural
	// matches without extra sorting.
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].WrittenAtMillis != rows[j].WrittenAtMillis {
			return rows[i].WrittenAtMillis < rows[j].WrittenAtMillis
		}
		return rows[i].EventID < rows[j].EventID
	})

	if err := emitAuditVerify(deps.Stdout, *format, summary, rows); err != nil {
		_, _ = fmt.Fprintf(deps.Stderr, "regatta audit verify: emit: %v\n", err)
		return 1
	}

	if summary.ChainBroken > 0 || keyErr != nil {
		return 1
	}
	return 0
}

// loadAuditVerifyKeyring resolves the HMAC keyring via secrets.Fetcher
// so an operator may route the audit key through `secrets:` in
// regatta.yaml; back-compat: no block ⇒ legacy REGATTA_AUDIT_HMAC_KEY
// path inside the Default chain still wins.
func loadAuditVerifyKeyring() (map[string][]byte, error) {
	ctx := context.Background()
	var key string
	if v, err := secrets.DefaultNoPlatform(ctx).Get(ctx, secrets.KeyAuditHMACKey); err == nil {
		key = string(v.Bytes())
	}
	keyID := os.Getenv("REGATTA_AUDIT_HMAC_KEY_ID")
	if key == "" {
		return nil, fmt.Errorf("REGATTA_AUDIT_HMAC_KEY not set")
	}
	if keyID == "" {
		keyID = "k1"
	}
	return map[string][]byte{keyID: []byte(key)}, nil
}

// emitAuditVerify writes the summary + rows in the requested format.
// JSON is the default for downstream tooling; table is operator
// triage shape.
func emitAuditVerify(w io.Writer, format string, summary auditVerifySummary, rows []auditVerifyRow) error {
	switch format {
	case logFormatJSON, "":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		out := struct {
			Summary auditVerifySummary `json:"summary"`
			Rows    []auditVerifyRow   `json:"rows"`
		}{summary, rows}
		return enc.Encode(out)
	case formatTable:
		_, _ = fmt.Fprintf(w, "run=%s total=%d chain_ok=%d chain_broken=%d schema_skew=%d reproducible=%d verify_only=%d running_schema=%d\n",
			summary.RunID, summary.Total, summary.ChainOK, summary.ChainBroken,
			summary.SchemaSkew, summary.Reproducible, summary.VerifyOnly, summary.RunningSchemaVersion)
		_, _ = fmt.Fprintln(w, "gate                       pass   tool             tool_version              det  posture       hmac              schema(rec/run)")
		for _, r := range rows {
			_, _ = fmt.Fprintf(w, "%-26s %-6t %-16s %-25s %-4t %-13s %-17s %d/%d%s\n",
				truncate(r.GateName, 26), r.Pass, truncate(r.Tool, 16), truncate(r.ToolVersion, 25),
				r.Deterministic, r.AuditPosture, r.HMACStatus,
				r.RecordedSchema, r.RunningSchema, skewSuffix(r.SchemaSkew))
		}
		return nil
	default:
		return fmt.Errorf("unknown format %q (want json|table)", format)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

func skewSuffix(skew bool) string {
	if skew {
		return " SKEW"
	}
	return ""
}
