package widgets

import (
	"fmt"
	"image"
	"math"

	"github.com/daltonbr/kindle-dashboard/server/internal/data"
)

// WeatherToday is the 1×1 "right now / today" card: big current temperature,
// condition word, today's high/low and rain chance.
type WeatherToday struct {
	M data.WeatherModel
}

func (WeatherToday) Footprint() (cols, rows int) { return 1, 1 }

func (w WeatherToday) Render(dst *image.Gray, area image.Rectangle) {
	const pad = 16
	cx := (area.Min.X + area.Max.X) / 2
	today := w.M.Today()
	s := vscale(area)
	yo := func(v int) int { return area.Min.Y + int(float64(v)*s) }
	fs := func(px float64) float64 { return px * s }

	// Card label + rule.
	labelY := yo(30)
	drawAt(dst, face(fs(22)), "TODAY", area.Min.X+pad, labelY, 90)
	hLine(dst, area.Min.X+pad, area.Max.X-pad, labelY+int(10*s), 180)

	// Big current temperature — the headline, readable across the room.
	drawCentered(dst, face(fs(78)), fmt.Sprintf("%d°", round(w.M.Now.TempC)), cx, yo(130), 0)

	// Condition word.
	drawCentered(dst, face(fs(28)), conditionWord(w.M.Now.Code), cx, yo(172), 0)

	// Today's high / low.
	drawCentered(dst, face(fs(26)),
		fmt.Sprintf("H %d°   L %d°", round(today.HighC), round(today.LowC)),
		cx, yo(214), 0)

	// Rain chance for the day.
	drawCentered(dst, face(fs(24)),
		fmt.Sprintf("Rain %d%%", today.PrecipChance), cx, yo(250), 60)
}

func round(f float64) int { return int(math.Round(f)) }
