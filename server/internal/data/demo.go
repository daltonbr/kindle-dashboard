package data

import (
	"context"
	"time"
)

// DemoWeather is a WeatherProvider that returns hardcoded, network-free data.
// It is the fixture the weather widgets are developed and tested against, and
// the fallback when no real provider is configured (decision D16).
type DemoWeather struct{}

// Weather returns a demo model anchored to the current wall clock so the
// hourly timeline lines up with "now".
func (DemoWeather) Weather(_ context.Context) (WeatherModel, error) {
	return DemoModel(time.Now()), nil
}

// DemoModel builds a deterministic, realistic-looking WeatherModel anchored to
// ref. Exposed (rather than hidden behind the provider) so tests and previews
// can pin the reference time and get stable output.
func DemoModel(ref time.Time) WeatherModel {
	// Anchor the hourly series to the top of ref's hour so bars align to clock hours.
	hourStart := ref.Truncate(time.Hour)

	// A plausible day: dry morning, a rain band building through the afternoon
	// peaking mid-afternoon, tapering into the evening. Indices are hours from
	// hourStart.
	chances := []int{5, 5, 0, 0, 0, 10, 20, 35, 55, 70, 80, 75, 60, 40, 25, 15, 10, 10, 5, 5, 0, 0, 0, 5}
	amounts := []float64{0, 0, 0, 0, 0, 0, 0.1, 0.3, 0.8, 1.4, 2.1, 1.7, 1.0, 0.5, 0.2, 0.1, 0, 0, 0, 0, 0, 0, 0, 0}
	temps := []float64{12, 11, 11, 10, 10, 11, 12, 13, 14, 14, 13, 13, 12, 12, 13, 14, 14, 13, 12, 12, 11, 11, 10, 10}

	hourly := make([]HourPoint, len(temps))
	for i := range temps {
		hourly[i] = HourPoint{
			Time:         hourStart.Add(time.Duration(i) * time.Hour),
			TempC:        temps[i],
			PrecipChance: chances[i],
			PrecipMM:     amounts[i],
		}
	}

	day0 := ref.Truncate(24 * time.Hour)
	days := []DayOutlook{
		{Date: day0, HighC: 14, LowC: 8, Code: 61, PrecipChance: 80},                    // today: rain
		{Date: day0.Add(24 * time.Hour), HighC: 16, LowC: 9, Code: 3, PrecipChance: 20}, // overcast
		{Date: day0.Add(48 * time.Hour), HighC: 18, LowC: 10, Code: 1, PrecipChance: 5}, // mainly clear
	}

	return WeatherModel{
		Place:      "Demo",
		Now:        Conditions{TempC: 13.4, Code: 61},
		Days:       days,
		Hourly:     hourly,
		ObservedAt: ref,
		FetchedAt:  ref,
	}
}
