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
	)

	// Right-align: number on the right, icon to its left.
	numberX := area.Max.X - labelWidth
	iconRight := numberX - gap
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
		// Simple lightning bolt: a small diamond-ish shape centered on the icon
		// in white over the fill, so it reads at any battery level.
		cx := (body.Min.X + body.Max.X) / 2
		cy := (body.Min.Y + body.Max.Y) / 2
		drawBolt(img, cx, cy)
	}

	drawAt(img, numberFace, label, numberX, area.Max.Y, 0)
}

// drawBolt paints a small white lightning bolt centered at (cx, cy). It's
// drawn as two filled triangles meeting at the centre — crude but readable
// at 600×800.
func drawBolt(img *image.Gray, cx, cy int) {
	white := color.Gray{Y: 255}
	// Upper-left triangle and lower-right triangle, joined at the middle.
	// Coordinates relative to centre, then translated.
	points := [][2]int{
		{-3, -7}, {2, -7}, {2, -1}, {4, -1}, {-1, 7}, {-1, 1}, {-3, 1},
	}
	// Fill the polygon by scanning each row in its bounding box.
	minX, minY, maxX, maxY := 99, 99, -99, -99
	for _, p := range points {
		if p[0] < minX {
			minX = p[0]
		}
		if p[1] < minY {
			minY = p[1]
		}
		if p[0] > maxX {
			maxX = p[0]
		}
		if p[1] > maxY {
			maxY = p[1]
		}
	}
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			if pointInPolygon(x, y, points) {
				img.SetGray(cx+x, cy+y, white)
			}
		}
	}
}

func pointInPolygon(x, y int, poly [][2]int) bool {
	inside := false
	j := len(poly) - 1
	for i := range poly {
		xi, yi := poly[i][0], poly[i][1]
		xj, yj := poly[j][0], poly[j][1]
		if ((yi > y) != (yj > y)) && (x < (xj-xi)*(y-yi)/(yj-yi)+xi) {
			inside = true
		}
		j = i
	}
	return inside
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
