package css

import (
	"context"

	gcss "github.com/nathanstitt/omnidoc/pkg/internal/css"
	"github.com/nathanstitt/omnidoc/pkg/internal/layout/cssbox"
)

// rect is an axis-aligned rectangle in page space (points, Y-down): (x,y) is the
// top-left, w/h the extent. Used for the abs-pos containing block and the resolved
// border box.
type rect struct{ x, y, w, h float64 }

// relativeOffset resolves a relatively-positioned box's paint-time offset (dx,dy)
// from its top/right/bottom/left against the containing-block dimensions cbW
// (left/right percentages) and cbH (top/bottom percentages). CSS 9.4.3
// over-constrained resolution: left wins over right, top wins over bottom; an auto
// offset contributes 0. The offset shifts only the painted position — flow is
// unchanged (the engine applies it at flatten time).
func relativeOffset(b *cssbox.Box, cbW, cbH float64) (dx, dy float64) {
	fs := b.Style.FontSizePt
	dx = axisRelative(b.Style.Left, b.Style.Right, fs, cbW)
	dy = axisRelative(b.Style.Top, b.Style.Bottom, fs, cbH)
	return dx, dy
}

// warnCollapseThrough reports, once, that an EMPTY block's own top and bottom margins
// did not collapse through it.
//
// This is backlog item E3's first sub-case. CSS 2.1 §8.3.1 says a box with no line
// boxes, no in-flow content, no border/padding, and auto height collapses its own top
// and bottom margins together and then through to its siblings — so an empty
// `<div style="margin:40px 0">` between two paragraphs contributes ONE 40pt gap. This
// engine collapses adjacent siblings pairwise instead, so the empty box's top margin
// collapses with the preceding sibling and its bottom margin with the following one,
// and the gap comes out DOUBLED. Measured before this warning landed: 80pt where a
// browser gives 40pt, with nothing in the log.
//
// The condition is exact rather than heuristic (zero-height fragment, no children,
// margins on both sides), so this cannot fire on a box whose margins the engine did
// collapse correctly. A zero-margin spacer stays quiet — with nothing to collapse there
// is no divergence to report.
func (e *Engine) warnCollapseThrough(b *cssbox.Box, res blockResult) {
	if res.frag == nil || res.frag.H != 0 || len(res.frag.Children) > 0 {
		return
	}
	if res.marginTop == 0 || res.marginBottom == 0 {
		return
	}
	// A box establishing a new BFC does not collapse through — its margins are solid by
	// design, so the current behaviour is already correct for it.
	if establishesNewBFC(b) {
		return
	}
	e.warnOnce("margin-collapse-through",
		"css layout: an empty block's top and bottom margins do not collapse through it "+
			"(CSS 2.1 8.3.1); the %gpt/%gpt margins each collapse with a sibling instead, "+
			"so the gap is up to twice what a browser gives", res.marginTop, res.marginBottom)
}

// warnRelativeInline reports, once, that `position: relative` with a non-zero offset
// on a NON-REPLACED INLINE box does not move anything.
//
// This is backlog item C6 and is structural, not an oversight: an inline element box
// generates no fragment — its glyphs flatten into the parent's line boxes — so there is
// nothing to hang a RelOffset on. Honouring it needs either inline-box fragments or a
// per-run offset on LineFragment. Until then the declaration is a complete no-op, and
// before this warning it was a SILENT one: the PDF for `left:40px;top:15px` on a
// <span> was byte-identical to the PDF without it, with nothing in the log to say so.
//
// Deliberately quiet in two cases that are not authoring mistakes:
//   - a zero/absent offset, which is the common `position: relative` used only to
//     establish a containing block for an absolutely-positioned descendant — that DOES
//     work here, so warning would be wrong;
//   - atomic inlines (inline-block, replaced), which take the block path above and get
//     a real fragment and a real offset.
//
// cbW is the containing block width, used so a percentage offset is judged by its used
// value rather than assumed non-zero.
func (e *Engine) warnRelativeInline(b *cssbox.Box, cbW float64) {
	if b.Position != cssbox.PosRelative {
		return
	}
	// Percentages of the block axis resolve against the containing block HEIGHT, which
	// is not known here; pass 0 so a percentage top/bottom contributes 0. That can only
	// under-report (a percentage-only offset stays quiet), never warn spuriously.
	dx, dy := relativeOffset(b, cbW, 0)
	if dx == 0 && dy == 0 {
		return
	}
	e.warnOnce("rel-inline-box",
		"css layout: position:relative on a non-replaced inline box is not supported; "+
			"the box keeps its in-flow position (offset %gpt,%gpt ignored)", dx, dy)
}

