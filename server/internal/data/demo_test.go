package data

import (
	"context"
	"testing"
	"time"
)

func TestDemoModel_shape(t *testing.T) {
	ref := time.Date(2026, 6, 14, 9, 30, 0, 0, time.UTC)
	m := DemoModel(ref)

	if len(m.Days) < 3 {
		t.Fatalf("Days = %d, want >= 3 for the forecast card", len(m.Days))
	}
	if len(m.Hourly) != 24 {
		t.Fatalf("Hourly = %d, want 24", len(m.Hourly))
	}

	// Hourly samples must be chronological and one hour apart.
	for i := 1; i < len(m.Hourly); i++ {
		if !m.Hourly[i].Time.After(m.Hourly[i-1].Time) {
			t.Fatalf("Hourly not ascending at %d", i)
		}
	}

	// Today() is the first day and carries a precip chance the rain card needs.
	if got := m.Today(); got != m.Days[0] {
		t.Errorf("Today() = %+v, want Days[0] %+v", got, m.Days[0])
	}

	// Precip chances must be valid percentages.
	for i, h := range m.Hourly {
		if h.PrecipChance < 0 || h.PrecipChance > 100 {
			t.Errorf("Hourly[%d] precip chance %d out of range", i, h.PrecipChance)
		}
	}
}

func TestDemoModel_emptyToday(t *testing.T) {
	if got := (WeatherModel{}).Today(); got != (DayOutlook{}) {
		t.Errorf("Today() on empty model = %+v, want zero", got)
	}
}

func TestDemoWeather_provider(t *testing.T) {
	m, err := DemoWeather{}.Weather(context.Background())
	if err != nil {
		t.Fatalf("DemoWeather returned error: %v", err)
	}
	if len(m.Hourly) == 0 {
		t.Error("DemoWeather produced no hourly data")
	}
}
