package widgets

import (
	"fmt"
	"image"
	"image/color"
	"time"

	"github.com/daltonbr/kindle-dashboard/server/internal/data"
)

// Rain visualises how the chance of rain is distributed across the coming
// hours: a probability bar per hour with the wettest stretch called out. Its
// renderer is rect-agnostic — the same widget reads well as a wide 2×1 grid
// card or as a short full-width footer strip.
type Rain struct {
	Hours []data.HourPoint
}

func (Rain) Footprint() (cols, rows int) { return 2, 1 }

func (r Rain) Render(dst *image.Gray, area image.Rectangle) {
	const pad = 16

	// Header line: label on the left, the peak called out on the right.
	labelY := area.Min.Y + 28
	drawAt(dst, face(22), "RAIN", area.Min.X+pad, labelY, 90)

	peakChance, peakAt, totalMM := r.summarise()
	summary := "none expected"
	if peakChance > 0 {
		summary = fmt.Sprintf("peak %d%% at %s · %.1fmm", peakChance, peakAt.Format("15:04"), totalMM)
	}
	drawRight(dst, face(22), summary, area.Max.X-pad, labelY, 60)

	if len(r.Hours) < 2 {
		return
	}

	// Chart area: below the header, above a thin baseline + hour ticks.
	chart := image.Rect(area.Min.X+pad, labelY+14, area.Max.X-pad, area.Max.Y-26)
	if chart.Dy() < 8 {
		return
	}
	drawBars(dst, chart, r.Hours)

	// Baseline + 6-hourly tick labels.
	hLine(dst, chart.Min.X, chart.Max.X, chart.Max.Y, 120)
	n := len(r.Hours)
	tick := face(18)
	for i, h := range r.Hours {
		if h.Time.Hour()%6 != 0 {
			continue
		}
		px := chart.Min.X + (chart.Dx()*i)/(n-1)
		drawCentered(dst, tick, h.Time.Format("15"), px, chart.Max.Y+20, 90)
	}
}

// drawBars paints one probability bar per hour, scaled to the chart height.
func drawBars(dst *image.Gray, chart image.Rectangle, hours []data.HourPoint) {
	n := len(hours)
	slot := float64(chart.Dx()) / float64(n)
	barW := max(int(slot)-2, 2)
	for i, h := range hours {
		if h.PrecipChance <= 0 {
			continue
		}
		x0 := chart.Min.X + int(float64(i)*slot)
		barH := max((chart.Dy()*h.PrecipChance)/100, 1)
		// Wetter hours render darker so the peak reads at a glance.
		shade := uint8(160 - h.PrecipChance) // 100% -> 60, 10% -> 150
		fillBar(dst, image.Rect(x0, chart.Max.Y-barH, x0+barW, chart.Max.Y), shade)
	}
}

func fillBar(dst *image.Gray, r image.Rectangle, gray uint8) {
	c := color.Gray{Y: gray}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			dst.SetGray(x, y, c)
		}
	}
}

// summarise returns the peak chance, the time it occurs, and the total expected
// precipitation across the window.
func (r Rain) summarise() (peakChance int, peakAt time.Time, totalMM float64) {
	bestIdx := -1
	for i, h := range r.Hours {
		totalMM += h.PrecipMM
		if h.PrecipChance > peakChance {
			peakChance = h.PrecipChance
			bestIdx = i
		}
	}
	if bestIdx >= 0 {
		peakAt = r.Hours[bestIdx].Time
	} else {
		peakAt = r.Hours[0].Time
	}
	return peakChance, peakAt, totalMM
}
