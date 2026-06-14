package calendar

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeFetcher struct {
	calls atomic.Int64
	cal   Calendar
	err   error
	gate  chan struct{} // if non-nil, Fetch blocks until closed
}

func (f *fakeFetcher) Fetch(_ context.Context) (Calendar, error) {
	f.calls.Add(1)
	if f.gate != nil {
		<-f.gate
	}
	return f.cal, f.err
}

func TestCache_servesFreshWithoutRefetch(t *testing.T) {
	f := &fakeFetcher{cal: Calendar{Events: []VEvent{{UID: "a"}}}}
	c := NewCache(f, time.Minute)

	if _, err := c.Get(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Get(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := f.calls.Load(); got != 1 {
		t.Errorf("fetch called %d times within TTL, want 1", got)
	}
}

func TestCache_refetchesAfterTTL(t *testing.T) {
	f := &fakeFetcher{cal: Calendar{Events: []VEvent{{UID: "a"}}}}
	c := NewCache(f, time.Minute)

	now := time.Date(2026, 6, 14, 8, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }

	_, _ = c.Get(context.Background())
	now = now.Add(2 * time.Minute) // past TTL
	_, _ = c.Get(context.Background())

	if got := f.calls.Load(); got != 2 {
		t.Errorf("fetch called %d times, want 2 after TTL expiry", got)
	}
}

func TestCache_errorsNotCached(t *testing.T) {
	f := &fakeFetcher{err: errors.New("boom")}
	c := NewCache(f, time.Minute)

	if _, err := c.Get(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	if _, err := c.Get(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	if got := f.calls.Load(); got != 2 {
		t.Errorf("errors should not be cached: fetch called %d times, want 2", got)
	}
}

func TestCache_singleFlight(t *testing.T) {
	f := &fakeFetcher{cal: Calendar{Events: []VEvent{{UID: "a"}}}, gate: make(chan struct{})}
	c := NewCache(f, time.Minute)

	const n = 25
	var wg sync.WaitGroup
	wg.Add(n)
	for range n {
		go func() {
			defer wg.Done()
			_, _ = c.Get(context.Background())
		}()
	}
	// Give the goroutines a moment to coalesce, then release the fetch.
	time.Sleep(20 * time.Millisecond)
	close(f.gate)
	wg.Wait()

	if got := f.calls.Load(); got != 1 {
		t.Errorf("single-flight failed: %d upstream calls, want 1", got)
	}
}

func TestCache_ctxCancelWhileWaiting(t *testing.T) {
	f := &fakeFetcher{gate: make(chan struct{})}
	c := NewCache(f, time.Minute)

	// First caller occupies the in-flight slot and blocks on the gate.
	go func() { _, _ = c.Get(context.Background()) }()
	time.Sleep(20 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Get(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("waiting caller err = %v, want context.Canceled", err)
	}
	close(f.gate)
}
