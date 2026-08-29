package css

import (
	"image"
	"image/color"
	"sort"

	"github.com/nathanstitt/doctaculous/pkg/filtereffects"
	"github.com/nathanstitt/doctaculous/pkg/font"
	"github.com/nathanstitt/doctaculous/pkg/layout"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
	"github.com/nathanstitt/doctaculous/pkg/layout/inline"
	"github.com/nathanstitt/doctaculous/pkg/render"
)

// Fragment is one positioned box in page space (points, Y-down, origin at the
// page top-left). Produced by the CSS layout engine; read-only after layout, so a
// fragment tree may be shared across the render fan-out without locks. Paint order
// follows CSS 2.1 Appendix E for a fragment that establishes a block formatting
// context (IsBFC): in-flow block backgrounds/borders, then floats, then in-flow
// inline content. A non-BFC fragment keeps the simpler parent-before-child tree
// order (its own background and border, then its content, then its children). See
// AppendItems.
//
// Fragment is the recursive analogue of layout.Item: the layout engine emits this
// tree, and AppendItems flattens it into the flat layout.Page.Items slice the paint
// stage already consumes. The flatten is a pure read of the tree; it never mutates
// it, preserving the read-only-after-layout contract.
type Fragment struct {
	X, Y, W, H float64                 // the BORDER box rectangle in page space
	Background color.RGBA              // zero-alpha => no background fill
	Border     [4]BorderEdge           // indexed by layout.EdgeSide (EdgeTop, EdgeRight, EdgeBottom, EdgeLeft)
	Lines      []LineFragment          // inline content (set for a box establishing an inline formatting context)
	Children   []*Fragment             // child box fragments (block children; atomic inline boxes)
	Image      *ImageContent           // decoded replaced-element image (set for a replaced box), painted in the content box
	Vector     *VectorContent          // parsed replaced-element SVG scene (set for an SVG replaced box), painted in the content box
	Control    *ControlContent         // form-control widget (set for a control replaced box), painted in the content box
	BgImage    *BackgroundImageContent // decoded CSS background image (set when the box has a decodable background-image), painted behind content
	DebugTag   string                  // optional label for test lookup; not used in paint

	// Radii is the box's border-radius resolved against THIS fragment's border box
	// and already overlap-corrected (CSS Backgrounds 3 §5.1). It is resolved at
	// fragment-build time rather than at paint time because the percentages resolve
	// against the used border-box size, which only layout knows, and because the
	// correction factor depends on that same size — a paint-time resolution would
	// have to re-derive both.
	//
	// The zero value (every corner square) is the initial value and the common case;
	// every consumer checks Radii.Zero() first and takes its pre-existing
	// square-cornered path, so documents without radii are unaffected.
	Radii layout.CornerRadii

	// Box is the source cssbox.Box this fragment was produced from, retained so the
	// flatten/paint stage can read style-driven paint facts that are not pre-resolved
	// onto the fragment — today the stacking z-index (Box.Style.ZIndex/ZIndexAuto),
	// later opacity/isolation and SPA-snapshot re-flow. Set after layout; the flatten
	// stage only READS it and never mutates it, so the fragment tree stays safe to
	// share across the concurrent render fan-out — which holds only because layout has
	// fully completed before any flatten begins (there is no incremental relayout in
	// this engine yet). A nil Box reads as the initial style (z-index auto):
	// anonymous/synthetic fragments and the page root need not set it.
	Box *cssbox.Box

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

	// HeaderBottom is the page-space bottom Y of a table's repeatable <thead> rows, or
	// 0 for a non-table or a table with no header. Pagination clones the cells above
	// this line onto each continuation page so a long table keeps its column headings.
	//
	// It is a Y rather than a row count because the head/body/footer distinction is
	// flattened away by grid construction — by the time a fragment exists, a header
	// cell is indistinguishable from a body cell except by position.
	HeaderBottom float64
	// IsPositioned marks a fragment produced by a positioned box (relative,
	// absolute, or fixed). The stacking pass lifts such a fragment out of the
	// in-flow decoration/content passes and paints it in the positioned layer
	// instead (CSS 2.1 Appendix E). For a relative box (which IS in flow) this
	// moves only its painting; its in-flow space stays reserved.
	IsPositioned bool
	// RelOffsetX/RelOffsetY is a relatively-positioned box's paint-time offset
	// (CSS 9.4.3). Applied as a translate over the fragment's flattened item range
	// when the positioned layer paints it (NOT by shiftFragment/translateFragment,
	// which do not recurse Positioned). Zero for absolute/fixed (their position is
	// baked into the fragment coordinates by the abs-pos pass).
	RelOffsetX, RelOffsetY float64
	// IsStackingContext marks a fragment that establishes a stacking context (the
	// root and every positioned box). Such a fragment owns the Appendix E phase
	// ordering for its subtree, ending with its positioned layer.
	IsStackingContext bool
	// Positioned holds the fragments of positioned descendants painted in this
	// stacking context's positioned layer. Kept separate from Children so in-flow
	// tree order is untouched; a descendant in Positioned is skipped in the in-flow
	// passes (IsPositioned) so it paints exactly once. AppendItems z-index-sorts these
	// into three Appendix E bands (negatives before decorations, the z:auto/0 middle
	// after in-flow content, positives last); see sortedPositioned and AppendItems.
	Positioned []*Fragment

	// Collapsed holds the resolved border-collapse:collapse edge strips for a table
	// fragment (nil for every other fragment — so non-collapse pages are byte-identical).
	// Painted via the normal border path (BorderKind items) after the cell backgrounds
	// and cell content are emitted, so the grid lines paint on top of cell fills.
	// In the same page space as the fragment's border box.
	Collapsed []layout.BorderItem

	// Clips marks a fragment whose box has overflow ≠ visible: the stacking pass
	// brackets its contents (descendant decorations, floats, in-flow content, and the
	// CB-owned subset of its positioned layer) with a ClipPush(ClipRect)/ClipPop pair,
	// so they paint clipped to the padding box. The fragment's OWN background/border
	// paint OUTSIDE the bracket (a box does not clip its own border box). A clipping
	// fragment is always a BFC (overflow≠visible establishes one), so AppendItems
	// reaches it via the IsStackingContext||IsBFC branch.
	Clips bool
	// ClipRect is the clip rectangle when Clips is true: the padding box (the border
	// box deflated by the border widths), in page space. Zero when !Clips.
	ClipRect rect
	// PositionedInfo parallels Positioned: per-entry clip metadata telling the stacking
	// pass how to clip each positioned descendant painted in THIS holder's positioned
	// layer. len(PositionedInfo) == len(Positioned) when set; a nil/short slice reads as
	// the zero value (CBOwned=false, no clip chain) — the safe default, consulted only on
	// a clipping fragment.
	PositionedInfo []PositionedInfo

	// Filter holds the box's parsed CSS `filter` chain (nil — the overwhelmingly
	// common case — means no filter, and every unfiltered document's item stream is
	// byte-identical to before the property existed). When set, AppendItems brackets
	// EVERYTHING this fragment paints — its own background and border as well as its
	// contents and descendants — in a FilterPushKind/FilterPopKind pair, because a
	// CSS filter applies to the element's whole rendering, not just its contents.
	// That is the one structural difference from Clips, whose bracket deliberately
	// EXCLUDES the box's own border box.
	//
	// The bracket's rectangle is not stored: it is read from the fragment's own X/Y/
	// W/H at flatten time, so it needs no separate shift and can never drift out of
	// sync with the box after a pagination shift or a split.
	Filter []filtereffects.Function

	// FilterShadows carries each Filter entry's resolved drop-shadow() colour,
	// positionally aligned with Filter (see layout.FilterItem.ShadowColors). It
	// lives beside Filter rather than inside it because filtereffects.Function is
	// the shared, document-agnostic parse result and deliberately leaves a colour
	// token unparsed for its caller to resolve.
	FilterShadows []color.RGBA

	// Shadows holds the box's resolved `box-shadow` list (nil — the common case
	// — means no shadow, so every shadowless document's item stream stays
	// byte-identical to before the property existed). Entries are in SOURCE
	// order; appendSelfDecorations reverses them, because CSS paints the FIRST
	// shadow on top.
	//
	// Each entry carries only the shadow's own PARAMETERS (offset, blur,
	// spread, colour, inset) — deliberately NOT an absolute rectangle. The
	// shadow box is derived at flatten time from the fragment's own X/Y/W/H and
	// its border widths, exactly as the Filter bracket's rectangle is, so a
	// pagination shift or a fragment split can never leave a shadow behind at a
	// stale position. Storing a resolved rect here would need its own entry in
	// translateFragment, which is precisely the bookkeeping this avoids.
	Shadows []ShadowSpec
}

