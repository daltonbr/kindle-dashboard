package render

import "image"

// Orientation selects the panel's long axis. Portrait is the device default.
type Orientation int

const (
	Portrait  Orientation = iota // 600×800
	Landscape                    // 800×600
)

// Dims returns the pixel dimensions for the orientation.
func (o Orientation) Dims() (w, h int) {
	if o == Landscape {
		return 800, 600
	}
	return 600, 800
}

// Grid lays out cols×rows cells that fill the area between fixed header and
// footer bands. Cells are not forced square — they fill the available middle so
// no space is wasted; widgets scale their content to whatever rect they get.
type Grid struct {
	W, H         int
	Cols, Rows   int
	Origin       image.Point // top-left of the grid block
	CellW, CellH int
	Gutter       int
	HeaderH      int // height of the band above the grid (== footer height)
}

const (
	gridSideMargin = 16 // left/right inset
	gridGutter     = 16 // gap between cells
	gridBand       = 84 // header/footer band height (large date + battery fit comfortably)
)

// NewGrid computes the layout for an orientation and cell count.
func NewGrid(o Orientation, cols, rows int) Grid {
	w, h := o.Dims()

	cellW := (w - 2*gridSideMargin - (cols-1)*gridGutter) / cols
	cellH := (h - 2*gridBand - (rows-1)*gridGutter) / rows

	blockW := cols*cellW + (cols-1)*gridGutter
	originX := (w - blockW) / 2

	return Grid{
		W: w, H: h,
		Cols: cols, Rows: rows,
		Origin:  image.Pt(originX, gridBand),
		CellW:   cellW,
		CellH:   cellH,
		Gutter:  gridGutter,
		HeaderH: gridBand,
	}
}

// CellRect returns the pixel rect for a widget whose top-left cell is (col,row)
// and which spans cols×rows cells.
func (g Grid) CellRect(col, row, cols, rows int) image.Rectangle {
	x0 := g.Origin.X + col*(g.CellW+g.Gutter)
	y0 := g.Origin.Y + row*(g.CellH+g.Gutter)
	x1 := x0 + cols*g.CellW + (cols-1)*g.Gutter
	y1 := y0 + rows*g.CellH + (rows-1)*g.Gutter
	return image.Rect(x0, y0, x1, y1)
}

// HeaderRect is the full-width band above the grid.
func (g Grid) HeaderRect() image.Rectangle {
	return image.Rect(gridSideMargin, 0, g.W-gridSideMargin, g.Origin.Y)
}

// FooterRect is the full-width band below the grid.
func (g Grid) FooterRect() image.Rectangle {
	footerTop := g.Origin.Y + g.Rows*g.CellH + (g.Rows-1)*g.Gutter
	return image.Rect(gridSideMargin, footerTop, g.W-gridSideMargin, g.H)
}
