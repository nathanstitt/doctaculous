package css

import (
	"context"

	gcss "github.com/nathanstitt/omnidoc/pkg/css"
	"github.com/nathanstitt/omnidoc/pkg/layout"
	"github.com/nathanstitt/omnidoc/pkg/layout/cssbox"
	"github.com/nathanstitt/omnidoc/pkg/svg"
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
// resolveBackgroundImages resolves every layer of the box's background list, in PAINT
// order (last layer first, so the first layer ends up on top).
//
// A comma-separated list used to be unparseable, so `background: <gradient>, <color>`
// — the ordinary way to give a gradient a fallback colour — dropped the whole
// declaration and the element painted nothing. Resolving a list rather than a single
// image is what makes the layering the property is for actually work.
func (e *Engine) resolveBackgroundImages(ctx context.Context, b *cssbox.Box, borderX, borderY, borderW, borderH float64, ed edges) []*BackgroundImageContent {
	layers := b.Style.BackgroundLayers
	if len(layers) == 0 {
		// No list: fall back to the single-value fields, which the longhand
		// background-image/background-repeat properties still populate.
		if bg := e.resolveBackgroundLayer(ctx, b, b.Style.BackgroundImage, b.Style.BackgroundGradient,
			b.Style.BackgroundRepeat, b.Style.BackgroundPosition, b.Style.BackgroundSize,
			b.Style.BackgroundOrigin, b.Style.BackgroundClip,
			borderX, borderY, borderW, borderH, ed); bg != nil {
			return []*BackgroundImageContent{bg}
		}
		return nil
	}
	out := make([]*BackgroundImageContent, 0, len(layers))
	// Walk BACKWARDS: CSS paints the first layer on top, and the item stream paints in
	// order, so the last layer must be emitted first.
	for i := len(layers) - 1; i >= 0; i-- {
		l := layers[i]
		if !l.HasImage() {
			continue
		}
		// The non-image properties come from the element's computed longhands, which
		// apply to every layer (see ComputedStyle.BackgroundLayers).
		if bg := e.resolveBackgroundLayer(ctx, b, l.Image, l.Gradient,
			b.Style.BackgroundRepeat, b.Style.BackgroundPosition, b.Style.BackgroundSize,
			b.Style.BackgroundOrigin, b.Style.BackgroundClip,
			borderX, borderY, borderW, borderH, ed); bg != nil {
			out = append(out, bg)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (e *Engine) resolveBackgroundLayer(ctx context.Context, b *cssbox.Box, ref string, grad *gcss.Gradient,
	repeat string, position gcss.BackgroundPos, size gcss.BackgroundSize, origin, clip string,
	borderX, borderY, borderW, borderH float64, ed edges) *BackgroundImageContent {
	if ref == "" && grad == nil {
		return nil
	}

	ox0, oy0, ow0, oh0 := bgBox(origin, borderX, borderY, borderW, borderH, ed)

	var bg *BackgroundImageContent
	if grad != nil {
		// A gradient has NO intrinsic size, so it takes the ORIGIN BOX's size as
		// its intrinsic one. That single substitution is what lets every
		// background-* property keep working through the unchanged geometry path:
		// `background-size: auto` then resolves to the origin box (the CSS rule
		// for a sizeless generated image), cover/contain become no-ops, and an
		// explicit size still overrides both.
		if ow0 <= 0 || oh0 <= 0 {
			return nil
		}
		bg = &BackgroundImageContent{IntrinsicW: ow0, IntrinsicH: oh0}
	} else {
		bg = e.backgroundSource(ctx, ref)
	}
	if bg == nil {
		return nil // missing/undecodable: the background color (if any) still paints
	}

	ox, oy, ow, oh := ox0, oy0, ow0, oh0
	cx, cy, cw, ch := bgBox(clip, borderX, borderY, borderW, borderH, ed)
	if ow <= 0 || oh <= 0 || cw <= 0 || ch <= 0 {
		return nil
	}
	bg.OriginX, bg.OriginY, bg.OriginW, bg.OriginH = ox, oy, ow, oh
	bg.ClipX, bg.ClipY, bg.ClipW, bg.ClipH = cx, cy, cw, ch

	// Size.
	fs := b.Style.FontSizePt
	switch size.Kind {
	case gcss.BgSizeCover:
		bg.SizeKind = layout.BgSizeCover
	case gcss.BgSizeContain:
		bg.SizeKind = layout.BgSizeContain
	case gcss.BgSizeExplicit:
		bg.SizeKind = layout.BgSizeExplicit
		bg.SizeW = bgSizeAxis(size.W, fs, ow)
		bg.SizeH = bgSizeAxis(size.H, fs, oh)
	default:
		bg.SizeKind = layout.BgSizeAuto
	}

	// Position: a percentage stays a fraction (resolved against origin−tile at paint
	// time); a length/em resolves to px now.
	bg.PosXIsPct, bg.PosXFrac, bg.PosXPx = bgPosAxis(position.X, fs)
	bg.PosYIsPct, bg.PosYFrac, bg.PosYPx = bgPosAxis(position.Y, fs)

	// Repeat.
	bg.RepeatX, bg.RepeatY = bgRepeatAxes(repeat)
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

	if grad != nil {
		// Resolve the gradient LAST: its geometry is laid out inside one tile,
		// and the tile's size is only known once background-size has been
		// resolved against the origin box above. TileSize is the same
		// computation the painter uses, so the gradient box the author sees
		// matches the cell the painter tiles.
		tw, th := (&layout.BackgroundImageItem{
			IntrinsicW: bg.IntrinsicW, IntrinsicH: bg.IntrinsicH,
			OriginW: ow, OriginH: oh,
			SizeKind: bg.SizeKind, SizeW: bg.SizeW, SizeH: bg.SizeH,
		}).TileSize()
		rg, ok := resolveGradient(grad, tw, th, fs)
		if !ok {
			// A gradient that cannot establish geometry (a degenerate tile, or a
			// zero-radius ending shape) paints nothing. The background COLOUR, if
			// any, still paints — the same degradation an undecodable url() gets.
			e.warnOnce("bg-gradient-degenerate",
				"css layout: background gradient has degenerate geometry (tile %gx%g); not painting it", tw, th)
			return nil
		}
		bg.Gradient = rg
	}
	return bg
}

// resolveGradient converts a parsed CSS gradient into the painter's tile-space
// geometry for a tile of tileW x tileH points. fontSizePt resolves an em stop
// position or radius.
//
// ok=false means no geometry can be established (a degenerate tile, a zero-length
// gradient line, or a zero-radius ending shape) and nothing should paint.
func resolveGradient(g *gcss.Gradient, tileW, tileH, fontSizePt float64) (*layout.BackgroundGradient, bool) {
	if tileW <= 0 || tileH <= 0 {
		return nil, false
	}
	out := &layout.BackgroundGradient{Repeating: g.Repeating}

	// lineLen is what an ABSOLUTE stop position ("black 20px") is a fraction of.
	// For a linear gradient it is the gradient line's own length; for a radial
	// one CSS defines stop positions against the ending shape's HORIZONTAL
	// radius, which is the ray the ramp is parameterized along.
	var lineLen float64

	switch g.Kind {
	case gcss.GradientRadial:
		out.Kind = layout.GradientRadial
		cx := resolvePosAxis(g.Center.X, tileW, fontSizePt)
		cy := resolvePosAxis(g.Center.Y, tileH, fontSizePt)
		rx, ry := g.RadialRadii(tileW, tileH, cx, cy, fontSizePt)
		if rx <= 0 || ry <= 0 {
			return nil, false
		}
		out.CX, out.CY, out.RX, out.RY = cx, cy, rx, ry
		lineLen = rx

	default:
		out.Kind = layout.GradientLinear
		x0, y0, x1, y1, l := g.GradientLine(tileW, tileH)
		if l <= 0 {
			return nil, false
		}
		out.X0, out.Y0, out.X1, out.Y1 = x0, y0, x1, y1
		lineLen = l
	}

	resolved := gcss.NormalizeStops(g.Stops, lineLen, fontSizePt)
	if len(resolved) < 2 {
		return nil, false
	}
	if g.Repeating {
		// A repeating gradient's ramp must be re-parameterized so its stop range
		// becomes exactly [0,1] — the range the shader's repeat spread folds
		// into. Without this, `repeating-linear-gradient(red 0, blue 20px)` on a
		// 100px line would repeat every 100px (the whole line) instead of every
		// 20px, which is the entire point of the property.
		//
		// A ZERO-LENGTH stop range cannot be re-parameterized (every stop at the
		// same position leaves nothing to repeat), and CSS says to render such a
		// gradient as a solid of the last colour. Declining here would drop the
		// paint entirely, so it degrades to the equivalent NON-repeating gradient
		// instead, which paints that solid colour.
		lo, hi := resolved[0].Pos, resolved[len(resolved)-1].Pos
		if span := hi - lo; span > 0 {
			for i := range resolved {
				resolved[i].Pos = (resolved[i].Pos - lo) / span
			}
			// Re-parameterizing moved the ramp's start to 0, so the gradient
			// line must move with it or the first repetition lands in the wrong
			// place. Shift the line's start to where the first stop actually was
			// and its end to where the last was.
			out.Reparameterize(lo, hi)
		} else {
			out.Repeating = false
		}
	}

	out.Stops = make([]layout.GradientStop, len(resolved))
	for i, s := range resolved {
		out.Stops[i] = layout.GradientStop{Pos: s.Pos, Color: s.Color}
	}
	return out, true
}

// resolvePosAxis resolves one `at <position>` component against the tile axis it
// is measured along. It mirrors bgPosAxis, but resolves a percentage IMMEDIATELY
// (against the full axis) rather than deferring it: a gradient centre's
// percentage is a fraction of the gradient BOX, with no tile-size subtraction —
// unlike background-position, whose percentage is a fraction of the free space
// left over after the tile is placed.
func resolvePosAxis(l gcss.Length, axis, fontSizePt float64) float64 {
	if l.Unit == gcss.UnitPercent {
		return l.Value / 100 * axis
	}
	v, _ := resolveLen(l, fontSizePt, axis)
	return v
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
