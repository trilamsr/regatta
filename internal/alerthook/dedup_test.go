package alerthook

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestDedupCache_ExpiresAfterTTL asserts the 60s TTL kicks the entry so a long-quiet alertname re-hits the search API instead of serving stale state.
func TestDedupCache_ExpiresAfterTTL(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	clock := &fakeClock{t: now}
	cache := newDedupCache(clock.Now, 60*time.Second)
	cache.put("X", 7, true)

	clock.advance(59 * time.Second)
	if num, has, ok := cache.get("X"); !ok || !has || num != 7 {
		t.Fatalf("pre-expiry: got (num=%d has=%v ok=%v) want (7 true true)", num, has, ok)
	}

	clock.advance(2 * time.Second)
	if _, _, ok := cache.get("X"); ok {
		t.Fatalf("post-expiry: cache must miss but did not")
	}
}

// TestFindExistingIssue_CacheServesWithoutNetworkCall asserts a cache hit short-circuits the GH search call entirely.
func TestFindExistingIssue_CacheServesWithoutNetworkCall(t *testing.T) {
	fake := &fakeGitHub{}
	cache := newDedupCache(nil, 60*time.Second)
	cache.put("X", 99, true)

	num, has, err := findExistingIssue(context.Background(), fake, cache, "X")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !has || num != 99 {
		t.Fatalf("got (num=%d has=%v) want (99 true)", num, has)
	}
	if fake.ListCalls != 0 {
		t.Fatalf("list calls: got %d want 0 (cache must serve without network)", fake.ListCalls)
	}
}

// TestFindExistingIssue_PropagatesGHError asserts a GH search failure surfaces as an error rather than a silent zero-issue answer.
func TestFindExistingIssue_PropagatesGHError(t *testing.T) {
	fake := &fakeGitHub{ListErr: errors.New("rate limited")}
	num, has, err := findExistingIssue(context.Background(), fake, nil, "X")
	if err == nil {
		t.Fatalf("want error; got (num=%d has=%v err=nil)", num, has)
	}
}

type fakeClock struct {
	t time.Time
}

func (c *fakeClock) Now() time.Time         { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }
