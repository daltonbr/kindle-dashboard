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

// The agenda occupies the bottom-left cell only when a calendar model is present
// and rain has moved to the footer (freeing the bottom grid row).
func TestDashboard_agendaPlacement(t *testing.T) {
	now := time.Date(2026, 6, 14, 8, 54, 0, 0, time.UTC)
	model := data.DemoModel(now)
	cal := data.DemoCalendarModel(now)

	g := NewGrid(Portrait, 2, 2)
	bottomLeft := g.CellRect(0, 1, 1, 1)

	withCal := Dashboard(&model, Options{Orientation: Portrait, Now: now, RainInFooter: true, Calendar: &cal})
	if !hasInkIn(withCal, bottomLeft) {
		t.Error("agenda card drew no ink in the bottom-left cell when wired")
	}

	// No calendar model → bottom-left cell stays blank (rain is in the footer).
	noCal := Dashboard(&model, Options{Orientation: Portrait, Now: now, RainInFooter: true})
	if hasInkIn(noCal, bottomLeft) {
		t.Error("bottom-left cell should be blank when no calendar model is set")
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
