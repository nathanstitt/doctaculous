package css

import (
	"image"
	"image/color"

	"github.com/nathanstitt/doctaculous/pkg/layout"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
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
	X, Y, W, H float64        // the BORDER box rectangle in page space
	Background color.RGBA     // zero-alpha => no background fill
	Border     [4]BorderEdge  // indexed by layout.EdgeSide (EdgeTop, EdgeRight, EdgeBottom, EdgeLeft)
	Lines      []LineFragment // inline content (set for a box establishing an inline formatting context)
	Children   []*Fragment    // child box fragments (block children; atomic inline boxes)
	Image      *ImageContent  // decoded replaced-element image (set for a replaced box), painted in the content box
	DebugTag   string         // optional label for test lookup; not used in paint

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
	// stacking context's positioned layer (after in-flow content). Kept separate
	// from Children so in-flow tree order is untouched; a descendant in Positioned
	// is skipped in the in-flow passes (IsPositioned) so it paints exactly once.
	// In the minimal cut these paint in document order (no z-index sort).
	Positioned []*Fragment

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
// stacking context (IsStackingContext — the root and every positioned box) OR a block
// formatting context (IsBFC — inline-blocks and floats), the order follows the
// supported subset of CSS 2.1 Appendix E within the context (4 phases): in-flow block
// backgrounds/borders (appendDecorations), then the float layer (Floats), then in-flow
// inline content / images / atomics (appendContent), then the positioned layer
// (Positioned) — each in-flow phase skipping floated AND positioned subtrees so they
// paint exactly once in their own layer. A plain BFC that is not a stacking context has
// an empty Positioned slice, so the positioned phase is a no-op and the order reduces to
// the prior 3-phase sequence (preserving byte-identical output for non-positioned
// pages). A non-BFC, non-stacking fragment paints its own background/border/inline/image
// then recurses into its children (normal-flow tree order), skipping floated and
// positioned children, which is what the context phases call per in-flow subtree.
//
// A relatively-positioned descendant carries a paint-time offset (RelOffsetX/Y); the
// positioned (and float, for a positioned float) layer applies it via translateItems
// over the descendant's freshly-flattened item range, so the whole positioned subtree
// rides the relative shift. AppendItems itself never mutates the fragment tree (only the
// appended dst items are translated), so it is safe to call on a tree shared across the
// render fan-out.
//
// The positioned-layer loop is the seam the deferred full-z-index slice extends: it
// gains a z-index sort and splits into negative / zero / positive sub-layers (with the
// negative sub-layer moving before appendDecorations and the positive after this loop).
// The phase split is written so that slice re-points the collection, not rewrites the
// pass — keep it intact when extending this.
func (f *Fragment) AppendItems(dst []layout.Item) []layout.Item {
	if f.IsStackingContext || f.IsBFC {
		if f.Clips {
			// Clipping context: own decorations paint UNCLIPPED, then a clip bracket wraps
			// the children's decorations, the float layer, the in-flow content, and the
			// CB-owned subset of the positioned layer. Escaped positioned descendants (CB
			// outside this box) paint AFTER ClipPop, unclipped.
			dst = f.appendSelfDecorations(dst) // own bg + border — outside the clip
			dst = append(dst, layout.Item{Kind: layout.ClipPushKind, Rule: layout.RuleItem{
				XPt: f.ClipRect.x, YPt: f.ClipRect.y, WPt: f.ClipRect.w, HPt: f.ClipRect.h,
			}})
			dst = f.appendChildDecorations(dst) // children's bg/border — clipped
			for _, fl := range f.Floats {       // the float layer — clipped
				start := len(dst)
				dst = fl.AppendItems(dst)
				if fl.RelOffsetX != 0 || fl.RelOffsetY != 0 {
					translateItems(dst, start, fl.RelOffsetX, fl.RelOffsetY)
				}
			}
			dst = f.appendContent(dst) // in-flow inline content + images — clipped
			dst = f.appendPositioned(dst, true)
			dst = append(dst, layout.Item{Kind: layout.ClipPopKind})
			dst = f.appendPositioned(dst, false)
			return dst
		}
		// Non-clipping stacking context / BFC: the prior 4-phase order (decorations →
		// floats → in-flow content → positioned layer), unchanged.
		dst = f.appendDecorations(dst)
		for _, fl := range f.Floats {
			start := len(dst)
			dst = fl.AppendItems(dst)
			if fl.RelOffsetX != 0 || fl.RelOffsetY != 0 {
				translateItems(dst, start, fl.RelOffsetX, fl.RelOffsetY)
			}
		}
		dst = f.appendContent(dst)
		dst = f.appendPositioned(dst, false)
		return dst
	}
	// Non-BFC, non-stacking fragment: paint self, then recurse (normal tree order),
	// skipping floated AND positioned children (painted by the owning stacking context's
	// float / positioned layers). This is the per-subtree behavior the context phases
	// invoke; it is unchanged from the prior single-pass AppendItems except for those
	// skips.
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

// appendPositioned emits the fragment's positioned layer (each entry painted fully via
// its own AppendItems, with a relatively-positioned entry's RelOffset applied over its
// emitted range). It paints positioned descendants in document order (the minimal
// z-index cut).
//
// onlyCBOwned selects which subset to emit, and is only meaningful when f.Clips:
//   - f.Clips == false: the whole layer is emitted in one call; onlyCBOwned is ignored.
//     This is the non-clipping path and is byte-identical to the prior single loop.
//   - f.Clips == true: the clipping path calls this TWICE — once with onlyCBOwned=true
//     (inside the clip bracket: entries whose containing block IS f, PositionedInfo[i].CBOwned
//     true) and once with onlyCBOwned=false (after ClipPop: the escaped entries, whose
//     CB is an ancestor). So CB-owned descendants are clipped and escaped ones are not.
//
// A missing/short PositionedInfo entry counts as not-CB-owned (false), the safe default.
func (f *Fragment) appendPositioned(dst []layout.Item, onlyCBOwned bool) []layout.Item {
	for i, pf := range f.Positioned {
		if f.Clips {
			owned := i < len(f.PositionedInfo) && f.PositionedInfo[i].CBOwned
			if owned != onlyCBOwned {
				continue
			}
		}
		start := len(dst)
		dst = pf.AppendItems(dst)
		if pf.RelOffsetX != 0 || pf.RelOffsetY != 0 {
			translateItems(dst, start, pf.RelOffsetX, pf.RelOffsetY)
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

// translateItems shifts every item in dst[start:] by (dx,dy), mutating their XPt/YPt
// in place. It applies a relatively-positioned fragment's paint offset to the items
// the fragment (and its subtree, incl. any abs-pos descendant on its Positioned
// layer) just emitted via AppendItems — so the whole positioned subtree rides the
// relative shift. Every coordinate-bearing item kind carries XPt/YPt
// (Background/Border/Glyph/Image), so a uniform per-item translate is exact. This
// keeps AppendItems a pure read of the fragment tree: only the freshly-appended dst
// items are moved, never a Fragment.
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
		}
	}
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
