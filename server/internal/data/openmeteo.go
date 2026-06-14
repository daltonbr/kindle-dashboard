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
// WeatherProvider seam, projecting the client's forecast (hourly precipitation
// plus a 3-day daily outlook) onto the render model.
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
		hourly[i] = HourPoint{
			Time:         h.Time,
			TempC:        h.TempC,
			PrecipChance: h.PrecipChance,
			PrecipMM:     h.PrecipMM,
		}
	}

	days := make([]DayOutlook, len(fc.Days))
	for i, d := range fc.Days {
		days[i] = DayOutlook{
			Date:         d.Date,
			HighC:        d.HighC,
			LowC:         d.LowC,
			Code:         d.WeatherCode,
			PrecipChance: d.PrecipChance,
		}
	}

	return WeatherModel{
		Now:        Conditions{TempC: fc.Now.TempC, Code: fc.Now.WeatherCode},
		Days:       days,
		Hourly:     hourly,
		ObservedAt: fc.Now.Time,
		FetchedAt:  fc.FetchedAt,
	}, nil
}
