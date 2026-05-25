// Package render composes the dashboard image.
//
// M3 step 1: weather data is wired in but layout/typography are still the
// M2 placeholder. M3.5 will refactor into proper panels; M3.4 swaps fonts.
package render

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"

	"github.com/daltonbr/kindle-dashboard/server/internal/weather"
)

// Dashboard renders a w×h 8-bit grayscale image for the eink panel.
// Pass forecast=nil to render the "weather unavailable" state.
func Dashboard(w, h int, now time.Time, forecast *weather.Forecast) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, w, h))

	fill(img, img.Bounds(), 255)

	// Double border so we can spot cropping on the panel.
	strokeRect(img, image.Rect(0, 0, w, h), 0)
	strokeRect(img, image.Rect(10, 10, w-10, h-10), 0)

	drawText(img, "Kindle Dashboard", 40, 50, 0)
	drawText(img, fmt.Sprintf("Served at %s", now.Format("2006-01-02 15:04:05 MST")), 40, 80, 0)

	drawText(img, "Weather", 40, 140, 0)
	if forecast != nil {
		drawText(img, fmt.Sprintf("Now: %.1f C  (code %d)", forecast.Now.TempC, forecast.Now.WeatherCode), 40, 170, 0)
		drawText(img, fmt.Sprintf("Today: H %.1f / L %.1f", forecast.HighToday, forecast.LowToday), 40, 195, 0)
		drawText(img, fmt.Sprintf("Observed at %s", forecast.Now.Time.Format("15:04 MST")), 40, 220, 0)
		drawText(img, fmt.Sprintf("Fetched at %s", forecast.FetchedAt.Format("15:04:05 UTC")), 40, 245, 0)
	} else {
		drawText(img, "(weather unavailable)", 40, 170, 0)
	}

	// 16-level grayscale ramp. Validates the panel actually renders grays.
	rampY := 450
	for i := range 16 {
		x := 40 + i*32
		fill(img, image.Rect(x, rampY, x+30, rampY+80), uint8(i*17))
		strokeRect(img, image.Rect(x, rampY, x+30, rampY+80), 0)
	}
	drawText(img, "16-level grayscale ramp", 40, rampY+100, 0)

	drawText(img, "kindle-dashboard / github.com/daltonbr", 40, h-40, 0)

	return img
}

func fill(img *image.Gray, r image.Rectangle, y uint8) {
	draw.Draw(img, r, &image.Uniform{C: color.Gray{Y: y}}, image.Point{}, draw.Src)
}

func strokeRect(img *image.Gray, r image.Rectangle, y uint8) {
	c := color.Gray{Y: y}
	for x := r.Min.X; x < r.Max.X; x++ {
		img.SetGray(x, r.Min.Y, c)
		img.SetGray(x, r.Max.Y-1, c)
	}
	for yy := r.Min.Y; yy < r.Max.Y; yy++ {
		img.SetGray(r.Min.X, yy, c)
		img.SetGray(r.Max.X-1, yy, c)
	}
}

func drawText(img *image.Gray, s string, x, y int, gray uint8) {
	d := &font.Drawer{
		Dst:  img,
		Src:  &image.Uniform{C: color.Gray{Y: gray}},
		Face: basicfont.Face7x13,
		Dot:  fixed.Point26_6{X: fixed.I(x), Y: fixed.I(y)},
	}
	d.DrawString(s)
}
