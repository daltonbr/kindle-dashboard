package data

import (
	"context"
	"testing"
	"time"

	"github.com/daltonbr/kindle-dashboard/server/internal/weather"
)

// fakeForecaster returns a fixed forecast, standing in for *weather.Cache.
type fakeForecaster struct {
	fc  weather.Forecast
	err error
}

func (f fakeForecaster) Get(context.Context) (weather.Forecast, error) {
	return f.fc, f.err
}

func TestOpenMeteo_mapsPrecipAndDays(t *testing.T) {
	loc := time.FixedZone("BST", 3600)
	now := time.Date(2026, 5, 25, 16, 0, 0, 0, loc)

	src := fakeForecaster{fc: weather.Forecast{
		Now: weather.CurrentReading{Time: now, TempC: 19.3, WeatherCode: 61},
		Days: []weather.DailyReading{
			{Date: now.Truncate(24 * time.Hour), HighC: 21, LowC: 12, WeatherCode: 61, PrecipChance: 80},
			{Date: now.Add(24 * time.Hour), HighC: 18, LowC: 11, WeatherCode: 3, PrecipChance: 30},
			{Date: now.Add(48 * time.Hour), HighC: 20, LowC: 10, WeatherCode: 1, PrecipChance: 10},
		},
		Next24h: []weather.HourlyReading{
			{Time: now, TempC: 19.3, PrecipChance: 55, PrecipMM: 1.2},
			{Time: now.Add(time.Hour), TempC: 18.1, PrecipChance: 40, PrecipMM: 0.4},
		},
		FetchedAt: now,
	}}

	m, err := (&OpenMeteo{src: src}).Weather(context.Background())
	if err != nil {
		t.Fatalf("Weather: %v", err)
	}

	if m.Now != (Conditions{TempC: 19.3, Code: 61}) {
		t.Errorf("Now = %+v", m.Now)
	}
	if len(m.Days) != 3 {
		t.Fatalf("Days = %d, want 3", len(m.Days))
	}
	if got := m.Today(); got.HighC != 21 || got.LowC != 12 || got.Code != 61 || got.PrecipChance != 80 {
		t.Errorf("Today() = %+v", got)
	}
	if len(m.Hourly) != 2 {
		t.Fatalf("Hourly = %d, want 2", len(m.Hourly))
	}
	if h := m.Hourly[0]; h.PrecipChance != 55 || h.PrecipMM != 1.2 {
		t.Errorf("Hourly[0] = %+v, want precip 55%% / 1.2mm", h)
	}
}

func TestOpenMeteo_propagatesError(t *testing.T) {
	src := fakeForecaster{err: context.DeadlineExceeded}
	if _, err := (&OpenMeteo{src: src}).Weather(context.Background()); err == nil {
		t.Fatal("expected error to propagate")
	}
}
