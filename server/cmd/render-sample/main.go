// Command render-sample writes a synthetic dashboard PNG to /tmp/sample.png.
// Useful for iterating on layout/font tweaks without spinning up the HTTP
// server or redeploying.
//
//	go run ./cmd/render-sample && open /tmp/sample.png
package main

import (
	"fmt"
	"image/png"
	"log"
	"os"
	"time"

	"github.com/daltonbr/kindle-dashboard/server/internal/render"
	"github.com/daltonbr/kindle-dashboard/server/internal/weather"
)

func main() {
	now := time.Date(2026, 5, 26, 8, 54, 0, 0, time.UTC)
	temps := []float64{11, 10, 9, 9, 8, 8, 9, 10, 12, 14, 16, 17, 18, 19, 19, 18, 17, 15, 14, 13, 12, 12, 11, 11}
	hourly := make([]weather.HourlyReading, len(temps))
	for i, t := range temps {
		hourly[i] = weather.HourlyReading{Time: now.Add(time.Duration(i) * time.Hour), TempC: t}
	}
	f := &weather.Forecast{
		Now:       weather.CurrentReading{Time: now, TempC: 13.4, WeatherCode: 3},
		HighToday: 19.2,
		LowToday:  8.1,
		Next24h:   hourly,
		FetchedAt: now,
	}

	img := render.Dashboard(600, 800, now, f)

	const path = "/tmp/sample.png"
	out, err := os.Create(path)
	if err != nil {
		log.Fatalf("create %s: %v", path, err)
	}
	if err := png.Encode(out, img); err != nil {
		_ = out.Close()
		log.Fatalf("encode: %v", err)
	}
	if err := out.Close(); err != nil {
		log.Fatalf("close: %v", err)
	}
	fmt.Println("wrote", path)
}
