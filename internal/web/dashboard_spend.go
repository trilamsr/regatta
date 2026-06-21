package web

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"strings"
	"time"

	"github.com/trilamsr/regatta/internal/cost/spend"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

func loadSpendView(ctx context.Context, deps Dependencies) any {
	view := dashboardSpendView{}
	if deps.DB == nil {
		return view
	}
	reader := spend.NewReader(deps.DB.SQL(), deps.Clock)
	now := deps.Clock()
	last24, err := reader.RecordedUSDForWindow(ctx, "default", now.Add(-dashboardLast24hWindow*time.Hour), now)
	if err != nil {
		view.Err = err.Error()
		return view
	}
	view.Last24hMicros = int64(last24)
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	today, err := reader.RecordedUSDForWindow(ctx, "default", todayStart, now)
	if err != nil {
		view.Err = err.Error()
		return view
	}
	view.TodayMicros = int64(today)
	lifetime, err := reader.RecordedUSDForWindow(ctx, "default", time.Unix(0, 0), now)
	if err != nil {
		view.Err = err.Error()
		return view
	}
	view.LifetimeMicros = int64(lifetime)
	view.Spark = buildSparkSeries(ctx, reader, now)
	if view.Last24hMicros == 0 && view.TodayMicros == 0 && view.LifetimeMicros == 0 {
		annotateSpendEmptyReason(ctx, deps.DB, &view, now)
	}
	return view
}

// annotateSpendEmptyReason scans events from the last 24h for agent.exited rows whose payload exit_reason != "completed" so the all-zero spend panel reads as "agents exited before reporting usage" instead of "spend tracker broken". The exit_reason=provider_credit_exhausted count is surfaced separately because it is the dominant cause when the operator's API key is missing or rate-limited.
func annotateSpendEmptyReason(ctx context.Context, db *state.DB, view *dashboardSpendView, now time.Time) {
	since := now.Add(-dashboardLast24hWindow * time.Hour).Unix()
	rows, err := db.SQL().QueryContext(ctx,
		`SELECT payload_json FROM events WHERE kind = ? AND created_at >= ?`,
		"agent.exited", since)
	if err != nil {
		return
	}
	defer func() { _ = rows.Close() }()
	var exitedNonCompleted, credit int
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			continue
		}
		var p struct {
			ExitReason string `json:"exit_reason"`
		}
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			continue
		}
		if p.ExitReason != "" && p.ExitReason != exitReasonCompleted {
			exitedNonCompleted++
		}
		if p.ExitReason == "provider_credit_exhausted" {
			credit++
		}
	}
	if exitedNonCompleted > 0 {
		view.EmptyReason = "agents exited before reporting usage (likely provider auth)"
		view.CreditExhaustedCount = credit
	}
}

// buildSparkSeries fires one RecordedUSDForWindow per histogram bucket because spend.Reader does not (yet) expose a batched window query. 12 buckets × 30s poll = 24 queries/min — each is a small SUM that hits the same composite index, well under sqlite's local-disk cap. A batched API + memo cache lands when spend.Reader gains a window-iterator surface; tracking issue is left to the spend pkg owner since the dashboard is read-side only.
func buildSparkSeries(ctx context.Context, reader *spend.Reader, now time.Time) []int64 {
	out := make([]int64, dashboardSparkBuckets)
	step := dashboardLast24hWindow * time.Hour / dashboardSparkBuckets
	for i := 0; i < dashboardSparkBuckets; i++ {
		end := now.Add(time.Duration(-i) * step)
		start := end.Add(-step)
		v, err := reader.RecordedUSDForWindow(ctx, "default", start, end)
		if err == nil {
			out[dashboardSparkBuckets-1-i] = int64(v)
		}
	}
	return out
}

// sparkSVG renders a 24h spend histogram as a tiny inline SVG so the spend panel carries a trend signal without a charting dep. Empty / zero series collapses to a flat baseline so the metric reads "—" visually instead of confusing the operator with a flat-line-at-zero overlay.
//
//nolint:gosec // returned HTML wraps fmt.Sprintf'd static SVG with %d/%.1f-formatted numeric coords — no caller-controlled string interpolation
func sparkSVG(series []int64) template.HTML {
	if len(series) == 0 {
		return template.HTML(fmt.Sprintf(`<svg class="spark" width="%d" height="%d"></svg>`, dashboardSparkWidth, dashboardSparkHeight))
	}
	var maxv int64
	for _, v := range series {
		if v > maxv {
			maxv = v
		}
	}
	if maxv == 0 {
		return template.HTML(fmt.Sprintf(`<svg class="spark" width="%d" height="%d"><line x1="0" y1="%d" x2="%d" y2="%d" stroke="#c8c3b6"/></svg>`, dashboardSparkWidth, dashboardSparkHeight, dashboardSparkBarMaxH, dashboardSparkWidth, dashboardSparkBarMaxH))
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<svg class="spark" width="%d" height="%d" viewBox="0 0 %d %d" preserveAspectRatio="none">`, dashboardSparkWidth, dashboardSparkHeight, dashboardSparkWidth, dashboardSparkHeight)
	step := float64(dashboardSparkWidth) / float64(len(series))
	for i, v := range series {
		h := float64(v) / float64(maxv) * float64(dashboardSparkBarMaxH)
		x := float64(i) * step
		y := float64(dashboardSparkBaseline) - h
		_, _ = fmt.Fprintf(&b, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="#c2410c"/>`, x, y, step-1, h)
	}
	b.WriteString(`</svg>`)
	return template.HTML(b.String())
}
