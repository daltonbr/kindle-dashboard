// Package render composes the dashboard image. It owns the page-level chrome
// (border, header, footer) and the grid that places widgets; per-card drawing
// lives in internal/render/widgets, and the data behind each card comes from
// internal/data. See docs/widgets.md.
package render

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"

	"github.com/daltonbr/kindle-dashboard/server/internal/data"
	"github.com/daltonbr/kindle-dashboard/server/internal/render/fonts"
	"github.com/daltonbr/kindle-dashboard/server/internal/render/widgets"
)

// Battery describes the device's power state for the header widget. Pass nil to
// omit it entirely.
type Battery struct {
	Level    int  // 0–100; clamped on render.
	Charging bool // true ⇒ a charging glyph overlays the fill.
}

// Options controls how a dashboard frame is composed.
type Options struct {
	Orientation  Orientation
	Now          time.Time
	Battery      *Battery            // nil ⇒ no battery indicator
	RainInFooter bool                // true ⇒ rain renders as the footer strip; false ⇒ as the bottom 2×1 card
	Calendar     *data.CalendarModel // nil ⇒ no agenda card (provider unset / unavailable)
}

// Dashboard composes a full dashboard frame. Pass model=nil to render the
// "weather unavailable" state.
//
// Layout (portrait): a square 2×2 grid between equal header/footer bands.
//   - top-left:  today  (1×1)   top-right: forecast (1×1)
//   - bottom:    rain    (2×1)   — unless RainInFooter, then the footer holds it
//     and the bottom row frees up: bottom-left → agenda (when Calendar is set),
//     bottom-right reserved for a future widget.
func Dashboard(model *data.WeatherModel, opts Options) *image.Gray {
	g := NewGrid(opts.Orientation, 2, 2)

	img := image.NewGray(image.Rect(0, 0, g.W, g.H))
	fill(img, img.Bounds(), 255)

	drawHeader(img, g, opts)

	if model == nil {
		hr := g.HeaderRect()
		drawAt(img, fonts.Face(30), "Weather unavailable", hr.Min.X, g.Origin.Y+g.CellH, 0)
		drawAgenda(img, g, opts) // calendar is independent of weather
		drawFooterCredit(img, g, opts, false)
		return img
	}

	m := *model
	widgets.WeatherToday{M: m}.Render(img, g.CellRect(0, 0, 1, 1))
	widgets.WeatherForecast{M: m}.Render(img, g.CellRect(1, 0, 1, 1))

	rain := widgets.Rain{Hours: m.Hourly}
	if opts.RainInFooter {
		rain.Render(img, g.FooterRect())
	} else {
		rain.Render(img, g.CellRect(0, 1, 2, 1)) // span the full bottom row
	}
	drawAgenda(img, g, opts)
	drawFooterCredit(img, g, opts, opts.RainInFooter)

	return img
}

// drawAgenda places the agenda card in the bottom-left cell. It only renders
// when a calendar model is present and the bottom grid row is free (rain moved
// to the footer); with rain occupying the bottom 2×1 card there is no room.
func drawAgenda(img *image.Gray, g Grid, opts Options) {
	if opts.Calendar == nil || !opts.RainInFooter {
		return
	}
	widgets.CalendarAgenda{M: *opts.Calendar, Now: opts.Now}.Render(img, g.CellRect(0, 1, 1, 1))
}

// drawHeader paints the date on the left and the optional battery on the right,
// vertically centred in the header band with a large, readable date.
func drawHeader(img *image.Gray, g Grid, opts Options) {
	hr := g.HeaderRect()
	baseY := (hr.Min.Y+hr.Max.Y)/2 + 12

	drawAt(img, fonts.Face(34), opts.Now.Format("Mon 2 Jan"), hr.Min.X+4, baseY, 0)

	if opts.Battery != nil {
		drawBattery(img, image.Rect(0, baseY-28, hr.Max.X, baseY), *opts.Battery)
	}
}

// drawFooterCredit draws the footer text. When the footer band is occupied by
// the rain strip (rainInFooter), there is no room for text, so this is a no-op.
func drawFooterCredit(img *image.Gray, g Grid, opts Options, rainInFooter bool) {
	if rainInFooter {
		return
	}
	fr := g.FooterRect()
	baseY := (fr.Min.Y+fr.Max.Y)/2 + 8
	drawAt(img, fonts.Face(20), fmt.Sprintf("updated %s", opts.Now.Format("15:04")), fr.Min.X+4, baseY, 60)
	drawRight(img, fonts.Face(20), "github.com/daltonbr/kindle-dashboard", fr.Max.X-4, baseY, 60)
}

// drawBattery paints an icon + percent number whose right edge sits at
// area.Max.X and whose baseline sits at area.Max.Y. The width is dynamic
// (depends on whether the number has two or three digits).
func drawBattery(img *image.Gray, area image.Rectangle, b Battery) {
	level := min(max(b.Level, 0), 100)

	numberFace := fonts.Face(28)
	label := fmt.Sprintf("%d%%", level)
	labelWidth := fontMeasure(numberFace, label)

	const (
		iconW    = 48
		iconH    = 24
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
	iconTop := area.Max.Y - 10 - iconH/2
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
		boltCX := iconRight + (boltSlot / 2) + 1
		boltCY := (iconTop + iconBot) / 2
		drawBolt(img, boltCX, boltCY)
	}

	drawAt(img, numberFace, label, numberX, area.Max.Y, 0)
}

// boltMask is a hand-drawn pixel bitmap for the charging indicator. Each '#'
// is a black pixel; '.' is left transparent.
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

func drawRight(img *image.Gray, face font.Face, s string, rx, y int, gray uint8) {
	w := font.MeasureString(face, s).Round()
	drawAt(img, face, s, rx-w, y, gray)
}
