package render

import (
	"bytes"
	"image"
	"image/png"
	"testing"
	"time"

	"github.com/daltonbr/kindle-dashboard/server/internal/data"
)

func TestDashboard_dimensionsAndEncoding(t *testing.T) {
	const (
		w = 600
		h = 800
	)
	now := time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC)

	img := Dashboard(nil, Options{Orientation: Portrait, Now: now})

	want := image.Rect(0, 0, w, h)
	if got := img.Bounds(); got != want {
		t.Fatalf("Dashboard bounds = %v, want %v", got, want)
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode failed: %v", err)
	}

	if buf.Len() == 0 {
		t.Fatal("png.Encode produced empty buffer")
	}

	// Sanity-check the PNG magic bytes — same check the client does.
	got := buf.Bytes()[:8]
	wantMagic := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	if !bytes.Equal(got, wantMagic) {
		t.Errorf("PNG magic = % x, want % x", got, wantMagic)
	}
}

// The calendar occupies the top row (month grid top-left, agenda top-right)
// when a model is present and rain is in the footer; weather sits on the bottom
// row regardless.
func TestDashboard_calendarPlacement(t *testing.T) {
	now := time.Date(2026, 6, 14, 8, 54, 0, 0, time.UTC)
	model := data.DemoModel(now)
	cal := data.DemoCalendarModel(now)

	g := NewGrid(Portrait, 2, 2)
	topLeft := g.CellRect(0, 0, 1, 1)
	topRight := g.CellRect(1, 0, 1, 1)
	bottomLeft := g.CellRect(0, 1, 1, 1)

	withCal := Dashboard(&model, Options{Orientation: Portrait, Now: now, RainInFooter: true, Calendar: &cal})
	if !hasInkIn(withCal, topLeft) {
		t.Error("month grid drew no ink in the top-left cell when wired")
	}
	if !hasInkIn(withCal, topRight) {
		t.Error("agenda drew no ink in the top-right cell when wired")
	}
	if !hasInkIn(withCal, bottomLeft) {
		t.Error("weather (today) drew no ink in the bottom-left cell")
	}

	// No calendar model → top row stays blank (rain is in the footer, not a card).
	noCal := Dashboard(&model, Options{Orientation: Portrait, Now: now, RainInFooter: true})
	if hasInkIn(noCal, topLeft) {
		t.Error("top-left cell should be blank when no calendar model is set")
	}
}

func hasInkIn(img *image.Gray, area image.Rectangle) bool {
	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			if img.GrayAt(x, y).Y < 255 {
				return true
			}
		}
	}
	return false
}
