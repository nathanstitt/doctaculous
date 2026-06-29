package css

import (
	"context"
	"image"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// replacedUsedSize computes the used content-box size (in points) of replaced box
// b per the CSS 2.1 §10.3.2 / §10.6.2 replaced-element sizing rules, decoding b's
// image for its intrinsic size. pctBasis is the width percentage basis (the
// containing block / inline content width); a percentage height has no basis here
// and is treated as auto (logged once). The steps:
//
//  1. Resolve the specified width/height: CSS width/height take precedence over the
//     presentational width/height attributes (a specified CSS length wins over the
//     attribute per CSS); an auto/absent dimension is left unspecified (the
//     hasW/hasH flags record "specified", distinguishing auto from an explicit 0).
//  2. Decode the image for its intrinsic size (iw, ih). A failed decode leaves no
//     intrinsic size, so an unspecified dimension falls back to 0 (a placeholder of
//     whatever explicit size the box has, else collapsed) per the degradation rule.
//  3. Apply the used-size cases: both specified -> use them; only one specified ->
//     derive the other from the intrinsic aspect ratio (0 with no intrinsic ratio);
//     neither -> the intrinsic size (0 with no intrinsic image).
//  4. Clamp each axis by min/max-width/-height (the same primitive the block width
//     algorithm uses), independently per axis. (CSS 10.4's ratio-preserving min/max
//     step is a deferred refinement; per-axis clamping matches the rest of the
//     engine's single-axis model.)
func (e *Engine) replacedUsedSize(ctx context.Context, b *cssbox.Box, pctBasis float64) (w, h float64) {
	fs := b.Style.FontSizePt

	// The width axis has a percentage basis (the containing block / inline content
	// width); the height axis does not in this single-axis model, so a percentage
	// height (or min/max-height) is treated as auto/none rather than resolving
	// against a zero basis (which would wrongly squash the image to 0).
	specW, hasW := specifiedReplacedLen(b, b.Style.Width, "width", fs, pctBasis, true)
	specH, hasH := specifiedReplacedLen(b, b.Style.Height, "height", fs, 0, false)
	if !hasH && b.Style.Height.Unit == gcss.UnitPercent {
		e.logf("css layout: percentage height on replaced element has no basis; treating as auto")
	}

	var iw, ih float64
	var haveIntrinsic bool
	if b.Replaced != nil && b.Replaced.Control != cssbox.CtrlNone {
		iw, ih = e.controlIntrinsicSize(ctx, b)
		haveIntrinsic = true
	} else {
		iw, ih, haveIntrinsic = e.intrinsicSize(ctx, b)
	}

	switch {
	case hasW && hasH:
		w, h = specW, specH
	case hasW:
		w = specW
		if haveIntrinsic {
			h = specW * ih / iw
		}
	case hasH:
		h = specH
		if haveIntrinsic {
			w = specH * iw / ih
		}
	default:
		if haveIntrinsic {
			w, h = iw, ih
		}
	}

	maxW, hasMaxW := resolveMaxFor(b.Style.MaxWidth, fs, pctBasis, true)
	maxH, hasMaxH := resolveMaxFor(b.Style.MaxHeight, fs, 0, false)
	minW := resolveMin(b.Style.MinWidth, fs, pctBasis, true)
	minH := resolveMin(b.Style.MinHeight, fs, 0, false)

	// CSS 10.4: when the used size came from the intrinsic aspect ratio (at least one
	// axis auto, an intrinsic ratio available) and is NOT explicitly pinned on both axes,
	// apply min/max preserving the ratio (the §10.4 constraint-violation table). When both
	// width and height are explicitly specified the ratio is already broken, so fall back
	// to independent per-axis clamping.
	if haveIntrinsic && (!hasW || !hasH) && w > 0 && h > 0 {
		w, h = constrainRatio(w, h, minW, maxW, hasMaxW, minH, maxH, hasMaxH)
	} else {
		w = clampMaxMin(w, minW, maxW, hasMaxW)
		h = clampMaxMin(h, minH, maxH, hasMaxH)
	}
	return w, h
}

// constrainRatio applies min/max-width/-height to a tentative size (w,h) that carries an
// intrinsic aspect ratio, following the CSS 2.1 §10.4 constraint-violation table: a single
// violated bound scales the OTHER axis to preserve the ratio; conflicting bounds on both
// axes clamp each independently (the ratio cannot be preserved). hasMaxW/hasMaxH gate the
// max bounds ("none" when false). All bounds are content-box values; min defaults to 0.
func constrainRatio(w, h, minW, maxW float64, hasMaxW bool, minH, maxH float64, hasMaxH bool) (float64, float64) {
	// Effective max (huge when "none") simplifies the comparisons.
	mw, mh := maxW, maxH
	if !hasMaxW {
		mw = 1e18
	}
	if !hasMaxH {
		mh = 1e18
	}
	gtMaxW, gtMaxH := w > mw, h > mh
	ltMinW, ltMinH := w < minW, h < minH

	switch {
	case gtMaxW && gtMaxH:
		// Both exceed max: shrink by the more-constraining ratio.
		if mw/w <= mh/h {
			return mw, max0(mw * h / w)
		}
		return max0(mh * w / h), mh
	case ltMinW && ltMinH:
		// Both below min: grow by the more-constraining ratio.
		if minW/w >= minH/h {
			return minW, minW * h / w
		}
		return minH * w / h, minH
	case gtMaxW && ltMinH:
		return mw, minH
	case ltMinW && gtMaxH:
		return minW, mh
	case gtMaxW:
		// Width over max: set w=max-w, derive h, then clamp h to its own min/max.
		return mw, clampMaxMin(mw*h/w, minH, mh, true)
	case ltMinW:
		return minW, clampMaxMin(minW*h/w, minH, mh, true)
	case gtMaxH:
		return clampMaxMin(mh*w/h, minW, mw, true), mh
	case ltMinH:
		return clampMaxMin(minH*w/h, minW, mw, true), minH
	}
	return w, h
}

// max0 floors a value at 0.
func max0(v float64) float64 {
	if v < 0 {
		return 0
	}
	return v
}

// intrinsicSize returns b's replaced image's intrinsic pixel size (treated 1:1 as
// points), with ok=false when b has no decodable image (no src, failed decode, or
// a zero-area image). The decoded image is cached on the engine, so repeated calls
// for the same src are cheap.
func (e *Engine) intrinsicSize(ctx context.Context, b *cssbox.Box) (iw, ih float64, ok bool) {
	if b.Replaced == nil {
		return 0, 0, false
	}
	d := e.images.get(ctx, b.Replaced.Attrs["src"])
	if !d.ok || d.w <= 0 || d.h <= 0 {
		return 0, 0, false
	}
	return d.w, d.h, true
}

// specifiedReplacedLen resolves a replaced box's specified width/height: the CSS
// length if it is not auto (px/pt 1:1, em against the box's font size, % against
// basis), else the presentational attribute (an integer pixel count) if present.
// ok reports whether a definite size was specified at all (so the caller can
// distinguish "auto" from an explicit "0"). A negative result floors to 0. When
// hasBasis is false a percentage length has no resolvable basis (the height axis in
// this single-axis model) and is treated as unspecified (auto), so the caller
// derives the dimension from the aspect ratio / intrinsic size instead of squashing
// it to a zero-basis 0.
func specifiedReplacedLen(b *cssbox.Box, l gcss.Length, attr string, fs, basis float64, hasBasis bool) (val float64, ok bool) {
	if l.Unit == gcss.UnitPercent && !hasBasis {
		// fall through to the attr/auto path below
	} else if v, isAuto := resolveLen(l, fs, basis); !isAuto {
		if v < 0 {
			v = 0
		}
		return v, true
	}
	if b.Replaced != nil {
		if s, present := b.Replaced.Attrs[attr]; present {
			if v := attrPx(s); v > 0 || s == "0" {
				return v, true
			}
		}
	}
	return 0, false
}

// resolveMin resolves a min-width/min-height length to points (its default is 0). A
// percentage with no basis (hasBasis false — the height axis) contributes no minimum
// (0) rather than resolving against a zero basis.
func resolveMin(l gcss.Length, fs, basis float64, hasBasis bool) float64 {
	if l.Unit == gcss.UnitPercent && !hasBasis {
		return 0
	}
	v, _ := resolveLen(l, fs, basis)
	return v
}

// resolveMaxFor resolves a max-width/max-height length to points and reports whether
// a maximum applies (UnitAuto models CSS "none" = no maximum). A percentage with no
// basis (hasBasis false — the height axis) imposes no maximum rather than clamping to
// a zero-basis 0.
func resolveMaxFor(l gcss.Length, fs, basis float64, hasBasis bool) (float64, bool) {
	if l.Unit == gcss.UnitAuto || (l.Unit == gcss.UnitPercent && !hasBasis) {
		return 0, false
	}
	v, _ := resolveLen(l, fs, basis)
	return v, true
}

// layoutBlockReplaced lays out a block-level replaced box (e.g.
// <img style="display:block">) into a containing block of width cbWidth whose left
// content edge derives from originX, with the box's top margin edge at page-space y
// marginTopEdgeY. It returns the positioned border-box fragment and the box's solid
// top/bottom margins (a replaced box has no in-flow children, so nothing collapses
// through it). The used content size comes from the replaced-element algorithm
// against cbWidth (so a percentage width and width:auto→intrinsic both resolve);
// the fragment carries the decoded image for paint.
func (e *Engine) layoutBlockReplaced(ctx context.Context, b *cssbox.Box, cbWidth, originX, marginTopEdgeY float64) blockResult {
	ed := usedEdges(b, cbWidth)
	w, h := e.replacedUsedSize(ctx, b, cbWidth)
	borderX := originX + ed.mL
	borderY := marginTopEdgeY + ed.mT
	frag := e.replacedFragment(ctx, b, w, h, borderX, borderY, cbWidth)
	return blockResult{frag: frag, marginTop: ed.mT, marginBottom: ed.mB}
}

// replacedFragment builds the positioned border-box fragment for replaced box b,
// given its used content size (w,h) and the page-space top-left of its BORDER box
// (borderX, borderY). It wraps the content in the box's own padding+border
// (usedEdges; margins are handled by the caller's flow, not here), carries the
// decoded image (or nil for a failed decode — a sized placeholder) in the content
// box, and copies the box's background and border edges so a styled <img> paints
// its own decoration. ctx is used to fetch the (cached) decoded image.
func (e *Engine) replacedFragment(ctx context.Context, b *cssbox.Box, w, h, borderX, borderY, pctBasis float64) *Fragment {
	ed := usedEdges(b, pctBasis)

	borderW := w + ed.pL + ed.pR + ed.bL + ed.bR
	borderH := h + ed.pT + ed.pB + ed.bT + ed.bB
	contentX := borderX + ed.bL + ed.pL
	contentY := borderY + ed.bT + ed.pT

	frag := &Fragment{
		X: borderX, Y: borderY, W: borderW, H: borderH,
		Background: b.Style.BackgroundColor,
		DebugTag:   debugTag(b),
	}
	if b.Replaced != nil && b.Replaced.Control != cssbox.CtrlNone {
		frag.Control = e.controlContentFor(b, contentX, contentY, w, h)
	} else {
		img := decodedImageFor(ctx, e, b)
		frag.Image = &ImageContent{
			Img: img,
			CX:  contentX, CY: contentY, CW: w, CH: h,
			Fit:  mapObjectFit(b.Style.ObjectFit),
			PosX: b.Style.ObjectPositionX, PosY: b.Style.ObjectPositionY,
		}
	}
	// Retain the source box like the block-fragment constructor does (block.go): the
	// flatten reads it for the stacking z-index, and isRelativeFragment relies on it to
	// tell a relative replaced element (in flow → aliased with a Children entry, must NOT
	// be shifted again via Positioned) from an abs/fixed one. A nil Box here would read as
	// non-relative and risk a double-shift if a relative replaced box reaches a holder's
	// Positioned layer.
	frag.Box = b
	frag.Border[layout.EdgeTop] = BorderEdge{Width: ed.bT, Color: b.Style.BorderTopColor, Style: mapBorderStyle(b.Style.BorderTopStyle)}
	frag.Border[layout.EdgeRight] = BorderEdge{Width: ed.bR, Color: b.Style.BorderRightColor, Style: mapBorderStyle(b.Style.BorderRightStyle)}
	frag.Border[layout.EdgeBottom] = BorderEdge{Width: ed.bB, Color: b.Style.BorderBottomColor, Style: mapBorderStyle(b.Style.BorderBottomStyle)}
	frag.Border[layout.EdgeLeft] = BorderEdge{Width: ed.bL, Color: b.Style.BorderLeftColor, Style: mapBorderStyle(b.Style.BorderLeftStyle)}
	return frag
}

// decodedImageFor returns b's decoded image (nil if it has no decodable image), for
// the fragment to paint. The decode result is cached, so this does not re-fetch.
func decodedImageFor(ctx context.Context, e *Engine, b *cssbox.Box) image.Image {
	if b.Replaced == nil {
		return nil
	}
	if d := e.images.get(ctx, b.Replaced.Attrs["src"]); d.ok {
		return d.img
	}
	return nil
}

// mapObjectFit maps a CSS object-fit keyword to the layout ObjectFit. Unknown or
// empty values default to FitFill (the CSS initial value).
func mapObjectFit(s string) layout.ObjectFit {
	switch s {
	case "contain":
		return layout.FitContain
	case "cover":
		return layout.FitCover
	case "none":
		return layout.FitNone
	case "scale-down":
		return layout.FitScaleDown
	default: // "fill", "", unknown
		return layout.FitFill
	}
}
