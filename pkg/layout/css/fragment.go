package css

import (
	"image"
	"image/color"
	"sort"

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
	X, Y, W, H float64         // the BORDER box rectangle in page space
	Background color.RGBA      // zero-alpha => no background fill
	Border     [4]BorderEdge   // indexed by layout.EdgeSide (EdgeTop, EdgeRight, EdgeBottom, EdgeLeft)
	Lines      []LineFragment  // inline content (set for a box establishing an inline formatting context)
	Children   []*Fragment     // child box fragments (block children; atomic inline boxes)
	Image      *ImageContent   // decoded replaced-element image (set for a replaced box), painted in the content box
	Control    *ControlContent // form-control widget (set for a control replaced box), painted in the content box
	DebugTag   string          // optional label for test lookup; not used in paint

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
func (f *Fragment) AppendItems(dst []layout.Item) []layout.Item {
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
				Fit:  f.Image.Fit,
				PosX: f.Image.PosX, PosY: f.Image.PosY,
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
		case layout.ClipPushKind:
			// A clip established by the offset box itself (overflow≠visible) or by an
			// overflow box inside its subtree rides the paint-time offset with the content
			// it clips. The range [start:] excludes the fixed ancestor-clip pushes a
			// positioned entry passed through (appendBand emits those BEFORE start), so only
			// the box's own / descendant clips move — exactly what CSS 9.4.3 requires.
			dst[i].Rule.XPt += dx
			dst[i].Rule.YPt += dy
		case layout.ClipPopKind:
			// No coordinates; nothing to translate (paired with its ClipPushKind).
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