// ShadowSpec is one `box-shadow` entry with its lengths resolved to points and
// its colour resolved against the box, but WITHOUT geometry: the shadow box
// comes from the owning Fragment at flatten time (see Fragment.Shadows).
type ShadowSpec struct {
	OffsetX, OffsetY float64
	Blur             float64 // non-negative; a negative blur invalidates the declaration at parse time
	Spread           float64 // may be negative, which shrinks the shadow
	Color            color.RGBA
	Inset            bool
}

// PositionedInfo is one entry of a Fragment's PositionedInfo slice (parallel to
// Positioned): how to clip the matching positioned descendant when it paints in this
// holder's positioned layer.
type PositionedInfo struct {
	// CBOwned reports that Positioned[i]'s containing block IS this holder fragment.
	// A clipping holder paints a CB-owned entry INSIDE its own clip bracket; a
	// non-CB-owned (bubbled-through) entry paints after ClipPop, outside this holder's
	// own clip.
	CBOwned bool
	// ClipChain holds the padding-box rects of every overflow≠visible box the descendant
	// passed THROUGH between itself and this holder, outermost-first. Empty for the
	// common case. When non-empty, the positioned phase brackets THIS entry's emitted
	// item range in a nested ClipPush(rect)…ClipPop for each rect — so a positioned
	// descendant of a non-positioned overflow:hidden box is cut at that box's padding box
	// even though it paints in an ancestor's layer (CSS: every overflow≠visible ancestor
	// between the box and its CB clips it). The holder's OWN clip (when CBOwned) is
	// applied by the bracket, NOT by this chain.
	ClipChain []rect
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
	// PosX, PosY are the object-position as fractions of the content box's free space
	// (0.5/0.5 = centered, the default). See layout.ImageItem.
	PosX, PosY float64
}

// VectorContent is a resolution-independent replaced-element drawing (an SVG)
// carried on a Fragment. It is the VECTOR sibling of ImageContent and exists
// precisely so an SVG never has to become an image.Image: the scene flattens to a
// layout.VectorItem, which the painter hands straight to the render.Device, so a
// PDF backend emits real path operators instead of a rasterized bitmap.
//
// CX,CY,CW,CH is the fragment's content box in the same frame as the fragment's
// own border box (so it shifts with the fragment). The content box IS the SVG
// viewport: the scene is scaled to fill it and clipped to it (an SVG viewport is
// overflow:hidden), which is why there is no object-fit here — the SVG's own
// preserveAspectRatio governs how its viewBox maps into the viewport. A nil Scene
// means the parse failed: the fragment still reserves its box, but nothing paints.
type VectorContent struct {
	Scene          layout.VectorScene
	CX, CY, CW, CH float64
}

