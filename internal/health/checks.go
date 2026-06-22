// Package health provides the W3 /healthz probe + check primitives —
// JSON shape per spec §3.5, the supervisor-observable readiness surface.
package health

import (
	"context"
	"database/sql"
	"sync"
	"time"
)

// Status enumerates the three-state liveness signal supervisors and
// operators both read: ok > degraded > down (worst-wins aggregation).
type Status string

// Status values aggregated from per-check states; worst-wins.
const (
	StatusOK       Status = "ok"
	StatusDegraded Status = "degraded"
	StatusDown     Status = "down"
)

// Per-check state literals used in the JSON response.
const (
	checkOK      = "ok"
	checkDown    = "down"
	checkStale   = "stale"
	checkMissing = "missing"
)

// CheckResult is one row in the /healthz JSON checks map.
type CheckResult struct {
	Status    string `json:"status"`
	LatencyMS int64  `json:"latency_ms,omitempty"`
	AgeSec    int64  `json:"age_seconds,omitempty"`
	LoadedAt  string `json:"loaded_path,omitempty"`
	Error     string `json:"error,omitempty"`
}

// Response is the full /healthz JSON envelope per spec §3.5.
type Response struct {
	Status  Status                 `json:"status"`
	Version string                 `json:"version"`
	Checks  map[string]CheckResult `json:"checks"`
}

// dbPingTimeout caps the readiness DB ping — spec §3.5 row "db".
const dbPingTimeout = 500 * time.Millisecond

// heartbeatStaleThreshold marks heartbeat as stale — spec §3.5 row "heartbeat".
const heartbeatStaleThreshold = 60 * time.Second

// Probe runs all three subsystem checks and folds them into Response.
// Worst-wins: any down → down; otherwise any stale|missing → degraded.
func Probe(ctx context.Context, deps Dependencies) Response {
	r := Response{
		Version: deps.Version,
		Checks:  map[string]CheckResult{},
	}
	r.Checks["db"] = checkDB(ctx, deps.DB)
	r.Checks["heartbeat"] = checkHeartbeat(deps.Heartbeat)
	r.Checks["brief"] = checkBrief(deps.Brief)
	r.Status = aggregate(r.Checks)
	return r
}

// Dependencies bundles probe inputs so the handler stays one-arg.
type Dependencies struct {
	DB        *sql.DB
	Heartbeat *HeartbeatCell
	Brief     *BriefCell
	Version   string
	Now       func() time.Time
}

func checkDB(ctx context.Context, db *sql.DB) CheckResult {
	if db == nil {
		return CheckResult{Status: checkDown, Error: "db handle nil"}
	}
	pctx, cancel := context.WithTimeout(ctx, dbPingTimeout)
	defer cancel()
	start := time.Now()
	if err := db.PingContext(pctx); err != nil {
		return CheckResult{Status: checkDown, Error: err.Error()}
	}
	return CheckResult{Status: checkOK, LatencyMS: time.Since(start).Milliseconds()}
}

func checkHeartbeat(c *HeartbeatCell) CheckResult {
	if c == nil {
		return CheckResult{Status: checkStale, Error: "no heartbeat writer"}
	}
	age := c.Age()
	res := CheckResult{AgeSec: int64(age.Seconds())}
	if age > heartbeatStaleThreshold {
		res.Status = checkStale
		return res
	}
	res.Status = checkOK
	return res
}

func checkBrief(c *BriefCell) CheckResult {
	if c == nil {
		return CheckResult{Status: checkMissing, Error: "brief cell nil"}
	}
	path := c.Path()
	if path == "" {
		return CheckResult{Status: checkOK}
	}
	return CheckResult{Status: checkOK, LoadedAt: path}
}

// aggregate maps the per-check states to the overall Status.
func aggregate(checks map[string]CheckResult) Status {
	overall := StatusOK
	for _, c := range checks {
		switch c.Status {
		case checkDown:
			return StatusDown
		case checkStale, checkMissing:
			overall = StatusDegraded
		}
	}
	return overall
}

// HeartbeatCell is the in-memory mirror of the latest heartbeat write —
// the readiness probe avoids hitting SQLite for liveness staleness
// because the dedicated writer (spec §3.5) is the source of truth.
type HeartbeatCell struct {
	mu   sync.RWMutex
	last time.Time
	now  func() time.Time
}

// NewHeartbeatCell returns a cell with `now` injectable for tests.
func NewHeartbeatCell(now func() time.Time) *HeartbeatCell {
	if now == nil {
		now = time.Now
	}
	return &HeartbeatCell{now: now}
}

// Touch records a heartbeat — callers (the writer goroutine) invoke
// after each successful row insert.
func (h *HeartbeatCell) Touch() {
	h.mu.Lock()
	h.last = h.now()
	h.mu.Unlock()
}

// Age returns elapsed since last Touch; a never-touched cell reports
// MaxInt64 seconds so the aggregator always classifies it stale.
func (h *HeartbeatCell) Age() time.Duration {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.last.IsZero() {
		return 365 * 24 * time.Hour
	}
	return h.now().Sub(h.last)
}

// BriefCell mirrors the brief-loader path so the probe stays read-only.
type BriefCell struct {
	mu   sync.RWMutex
	path string
}

// NewBriefCell returns an empty cell — install before brief load fills it.
func NewBriefCell() *BriefCell { return &BriefCell{} }

// SetPath records the loaded brief path — empty string ⇒ missing.
func (b *BriefCell) SetPath(p string) {
	b.mu.Lock()
	b.path = p
	b.mu.Unlock()
}

// Path returns the loaded brief path; empty ⇒ not yet loaded.
func (b *BriefCell) Path() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.path
}
