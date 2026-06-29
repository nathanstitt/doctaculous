package css

import (
	"context"
	"image/color"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
	"github.com/nathanstitt/doctaculous/pkg/layout/inline"
)

// cssDefaultLineMult is the line-height multiplier applied to the natural font
// height when line-height is "normal"/auto. It mirrors pkg/layout's unexported
// defaultLineMult (~1.15, Word's default leading) so the CSS and flat engines
// agree on auto line spacing; it is redeclared here because the flat engine's
// constant is unexported and outside this package.
const cssDefaultLineMult = 1.15

// layoutInline is the inline-formatting-context hook a block box establishing an
// inline formatting context calls. It lays out b's inline-level children into line
// fragments within contentW points, with line baselines measured from the local
// content-box top (contentTopY, which block layout passes as 0 and later shifts
// into page space) and glyph/atomic X already in page space (offset by contentX so
// the caller's vertical-only shift lands them correctly).
//
// It returns the positioned lines, the total inline content height (the sum of the
// line heights), and the atomic child fragments (inline-block / replaced boxes) the
// caller must attach to the box's Fragment.Children so they paint. The atomic
// fragments are in the same local content-top-0 / page-space-X frame as the lines.
//
// Inline-box decoration (backgrounds/borders/padding on a BoxInline / BoxAnonInline)
// is deferred: this pass renders inline TEXT plus atomic inline-blocks/replaced
// boxes; an inline element box contributes only its text leaves' glyphs.
func (e *Engine) layoutInline(ctx context.Context, b *cssbox.Box, contentW, contentTopY, contentX, bandOriginY float64, fc *floatContext) (lines []LineFragment, height float64, atomics []*Fragment) {
	// 1. Gather inline-level descendants into styled runs (atomics laid out eagerly).
	var runs []inline.Run
	e.gatherInlineRuns(ctx, b, contentW, &runs, &atomics)
	if len(runs) == 0 {
		return nil, 0, nil
	}

	// 2. Shape once (width-independent).
	glyphs := inline.Shape(e.faces, runs, e.logf)
	align := effectiveTextAlign(b)

	// 3. Break + position one line at a time against the float-narrowed band. Float
	//    queries use the BFC-root frame (bandOriginY + the local pen offset); emitted
	//    glyph/line Y stays in the local content-top-0 frame (the block stacker shifts
	//    the whole interior into page space later). Glyph X is absolute page-space
	//    already (availLeft and contentX are absolute), so X is never shifted.
	//
	//    The float edges are CLAMPED to THIS box's own content box [contentX,
	//    contentX+contentW]: the float context spans the whole BFC (its cbLeft/cbRight
	//    are the BFC root's content box, which for a non-BFC box narrower than its
	//    containing block — e.g. a width:200px <p> in a 1280px page — is WIDER than this
	//    box). Without the clamp such a box would lay text out to the BFC width, not its
	//    own. When no float intrudes, the clamp pins avail to exactly [contentX,
	//    contentX+contentW] and the per-line BreakNext reproduces a single fixed-width
	//    Break (the non-float invariant). When a float intrudes, its edge is inside the
	//    box, so the clamp leaves the float-narrowing intact.
	penY := contentTopY
	rest := glyphs
	lineHGuess := e.lineHeightGuess(b)             // representative band height for the float-band queries
	cbLeft, cbRight := contentX, contentX+contentW // this box's own content box (clamp bounds)
	for len(rest) > 0 {
		bandY := bandOriginY + (penY - contentTopY) // this line's Y in the BFC-root frame
		availLeft := fc.leftEdge(bandY, lineHGuess)
		if availLeft < cbLeft {
			availLeft = cbLeft
		}
		availRight := fc.rightEdge(bandY, lineHGuess)
		if availRight > cbRight {
			availRight = cbRight
		}
		avail := availRight - availLeft
		if avail < 1 {
			// Fully blocked band (opposite-side floats meet). Drop below the shallowest
			// float and retry rather than emitting a zero-width line.
			next := fc.nextDropY(bandY, lineHGuess)
			if next > bandY {
				penY += next - bandY
				continue
			}
			// Non-advancing band (shouldn't happen): fall back to full width to avoid a
			// spin.
			availLeft, avail = contentX, contentW
		}

		var lineGlyphs []inline.Glyph
		lineGlyphs, rest = inline.BreakNext(rest, avail)
		line := inline.MakeLine(lineGlyphs)

		lh := e.effectiveLineHeight(b, line)
		baselineY := penY + ascentOfLine(line)

		spaceCount := inline.CountSpaces(line.Glyphs)
		isLast := len(rest) == 0
		// availLeft is the absolute page-space left for this line; avail is its width.
		p := inline.Place(align, availLeft, avail, line.WidthPt, spaceCount, isLast)

		var emitted []GlyphFragment
		x := p.StartX
		for gi := range line.Glyphs {
			g := &line.Glyphs[gi]
			switch {
			case g.Atomic != nil:
				// Place the atom: its border-box left = x + the atom's left margin (the
				// margin is part of the advance); its top = baselineY - BaselinePt, i.e.
				// bottom-aligned so the atom's baseline rests on the line baseline.
				if frag, ok := g.Atomic.Ref.(*Fragment); ok && frag != nil {
					translateFragment(frag, (x+g.Atomic.MarginLeftPt)-frag.X, (baselineY-g.Atomic.BaselinePt)-frag.Y)
				}
			case g.Outline != nil:
				emitted = append(emitted, GlyphFragment{
					Outline: g.Outline, X: x, SizePt: g.SizePt,
					Color: color.RGBA{R: g.Color.R, G: g.Color.G, B: g.Color.B, A: g.Color.A},
				})
			}
			x += g.Advance
			if g.Space {
				x += p.ExtraPerSpace
			}
		}

		lines = append(lines, LineFragment{BaselineY: baselineY, Glyphs: emitted})
		penY += lh
	}

	return lines, penY - contentTopY, atomics
}

