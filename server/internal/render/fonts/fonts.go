// Package fonts owns the embedded TTF + a small Face(size) helper for the renderer.
//
// The font is Atkinson Hyperlegible (Braille Institute, OFL-licensed). It's
// embedded directly into the binary via //go:embed so the FROM-scratch image
// needs nothing extra at runtime.
package fonts

import (
	_ "embed"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
)

//go:embed AtkinsonHyperlegible-Regular.ttf
var atkinsonTTF []byte

var (
	parsed *opentype.Font

	faceMu sync.Mutex
	faces  = map[float64]font.Face{}

	// glyphBuf is reused across HasGlyph calls; sfnt.Buffer is not safe for
	// concurrent use, so access is guarded by faceMu.
	glyphBuf sfnt.Buffer
)

// HasGlyph reports whether the embedded font has a glyph for r. Runes without
// one (emoji, other pictographs) draw as blank "tofu", so callers can strip
// them before rendering. Space and other covered punctuation return true.
func HasGlyph(r rune) bool {
	faceMu.Lock()
	defer faceMu.Unlock()
	idx, err := parsed.GlyphIndex(&glyphBuf, r)
	return err == nil && idx != 0
}

func init() {
	f, err := opentype.Parse(atkinsonTTF)
	if err != nil {
		// The font is committed to the repo; a parse failure here is a build
		// problem, not a runtime input we can recover from.
		panic("fonts: parsing embedded Atkinson Hyperlegible: " + err.Error())
	}
	parsed = f
}

// Face returns an opentype Face at the given pixel size, with full hinting.
// Faces are cached per size; concurrent callers share the same face.
func Face(sizePx float64) font.Face {
	faceMu.Lock()
	defer faceMu.Unlock()
	if f, ok := faces[sizePx]; ok {
		return f
	}
	// DPI=72 makes Size effectively a pixel count, which is what we want when
	// we're laying out against a known pixel canvas.
	f, err := opentype.NewFace(parsed, &opentype.FaceOptions{
		Size:    sizePx,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		panic("fonts: NewFace: " + err.Error())
	}
	faces[sizePx] = f
	return f
}
