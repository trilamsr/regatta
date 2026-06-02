package l4

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"sync"

	"github.com/trilamsr/regatta/contracts/schemas"
)

// DefaultCacheCapacity bounds the in-memory findings cache. Per
// issue #357: re-runs at the same (diff, spec, model) reuse the
// prior findings instead of re-invoking the LLM. The working set is
// small in practice -- CI flakes and scheduler-tick re-evals retry
// the same key a handful of times -- so a 64-entry LRU saturates
// realistic traffic without unbounded memory growth.
//
// In-memory was chosen over substrate persistence (per
// feedback_decision_priority): the cache is a cost optimization,
// not a correctness gate. A miss after restart still produces the
// correct verdict; the only impact is one extra model call per
// (PR SHA, prompt SHA) per process lifetime -- substrate dep +
// migration risk for ~0 incremental UX win.
const DefaultCacheCapacity = 64

// NewCachedInvoker wraps base with an LRU memoizing successful
// responses keyed on sha256(diff || spec || model). Capacity <= 0
// disables caching (returns base unchanged) so the wrapper is safe
// to compose unconditionally.
//
// Only successful invocations cache. Errors propagate uncached so
// a transient model outage does not poison subsequent runs.
func NewCachedInvoker(base Invoker, capacity int) Invoker {
	if capacity <= 0 || base == nil {
		return base
	}
	c := &invokerCache{base: base, cap: capacity, entries: map[string]*list.Element{}, order: list.New()}
	return c.invoke
}

type cacheEntry struct {
	key  string
	resp InvokeResponse
}

type invokerCache struct {
	base    Invoker
	cap     int
	mu      sync.Mutex
	entries map[string]*list.Element
	order   *list.List
}

func (c *invokerCache) invoke(ctx context.Context, req InvokeRequest) (InvokeResponse, error) {
	key := cacheKey(req)
	if hit, ok := c.get(key); ok {
		return hit, nil
	}
	resp, err := c.base(ctx, req)
	if err != nil {
		return resp, err
	}
	c.put(key, resp)
	return resp, nil
}

func (c *invokerCache) get(key string) (InvokeResponse, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.entries[key]
	if !ok {
		return InvokeResponse{}, false
	}
	c.order.MoveToFront(el)
	entry, _ := el.Value.(*cacheEntry)
	return entry.resp, true
}

func (c *invokerCache) put(key string, resp InvokeResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.entries[key]; ok {
		entry, _ := el.Value.(*cacheEntry)
		entry.resp = cloneResponse(resp)
		c.order.MoveToFront(el)
		return
	}
	el := c.order.PushFront(&cacheEntry{key: key, resp: cloneResponse(resp)})
	c.entries[key] = el
	for c.order.Len() > c.cap {
		oldest := c.order.Back()
		if oldest == nil {
			return
		}
		c.order.Remove(oldest)
		entry, _ := oldest.Value.(*cacheEntry)
		delete(c.entries, entry.key)
	}
}

// cacheKey hashes the (diff, spec, model) tuple. PRSHA + RunID are
// deliberately excluded: re-runs at distinct PR heads on identical
// diff+spec (cherry-pick, rebase-no-op, retry) should hit. PromptSHA
// is folded in too once the model adapter resolves it -- callers may
// override by stamping a sentinel byte into Input.Spec if isolation
// is required.
func cacheKey(req InvokeRequest) string {
	h := sha256.New()
	// Length-prefixed separators avoid (diff="AB", spec="C") colliding
	// with (diff="A", spec="BC").
	writeField(h, req.Input.Diff)
	writeField(h, req.Input.Spec)
	writeField(h, req.Model)
	sum := h.Sum(nil)
	return hex.EncodeToString(sum)
}

func writeField(h interface {
	Write([]byte) (int, error)
}, s string) {
	var lp [8]byte
	binary.LittleEndian.PutUint64(lp[:], uint64(len(s)))
	_, _ = h.Write(lp[:])
	_, _ = h.Write([]byte(s))
}

// cloneResponse deep-copies findings so cache hits cannot be mutated
// by callers that append to gr.Findings after Run returns.
func cloneResponse(r InvokeResponse) InvokeResponse {
	if len(r.Findings) == 0 {
		return r
	}
	out := r
	out.Findings = make([]schemas.Finding, len(r.Findings))
	copy(out.Findings, r.Findings)
	return out
}