// axisRelative resolves one axis of a relative offset: the start offset (left/top)
// wins; if it is auto, the negated end offset (right/bottom) applies; if both are
// auto, 0.
func axisRelative(start, end gcss.Length, fontSizePt, pctBasis float64) float64 {
	if v, isAuto := resolveLen(start, fontSizePt, pctBasis); !isAuto {
		return v
	}
	if v, isAuto := resolveLen(end, fontSizePt, pctBasis); !isAuto {
		return -v
	}
	return 0
}

// absRect resolves an absolutely/fixed-positioned box's used border-box rectangle
// and used content width against the containing-block content rect cb, given the
// box's resolved edges ed (auto margins already 0). Offsets are measured from cb to
// the box's MARGIN edge, so margin terms are explicit. Implements the supported
// subset of CSS 10.3.7/10.6.4 (see the spec's "The positioning geometry"):
// over-constrained cases resolve start-edge-wins; all-auto offsets approximate the
// static position (cb top-left); width:auto with both left+right derives the width.
func absRect(b *cssbox.Box, ed edges, cb rect, autoFillW float64) (border rect, contentW float64) {
	fs := b.Style.FontSizePt
	insetsX := ed.bL + ed.bR + ed.pL + ed.pR
	insetsY := ed.bT + ed.bB + ed.pT + ed.pB
	borderBox := b.Style.BoxSizing == "border-box"

	// Resolve specified width/height (content-box terms) if not auto.
	wVal, wAuto := resolveLen(b.Style.Width, fs, cb.w)
	if !wAuto && borderBox {
		wVal -= insetsX
	}
	hVal, hAuto := resolveLen(b.Style.Height, fs, cb.h)
	if !hAuto && borderBox {
		hVal -= insetsY
	}
	// Fallback content width when width:auto (no offset constraint): the caller supplies
	// either the containing-block fill or the CSS 10.3.7 shrink-to-fit width.
	fillW := autoFillW
	if fillW < 0 {
		fillW = 0
	}

	// Auto-margin flags (usedEdges already resolved auto margins to 0); abs margin:auto
	// centering distributes the over-constrained leftover space to the auto margins.
	mLAuto := isAuto2(b.Style.MarginLeft, fs)
	mRAuto := isAuto2(b.Style.MarginRight, fs)
	mTAuto := isAuto2(b.Style.MarginTop, fs)
	mBAuto := isAuto2(b.Style.MarginBottom, fs)

	bx, cw := axisAbs(cb.x, cb.w, b.Style.Left, b.Style.Right, fs, ed.mL, ed.mR, mLAuto, mRAuto, insetsX, wVal, wAuto, fillW)
	by, _ := axisAbs(cb.y, cb.h, b.Style.Top, b.Style.Bottom, fs, ed.mT, ed.mB, mTAuto, mBAuto, insetsY, hVal, hAuto, hVal)

	// The returned border-box WIDTH/HEIGHT are advisory: the abs-pos pass
	// (resolveAbsolute) uses the laid-out fragment's own border box for W/H and reads
	// only border.x/border.y from this result to reposition the origin. They are still
	// computed for completeness and for a future caller that wants the resolved box
	// without laying out an interior.
	bw := cw + insetsX
	bh := hVal + insetsY
	if hAuto {
		// height:auto: the laid-out interior's content height is authoritative (the
		// caller keeps the fragment's own H). When both top+bottom are specified the
		// height is instead derived from the offsets; otherwise it is left at the
		// border insets (the caller's fragment H supplies the content).
		if !isAuto2(b.Style.Top, fs) && !isAuto2(b.Style.Bottom, fs) {
			topV, _ := resolveLen(b.Style.Top, fs, cb.h)
			botV, _ := resolveLen(b.Style.Bottom, fs, cb.h)
			bh = cb.h - topV - botV - ed.mT - ed.mB
			if bh < 0 {
				bh = 0
			}
		} else {
			bh = insetsY
		}
	}
	return rect{x: bx, y: by, w: bw, h: bh}, cw
}

