package css

import (
	"context"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
	"github.com/nathanstitt/doctaculous/pkg/svg"
)

// resolveBackgroundImage decodes a box's CSS background-image (if any) and resolves the
// geometry the painter needs into a *BackgroundImageContent, or returns nil when the
// box has no background-image, the image cannot be decoded, or the boxes are degenerate.
// A failed decode degrades gracefully (the background color, if any, still paints) and
// is logged by the image cache.
//
// borderX/Y/W/H is the box's border box in page space; ed carries its border widths and
// padding, from which the padding box (background-origin/clip "padding-box", the
// initial origin) and content box are derived. The clip box defaults to the border box.
func (e *Engine) resolveBackgroundImage(ctx context.Context, b *cssbox.Box, borderX, borderY, borderW, borderH float64, ed edges) *BackgroundImageContent {
	ref := b.Style.BackgroundImage
	if ref == "" {
		return nil
	}
	bg := e.backgroundSource(ctx, ref)
	if bg == nil {
		return nil // missing/undecodable: the background color (if any) still paints
	}

	ox, oy, ow, oh := bgBox(b.Style.BackgroundOrigin, borderX, borderY, borderW, borderH, ed)
	cx, cy, cw, ch := bgBox(b.Style.BackgroundClip, borderX, borderY, borderW, borderH, ed)
	if ow <= 0 || oh <= 0 || cw <= 0 || ch <= 0 {
		return nil
	}
	bg.OriginX, bg.OriginY, bg.OriginW, bg.OriginH = ox, oy, ow, oh
	bg.ClipX, bg.ClipY, bg.ClipW, bg.ClipH = cx, cy, cw, ch

	// Size.
	fs := b.Style.FontSizePt
	switch b.Style.BackgroundSize.Kind {
	case gcss.BgSizeCover:
		bg.SizeKind = layout.BgSizeCover
	case gcss.BgSizeContain:
		bg.SizeKind = layout.BgSizeContain
	case gcss.BgSizeExplicit:
		bg.SizeKind = layout.BgSizeExplicit
		bg.SizeW = bgSizeAxis(b.Style.BackgroundSize.W, fs, ow)
		bg.SizeH = bgSizeAxis(b.Style.BackgroundSize.H, fs, oh)
	default:
		bg.SizeKind = layout.BgSizeAuto
	}

	// Position: a percentage stays a fraction (resolved against origin−tile at paint
	// time); a length/em resolves to px now.
	bg.PosXIsPct, bg.PosXFrac, bg.PosXPx = bgPosAxis(b.Style.BackgroundPosition.X, fs)
	bg.PosYIsPct, bg.PosYFrac, bg.PosYPx = bgPosAxis(b.Style.BackgroundPosition.Y, fs)

	// Repeat.
	bg.RepeatX, bg.RepeatY = bgRepeatAxes(b.Style.BackgroundRepeat)
	if bg.Scene != nil && (bg.RepeatX || bg.RepeatY) {
		// Tiling a VECTOR background is deliberately not implemented: repeating an
		// SVG interacts with the SVG's own viewBox/preserveAspectRatio mapping in a
		// corner most engines special-case, and getting it subtly wrong would be a
		// silent fidelity bug. Degrade to a single paint — visible, correctly sized
		// and positioned, just not repeated — and say so once, so an author whose
		// tile does not repeat learns why instead of guessing.
		e.warnOnce("bg-svg-repeat",
			"css layout: background-repeat of an SVG background is not supported; painting %q once", ref)
		bg.RepeatX, bg.RepeatY = false, false
	}
	return bg
}

