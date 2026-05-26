package render

import (
	"bytes"
	"image"
	"image/png"
	"testing"
	"time"
)

func TestDashboard_dimensionsAndEncoding(t *testing.T) {
	const (
		w = 600
		h = 800
	)
	now := time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC)

	img := Dashboard(w, h, now, nil, nil)

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
