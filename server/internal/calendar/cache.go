package calendar

import (
	"context"
	"sync"
	"time"
)

// Fetcher is the dependency the Cache wraps. *Client satisfies it.
type Fetcher interface {
	Fetch(ctx context.Context) (Calendar, error)
}

// Cache is a single-entry TTL cache around a Fetcher, mirroring
// weather.Cache: concurrent Gets coalesce onto one in-flight fetch, errors are
// not cached, and a successful result is kept for ttl from the fetch instant.
//
// It caches the *parsed feed*, not materialised occurrences — expansion depends
// on "now" and is cheap, so callers expand the cached Calendar per request.
type Cache struct {
	fetcher Fetcher
	ttl     time.Duration

	now func() time.Time // injectable for tests

	mu       sync.Mutex
	value    Calendar
	valueAt  time.Time
	inflight *pending
}

type pending struct {
	done chan struct{}
	cal  Calendar
	err  error
}

// NewCache wraps fetcher with a TTL cache. ttl <= 0 disables caching.
func NewCache(fetcher Fetcher, ttl time.Duration) *Cache {
	return &Cache{fetcher: fetcher, ttl: ttl, now: time.Now}
}

// Get returns the cached calendar if fresh, otherwise fetches one. Concurrent
// callers during a fetch wait for that fetch's result instead of stampeding.
func (c *Cache) Get(ctx context.Context) (Calendar, error) {
	c.mu.Lock()

	if c.fresh() {
		v := c.value
		c.mu.Unlock()
		return v, nil
	}

	if c.inflight != nil {
		p := c.inflight
		c.mu.Unlock()
		select {
		case <-p.done:
			return p.cal, p.err
		case <-ctx.Done():
			return Calendar{}, ctx.Err()
		}
	}

	p := &pending{done: make(chan struct{})}
	c.inflight = p
	c.mu.Unlock()

	cal, err := c.fetcher.Fetch(ctx)

	c.mu.Lock()
	if err == nil {
		c.value = cal
		c.valueAt = c.now()
	}
	p.cal = cal
	p.err = err
	c.inflight = nil
	c.mu.Unlock()
	close(p.done)

	return cal, err
}

// fresh reports whether the cached value is set and within ttl.
// Caller must hold c.mu.
func (c *Cache) fresh() bool {
	if c.valueAt.IsZero() || c.ttl <= 0 {
		return false
	}
	return c.now().Sub(c.valueAt) < c.ttl
}
