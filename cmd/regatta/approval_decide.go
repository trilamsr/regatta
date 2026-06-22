// approval subcommand tree: `decide` consumes a signed token + a
// reviewer decision; `list` enumerates pending approvals. Both share
// the runtime keyring + DB-open path (see loadApprovalTokenKeyring,
// openApprovalDB) so a future `approval cancel` follows one wiring.
package main

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/trilamsr/regatta/internal/canon/approvaltoken"
	"github.com/trilamsr/regatta/internal/gates/approval"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/secrets"
)

// Exit codes for `approval decide` per spec §5.6.
const (
	exitTokenInvalid = 1
	exitUnverifiable = 2
	exitTokenExpired = 3
	exitTokenReplay  = 4
	exitUnknownKeyID = 5
	exitNotReviewer  = 6
	exitSelfReview   = 7
)

// Aliases kept post-lift so existing test identifiers compile without edits.
var (
	errApprovalNotReviewer = approval.ErrNotReviewer
	errApprovalSelfReview  = approval.ErrSelfReview
)

// insertApprovalEvent aliases approval.InsertApprovalEvent so approval_decide_trace_id_test.go compiles post-lift without edits.
func insertApprovalEvent(ctx context.Context, tx *sql.Tx, ev state.ApprovalEvent) error {
	return approval.InsertApprovalEvent(ctx, tx, ev)
}

// runApproval is the CLI entry point for the `approval ...` subcommand tree.
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

// approvalDecideDeps lifts clock + stdio + DSN so tests substitute deterministic fakes.
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

// defaultDBPath scans args for --db override; default falls through
// to defaultStateDB() so the REGATTA_STATE_DB env override the docker
// compose stack pins (R3-Bug-6) flows into every subcommand using
// this resolver (approval decide, events tail). Without this the
// literal "regatta.db" wins and operators get an empty cwd-sqlite.
func defaultDBPath(args []string) string {
	for i, a := range args {
		switch {
		case a == "--db" || a == "-db": //nolint:goconst // operator-facing flag name; const would obscure the literal at the parse site
			if i+1 < len(args) {
				return args[i+1]
			}
		case strings.HasPrefix(a, "--db="):
			return strings.TrimPrefix(a, "--db=")
		case strings.HasPrefix(a, "-db="):
			return strings.TrimPrefix(a, "-db=")
		}
	}
	return defaultStateDB()
}

// runApprovalDecideWith is the testable entry point so tests bypass os.Stdout/os.Stderr/time.Now.
func runApprovalDecideWith(deps approvalDecideDeps, args []string) int {
	fs := flag.NewFlagSet("approval decide", flag.ContinueOnError)
	fs.SetOutput(deps.Stderr)
	tokenFlag := fs.String("token", "", "Signed approval token (wire format)")
	decisionFlag := fs.String("decision", "", "allow | deny")
	reasonFlag := fs.String("reason", "", "Optional human-readable reason")
	reviewerIDFlag := fs.String("reviewer-id", "", "Reviewer id presenting the token (required)")
	_ = fs.String("db", defaultStateDB(), "Path to sqlite state DB")
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

	payload, err := approvaltoken.VerifyToken(keyring, *tokenFlag, *reviewerIDFlag, deps.Clock())
	if err != nil {
		_, _ = fmt.Fprintln(deps.Stderr, "regatta approval decide:", err)
		return exitCodeFor(err)
	}

	// Belt-and-suspenders constant-time re-check at the CLI surface so a
	// future --impersonate flag cannot erode the timing-safe property.
	if subtle.ConstantTimeCompare([]byte(payload.Reviewer), []byte(*reviewerIDFlag)) != 1 {
		_, _ = fmt.Fprintln(deps.Stderr, "regatta approval decide:", approvaltoken.ErrUnverifiable)
		return exitUnverifiable
	}

	db, err := state.OpenWithClock(context.Background(), deps.DSN, deps.Clock)
	if err != nil {
		_, _ = fmt.Fprintln(deps.Stderr, "regatta approval decide: open db:", err)
		return 2
	}
	defer func() { _ = db.Close() }()

	folded, status, err := approval.DecideTx(context.Background(), db, payload, *reviewerIDFlag, *decisionFlag, *reasonFlag, deps.Clock)
	if err != nil {
		_, _ = fmt.Fprintln(deps.Stderr, "regatta approval decide:", err)
		return exitCodeFor(err)
	}
	slog.Info("approval.decided",
		slog.String("approval_id", payload.AID),
		slog.String("reviewer_id", *reviewerIDFlag),
		slog.String("decision", *decisionFlag),
		slog.String("status", status),
		slog.Int("decided_by_count", len(folded.DecidedBy)),
	)
	return 0
}

// exitCodeFor maps typed sentinels to spec §5.6 exit codes; off-table errors fall back to 1 (generic).
func exitCodeFor(err error) int {
	switch {
	case errors.Is(err, approvaltoken.ErrTokenInvalid):
		return exitTokenInvalid
	case errors.Is(err, approvaltoken.ErrTokenExpired):
		return exitTokenExpired
	case errors.Is(err, approvaltoken.ErrUnknownKeyID):
		return exitUnknownKeyID
	case errors.Is(err, approvaltoken.ErrUnverifiable):
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

// loadApprovalTokenKeyring resolves the approval-token HMAC via the
// secrets.Fetcher chain so the surface rotates independently of brief
// signing; back-compat: REGATTA_APPROVAL_TOKEN_KEY_ENV → named env or
// the legacy REGATTA_APPROVAL_TOKEN_KEY (via alias adapter) still wins.
func loadApprovalTokenKeyring() (approvaltoken.Keyring, error) {
	envName := os.Getenv("REGATTA_APPROVAL_TOKEN_KEY_ENV")
	var v string
	if envName != "" {
		v = os.Getenv(envName)
	} else {
		ctx := context.Background()
		if got, err := secrets.DefaultNoPlatform(ctx).Get(ctx, secrets.KeyApprovalToken); err == nil {
			v = string(got.Bytes())
		}
		envName = "REGATTA_APPROVAL_TOKEN_KEY"
	}
	if v == "" {
		return nil, fmt.Errorf("approval token key not set: export %s or $REGATTA_APPROVAL_TOKEN_KEY_ENV", envName)
	}
	keyID := os.Getenv("REGATTA_APPROVAL_TOKEN_KEY_ID")
	if keyID == "" {
		keyID = "k1"
	}
	return approvaltoken.MapKeyring{keyID: []byte(v)}, nil
}
