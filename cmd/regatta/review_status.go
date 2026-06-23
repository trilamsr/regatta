// review subcommand: operator surfaces for the W7 L4-as-review path.
// Two verbs today — `status` reads recent gate_verdict events from the
// substrate so an operator can audit what the reviewer has decided
// (#625); `setup-codeowners` appends a catch-all line that auto-requests
// the reviewer-bot on every PR (#624). Both are read-mostly and idempotent.
package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

const (
	subcmdReview              = "review"
	reviewVerbStatus          = "status"
	reviewVerbSetupCodeowners = "setup-codeowners"
)

// runReview dispatches `regatta review <verb>`. Two verbs are wired:
// status (operator audit of recent verdicts) + setup-codeowners
// (idempotent CODEOWNERS catch-all).
func runReview(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "regatta review: expected verb (status | setup-codeowners)")
		return 2
	}
	switch args[0] {
	case reviewVerbStatus:
		return runReviewStatus(args[1:], os.Stdout)
	case reviewVerbSetupCodeowners:
		return runReviewSetupCodeowners(args[1:], os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "regatta review: unknown verb %q\n", args[0])
		return 2
	}
}

// runReviewStatus reads the last 7d of gate_verdict events from the
// substrate and prints one row per verdict. Read-only; does not touch
// GitHub so the call is safe to run inside the autonomous loop.
func runReviewStatus(args []string, out io.Writer) int {
	fs := flag.NewFlagSet("review status", flag.ContinueOnError)
	dbPath := fs.String("db", defaultStateDB(), "path to regatta state.db")
	sinceDur := fs.Duration("since", 7*24*time.Hour, "lookback window (e.g. 24h, 168h)")
	limit := fs.Int("limit", 50, "max rows to print")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *sinceDur <= 0 {
		fmt.Fprintf(os.Stderr, "review status: --since must be > 0 (got %s)\n", *sinceDur)
		return 2
	}
	db, err := sql.Open("sqlite", *dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "review status: open db: %v\n", err)
		return 1
	}
	defer func() { _ = db.Close() }()

	sinceMs := time.Now().Add(-*sinceDur).UnixMilli()
	rows, err := db.Query(
		`SELECT id, work_item_id, written_at, payload_json
		   FROM substrate_events
		  WHERE kind = ? AND written_at >= ?
		  ORDER BY written_at DESC
		  LIMIT ?`,
		string(substrate.KindGateVerdict), sinceMs, *limit,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "review status: query: %v\n", err)
		return 1
	}
	defer func() { _ = rows.Close() }()

	_, _ = fmt.Fprintf(out, "%-26s %-20s %-7s %-25s %s\n",
		"AGENT/WORK-ITEM", "GATE", "PASS", "POSTED-AT", "REASON")
	_, _ = fmt.Fprintln(out, strings.Repeat("-", 100))
	var printed int
	for rows.Next() {
		var id, workItem, payload string
		var writtenMs int64
		if err := rows.Scan(&id, &workItem, &writtenMs, &payload); err != nil {
			fmt.Fprintf(os.Stderr, "review status: scan: %v\n", err)
			return 1
		}
		var p substrate.GateVerdictPayload
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			continue
		}
		_, _ = fmt.Fprintf(out, "%-26s %-20s %-7s %-25s %s\n",
			collapse(workItem, 26),
			collapse(p.GateName, 20),
			passStr(p.Pass),
			time.UnixMilli(writtenMs).UTC().Format(time.RFC3339),
			collapse(p.Reason, 40),
		)
		printed++
	}
	if printed == 0 {
		_, _ = fmt.Fprintln(out, "(no gate verdicts in window)")
	}
	return 0
}

// passStr maps the bool to a short readable label.
func passStr(b bool) string {
	if b {
		return string(statusPass)
	}
	return string(statusFail)
}

// collapse truncates a string to width with an ellipsis — the 30-col
// collapse pattern used by `regatta status` panels (#625 brief).
func collapse(s string, width int) string {
	if width <= 1 {
		return s
	}
	if len(s) <= width {
		return s
	}
	return s[:width-1] + "…"
}

