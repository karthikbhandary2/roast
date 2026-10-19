package middleware

import (
	"context"
	"sync"
	"time"
)

// RevocationChecker reports the "tokens issued before this instant are void"
// cutoff for a user (users.tokens_valid_after in User Service), or nil when
// the user has none. AuthUnaryInterceptor consults it for seller/admin tokens.
type RevocationChecker interface {
	TokensValidAfter(ctx context.Context, userID string) (*time.Time, error)
}

const maxCacheEntries = 10000

type cacheEntry struct {
	cutoff  *time.Time
	expires time.Time
}

// CachedRevocationChecker wraps a loader (a database read in User Service; a
// GetUser RPC in other services) with a small in-process TTL cache, so a
// privileged request normally costs one map lookup rather than a round trip.
//
// Staleness is bounded by the TTL. The service that records a revocation
// should call Invalidate right after, which makes it take effect immediately
// on that instance; other instances catch up within the TTL. (A request that is
// already loading when Invalidate runs can re-cache the old value, but only
// until the TTL expires.)
type CachedRevocationChecker struct {
	load func(ctx context.Context, userID string) (*time.Time, error)
	ttl  time.Duration

	mu sync.Mutex
	m  map[string]cacheEntry
}

func NewCachedRevocationChecker(
	load func(ctx context.Context, userID string) (*time.Time, error), ttl time.Duration,
) *CachedRevocationChecker {
	return &CachedRevocationChecker{load: load, ttl: ttl, m: make(map[string]cacheEntry)}
}

func (c *CachedRevocationChecker) TokensValidAfter(ctx context.Context, userID string) (*time.Time, error) {
	now := time.Now()

	c.mu.Lock()
	if e, ok := c.m[userID]; ok && now.Before(e.expires) {
		c.mu.Unlock()
		return e.cutoff, nil
	}
	c.mu.Unlock()

	cutoff, err := c.load(ctx, userID)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	if len(c.m) >= maxCacheEntries { // keep memory bounded: sweep expired entries
		for k, e := range c.m {
			if !now.Before(e.expires) {
				delete(c.m, k)
			}
		}
	}
	c.m[userID] = cacheEntry{cutoff: cutoff, expires: now.Add(c.ttl)}
	c.mu.Unlock()
	return cutoff, nil
}

// Invalidate drops a user's cached cutoff so the next check reloads it.
func (c *CachedRevocationChecker) Invalidate(userID string) {
	c.mu.Lock()
	delete(c.m, userID)
	c.mu.Unlock()
}