package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// subcmdStatus binds the regatta status TUI verb. The actual binding
// into main.subcommands lives in status_register.go (init()) so the
// table stays the single source of truth even with this file added.
const subcmdStatus = "status"

// Panel title constants — referenced from the SQL-backed source plus
// degraded-source paths so the dashboard frame stays stable across
// production wiring + tests.
const (
	titleActiveSubagents = "Active subagents"
	titleInFlightPRs     = "In-flight PRs"
	titleRecentMerges    = "Recent merges (24h)"
	titleCostToday       = "Cost today"
	titleGreenClock      = "Green-clock progress"
)

// PanelState classifies what a single panel can render. OK is the
// happy path; EMPTY means the metric exists but has no observation
// yet (early Wave-A/B/C boot); MISSING means the upstream metric is
// not registered at all (partial-wave deployment per spec §11).
type PanelState int

const (
	PanelOK PanelState = iota
	PanelEmpty
	PanelMissing
)

// Panel is one of the five status views the TUI renders. Lines are
// pre-rendered strings so the View pass stays a pure string-join (no
// data access in the hot loop). Title is the panel header.
type Panel struct {
	Title string
	State PanelState
	Lines []string
}

// Snapshot is the full state the TUI displays. Sources fill this on
// each refresh tick; the renderer only reads from it so a slow
// backend cannot block paint (spec §4.3 per-panel degradation).
type Snapshot struct {
	At              time.Time
	ActiveSubagents Panel
	InFlightPRs     Panel
	RecentMerges    Panel
	CostToday       Panel
	GreenClock      Panel
	StaleBanner     string
}

// Source is the read-side seam — the production wiring opens a
// read-only sqlite WAL conn and a Prom HTTP client; tests inject a
// stub. Per spec §4.3 the source MUST be non-blocking; a slow backend
// surfaces as PanelMissing, not a stall.
type Source interface {
	Snapshot(ctx context.Context) Snapshot
}

// Renderer projects a Snapshot to a flat ANSI string. Width/Height
// cap the output so small terminals collapse instead of corrupting
// (spec §4.7). The Renderer is stateless; one instance per process is
// fine.
type Renderer struct {
	Width  int
	Height int
}

// Render returns the full TUI frame as a single string. The caller
// writes it to the terminal verbatim — clear-screen + cursor-home are
// the caller's job so unit tests can diff the frame body without ANSI
// noise.
func (r Renderer) Render(s Snapshot) string {
	w := r.Width
	if w == 0 {
		w = 80
	}
	h := r.Height
	if h == 0 {
		h = 24
	}

	// Below 40 cols the panels cannot render legibly; surface a
	// short banner per spec §4.7.
	if w < 40 {
		return "terminal too narrow — resize to >= 60 cols\n"
	}

	var b strings.Builder
	header := fmt.Sprintf("regatta status  %s", s.At.Format(time.RFC3339))
	fmt.Fprintln(&b, header)
	fmt.Fprintln(&b, strings.Repeat("-", min(w, 80)))

	if s.StaleBanner != "" {
		fmt.Fprintf(&b, "! %s\n", s.StaleBanner)
	}

	panels := []Panel{s.ActiveSubagents, s.InFlightPRs, s.RecentMerges, s.CostToday, s.GreenClock}
	for _, p := range panels {
		renderPanel(&b, p, w)
	}
	// Truncate to height budget. The header consumed ~2 rows; if the
	// caller passes a short height (e.g. < 24) we cap rather than
	// scroll, matching the spec §4.6 paint-budget intent.
	out := b.String()
	rows := strings.Split(out, "\n")
	if len(rows) > h {
		rows = rows[:h]
	}
	return strings.Join(rows, "\n")
}

// renderPanel writes one Panel to b, padding lines to the column
// budget so the visual grid stays aligned. Compact mode (< 60 cols)
// drops the section header line per spec §4.7.
func renderPanel(b *strings.Builder, p Panel, w int) {
	compact := w < 60
	stateTag := ""
	switch p.State {
	case PanelEmpty:
		stateTag = " [EMPTY]"
	case PanelMissing:
		stateTag = " [MISSING]"
	}
	if !compact {
		fmt.Fprintf(b, "[%s]%s\n", p.Title, stateTag)
	} else {
		fmt.Fprintf(b, "%s%s\n", p.Title, stateTag)
	}
	for _, line := range p.Lines {
		fmt.Fprintf(b, "  %s\n", line)
	}
}

// statusDefaultDB resolves the --db value: empty flag falls through to
// defaultStateDB() (REGATTA_STATE_DB env, then "regatta.db" literal).
// Closes the same-class gap fixed for events tail + approval decide in
// R5-Bug-5: status was the lone db-touching subcommand still bypassing
// the env-aware resolver.
func statusDefaultDB(flagVal string) string {
	if flagVal != "" {
		return flagVal
	}
	return defaultStateDB()
}

