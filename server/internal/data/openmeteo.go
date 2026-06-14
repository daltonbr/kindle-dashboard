package data

import (
	"context"

	"github.com/daltonbr/kindle-dashboard/server/internal/weather"
)

// forecaster is the slice of weather.Cache this adapter needs. Declared as an
// interface so tests can substitute a fake without a live cache.
type forecaster interface {
	Get(ctx context.Context) (weather.Forecast, error)
}

// OpenMeteo adapts the existing internal/weather client+cache to the
// WeatherProvider seam. It maps the fields the M3 client already fetches;
// precipitation and the full multi-day outlook are zero/today-only until the
// client is extended (roadmap M5.3).
type OpenMeteo struct {
	src forecaster
}

// NewOpenMeteo wraps a weather.Cache as a WeatherProvider.
func NewOpenMeteo(cache *weather.Cache) *OpenMeteo {
	return &OpenMeteo{src: cache}
}

// Weather fetches a forecast and projects it onto the render model.
func (o *OpenMeteo) Weather(ctx context.Context) (WeatherModel, error) {
	fc, err := o.src.Get(ctx)
	if err != nil {
		return WeatherModel{}, err
	}

	hourly := make([]HourPoint, len(fc.Next24h))
	for i, h := range fc.Next24h {
		// TODO(M5.3): carry precipitation_probability + precipitation once the
		// Open-Meteo client requests them.
		hourly[i] = HourPoint{Time: h.Time, TempC: h.TempC}
	}

	return WeatherModel{
		Now: Conditions{TempC: fc.Now.TempC, Code: fc.Now.WeatherCode},
		Days: []DayOutlook{
			// TODO(M5.3): the client only keeps day[0]; widen to 3 days + precip.
			{Date: fc.Now.Time, HighC: fc.HighToday, LowC: fc.LowToday, Code: fc.Now.WeatherCode},
		},
		Hourly:     hourly,
		ObservedAt: fc.Now.Time,
		FetchedAt:  fc.FetchedAt,
	}, nil
}
