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
func (e *Engine) layoutInline(ctx context.Context, b *cssbox.Box, contentW, contentTopY, contentX float64) (lines []LineFragment, height float64, atomics []*Fragment) {
	// 1. Gather the inline-level descendants into neutral styled runs, laying out
	//    any atomic boxes (inline-block / replaced) eagerly so their size is known.
	var runs []inline.Run
	e.gatherInlineRuns(ctx, b, contentW, &runs, &atomics)
	if len(runs) == 0 {
		return nil, 0, nil
	}

	// 2. Shape the runs into glyphs, then 3. break them into lines at the content
	//    width (no first-line indent for HTML in this pass: same width for both).
	glyphs := inline.Shape(e.faces, runs, e.logf)
	breakLines := inline.Break(glyphs, contentW, contentW)

	// 4. Resolve the box's alignment once (see effectiveTextAlign for the anonymous-
	//    block subtlety).
	align := effectiveTextAlign(b)

	// 5. Position each line top-to-bottom from contentTopY, building a LineFragment
	//    and placing any atomic fragments the line carries on its baseline.
	penY := contentTopY
	for i := range breakLines {
		line := breakLines[i]
		lh := e.effectiveLineHeight(b, line)
		// Known simplification: the baseline is placed from the TEXT ascent
		// (ascentOfLine), so when a line mixes text with an inline-block/replaced atom
		// taller than that ascent, the atom (bottom-aligned below) extends above the
		// line top. effectiveLineHeight floors the line HEIGHT to the atom height so the
		// next line doesn't overlap, but the within-line top alignment is not raised to
		// fit the atom. Proper inline-box vertical-align / line-box ascent including
		// atomics is deferred (a later sub-project).
		baselineY := penY + ascentOfLine(line)

		spaceCount := inline.CountSpaces(line.Glyphs)
		isLast := i == len(breakLines)-1
		p := inline.Place(align, contentX, contentW, line.WidthPt, spaceCount, isLast)

		var lineGlyphs []GlyphFragment
		x := p.StartX
		for gi := range line.Glyphs {
			g := &line.Glyphs[gi]
			switch {
			case g.Atomic != nil:
				// Position the kept fragment: border-box left at x, bottom-aligned so
				// its baseline (BaselinePt from the top) rests on the line baseline.
				if frag, ok := g.Atomic.Ref.(*Fragment); ok && frag != nil {
					translateFragment(frag, x-frag.X, (baselineY-g.Atomic.BaselinePt)-frag.Y)
				}
			case g.Outline != nil:
				lineGlyphs = append(lineGlyphs, GlyphFragment{
					Outline: g.Outline,
					X:       x,
					SizePt:  g.SizePt,
					Color:   color.RGBA{R: g.Color.R, G: g.Color.G, B: g.Color.B, A: g.Color.A},
				})
			}
			x += g.Advance
			if g.Space {
				x += p.ExtraPerSpace
			}
		}

		lines = append(lines, LineFragment{BaselineY: baselineY, Glyphs: lineGlyphs})
		penY += lh
	}

	return lines, penY - contentTopY, atomics
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
		case child.Display == cssbox.DisplayInlineBlock:
			// An inline-block establishes a new BFC; lay it out as a block at its
			// resolved width and carry its border box as an atomic unit.
			res := e.layoutBlock(ctx, child, contentW, 0, 0)
			frag := res.frag
			*atomics = append(*atomics, frag)
			*runs = append(*runs, inline.Run{Atomic: &inline.AtomicItem{
				WidthPt:    frag.W,
				HeightPt:   frag.H,
				BaselinePt: frag.H, // bottom-aligned baseline for now
				Ref:        frag,
			}})
		case child.Kind == cssbox.BoxReplaced:
			// A replaced inline (e.g. <img>) has no decoded intrinsic size yet
			// (sub-project 4), so it sizes from its style/attrs or degrades to zero.
			w, h := replacedSize(child)
			*runs = append(*runs, inline.Run{Atomic: &inline.AtomicItem{
				WidthPt:    w,
				HeightPt:   h,
				BaselinePt: h, // bottom-aligned baseline for now
				Ref:        child,
			}})
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

// replacedSize resolves a replaced box's inline size in points: from its style
// width/height (px/pt 1:1, em against its own font size), falling back to the
// width/height presentation attributes parsed as integer pixels, else zero (no
// intrinsic size until image decoding lands). A missing dimension is zero.
func replacedSize(b *cssbox.Box) (w, h float64) {
	fs := b.Style.FontSizePt
	// resolveLen returns isAuto exactly when Unit==UnitAuto, so !isAuto already
	// excludes auto — no separate Unit check needed.
	if v, isAuto := resolveLen(b.Style.Width, fs, 0); !isAuto {
		w = v
	} else if b.Replaced != nil {
		w = attrPx(b.Replaced.Attrs["width"])
	}
	if v, isAuto := resolveLen(b.Style.Height, fs, 0); !isAuto {
		h = v
	} else if b.Replaced != nil {
		h = attrPx(b.Replaced.Attrs["height"])
	}
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	return w, h
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
	if atomic := maxAtomicHeight(line); atomic > h {
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

// autoLineHeight is the "normal"/auto line height: the line's natural height
// (ascent+descent+gap) times cssDefaultLineMult.
func autoLineHeight(line inline.Line) float64 {
	return (line.AscentPt + line.DescentPt + line.LineGapPt) * cssDefaultLineMult
}

// ascentOfLine returns the line's ascent used to place its baseline below the pen
// top. It is the line's glyph ascent, falling back to the tallest atomic's height
// for an all-atomic line (whose glyph ascent is zero) so the line still has height.
func ascentOfLine(line inline.Line) float64 {
	if line.AscentPt > 0 {
		return line.AscentPt
	}
	return maxAtomicHeight(line)
}

// maxAtomicHeight returns the tallest atomic box on the line, or 0 if it has none.
// An atomic is bottom-aligned (its baseline rests on the line baseline), so its
// height is its extent above the baseline for line-box sizing purposes.
func maxAtomicHeight(line inline.Line) float64 {
	var maxH float64
	for i := range line.Glyphs {
		if a := line.Glyphs[i].Atomic; a != nil && a.HeightPt > maxH {
			maxH = a.HeightPt
		}
	}
	return maxH
}

// translateFragment shifts fragment f and its whole subtree by (dx, dy): the
// border box, its inline line glyph X / baselines, and every descendant fragment.
// It is used to move an atomic inline box's already-laid-out fragment from its
// provisional origin to its position on a line; unlike shiftFragment (block flow,
// vertical-only) it moves X too, because an atomic's horizontal position is set
// here rather than by the block stacker.
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
	for _, c := range f.Children {
		translateFragment(c, dx, dy)
	}
}
