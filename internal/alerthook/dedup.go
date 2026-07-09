package alerthook

import (
	"context"
	"sync"
	"time"

	"github.com/trilamsr/regatta/internal/ghclient"
)

// dedupLabel is the constant GitHub label every receiver-managed issue
// carries. The search query narrows on it before the in:title alertname
// filter, so an unrelated issue whose title happens to contain the
// alertname is never mistaken for a dedup target.
const dedupLabel = "obs-alert"

// autonomousLabel routes the issue into the autonomous-loop work queue.
// W4 (self-improvement) and W3 (supervisor) consume this label to
// distinguish operator-filed issues from receiver-filed ones.
const autonomousLabel = "autonomous"

// dedupCache memoises alertname → issueNumber for cacheTTL to absorb
// alert-storm bursts without hammering the search API. ResolveExisting
// reads the cache first; a miss falls through to the live GitHub call,
// and a hit short-circuits without a network round-trip.
type dedupCache struct {
	mu      sync.Mutex
	entries map[string]dedupEntry
	now     func() time.Time
	ttl     time.Duration
}

type dedupEntry struct {
	number   int
	hasIssue bool
	at       time.Time
}

func newDedupCache(now func() time.Time, ttl time.Duration) *dedupCache {
	if now == nil {
		now = time.Now
	}
	if ttl <= 0 {
		ttl = 60 * time.Second
	}
	return &dedupCache{
		entries: make(map[string]dedupEntry),
		now:     now,
		ttl:     ttl,
	}
}

func (d *dedupCache) get(alertname string) (int, bool, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	e, ok := d.entries[alertname]
	if !ok {
		return 0, false, false
	}
	if d.now().Sub(e.at) > d.ttl {
		delete(d.entries, alertname)
		return 0, false, false
	}
	return e.number, e.hasIssue, true
}

func (d *dedupCache) put(alertname string, number int, hasIssue bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.entries[alertname] = dedupEntry{number: number, hasIssue: hasIssue, at: d.now()}
}

// findExistingIssue checks the 60s cache first, then asks GitHub. First
// match wins (oldest open issue) — `is:open` already filters by state
// and Search/Issues returns results ranked best-match-first; the
// receiver explicitly takes index 0 to make the rule one line.
func findExistingIssue(ctx context.Context, client ghclient.Client, cache *dedupCache, alertname string) (int, bool, error) {
	if cache != nil {
		if num, has, ok := cache.get(alertname); ok {
			return num, has, nil
		}
	}
	issues, err := client.ListOpenIssuesByLabel(ctx, dedupLabel, alertname)
	if err != nil {
		return 0, false, err
	}
	if len(issues) == 0 {
		if cache != nil {
			cache.put(alertname, 0, false)
		}
		return 0, false, nil
	}
	num := issues[0].Number
	if cache != nil {
		cache.put(alertname, num, true)
	}
	return num, true, nil
}
