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

// Battery describes the device's power state for the optional top-right
// widget. Pass nil to omit the widget entirely.
type Battery struct {
	Level    int  // 0–100; clamped on render.
	Charging bool // true ⇒ a charging glyph overlays the fill.
}

// Dashboard renders a w×h 8-bit grayscale image for the eink panel.
// Pass forecast=nil to render the "weather unavailable" state.
// Pass batt=nil to omit the battery widget.
func Dashboard(w, h int, now time.Time, forecast *weather.Forecast, batt *Battery) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, w, h))
	fill(img, img.Bounds(), 255)

	// Single thin border — calmer than M2's double border now that we have real type.
	strokeRect(img, image.Rect(8, 8, w-8, h-8), 0)

	headerFace := fonts.Face(28)
	dateFace := fonts.Face(28)
	timeFace := fonts.Face(18)
	footerFace := fonts.Face(16)
	drawAt(img, headerFace, "Kindle Dashboard", 30, 50, 0)
	drawAt(img, dateFace, now.Format("Mon 2 Jan"), 30, 88, 0)
	drawAt(img, timeFace, fmt.Sprintf("updated %s", now.Format("15:04")), 30, 110, 80)

	if batt != nil {
		// Right-aligned to mirror the left-aligned date row.
		drawBattery(img, image.Rect(0, 64, w-30, 92), *batt)
	}

	weatherArea := image.Rect(20, 130, w-20, h-50)
	if forecast != nil {
		panels.Weather(img, weatherArea, *forecast)
	} else {
		drawAt(img, fonts.Face(24), "Weather: (unavailable)",
			weatherArea.Min.X+20, weatherArea.Min.Y+40, 0)
	}

	drawAt(img, footerFace, "kindle-dashboard · github.com/daltonbr", 30, h-20, 100)

	return img
}

// drawBattery paints an icon + percent number whose right edge sits at
// area.Max.X and whose baseline sits at area.Max.Y. The width is dynamic
// (depends on whether the number has two or three digits).
func drawBattery(img *image.Gray, area image.Rectangle, b Battery) {
	level := min(max(b.Level, 0), 100)

	numberFace := fonts.Face(24)
	label := fmt.Sprintf("%d%%", level)
	labelWidth := fontMeasure(numberFace, label)

	const (
		iconW    = 44
		iconH    = 22
		notchW   = 4
		notchH   = 10
		padInner = 3
		gap      = 8
		boltSlot = 18 // horizontal slot reserved between icon and number when charging
	)

	// Right-align: number on the right, then optionally a charging bolt,
	// then the battery icon to its left.
	numberX := area.Max.X - labelWidth
	iconRight := numberX - gap
	if b.Charging {
		iconRight -= boltSlot
	}
	iconLeft := iconRight - (iconW + notchW)
	// Sit the icon so its vertical midpoint matches the digits' optical centre.
	// Atkinson Hyperlegible at 24pt has ~17px of cap height; visual centre of
	// the digits is roughly baseline - 8. Centring iconH there gives iconTop
	// = baseline - 8 - iconH/2.
	iconTop := area.Max.Y - 8 - iconH/2
	iconBot := iconTop + iconH

	body := image.Rect(iconLeft, iconTop, iconLeft+iconW, iconBot)
	strokeRect(img, body, 0)
	notch := image.Rect(body.Max.X, iconTop+(iconH-notchH)/2, body.Max.X+notchW, iconTop+(iconH+notchH)/2)
	fill(img, notch, 0)

	// Proportional fill from the left, leaving a small inner padding so the
	// outline stays visible. At 0% nothing is drawn; at 100% the bar fills
	// the entire inner area.
	innerL := body.Min.X + padInner
	innerR := body.Max.X - padInner
	innerT := body.Min.Y + padInner
	innerB := body.Max.Y - padInner
	fillW := ((innerR - innerL) * level) / 100
	if fillW > 0 {
		fill(img, image.Rect(innerL, innerT, innerL+fillW, innerB), 0)
	}

	if b.Charging {
		// Standalone bolt between icon and number, in black over white. Sits
		// on its own background so it reads at any battery level.
		boltCX := iconRight + (boltSlot / 2) + 1
		boltCY := (iconTop + iconBot) / 2
		drawBolt(img, boltCX, boltCY)
	}

	drawAt(img, numberFace, label, numberX, area.Max.Y, 0)
}

// boltMask is a hand-drawn pixel bitmap for the charging indicator. Each '#'
// is a black pixel; '.' is left transparent. Polygon rasterisation at this
// scale (~10px wide) was too unreliable — boundary pixels at integer
// coordinates dropped or doubled unpredictably — so we just draw the shape
// directly. Tweak the mask freely; drawBolt centres it on (cx, cy).
var boltMask = []string{
	".........#",
	"........##",
	".......###",
	"......####",
	".....####.",
	"....####..",
	"...####...",
	"..####....",
	".####.....",
	"##########",
	".########.",
	".......###",
	"......####",
	".....####.",
	"....####..",
	"...####...",
	"..####....",
	".####.....",
	".###......",
	".##.......",
	"#.........",
}

// drawBolt paints the bolt bitmap centred at (cx, cy) in solid black.
func drawBolt(img *image.Gray, cx, cy int) {
	black := color.Gray{Y: 0}
	h := len(boltMask)
	w := len(boltMask[0])
	offsetX := cx - w/2
	offsetY := cy - h/2
	for y, row := range boltMask {
		for x := 0; x < len(row); x++ {
			if row[x] == '#' {
				img.SetGray(offsetX+x, offsetY+y, black)
			}
		}
	}
}

func fontMeasure(face font.Face, s string) int {
	return font.MeasureString(face, s).Round()
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
