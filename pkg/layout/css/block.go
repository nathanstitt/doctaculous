package css

import (
	"context"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
	layoutfont "github.com/nathanstitt/doctaculous/pkg/layout/font"
)

// Engine lays out a cssbox tree into a positioned fragment tree at a fixed
// viewport width (the single-tall-page model). Safe for concurrent use: its only
// shared state is the face cache (itself concurrent). Build with New.
//
// The engine implements the block formatting context (this file) and delegates
// inline-level content to the inline formatting context (inline.go); the two meet
// at layoutInline, the documented hook a block box establishing an inline
// formatting context calls.
type Engine struct {
	faces *layoutfont.FaceCache
	logf  func(string, ...any)
}

// New returns an Engine that resolves fonts through faces and logs unsupported or
// degraded cases through logf. A nil faces builds a fresh cache; a nil logf is a
// no-op, so callers need not supply either.
func New(faces *layoutfont.FaceCache, logf func(string, ...any)) *Engine {
	if faces == nil {
		faces = layoutfont.NewFaceCache()
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Engine{faces: faces, logf: logf}
}

// Layout lays out root at viewportW points and returns a single tall page sized
// viewportW × content-height. It honors ctx cancellation (checked between block
// children, like the flat engine checks between blocks) and never panics on
// malformed input: a recover at the page boundary returns whatever was built —
// at minimum a single empty page.
func (e *Engine) Layout(ctx context.Context, root *cssbox.Box, viewportW float64) (pages *layout.Pages, err error) {
	defer func() {
		if r := recover(); r != nil {
			e.logf("css layout: recovered from panic: %v", r)
			pages = &layout.Pages{Pages: []layout.Page{{WidthPt: viewportW, HeightPt: 0}}}
			err = nil
		}
	}()

	frag := e.layoutTree(ctx, root, viewportW)
	if frag == nil {
		return &layout.Pages{Pages: []layout.Page{{WidthPt: viewportW, HeightPt: 0}}}, nil
	}
	// The content height is the root fragment's bottom edge in page space. The root
	// is laid out at the ICB origin (0,0) with a zero outer margin (the ICB has no
	// margins to collapse against), so its border-box bottom is the page height.
	contentH := frag.Y + frag.H
	page := frag.Page(viewportW, contentH)
	return &layout.Pages{Pages: []layout.Page{page}}, nil
}

// layoutTree establishes the initial containing block (ICB) — width viewportW,
// origin (0,0), height auto — and lays out root as a block within it, returning
// the root's positioned border-box fragment (or nil for a degenerate root). It is
// the seam Layout flattens and tests assert geometry against directly, before the
// flatten to a layout.Page.
func (e *Engine) layoutTree(ctx context.Context, root *cssbox.Box, viewportW float64) *Fragment {
	if root == nil {
		return nil
	}
	res := e.layoutBlock(ctx, root, viewportW, 0, 0)
	return res.frag
}

// blockResult is the outcome of laying out one block box: its positioned
// border-box fragment plus its used (and collapse-resolved) top and bottom
// margins, which the caller folds into adjacent-sibling and parent/child margin
// collapsing. The margins are reported separately from the fragment because a
// fragment carries only its border box — margins live outside it.
type blockResult struct {
	frag         *Fragment
	marginTop    float64 // used top margin, already folded with the first child's per collapse-through
	marginBottom float64 // used bottom margin, already folded with the last child's per collapse-through
}

// layoutBlock lays out block box b into a containing block of width cbWidth whose
// left content edge is at originX, with the box's top margin edge at page-space y
// marginTopEdgeY. It returns the positioned border-box fragment and the box's used
// top/bottom margins (for the caller's margin collapsing).
//
// The fragment's X/Y are the border-box top-left: X = originX + usedMarginLeft,
// Y = marginTopEdgeY + result.marginTop. Children are positioned absolutely in
// page space within the fragment.
func (e *Engine) layoutBlock(ctx context.Context, b *cssbox.Box, cbWidth, originX, marginTopEdgeY float64) blockResult {
	ed := usedEdges(b, cbWidth)

	// Resolve the content width the children flow into, then the border-box width.
	contentW := resolveContentWidth(b, cbWidth, ed)
	borderW := contentW + ed.pL + ed.pR + ed.bL + ed.bR

	// The left edge of the content box in page space. The border-box left is
	// originX+mL; content sits inside the left border+padding.
	contentX := originX + ed.mL + ed.bL + ed.pL

	// Lay out the interior, producing child fragments / inline lines in a *local*
	// frame whose content-box top is local Y 0, plus the collapsing facts.
	in := e.layoutInterior(ctx, b, contentW, contentX)

	// Resolve the box's own collapse-resolved top/bottom margins and the offset of
	// the content-box top from the border-box top's "natural" position.
	//
	// Parent/first-child collapse-through: when this box has no top border and no
	// top padding, its top margin collapses with the first in-flow child's top
	// margin, and that child sits flush with the content-box top (its margin passes
	// through). Otherwise the child's top margin is laid inside the content box as a
	// leading gap. The bottom is symmetric, but only collapses through when the
	// box's height is auto (a fixed height interposes content between the last child
	// and the bottom edge).
	//
	// Implemented collapsing scope: adjacent siblings (layoutBlockChildren) plus
	// parent/first-child and parent/last-child through zero border/padding (here).
	// Out of scope this PR (no special handling): collapse-through of an empty
	// zero-height block's own top and bottom margins, clearance from floats, and the
	// min-height × collapse-through interaction.
	// A box that establishes a new block formatting context (an inline-block here)
	// does not collapse its margins with its in-flow children — its top/bottom
	// margins stay solid and the children's leading/trailing margins are interior.
	newBFC := establishesNewBFC(b)

	marginTop := ed.mT
	leadingGap := in.leadingMargin
	if !newBFC && ed.bT == 0 && ed.pT == 0 {
		marginTop = collapseMargins(ed.mT, in.leadingMargin)
		leadingGap = 0
	}

	contentH := in.contentHeight + leadingGap

	heightAuto := isHeightAuto(b)
	marginBottom := ed.mB
	if heightAuto {
		if !newBFC && ed.bB == 0 && ed.pB == 0 {
			marginBottom = collapseMargins(ed.mB, in.trailingMargin)
		} else {
			contentH += in.trailingMargin
		}
	} else {
		// A fixed height interrupts bottom collapse-through and overrides the
		// content-derived height; the trailing child margin is then interior space
		// already captured by the fixed box, so it does not extend the box.
		contentH = resolveFixedHeight(b, cbWidth, ed)
	}
	if contentH < 0 {
		contentH = 0
	}

	borderH := contentH + ed.pT + ed.pB + ed.bT + ed.bB

	// Position: border-box top-left in page space.
	borderX := originX + ed.mL
	borderY := marginTopEdgeY + marginTop
	contentTopY := borderY + ed.bT + ed.pT

	// Shift the interior from its local content-top-0 frame into page space. The
	// leading gap (non-collapsing top margin) pushes content down inside the box.
	shiftFragments(in.children, contentTopY+leadingGap)
	shiftLines(in.lines, contentTopY+leadingGap)

	frag := &Fragment{
		X: borderX, Y: borderY, W: borderW, H: borderH,
		Background: b.Style.BackgroundColor,
		Children:   in.children,
		Lines:      in.lines,
		DebugTag:   debugTag(b),
	}
	frag.Border[layout.EdgeTop] = BorderEdge{Width: ed.bT, Color: b.Style.BorderTopColor, Style: mapBorderStyle(b.Style.BorderTopStyle)}
	frag.Border[layout.EdgeRight] = BorderEdge{Width: ed.bR, Color: b.Style.BorderRightColor, Style: mapBorderStyle(b.Style.BorderRightStyle)}
	frag.Border[layout.EdgeBottom] = BorderEdge{Width: ed.bB, Color: b.Style.BorderBottomColor, Style: mapBorderStyle(b.Style.BorderBottomStyle)}
	frag.Border[layout.EdgeLeft] = BorderEdge{Width: ed.bL, Color: b.Style.BorderLeftColor, Style: mapBorderStyle(b.Style.BorderLeftStyle)}

	return blockResult{frag: frag, marginTop: marginTop, marginBottom: marginBottom}
}

// interior is the laid-out content of a block box in a local frame whose
// content-box top is at local Y 0: positioned child fragments (or inline lines)
// plus the margin-collapsing facts the parent needs. contentHeight is the local Y
// of the bottom of the in-flow content, excluding the collapsible leading and
// trailing child margins (which the parent decides whether to render or collapse).
type interior struct {
	children       []*Fragment
	lines          []LineFragment
	contentHeight  float64
	leadingMargin  float64 // first in-flow child's top margin (block flow only)
	trailingMargin float64 // last in-flow child's bottom margin (block flow only)
}

// layoutInterior lays out b's children into a local frame (content-box top at 0)
// according to b's formatting context. contentW is the content width children flow
// into; contentX is the page-space x of the content box's left edge (children that
// establish their own page-space position, i.e. blocks, use it directly).
func (e *Engine) layoutInterior(ctx context.Context, b *cssbox.Box, contentW, contentX float64) interior {
	switch b.Formatting {
	case cssbox.InlineFC:
		// Inline-level children: hand off to the inline formatting context. The hook
		// returns line fragments already positioned in page-space X (at contentX) but
		// in the local content-top-0 frame for Y; block layout shifts them into place.
		// Any atomic inline boxes (inline-block / replaced) come back as child
		// fragments in the same frame, to attach as fragment children so they paint.
		lines, h, atomics := e.layoutInline(ctx, b, contentW, 0, contentX)
		return interior{lines: lines, children: atomics, contentHeight: h}
	case cssbox.BlockFC:
		return e.layoutBlockChildren(ctx, b, contentW, contentX)
	default:
		// TableFC / FlexFC / GridFC: their real layout algorithms are later
		// sub-projects. Degrade to block normal flow so the children still position
		// and paint (per the degradation contract: the box arrives with its true
		// Formatting; the fallback is at this layout stage).
		e.logf("css layout: %v not yet implemented; falling back to block normal flow", b.Formatting)
		return e.layoutBlockChildren(ctx, b, contentW, contentX)
	}
}

// layoutBlockChildren stacks b's block-level children vertically in a local frame
// whose content-box top is local Y 0, collapsing margins between adjacent siblings.
// The first child's border top sits at local Y 0; its top margin is reported as
// leadingMargin for the parent to collapse or render. Each subsequent child's
// border top sits at the previous child's border bottom plus their collapsed
// margin. The last child's bottom margin is reported as trailingMargin.
//
// Each child is laid out at a provisional origin and then shifted into place using
// its own *reported* top margin (which already folds in any nested
// parent/first-child collapse-through), so the gap arithmetic stays correct for
// deeply collapsing subtrees, not just leaf children.
func (e *Engine) layoutBlockChildren(ctx context.Context, b *cssbox.Box, contentW, contentX float64) interior {
	var (
		out        []*Fragment
		prevBottom float64 // previous in-flow sibling's reported bottom margin
		prevBorder float64 // previous in-flow sibling's border-box bottom (local Y)
		leading    float64
		trailing   float64
		first      = true
	)
	for _, child := range b.Children {
		if err := ctx.Err(); err != nil {
			// Cancellation: stop adding children but return what we have (the caller
			// is the Layout recover/return path; we degrade rather than propagate
			// here to keep partial output renderable).
			break
		}
		if !child.Kind.IsBlockLevel() {
			// The box-gen invariant guarantees a block container's children are all
			// block-level or all inline-level, so a stray inline here is unexpected;
			// skip it defensively rather than misplacing it.
			e.logf("css layout: unexpected inline-level child in block formatting context; skipping")
			continue
		}

		// Lay the child out at a provisional margin edge of 0; its border top then
		// sits at res.marginTop, which we now know and use to place it exactly.
		res := e.layoutBlock(ctx, child, contentW, contentX, 0)

		var borderTop float64 // desired local Y of this child's border-box top
		if first {
			borderTop = 0 // first child's border top defines the content-box top
			leading = res.marginTop
			first = false
		} else {
			borderTop = prevBorder + collapseMargins(prevBottom, res.marginTop)
		}
		// The child currently sits with its border top at res.marginTop (margin edge
		// was 0); shift it so its border top lands at borderTop.
		shiftFragment(res.frag, borderTop-res.marginTop)
		out = append(out, res.frag)

		prevBorder = res.frag.Y + res.frag.H
		prevBottom = res.marginBottom
		trailing = res.marginBottom
	}
	return interior{children: out, contentHeight: prevBorder, leadingMargin: leading, trailingMargin: trailing}
}

// The inline formatting context (layoutInline) lives in inline.go; it is the hook
// the InlineFC case of layoutInterior calls to lay out a block's inline-level
// children into LineFragments and atomic child fragments.

// --- box-model helpers (the inline formatting context reuses resolveLen and
// usedEdges) ---

// edges holds a box's used (resolved, content-box-relative) margin, border, and
// padding widths for all four sides, in points.
type edges struct {
	mT, mR, mB, mL float64
	bT, bR, bB, bL float64
	pT, pR, pB, pL float64
}

// usedEdges resolves the margins, border widths, and paddings of box b against a
// containing block of width cbWidth. Percentages on every edge resolve against
// cbWidth (CSS resolves vertical padding/margin percentages against the containing
// block's width too). Padding and border widths clamp to non-negative; margins may
// be negative. A border edge's used width is zero unless its style draws (a
// border-style of none/"" yields zero used width regardless of the declared width).
// Auto margins compute to 0 in this PR (horizontal margin:auto centering is
// deferred).
func usedEdges(b *cssbox.Box, cbWidth float64) edges {
	fs := b.Style.FontSizePt
	margin := func(l gcss.Length) float64 {
		// Auto margins -> 0 (centering deferred); other values may be negative.
		v, isAuto := resolveLen(l, fs, cbWidth)
		if isAuto {
			return 0
		}
		return v
	}
	pad := func(l gcss.Length) float64 {
		v, _ := resolveLen(l, fs, cbWidth)
		if v < 0 {
			v = 0
		}
		return v
	}
	border := func(width gcss.Length, style string) float64 {
		if mapBorderStyle(style) == layout.BorderNone {
			return 0
		}
		v, _ := resolveLen(width, fs, cbWidth)
		if v < 0 {
			v = 0
		}
		return v
	}
	s := &b.Style
	return edges{
		mT: margin(s.MarginTop), mR: margin(s.MarginRight), mB: margin(s.MarginBottom), mL: margin(s.MarginLeft),
		pT: pad(s.PaddingTop), pR: pad(s.PaddingRight), pB: pad(s.PaddingBottom), pL: pad(s.PaddingLeft),
		bT: border(s.BorderTopWidth, s.BorderTopStyle),
		bR: border(s.BorderRightWidth, s.BorderRightStyle),
		bB: border(s.BorderBottomWidth, s.BorderBottomStyle),
		bL: border(s.BorderLeftWidth, s.BorderLeftStyle),
	}
}

// establishesNewBFC reports whether b establishes a new block formatting context,
// which suppresses margin collapsing between the box and its in-flow children. In
// the supported subset only an inline-block does (its interior is an independent
// BFC); floats and overflow≠visible — the other BFC triggers — are not modeled yet.
func establishesNewBFC(b *cssbox.Box) bool {
	return b.Display == cssbox.DisplayInlineBlock
}

// isAnonymous reports whether b is an engine-generated anonymous box. Anonymous
// boxes (inline-in-block wrappers and block-in-inline splits) carry a zero-value
// ComputedStyle, NOT the CSS initial style: e.g. its Width/MaxWidth Unit is the
// zero UnitPx, which would read as "width:0/max-width:0" rather than the auto/none
// an anonymous box must have. Box-model resolution therefore treats an anonymous
// box's own dimensions as their defaults (auto width/height, no min/max, zero
// margins/padding/borders — the last three already follow from the zero style).
func isAnonymous(b *cssbox.Box) bool {
	return b.Kind == cssbox.BoxAnonBlock || b.Kind == cssbox.BoxAnonInline
}

// resolveContentWidth resolves the content-box width of block box b in a containing
// block of width cbWidth, given its already-resolved edges ed. It implements the
// CSS block width algorithm for the supported subset: an auto width fills the
// containing block minus the horizontal margins/borders/paddings; a fixed width is
// used directly (content-box) or de-padded (border-box). The result is then clamped
// by min-width/max-width (CSS 10.4) in content-box terms and floored at 0.
func resolveContentWidth(b *cssbox.Box, cbWidth float64, ed edges) float64 {
	fs := b.Style.FontSizePt
	horiz := ed.mL + ed.mR + ed.bL + ed.bR + ed.pL + ed.pR
	insets := ed.pL + ed.pR + ed.bL + ed.bR // padding+border subtracted under border-box
	borderBox := b.Style.BoxSizing == "border-box"

	// An anonymous box has no specified dimensions: it fills its container (its
	// edges are all zero, so this is just cbWidth) with no min/max clamp.
	if isAnonymous(b) {
		w := cbWidth - horiz
		if w < 0 {
			w = 0
		}
		return w
	}

	var contentW float64
	if w, isAuto := resolveLen(b.Style.Width, fs, cbWidth); isAuto {
		contentW = cbWidth - horiz
	} else if borderBox {
		contentW = w - insets
	} else {
		contentW = w
	}

	// min/max clamp. Both describe the same box edge as width does (border box under
	// border-box sizing), so convert each to content-box terms by subtracting the
	// insets before comparing — keeping the comparison consistent with contentW.
	if maxL := b.Style.MaxWidth; maxL.Unit != gcss.UnitAuto { // UnitAuto models "none"
		maxW, _ := resolveLen(maxL, fs, cbWidth)
		if borderBox {
			maxW -= insets
		}
		if contentW > maxW {
			contentW = maxW
		}
	}
	// min-width default is 0 (UnitPx zero); resolve and apply unconditionally.
	minW, _ := resolveLen(b.Style.MinWidth, fs, cbWidth)
	if borderBox {
		minW -= insets
	}
	if contentW < minW {
		contentW = minW
	}
	if contentW < 0 {
		contentW = 0
	}
	return contentW
}

// isHeightAuto reports whether b's height is auto (content-derived). An anonymous
// box is always auto-height (its zero-value style would otherwise read as
// height:0; see isAnonymous).
func isHeightAuto(b *cssbox.Box) bool {
	if isAnonymous(b) {
		return true
	}
	_, isAuto := resolveLen(b.Style.Height, b.Style.FontSizePt, 0)
	return isAuto
}

// resolveFixedHeight resolves a block's fixed (non-auto) content height against a
// containing block of width cbWidth (CSS resolves height percentages against the
// containing block width in this engine's single-axis model), converting for
// box-sizing and clamping by min-height/max-height. The percentage basis is cbWidth
// to match how the vertical margins/paddings resolve; a height percentage against an
// auto-height containing block is an edge case deferred with the rest of vertical
// percentage subtleties.
func resolveFixedHeight(b *cssbox.Box, cbWidth float64, ed edges) float64 {
	fs := b.Style.FontSizePt
	insets := ed.pT + ed.pB + ed.bT + ed.bB
	borderBox := b.Style.BoxSizing == "border-box"

	h, _ := resolveLen(b.Style.Height, fs, cbWidth)
	if borderBox {
		h -= insets
	}
	if maxL := b.Style.MaxHeight; maxL.Unit != gcss.UnitAuto {
		maxH, _ := resolveLen(maxL, fs, cbWidth)
		if borderBox {
			maxH -= insets
		}
		if h > maxH {
			h = maxH
		}
	}
	minH, _ := resolveLen(b.Style.MinHeight, fs, cbWidth)
	if borderBox {
		minH -= insets
	}
	if h < minH {
		h = minH
	}
	if h < 0 {
		h = 0
	}
	return h
}

// resolveLen resolves a CSS length to points against a font size (for em) and a
// percentage basis (for %). px and pt are treated 1:1 (consistent with the
// cascade's px→pt handling). isAuto is true for the auto keyword, with val 0.
func resolveLen(l gcss.Length, fontSizePt, pctBasis float64) (val float64, isAuto bool) {
	switch l.Unit {
	case gcss.UnitPx, gcss.UnitPt:
		return l.Value, false
	case gcss.UnitEm:
		return l.Value * fontSizePt, false
	case gcss.UnitPercent:
		return l.Value / 100 * pctBasis, false
	case gcss.UnitAuto:
		return 0, true
	default:
		return l.Value, false
	}
}

// collapseMargins collapses a set of adjoining margins per CSS 8.3.1: the collapsed
// margin is the largest of the non-negative margins plus the most-negative of the
// negative margins. With only non-negative inputs it is their max; with mixed signs
// it is maxPositive + minNegative; with only negatives it is the most negative.
func collapseMargins(margins ...float64) float64 {
	maxPos, minNeg := 0.0, 0.0
	for _, m := range margins {
		if m > maxPos {
			maxPos = m
		}
		if m < minNeg {
			minNeg = m
		}
	}
	return maxPos + minNeg
}

// mapBorderStyle maps a CSS border-style keyword to the layout border style. Unknown
// values, the empty string, and "none" map to BorderNone (no border drawn, and zero
// used border width).
func mapBorderStyle(s string) layout.BorderStyle {
	switch s {
	case "solid":
		return layout.BorderSolid
	case "dashed":
		return layout.BorderDashed
	case "dotted":
		return layout.BorderDotted
	case "double":
		return layout.BorderDouble
	default:
		return layout.BorderNone
	}
}

// shiftFragments translates a slice of fragments (laid out in a local frame whose
// content-box top is local Y 0) down by dy into page space, recursing into
// descendants so the whole subtree moves together.
func shiftFragments(frags []*Fragment, dy float64) {
	if dy == 0 {
		return
	}
	for _, f := range frags {
		shiftFragment(f, dy)
	}
}

// shiftFragment translates one fragment and its descendants (children and inline
// line baselines) by dy. Block children were positioned in page-space X already, so
// only Y moves.
func shiftFragment(f *Fragment, dy float64) {
	f.Y += dy
	for li := range f.Lines {
		f.Lines[li].BaselineY += dy
	}
	for _, c := range f.Children {
		shiftFragment(c, dy)
	}
}

// shiftLines translates inline line baselines by dy (the inline content of the box
// being shifted; its descendant fragments move via shiftFragments).
func shiftLines(lines []LineFragment, dy float64) {
	if dy == 0 {
		return
	}
	for li := range lines {
		lines[li].BaselineY += dy
	}
}

// debugTag derives a stable, paint-irrelevant label for a fragment to aid test
// lookup and debugging. The cssbox.Box does not carry the source element tag/id, so
// this is the box's structural kind and display intent rather than an element name;
// tests navigate the fragment tree positionally, not by this tag.
func debugTag(b *cssbox.Box) string {
	switch b.Kind {
	case cssbox.BoxAnonBlock:
		return "anon-block"
	case cssbox.BoxAnonInline:
		return "anon-inline"
	case cssbox.BoxReplaced:
		return "replaced"
	case cssbox.BoxText:
		return "text"
	case cssbox.BoxInline:
		return "inline"
	default:
		return "block"
	}
}
