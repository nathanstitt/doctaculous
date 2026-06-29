package css

import (
	"context"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
	"github.com/nathanstitt/doctaculous/pkg/layout/inline"
)

// measureMaxContent returns box's max-content width: the width it occupies with no
// line wrapping (used by auto table layout, CSS 17.5.2.2). For an inline-formatting
// box it is the width of the single unbroken line of all its inline content; for a
// block container it is the widest child's max-content; a specified width pins it.
// The box's own horizontal padding+border is NOT added here — the caller adds edges
// where it needs the border-box contribution.
func (e *Engine) measureMaxContent(ctx context.Context, b *cssbox.Box) float64 {
	return e.measureContent(ctx, b, true)
}

// measureMinContent returns box's min-content width: the narrowest width without
// overflow — the widest single unbreakable unit (longest word / atomic inline /
// replaced intrinsic width). Computed by breaking the shaped glyphs at width 0 and
// taking the widest resulting line.
func (e *Engine) measureMinContent(ctx context.Context, b *cssbox.Box) float64 {
	return e.measureContent(ctx, b, false)
}

// measureContent is the shared core; wantMax selects max-content (no wrap) vs
// min-content (everything wraps to its smallest unit). It honors a specified, fixed
// (non-auto, non-percentage) width on the box (which pins the contribution), and
// recurses block children (a block container's contribution is its widest child's,
// plus that child's own horizontal border+padding). Allocates no committed layout.
func (e *Engine) measureContent(ctx context.Context, b *cssbox.Box, wantMax bool) float64 {
	if w, ok := specifiedFixedWidth(b); ok {
		return w
	}
	if b.Formatting == cssbox.InlineFC {
		return e.measureInline(ctx, b, wantMax)
	}
	inner := 0.0
	for _, c := range b.Children {
		cw := e.measureContent(ctx, c, wantMax) + horizontalEdges(c)
		if cw > inner {
			inner = cw
		}
	}
	return inner
}

// measureInline gathers b's inline runs, shapes them once, and returns either the
// unbroken width (max-content) or the widest zero-width-break line (min-content).
func (e *Engine) measureInline(ctx context.Context, b *cssbox.Box, wantMax bool) float64 {
	var runs []inline.Run
	var atomics []*Fragment // gatherInlineRuns fully lays out inline-block atoms; their fragments are unused in measure mode, but that layout cost is real
	e.gatherInlineRuns(ctx, b, 1e9, &runs, &atomics)
	if len(runs) == 0 {
		return 0
	}
	glyphs := inline.Shape(e.faces, runs, e.logf)
	if wantMax {
		return inline.VisibleWidth(glyphs)
	}
	widest := 0.0
	rest := glyphs
	for len(rest) > 0 {
		var line []inline.Glyph
		line, rest = inline.BreakNext(rest, 0)
		if len(line) == 0 {
			// BreakNext could not place even one unit at width 0 — force one glyph to
			// avoid a spin (the unit is wider than 0; take its visible width).
			line, rest = rest[:1], rest[1:]
		}
		w := inline.VisibleWidth(line)
		if w > widest {
			widest = w
		}
	}
	return widest
}

// specifiedFixedWidth returns the box's content-box width when it has a fixed
// (px/pt/em, non-auto, non-percentage) width, accounting for box-sizing. ok is false
// for auto/percentage widths (which do not pin intrinsic sizing).
func specifiedFixedWidth(b *cssbox.Box) (float64, bool) {
	u := b.Style.Width.Unit
	if u == gcss.UnitAuto || u == gcss.UnitPercent {
		return 0, false
	}
	val, isAuto := resolveLen(b.Style.Width, b.Style.FontSizePt, 0)
	if isAuto { // defensive: UnitAuto already excluded above, so this cannot fire today
		return 0, false
	}
	if b.Style.BoxSizing == "border-box" {
		val -= horizontalEdges(b)
		if val < 0 {
			val = 0
		}
	}
	return val, true
}

// horizontalEdges is the box's left+right padding + border width in points
// (percentage padding is treated as 0 for measurement — a documented approximation).
func horizontalEdges(b *cssbox.Box) float64 {
	ed := usedEdges(b, 0)
	return ed.pL + ed.pR + ed.bL + ed.bR
}
