// approval subcommand tree: `decide` consumes a signed token + a
// reviewer decision; `list` enumerates pending approvals. Both share
// the runtime keyring + DB-open path (see loadApprovalTokenKeyring,
// openApprovalDB) so a future `approval cancel` follows one wiring.
package main

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/trilamsr/regatta/internal/canon"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// Exit codes for `approval decide`. Per spec §5.6 each typed sentinel
// maps to a stable code so operator runbooks can grep on the number.
// Distinct codes (no overload) — TestApprovalDecide_ExitCodeMappingTable
// asserts uniqueness so a refactor cannot silently collapse two.
const (
	exitTokenInvalid = 1
	exitUnverifiable = 2
	exitTokenExpired = 3
	exitTokenReplay  = 4
	exitUnknownKeyID = 5
	exitNotReviewer  = 6
	exitSelfReview   = 7
)

// systemActor stamps approval_events rows the CLI emits on behalf of
// the orchestrator (terminal approved|rejected wrappers). Distinct from
// the reviewer actor so the audit log can filter "machine wrote this"
// vs "human voted".
const systemActor = "system"

// errApprovalNotReviewer signals --reviewer-id ∉ reviewer_set_snapshot.
// Distinct from canon's ErrUnverifiable so the operator can see "your
// id is not on the gate" vs "your token does not check out".
var errApprovalNotReviewer = errors.New("approval: reviewer not in snapshot")

// errApprovalSelfReview signals the reviewer is the work_item's
// requested_by under prevent_self_review=true.
var errApprovalSelfReview = errors.New("approval: self-review blocked by gate policy")

// errApprovalDoubleVote signals the reviewer already appears in
// decided_by — a single reviewer cannot count twice toward quorum.
var errApprovalDoubleVote = errors.New("approval: reviewer already voted")

// runApproval dispatches the `approval ...` subcommand tree.
func runApproval(args []string) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(os.Stderr, "regatta approval: expected sub-subcommand (decide|list)")
		return 2
	}
	switch args[0] {
	case "decide":
		return runApprovalDecide(args[1:])
	case "list":
		return runApprovalList(args[1:])
	default:
		_, _ = fmt.Fprintf(os.Stderr, "regatta approval: unknown subcommand %q\n", args[0])
		return 2
	}
}

// approvalDecideDeps injects every side-effect the decide path touches:
// io for output, the clock for token-expiry math, and the DSN. Tests
// supply a temp-DB + fixed clock; production wiring uses os.Stdout/
// os.Stderr + time.Now + the `--db` flag.
type approvalDecideDeps struct {
	Stdout io.Writer
	Stderr io.Writer
	Clock  func() time.Time
	DSN    string
}

func runApprovalDecide(args []string) int {
	return runApprovalDecideWith(approvalDecideDeps{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Clock:  time.Now,
		DSN:    state.DSN(defaultDBPath(args)),
	}, args)
}

// defaultDBPath scans args for an explicit --db override; defaults to
// "regatta.db" so the production CLI matches `regatta serve`'s default
// and operator muscle memory.
func defaultDBPath(args []string) string {
	for i, a := range args {
		switch {
		case a == "--db" || a == "-db":
			if i+1 < len(args) {
				return args[i+1]
			}
		case strings.HasPrefix(a, "--db="):
			return strings.TrimPrefix(a, "--db=")
		case strings.HasPrefix(a, "-db="):
			return strings.TrimPrefix(a, "-db=")
		}
	}
	return "regatta.db"
}