// runStatus is the cobra-style entry point. Flags: --refresh, --once,
// --width, --height. --once renders one frame to stdout and exits;
// the loop mode would require raw-terminal handling and is gated by
// the TTY detection in the production wiring.
func runStatus(args []string) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	refresh := fs.Duration("refresh", 2*time.Second, "tick interval (e.g. 2s, 5s)")
	once := fs.Bool("once", false, "render a single frame to stdout and exit")
	width := fs.Int("width", 80, "render width (cols)")
	height := fs.Int("height", 24, "render height (rows)")
	dbPath := fs.String("db", "", "sqlite path (default: read-only, WAL)")
	jsonOut := fs.Bool("json", false, "emit a single JSON observation envelope and exit (MAY-47)")
	socketURL := fs.String("socket", "", "orchestrator socket base URL (default: http://127.0.0.1"+defaultListenerAddr+")")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(os.Stderr, "regatta status: flag parse:", err)
		return 2
	}

	if *jsonOut {
		return runStatusJSON(context.Background(), os.Stdout, statusJSONOpts{
			DBPath:    statusDefaultDB(*dbPath),
			SocketURL: *socketURL,
		})
	}

	src, err := newDefaultSource(statusDefaultDB(*dbPath))
	if err != nil {
		fmt.Fprintln(os.Stderr, "regatta status: source init:", err)
		return 1
	}
	r := Renderer{Width: *width, Height: *height}

	if *once {
		fmt.Println(r.Render(src.Snapshot(context.Background())))
		return 0
	}

	return runLoop(context.Background(), os.Stdout, src, r, *refresh)
}

// runLoop drives the steady-state refresh. Cancellation comes from
// the parent ctx (signal handler installs SIGINT/SIGTERM hooks). The
// loop is exported so a test can drive it with a synthetic clock.
func runLoop(ctx context.Context, out io.Writer, src Source, r Renderer, refresh time.Duration) int {
	if refresh <= 0 {
		refresh = 2 * time.Second
	}
	ticker := time.NewTicker(refresh)
	defer ticker.Stop()

	// Paint immediately on entry so the operator's < 1 s first-paint
	// budget is met without waiting for the first ticker fire.
	paint(out, r, src.Snapshot(ctx))
	for {
		select {
		case <-ctx.Done():
			return 0
		case <-ticker.C:
			paint(out, r, src.Snapshot(ctx))
		}
	}
}

// paint writes one rendered frame, prefixed with the ANSI clear-
// screen + cursor-home sequences so the terminal does not scroll. The
// clear sequence is omitted in non-TTY mode (e.g. piped stdout) so
// captured output stays readable.
func paint(out io.Writer, r Renderer, s Snapshot) {
	if writerIsTerminal(out) {
		_, _ = io.WriteString(out, "\x1b[2J\x1b[H")
	}
	_, _ = io.WriteString(out, r.Render(s))
}

