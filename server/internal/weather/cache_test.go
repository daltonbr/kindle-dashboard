package weather

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeFetcher counts calls and returns a programmable result. Each call may
// block until release is signalled, if blocking is enabled — useful for
// driving single-flight tests deterministically.
type fakeFetcher struct {
	mu       sync.Mutex
	calls    atomic.Int32
	forecast Forecast
	err      error
	block    chan struct{} // if non-nil, Fetch blocks on it
}

func (f *fakeFetcher) Fetch(ctx context.Context, _ float64, _ float64) (Forecast, error) {
	f.calls.Add(1)
	f.mu.Lock()
	block := f.block
	fc := f.forecast
	err := f.err
	f.mu.Unlock()
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return Forecast{}, ctx.Err()
		}
	}
	return fc, err
}

func sampleForecast(temp float64) Forecast {
	return Forecast{
		Now:       CurrentReading{TempC: temp},
		HighToday: temp + 2,
		LowToday:  temp - 2,
	}
}

func TestCache_SecondGetWithinTTLDoesNotFetch(t *testing.T) {
	f := &fakeFetcher{forecast: sampleForecast(20)}
	c := NewCache(f, 0, 0, time.Minute)

	a, err := c.Get(context.Background())
	if err != nil {
		t.Fatalf("first Get: %v", err)
	}
	b, err := c.Get(context.Background())
	if err != nil {
		t.Fatalf("second Get: %v", err)
	}
	if a.Now.TempC != 20 || b.Now.TempC != 20 {
		t.Errorf("unexpected values: a=%v b=%v", a, b)
	}
	if got := f.calls.Load(); got != 1 {
		t.Errorf("fetcher calls = %d, want 1", got)
	}
}

func TestCache_RefetchesAfterTTLExpiry(t *testing.T) {
	f := &fakeFetcher{forecast: sampleForecast(20)}
	c := NewCache(f, 0, 0, time.Minute)

	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return clock }

	if _, err := c.Get(context.Background()); err != nil {
		t.Fatalf("Get #1: %v", err)
	}
	if _, err := c.Get(context.Background()); err != nil {
		t.Fatalf("Get #2: %v", err)
	}
	if got := f.calls.Load(); got != 1 {
		t.Fatalf("after 2 fresh Gets, calls = %d, want 1", got)
	}

	// Advance clock past TTL.
	clock = clock.Add(2 * time.Minute)
	f.mu.Lock()
	f.forecast = sampleForecast(25)
	f.mu.Unlock()

	got, err := c.Get(context.Background())
	if err != nil {
		t.Fatalf("Get #3: %v", err)
	}
	if got.Now.TempC != 25 {
		t.Errorf("expected refresh to 25, got %v", got.Now.TempC)
	}
	if calls := f.calls.Load(); calls != 2 {
		t.Errorf("fetcher calls = %d, want 2", calls)
	}
}

func TestCache_ErrorsAreNotCached(t *testing.T) {
	wantErr := errors.New("boom")
	f := &fakeFetcher{err: wantErr}
	c := NewCache(f, 0, 0, time.Minute)

	if _, err := c.Get(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("Get #1 err = %v, want %v", err, wantErr)
	}
	if _, err := c.Get(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("Get #2 err = %v, want %v", err, wantErr)
	}
	if got := f.calls.Load(); got != 2 {
		t.Errorf("errors should not be cached; calls = %d, want 2", got)
	}
}

func TestCache_SingleFlightConcurrentColdStart(t *testing.T) {
	release := make(chan struct{})
	f := &fakeFetcher{forecast: sampleForecast(20), block: release}
	c := NewCache(f, 0, 0, time.Minute)

	const goroutines = 50
	var wg sync.WaitGroup
	results := make([]Forecast, goroutines)
	errs := make([]error, goroutines)

	for i := range goroutines {
		wg.Go(func() {
			results[i], errs[i] = c.Get(context.Background())
		})
	}

	// Give all goroutines time to queue up on the inflight pending.
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	if got := f.calls.Load(); got != 1 {
		t.Errorf("with %d concurrent cold-start Gets, fetcher calls = %d, want 1", goroutines, got)
	}
	for i, r := range results {
		if errs[i] != nil {
			t.Errorf("goroutine %d err: %v", i, errs[i])
		}
		if r.Now.TempC != 20 {
			t.Errorf("goroutine %d temp = %v, want 20", i, r.Now.TempC)
		}
	}
}

func TestCache_ContextCancelWhileWaitingOnInflight(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	f := &fakeFetcher{forecast: sampleForecast(20), block: release}
	c := NewCache(f, 0, 0, time.Minute)

	// Kick off a leader that will block.
	leaderDone := make(chan struct{})
	go func() {
		_, _ = c.Get(context.Background())
		close(leaderDone)
	}()

	// Give the leader time to start its fetch.
	time.Sleep(20 * time.Millisecond)

	// Waiter with a soon-to-cancel context.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := c.Get(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("waiter err = %v, want context.DeadlineExceeded", err)
	}
}

func TestCache_TTLZeroDisablesCaching(t *testing.T) {
	f := &fakeFetcher{forecast: sampleForecast(20)}
	c := NewCache(f, 0, 0, 0)

	for range 3 {
		if _, err := c.Get(context.Background()); err != nil {
			t.Fatalf("Get: %v", err)
		}
	}
	if got := f.calls.Load(); got != 3 {
		t.Errorf("with ttl=0, fetcher calls = %d, want 3", got)
	}
}
