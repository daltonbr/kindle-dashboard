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
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"

	"github.com/daltonbr/kindle-dashboard/server/internal/weather"
)

// Weather paints the weather card inside area.
func Weather(dst *image.Gray, area image.Rectangle, forecast weather.Forecast) {
	const padding = 20
	x := area.Min.X + padding
	y := area.Min.Y + 25

	drawText(dst, "Weather", x, y, 0)
	y += 40

	drawText(dst, fmt.Sprintf("%.1f C", forecast.Now.TempC), x, y, 0)
	y += 25

	drawText(dst, conditionWord(forecast.Now.WeatherCode), x, y, 0)
	y += 30

	drawText(dst, fmt.Sprintf("Today  H %.1f  /  L %.1f", forecast.HighToday, forecast.LowToday), x, y, 0)
	y += 25

	drawText(dst, fmt.Sprintf("Observed %s", forecast.Now.Time.Format("15:04 MST")), x, y, 0)
	y += 25
	drawText(dst, fmt.Sprintf("Fetched  %s", forecast.FetchedAt.Format("15:04:05 UTC")), x, y, 0)

	if len(forecast.Next24h) > 0 {
		chart := image.Rect(area.Min.X+padding, area.Max.Y-200, area.Max.X-padding, area.Max.Y-40)
		drawText(dst, "Next 24 hours", area.Min.X+padding, chart.Min.Y-10, 0)
		draw24hChart(dst, chart, forecast.Next24h)
	}
}

// conditionWord maps a WMO weather code to a short human label. Anything we
// don't recognise becomes "Unknown" rather than blowing up the render.
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

// draw24hChart paints a simple line chart of the hourly temperatures inside
// area. Includes a baseline and a label at min/max.
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
	// Avoid a degenerate range (flat line) producing a divide-by-zero.
	if maxT-minT < 0.5 {
		maxT = minT + 1
	}

	// Reserve some headroom inside area so labels don't overlap the line.
	plot := image.Rect(area.Min.X, area.Min.Y+10, area.Max.X, area.Max.Y-15)

	// Bottom axis.
	for px := plot.Min.X; px < plot.Max.X; px++ {
		dst.SetGray(px, plot.Max.Y, color.Gray{Y: 100})
	}

	// Line through the hourly points.
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

	// Hour ticks every 6h with the hour-of-day under the tick.
	for i, h := range hourly {
		if h.Time.Hour()%6 != 0 {
			continue
		}
		px := plot.Min.X + (plot.Dx()*i)/(n-1)
		for ty := plot.Max.Y; ty <= plot.Max.Y+3; ty++ {
			dst.SetGray(px, ty, color.Gray{Y: 0})
		}
		drawText(dst, fmt.Sprintf("%02d", h.Time.Hour()), px-7, plot.Max.Y+15, 0)
	}

	drawText(dst, fmt.Sprintf("max %.1f", maxT), plot.Max.X-50, plot.Min.Y, 0)
	drawText(dst, fmt.Sprintf("min %.1f", minT), plot.Max.X-50, plot.Max.Y-2, 0)
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

func drawText(dst *image.Gray, s string, x, y int, gray uint8) {
	d := &font.Drawer{
		Dst:  dst,
		Src:  &image.Uniform{C: color.Gray{Y: gray}},
		Face: basicfont.Face7x13,
		Dot:  fixed.Point26_6{X: fixed.I(x), Y: fixed.I(y)},
	}
	d.DrawString(s)
}
