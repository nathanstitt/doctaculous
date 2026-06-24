package css

import (
	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
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
func absRect(b *cssbox.Box, ed edges, cb rect) (border rect, contentW float64) {
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
	// Fallback width for the no-constraint case: containing-block fill for a
	// non-replaced box (replaced boxes are handled by the replaced path before
	// reaching here in pass 2; if a replaced box does reach here its intrinsic
	// width is already in b.Style.Width or resolved upstream).
	fillW := cb.w - ed.mL - ed.mR - insetsX
	if fillW < 0 {
		fillW = 0
	}

	bx, cw := axisAbs(cb.x, cb.w, b.Style.Left, b.Style.Right, fs, ed.mL, ed.mR, insetsX, wVal, wAuto, fillW)
	by, _ := axisAbs(cb.y, cb.h, b.Style.Top, b.Style.Bottom, fs, ed.mT, ed.mB, insetsY, hVal, hAuto, hVal)

	bw := cw + insetsX
	bh := hVal + insetsY
	if hAuto {
		// Height is content-derived: the caller lays out the interior and sets the
		// real height; absRect returns a provisional 0-content height here. The
		// two-pass caller recomputes by/bh once the interior height is known when
		// only `top` (not bottom) is specified. (When both top+bottom are specified
		// with height:auto, the derived height below wins.)
		if !isAuto2(b.Style.Top, fs) && !isAuto2(b.Style.Bottom, fs) {
			// both specified: derive height from offsets.
			topV, _ := resolveLen(b.Style.Top, fs, cb.h)
			botV, _ := resolveLen(b.Style.Bottom, fs, cb.h)
			bh = cb.h - topV - botV - ed.mT - ed.mB
			if bh < 0 {
				bh = 0
			}
		} else {
			bh = insetsY // content height filled in by the caller
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
func axisAbs(cbStart, cbExtent float64, startOff, endOff gcss.Length, fontSizePt, mStart, mEnd, insets, sizeVal float64, sizeAuto bool, fillSize float64) (borderStart, contentSize float64) {
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

// isAuto2 reports whether a length is the auto keyword (helper for absRect's
// both-specified height-derivation guard).
func isAuto2(l gcss.Length, fontSizePt float64) bool {
	_, a := resolveLen(l, fontSizePt, 0)
	return a
}
