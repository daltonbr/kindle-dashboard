package weather

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFetch_DecodesBrightonFixture(t *testing.T) {
	fixture := readFixture(t, "brighton.json")

	var capturedQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, srv.Client())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	fc, err := client.Fetch(ctx, 50.8225, -0.1372)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	for _, want := range []string{
		"latitude=50.8225",
		"longitude=-0.1372",
		"current=temperature_2m%2Cweather_code",
		"daily=temperature_2m_max%2Ctemperature_2m_min",
		"hourly=temperature_2m",
		"timezone=auto",
		"forecast_days=2",
	} {
		if !strings.Contains(capturedQuery, want) {
			t.Errorf("query missing %q; got %q", want, capturedQuery)
		}
	}

	if fc.Now.TempC != 30.1 {
		t.Errorf("Now.TempC = %v, want 30.1", fc.Now.TempC)
	}
	if fc.Now.WeatherCode != 0 {
		t.Errorf("Now.WeatherCode = %v, want 0", fc.Now.WeatherCode)
	}
	wantNow := time.Date(2026, 5, 25, 16, 0, 0, 0, time.UTC)
	if !fc.Now.Time.Equal(wantNow) {
		t.Errorf("Now.Time = %v, want %v", fc.Now.Time, wantNow)
	}
	if fc.HighToday != 30.5 {
		t.Errorf("HighToday = %v, want 30.5", fc.HighToday)
	}
	if fc.LowToday != 18.5 {
		t.Errorf("LowToday = %v, want 18.5", fc.LowToday)
	}
	if got := len(fc.Next24h); got != 24 {
		t.Fatalf("len(Next24h) = %d, want 24", got)
	}
	// First Next24h entry should be the same hour as Now (16:00 on the 25th).
	if !fc.Next24h[0].Time.Equal(wantNow) {
		t.Errorf("Next24h[0].Time = %v, want %v", fc.Next24h[0].Time, wantNow)
	}
	// Last Next24h entry should be 23h after that (15:00 next day).
	wantLast := wantNow.Add(23 * time.Hour)
	if !fc.Next24h[23].Time.Equal(wantLast) {
		t.Errorf("Next24h[23].Time = %v, want %v", fc.Next24h[23].Time, wantLast)
	}
	if fc.FetchedAt.IsZero() {
		t.Error("FetchedAt should be set")
	}
}

func TestFetch_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, srv.Client())
	_, err := client.Fetch(context.Background(), 0, 0)
	if err == nil {
		t.Fatal("expected error for non-200 response")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error should mention status code; got: %v", err)
	}
}

func TestFetch_BadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("{not json"))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, srv.Client())
	_, err := client.Fetch(context.Background(), 0, 0)
	if err == nil {
		t.Fatal("expected decode error")
	}
}

func TestFetch_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, srv.Client())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	if _, err := client.Fetch(ctx, 0, 0); err == nil {
		t.Fatal("expected context-cancellation error")
	}
}

func TestSliceNext24_TailWindow(t *testing.T) {
	base := time.Date(2026, 5, 25, 0, 0, 0, 0, time.UTC)
	hourly := make([]HourlyReading, 48)
	for i := range hourly {
		hourly[i] = HourlyReading{Time: base.Add(time.Duration(i) * time.Hour), TempC: float64(i)}
	}

	// Anchor at hour 40 → only 8 hourly entries remain.
	out := sliceNext24(hourly, base.Add(40*time.Hour))
	if len(out) != 8 {
		t.Errorf("len = %d, want 8 (truncated window)", len(out))
	}
}

func TestSliceNext24_AnchorAfterAll(t *testing.T) {
	base := time.Date(2026, 5, 25, 0, 0, 0, 0, time.UTC)
	hourly := []HourlyReading{
		{Time: base, TempC: 1},
	}
	if got := sliceNext24(hourly, base.Add(time.Hour)); got != nil {
		t.Errorf("expected nil when anchor is past all entries; got %v", got)
	}
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return b
}