// axisAbs resolves one axis (horizontal with cb.x/cb.w, or vertical with cb.y/cb.h)
// of an abs-pos box: returns the border-box start coordinate and the used content
// SIZE on that axis. startOff/endOff are the offset Lengths (left/right or
// top/bottom); mStart/mEnd the margins; insets the border+padding on the axis;
// sizeVal/sizeAuto the box's own content size; fillSize the fallback content size
// when the size is auto and at most one offset is set.
func axisAbs(cbStart, cbExtent float64, startOff, endOff gcss.Length, fontSizePt, mStart, mEnd float64, mStartAuto, mEndAuto bool, insets, sizeVal float64, sizeAuto bool, fillSize float64) (borderStart, contentSize float64) {
	sVal, sAuto := resolveLen(startOff, fontSizePt, cbExtent)
	eVal, eAuto := resolveLen(endOff, fontSizePt, cbExtent)

	switch {
	case !sAuto && !eAuto && sizeAuto:
		// start+end specified, size auto: derive content size from the offsets.
		cs := cbExtent - sVal - eVal - mStart - mEnd - insets
		if cs < 0 {
			cs = 0
		}
		return cbStart + sVal + mStart, cs
	case !sAuto && !eAuto:
		// start+end+size all definite (over-constrained, CSS 10.3.7): auto margins absorb
		// the leftover space — both auto → split evenly (centering); one auto → it takes
		// all. With no auto margin the box is over-constrained and the start edge wins
		// (the leftover is ignored, matching "ignore end"). Only the START margin affects
		// the placement (the box is positioned by start + margin-start); the end margin is
		// computed by distributeAbsMargins for completeness but does not move the box.
		usedMStart, _ := distributeAbsMargins(cbExtent-sVal-eVal-sizeVal-insets, mStart, mEnd, mStartAuto, mEndAuto)
		return cbStart + sVal + usedMStart, sizeVal
	case !sAuto:
		// start specified (size definite, or end ignored): place against the start edge.
		size := sizeVal
		if sizeAuto {
			size = fillSize
		}
		return cbStart + sVal + mStart, size
	case sAuto && !eAuto:
		// end specified, start auto: place against the far edge.
		size := sizeVal
		if sizeAuto {
			size = fillSize
		}
		borderBoxSize := size + insets
		return cbStart + cbExtent - eVal - mEnd - borderBoxSize, size
	default:
		// all auto: static-position approximation (cb start edge).
		size := sizeVal
		if sizeAuto {
			size = fillSize
		}
		return cbStart + mStart, size
	}
}

// distributeAbsMargins resolves auto margins on an over-constrained abs axis (both
// offsets + a definite size specified, CSS 10.3.7): the leftover space is split evenly
// between two auto margins (centering), given entirely to a single auto margin, or
// ignored when neither margin is auto. Returns the used (mStart, mEnd). A negative
// leftover (the box overflows) still distributes (the margins go negative), matching
// browsers.
func distributeAbsMargins(leftover, mStart, mEnd float64, mStartAuto, mEndAuto bool) (float64, float64) {
	switch {
	case mStartAuto && mEndAuto:
		return leftover / 2, leftover / 2
	case mStartAuto:
		return leftover, mEnd
	case mEndAuto:
		return mStart, leftover
	default:
		return mStart, mEnd
	}
}

// isAuto2 reports whether a length is the auto keyword (helper for absRect's
// both-specified height-derivation guard).
func isAuto2(l gcss.Length, fontSizePt float64) bool {
	_, a := resolveLen(l, fontSizePt, 0)
	return a
}

// absWidthIsOffsetConstrained reports whether an abs/fixed box's used width is
// determined by its left+right offsets (both specified) with width:auto — CSS
// 10.3.7's "shrink to fit the offsets" case. When true, the box's used content width
// is the value absRect returns (cb.w - left - right - margins - insets), NOT the
// containing-block fill a plain auto width gets; the abs-pos pass must lay the box's
// interior out at that width. (The single-offset and explicit-width cases use the
// normal width resolution, so this returns false for them.)
func absWidthIsOffsetConstrained(b *cssbox.Box) bool {
	fs := b.Style.FontSizePt
	return isAuto2(b.Style.Width, fs) && !isAuto2(b.Style.Left, fs) && !isAuto2(b.Style.Right, fs)
}

// absWidthShrinksToFit reports whether an abs/fixed box's width:auto is resolved by
// CSS 10.3.7 shrink-to-fit (min(max(min-content, available), max-content)) rather than
// filling the containing block: width is auto, the offsets do NOT both pin it (that is
// the offset-constrained case handled separately), and it is not a replaced box (which
// has an intrinsic width). A box with no offsets, or a single offset, takes this path.
func (e *Engine) absWidthShrinksToFit(b *cssbox.Box) bool {
	fs := b.Style.FontSizePt
	return isAuto2(b.Style.Width, fs) && !absWidthIsOffsetConstrained(b) && b.Kind != cssbox.BoxReplaced
}

// absShrinkToFitWidth returns the CSS 10.3.7 shrink-to-fit CONTENT width for a width:auto
// abs box: min(max(preferred-minimum, available), preferred), where available is the box's
// available content width (CB content width minus its own margins/border/padding), the
// preferred width is the max-content width, and the preferred minimum is the min-content
// width. Measured via the memoized measure helpers (shared with table/grid/inline-block).
func (e *Engine) absShrinkToFitWidth(ctx context.Context, b *cssbox.Box, available float64) float64 {
	maxC := e.measureMaxContent(ctx, b)
	minC := e.measureMinContent(ctx, b)
	w := available
	if maxC < w {
		w = maxC
	}
	if minC > w {
		w = minC
	}
	if w < 0 {
		w = 0
	}
	return w
}