// lineHeightGuess estimates a line's height for the float band query, before the
// line's glyphs are measured. It uses the box's resolved line-height when fixed
// (px/pt/em); for "normal"/auto it falls back to the font size times the default
// multiplier — a representative value (the float bands are coarse, so an exact
// per-line height is unnecessary, and the actual line height is recomputed once the
// line is built). For an anonymous block it reads the inherited line-height/font-size
// from a text leaf, mirroring effectiveLineHeight.
func (e *Engine) lineHeightGuess(b *cssbox.Box) float64 {
	lh := b.Style.LineHeight
	fs := b.Style.FontSizePt
	if isAnonymous(b) {
		if l, f, ok := firstInlineLineHeight(b); ok {
			lh, fs = l, f
		}
	}
	switch lh.Unit {
	case gcss.UnitPx, gcss.UnitPt:
		return lh.Value
	case gcss.UnitEm:
		return lh.Value * fs
	default:
		if fs <= 0 {
			fs = 12 // defensive: a sane default if the font size is unset
		}
		return fs * cssDefaultLineMult
	}
}

// gatherInlineRuns walks b's inline-level descendants depth-first, appending one
// inline.Run per text run / atomic box to *runs and any laid-out atomic fragment to
// *atomics. contentW is the inline content width atomics resolve percentages and
// auto widths against. It recurses into inline element boxes (whose text leaves
// already carry the correct cascaded style) and never panics on an unexpected box.
func (e *Engine) gatherInlineRuns(ctx context.Context, b *cssbox.Box, contentW float64, runs *[]inline.Run, atomics *[]*Fragment) {
	for _, child := range b.Children {
		switch {
		case child.Kind == cssbox.BoxText:
			// A text box carries the parent's inherited font/color/size; only those
			// fields are meaningful (see makeTextBox) — never its box-level fields.
			*runs = append(*runs, inline.Run{
				Text:   child.Text,
				Family: child.Style.FontFamily,
				Bold:   child.Style.Bold,
				Italic: child.Style.Italic,
				SizePt: child.Style.FontSizePt,
				Color:  child.Style.Color,
			})
		case child.Kind == cssbox.BoxReplaced:
			// A replaced inline (e.g. <img>, including an inline-block <img>) sizes via
			// the replaced-element algorithm (intrinsic size + aspect ratio, clamped),
			// decoding its image; its fragment carries the image for paint. This case
			// precedes the inline-block case so a replaced inline-block is sized as a
			// replaced box (not laid out as an empty block container).
			w, h := e.replacedUsedSize(ctx, child, contentW)
			frag := e.replacedFragment(ctx, child, w, h, 0, 0, contentW)
			*atomics = append(*atomics, frag)
			*runs = append(*runs, atomicRunFor(child, frag, contentW))
		case child.Display == cssbox.DisplayInlineBlock || child.Display == cssbox.DisplayInlineFlex || child.Display == cssbox.DisplayInlineGrid:
			// An inline-block, inline-flex, or inline-grid establishes a new BFC; lay it
			// out as a block at its resolved width and carry its border box as an atomic
			// unit. It gets a fresh float context (its internal floats stay self-contained)
			// and bandOriginY 0; the IFC then positions the whole atom (subtree + any
			// internal floats) on the line via translateFragment. For inline-flex the
			// layoutBlock→layoutInterior→FlexFC path routes to layoutFlex; for inline-grid
			// it routes to layoutGrid; the outer atom mechanism is identical to inline-block.
			// Positioning inside an inline atom is self-contained: give it a fresh
			// throwaway positionedContext and the page sentinel as its abs-pos CB, then
			// resolve that context immediately against the atom's own provisional frame.
			// Relative/abs positioning of inline ATOMS (and abs descendants of an
			// inline-block) is out of scope this slice (the atom is repositioned on the
			// line by translateFragment, which does not move its Positioned layer), so
			// these resolve approximately against the atom's provisional box — a
			// documented limitation; block-level positioning is exact.
			// CSS 10.3.9 shrink-to-fit: a width:auto inline-block wraps its content
			// rather than filling the parent's content width. Compute the shrink-to-fit
			// width and lay the atom out as if its containing block were that wide, so
			// resolveContentWidth's auto branch (cbWidth - horiz) yields the shrink-to-fit
			// content width. (inline-flex/inline-grid keep their own sizing — inline-grid
			// already shrink-to-fits via its intrinsic width; only inline-BLOCK is filled
			// here.) A specified width is honored as-is (atomCBWidth stays contentW).
			atomCBWidth := contentW
			if child.Display == cssbox.DisplayInlineBlock {
				atomCBWidth = e.inlineBlockCBWidth(ctx, child, contentW)
			}
			atomPos := &positionedContext{}
			res := e.layoutBlock(ctx, child, atomCBWidth, 0, 0, 0, &floatContext{cbLeft: 0, cbRight: atomCBWidth}, atomPos, posCBOwner{isPage: true})
			frag := res.frag
			e.resolveAbsolute(ctx, atomPos, frag, atomCBWidth, 0)
			*atomics = append(*atomics, frag)
			*runs = append(*runs, atomicRunFor(child, frag, atomCBWidth))
		case child.Kind == cssbox.BoxInline || child.Kind == cssbox.BoxAnonInline:
			// Descend into the inline element box; its text leaves carry the correct
			// cascaded font/color. Inline-box decoration is deferred (see layoutInline).
			e.gatherInlineRuns(ctx, child, contentW, runs, atomics)
		default:
			// A block-level child in an inline formatting context violates the box-gen
			// invariant; skip it defensively rather than misplacing it.
			e.logf("css layout: unexpected non-inline child in inline formatting context; skipping")
		}
	}
}

