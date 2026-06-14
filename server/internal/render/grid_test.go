package render

import (
	"image"
	"testing"
)

func TestNewGrid_portraitDims(t *testing.T) {
	g := NewGrid(Portrait, 2, 2)
	if g.W != 600 || g.H != 800 {
		t.Fatalf("dims = %dx%d, want 600x800", g.W, g.H)
	}
	if g.Cols != 2 || g.Rows != 2 {
		t.Fatalf("cols/rows = %d/%d, want 2/2", g.Cols, g.Rows)
	}
}

func TestNewGrid_landscapeDims(t *testing.T) {
	g := NewGrid(Landscape, 2, 2)
	if g.W != 800 || g.H != 600 {
		t.Fatalf("dims = %dx%d, want 800x600", g.W, g.H)
	}
}

// The grid block, header band and footer band must tile the panel vertically
// without gaps or overlaps, and all cells must stay inside the panel.
func TestGrid_bandsAndCellsTile(t *testing.T) {
	g := NewGrid(Portrait, 2, 2)
	bounds := image.Rect(0, 0, g.W, g.H)

	header := g.HeaderRect()
	footer := g.FooterRect()

	if header.Max.Y != g.Origin.Y {
		t.Errorf("header bottom %d != grid origin %d", header.Max.Y, g.Origin.Y)
	}
	bottomRow := g.CellRect(0, 1, 2, 1)
	if footer.Min.Y != bottomRow.Max.Y {
		t.Errorf("footer top %d != bottom row %d", footer.Min.Y, bottomRow.Max.Y)
	}
	if footer.Max.Y != g.H {
		t.Errorf("footer bottom %d != panel height %d", footer.Max.Y, g.H)
	}

	for _, c := range []image.Rectangle{
		g.CellRect(0, 0, 1, 1),
		g.CellRect(1, 0, 1, 1),
		g.CellRect(0, 1, 2, 1),
	} {
		if !c.In(bounds) {
			t.Errorf("cell %v escapes panel %v", c, bounds)
		}
	}
}

// The two top cells must be equal width and separated by exactly one gutter;
// a 2-wide span must equal their combined footprint.
func TestGrid_topRowAndSpan(t *testing.T) {
	g := NewGrid(Portrait, 2, 2)
	left := g.CellRect(0, 0, 1, 1)
	right := g.CellRect(1, 0, 1, 1)

	if left.Dx() != right.Dx() {
		t.Errorf("top cells unequal width: %d vs %d", left.Dx(), right.Dx())
	}
	if gap := right.Min.X - left.Max.X; gap != g.Gutter {
		t.Errorf("inter-cell gap = %d, want gutter %d", gap, g.Gutter)
	}

	span := g.CellRect(0, 1, 2, 1)
	if span.Min.X != left.Min.X || span.Max.X != right.Max.X {
		t.Errorf("2-wide span x[%d,%d] != combined top row x[%d,%d]",
			span.Min.X, span.Max.X, left.Min.X, right.Max.X)
	}
}
