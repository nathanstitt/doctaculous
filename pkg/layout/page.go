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

// ItemKind discriminates the Item union.
type ItemKind int

const (
	// GlyphKind is a single positioned glyph (Item.Glyph is set).
	GlyphKind ItemKind = iota
	// RuleKind is a filled rectangle — underlines and borders (Item.Rule is set).
	RuleKind
)

// Item is one drawing primitive on a page. It is a small tagged union rather than
// an interface so a page's items live in one contiguous slice.
type Item struct {
	Kind  ItemKind
	Glyph GlyphItem
	Rule  RuleItem
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

// RuleItem is an axis-aligned filled rectangle in page space (points, Y-down),
// used for underlines and, later, borders and backgrounds.
type RuleItem struct {
	XPt, YPt, WPt, HPt float64
	Color              color.RGBA
}
