package css

import (
	"image/color"

	"github.com/nathanstitt/doctaculous/pkg/layout"
	"github.com/nathanstitt/doctaculous/pkg/render"
)

// Fragment is one positioned box in page space (points, Y-down, origin at the
// page top-left). Produced by the CSS layout engine; read-only after layout, so a
// fragment tree may be shared across the render fan-out without locks. Children
// paint after this box's own background and border, in slice order, giving correct
// normal-flow paint order (background, border, then content; parent before child).
//
// Fragment is the recursive analogue of layout.Item: the layout engine emits this
// tree, and AppendItems flattens it into the flat layout.Page.Items slice the paint
// stage already consumes. The flatten is a pure read of the tree; it never mutates
// it, preserving the read-only-after-layout contract.
type Fragment struct {
	X, Y, W, H float64        // the BORDER box rectangle in page space
	Background color.RGBA     // zero-alpha => no background fill
	Border     [4]BorderEdge  // indexed by layout.EdgeSide (EdgeTop, EdgeRight, EdgeBottom, EdgeLeft)
	Lines      []LineFragment // inline content (set for a box establishing an inline formatting context)
	Children   []*Fragment    // child box fragments (block children; atomic inline boxes)
	DebugTag   string         // optional label for test lookup; not used in paint
}

// BorderEdge is one side of a fragment's border box. A zero edge (Width == 0 or
// Style == layout.BorderNone) paints nothing. The four edges of a Fragment are held
// in a [4]BorderEdge indexed by layout.EdgeSide, so Border[layout.EdgeTop] is the
// top edge, Border[layout.EdgeLeft] the left, and so on.
type BorderEdge struct {
	Width float64
	Color color.RGBA
	Style layout.BorderStyle
}

// LineFragment is one positioned line box of an inline formatting context: a single
// baseline shared by all its glyphs. The engine produces one per line after
// line-breaking; flattening emits each glyph as a layout.GlyphItem on this baseline.
type LineFragment struct {
	BaselineY float64 // page-space Y of the line's baseline
	Glyphs    []GlyphFragment
}

// GlyphFragment is one positioned glyph on a line. It mirrors layout.GlyphItem so
// flattening is a direct copy. Outline is in em units, Y up (as the font face
// returns it); X is the pen origin on the baseline in page space (the Y comes from
// the owning LineFragment's BaselineY). A nil Outline (e.g. whitespace) is skipped.
type GlyphFragment struct {
	Outline *render.Path
	X       float64
	SizePt  float64
	Color   color.RGBA
}

// AppendItems appends f's drawing primitives, and its descendants', to dst in
// paint order and returns the extended slice: this box's background (if any), then
// each non-empty border edge, then its inline line glyphs, then each child
// (recursively). The result feeds layout.Page.Items / paint.PaintPage.
//
// AppendItems only reads the fragment tree; it does not mutate it, so it is safe to
// call on a tree shared across the render fan-out.
func (f *Fragment) AppendItems(dst []layout.Item) []layout.Item {
	// 1. Background fills the border box. A zero-alpha background means none.
	if f.Background.A > 0 {
		dst = append(dst, layout.Item{
			Kind: layout.BackgroundKind,
			Rule: layout.RuleItem{XPt: f.X, YPt: f.Y, WPt: f.W, HPt: f.H, Color: f.Background},
		})
	}

	// 2. Border edges, in EdgeTop, EdgeRight, EdgeBottom, EdgeLeft order. Each edge is
	// a full-length strip cut from the border box; the four strips overlap at the
	// corners (mitering is out of scope, matching paint.paintBorder).
	for _, s := range [...]layout.EdgeSide{layout.EdgeTop, layout.EdgeRight, layout.EdgeBottom, layout.EdgeLeft} {
		e := f.Border[s]
		if e.Width <= 0 || e.Style == layout.BorderNone {
			continue
		}
		dst = append(dst, layout.Item{
			Kind:   layout.BorderKind,
			Border: f.edgeStrip(s, e),
		})
	}

	// 3. Inline line glyphs, in line then glyph order. A glyph with no outline
	// (whitespace) contributes nothing.
	for li := range f.Lines {
		ln := &f.Lines[li]
		for gi := range ln.Glyphs {
			g := &ln.Glyphs[gi]
			if g.Outline == nil {
				continue
			}
			dst = append(dst, layout.Item{
				Kind: layout.GlyphKind,
				Glyph: layout.GlyphItem{
					Outline: g.Outline,
					XPt:     g.X,
					YPt:     ln.BaselineY,
					SizePt:  g.SizePt,
					Color:   g.Color,
				},
			})
		}
	}

	// 4. Children paint after this box (parent-before-child), in slice order.
	for _, c := range f.Children {
		dst = c.AppendItems(dst)
	}
	return dst
}

// edgeStrip returns the border-edge item for side s of f's border box, given that
// side's edge e. The strip is the full-length band of thickness e.Width along the
// named side; adjacent strips meet (and overlap) at the corners. Side is recorded so
// the painter knows the strip's thickness and length axes.
func (f *Fragment) edgeStrip(s layout.EdgeSide, e BorderEdge) layout.BorderItem {
	b := layout.BorderItem{Color: e.Color, Style: e.Style, Side: s}
	switch s {
	case layout.EdgeTop:
		b.XPt, b.YPt, b.WPt, b.HPt = f.X, f.Y, f.W, e.Width
	case layout.EdgeBottom:
		b.XPt, b.YPt, b.WPt, b.HPt = f.X, f.Y+f.H-e.Width, f.W, e.Width
	case layout.EdgeLeft:
		b.XPt, b.YPt, b.WPt, b.HPt = f.X, f.Y, e.Width, f.H
	case layout.EdgeRight:
		b.XPt, b.YPt, b.WPt, b.HPt = f.X+f.W-e.Width, f.Y, e.Width, f.H
	}
	return b
}

// Page returns a single Page sized widthPt × heightPt whose Items are the flattened
// drawing primitives of the fragment tree rooted at f. This is the single-tall-page
// output model; real pagination is a later sub-project. It feeds the same
// paint.PaintPage path as the flat (DOCX) engine's output.
func (f *Fragment) Page(widthPt, heightPt float64) layout.Page {
	return layout.Page{
		WidthPt:  widthPt,
		HeightPt: heightPt,
		Items:    f.AppendItems(nil),
	}
}