// BackgroundImageContent is a fragment's resolved CSS background image plus the
// geometry the painter needs, all in page-space points. The origin box is where the
// image is sized and positioned (background-origin); the clip box is the paint area it
// is confined to (background-clip) — the two differ when the properties differ. It is
// flattened into a layout.BackgroundImageItem in paint order (behind the box's
// content, after its background color, before its border).
//
// The source is either a raster image (Img) or a vector scene (Scene, an SVG); the
// geometry model is identical for both.
type BackgroundImageContent struct {
	Img image.Image
	// Scene is set INSTEAD of Img when background-image resolves to an SVG: the
	// scene travels to the painter and is drawn through a ctm, so the background
	// stays vector all the way to the backend. SceneW/SceneH are the scene's own
	// authored viewport size, which the painter needs in order to scale it into the
	// computed tile rectangle.
	Scene          layout.VectorScene
	SceneW, SceneH float64

	// Gradient is set INSTEAD of Img and Scene when background-image is a CSS
	// <gradient>. Its geometry is already resolved into TILE space, so it
	// travels to the painter needing nothing further from the cascade.
	Gradient *layout.BackgroundGradient

	IntrinsicW, IntrinsicH             float64
	OriginX, OriginY, OriginW, OriginH float64
	ClipX, ClipY, ClipW, ClipH         float64

	SizeKind     layout.BgSizeKind
	SizeW, SizeH float64 // resolved px per axis for BgSizeExplicit (≤0 = auto)

	PosXFrac, PosYFrac   float64
	PosXPx, PosYPx       float64
	PosXIsPct, PosYIsPct bool

	RepeatX, RepeatY bool
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
	// AdvancePt is the glyph's horizontal advance in page-space points. It sizes a
	// text-decoration underline span: the span's right edge is the last glyph's
	// X+AdvancePt (the visible glyph extent is approximated by the pen advance —
	// adequate for underlines). The CSS engine always sets it; a zero value would
	// shorten an underline's trailing edge by the last glyph's width.
	AdvancePt float64
	SizePt    float64
	Color     color.RGBA
	// Underline marks a glyph whose box has text-decoration: underline; consecutive
	// underlined glyphs on a line are painted with one underline rule (see
	// appendSelfContent).
	Underline bool
	// Strike marks a glyph whose box has text-decoration: line-through; consecutive
	// struck glyphs on a line are painted with one mid-glyph rule (see appendStrikes,
	// called alongside appendUnderlines in appendSelfContent).
	Strike bool
	// Edge marks a zero-ink glyph carrying an inline box's leading/trailing padding +
	// border as its advance. The decoration pass treats it as part of the box's extent;
	// the paint loop skips it like any other inkless glyph.
	Edge inline.InlineEdge
	// InlineBox identifies the innermost non-replaced inline box this glyph belongs to,
	// or nil when its nearest box is the block. Consecutive glyphs sharing one pointer
	// form the box's fragment ON THIS LINE, which is what appendInlineBoxDecorations
	// paints a background and border for. The pointer is the identity: two adjacent
	// spans styled identically compare unequal here, so their rects stay separate.
	InlineBox *inline.InlineBoxStyle
	// AscentPt and DescentPt are this glyph's own font metrics in points (both
	// positive, measured from the baseline). They give an inline box's background the
	// CONTENT-AREA height CSS specifies — the font's ascent+descent, NOT the line
	// height — and being per-glyph rather than per-line means a span mixing font sizes
	// gets a rect tall enough for its largest.
	//
	// Only the inline-decoration path reads them; they are zero for a glyph whose
	// producer does not set them, which simply yields no decoration rect.
	AscentPt, DescentPt float64
	// Blank marks a glyph that carries no ink (a space) but is retained anyway because
	// it sits INSIDE a decorated inline box. Without it, `case g.Outline != nil` would
	// drop the spaces in `<span>Hello world</span>` and the background would paint as
	// two rects with a hole between them. The paint loop skips these (they have a nil
	// Outline); only the decoration coalescing reads them, to stay continuous.
	Blank bool
	// BaselineShiftPt raises (positive) or lowers (negative) this glyph relative to the
	// line baseline, in page-space points — vertical-align: super/sub. The glyph's paint
	// Y is ln.BaselineY - BaselineShiftPt (up = smaller Y). Zero (the default) leaves the
	// glyph on the line baseline, so a run without super/sub is unchanged.
	BaselineShiftPt float64
	// Face, GID, and Runes carry font identity for text-emitting backends (the PDF
	// writer). Face is nil for a glyph with no identity; the rasterizer ignores them.
	Face  *font.Face
	GID   uint16
	Runes []rune
}

// AppendItems appends f's drawing primitives, and its descendants', to dst in CSS 2.1
// Appendix E paint order, returning the extended slice. For a fragment that establishes
// a stacking context (IsStackingContext — the root and every positioned box) OR a block
// formatting context (IsBFC — inline-blocks and floats), the positioned layer is split
// by z-index into three bands (sortedPositioned): NEGATIVE z paints BEFORE the context's
// in-flow decorations (Appendix E step 2, behind in-flow content); then in-flow block
// decorations, the float layer, and in-flow inline content/images (steps 3–5, each
// skipping floated AND positioned subtrees); then the MIDDLE band (z:auto / z:0 in
// document order, step 6); then the POSITIVE band (step 7). The sort is STABLE so equal
// keys keep document order — a context whose positioned boxes are all z:auto produces
// the same stream as the prior document-order pass (byte-identical for the existing
// corpus). A plain BFC that is not a stacking context has an empty positioned layer, so
// all three bands are empty and the order reduces to decorations → floats → content. A
// non-BFC, non-stacking fragment paints self then recurses children (skipping floated
// and positioned children), unchanged.
//
// A clipping fragment (Clips) brackets its CONTENTS — children's decorations, floats,
// in-flow content, the CB-owned subset of each band — with a ClipPush(ClipRect)/ClipPop
// pair (its own background/border paint outside it). CB-owned negatives paint inside the
// bracket behind the children; escaped entries (CB is an ancestor) paint outside it. An
// entry carrying a ClipChain (a positioned descendant that bubbled through an
// overflow≠visible box on its way to this holder) is itself bracketed by that chain's
// rects, so it is clipped to the intervening box even when it paints in this layer.
//
// A relatively-positioned entry carries a paint-time RelOffset, applied via
// translateItems over its freshly-flattened range. AppendItems never mutates the
// fragment tree (the sort packs a local copy; only appended dst items are translated),
// so it is safe on a tree shared across the render fan-out.
//
// A FILTERED fragment (Filter non-empty) wraps everything below in a
// FilterPushKind/FilterPopKind pair, so the whole box — its own decorations included
// — composites through one offscreen group. The pair is emitted here, at the single
// entry point, so every path that reaches a fragment (in-flow recursion, the float
// layer, a positioned band, a page's root wrapper) gets a BALANCED pair without each
// call site having to remember. Pagination splits the FRAGMENT TREE, not the item
// list — each page flattens its own subtree through this method — so a filtered box
// straddling a page break emits its own balanced pair on each page.
func (f *Fragment) AppendItems(dst []layout.Item) []layout.Item {
	if len(f.Filter) == 0 {
		return f.appendItemsUnfiltered(dst)
	}
	dst = append(dst, layout.Item{Kind: layout.FilterPushKind, Filter: layout.FilterItem{
		Funcs:        f.Filter,
		ShadowColors: f.FilterShadows,
		XPt:          f.X, YPt: f.Y, WPt: f.W, HPt: f.H,
	}})
	dst = f.appendItemsUnfiltered(dst)
	return append(dst, layout.Item{Kind: layout.FilterPopKind})
}

