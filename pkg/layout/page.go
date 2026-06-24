package layout

import (
	"image/color"

	"github.com/nathanstitt/doctaculous/pkg/render"
)

// Pages is the engine's output: a document laid out into discrete pages of
// positioned drawing primitives. It is read-only after Layout, so it can be
// shared across the per-page render fan-out without locks.
type Pages struct {
	Pages []Page
}

// Page is one laid-out page: its size in points plus the primitives to draw,
// already positioned in page space (points, Y-down, origin at the top-left
// corner of the page). The paint stage maps page space to device pixels with a
// single matrix.
type Page struct {
	WidthPt, HeightPt float64
	Items             []Item
}

// BorderStyle is a border edge's line style.
type BorderStyle int

const (
	// BorderNone draws no border.
	BorderNone BorderStyle = iota
	// BorderSolid is a single continuous line filling the whole edge strip.
	BorderSolid
	// BorderDashed is a run of filled dashes along the edge with gaps between.
	BorderDashed
	// BorderDotted is a run of square dots along the edge with gaps between.
	BorderDotted
	// BorderDouble is two parallel lines (the outer and inner thirds of the strip)
	// with an empty gap between them.
	BorderDouble
)

// EdgeSide identifies which side of a box a border edge is on. It also tells the
// painter whether a dashed/dotted run steps along X (top/bottom) or Y (left/right)
// and which axis carries the edge's thickness.
type EdgeSide int

const (
	// EdgeTop is the top edge (horizontal strip; thickness along Y).
	EdgeTop EdgeSide = iota
	// EdgeRight is the right edge (vertical strip; thickness along X).
	EdgeRight
	// EdgeBottom is the bottom edge (horizontal strip; thickness along Y).
	EdgeBottom
	// EdgeLeft is the left edge (vertical strip; thickness along X).
	EdgeLeft
)

// ItemKind discriminates the Item union.
type ItemKind int

const (
	// GlyphKind is a single positioned glyph (Item.Glyph is set).
	GlyphKind ItemKind = iota
	// RuleKind is a filled rectangle — underlines and borders (Item.Rule is set).
	RuleKind
	// BackgroundKind is a filled rectangle behind content (Item.Rule is set); it is
	// painted exactly like a rule.
	BackgroundKind
	// BorderKind is one styled border edge (Item.Border is set).
	BorderKind
)

// Item is one drawing primitive on a page. It is a small tagged union rather than
// an interface so a page's items live in one contiguous slice.
type Item struct {
	Kind   ItemKind
	Glyph  GlyphItem
	Rule   RuleItem
	Border BorderItem
}

// GlyphItem is a glyph to fill. The outline is kept in raw em units (Y up, as the
// font face returns it); the paint stage composes one matrix per glyph from
// SizePt, the baseline origin, and the page matrix, mirroring the PDF text path.
type GlyphItem struct {
	Outline  *render.Path // em units, Y up; nil for whitespace
	XPt, YPt float64      // pen origin on the baseline, in page space
	SizePt   float64      // em scale in points
	Color    color.RGBA
}

// RuleItem is an axis-aligned filled rectangle in page space (points, Y-down). It
// backs both RuleKind (underlines, and solid borders the engine flattens to rules)
// and BackgroundKind (a fill drawn behind a box's content).
type RuleItem struct {
	XPt, YPt, WPt, HPt float64
	Color              color.RGBA
}

// BorderItem is one border edge: the edge's own rectangle (the strip) in page
// space (points, Y-down), its color, line style, and which side it is. Side gives
// the strip's orientation so the painter knows the thickness axis and, for
// dashed/dotted styles, the length axis to step along.
type BorderItem struct {
	XPt, YPt, WPt, HPt float64
	Color              color.RGBA
	Style              BorderStyle
	Side               EdgeSide
}