// inlineBlockCBWidth returns the containing-block width to lay a width:auto inline-block
// out against so it SHRINKS TO FIT its content (CSS 10.3.9) rather than filling the
// parent's content width (the default resolveContentWidth auto behavior). It returns a
// width W such that the auto branch (W - horiz) equals the shrink-to-fit content width
//
//	stf = min( max(min-content, available), max-content )
//
// where available = parentContentW - horiz (the inline-block's own margins/border/padding).
// A specified (non-auto) width is left to resolveContentWidth — this returns parentContentW
// unchanged so the specified width and its min/max clamp apply normally. Percentage widths
// also pass through (resolved against parentContentW). Min/max-content are measured via the
// memoized measure helpers (shared with table/grid sizing), so this adds no committed layout.
func (e *Engine) inlineBlockCBWidth(ctx context.Context, b *cssbox.Box, parentContentW float64) float64 {
	if _, isAuto := resolveLen(b.Style.Width, b.Style.FontSizePt, parentContentW); !isAuto {
		return parentContentW // specified width: normal resolution + clamp
	}
	ed := usedEdges(b, parentContentW)
	horiz := ed.mL + ed.mR + ed.bL + ed.bR + ed.pL + ed.pR
	avail := parentContentW - horiz
	if avail < 0 {
		avail = 0
	}
	maxC := e.measureMaxContent(ctx, b)
	minC := e.measureMinContent(ctx, b)
	stf := avail
	if minC > stf { // never narrower than min-content
		stf = minC
	}
	if maxC < stf { // never wider than max-content
		stf = maxC
	}
	if stf < 0 {
		stf = 0
	}
	return stf + horiz
}