// runApprovalDecideWith is the testable entry point. All side effects
// are routed through deps so the test harness can substitute a fixed
// clock + a temp-DB DSN. Exit codes map per spec §5.6 via exitCodeFor.
func runApprovalDecideWith(deps approvalDecideDeps, args []string) int {
	fs := flag.NewFlagSet("approval decide", flag.ContinueOnError)
	fs.SetOutput(deps.Stderr)
	tokenFlag := fs.String("token", "", "Signed approval token (wire format)")
	decisionFlag := fs.String("decision", "", "allow | deny")
	reasonFlag := fs.String("reason", "", "Optional human-readable reason")
	reviewerIDFlag := fs.String("reviewer-id", "", "Reviewer id presenting the token (required)")
	// --db is consumed by the outer caller via runApprovalDecide; tests
	// route around it by passing approvalDecideDeps.DSN directly.
	_ = fs.String("db", "regatta.db", "Path to sqlite state DB")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(deps.Stderr, "Usage: regatta approval decide --token <signed> --decision allow|deny --reviewer-id <id> [--reason <text>]")
		_, _ = fmt.Fprintln(deps.Stderr, "Exit codes:")
		_, _ = fmt.Fprintf(deps.Stderr, "  %d ErrTokenInvalid     malformed wire envelope\n", exitTokenInvalid)
		_, _ = fmt.Fprintf(deps.Stderr, "  %d ErrUnverifiable     HMAC mismatch or unknown-field payload\n", exitUnverifiable)
		_, _ = fmt.Fprintf(deps.Stderr, "  %d ErrTokenExpired     decision window has passed\n", exitTokenExpired)
		_, _ = fmt.Fprintf(deps.Stderr, "  %d ErrTokenReplay      token already consumed\n", exitTokenReplay)
		_, _ = fmt.Fprintf(deps.Stderr, "  %d ErrUnknownKeyID     keyring rotated; request fresh token\n", exitUnknownKeyID)
		_, _ = fmt.Fprintf(deps.Stderr, "  %d ErrNotReviewer      reviewer-id not in approval's snapshot\n", exitNotReviewer)
		_, _ = fmt.Fprintf(deps.Stderr, "  %d ErrSelfReview       self-review blocked by gate policy\n", exitSelfReview)
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *tokenFlag == "" || *decisionFlag == "" || *reviewerIDFlag == "" {
		fs.Usage()
		return 2
	}
	if *decisionFlag != "allow" && *decisionFlag != "deny" {
		_, _ = fmt.Fprintf(deps.Stderr, "regatta approval decide: --decision must be allow|deny, got %q\n", *decisionFlag)
		return 2
	}

	keyring, err := loadApprovalTokenKeyring()
	if err != nil {
		_, _ = fmt.Fprintln(deps.Stderr, "regatta approval decide:", err)
		return 2
	}

	payload, err := canon.VerifyToken(keyring, *tokenFlag, *reviewerIDFlag, deps.Clock())
	if err != nil {
		_, _ = fmt.Fprintln(deps.Stderr, "regatta approval decide:", err)
		return exitCodeFor(err)
	}

	// Belt-and-suspenders: VerifyToken already compared payload.Reviewer
	// to *reviewerIDFlag under the HMAC; we re-do that in constant time
	// here to keep the idiom explicit at the surface where an operator
	// would later add an --impersonate flag (don't let future-me erode
	// the timing-safe property).
	if subtle.ConstantTimeCompare([]byte(payload.Reviewer), []byte(*reviewerIDFlag)) != 1 {
		_, _ = fmt.Fprintln(deps.Stderr, "regatta approval decide:", canon.ErrUnverifiable)
		return exitUnverifiable
	}

	db, err := state.OpenWithClock(context.Background(), deps.DSN, deps.Clock)
	if err != nil {
		_, _ = fmt.Fprintln(deps.Stderr, "regatta approval decide: open db:", err)
		return 2
	}
	defer func() { _ = db.Close() }()

	folded, status, err := decideTx(context.Background(), db, payload, *reviewerIDFlag, *decisionFlag, *reasonFlag, deps.Clock)
	if err != nil {
		_, _ = fmt.Fprintln(deps.Stderr, "regatta approval decide:", err)
		return exitCodeFor(err)
	}
	slog.Info("approval.decided",
		slog.String("approval_id", payload.AID),
		slog.String("reviewer_id", *reviewerIDFlag),
		slog.String("decision", *decisionFlag),
		slog.String("status", status),
		slog.Int("decided_by_count", len(folded.decidedBy)),
	)
	return 0
}

// foldResult is the minimal slice of the event-log fold needed by the
// CLI: the running decided_by list (for self-double-vote rejection)
// and the derived terminal status (or pending). The full fold helper
// will move to internal/gates/approval/fold.go when PR #144 (A2)
// merges; until then this local helper keeps the decide path self-
// contained per the spec-deviation rule. Tracking issue filed in PR.
type foldResult struct {
	decidedBy     []string
	allowCount    int
	denyCount     int
	terminal      bool
	terminalAllow bool
}

func foldDecisions(events []state.ApprovalEvent, quorum int, reviewerSetSize int) foldResult {
	r := foldResult{}
	for _, ev := range events {
		if ev.Kind != "decided" {
			continue
		}
		var p struct {
			Decision string `json:"decision"`
		}
		_ = json.Unmarshal(ev.Payload, &p)
		r.decidedBy = append(r.decidedBy, ev.Actor)
		switch p.Decision {
		case "allow":
			r.allowCount++
		case "deny":
			r.denyCount++
		}
	}
	switch {
	case r.allowCount >= quorum:
		r.terminal = true
		r.terminalAllow = true
	case r.denyCount >= quorum:
		// ≥quorum denies terminates the gate as rejected (spec §6 N-of-M).
		r.terminal = true
	case r.denyCount+r.allowCount >= reviewerSetSize:
		// Every reviewer has voted and neither side reached quorum:
		// the gate cannot reach approval, so we close as rejected.
		r.terminal = true
	}
	return r
}

