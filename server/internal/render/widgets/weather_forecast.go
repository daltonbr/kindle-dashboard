package widgets

import (
	"fmt"
	"image"

	"github.com/daltonbr/kindle-dashboard/server/internal/data"
)

// WeatherForecast is the 1×1 multi-day card: one row per upcoming day with its
// condition, high/low and rain chance.
type WeatherForecast struct {
	M data.WeatherModel
	// Days caps how many rows to draw (default 3 when zero).
	Days int
}

func (WeatherForecast) Footprint() (cols, rows int) { return 1, 1 }

func (w WeatherForecast) Render(dst *image.Gray, area image.Rectangle) {
	const pad = 16
	n := w.Days
	if n == 0 {
		n = 3
	}
	if n > len(w.M.Days) {
		n = len(w.M.Days)
	}

	s := vscale(area)
	fs := func(px float64) float64 { return px * s }

	labelY := area.Min.Y + int(30*s)
	drawAt(dst, face(fs(24)), "FORECAST", area.Min.X+pad, labelY, 90)
	hLine(dst, area.Min.X+pad, area.Max.X-pad, labelY+int(10*s), 180)

	rowH := int(64 * s)
	top := labelY + int(26*s)
	for i := 0; i < n; i++ {
		d := w.M.Days[i]
		rowTop := top + i*rowH

		label := d.Date.Format("Mon")
		if i == 0 {
			label = "Today"
		}
		drawAt(dst, face(fs(30)), label, area.Min.X+pad, rowTop+int(26*s), 0)
		drawRight(dst, face(fs(30)),
			fmt.Sprintf("%d° / %d°", round(d.HighC), round(d.LowC)),
			area.Max.X-pad, rowTop+int(26*s), 0)

		drawAt(dst, face(fs(22)),
			fmt.Sprintf("%s · rain %d%%", conditionWord(d.Code), d.PrecipChance),
			area.Min.X+pad, rowTop+int(50*s), 60)

		if i < n-1 {
			hLine(dst, area.Min.X+pad, area.Max.X-pad, rowTop+rowH-int(4*s), 220)
		}
	}
}