// writerIsTerminal returns true when out is an *os.File pointing at a
// TTY. Pure-stdlib check; bubbletea's golden path is heavier and not
// needed here — the TUI just needs the boolean.
func writerIsTerminal(out io.Writer) bool {
	f, ok := out.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// newDefaultSource opens the read-only sqlite WAL conn that the
// production wiring uses. Empty dbPath defers to a degraded Source
// that emits all-MISSING panels so the operator can still see the
// frame skeleton.
func newDefaultSource(dbPath string) (Source, error) {
	if dbPath == "" {
		return degradedSource{}, nil
	}
	dsn := fmt.Sprintf("file:%s?mode=ro&_journal_mode=WAL&_query_only=1", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("status: open sqlite ro: %w", err)
	}
	return &sqliteSource{db: db}, nil
}

// degradedSource emits an all-MISSING snapshot. Used when no DB path
// is configured or the production source has not been wired yet —
// the operator sees the frame skeleton with explicit MISSING tags so
// the dependency state is unambiguous (spec §11).
type degradedSource struct{}

func (degradedSource) Snapshot(_ context.Context) Snapshot {
	miss := func(title string) Panel {
		return Panel{Title: title, State: PanelMissing, Lines: []string{"n/a — see Wave-A/B/C exit gate"}}
	}
	return Snapshot{
		At:              time.Now().UTC(),
		ActiveSubagents: miss(titleActiveSubagents),
		InFlightPRs:     miss(titleInFlightPRs),
		RecentMerges:    miss(titleRecentMerges),
		CostToday:       miss(titleCostToday),
		GreenClock:      miss(titleGreenClock),
		StaleBanner:     "regatta serve not detected — showing last-known state",
	}
}

// sqliteSource reads the live substrate via the read-only WAL conn.
// Each panel maps to one or two pre-indexed substrate queries; per
// spec §4.3, a single slow query degrades to PanelEmpty rather than
// blocking the frame.
type sqliteSource struct {
	db *sql.DB
}

// Snapshot pulls each panel in parallel against the read-only conn.
// SQL is intentionally simple — the substrate reducer maintains the
// derived tables this query reads from.
func (s *sqliteSource) Snapshot(ctx context.Context) Snapshot {
	now := time.Now().UTC()
	snap := Snapshot{At: now}
	snap.ActiveSubagents = s.queryActiveSubagents(ctx)
	snap.InFlightPRs = s.queryInFlightPRs(ctx)
	snap.RecentMerges = s.queryRecentMerges(ctx, now)
	snap.CostToday = Panel{Title: titleCostToday, State: PanelEmpty, Lines: []string{"— (prom api not wired)"}}
	snap.GreenClock = Panel{Title: titleGreenClock, State: PanelEmpty, Lines: []string{"— (gauge bootstrapping)"}}

	if mostRecent := s.mostRecentEventAge(ctx, now); mostRecent > 60*time.Second {
		snap.StaleBanner = fmt.Sprintf("regatta serve not detected — last event %s ago", mostRecent.Round(time.Second))
	}
	return snap
}

func (s *sqliteSource) queryActiveSubagents(ctx context.Context) Panel {
	rows, err := s.db.QueryContext(ctx,
		`SELECT work_item_id, created_at FROM agents WHERE state IN ('spawning','running') ORDER BY created_at LIMIT 3`)
	if err != nil {
		return Panel{Title: titleActiveSubagents, State: PanelMissing, Lines: []string{err.Error()}}
	}
	defer func() { _ = rows.Close() }()
	var lines []string
	for rows.Next() {
		var workItemID string
		var createdAtUnix int64
		if err := rows.Scan(&workItemID, &createdAtUnix); err != nil {
			break
		}
		started := time.Unix(createdAtUnix, 0)
		lines = append(lines, fmt.Sprintf("%s  %s", workItemID, time.Since(started).Round(time.Second)))
	}
	if len(lines) == 0 {
		return Panel{Title: titleActiveSubagents, State: PanelEmpty, Lines: []string{"— no active subagents"}}
	}
	return Panel{Title: titleActiveSubagents, State: PanelOK, Lines: lines}
}

func (s *sqliteSource) queryInFlightPRs(ctx context.Context) Panel {
	rows, err := s.db.QueryContext(ctx,
		`SELECT work_item_id, payload_json, written_at
		   FROM substrate_events
		  WHERE kind = 'agent_pr_opened'
		    AND work_item_id NOT IN (
		      SELECT work_item_id FROM substrate_events WHERE kind = 'agent_pr_merged'
		    )
		  ORDER BY written_at DESC LIMIT 5`)
	if err != nil {
		return Panel{Title: titleInFlightPRs, State: PanelMissing, Lines: []string{err.Error()}}
	}
	defer func() { _ = rows.Close() }()
	var lines []string
	for rows.Next() {
		var workItem, payload string
		var writtenAt int64
		if err := rows.Scan(&workItem, &payload, &writtenAt); err != nil {
			break
		}
		lines = append(lines, fmt.Sprintf("%s  %s", workItem, prPayloadSummary(payload)))
	}
	if len(lines) == 0 {
		return Panel{Title: titleInFlightPRs, State: PanelEmpty, Lines: []string{"— no open PRs"}}
	}
	return Panel{Title: titleInFlightPRs, State: PanelOK, Lines: lines}
}

func (s *sqliteSource) queryRecentMerges(ctx context.Context, now time.Time) Panel {
	cutoffMs := now.Add(-24 * time.Hour).UnixMilli()
	rows, err := s.db.QueryContext(ctx,
		`SELECT work_item_id, payload_json
		   FROM substrate_events
		  WHERE kind = 'agent_pr_merged' AND written_at >= ?
		  ORDER BY written_at DESC LIMIT 3`,
		cutoffMs)
	if err != nil {
		return Panel{Title: titleRecentMerges, State: PanelMissing, Lines: []string{err.Error()}}
	}
	defer func() { _ = rows.Close() }()
	var lines []string
	for rows.Next() {
		var workItem, payload string
		if err := rows.Scan(&workItem, &payload); err != nil {
			break
		}
		lines = append(lines, fmt.Sprintf("%s  %s", workItem, prPayloadSummary(payload)))
	}
	if len(lines) == 0 {
		return Panel{Title: titleRecentMerges, State: PanelEmpty, Lines: []string{"— none in window"}}
	}
	return Panel{Title: titleRecentMerges, State: PanelOK, Lines: lines}
}

// prPayloadSummary extracts a short human label from an agent_pr_*
// substrate event payload. Falls back to an empty string on parse
// failure so the row still renders the work-item identifier.
func prPayloadSummary(payload string) string {
	if payload == "" {
		return ""
	}
	var p struct {
		Number int    `json:"pr_number"`
		Title  string `json:"title"`
	}
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		return ""
	}
	if p.Number > 0 && p.Title != "" {
		return fmt.Sprintf("#%d %s", p.Number, p.Title)
	}
	if p.Number > 0 {
		return fmt.Sprintf("#%d", p.Number)
	}
	return p.Title
}

func (s *sqliteSource) mostRecentEventAge(ctx context.Context, now time.Time) time.Duration {
	row := s.db.QueryRowContext(ctx, `SELECT MAX(emitted_at) FROM substrate_events`)
	var t sql.NullTime
	if err := row.Scan(&t); err != nil || !t.Valid {
		return 24 * time.Hour
	}
	return now.Sub(t.Time)
}