// decideTx is the single sqlite transaction that consumes the token,
// records the decision, folds the event log, and (when terminal)
// stamps the denorm status. Order matters: token_consumed first so
// the UNIQUE-index collision short-circuits replay before any 'decided'
// event leaks into the log. See spec §3.2 step 6.
func decideTx(ctx context.Context, db *state.DB, payload canon.TokenPayload, reviewerID, decision, reason string, clock func() time.Time) (foldResult, string, error) {
	a, err := db.GetApproval(ctx, payload.AID)
	if err != nil {
		return foldResult{}, "", err
	}
	if !inReviewerSet(a.ReviewerSetSnapshot.Reviewers, reviewerID) {
		return foldResult{}, "", errApprovalNotReviewer
	}
	if a.ReviewerSetSnapshot.PreventSelfReview && a.RequestedBy == reviewerID {
		return foldResult{}, "", errApprovalSelfReview
	}
	for _, prev := range a.DecidedBy {
		if prev == reviewerID {
			return foldResult{}, "", errApprovalDoubleVote
		}
	}

	tx, err := db.SQL().BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return foldResult{}, "", fmt.Errorf("begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	now := clock().UTC()

	// (1) token_consumed first: the UNIQUE(approval_id,kind,token_jti)
	// index turns a replayed token into a transactional abort BEFORE
	// any 'decided' row materialises. AppendApprovalEvent maps the
	// collision to state.ErrTokenReplay.
	if err := insertApprovalEvent(ctx, tx, state.ApprovalEvent{
		ApprovalID: payload.AID,
		Ts:         now,
		Kind:       "token_consumed",
		Actor:      reviewerID,
		TokenJTI:   payload.JTI,
	}); err != nil {
		return foldResult{}, "", err
	}

	// (2) decided event carries the vote.
	decidedPayload, _ := json.Marshal(struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason,omitempty"`
	}{Decision: decision, Reason: reason})
	if err := insertApprovalEvent(ctx, tx, state.ApprovalEvent{
		ApprovalID: payload.AID,
		Ts:         now,
		Kind:       "decided",
		Actor:      reviewerID,
		Payload:    decidedPayload,
	}); err != nil {
		return foldResult{}, "", err
	}

	// (3) Re-list events inside the tx so the fold sees the just-inserted
	// vote. sqlite's single-writer guarantee makes this consistent.
	events, err := listApprovalEventsTx(ctx, tx, payload.AID)
	if err != nil {
		return foldResult{}, "", err
	}
	folded := foldDecisions(events, a.Quorum, len(a.ReviewerSetSnapshot.Reviewers))

	status := state.ApprovalStatusPending
	if folded.terminal {
		if folded.terminalAllow {
			status = state.ApprovalStatusApproved
			if err := insertApprovalEvent(ctx, tx, state.ApprovalEvent{
				ApprovalID: payload.AID, Ts: now, Kind: "approved", Actor: systemActor,
			}); err != nil {
				return foldResult{}, "", err
			}
		} else {
			status = state.ApprovalStatusRejected
			if err := insertApprovalEvent(ctx, tx, state.ApprovalEvent{
				ApprovalID: payload.AID, Ts: now, Kind: "rejected", Actor: systemActor,
			}); err != nil {
				return foldResult{}, "", err
			}
		}
		if err := markApprovalDecidedTx(ctx, tx, payload.AID, status, folded.decidedBy, now); err != nil {
			return foldResult{}, "", err
		}
	}

	if err := tx.Commit(); err != nil {
		return foldResult{}, "", fmt.Errorf("commit: %w", err)
	}
	committed = true
	return folded, status, nil
}