// atomicRunFor builds the inline.Run for an atomic inline-level box (inline-block
// or replaced) whose border-box fragment is frag, resolving its horizontal margins
// against basis. The atom's advance is marginL + borderWidth + marginR, with the
// left margin recorded so the IFC offsets the fragment's border box past it. Negative
// margins are honored (they may pull the atom or following content leftward), matching
// the block flow's margin treatment.
//
// The atom's baseline (BaselinePt, measured from its border-box top) follows CSS 2.1
// §10.8.1 for vertical-align:baseline: it is the baseline of the atom's LAST in-flow
// line box, UNLESS the atom has no in-flow line boxes or its overflow is not visible —
// in which case it is the bottom margin edge (frag.H). So a replaced atom (an <img>: no
// line boxes) and an overflow:hidden inline-block stay bottom-aligned, while a plain
// inline-block with text aligns its text baseline with the surrounding line's baseline
// (rather than resting its whole box on the baseline, which dropped the box too low and
// inflated the line box). Full vertical-align keyword handling (sub/super/top/middle/…)
// is still deferred — this fixes only the default-baseline case.
func atomicRunFor(b *cssbox.Box, frag *Fragment, basis float64) inline.Run {
	ed := usedEdges(b, basis)
	baseline := frag.H // default: bottom margin edge (replaced, overflow≠visible, empty)
	if !clips(b) {
		if by, ok := lastInFlowLineBaseline(frag); ok {
			baseline = by - frag.Y // distance from the border-box top to that baseline
		}
	}
	return inline.Run{Atomic: &inline.AtomicItem{
		WidthPt:      ed.mL + frag.W + ed.mR,
		HeightPt:     frag.H,
		MarginLeftPt: ed.mL,
		BaselinePt:   baseline,
		Ref:          frag,
	}}
}

// lastInFlowLineBaseline returns the page-frame Y of the baseline of the LAST in-flow
// line box in frag's subtree (CSS 2.1 §10.8.1 "the baseline of its last line box in the
// normal flow"), and whether one exists. A fragment that establishes an inline formatting
// context carries its line boxes in Lines; a block-container fragment carries block
// children, so the last line box is found by recursing into its last IN-FLOW child
// (skipping out-of-flow positioned/floated children, which do not contribute a normal-flow
// line box). Returns ok=false for an atom with no in-flow line box (e.g. a replaced image,
// or an empty inline-block), so the caller falls back to the bottom margin edge.
func lastInFlowLineBaseline(frag *Fragment) (float64, bool) {
	if n := len(frag.Lines); n > 0 {
		return frag.Lines[n-1].BaselineY, true
	}
	for i := len(frag.Children) - 1; i >= 0; i-- {
		c := frag.Children[i]
		if c.IsPositioned || c.IsFloat {
			continue
		}
		if by, ok := lastInFlowLineBaseline(c); ok {
			return by, true
		}
	}
	return 0, false
}

// attrPx parses a width/height presentation attribute as a non-negative integer
// pixel count (treated 1:1 as points). A non-numeric or empty value yields 0.
func attrPx(s string) float64 {
	n := 0
	if s == "" {
		return 0
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0 // non-integer (e.g. "50%"): unsupported here, degrade to 0
		}
		n = n*10 + int(r-'0')
	}
	return float64(n)
}

// effectiveTextAlign resolves the alignment for b's inline content as an
// inline.Align. A real element box (BoxBlock / BoxInline) uses its own
// Style.TextAlign. An anonymous block (BoxAnonBlock) has a zero-value Style whose
// TextAlign is "", so the alignment is read from the inherited Style of its first
// descendant text/inline box that carries a non-empty TextAlign (the cascade copies
// text-align onto every inline descendant), falling back to left.
func effectiveTextAlign(b *cssbox.Box) inline.Align {
	if b.Kind == cssbox.BoxAnonBlock {
		return mapTextAlign(firstInlineTextAlign(b))
	}
	return mapTextAlign(b.Style.TextAlign)
}

