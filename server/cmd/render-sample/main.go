// Command render-sample writes synthetic dashboard PNGs to /tmp so you can
// iterate on layout/font tweaks without the HTTP server. It renders the demo
// model in a few configurations (rain-as-card vs rain-in-footer, landscape).
//
//	go run ./cmd/render-sample && open /tmp/sample-card.png
package main

import (
	"fmt"
	"image"
	"image/png"
	"log"
	"os"
	"time"

	"github.com/daltonbr/kindle-dashboard/server/internal/data"
	"github.com/daltonbr/kindle-dashboard/server/internal/render"
)

func main() {
	now := time.Date(2026, 6, 14, 8, 54, 0, 0, time.UTC)
	model := data.DemoModel(now)
	cal := data.DemoCalendarModel(now)
	batt := &render.Battery{Level: 53, Charging: true}

	samples := []struct {
		path string
		opts render.Options
	}{
		{"/tmp/sample-portrait.png", render.Options{Orientation: render.Portrait, Now: now, Battery: batt}},
		// Footer rain frees the bottom row, so the agenda card appears bottom-left.
		{"/tmp/sample-footer.png", render.Options{Orientation: render.Portrait, Now: now, Battery: batt, RainInFooter: true, Calendar: &cal}},
		{"/tmp/sample-landscape.png", render.Options{Orientation: render.Landscape, Now: now, Battery: batt, RainInFooter: true, Calendar: &cal}},
	}

	for _, s := range samples {
		img := render.Dashboard(&model, s.opts)
		write(s.path, img)
	}
}

func write(path string, img image.Image) {
	out, err := os.Create(path)
	if err != nil {
		log.Fatalf("create %s: %v", path, err)
	}
	if err := png.Encode(out, img); err != nil {
		_ = out.Close()
		log.Fatalf("encode: %v", err)
	}
	if err := out.Close(); err != nil {
		log.Fatalf("close: %v", err)
	}
	fmt.Println("wrote", path)
}
