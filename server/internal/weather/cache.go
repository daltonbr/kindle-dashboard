package weather

import (
	"context"
	"sync"
	"time"
)

// Fetcher is the dependency the Cache wraps. *Client satisfies it.
type Fetcher interface {
	Fetch(ctx context.Context, lat, lon float64) (Forecast, error)
}

// Cache is a single-entry TTL cache around a Fetcher for one (lat, lon) location.
//
// Concurrent Gets coalesce: at most one upstream fetch is in flight at a time;
// all waiters see the same result. Errors are not cached — the next Get will
// retry. Successful results are kept for `ttl` (measured from the moment of
// the successful fetch).
type Cache struct {
	fetcher  Fetcher
	lat, lon float64
	ttl      time.Duration

	// now is injectable for tests; defaults to time.Now.
	now func() time.Time

	mu       sync.Mutex
	value    Forecast
	valueAt  time.Time // zero if nothing cached yet
	inflight *pending
}

type pending struct {
	done     chan struct{}
	forecast Forecast
	err      error
}

// NewCache wraps fetcher with a TTL cache pinned to (lat, lon).
// Pass ttl <= 0 to disable caching entirely (every Get refetches).
func NewCache(fetcher Fetcher, lat, lon float64, ttl time.Duration) *Cache {
	return &Cache{
		fetcher: fetcher,
		lat:     lat,
		lon:     lon,
		ttl:     ttl,
		now:     time.Now,
	}
}

// Get returns the cached forecast if fresh, otherwise fetches one. Concurrent
// callers during a fetch wait for that fetch's result instead of stampeding.
func (c *Cache) Get(ctx context.Context) (Forecast, error) {
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
			return p.forecast, p.err
		case <-ctx.Done():
			return Forecast{}, ctx.Err()
		}
	}

	p := &pending{done: make(chan struct{})}
	c.inflight = p
	c.mu.Unlock()

	forecast, err := c.fetcher.Fetch(ctx, c.lat, c.lon)

	c.mu.Lock()
	if err == nil {
		c.value = forecast
		c.valueAt = c.now()
	}
	p.forecast = forecast
	p.err = err
	c.inflight = nil
	c.mu.Unlock()
	close(p.done)

	return forecast, err
}

// fresh reports whether the cached value is non-empty and within ttl.
// Caller must hold c.mu.
func (c *Cache) fresh() bool {
	if c.valueAt.IsZero() || c.ttl <= 0 {
		return false
	}
	return c.now().Sub(c.valueAt) < c.ttl
}
