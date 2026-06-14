// Package widgets holds the dashboard's composable cards. A widget draws a
// typed data model into the pixel rectangle the grid assigns it, and declares
// how many grid cells it occupies. Widgets never reach for data themselves —
// they are constructed with their model — which keeps the grid agnostic about
// widget-specific types. See docs/widgets.md.
package widgets

import (
	"image"
	"image/color"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"

	"github.com/daltonbr/kindle-dashboard/server/internal/render/fonts"
)

// Widget is one card on the dashboard grid.
type Widget interface {
	// Footprint is the widget's size in grid cells: cols×rows, each ∈ {1,2}.
	Footprint() (cols, rows int)
	// Render draws into area. The widget must not draw outside it.
	Render(dst *image.Gray, area image.Rectangle)
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
		return "Storm + hail"
	default:
		return "—"
	}
}

// --- shared drawing helpers (package-local) ---

// refCell is the portrait 1×1 cell height the 1×1 widget layouts are tuned for.
// Smaller cells (e.g. landscape) scale their offsets and fonts down by the ratio
// so content never spills outside the assigned rect.
const refCell = 276

// vscale returns a 0–1 factor for fitting a 1×1 layout into area's height.
func vscale(area image.Rectangle) float64 {
	if s := float64(area.Dy()) / refCell; s < 1 {
		return s
	}
	return 1
}

func face(px float64) font.Face { return fonts.Face(px) }

func drawAt(dst *image.Gray, f font.Face, s string, x, y int, gray uint8) {
	d := &font.Drawer{
		Dst:  dst,
		Src:  &image.Uniform{C: color.Gray{Y: gray}},
		Face: f,
		Dot:  fixed.P(x, y),
	}
	d.DrawString(s)
}

func drawCentered(dst *image.Gray, f font.Face, s string, cx, y int, gray uint8) {
	w := font.MeasureString(f, s).Round()
	drawAt(dst, f, s, cx-w/2, y, gray)
}

func drawRight(dst *image.Gray, f font.Face, s string, rx, y int, gray uint8) {
	w := font.MeasureString(f, s).Round()
	drawAt(dst, f, s, rx-w, y, gray)
}

// hLine draws a horizontal rule across [x0,x1) at y.
func hLine(dst *image.Gray, x0, x1, y int, gray uint8) {
	c := color.Gray{Y: gray}
	for x := x0; x < x1; x++ {
		dst.SetGray(x, y, c)
	}
}
