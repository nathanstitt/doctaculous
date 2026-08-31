// Package paint draws a laid-out page (pkg/layout) onto a render.Device. It is
// format-neutral: it consumes only positioned glyphs and rules in page space and
// knows nothing about DOCX/HTML/EPUB. Together with pkg/render/raster this turns
// the engine's output into pixels.
package paint

import (
	"image/color"

	"github.com/nathanstitt/omnidoc/pkg/internal/font"
	"github.com/nathanstitt/omnidoc/pkg/internal/layout"
	"github.com/nathanstitt/omnidoc/pkg/internal/render"
)

// imageDest is a destination rectangle in page space (points, Y-down) into which a
// replaced image's full pixel grid is drawn. For object-fit modes that scale
// uniformly (contain/cover/none/scale-down) it may be larger or smaller than, and
// offset from, the content box; the caller clips to the content box when it
// overflows.
type imageDest struct {
	x, y, w, h float64
}

// Options configures one PaintPage call. The zero value is exactly the
// historical behavior, so a caller that does not care about diagnostics keeps
// using PaintPage and pays nothing for the option it did not set.
type Options struct {
	// Logf, if set, receives one debug line per DISTINCT fidelity degradation
	// hit while painting this page — currently the two CSS `filter` caps
	// (maxCSSFilterPixels and maxFilterNestingDepth), each emitted at most once
	// per PaintPageWithOptions call so a page full of filtered boxes cannot turn
	// into a page full of log lines. nil means silent, which is what plain
	// PaintPage passes.
	//
	// It matches the signature every other degradation logger in the engine uses
	// (pkg/svg/draw.Renderer.Logf, pkg/render/raster.Options.Logf,
	// pkg/render/pdfwrite.Options.Logf), so a caller threads ONE func through
	// the whole pipeline rather than adapting between logger types.
	Logf func(string, ...any)
}

// PaintPage draws every item of page onto dev. mat maps page space (points,
// Y-down, origin at the page's top-left) into device space (pixels); for a simple
// rasterization it is a uniform scale of dpi/72.
//
// Degradations are painted but not reported; use PaintPageWithOptions to receive
// them.
func PaintPage(dev render.Device, page *layout.Page, mat render.Matrix) {
	PaintPageWithOptions(dev, page, mat, Options{})
}

// PaintPageWithOptions is PaintPage with per-call options — currently just a
// diagnostics logger.
//
// It is a SEPARATE entry point rather than an extra parameter on PaintPage
// because PaintPage is the painter's public seam and is called from three
// packages plus the tests; widening its signature would churn every caller for
// a knob almost none of them set. The engine already spells this the same way
// elsewhere (layoutfont.NewOSFontProvider / NewOSFontProviderWithLogf), and the
// Options struct — rather than a bare logf argument — is what lets a future knob
// land without a third entry point.
func PaintPageWithOptions(dev render.Device, page *layout.Page, mat render.Matrix, opts Options) {
	// warnFlags is allocated per call, never stored, so concurrent PaintPage
	// calls (pdfwrite renders its bands on a worker pool) cannot race on it or
	// suppress each other's first notice. With no logger there is nothing to
	// suppress, so it stays a nil pointer and the whole mechanism costs one
	// pointer-nil check per degradation — nothing at all on the correct path.
	var warned *warnFlags
	if opts.Logf != nil {
		warned = &warnFlags{logf: opts.Logf}
	}
	paintItems(dev, page.Items, mat, 0, warned)
}

// warnFlags tracks, for one PaintPageWithOptions call, which one-per-page
// degradation notices have already been emitted, and carries the logger they go
// to.
//
// A nil *warnFlags is the "no logger" case and every method below tolerates it,
// which is what keeps the logger-less path (PaintPage, and every backend that
// leaves Options.Logf nil) allocation-free: no flags struct is created and no
// closure is captured.
//
// This mirrors pkg/svg/draw's identically-named type for the same reason it
// exists there — a page with a hundred over-cap filtered boxes must produce one
// line per distinct cause, not a hundred.
type warnFlags struct {
	logf func(string, ...any)

	// filterRegionCap/filterNestingCap track the two ways a CSS filter degrades
	// to painting its bracket unfiltered with a cause worth naming: a surface
	// past maxCSSFilterPixels (or one that is degenerate/off-device) and a
	// bracket nested past maxFilterNestingDepth. They are deliberately separate
	// flags: the two have entirely different causes, and reporting a nesting
	// overflow as "region exceeded N pixels" sends a reader looking at a region
	// that was perfectly fine.
	filterRegionCap  bool
	filterNestingCap bool
}

