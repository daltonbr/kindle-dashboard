// Package panels owns the per-card rendering routines that compose the dashboard.
//
// Each panel takes a destination image and a rectangle (the area it owns) plus
// whatever data it needs. Panels paint within their rect; clipping is the
// caller's responsibility.
package panels

import (
	"fmt"
	"image"
	"image/color"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"

	"github.com/daltonbr/kindle-dashboard/server/internal/render/fonts"
	"github.com/daltonbr/kindle-dashboard/server/internal/weather"
)

// Weather paints the weather card inside area.
func Weather(dst *image.Gray, area image.Rectangle, forecast weather.Forecast) {
	const (
		padding       = 20
		bigTempSize   = 110
		conditionSize = 32
		hiloSize      = 24
		labelSize     = 22
		footnoteSize  = 18
	)

	centerX := (area.Min.X + area.Max.X) / 2

	// Big centered current temperature.
	tempY := area.Min.Y + 140
	drawCentered(dst, fonts.Face(bigTempSize),
		fmt.Sprintf("%.1f°C", forecast.Now.TempC), centerX, tempY, 0)

	// Condition word, centered below.
	condY := tempY + 60
	drawCentered(dst, fonts.Face(conditionSize),
		conditionWord(forecast.Now.WeatherCode), centerX, condY, 0)

	// Today's H / L, centered.
	hlY := condY + 60
	drawCentered(dst, fonts.Face(hiloSize),
		fmt.Sprintf("Today   H %.1f°   L %.1f°", forecast.HighToday, forecast.LowToday),
		centerX, hlY, 0)

	// Observation/fetch timestamps. Short, left-aligned, subordinate to the
	// big temp by virtue of size + ligher gray.
	footnoteY := hlY + 40
	drawAt(dst, fonts.Face(footnoteSize),
		fmt.Sprintf("Reading %s · fetched %s",
			forecast.Now.Time.Format("15:04"),
			forecast.FetchedAt.Format("15:04")),
		area.Min.X+padding, footnoteY, 80)

	// 24h temperature chart at the bottom of the area.
	if len(forecast.Next24h) > 1 {
		chart := image.Rect(area.Min.X+padding, area.Max.Y-180, area.Max.X-padding, area.Max.Y-40)
		drawAt(dst, fonts.Face(labelSize), "Next 24 hours", area.Min.X+padding, chart.Min.Y-14, 0)
		draw24hChart(dst, chart, forecast.Next24h)
	}
}

// conditionWord maps a WMO weather code to a short human label. Anything we
// don't recognise becomes "Code N" rather than blowing up the render.
//
// Reference: https://open-meteo.com/en/docs (WMO Weather interpretation codes)
func conditionWord(code int) string {
	switch code {
	case 0:
		return "Clear"
	case 1:
		return "Mainly clear"
	case 2:
		return "Partly cloudy"
	case 3:
		return "Overcast"
	case 45, 48:
		return "Fog"
	case 51, 53, 55:
		return "Drizzle"
	case 56, 57:
		return "Freezing drizzle"
	case 61, 63, 65:
		return "Rain"
	case 66, 67:
		return "Freezing rain"
	case 71, 73, 75:
		return "Snow"
	case 77:
		return "Snow grains"
	case 80, 81, 82:
		return "Rain showers"
	case 85, 86:
		return "Snow showers"
	case 95:
		return "Thunderstorm"
	case 96, 99:
		return "Thunderstorm w/ hail"
	default:
		return fmt.Sprintf("Code %d", code)
	}
}

// draw24hChart paints a line chart of hourly temperatures inside area, with
// min/max labels and 6-hourly hour ticks along the bottom.
func draw24hChart(dst *image.Gray, area image.Rectangle, hourly []weather.HourlyReading) {
	if len(hourly) < 2 {
		return
	}

	minT, maxT := hourly[0].TempC, hourly[0].TempC
	for _, h := range hourly[1:] {
		if h.TempC < minT {
			minT = h.TempC
		}
		if h.TempC > maxT {
			maxT = h.TempC
		}
	}
	if maxT-minT < 0.5 {
		maxT = minT + 1
	}

	plot := image.Rect(area.Min.X, area.Min.Y+10, area.Max.X, area.Max.Y-20)

	// Bottom axis (a touch lighter so it sits behind the line).
	for px := plot.Min.X; px < plot.Max.X; px++ {
		dst.SetGray(px, plot.Max.Y, color.Gray{Y: 120})
	}

	// 1-px line through the hourly points.
	n := len(hourly)
	prevX, prevY := -1, -1
	for i, h := range hourly {
		px := plot.Min.X + (plot.Dx()*i)/(n-1)
		py := plot.Max.Y - int(float64(plot.Dy())*(h.TempC-minT)/(maxT-minT))
		if prevX >= 0 {
			drawLine(dst, prevX, prevY, px, py, 0)
		}
		prevX, prevY = px, py
	}

	// Hour ticks every 6h with the hour-of-day underneath.
	tickFace := fonts.Face(16)
	for i, h := range hourly {
		if h.Time.Hour()%6 != 0 {
			continue
		}
		px := plot.Min.X + (plot.Dx()*i)/(n-1)
		for ty := plot.Max.Y; ty <= plot.Max.Y+4; ty++ {
			dst.SetGray(px, ty, color.Gray{Y: 0})
		}
		drawCentered(dst, tickFace, fmt.Sprintf("%02d", h.Time.Hour()), px, plot.Max.Y+18, 0)
	}

	labelFace := fonts.Face(16)
	drawAt(dst, labelFace, fmt.Sprintf("max %.1f°", maxT), plot.Max.X-80, plot.Min.Y+4, 0)
	drawAt(dst, labelFace, fmt.Sprintf("min %.1f°", minT), plot.Max.X-80, plot.Max.Y-4, 0)
}

func drawAt(dst *image.Gray, face font.Face, s string, x, y int, gray uint8) {
	d := &font.Drawer{
		Dst:  dst,
		Src:  &image.Uniform{C: color.Gray{Y: gray}},
		Face: face,
		Dot:  fixed.P(x, y),
	}
	d.DrawString(s)
}

func drawCentered(dst *image.Gray, face font.Face, s string, cx, y int, gray uint8) {
	w := font.MeasureString(face, s).Round()
	drawAt(dst, face, s, cx-w/2, y, gray)
}

// drawLine paints a 1-pixel line from (x0,y0) to (x1,y1) using Bresenham.
func drawLine(dst *image.Gray, x0, y0, x1, y1 int, gray uint8) {
	dx := abs(x1 - x0)
	dy := -abs(y1 - y0)
	sx, sy := 1, 1
	if x0 > x1 {
		sx = -1
	}
	if y0 > y1 {
		sy = -1
	}
	err := dx + dy
	c := color.Gray{Y: gray}
	for {
		dst.SetGray(x0, y0, c)
		if x0 == x1 && y0 == y1 {
			return
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