// backgroundSource resolves a background-image ref to its paint source: the VECTOR
// branch first (an SVG becomes a layout.VectorScene and never a bitmap, exactly as
// for a replaced <img src="*.svg">), else the raster branch. It returns nil when the
// ref resolves to nothing usable, in which case the box's background color still
// paints. Only the source fields are filled; the caller resolves the geometry.
//
// The ordering mirrors replacedSVG: ask the SVG cache first and fall through to the
// image cache only when the ref is not SVG at all. An SVG that IS the right content
// type but fails to parse yields nil rather than falling through, so a broken SVG is
// never handed to image.Decode and misreported as an image failure.
func (e *Engine) backgroundSource(ctx context.Context, ref string) *BackgroundImageContent {
	if p := e.svgs.get(ctx, ref); p.wasSVG {
		if !p.ok || p.doc == nil || p.doc.WidthPt <= 0 || p.doc.HeightPt <= 0 {
			return nil // a broken SVG: the background color (if any) still paints
		}
		iw, ih := svgBackgroundIntrinsic(p.doc)
		return &BackgroundImageContent{
			Scene:  newSVGScene(p.doc),
			SceneW: p.doc.WidthPt, SceneH: p.doc.HeightPt,
			IntrinsicW: iw, IntrinsicH: ih,
		}
	}
	d := e.images.get(ctx, ref)
	if !d.ok || d.img == nil || d.w <= 0 || d.h <= 0 {
		return nil
	}
	return &BackgroundImageContent{Img: d.img, IntrinsicW: d.w, IntrinsicH: d.h}
}

// svgBackgroundIntrinsic returns the intrinsic size a background-sizing computation
// (background-size: auto/cover/contain, and a single-axis explicit size) should use
// for an SVG source.
//
// The un-defaulted facts come from svgIntrinsic, which is the same accessor the
// replaced-element path uses — so `cover`/`contain` scale by the SVG's real aspect
// ratio, including the common viewBox-only case where the document's own WidthPt /
// HeightPt have already been defaulted to 300x150 and would give the wrong ratio.
// When the SVG states neither a size nor a ratio, CSS's 300x150 default applies,
// which is what pkg/svg already resolved into WidthPt/HeightPt.
func svgBackgroundIntrinsic(doc *svg.Document) (iw, ih float64) {
	if w, h, ok := svgIntrinsic(doc); ok && w > 0 && h > 0 {
		return w, h
	}
	return doc.WidthPt, doc.HeightPt
}

// bgBox returns the page-space rectangle for a background-origin/clip box keyword,
// deriving padding and content boxes from the border box and the edge widths.
func bgBox(keyword string, bx, by, bw, bh float64, ed edges) (x, y, w, h float64) {
	switch keyword {
	case "border-box":
		return bx, by, bw, bh
	case "content-box":
		return bx + ed.bL + ed.pL, by + ed.bT + ed.pT,
			bw - ed.bL - ed.bR - ed.pL - ed.pR, bh - ed.bT - ed.bB - ed.pT - ed.pB
	default: // padding-box (the CSS initial background-origin)
		return bx + ed.bL, by + ed.bT, bw - ed.bL - ed.bR, bh - ed.bT - ed.bB
	}
}

// bgSizeAxis resolves one explicit background-size axis to px against the origin axis
// size; auto yields 0 (the painter treats ≤0 as auto for that axis).
func bgSizeAxis(l gcss.Length, fontSizePt, originAxis float64) float64 {
	if l.Unit == gcss.UnitAuto {
		return 0
	}
	v, _ := resolveLen(l, fontSizePt, originAxis)
	return v
}

// bgPosAxis resolves one background-position axis: a percentage is kept as a fraction
// (isPct=true, frac in [0,1]); any other length resolves to an absolute px offset.
func bgPosAxis(l gcss.Length, fontSizePt float64) (isPct bool, frac, px float64) {
	if l.Unit == gcss.UnitPercent {
		return true, l.Value / 100, 0
	}
	v, _ := resolveLen(l, fontSizePt, 0)
	return false, 0, v
}

// bgRepeatAxes maps a background-repeat keyword to per-axis tiling flags.
func bgRepeatAxes(repeat string) (x, y bool) {
	switch repeat {
	case "repeat-x":
		return true, false
	case "repeat-y":
		return false, true
	case "no-repeat":
		return false, false
	default: // repeat (and space/round, which degrade to repeat)
		return true, true
	}
}
