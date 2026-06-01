package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// approvalListDeps mirrors approvalDecideDeps — io + clock + DSN — so
// tests don't have to env-mutate the DB path.
type approvalListDeps struct {
	Stdout io.Writer
	Stderr io.Writer
	Clock  func() time.Time
	DSN    string
}

func runApprovalList(args []string) int {
	return runApprovalListWith(approvalListDeps{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Clock:  time.Now,
		DSN:    state.DSN(defaultDBPath(args)),
	}, args)
}

func runApprovalListWith(deps approvalListDeps, args []string) int {
	fs := flag.NewFlagSet("approval list", flag.ContinueOnError)
	fs.SetOutput(deps.Stderr)
	mineFlag := fs.String("mine", "", "Filter to approvals whose snapshot includes the given reviewer id")
	formatFlag := fs.String("format", "table", "Output format: table | json")
	_ = fs.String("db", "regatta.db", "Path to sqlite state DB")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(deps.Stderr, "Usage: regatta approval list [--mine <reviewer-id>] [--format=table|json]")
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *formatFlag != "table" && *formatFlag != "json" {
		_, _ = fmt.Fprintf(deps.Stderr, "regatta approval list: --format must be table|json, got %q\n", *formatFlag)
		return 2
	}

	db, err := state.OpenWithClock(context.Background(), deps.DSN, deps.Clock)
	if err != nil {
		_, _ = fmt.Fprintln(deps.Stderr, "regatta approval list: open db:", err)
		return 2
	}
	defer func() { _ = db.Close() }()

	approvals, err := db.ListPendingApprovals(context.Background())
	if err != nil {
		_, _ = fmt.Fprintln(deps.Stderr, "regatta approval list:", err)
		return 1
	}
	if *mineFlag != "" {
		filtered := approvals[:0:0]
		for _, a := range approvals {
			if containsString(a.ReviewerSetSnapshot.Reviewers, *mineFlag) {
				filtered = append(filtered, a)
			}
		}
		approvals = filtered
	}

	if *formatFlag == "json" {
		return emitApprovalsJSON(deps.Stdout, approvals)
	}
	return emitApprovalsTable(deps.Stdout, approvals)
}

// approvalListRow is the contract surface for --format=json. Pinned by
// TestApprovalList_JSONShape so a column drift in cmd/regatta cannot
// silently break a downstream operator scriptr.
type approvalListRow struct {
	ApprovalID      string   `json:"approval_id"`
	WorkItemID      string   `json:"work_item_id"`
	GateName        string   `json:"gate_name"`
	RequestedAtUnix int64    `json:"requested_at_unix"`
	TimeoutAtUnix   int64    `json:"timeout_at_unix"`
	ReviewerSet     []string `json:"reviewer_set"`
	Quorum          int      `json:"quorum"`
}

func emitApprovalsJSON(out io.Writer, approvals []state.Approval) int {
	rows := make([]approvalListRow, 0, len(approvals))
	for _, a := range approvals {
		rows = append(rows, approvalListRow{
			ApprovalID:      a.ID,
			WorkItemID:      a.WorkItemID,
			GateName:        a.GateName,
			RequestedAtUnix: a.RequestedAt.Unix(),
			TimeoutAtUnix:   a.TimeoutAt.Unix(),
			ReviewerSet:     a.ReviewerSetSnapshot.Reviewers,
			Quorum:          a.Quorum,
		})
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(rows); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "regatta approval list: encode json:", err)
		return 1
	}
	return 0
}

func emitApprovalsTable(out io.Writer, approvals []state.Approval) int {
	if len(approvals) == 0 {
		_, _ = fmt.Fprintln(out, "no approvals waiting.")
		return 0
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "APPROVAL_ID\tWORK_ITEM_ID\tGATE_NAME\tREQUESTED_AT\tTIMEOUT_AT\tREVIEWERS\tQUORUM")
	for _, a := range approvals {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%d\n",
			a.ID, a.WorkItemID, a.GateName,
			a.RequestedAt.UTC().Format(time.RFC3339),
			a.TimeoutAt.UTC().Format(time.RFC3339),
			strings.Join(a.ReviewerSetSnapshot.Reviewers, ","),
			a.Quorum,
		)
	}
	if err := tw.Flush(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "regatta approval list: flush:", err)
		return 1
	}
	return 0
}

func containsString(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}