// firstInlineTextAlign returns the first non-empty TextAlign found walking b's
// inline descendants depth-first, or "" if none carries one.
func firstInlineTextAlign(b *cssbox.Box) string {
	for _, c := range b.Children {
		if c.Style.TextAlign != "" {
			return c.Style.TextAlign
		}
		if a := firstInlineTextAlign(c); a != "" {
			return a
		}
	}
	return ""
}

// firstInlineLineHeight returns the inherited line-height carried by b's first
// BoxText descendant (walking inline descendants depth-first), with its font size
// for em resolution, or ok=false if b has no text leaf. It mirrors
// firstInlineTextAlign: an anonymous block's own Style is zero-valued, so its
// line-height (a CSS-inherited property) must be recovered from a text leaf, which
// carries the cascaded line-height/font-size from the real ancestor (see
// makeTextBox). The first BoxText is authoritative — every text leaf under the same
// anon block shares the inherited value.
func firstInlineLineHeight(b *cssbox.Box) (lh gcss.Length, fontSizePt float64, ok bool) {
	for _, c := range b.Children {
		if c.Kind == cssbox.BoxText {
			return c.Style.LineHeight, c.Style.FontSizePt, true
		}
		if l, fs, found := firstInlineLineHeight(c); found {
			return l, fs, true
		}
	}
	return gcss.Length{}, 0, false
}

// mapTextAlign maps a CSS text-align keyword to the inline core's neutral Align,
// defaulting (empty / unknown / "left") to AlignLeft.
func mapTextAlign(s string) inline.Align {
	switch s {
	case "right":
		return inline.AlignRight
	case "center":
		return inline.AlignCenter
	case "justify":
		return inline.AlignJustify
	default:
		return inline.AlignLeft
	}
}

// effectiveLineHeight computes a line's height from b's line-height and the line's
// own metrics. UnitAuto ("normal") uses the natural height (ascent+descent+gap)
// times cssDefaultLineMult; UnitPx/UnitPt is that exact value in points; UnitEm is
// the value times b's font size (see resolveLineHeight). A line containing atomic
// inline boxes is never shorter than its tallest atomic (which rests bottom-aligned
// on the baseline), so the atomic extent is a floor on every mode. Unitless
// multipliers (e.g. line-height:1.5) are not supported yet: the cascade drops a bare
// number, so they arrive as UnitAuto and degrade to the metrics-based default.
//
// An anonymous box (BoxAnonBlock/BoxAnonInline) is handled specially: it carries a
// zero-value ComputedStyle, so reading its own LineHeight gives Length{0, UnitPx} —
// which the UnitPx case would take literally as height 0, collapsing every line to a
// single baseline. But line-height is a CSS *inherited* property, so the real value
// lives on the anon box's text leaves (makeTextBox copies the cascaded line-height
// and font-size onto them). We therefore recover the inherited line-height (and the
// font size for em resolution) from the first text leaf via firstInlineLineHeight and
// resolve THAT — so e.g. a div with line-height:30px gives its bare-text anon block a
// 30px line box, not the auto default. Only when the anon box has no text leaf at all
// do we fall back to auto. (Same zero-value-Style-is-not-the-CSS-initial-value
// reasoning isAnonymous documents for width/height in block.go.)
func (e *Engine) effectiveLineHeight(b *cssbox.Box, line inline.Line) float64 {
	var h float64
	if isAnonymous(b) {
		if lh, fs, ok := firstInlineLineHeight(b); ok {
			h = resolveLineHeight(lh, fs, line)
		} else {
			h = autoLineHeight(line) // no text leaf: nothing to inherit from
		}
	} else {
		h = resolveLineHeight(b.Style.LineHeight, b.Style.FontSizePt, line)
	}
	if atomic := atomicLineExtent(line); atomic > h {
		h = atomic
	}
	return h
}

