package panels

import (
	"image"
	"testing"
	"time"

	"github.com/daltonbr/kindle-dashboard/server/internal/weather"
)

func TestWeather_paintsIntoArea(t *testing.T) {
	const w, h = 600, 800
	img := image.NewGray(image.Rect(0, 0, w, h))
	// Pre-fill white so we can detect any draw operation.
	for i := range img.Pix {
		img.Pix[i] = 255
	}

	area := image.Rect(20, 100, w-20, h-60)
	Weather(img, area, sampleForecast())

	// Every pixel still white means the panel didn't paint anything.
	allWhite := true
	for _, p := range img.Pix {
		if p != 255 {
			allWhite = false
			break
		}
	}
	if allWhite {
		t.Fatal("Weather painted nothing")
	}
}

func TestConditionWord(t *testing.T) {
	cases := []struct {
		code int
		want string
	}{
		{0, "Clear"},
		{3, "Overcast"},
		{45, "Fog"},
		{61, "Rain"},
		{95, "Thunderstorm"},
		{9999, "Code 9999"},
	}
	for _, c := range cases {
		if got := conditionWord(c.code); got != c.want {
			t.Errorf("conditionWord(%d) = %q, want %q", c.code, got, c.want)
		}
	}
}

func TestWeather_emptyHourlyDoesNotPanic(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 600, 800))
	fc := sampleForecast()
	fc.Next24h = nil
	// Should not panic.
	Weather(img, img.Bounds(), fc)
}

func sampleForecast() weather.Forecast {
	now := time.Date(2026, 5, 25, 16, 0, 0, 0, time.UTC)
	hourly := make([]weather.HourlyReading, 24)
	for i := range hourly {
		hourly[i] = weather.HourlyReading{
			Time:  now.Add(time.Duration(i) * time.Hour),
			TempC: 15 + float64(i%6), // a bit of variation
		}
	}
	return weather.Forecast{
		Now:       weather.CurrentReading{Time: now, TempC: 20.5, WeatherCode: 3},
		HighToday: 24.0,
		LowToday:  12.0,
		Next24h:   hourly,
		FetchedAt: now,
	}
}
