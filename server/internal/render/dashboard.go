// Package render composes the dashboard image. It owns the page-level layout
// (header, footer, panel placement) and delegates per-card drawing to
// internal/render/panels.
package render

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"

	"github.com/daltonbr/kindle-dashboard/server/internal/render/fonts"
	"github.com/daltonbr/kindle-dashboard/server/internal/render/panels"
	"github.com/daltonbr/kindle-dashboard/server/internal/weather"
)

// Dashboard renders a w×h 8-bit grayscale image for the eink panel.
// Pass forecast=nil to render the "weather unavailable" state.
func Dashboard(w, h int, now time.Time, forecast *weather.Forecast) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, w, h))
	fill(img, img.Bounds(), 255)

	// Single thin border — calmer than M2's double border now that we have real type.
	strokeRect(img, image.Rect(8, 8, w-8, h-8), 0)

	headerFace := fonts.Face(28)
	subFace := fonts.Face(14)
	drawAt(img, headerFace, "Kindle Dashboard", 30, 50, 0)
	drawAt(img, subFace, fmt.Sprintf("Served at %s", now.Format("2006-01-02 15:04:05 MST")),
		30, 75, 80)

	weatherArea := image.Rect(20, 100, w-20, h-50)
	if forecast != nil {
		panels.Weather(img, weatherArea, *forecast)
	} else {
		drawAt(img, fonts.Face(20), "Weather: (unavailable)",
			weatherArea.Min.X+20, weatherArea.Min.Y+40, 0)
	}

	drawAt(img, subFace, "kindle-dashboard · github.com/daltonbr", 30, h-20, 100)

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

func drawAt(img *image.Gray, face font.Face, s string, x, y int, gray uint8) {
	d := &font.Drawer{
		Dst:  img,
		Src:  &image.Uniform{C: color.Gray{Y: gray}},
		Face: face,
		Dot:  fixed.P(x, y),
	}
	d.DrawString(s)
}