// resolveLineHeight turns a line-height Length plus a font size into a line height in
// points, against the line's own metrics: UnitPx/UnitPt is the exact value, UnitEm is
// the value times fontSizePt, and UnitAuto ("normal") or any unsupported unit falls
// back to the natural metrics-based height. Both the real-element path and the
// anonymous-block path (which supplies the inherited line-height/font-size from a text
// leaf) share this so the two stay in lockstep. The atomic-height floor is applied by
// the caller, not here.
func resolveLineHeight(lh gcss.Length, fontSizePt float64, line inline.Line) float64 {
	switch lh.Unit {
	case gcss.UnitPx, gcss.UnitPt:
		return lh.Value
	case gcss.UnitEm:
		return lh.Value * fontSizePt
	default: // UnitAuto / unsupported: metrics × default multiplier
		return autoLineHeight(line)
	}
}

// autoLineHeight is the "normal"/auto line height: the glyph extent
// (ascent+descent) times cssDefaultLineMult, which supplies the leading.
//
// The font's own line GAP is deliberately NOT added: the bundled substitute faces (the
// TeX Gyre family) report an anomalous hhea line gap of ~1.3–1.4 em, so adding it (and
// then multiplying by the 1.15 leading factor) ballooned "normal" to ~3 em — a 16px line
// came out ~49pt tall. Browsers compute "normal" from a font's ascent/descent (the Windows
// or typo metrics), not a runaway hhea gap; (ascent+descent)×1.15 reproduces a
// browser-comparable ~1.2 em for all three bundled families (mono ≈ 1.21, serif ≈ 1.54,
// sans ≈ 1.65 em) without the bad gap. A real (small) line gap from a well-behaved embedded
// font is likewise folded into the leading factor rather than added on top. (LineGapPt is
// still tracked on the line for any future metric that wants the raw value.)
func autoLineHeight(line inline.Line) float64 {
	return (line.AscentPt + line.DescentPt) * cssDefaultLineMult
}

// ascentOfLine returns the line's ascent used to place its baseline below the pen
// top: the greater of the text ascent and the tallest atom's above-baseline extent
// (inline.MakeLine records them separately). The baseline thus sits below a tall
// atom's top rather than the atom overflowing above the line; an all-atomic line
// (zero text ascent) still gets its ascent from the atom.
func ascentOfLine(line inline.Line) float64 {
	if line.AtomAscentPt > line.AscentPt {
		return line.AtomAscentPt
	}
	return line.AscentPt
}

// atomicLineExtent returns the vertical extent an atomic inline box needs about the
// baseline (its above-baseline ascent plus below-baseline descent). It is the floor
// effectiveLineHeight applies to the line HEIGHT (vertical advance) so a fixed
// line-height shorter than a tall atom (e.g. line-height:10px with a 100px image)
// still reserves room for the atom — the atom-aware ascent places the baseline
// correctly, but a fixed line-height ignores ascent, so the height floor remains
// necessary. A line with no atoms returns 0.
func atomicLineExtent(line inline.Line) float64 {
	return line.AtomAscentPt + line.AtomDescentPt
}

// translateFragment shifts fragment f and its whole subtree by (dx, dy): the
// border box, its inline line glyph X / baselines, and every descendant fragment.
// It is used to move an atomic inline box's already-laid-out fragment from its
// provisional origin to its position on a line; unlike shiftFragment (block flow,
// vertical-only) it moves X too, because an atomic's horizontal position is set
// here rather than by the block stacker. Floats are recursed in addition to Children
// so a repositioned nested BFC carries its inner floats by the same delta.
func translateFragment(f *Fragment, dx, dy float64) {
	if dx == 0 && dy == 0 {
		return
	}
	f.X += dx
	f.Y += dy
	for li := range f.Lines {
		f.Lines[li].BaselineY += dy
		for gi := range f.Lines[li].Glyphs {
			f.Lines[li].Glyphs[gi].X += dx
		}
	}
	if f.Image != nil {
		f.Image.CX += dx
		f.Image.CY += dy
	}
	if f.Control != nil {
		f.Control.CX += dx
		f.Control.CY += dy
	}
	for _, c := range f.Children {
		translateFragment(c, dx, dy)
	}
	for _, fl := range f.Floats {
		translateFragment(fl, dx, dy)
	}
	// Move the page-space fields the fragment owns (clip rect, collapsed grid strips,
	// positioned clip chains, and out-of-flow positioned descendants) by the same delta,
	// AFTER the Children/Floats recursion so an abs/fixed Positioned entry — present only
	// here, never in Children — moves exactly once. See shiftFragmentExtras.
	shiftFragmentExtras(f, dx, dy)
}
