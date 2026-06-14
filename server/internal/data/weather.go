// Package data is the dashboard's typed data layer: render-ready models and the
// provider interfaces that fill them. Widgets draw from these models and never
// know the source, so a provider can be swapped freely (Demo* for offline
// development and tests, real implementations in production). See docs/widgets.md
// and decision D16.
package data

import (
	"context"
	"time"
)

// WeatherModel is the render-ready weather data the weather widgets draw from.
// It is source-agnostic: a WeatherProvider fills it from Open-Meteo, a demo
// fixture, or any other source.
type WeatherModel struct {
	Place      string       // human label for the location, e.g. "Brighton" (optional)
	Now        Conditions   // the current observation
	Days       []DayOutlook // chronological daily outlook; index 0 is today
	Hourly     []HourPoint  // chronological hourly points (feeds the rain timeline + temp charts)
	ObservedAt time.Time    // time of the current observation (location-local)
	FetchedAt  time.Time    // when the data was retrieved
}

// Today returns the day outlook for index 0, or a zero value if Days is empty.
func (m WeatherModel) Today() DayOutlook {
	if len(m.Days) == 0 {
		return DayOutlook{}
	}
	return m.Days[0]
}

// Conditions is the "right now" observation.
type Conditions struct {
	TempC float64
	Code  int // WMO weather interpretation code
}

// DayOutlook summarises a single day.
type DayOutlook struct {
	Date         time.Time
	HighC, LowC  float64
	Code         int // WMO weather interpretation code
	PrecipChance int // probability of precipitation, 0–100%, peak over the day
}

// HourPoint is one hourly sample. Precipitation fields feed the rain widget.
type HourPoint struct {
	Time         time.Time
	TempC        float64
	PrecipChance int     // probability of precipitation, 0–100%
	PrecipMM     float64 // expected precipitation amount, millimetres
}

// WeatherProvider produces a render-ready WeatherModel. Implementations must be
// inert without configuration: a provider that needs a key it doesn't have
// returns an error so the widget renders an "unavailable" state (decision D16).
type WeatherProvider interface {
	Weather(ctx context.Context) (WeatherModel, error)
}