// logFilterRegionCapOnce emits the surface-unavailable notice the first time it
// is needed for this page. reason names WHY the surface could not be built, so
// an over-cap page-sized filter is not confused with a degenerate box.
func (w *warnFlags) logFilterRegionCapOnce(reason string) {
	if w == nil || w.filterRegionCap || w.logf == nil {
		return
	}
	w.filterRegionCap = true
	w.logf("paint: CSS filter surface unavailable (%s); the element was painted unfiltered", reason)
}

// logFilterNestingCapOnce emits the filter-nesting-too-deep notice the first
// time it is needed for this page.
func (w *warnFlags) logFilterNestingCapOnce() {
	if w == nil || w.filterNestingCap || w.logf == nil {
		return
	}
	w.filterNestingCap = true
	w.logf("paint: CSS filter nesting exceeded %d levels; the element was painted unfiltered", maxFilterNestingDepth)
}

// paintItems draws one contiguous run of items. It is separate from PaintPage
// because a CSS `filter` bracket paints its own sub-run into an offscreen
// surface (see paintFilterBracket) and needs to re-enter with that sub-run and a
// shifted matrix — the flat item list has no other way to express a nested
// paint.
//
// warned may be nil (no logger); see warnFlags.
func paintItems(dev render.Device, items []layout.Item, mat render.Matrix, depth int, warned *warnFlags) {
	for i := 0; i < len(items); i++ {
		it := &items[i]
		if it.Kind == layout.TransformPushKind {
			// A CSS transform composes into the page matrix for the bracketed run:
			// everything inside paints through it, and the matrix is restored after.
			// An unmatched push takes the rest of the list, matching the filter
			// bracket's own tolerance for a corrupted stream.
			end := matchingTransformPop(items, i)
			paintItems(dev, items[i+1:end], it.Transform.Mul(mat), depth, warned)
			i = end
			continue
		}
		if it.Kind == layout.FilterPushKind {
			// The bracketed run is [i+1, end); end indexes the matching pop.
			// An UNMATCHED push (impossible from the emission side, but a
			// hand-built or corrupted stream could carry one) takes the rest of
			// the list and closes at its end, so the content still paints.
			end := matchingFilterPop(items, i)
			paintFilterBracket(dev, it, items[i+1:end], mat, depth, warned)
			i = end
			continue
		}
		paintItem(dev, it, mat)
	}
}

// matchingTransformPop returns the index of the TransformPopKind matching the push at
// i, or len(items) when the stream is unbalanced.
func matchingTransformPop(items []layout.Item, i int) int {
	depth := 0
	for j := i; j < len(items); j++ {
		switch items[j].Kind {
		case layout.TransformPushKind:
			depth++
		case layout.TransformPopKind:
			depth--
			if depth == 0 {
				return j
			}
		}
	}
	return len(items)
}

// maxFilterNestingDepth bounds how many CSS filters may be applied at once (a
// filtered box inside another filtered box's subtree).
//
// Each level holds an offscreen RGBA alive while its chain runs, PLUS that
// chain's own per-primitive float32 buffers, so unbounded nesting is unbounded
// LIVE memory rather than merely CPU time — maxCSSFilterPixels bounds one
// level's surface, not the product of N of them. It matches pkg/svg/draw's
// identically-named limit for the same reason and at the same value; real
// documents never nest filters at all.
//
// Exceeding it degrades to painting the content unfiltered (see
// paintFilterBracket), never to a crash or a blank.
const maxFilterNestingDepth = 4

