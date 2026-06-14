package widgets

import (
	"image"
	"testing"
	"time"

	"github.com/daltonbr/kindle-dashboard/server/internal/data"
)

func TestFootprints(t *testing.T) {
	cases := []struct {
		name       string
		w          Widget
		cols, rows int
	}{
		{"today", WeatherToday{}, 1, 1},
		{"forecast", WeatherForecast{}, 1, 1},
		{"rain", Rain{}, 2, 1},
	}
	for _, c := range cases {
		if cols, rows := c.w.Footprint(); cols != c.cols || rows != c.rows {
			t.Errorf("%s footprint = %dx%d, want %dx%d", c.name, cols, rows, c.cols, c.rows)
		}
	}
}

// Each widget should draw at least some ink inside its area and never panic,
// across a card-sized rect and a short footer-strip rect (the rain widget is
// used both ways).
func TestWidgets_renderInk(t *testing.T) {
	m := data.DemoModel(time.Date(2026, 6, 14, 9, 0, 0, 0, time.UTC))
	widgets := map[string]Widget{
		"today":    WeatherToday{M: m},
		"forecast": WeatherForecast{M: m},
		"rain":     Rain{Hours: m.Hourly},
	}
	areas := map[string]image.Rectangle{
		"card":  image.Rect(20, 20, 296, 320),
		"strip": image.Rect(20, 20, 560, 120),
	}

	for wname, w := range widgets {
		for aname, area := range areas {
			img := image.NewGray(image.Rect(0, 0, 600, 400))
			fillWhite(img)
			w.Render(img, area)
			if !hasInk(img, area) {
				t.Errorf("%s/%s drew no ink", wname, aname)
			}
		}
	}
}

func fillWhite(img *image.Gray) {
	for i := range img.Pix {
		img.Pix[i] = 255
	}
}

func hasInk(img *image.Gray, area image.Rectangle) bool {
	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			if img.GrayAt(x, y).Y < 255 {
				return true
			}
		}
	}
	return false
}