// stateDBDefaultLiteral is the canonical "regatta.db" literal default
// shared by every db-touching CLI subcommand (agents, audit, approval,
// cost, events, merge_status, resume, selfimprove) so a future
// relocation moves through one edit. Distinct from defaultDBPath
// (which is the resolver func for the --db flag default).
const stateDBDefaultLiteral = "regatta.db"

// defaultBranchName is the GitHub protected-branch default used when
// regatta.yaml omits repo.default_branch. Shared by doctor +
// verify-repo-config so a future convention change moves through one
// edit.
const defaultBranchName = "main"

// defaultStateDB returns the canonical regatta state.db path. Mirrors
// the stateDBDefaultLiteral the rest of the CLI uses so operators do
// not need to remember a different flag for each subcommand.
// REGATTA_STATE_DB overrides for daemon-side wiring that pins a
// non-default location (e.g. docker compose /data/regatta.db).
func defaultStateDB() string {
	if v := os.Getenv("REGATTA_STATE_DB"); v != "" {
		return v
	}
	return stateDBDefaultLiteral
}

// runReviewSetupCodeowners appends a catch-all CODEOWNERS line so the
// reviewer-bot is auto-requested on every PR (#624). Idempotent: if the
// line already exists the file is untouched and the verb exits 0.
func runReviewSetupCodeowners(args []string, out io.Writer) int {
	fs := flag.NewFlagSet("review setup-codeowners", flag.ContinueOnError)
	login := fs.String("reviewer-bot-login", "regatta-reviewer-bot",
		"GH login to request on every PR")
	repoRoot := fs.String("repo-root", ".", "repository root (CODEOWNERS lives under .github/)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*login) == "" {
		fmt.Fprintln(os.Stderr, "review setup-codeowners: --reviewer-bot-login required")
		return 2
	}
	target := filepath.Join(*repoRoot, ".github", "CODEOWNERS")
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		fmt.Fprintf(os.Stderr, "review setup-codeowners: mkdir: %v\n", err)
		return 1
	}
	wanted := fmt.Sprintf("* @%s", *login)
	existing, err := os.ReadFile(target) // #nosec G304 — target is built from a flag-constrained repo root + fixed suffix
	if err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "review setup-codeowners: read: %v\n", err)
		return 1
	}
	if containsLine(existing, wanted) {
		_, _ = fmt.Fprintf(out, "CODEOWNERS already has catch-all for %q — no change\n", *login)
		return 0
	}
	// Append; preserve any prior owner lines (operators may have repo-
	// specific patterns the catch-all must come AFTER per GH precedence).
	var buf strings.Builder
	if len(existing) > 0 {
		buf.Write(existing)
		if !strings.HasSuffix(string(existing), "\n") {
			buf.WriteString("\n")
		}
	}
	buf.WriteString("# regatta-reviewer-bot catch-all (W7 reviewer identity)\n")
	buf.WriteString(wanted)
	buf.WriteString("\n")
	// CODEOWNERS is a checked-in repo file — group-readable so a non-root
	// user committing the change downstream can stage it.
	if err := os.WriteFile(target, []byte(buf.String()), 0o644); err != nil { // #nosec G306 — repo-checked-in file, must be world-readable
		fmt.Fprintf(os.Stderr, "review setup-codeowners: write: %v\n", err)
		return 1
	}
	_, _ = fmt.Fprintf(out, "Wrote catch-all %q to %s\n", wanted, target)
	return 0
}

// containsLine returns true when `line` appears as a full line in body.
// Surrounding whitespace trimmed so editor-saved CODEOWNERS files do
// not duplicate the catch-all on re-run.
func containsLine(body []byte, line string) bool {
	for _, raw := range strings.Split(string(body), "\n") {
		if strings.TrimSpace(raw) == line {
			return true
		}
	}
	return false
}