// appendItemsUnfiltered is AppendItems without the filter bracket: the Appendix E
// phase ordering itself. Split out so the bracket is applied exactly once, at the
// public entry point, and so the recursive descent below (which calls AppendItems on
// children) still brackets each filtered descendant in turn — nesting the pairs.
func (f *Fragment) appendItemsUnfiltered(dst []layout.Item) []layout.Item {
	if f.IsStackingContext || f.IsBFC {
		ord := f.sortedPositioned()
		if f.Clips {
			// Clipping context. Own decorations paint UNCLIPPED. Then: escaped negatives
			// (CB is an ancestor) unclipped & behind; the clip bracket wraps CB-owned
			// negatives (behind the children), child decorations, the float layer, in-flow
			// content, and the CB-owned middle+positive bands; escaped middle+positive
			// paint after ClipPop (unclipped — their CB is an ancestor).
			dst = f.appendSelfDecorations(dst)
			dst = f.appendBand(dst, ord.negatives, true, false) // escaped negatives, unclipped
			dst = append(dst, layout.Item{Kind: layout.ClipPushKind, Rule: layout.RuleItem{
				XPt: f.ClipRect.x, YPt: f.ClipRect.y, WPt: f.ClipRect.w, HPt: f.ClipRect.h,
			}})
			dst = f.appendBand(dst, ord.negatives, true, true) // CB-owned negatives, clipped
			dst = f.appendChildDecorations(dst)
			dst = f.appendFloatLayer(dst)
			dst = f.appendContent(dst)
			// Collapsed border-collapse grid lines paint after all cell backgrounds and
			// content so they are visible on top of cell fills (inside the clip bracket,
			// so they are clipped with the rest of the table's content), but BEFORE the
			// positioned layer — so a z-indexed positioned descendant of a cell stays
			// above the grid lines (it paints in a later band).
			dst = f.appendCollapsedBorders(dst)
			dst = f.appendBand(dst, ord.middle, true, true)    // CB-owned middle, clipped
			dst = f.appendBand(dst, ord.positives, true, true) // CB-owned positives, clipped
			dst = append(dst, layout.Item{Kind: layout.ClipPopKind})
			dst = f.appendBand(dst, ord.middle, true, false)    // escaped middle, unclipped
			dst = f.appendBand(dst, ord.positives, true, false) // escaped positives, unclipped
			return dst
		}
		// Non-clipping stacking context / BFC. CSS 2.1 Appendix E order: the context's
		// OWN background/border first, THEN negative-z descendants (which paint behind the
		// in-flow content but in front of this box's own background), then child
		// decorations, the float layer, in-flow content, the z:auto/0 middle, and positives.
		// (Own decorations must precede negatives — a z-index:-1 positioned descendant peeks
		// out behind the in-flow content but is NOT hidden by this box's own background. The
		// clipping branch above already orders it this way.)
		dst = f.appendSelfDecorations(dst)
		dst = f.appendBand(dst, ord.negatives, false, false)
		dst = f.appendChildDecorations(dst)
		dst = f.appendFloatLayer(dst)
		dst = f.appendContent(dst)
		// Collapsed border-collapse grid lines paint after all cell backgrounds and
		// content so they are visible on top of cell fills (table is a BFC, so this
		// non-clipping path is the common case for a non-overflow table), but BEFORE
		// the positioned layer — so a z-indexed positioned descendant of a cell
		// remains above the grid lines (it paints in a later band).
		dst = f.appendCollapsedBorders(dst)
		dst = f.appendBand(dst, ord.middle, false, false)
		dst = f.appendBand(dst, ord.positives, false, false)
		return dst
	}
	// Non-BFC, non-stacking fragment: unchanged.
	dst = f.appendSelfDecorations(dst)
	dst = f.appendSelfContent(dst)
	for _, c := range f.Children {
		if c.IsFloat || c.IsPositioned {
			continue
		}
		dst = c.AppendItems(dst)
	}
	return dst
}

// appendCollapsedBorders emits the resolved border-collapse:collapse grid edge strips
// stored on f.Collapsed, if any. Called after appendContent so the collapsed grid lines
// paint on top of cell backgrounds and cell content — making the borders visible over
// any cell fill color. A no-op for every non-collapse fragment (f.Collapsed is nil).
func (f *Fragment) appendCollapsedBorders(dst []layout.Item) []layout.Item {
	for i := range f.Collapsed {
		dst = append(dst, layout.Item{Kind: layout.BorderKind, Border: f.Collapsed[i]})
	}
	return dst
}

// appendFloatLayer emits the fragment's float layer (CSS 2.1 Appendix E: floats paint
// after in-flow block decorations and before in-flow inline content). Each float in
// f.Floats is flattened via its own AppendItems (a float establishes its own BFC), and
// a relatively-positioned float's RelOffset is applied via translateItems over the
// item range it just emitted. Shared by both the clipping and non-clipping branches of
// AppendItems so the float-paint sequence is written once.
func (f *Fragment) appendFloatLayer(dst []layout.Item) []layout.Item {
	for _, fl := range f.Floats {
		start := len(dst)
		dst = fl.AppendItems(dst)
		if fl.RelOffsetX != 0 || fl.RelOffsetY != 0 {
			translateItems(dst, start, fl.RelOffsetX, fl.RelOffsetY)
		}
	}
	return dst
}

// appendDecorations recurses the in-flow subtree emitting only backgrounds and
// borders: this fragment's own, then its children's (skipping floated, nested-BFC,
// and positioned subtrees — see appendChildDecorations). It is the decoration-phase
// entry for a non-clipping context root.
//
// f itself may be a float here: a float establishes its own BFC, so its AppendItems
// takes the IsBFC branch and calls this on the float as the decoration-phase root of
// its OWN context — it must paint its own background/border. The float-skip therefore
// applies to in-flow CHILDREN (a floated child is painted by the BFC's float layer,
// not in this in-flow recursion), not to f itself.
func (f *Fragment) appendDecorations(dst []layout.Item) []layout.Item {
	dst = f.appendSelfDecorations(dst)
	return f.appendChildDecorations(dst)
}

