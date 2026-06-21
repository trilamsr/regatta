// Package lowrisk decides whether a PR is eligible for autonomous
// auto-merge (MAY-86). The decision is a single boolean — the W-spec's
// 4-tier model is deliberately collapsed to v1 (spec §10.5/§10.8). The
// real safety is the FIRST, UNCONDITIONAL load-bearing veto: anything
// touching the broader spec-§3 T3 surface is held for an operator
// glance, never auto-merged. LOC cap + stateless soak are secondary.
package lowrisk

import (
	"bufio"
	_ "embed"
	"path/filepath"
	"strings"
	"time"
)

// loadBearingList is the single physical source of truth for the veto.
// It is embedded here AND read by any shell consumer at the same
// repo-relative path, so the Go and shell views cannot drift — no parity
// gate needed (brief item 3, approach b).
//
//go:embed load-bearing-paths.txt
var loadBearingList string

// Reason tokens are stable strings the operator can grep in
// scheduler.gates_pass_held logs and the audit trail.
const (
	ReasonLoadBearing = "load_bearing_path"
	ReasonLOCOverCap  = "loc_over_cap"
	ReasonNotSoaked   = "soak_not_satisfied"
	ReasonEligible    = "eligible"
)

// PR is the value-object the classifier reasons over. It carries only
// what the decision needs — changed paths, total diff LOC, and the open
// time for the stateless soak — so the classifier stays a pure function
// with no GitHub or DB coupling.
type PR struct {
	ChangedPaths []string
	DiffLOC      int
	OpenedAt     time.Time
}

// Config tunes the secondary signals. Clock defaults to time.Now so
// soak math is testable by injection (mirrors scheduler.Config.Clock).
type Config struct {
	LOCCap     int
	HoldWindow time.Duration
	Clock      func() time.Time
}

// Classifier holds the parsed veto matchers + tunables. Construct once
// via New and reuse; Classify is pure and allocation-light.
type Classifier struct {
	cfg      Config
	matchers []matcher
}

// New parses the embedded load-bearing list once and returns a ready
// Classifier. A nil Clock falls back to time.Now.
func New(cfg Config) *Classifier {
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	return &Classifier{cfg: cfg, matchers: parseMatchers(loadBearingList)}
}

// Classify returns (eligible, reason). The load-bearing veto is the
// FIRST branch and runs UNCONDITIONALLY — it precedes the LOC and soak
// checks so a load-bearing PR is always held with reason
// "load_bearing_path", regardless of how small or how soaked it is. The
// secondary checks (LOC cap, stateless soak) only run once the veto
// clears.
func (c *Classifier) Classify(pr PR) (bool, string) {
	for _, p := range pr.ChangedPaths {
		if c.isLoadBearing(p) {
			return false, ReasonLoadBearing
		}
	}
	if pr.DiffLOC > c.cfg.LOCCap {
		return false, ReasonLOCOverCap
	}
	// Stateless, tick-derived soak: no DB column, no timer goroutine —
	// just compare at classify time. Inclusive at exactly HoldWindow.
	if c.cfg.Clock().Sub(pr.OpenedAt) < c.cfg.HoldWindow {
		return false, ReasonNotSoaked
	}
	return true, ReasonEligible
}

func (c *Classifier) isLoadBearing(path string) bool {
	clean := strings.TrimPrefix(filepath.ToSlash(path), "./")
	for _, m := range c.matchers {
		if m.matches(clean) {
			return true
		}
	}
	return false
}

// matcher is one parsed line from the embedded list. A dirPrefix (line
// ending in `/`) vetoes any path under it; a glob (line containing `*`)
// is matched with filepath.Match against the whole path; otherwise the
// line is an exact-file match.
type matcher struct {
	raw       string
	dirPrefix bool
	glob      bool
}

func (m matcher) matches(path string) bool {
	switch {
	case m.dirPrefix:
		return strings.HasPrefix(path, m.raw)
	case m.glob:
		ok, err := filepath.Match(m.raw, path)
		return err == nil && ok
	default:
		return path == m.raw
	}
}

// parseMatchers reads the embedded list: blank lines and `#` comments
// are skipped; a trailing `/` marks a directory prefix; a `*` marks a
// glob.
func parseMatchers(list string) []matcher {
	var out []matcher
	sc := bufio.NewScanner(strings.NewReader(list))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, matcher{
			raw:       line,
			dirPrefix: strings.HasSuffix(line, "/"),
			glob:      strings.Contains(line, "*"),
		})
	}
	return out
}