// insertApprovalEvent mirrors state.AppendApprovalEvent's INSERT
// statement but runs against the caller's *sql.Tx so all five mutations
// share one BEGIN..COMMIT (spec §3.2.1). The UNIQUE collision detection
// is duplicated here because exposing tx-aware AppendApprovalEvent
// would widen state's surface area mid-wave.
func insertApprovalEvent(ctx context.Context, tx *sql.Tx, ev state.ApprovalEvent) error {
	payload := ev.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO approval_events (
			approval_id, ts, kind, actor, payload_json, token_jti
		) VALUES (?, ?, ?, ?, ?, ?)`,
		ev.ApprovalID, ev.Ts.UTC().Unix(), ev.Kind, ev.Actor,
		string(payload), ev.TokenJTI,
	)
	if err != nil {
		if isUniqueTokenConsumeTx(err) {
			return state.ErrTokenReplay
		}
		return fmt.Errorf("insert approval_event: %w", err)
	}
	return nil
}

// isUniqueTokenConsumeTx mirrors state.isUniqueTokenConsume: modernc.org/sqlite
// surfaces the colliding column tuple (not the index name) in the error text.
// TestApprovalDecide_ReplayReturnsExit4 pins the two probes in lockstep.
func isUniqueTokenConsumeTx(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "approval_events.approval_id") &&
		strings.Contains(msg, "approval_events.kind") &&
		strings.Contains(msg, "approval_events.token_jti")
}

func listApprovalEventsTx(ctx context.Context, tx *sql.Tx, approvalID string) ([]state.ApprovalEvent, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, approval_id, ts, kind, actor, payload_json, token_jti
		FROM approval_events
		WHERE approval_id = ?
		ORDER BY id`, approvalID)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []state.ApprovalEvent
	for rows.Next() {
		var e state.ApprovalEvent
		var ts int64
		var payload string
		if err := rows.Scan(&e.ID, &e.ApprovalID, &ts, &e.Kind, &e.Actor, &payload, &e.TokenJTI); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		e.Ts = time.Unix(ts, 0).UTC()
		e.Payload = json.RawMessage(payload)
		out = append(out, e)
	}
	return out, rows.Err()
}

func markApprovalDecidedTx(ctx context.Context, tx *sql.Tx, id, status string, decidedBy []string, decidedAt time.Time) error {
	by, err := json.Marshal(decidedBy)
	if err != nil {
		return fmt.Errorf("marshal decided_by: %w", err)
	}
	now := decidedAt.UTC().Unix()
	res, err := tx.ExecContext(ctx, `
		UPDATE approvals
		SET status = ?, decided_at = ?, decided_by = ?, updated_at = ?
		WHERE id = ?`,
		status, decidedAt.UTC().Unix(), string(by), now, id)
	if err != nil {
		return fmt.Errorf("update approval: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("approval id=%q not found", id)
	}
	return nil
}

// inReviewerSet is a constant-time membership check across the snapshot.
// Linear scan is fine — the snapshot is bounded by the configured
// reviewer set, typically <10 ids. Using subtle.ConstantTimeCompare
// matches the spec's belt-and-suspenders posture on the --reviewer-id
// flag (no early-exit on first match either).
func inReviewerSet(set []string, id string) bool {
	hit := 0
	for _, r := range set {
		if subtle.ConstantTimeCompare([]byte(r), []byte(id)) == 1 {
			hit = 1
		}
	}
	return hit == 1
}

// exitCodeFor maps every typed sentinel the decide path can surface to
// its spec §5.6 exit code. errors.Is is used so wrapped errors still
// route correctly. Anything outside the table maps to 1 (generic) — the
// decide path only returns wrapped versions of the listed sentinels.
func exitCodeFor(err error) int {
	switch {
	case errors.Is(err, canon.ErrTokenInvalid):
		return exitTokenInvalid
	case errors.Is(err, canon.ErrTokenExpired):
		return exitTokenExpired
	case errors.Is(err, canon.ErrUnknownKeyID):
		return exitUnknownKeyID
	case errors.Is(err, canon.ErrUnverifiable):
		return exitUnverifiable
	case errors.Is(err, state.ErrTokenReplay):
		return exitTokenReplay
	case errors.Is(err, errApprovalNotReviewer):
		return exitNotReviewer
	case errors.Is(err, errApprovalSelfReview):
		return exitSelfReview
	default:
		return 1
	}
}

// loadApprovalTokenKeyring reads the HMAC key from
// $REGATTA_APPROVAL_TOKEN_KEY (or the env var named by
// $REGATTA_APPROVAL_TOKEN_KEY_ENV) and returns it keyed by
// $REGATTA_APPROVAL_TOKEN_KEY_ID (default "k1"). Distinct env from the
// brief-keyring loader so the approval-token surface can rotate
// without touching brief signing.
func loadApprovalTokenKeyring() (canon.Keyring, error) {
	envName := os.Getenv("REGATTA_APPROVAL_TOKEN_KEY_ENV")
	if envName == "" {
		envName = "REGATTA_APPROVAL_TOKEN_KEY"
	}
	v := os.Getenv(envName)
	if v == "" {
		return nil, fmt.Errorf("approval token key not set: export %s or $REGATTA_APPROVAL_TOKEN_KEY_ENV", envName)
	}
	keyID := os.Getenv("REGATTA_APPROVAL_TOKEN_KEY_ID")
	if keyID == "" {
		keyID = "k1"
	}
	return canon.MapKeyring{keyID: []byte(v)}, nil
}