// appendChildDecorations recurses ONLY f's children's backgrounds/borders (not f's
// own), skipping floated subtrees (painted in the float layer), NESTED BFC subtrees
// (an inline-block / new-BFC box paints as a single atom in the content phase via its
// own AppendItems), and positioned subtrees (painted in the stacking context's
// positioned layer). A clipping fragment calls this between its ClipPush and ClipPop
// so its children's decorations are clipped while its own (already emitted) are not.
func (f *Fragment) appendChildDecorations(dst []layout.Item) []layout.Item {
	for _, c := range f.Children {
		if c.IsFloat || c.IsBFC || c.IsPositioned {
			continue
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
//
// As in appendDecorations, f itself may be a float (its own BFC's content-phase
// root) and must paint its own inline content; only floated CHILDREN are skipped
// (they paint in the BFC root's float layer).
func (f *Fragment) appendContent(dst []layout.Item) []layout.Item {
	dst = f.appendSelfContent(dst)
	for _, c := range f.Children {
		if c.IsFloat || c.IsPositioned {
			// A floated child paints in the BFC root's float layer; a positioned child
			// paints in the stacking context's positioned layer. Skip both here. The
			// IsPositioned check precedes the IsBFC atomic branch so a positioned
			// inline-block (IsBFC && IsPositioned) is lifted to the positioned layer, not
			// also painted atomically in-flow.
			continue
		}
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
//
// CSS Backgrounds 3 §6 fixes where the two kinds of box-shadow sit in that
// sequence, and they are NOT adjacent:
//
//	outer shadows → background colour → background image → INSET shadows → border
//
// An outer shadow paints BEHIND everything the box draws (so an opaque
// background hides the part of the shadow that falls under the box); an inset
// shadow paints over both backgrounds but UNDER the border (so a border is never
// darkened by the box's own inner shadow). Emitting both at one point would get
// one of the two wrong, which is why the list is walked twice.
//
// Within each group the list is walked in REVERSE, because CSS paints the FIRST
// shadow in the list on TOP and this item stream is painted front-to-back last.
func (f *Fragment) appendSelfDecorations(dst []layout.Item) []layout.Item {
	dst = f.appendShadows(dst, false)
	if f.Background.A > 0 {
		dst = append(dst, layout.Item{
			Kind: layout.BackgroundKind,
			Rule: layout.RuleItem{XPt: f.X, YPt: f.Y, WPt: f.W, HPt: f.H, Color: f.Background, Radii: f.Radii},
		})
	}
	// Background image paints after the background color and before the border (CSS
	// Backgrounds 3 paint order).
	if bg := f.BgImage; bg != nil && (bg.Img != nil || bg.Scene != nil || bg.Gradient != nil) {
		// A rounded box clips its background IMAGE to the rounded border box. Unlike
		// the background COLOR — a single fill this engine can round directly — a
		// background image is an arbitrary number of tiles drawn by DrawImage, which
		// has no shape parameter, so the only way to round it is a clip bracket around
		// the whole tiling run. A gradient takes the same path: it paints as a
		// background image, so it rounds by the same bracket rather than needing the
		// shader to know about corners.
		if !f.Radii.Zero() {
			dst = append(dst, layout.Item{
				Kind: layout.ClipPushKind,
				Rule: layout.RuleItem{XPt: f.X, YPt: f.Y, WPt: f.W, HPt: f.H, Radii: f.Radii},
			})
		}
		dst = append(dst, layout.Item{
			Kind: layout.BackgroundImageKind,
			BgImage: layout.BackgroundImageItem{
				Img:      bg.Img,
				Gradient: bg.Gradient,
				Scene:    bg.Scene, SceneW: bg.SceneW, SceneH: bg.SceneH,
				IntrinsicW: bg.IntrinsicW, IntrinsicH: bg.IntrinsicH,
				OriginX: bg.OriginX, OriginY: bg.OriginY, OriginW: bg.OriginW, OriginH: bg.OriginH,
				ClipX: bg.ClipX, ClipY: bg.ClipY, ClipW: bg.ClipW, ClipH: bg.ClipH,
				SizeKind: bg.SizeKind, SizeW: bg.SizeW, SizeH: bg.SizeH,
				PosXFrac: bg.PosXFrac, PosYFrac: bg.PosYFrac,
				PosXPx: bg.PosXPx, PosYPx: bg.PosYPx,
				PosXIsPct: bg.PosXIsPct, PosYIsPct: bg.PosYIsPct,
				RepeatX: bg.RepeatX, RepeatY: bg.RepeatY,
			},
		})
		if !f.Radii.Zero() {
			dst = append(dst, layout.Item{Kind: layout.ClipPopKind})
		}
	}
	if !f.Radii.Zero() {
		if ring, ok := f.borderRing(); ok {
			return append(dst, ring)
		}
		return dst
	}
	dst = f.appendShadows(dst, true)
	for _, s := range [...]layout.EdgeSide{layout.EdgeTop, layout.EdgeRight, layout.EdgeBottom, layout.EdgeLeft} {
		e := f.Border[s]
		if e.Width <= 0 || e.Style == layout.BorderNone {
			continue
		}
		dst = append(dst, layout.Item{Kind: layout.BorderKind, Border: f.edgeStrip(s, e)})
	}
	return dst
}

// borderRing builds the single BorderKind item that draws a ROUNDED box's whole
// border, or ok=false when the box has no visible border at all.
//
// A rounded border cannot be four independent strips: each corner's ink is shared
// between two adjacent edges and follows an arc that neither strip's rectangle
// contains. The ring — outer rounded rect minus inner rounded rect, filled even-odd
// — is the shape that is actually correct, and it is drawn as one item.
//
// DEGRADATION (logged by the caller's engine, and stated in FEATURES.md): the ring
// carries ONE colour and is always filled solid. A rounded box whose sides disagree
// in colour, or whose style is dashed/dotted/double/ridge/groove/inset/outset, is
// painted as a solid ring in the first visible side's colour. Rendering those
// properly needs each side's ink clipped to its own corner-mitre wedge, and the
// non-solid styles need the dash pattern walked along a curve — both deferred (see
// docs/CSS-LAYOUT.md). The approximation is deliberate: a solid rounded ring reads
// far closer to the intent than four square strips with the corners missing.
func (f *Fragment) borderRing() (layout.Item, bool) {
	var first *BorderEdge
	for _, s := range [...]layout.EdgeSide{layout.EdgeTop, layout.EdgeRight, layout.EdgeBottom, layout.EdgeLeft} {
		if e := f.Border[s]; e.Width > 0 && e.Style != layout.BorderNone {
			edge := e
			first = &edge
			break
		}
	}
	if first == nil {
		return layout.Item{}, false // no visible border: background only
	}
	// A side with no visible style contributes no width to the ring, so the inner
	// rectangle only deflates by the sides that actually paint.
	width := func(s layout.EdgeSide) float64 {
		if e := f.Border[s]; e.Style != layout.BorderNone && e.Width > 0 {
			return e.Width
		}
		return 0
	}
	t, r := width(layout.EdgeTop), width(layout.EdgeRight)
	b, l := width(layout.EdgeBottom), width(layout.EdgeLeft)

	// The inner curve's radii shrink by the border widths and floor at zero, then are
	// re-corrected against the INNER box: correcting against the outer box would let
	// two inner radii still overlap along a side the deflation shortened.
	inner := f.Radii.Inset(t, r, b, l).Correct(f.W-l-r, f.H-t-b)
	return layout.Item{
		Kind: layout.BorderKind,
		Border: layout.BorderItem{
			XPt: f.X, YPt: f.Y, WPt: f.W, HPt: f.H,
			Color: first.Color, Style: first.Style, Side: layout.EdgeTop,
			Ring: &layout.BorderRing{
				Outer: f.Radii, Inner: inner,
				Top: t, Right: r, Bottom: b, Left: l,
			},
		},
	}, true
}

// appendShadows emits f's box-shadows of one kind (inset or outer) in PAINT
// order, which is the source list REVERSED: CSS Backgrounds 3 §6 says "the
// first shadow is on top", and items later in this stream paint later, i.e. on
// top. A shadowless fragment appends nothing, so its item stream is unchanged.
func (f *Fragment) appendShadows(dst []layout.Item, inset bool) []layout.Item {
	for i := len(f.Shadows) - 1; i >= 0; i-- {
		s := f.Shadows[i]
		if s.Inset != inset {
			continue
		}
		dst = append(dst, layout.Item{Kind: layout.ShadowKind, Shadow: f.shadowItem(s)})
	}
	return dst
}

// appendDecoRules emits one RuleKind item per contiguous run of glyphs for which sel
// returns true on a line: a single thin rectangle spanning the run's x-extent (pen
// origin of the first glyph to pen-end of the last), drawn thickFactor*size thick at
// yOffFactor*size from the (baseline-shift-adjusted) line baseline. A POSITIVE
// yOffFactor is BELOW the baseline (underline); a NEGATIVE one is ABOVE it
// (line-through). size is the run's tallest glyph's size, and the thickness is clamped
// to a 1pt minimum. Emitted after the glyphs (the rule and the ink don't overlap in a
// way that depends on order). Shared by appendUnderlines and appendStrikes so the
// run-detection logic lives once.
func appendDecoRules(dst []layout.Item, ln *LineFragment, sel func(*GlyphFragment) bool, yOffFactor, thickFactor float64) []layout.Item {
	i := 0
	for i < len(ln.Glyphs) {
		if g := &ln.Glyphs[i]; !sel(g) {
			i++
			continue
		}
		// Start of a decorated run: extend over consecutive selected glyphs.
		x0 := ln.Glyphs[i].X
		x1 := ln.Glyphs[i].X + ln.Glyphs[i].AdvancePt
		size := ln.Glyphs[i].SizePt
		// The rule is drawn in the glyph's own color. CSS paints a propagated decoration
		// in the DECORATING ancestor's color, which diverges only when a descendant
		// overrides color without re-declaring decoration (a narrow case the engine does
		// not yet thread the decorating color through — deferred).
		col := ln.Glyphs[i].Color
		// A super/sub run's rule follows the shifted glyphs (0 for the common case).
		shift := ln.Glyphs[i].BaselineShiftPt
		for i++; i < len(ln.Glyphs) && sel(&ln.Glyphs[i]); i++ {
			x1 = ln.Glyphs[i].X + ln.Glyphs[i].AdvancePt
			if ln.Glyphs[i].SizePt > size {
				size = ln.Glyphs[i].SizePt // a span uses its tallest glyph's metrics
			}
		}
		thickness := size * thickFactor
		if thickness < 1 {
			thickness = 1
		}
		y := ln.BaselineY - shift + yOffFactor*size
		if x1 > x0 {
			dst = append(dst, layout.Item{
				Kind: layout.RuleKind,
				Rule: layout.RuleItem{XPt: x0, YPt: y, WPt: x1 - x0, HPt: thickness, Color: col},
			})
		}
	}
	return dst
}

// glyphHasColorInk reports whether a glyph paints through a colour-font path rather
// than an outline.
func glyphHasColorInk(g *GlyphFragment) bool {
	if g.Face == nil {
		return false
	}
	if g.Face.HasColorBitmaps() {
		return true
	}
	if g.Face.HasColorGlyphs() {
		layers, ok := g.Face.ColorLayers(g.GID)
		return ok && len(layers) > 0
	}
	return false
}

// appendInlineBoxDecorations emits the background and border of every inline box that
// has a fragment on line ln — one rect per box PER LINE, which is what makes a <span>
// spanning a line break paint correctly without any explicit fragmentation bookkeeping.
//
// Runs are detected by INLINE-BOX IDENTITY (pointer equality), not by comparing colors:
// two adjacent spans styled identically are still two boxes, and coalescing them would
// erase the gap their padding creates. This is the one way it differs from
// appendDecoRules, whose bool predicate cannot make that distinction.
//
// The rect's height is the CONTENT AREA — the tallest ascent and deepest descent among
// the run's glyphs — not the line height. That is what CSS specifies, and it is why the
// metrics are carried per glyph: a span mixing font sizes gets a rect sized to its
// largest. The horizontal extent runs from the first glyph's pen origin to the last
// glyph's pen end — the same approximation appendDecoRules documents as adequate for
// underlines. Inline PADDING is not applied: it would have to widen the box's advance
// in the line breaker too, and painting it here alone would draw a rect wider than the
// layout actually reserved. See inline.InlineBoxStyle.
//
// Blank (inkless) glyphs participate, which is why they are retained at emit time: a
// background must stay continuous across the spaces inside a span.
func appendInlineBoxDecorations(dst []layout.Item, ln *LineFragment) []layout.Item {
	i := 0
	for i < len(ln.Glyphs) {
		box := ln.Glyphs[i].InlineBox
		if !box.Paints() {
			i++
			continue
		}
		start := i
		x0 := ln.Glyphs[i].X
		x1 := ln.Glyphs[i].X + ln.Glyphs[i].AdvancePt
		asc, desc := ln.Glyphs[i].AscentPt, ln.Glyphs[i].DescentPt
		shift := ln.Glyphs[i].BaselineShiftPt
		for i++; i < len(ln.Glyphs) && ln.Glyphs[i].InlineBox == box; i++ {
			x1 = ln.Glyphs[i].X + ln.Glyphs[i].AdvancePt
			if a := ln.Glyphs[i].AscentPt; a > asc {
				asc = a
			}
			if d := ln.Glyphs[i].DescentPt; d > desc {
				desc = d
			}
		}
		// A run of only zero-metric glyphs has no box to paint.
		if x1 <= x0 || asc+desc <= 0 {
			continue
		}
		// Trailing spaces at a line break belong to the line's whitespace, not to the
		// box's painted extent: a browser does not stretch a span's background to the
		// right margin because the line wrapped after a space inside it.
		for j := i - 1; j >= start && ln.Glyphs[j].Blank && ln.Glyphs[j].Edge == inline.EdgeNone; j-- {
			x1 = ln.Glyphs[j].X
		}
		if x1 <= x0 {
			continue
		}
		y := ln.BaselineY - shift - asc
		h := asc + desc
		if box.Background.A > 0 {
			dst = append(dst, layout.Item{
				Kind: layout.BackgroundKind,
				Rule: layout.RuleItem{XPt: x0, YPt: y, WPt: x1 - x0, HPt: h, Color: box.Background},
			})
		}
		if box.BorderWidthPt > 0 && box.BorderColor.A > 0 {
			dst = appendInlineBoxBorder(dst, x0, y, x1-x0, h, box.BorderWidthPt, box.BorderColor)
		}
	}
	return dst
}

// appendInlineBoxBorder emits an inline box's border as four filled edge strips around
// the rect, drawn OUTSIDE it so the border does not eat into the box's own background.
// Uniform width and color only; a per-edge inline border is not modeled (see
// inlineBoxStyleFor), and rounded inline borders are not either — both would need the
// ring machinery the block path uses.
func appendInlineBoxBorder(dst []layout.Item, x, y, w, h, bw float64, col color.RGBA) []layout.Item {
	edge := func(ex, ey, ew, eh float64) layout.Item {
		return layout.Item{
			Kind: layout.RuleKind,
			Rule: layout.RuleItem{XPt: ex, YPt: ey, WPt: ew, HPt: eh, Color: col},
		}
	}
	dst = append(dst,
		edge(x-bw, y-bw, w+2*bw, bw), // top
		edge(x-bw, y+h, w+2*bw, bw),  // bottom
		edge(x-bw, y, bw, h),         // left
		edge(x+w, y, bw, h),          // right
	)
	return dst
}

// appendUnderlines emits text-decoration:underline rules for one line: one thin
// RuleKind rectangle per contiguous run of underlined glyphs, spanning the run's
// x-extent (pen origin of the first glyph to pen-end of the last), positioned just
// below the baseline. Thickness and offset scale with the run's glyph size (≈0.07em
// thick, ≈0.12em below the baseline) — a simple, browser-plausible underline. Emitted
// after the glyphs (an underline sits below the ink, so order doesn't matter). Thin
// wrapper over appendDecoRules with the underline predicate/offset/thickness.
func appendUnderlines(dst []layout.Item, ln *LineFragment) []layout.Item {
	return appendDecoRules(dst, ln, func(g *GlyphFragment) bool { return g.Underline && g.Outline != nil }, 0.12, 0.07)
}

// appendStrikes emits text-decoration:line-through rules for one line: one thin
// RuleKind rectangle per contiguous run of struck glyphs, spanning the run's x-extent
// (pen origin of the first glyph to pen-end of the last), positioned at roughly the
// glyph's mid-height (≈0.30em above the baseline, near the x-height center) rather than
// below it. Thickness scales with the run's glyph size (≈0.06em thick). Mirrors
// appendUnderlines but at a mid-glyph (above-baseline) Y. Emitted after the glyphs.
// Thin wrapper over appendDecoRules with the strike predicate/offset/thickness.
func appendStrikes(dst []layout.Item, ln *LineFragment) []layout.Item {
	return appendDecoRules(dst, ln, func(g *GlyphFragment) bool { return g.Strike && g.Outline != nil }, -0.30, 0.06)
}

// appendSelfContent emits this fragment's own inline line glyphs then its replaced
// image (no recursion).
func (f *Fragment) appendSelfContent(dst []layout.Item) []layout.Item {
	for li := range f.Lines {
		ln := &f.Lines[li]
		// Inline-box backgrounds and borders paint BEHIND the line's text, so they are
		// emitted before the glyph loop. Within the inline content layer this is the
		// CSS-correct place: an inline box's background sits above block backgrounds
		// and floats but below its own ink (CSS 2.1 Appendix E step 5).
		dst = appendInlineBoxDecorations(dst, ln)
		for gi := range ln.Glyphs {
			g := &ln.Glyphs[gi]
			// A nil outline is usually whitespace or an inline-box edge — nothing to
			// paint. A COLOUR glyph is the exception: its ink lives in COLR layers or a
			// bitmap strike, resolved at paint time from Face+GID, so skipping on
			// "no outline" would drop every emoji after layout had already reserved its
			// advance (the following text sat correctly, with a gap where the glyph
			// should have been).
			if g.Outline == nil && !glyphHasColorInk(g) {
				continue
			}
			// vertical-align: super/sub shifts the glyph off the line baseline (up = a
			// smaller Y). BaselineShiftPt is 0 for the common case, leaving YPt at the
			// line baseline (byte-identical).
			dst = append(dst, layout.Item{
				Kind:  layout.GlyphKind,
				Glyph: layout.GlyphItem{Outline: g.Outline, XPt: g.X, YPt: ln.BaselineY - g.BaselineShiftPt, SizePt: g.SizePt, Color: g.Color, Face: g.Face, GID: g.GID, Runes: g.Runes},
			})
		}
		dst = appendUnderlines(dst, ln)
		dst = appendStrikes(dst, ln)
	}
	if f.Image != nil && f.Image.Img != nil {
		dst = append(dst, layout.Item{
			Kind: layout.ImageKind,
			Image: layout.ImageItem{
				Img: f.Image.Img,
				XPt: f.Image.CX, YPt: f.Image.CY, WPt: f.Image.CW, HPt: f.Image.CH,
				Fit:  f.Image.Fit,
				PosX: f.Image.PosX, PosY: f.Image.PosY,
			},
		})
	}
	// A vector replaced element (SVG) flattens to a VectorKind item, NOT an
	// ImageKind one: layout.VectorItem carries the scene itself, so the painter
	// hands it to the Device to draw at device resolution. A nil Scene (failed
	// parse) emits nothing — the box is already reserved.
	if f.Vector != nil && f.Vector.Scene != nil {
		dst = append(dst, layout.Item{
			Kind: layout.VectorKind,
			Vector: layout.VectorItem{
				Scene: f.Vector.Scene,
				XPt:   f.Vector.CX, YPt: f.Vector.CY, WPt: f.Vector.CW, HPt: f.Vector.CH,
			},
		})
	}
	if f.Control != nil {
		dst = f.Control.append(dst)
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

// translateItems shifts every item in dst[start:] by (dx,dy), mutating their XPt/YPt
// in place. It applies a relatively-positioned fragment's paint offset to the items
// the fragment (and its subtree, incl. any abs-pos descendant on its Positioned
// layer) just emitted via AppendItems — so the whole positioned subtree rides the
// relative shift. Every coordinate-bearing item kind carries XPt/YPt
// (Background/Border/Glyph/Image, plus a ClipPushKind's rect when the offset box or a
// descendant establishes an overflow clip), so a uniform per-item translate is exact;
// ClipPopKind has no coordinates. This keeps AppendItems a pure read of the fragment
// tree: only the freshly-appended dst items are moved, never a Fragment.
func translateItems(dst []layout.Item, start int, dx, dy float64) {
	for i := start; i < len(dst); i++ {
		switch dst[i].Kind {
		case layout.BackgroundKind:
			dst[i].Rule.XPt += dx
			dst[i].Rule.YPt += dy
		case layout.BorderKind:
			dst[i].Border.XPt += dx
			dst[i].Border.YPt += dy
		case layout.GlyphKind:
			dst[i].Glyph.XPt += dx
			dst[i].Glyph.YPt += dy
		case layout.ImageKind:
			dst[i].Image.XPt += dx
			dst[i].Image.YPt += dy
		case layout.VectorKind:
			dst[i].Vector.XPt += dx
			dst[i].Vector.YPt += dy
		case layout.BackgroundImageKind:
			dst[i].BgImage.OriginX += dx
			dst[i].BgImage.OriginY += dy
			dst[i].BgImage.ClipX += dx
			dst[i].BgImage.ClipY += dy
		case layout.ClipPushKind:
			// A clip established by the offset box itself (overflow≠visible) or by an
			// overflow box inside its subtree rides the paint-time offset with the content
			// it clips. The range [start:] excludes the fixed ancestor-clip pushes a
			// positioned entry passed through (appendBand emits those BEFORE start), so only
			// the box's own / descendant clips move — exactly what CSS 9.4.3 requires.
			dst[i].Rule.XPt += dx
			dst[i].Rule.YPt += dy
		case layout.FilterPushKind:
			// The filter region is the box's border box, so it rides the paint-time
			// offset with the content it filters — exactly like a ClipPushKind's rect.
			dst[i].Filter.XPt += dx
			dst[i].Filter.YPt += dy
		case layout.ShadowKind:
			// The shadow box is the offset box's own border (or padding) box, so
			// it rides the paint-time offset with the box it decorates. Only the
			// box's origin moves: the offset, spread and blur are relative to it.
			dst[i].Shadow.XPt += dx
			dst[i].Shadow.YPt += dy
		case layout.ClipPopKind, layout.FilterPopKind:
			// No coordinates; nothing to translate (each is paired with its push).
		}
	}
}

// positionedEntry pairs a positioned descendant's fragment with its per-entry clip
// metadata, so the z-sort moves the two together without index bookkeeping.
type positionedEntry struct {
	frag *Fragment
	info PositionedInfo
}

// sortedBands is a fragment's positioned layer split into the three CSS 2.1 Appendix E
// z-index bands, each in stable (z, document) order. negatives paint BEFORE the
// context's decorations (step 2, behind in-flow content); middle paints after content
// (step 6, z:auto and z:0 in document order); positives paint last (step 7).
type sortedBands struct {
	negatives []positionedEntry // zKey < 0
	middle    []positionedEntry // zKey == 0 (auto + explicit 0)
	positives []positionedEntry // zKey > 0
}

// zIndex returns f's stacking sort inputs from its source box. A nil Box reads as the
// initial value (z-index auto).
func (f *Fragment) zIndex() (z int, auto bool) {
	if f.Box != nil {
		return f.Box.Style.ZIndex, f.Box.Style.ZIndexAuto
	}
	return 0, true
}

// zKey is f's numeric stacking sort key: auto and explicit 0 both map to 0 (the middle
// band), so they sort together and stable order preserves document order among them.
func (f *Fragment) zKey() int {
	z, auto := f.zIndex()
	if auto {
		return 0
	}
	return z
}

// sortedPositioned packs f's positioned layer into a fresh []positionedEntry (zipping
// Positioned[i] with PositionedInfo[i], a missing info read as the zero value),
// STABLE-sorts it by zKey ascending (document order — the slice's existing order —
// breaks ties), and splits it into the three z-bands. Building a fresh slice each call
// keeps f.Positioned/f.PositionedInfo read-only, so the shared fragment tree stays safe
// to flatten concurrently. When every entry is z:auto (the entire pre-z-index corpus),
// the negative/positive bands are empty and middle is the entries in their original
// document order — so AppendItems reduces to the prior document-order pass and output
// stays byte-identical.
func (f *Fragment) sortedPositioned() sortedBands {
	n := len(f.Positioned)
	if n == 0 {
		return sortedBands{}
	}
	entries := make([]positionedEntry, n)
	for i, pf := range f.Positioned {
		var info PositionedInfo
		if i < len(f.PositionedInfo) {
			info = f.PositionedInfo[i]
		}
		entries[i] = positionedEntry{frag: pf, info: info}
	}
	sort.SliceStable(entries, func(a, b int) bool {
		return entries[a].frag.zKey() < entries[b].frag.zKey()
	})
	// Partition at the first zKey>=0 and first zKey>0 boundaries.
	negEnd := 0
	for negEnd < n && entries[negEnd].frag.zKey() < 0 {
		negEnd++
	}
	midEnd := negEnd
	for midEnd < n && entries[midEnd].frag.zKey() == 0 {
		midEnd++
	}
	return sortedBands{
		negatives: entries[:negEnd],
		middle:    entries[negEnd:midEnd],
		positives: entries[midEnd:],
	}
}

// appendBand emits one band's positioned entries (already in stable z/document order)
// to dst. When filterCB is true (the clipping path), only entries whose
// info.CBOwned == wantCBOwned are emitted; when false (the non-clipping path), all
// entries are emitted and wantCBOwned is ignored. For each emitted entry it brackets
// the entry's item range in its ClipChain (outer→inner ClipPush … inner→outer ClipPop)
// and applies the relative RelOffset over the emitted range.
func (f *Fragment) appendBand(dst []layout.Item, band []positionedEntry, filterCB, wantCBOwned bool) []layout.Item {
	for _, e := range band {
		if filterCB && e.info.CBOwned != wantCBOwned {
			continue
		}
		for _, r := range e.info.ClipChain { // outermost first
			dst = append(dst, layout.Item{Kind: layout.ClipPushKind, Rule: layout.RuleItem{
				XPt: r.x, YPt: r.y, WPt: r.w, HPt: r.h,
			}})
		}
		start := len(dst)
		dst = e.frag.AppendItems(dst)
		if e.frag.RelOffsetX != 0 || e.frag.RelOffsetY != 0 {
			translateItems(dst, start, e.frag.RelOffsetX, e.frag.RelOffsetY)
		}
		for range e.info.ClipChain {
			dst = append(dst, layout.Item{Kind: layout.ClipPopKind})
		}
	}
	return dst
}

// Page returns a single Page sized widthPt × heightPt whose Items are the flattened
// drawing primitives of the fragment tree rooted at f. It is called once for the
// single-tall-page output model and once per page by the pagination pass (paginate),
// which flattens each page's shallow-cloned root wrapper. It feeds the same
// paint.PaintPage path as the flat (DOCX) engine's output.
func (f *Fragment) Page(widthPt, heightPt float64) layout.Page {
	return layout.Page{
		WidthPt:  widthPt,
		HeightPt: heightPt,
		Items:    f.AppendItems(nil),
	}
}