// matchingFilterPop returns the index of the FilterPopKind closing the push at
// index push, counting nesting so an inner bracket does not close the outer one.
// With no matching pop it returns len(items), so the caller paints the rest of
// the run inside the bracket rather than dropping it.
func matchingFilterPop(items []layout.Item, push int) int {
	depth := 0
	for i := push; i < len(items); i++ {
		switch items[i].Kind {
		case layout.FilterPushKind:
			depth++
		case layout.FilterPopKind:
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return len(items)
}

// paintItem draws one non-bracket item. A stray FilterPopKind (one paintItems
// did not consume as part of a bracket) is a no-op: render.Device documents an
// unbalanced EndGroup the same way, and the painter must never panic on a
// malformed stream.
func paintItem(dev render.Device, it *layout.Item, mat render.Matrix) {
	switch it.Kind {
	case layout.GlyphKind:
		paintGlyph(dev, &it.Glyph, mat)
	case layout.RuleKind:
		paintRule(dev, &it.Rule, mat)
	case layout.BackgroundKind:
		// A background is just a filled rectangle behind content; reuse the rule
		// path (Item.Rule carries its geometry and color).
		paintRule(dev, &it.Rule, mat)
	case layout.BorderKind:
		paintBorder(dev, &it.Border, mat)
	case layout.ImageKind:
		paintImage(dev, &it.Image, mat)
	case layout.BackgroundImageKind:
		paintBackgroundImage(dev, &it.BgImage, mat)
	case layout.ClipPushKind:
		// Save the clip state, then intersect the active clip with the rect (mapped
		// through the page matrix). A degenerate rect makes clipRect a no-op push, but
		// Save/Restore still balance, so the stream stays well-formed.
		dev.Save()
		clipRoundedRect(dev, mat, it.Rule.XPt, it.Rule.YPt, it.Rule.WPt, it.Rule.HPt, it.Rule.Radii)
	case layout.ClipPopKind:
		dev.Restore()
	case layout.TransformPushKind, layout.TransformPopKind:
		// Handled by the caller, which owns the matrix stack (see paintItems).
	case layout.VectorKind:
		paintVector(dev, &it.Vector, mat)
	case layout.ShadowKind:
		paintShadow(dev, &it.Shadow, mat)
	}
}

// paintFilterBracket paints one CSS `filter` bracket: the items of inner
// (everything between the push and its matching pop) rendered through push's
// filter chain.
//
// The pipeline mirrors the SVG one (pkg/svg/draw's paintFiltered), because it is
// the same problem: rasterize the bracketed content into an isolated offscreen
// surface (render.Device.RenderOffscreen), convert those pixels into filter
// buffers, run the chain, and draw the result back as an image at the surface's
// device-space position.
//
// It degrades to an offscreen GROUP — dev.BeginGroup, paint, dev.EndGroup at
// alpha 1 — whenever the chain cannot run:
//
//   - the backend has no offscreen raster surface (pdfwrite's RenderOffscreen
//     returns nil, by design: PDF has no filter operator and a blur has no
//     vector representation, so a PDF stays vector-native and paints the content
//     unfiltered — logged by pkg/render/pdfwrite);
//   - the region is degenerate, fully off-device, or exceeds
//     maxCSSFilterPixels — logged here through warned;
//   - nesting exceeds maxFilterNestingDepth — logged here through warned;
//   - the chain is empty (a bracket carrying no functions).
//
// Only the two CAPPED cases log. An empty chain is `filter: none` reaching the
// painter and is not a degradation at all, and the no-offscreen case is a
// property of the OUTPUT FORMAT rather than of this page, which is why
// pkg/render/pdfwrite reports it once per document from the item stream instead
// of once per bracket from here.
//
// Every one of those paths still paints the content, correctly placed, just
// without the effect — the "a visible approximation beats a blank" rule the SVG
// side follows. The group (rather than a bare re-entry) is what keeps that path
// byte-identical to the pass-through this replaced.
func paintFilterBracket(dev render.Device, push *layout.Item, inner []layout.Item, mat render.Matrix, depth int, warned *warnFlags) {
	fi := &push.Filter
	unfiltered := func() {
		dev.BeginGroup()
		paintItems(dev, inner, mat, depth, warned)
		dev.EndGroup(1, "", nil, nil)
	}
	if len(fi.Funcs) == 0 {
		unfiltered()
		return
	}
	if depth >= maxFilterNestingDepth {
		warned.logFilterNestingCapOnce()
		unfiltered()
		return
	}
	devW, devH := dev.Size()
	fs, scale, reason := cssFilterSurface(fi, inner, mat, devW, devH)
	if reason != surfaceOK {
		warned.logFilterRegionCapOnce(reason.String())
		unfiltered()
		return
	}

	// Paint the bracketed run into the offscreen surface with the surface's
	// origin shifted to (0,0) — RenderOffscreen always allocates from the
	// origin, so the same shift that makes maxCSSFilterPixels bound the
	// allocation has to be applied to the geometry going in, and undone when
	// the result is placed.
	shifted := mat.Mul(render.Translate(float64(-fs.origin.X), float64(-fs.origin.Y)))
	src := dev.RenderOffscreen(fs.size, func(scratch render.Device) {
		paintItems(scratch, inner, shifted, depth+1, warned)
	})
	if src == nil {
		unfiltered()
		return
	}

	out := applyCSSFilterChain(src, fi.Funcs, fi.ShadowColors, scale)
	if out == nil {
		return // the chain consumed the content entirely
	}
	b := out.Bounds()
	if b.Empty() {
		return
	}
	// Place the result. The Y scale is NEGATED and the anchor taken at the
	// rect's BOTTOM edge because DrawImage maps the unit square in PDF IMAGE
	// space, where v runs Y-UP with the image's TOP row at v=1 (see
	// render.Device.DrawImage and the raster backend's `1-v` sampling). A plain
	// Scale.Mul(Translate) lands the result VERTICALLY MIRRORED — which reads as
	// a plausible-looking offset rather than an obvious error.
	place := render.Scale(float64(b.Dx()), -float64(b.Dy())).
		Mul(render.Translate(
			float64(fs.origin.X+b.Min.X),
			float64(fs.origin.Y+b.Max.Y),
		))
	dev.DrawImage(out, place, 1, "")
}

// paintGlyph fills one glyph. The outline is in em units (Y up); compose the
// transform em → page points → device:
//
//	scale(size, -size)  — em to points, flipping Y so the font's up becomes page down
//	rotate(Rotate)      — text-orientation, about the glyph's own origin (usually absent)
//	translate(X, Y)     — move to the glyph's baseline origin in page space
//	mat                 — page points to device pixels
//
// paintGlyph draws one glyph. When the glyph carries font identity (Face+GID), it
// uses DrawGlyph so text-emitting backends (PDF) can embed real text; otherwise it
// falls back to filling the raw outline. The em -> device transform is the same in
// both cases: Scale(size,-size) · [Rotate] · Translate(X,Y) · mat.
func paintGlyph(dev render.Device, g *layout.GlyphItem, mat render.Matrix) {
	m := render.Scale(g.SizePt, -g.SizePt)
	// text-orientation rotates the glyph about its OWN origin, which is why this sits
	// between the em scale and the translate to page position: at this point the origin
	// is still (0,0). Rotating after the translate would swing the glyph around the
	// page origin instead — visually dramatic, and the easy mistake to make here.
	//
	// The angle composes here UNNEGATED, which is worth stating because the em scale it
	// sits inside carries a -1 Y flip and that invites a compensating negation. Measured
	// through the composed matrix: at +90 degrees the em-space advance direction (1,0)
	// maps to page (0,+1) — straight DOWN, which is what a vertical line needs. Negating
	// sends it up the page instead.
	//
	// Guarded rather than composed unconditionally so an unrotated glyph — every glyph
	// in every horizontal document — takes the exact matrix it did before this existed.
	if g.Rotate != 0 {
		m = m.Mul(render.Rotate(g.Rotate))
	}
	m = m.Mul(render.Translate(g.XPt, g.YPt)).Mul(mat)
	// A colour glyph (COLR/CPAL) paints as a stack of coloured outlines rather than one
	// filled path. Expanding it HERE rather than in the item stream means every backend
	// — raster, PDF, anything reading layout.Item — gets colour emoji without changing
	// its own glyph handling, and a face with no colour tables costs one nil check.
	if paintColorGlyph(dev, g, m) {
		return
	}
	if paintColorBitmapGlyph(dev, g, mat) {
		return
	}
	if g.Face != nil {
		dev.DrawGlyph(render.GlyphRef{
			Face:      g.Face,
			GID:       g.GID,
			Runes:     g.Runes,
			Transform: m,
			Color:     render.FillColor{R: g.Color.R, G: g.Color.G, B: g.Color.B, A: g.Color.A},
		})
		return
	}
	if g.Outline == nil || g.Outline.Empty() {
		return
	}
	dev.FillGlyph(transformPath(g.Outline, m), render.FillColor{
		R: g.Color.R, G: g.Color.G, B: g.Color.B, A: g.Color.A,
	}, "")
}

// paintColorGlyph paints a COLR/CPAL colour glyph as its layer stack, returning false
// when the glyph has no colour data (the caller then paints it normally).
//
// Each layer is an ordinary outline filled with its own colour, bottom layer first. A
// layer marked Foreground takes the run's text colour, which is how a colour font marks
// the parts meant to follow the document rather than the palette.
//
// The layer transform is applied in FONT UNITS, inside the em scale, because that is
// the space COLR expresses it in: the outline and the offset share one coordinate
// system, so composing them before the em scale keeps them aligned at any size.
func paintColorGlyph(dev render.Device, g *layout.GlyphItem, m render.Matrix) bool {
	if g.Face == nil || !g.Face.HasColorGlyphs() {
		return false
	}
	layers, ok := g.Face.ColorLayers(g.GID)
	if !ok || len(layers) == 0 {
		return false
	}
	upem := g.Face.UnitsPerEm()
	if upem <= 0 {
		return false
	}
	painted := false
	for _, l := range layers {
		outline := g.Face.Outline(l.GID)
		if outline == nil || outline.Empty() {
			continue // an empty layer is legal; it contributes no ink
		}
		col := l.Color
		if l.Foreground {
			col = g.Color
		}
		if col.A == 0 && l.Gradient == nil {
			continue
		}
		lm := m
		if !l.IsIdentity() {
			// The layer's affine is in FONT units; the outline is in em units, so the
			// translation is scaled down by upem while the linear part carries over
			// unchanged (it is dimensionless).
			xx, yx, xy, yy, dx, dy := l.Transform()
			lm = render.Matrix{
				A: xx, B: yx,
				C: xy, D: yy,
				E: dx / upem, F: dy / upem,
			}.Mul(m)
		}
		path := transformPath(outline, lm)
		if l.Gradient != nil {
			// A gradient layer is the outline used as a CLIP with the shading filling
			// it, which is how the render layer expresses a non-flat fill. The shader
			// works in em units, matching the transformed path.
			fillGradientLayer(dev, path, l.Gradient, lm, upem)
			painted = true
			continue
		}
		dev.FillGlyph(path, render.FillColor{
			R: col.R, G: col.G, B: col.B, A: col.A,
		}, "")
		painted = true
	}
	return painted
}

// fillGradientLayer paints one COLR v1 gradient layer: clip to the layer's outline,
// then fill that region with the gradient. The gradient geometry arrives in font
// units, so it is mapped through the same matrix as the outline.
func fillGradientLayer(dev render.Device, path *render.Path, g *font.ColorGradient, lm render.Matrix, upem float64) {
	sh := newColorGradientShader(g, upem)
	if sh == nil {
		return
	}
	dev.Save()
	dev.PushClip(path, render.NonZero)
	dev.FillShading(sh, lm, "")
	dev.Restore()
}

// paintColorBitmapGlyph paints a colour glyph stored as a bitmap strike (sbix or
// CBDT), returning false when the face has none.
//
// Unlike the COLR path this draws an IMAGE, so it takes the page matrix directly
// rather than the glyph's em-scaled one: the image is placed by its own box in page
// space, sized by (em size / strike ppem).
func paintColorBitmapGlyph(dev render.Device, g *layout.GlyphItem, mat render.Matrix) bool {
	if g.Face == nil || !g.Face.HasColorBitmaps() {
		return false
	}
	bm, ok := g.Face.ColorBitmapFor(g.GID, g.SizePt)
	if !ok || bm.Img == nil || bm.PPEM <= 0 {
		return false
	}
	b := bm.Img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return false
	}
	// The strike is designed for bm.PPEM pixels per em, so one strike pixel is
	// (SizePt / PPEM) points. The origin offset is in the same pixel space.
	scale := g.SizePt / bm.PPEM
	w := float64(b.Dx()) * scale
	h := float64(b.Dy()) * scale
	// Y is measured DOWN in page space; BearingY is the height of the image's TOP
	// above the baseline in strike pixels, so the top edge sits that far above the pen
	// once scaled.
	//
	// A strike that declares no bearing (Apple Color Emoji reports 0) is designed to
	// sit on the baseline the way the font's own metrics describe, i.e. with its
	// descent below it — so the face's descent supplies the missing placement. Using
	// zero literally would drop the glyph by a full image height, which reads as emoji
	// hanging below the line.
	bearing := bm.BearingY
	if !bm.HasBearing {
		bearing = float64(b.Dy())
		if _, desc, _ := g.Face.Metrics(); desc > 0 {
			bearing -= desc * bm.PPEM
		}
	}
	x := g.XPt + bm.OriginX*scale
	y := g.YPt - bearing*scale
	// DrawImage maps the image's unit square through the ctm, so the size goes into
	// the matrix rather than into separate arguments.
	ctm := render.Scale(w, h).Mul(render.Translate(x, y)).Mul(mat)
	dev.DrawImage(bm.Img, ctm, 1, "")
	return true
}

// paintRule fills an axis-aligned rectangle (underline/background) in page space,
// rounding its corners when the item carries border radii.
func paintRule(dev render.Device, r *layout.RuleItem, mat render.Matrix) {
	if !r.Radii.Zero() {
		fillRoundedRect(dev, mat, r.XPt, r.YPt, r.WPt, r.HPt, r.Radii, r.Color)
		return
	}
	fillRect(dev, mat, r.XPt, r.YPt, r.XPt+r.WPt, r.YPt+r.HPt, r.Color)
}

// fillRoundedRect fills the rounded rectangle at (x,y) sized w×h with c. The radii
// arrive already overlap-corrected from the layout engine, so this is a pure
// path-build-and-fill. Filling the rounded path DIRECTLY (rather than clipping a
// square fill to it) is what lets the backend antialias the arcs with its own
// coverage rasterizer.
func fillRoundedRect(dev render.Device, mat render.Matrix, x, y, w, h float64, r layout.CornerRadii, c color.RGBA) {
	p := roundedRectPath(mat, x, y, w, h, r)
	if p == nil {
		return
	}
	dev.Fill(p, render.FillPaint{Color: c, Rule: render.NonZero})
}

// roundedRectPath builds the device-space path for a rounded rectangle, or nil for
// a degenerate one. It is the single place the painter turns layout radii into
// geometry, so the fill, the clip, and the border ring cannot drift apart.
func roundedRectPath(mat render.Matrix, x, y, w, h float64, r layout.CornerRadii) *render.Path {
	if w <= 0 || h <= 0 {
		return nil
	}
	p := &render.Path{}
	layout.AppendRoundedRect(p, x, y, w, h, r, mat.Apply)
	if p.Empty() {
		return nil
	}
	return p
}

// fillRect fills the axis-aligned page-space rectangle [x0,x1]×[y0,y1] with c,
// mapping its corners through mat. A degenerate (zero/negative-area) rect draws
// nothing, matching the painter's never-panic contract for degenerate input.
func fillRect(dev render.Device, mat render.Matrix, x0, y0, x1, y1 float64, c color.RGBA) {
	if x1 <= x0 || y1 <= y0 {
		return
	}
	p := &render.Path{}
	moveTo(p, mat, x0, y0)
	lineTo(p, mat, x1, y0)
	lineTo(p, mat, x1, y1)
	lineTo(p, mat, x0, y1)
	p.Close()
	dev.Fill(p, render.FillPaint{
		Color: c,
		Rule:  render.NonZero,
	})
}

// paintBorder draws one styled border edge. The edge is a full-length strip whose
// rectangle the caller (the CSS layout engine) computed; corner mitering between
// adjacent strips is out of scope, so dashes/dots simply run the strip's length.
//
//	solid  — fill the whole strip.
//	double — fill the outer and inner thirds across the strip's thickness, leaving
//	         the middle third empty.
//	dashed — tile filled rects along the strip's length, dash ≈ gap ≈ 3×thickness.
//	dotted — like dashed but dash = gap = thickness (square dots).
//
// The thickness axis and length axis depend on Side: top/bottom strips are
// horizontal (thickness along Y, length along X); left/right strips are vertical
// (thickness along X, length along Y).
func paintBorder(dev render.Device, b *layout.BorderItem, mat render.Matrix) {
	if b.Style == layout.BorderNone || b.WPt <= 0 || b.HPt <= 0 {
		return
	}
	if b.Ring != nil {
		paintBorderRing(dev, b, mat)
		return
	}
	x0, y0 := b.XPt, b.YPt
	x1, y1 := b.XPt+b.WPt, b.YPt+b.HPt
	horizontal := b.Side == layout.EdgeTop || b.Side == layout.EdgeBottom

	switch b.Style {
	case layout.BorderSolid:
		fillRect(dev, mat, x0, y0, x1, y1, b.Color)

	case layout.BorderDouble:
		// Split across the thickness axis into thirds; fill the outer and inner band.
		if horizontal {
			t := b.HPt / 3
			fillRect(dev, mat, x0, y0, x1, y0+t, b.Color)
			fillRect(dev, mat, x0, y1-t, x1, y1, b.Color)
		} else {
			t := b.WPt / 3
			fillRect(dev, mat, x0, y0, x0+t, y1, b.Color)
			fillRect(dev, mat, x1-t, y0, x1, y1, b.Color)
		}

	case layout.BorderDashed, layout.BorderDotted:
		// thick is the strip's thickness; dash/gap are measured along its length.
		thick := b.HPt
		if !horizontal {
			thick = b.WPt
		}
		dash := 3 * thick
		if b.Style == layout.BorderDotted {
			dash = thick
		}
		gap := dash
		step := dash + gap
		if step <= 0 {
			return
		}
		if horizontal {
			for x := x0; x < x1; x += step {
				end := x + dash
				if end > x1 {
					end = x1 // clamp the final dash to the strip
				}
				fillRect(dev, mat, x, y0, end, y1, b.Color)
			}
		} else {
			for y := y0; y < y1; y += step {
				end := y + dash
				if end > y1 {
					end = y1 // clamp the final dash to the strip
				}
				fillRect(dev, mat, x0, y, x1, end, b.Color)
			}
		}

	case layout.BorderOutset, layout.BorderInset:
		// 3D edge: fill the whole strip with the light or dark shade chosen by side.
		// outset = top/left light, bottom/right dark; inset is the inverse.
		light := b.Style == layout.BorderOutset
		fillRect(dev, mat, x0, y0, x1, y1, edge3DColor(b.Color, b.Side, light))

	case layout.BorderRidge, layout.BorderGroove:
		// 3D ridge/groove: split the strip across its thickness into an outer and inner
		// half, painting them with opposite light/dark shades. ridge = outer behaves
		// like outset; groove is the inverse.
		outerLight := b.Style == layout.BorderRidge
		outer := edge3DColor(b.Color, b.Side, outerLight)
		inner := edge3DColor(b.Color, b.Side, !outerLight)
		if horizontal {
			mid := (y0 + y1) / 2
			fillRect(dev, mat, x0, y0, x1, mid, outer)
			fillRect(dev, mat, x0, mid, x1, y1, inner)
		} else {
			mid := (x0 + x1) / 2
			fillRect(dev, mat, x0, y0, mid, y1, outer)
			fillRect(dev, mat, mid, y0, x1, y1, inner)
		}
	}
}

// paintBorderRing fills a rounded box's whole border as ONE even-odd path: the
// outer (border-box) rounded rectangle followed by the inner (padding-box) one, so
// the interior falls out as a hole and only the ring is inked.
//
// The even-odd rule is what makes the hole appear. Both sub-paths are emitted in
// the same (clockwise) direction by AppendRoundedRect, so under the nonzero rule
// their windings would ADD and the whole outer shape would fill solid — the ring
// would vanish into a filled box. Reversing the inner path to make nonzero work is
// the usual alternative; even-odd is chosen instead because it needs no second
// traversal order and both backends that consume this (raster's coverage
// rasterizer, pdfwrite's `f*`) implement it natively.
//
// A fully-collapsed inner rectangle (borders thicker than the box) simply yields no
// hole, so the box fills solid — which is what a border that consumes the whole box
// should look like.
func paintBorderRing(dev render.Device, b *layout.BorderItem, mat render.Matrix) {
	ring := b.Ring
	outer := roundedRectPath(mat, b.XPt, b.YPt, b.WPt, b.HPt, ring.Outer)
	if outer == nil {
		return
	}
	ix := b.XPt + ring.Left
	iy := b.YPt + ring.Top
	iw := b.WPt - ring.Left - ring.Right
	ih := b.HPt - ring.Top - ring.Bottom
	// A nil inner path (borders meet or overlap) leaves `outer` alone: no hole.
	if inner := roundedRectPath(mat, ix, iy, iw, ih, ring.Inner); inner != nil {
		outer.Segments = append(outer.Segments, inner.Segments...)
	}
	dev.Fill(outer, render.FillPaint{Color: b.Color, Rule: render.EvenOdd})
}

// clipRoundedRect intersects the device clip with a rounded rectangle, falling back
// to the plain rectangular clip when no corner is rounded so the square-cornered
// path stays exactly as it was.
func clipRoundedRect(dev render.Device, mat render.Matrix, x, y, w, h float64, r layout.CornerRadii) {
	if r.Zero() {
		clipRect(dev, mat, x, y, x+w, y+h)
		return
	}
	if p := roundedRectPath(mat, x, y, w, h, r); p != nil {
		dev.PushClip(p, render.NonZero)
	}
}

// edge3DColor returns the shade for a 3D border edge: the "light" side of the bevel
// (top/left when raised) keeps the base color, the "dark" side is darkened to ~half.
// light selects whether THIS edge is on the lit side (caller derives it from the style
// and side). The top and left edges are lit when light is true; bottom and right edges
// are the opposite, so the caller passes the already-resolved light flag and this only
// flips it for bottom/right. (Matches browser bevels: a raised box lights top+left.)
func edge3DColor(c color.RGBA, side layout.EdgeSide, light bool) color.RGBA {
	// Bottom/right edges are the opposite face of the bevel from top/left.
	if side == layout.EdgeBottom || side == layout.EdgeRight {
		light = !light
	}
	if light {
		return c
	}
	return color.RGBA{R: c.R / 2, G: c.G / 2, B: c.B / 2, A: c.A}
}

// paintImage draws a replaced-element image into its content box under the chosen
// object-fit mapping. it.XPt,YPt,WPt,HPt is the content box in page space (points,
// Y-down). The destination rectangle (after object-fit) maps the image's unit
// square upright into page space:
//
//	Mimg = scale(destW, -destH) · translate(destX, destY+destH)
//
// At image-bottom (v=0) y = destY+destH; at image-top (v=1) y = destY. This matches
// render.Device.DrawImage, which samples the source row from (1-v), so the image
// renders upright. Mimg is then composed with mat (page→device). When the
// destination overflows the content box (cover / oversized none), the content box is
// pushed as a clip so only the box-sized region is painted.
func paintImage(dev render.Device, it *layout.ImageItem, mat render.Matrix) {
	if it.Img == nil || it.WPt <= 0 || it.HPt <= 0 {
		return
	}
	b := it.Img.Bounds()
	iw, ih := float64(b.Dx()), float64(b.Dy())
	if iw <= 0 || ih <= 0 {
		return
	}

	d := fitDest(it.Fit, it.XPt, it.YPt, it.WPt, it.HPt, iw, ih, it.PosX, it.PosY)
	if d.w <= 0 || d.h <= 0 {
		return
	}

	// Clip to the content box when the fitted image can extend beyond it (cover, or
	// an oversized none/scale-down). fill/contain never overflow, so they skip the
	// clip (and its save/restore cost).
	clip := d.x < it.XPt-epsilon || d.y < it.YPt-epsilon ||
		d.x+d.w > it.XPt+it.WPt+epsilon || d.y+d.h > it.YPt+it.HPt+epsilon
	if clip {
		dev.Save()
		clipRect(dev, mat, it.XPt, it.YPt, it.XPt+it.WPt, it.YPt+it.HPt)
	}

	mImg := render.Scale(d.w, -d.h).Mul(render.Translate(d.x, d.y+d.h))
	dev.DrawImage(it.Img, mImg.Mul(mat), 1, "")

	if clip {
		dev.Restore()
	}
}

// paintVector draws a vector scene into its page-space box: clip to the box
// (the SVG viewport clips), then let the scene draw with a ctm that maps its
// viewport coordinates to device space. A nil Scene draws nothing.
func paintVector(dev render.Device, v *layout.VectorItem, mat render.Matrix) {
	if v.Scene == nil {
		return
	}
	dev.Save()
	clipRect(dev, mat, v.XPt, v.YPt, v.XPt+v.WPt, v.YPt+v.HPt)
	v.Scene.DrawVector(dev, render.Translate(v.XPt, v.YPt).Mul(mat))
	dev.Restore()
}

// epsilon guards the overflow comparison against float rounding so an
// exactly-fitting image isn't needlessly clipped.
const epsilon = 1e-6

// fitDest computes the destination rectangle (page space) the image's full pixel
// grid maps into, for content box (cx,cy,cw,ch) and intrinsic size (iw,ih), under
// fit. fill stretches to the box; contain/cover scale uniformly by the min/max axis
// ratio and center; none uses intrinsic size centered; scale-down picks the smaller
// of none and contain. The result may exceed the content box (cover, oversized
// none) — the caller clips.
func fitDest(fit layout.ObjectFit, cx, cy, cw, ch, iw, ih, posX, posY float64) imageDest {
	// positioned places a w×h image within the content box at the object-position
	// fractions (posX/posY of the free space cw-w / ch-h). posX=posY=0.5 centers it
	// (the default), reproducing the prior behavior exactly.
	positioned := func(w, h float64) imageDest {
		return imageDest{x: cx + (cw-w)*posX, y: cy + (ch-h)*posY, w: w, h: h}
	}
	switch fit {
	case layout.FitContain:
		s := scaleRatio(cw/iw, ch/ih, true) // fit inside: the smaller ratio
		return positioned(iw*s, ih*s)
	case layout.FitCover:
		s := scaleRatio(cw/iw, ch/ih, false) // cover: the larger ratio
		return positioned(iw*s, ih*s)
	case layout.FitNone:
		return positioned(iw, ih)
	case layout.FitScaleDown:
		// none unless it overflows the box, in which case contain (the smaller image).
		s := scaleRatio(cw/iw, ch/ih, true)
		if s >= 1 {
			return positioned(iw, ih) // intrinsic already fits: use none
		}
		return positioned(iw*s, ih*s)
	default: // FitFill
		return imageDest{x: cx, y: cy, w: cw, h: ch}
	}
}

// scaleRatio returns the smaller of a and b when min is true, else the larger —
// the uniform scale factor for contain (min) and cover (max).
func scaleRatio(a, b float64, min bool) float64 {
	if min {
		if a < b {
			return a
		}
		return b
	}
	if a > b {
		return a
	}
	return b
}

// clipRect intersects the device clip with the axis-aligned page-space rectangle
// [x0,x1]×[y0,y1], mapped through mat. Used to confine an object-fit:cover (or
// oversized) image to its content box.
func clipRect(dev render.Device, mat render.Matrix, x0, y0, x1, y1 float64) {
	if x1 <= x0 || y1 <= y0 {
		return
	}
	p := &render.Path{}
	moveTo(p, mat, x0, y0)
	lineTo(p, mat, x1, y0)
	lineTo(p, mat, x1, y1)
	lineTo(p, mat, x0, y1)
	p.Close()
	dev.PushClip(p, render.NonZero)
}

// transformPath returns a copy of src with every point mapped through m.
func transformPath(src *render.Path, m render.Matrix) *render.Path {
	return render.TransformPath(src, m)
}

func moveTo(p *render.Path, m render.Matrix, x, y float64) {
	dx, dy := m.Apply(x, y)
	p.MoveTo(dx, dy)
}

func lineTo(p *render.Path, m render.Matrix, x, y float64) {
	dx, dy := m.Apply(x, y)
	p.LineTo(dx, dy)
}
