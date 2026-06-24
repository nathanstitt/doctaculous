package css

import (
	"image"
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
	Image      *ImageContent  // decoded replaced-element image (set for a replaced box), painted in the content box
	DebugTag   string         // optional label for test lookup; not used in paint

	// IsFloat marks a fragment produced by a floated box. The float paint phases
	// skip such subtrees during the in-flow passes and paint them in the float pass
	// instead (CSS 2.1 Appendix E).
	IsFloat bool
	// IsBFC marks a fragment that establishes a block formatting context (the page
	// root and inline-blocks). Such a fragment owns the float-layer paint sequencing
	// for the floats placed in its BFC (held in Floats); a non-BFC fragment recurses
	// normally within each phase.
	IsBFC bool
	// Floats holds the fragments of floats placed in this fragment's BFC, painted in
	// their own layer (after in-flow block decorations, before in-flow inline
	// content). Set only on an IsBFC fragment. Kept separate from Children so in-flow
	// tree order is untouched.
	Floats []*Fragment
}

// ImageContent is a decoded replaced-element image carried on a Fragment. CX,CY,
// CW,CH is the fragment's content box in the same frame as the fragment's own
// border box (so it shifts with the fragment), resolved at layout time by deflating
// the border box by the box's border+padding. Fit is the object-fit mapping. A nil
// Img means decode failed: the fragment still reserves its box (a sized
// placeholder), but no image is painted.
type ImageContent struct {
	Img            image.Image
	CX, CY, CW, CH float64
	Fit            layout.ObjectFit
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

// AppendItems appends f's drawing primitives, and its descendants', to dst in CSS
// paint order and returns the extended slice. For a fragment that establishes a
// block formatting context (IsBFC), the order follows CSS 2.1 Appendix E within the
// context: in-flow block backgrounds/borders, then floats (Floats), then in-flow
// inline content / images / atomics — each phase skipping floated subtrees in the
// in-flow passes. A non-BFC fragment paints its own background/border/inline/image
// then recurses into its children (normal-flow tree order), which is what the BFC
// phases call per in-flow subtree.
//
// AppendItems only reads the fragment tree; it does not mutate it, so it is safe to
// call on a tree shared across the render fan-out.
func (f *Fragment) AppendItems(dst []layout.Item) []layout.Item {
	if f.IsBFC {
		dst = f.appendDecorations(dst) // in-flow backgrounds + borders (skip floats)
		for _, fl := range f.Floats {  // the float layer
			dst = fl.AppendItems(dst)
		}
		dst = f.appendContent(dst) // in-flow inline content + images (skip floats)
		return dst
	}
	// Non-BFC fragment: paint self, then recurse (normal tree order). This is the
	// per-subtree behavior the BFC phases invoke; it is unchanged from the original
	// single-pass AppendItems except that a floated subtree is skipped (the BFC root
	// paints it via Floats instead).
	dst = f.appendSelfDecorations(dst)
	dst = f.appendSelfContent(dst)
	for _, c := range f.Children {
		if c.IsFloat {
			continue
		}
		dst = c.AppendItems(dst)
	}
	return dst
}

// appendDecorations recurses the in-flow subtree emitting only backgrounds and
// borders, skipping floated subtrees (painted in the float layer) and NESTED BFC
// subtrees (an inline-block / new-BFC box paints as a single atom in the content
// phase via its own AppendItems — its internal block/float/inline layering is
// self-contained, so it must not be flattened into this BFC's decoration layer).
func (f *Fragment) appendDecorations(dst []layout.Item) []layout.Item {
	if f.IsFloat {
		return dst
	}
	dst = f.appendSelfDecorations(dst)
	for _, c := range f.Children {
		if c.IsBFC {
			continue // painted whole in the content phase (atomic)
		}
		dst = c.appendDecorations(dst)
	}
	return dst
}

// appendContent recurses the in-flow subtree emitting glyphs, images, and inline
// atomics, skipping floated subtrees. A NESTED BFC child (inline-block / new BFC)
// is painted here as a single atom via its full AppendItems — running its own
// decoration → float → content phases as a self-contained unit (CSS paints an
// atomic inline / BFC as one item in step 7), rather than being split across this
// BFC's phases.
func (f *Fragment) appendContent(dst []layout.Item) []layout.Item {
	if f.IsFloat {
		return dst
	}
	dst = f.appendSelfContent(dst)
	for _, c := range f.Children {
		if c.IsBFC {
			dst = c.AppendItems(dst) // atomic: its own full phase sequence
			continue
		}
		dst = c.appendContent(dst)
	}
	return dst
}

// appendSelfDecorations emits this fragment's own background then border edges (no
// recursion).
func (f *Fragment) appendSelfDecorations(dst []layout.Item) []layout.Item {
	if f.Background.A > 0 {
		dst = append(dst, layout.Item{
			Kind: layout.BackgroundKind,
			Rule: layout.RuleItem{XPt: f.X, YPt: f.Y, WPt: f.W, HPt: f.H, Color: f.Background},
		})
	}
	for _, s := range [...]layout.EdgeSide{layout.EdgeTop, layout.EdgeRight, layout.EdgeBottom, layout.EdgeLeft} {
		e := f.Border[s]
		if e.Width <= 0 || e.Style == layout.BorderNone {
			continue
		}
		dst = append(dst, layout.Item{Kind: layout.BorderKind, Border: f.edgeStrip(s, e)})
	}
	return dst
}

// appendSelfContent emits this fragment's own inline line glyphs then its replaced
// image (no recursion).
func (f *Fragment) appendSelfContent(dst []layout.Item) []layout.Item {
	for li := range f.Lines {
		ln := &f.Lines[li]
		for gi := range ln.Glyphs {
			g := &ln.Glyphs[gi]
			if g.Outline == nil {
				continue
			}
			dst = append(dst, layout.Item{
				Kind:  layout.GlyphKind,
				Glyph: layout.GlyphItem{Outline: g.Outline, XPt: g.X, YPt: ln.BaselineY, SizePt: g.SizePt, Color: g.Color},
			})
		}
	}
	if f.Image != nil && f.Image.Img != nil {
		dst = append(dst, layout.Item{
			Kind: layout.ImageKind,
			Image: layout.ImageItem{
				Img: f.Image.Img,
				XPt: f.Image.CX, YPt: f.Image.CY, WPt: f.Image.CW, HPt: f.Image.CH,
				Fit: f.Image.Fit,
			},
		})
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
