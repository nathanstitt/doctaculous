package paint

import (
	"image"
	"image/color"
	"math"

	"github.com/nathanstitt/omnidoc/pkg/internal/layout"
	"github.com/nathanstitt/omnidoc/pkg/internal/render"
	svgfilter "github.com/nathanstitt/omnidoc/pkg/internal/svg/filter"
)

// maxShadowPixels bounds how many pixels one BLURRED box-shadow may allocate
// for its offscreen surface.
//
// It exists for the same reason maxCSSFilterPixels does and is set to the same
// value for the same arithmetic: the blur runs through pkg/svg/filter, whose
// Buffer is float32 RGBA (16 bytes per pixel), so 4M pixels is roughly 64 MB
// per buffer. A `box-shadow: 0 0 0 1e7px` is untrusted input exactly as a
// `width:1e7px` box is.
//
// Over the cap the shadow degrades to its UNBLURRED shape (see paintShadow),
// not to nothing: a hard-edged shadow at the right place and size is a visible
// approximation, whereas dropping it would silently lose the box's decoration.
const maxShadowPixels = 4 << 20

// shadowBlurMargin is how far, in multiples of the blur's standard deviation,
// the offscreen surface is grown beyond the shadow's own shape.
//
// Three standard deviations captures ~99.7% of a Gaussian's mass, which is the
// margin the CSS filter path already uses (see filterMargin) and the one
// browsers use. Sizing the surface to the shape alone would cut the blur off
// dead at the shape's edge, turning a soft shadow into a hard-edged one with a
// blurry interior.
const shadowBlurMargin = 3

// paintShadow draws one CSS box-shadow (CSS Backgrounds 3 §6).
//
// There are two rendering paths, and which one runs is decided by the blur
// radius alone:
//
//   - blur == 0: the shadow is a SOLID SHAPE, so it is drawn as a plain vector
//     fill. That keeps a crisp shadow — including the `inset 3px 0 0` spine
//     pattern and every `0 0 0 <spread>` ring — fully vector in a PDF, at no
//     cost and with no rasterization anywhere.
//   - blur > 0: the shape is rasterized into an offscreen surface, blurred
//     through pkg/svg/filter's feGaussianBlur (the same three-box
//     approximation the SVG and CSS `filter` paths use — there is exactly one
//     blur implementation in this repo), and composited back. When the backend
//     has no offscreen surface (pdfwrite's RenderOffscreen returns nil by
//     design), or the surface would be degenerate or over maxShadowPixels, the
//     shadow degrades to the FIRST path: same shape, same place, hard edge.
//
// The distinction between an outer and an inset shadow is NOT a sign flip. An
// outer shadow fills the region OUTSIDE the box's own shape (so it never shows
// through a transparent background) and an inset shadow fills the region INSIDE
// the padding box that the offset/spread shape does not cover. Both are rings,
// and shadowRingPath builds each as a two-subpath even-odd fill.
func paintShadow(dev render.Device, s *layout.ShadowItem, mat render.Matrix) {
	if s.Color.A == 0 || s.WPt <= 0 || s.HPt <= 0 {
		return
	}
	if !finiteShadow(s) {
		return
	}
	if s.Blur <= 0 {
		fillShadowRing(dev, s, mat)
		return
	}
	if paintBlurredShadow(dev, s, mat) {
		return
	}
	// Degraded: the offscreen surface was unavailable or unaffordable. Paint the
	// same shadow with a hard edge rather than dropping it.
	fillShadowRing(dev, s, mat)
}

// finiteShadow rejects a shadow carrying a non-finite parameter, which would
// otherwise reach the rasterizer as a NaN coordinate. A hand-built item or a
// pathological em length is the only way to produce one — the CSS parser
// rejects NaN and infinity — but the painter's contract is to never panic on a
// malformed stream.
func finiteShadow(s *layout.ShadowItem) bool {
	for _, v := range [...]float64{s.XPt, s.YPt, s.WPt, s.HPt, s.OffsetX, s.OffsetY, s.Blur, s.Spread} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return true
}

// shadowShape returns the shadow's own shape in page space: the shadow box
// displaced by the offset and inflated by the spread.
//
// The spread is applied on ALL FOUR sides, so a spread of r grows the shape by
// 2r on each axis — which is what makes `box-shadow: 0 0 0 10px` on a 40x40 box
// paint a 60x60 shadow. A NEGATIVE spread shrinks it, and may shrink it away
// entirely; ok is false in that case.
//
// For an INSET shadow the sign of the spread is INVERTED relative to the outer
// case: the spec defines an inner shadow's shape as the padding box shrunk by
// the spread, so a positive spread makes the lit interior region smaller and
// therefore the shadow itself thicker. Writing this as one signed inflate with
// the inset case negated (rather than as two branches) is what keeps the two
// paths sharing every step that follows.
func shadowShape(s *layout.ShadowItem) (x0, y0, x1, y1 float64, ok bool) {
	spread := s.Spread
	if s.Inset {
		spread = -spread
	}
	x0 = s.XPt + s.OffsetX - spread
	y0 = s.YPt + s.OffsetY - spread
	x1 = s.XPt + s.WPt + s.OffsetX + spread
	y1 = s.YPt + s.HPt + s.OffsetY + spread
	return x0, y0, x1, y1, x1 > x0 && y1 > y0
}

// shadowOutline appends the shadow's SHAPE — as opposed to the region it fills
// — to p, mapped through mat.
//
// BORDER-RADIUS INTEGRATION POINT. This is the ONLY place the shadow's shape is
// expressed as a path, and today it is a plain rectangle because this branch
// has no corner radii to honour. When border-radius lands, this function grows
// the four radii as parameters and emits the rounded outline instead; every
// caller (the outer ring, the inset ring, and the offscreen rasterization of
// the blurred case) picks the new shape up unchanged, because none of them
// looks at the geometry — they only ask for the outline and for its bounds.
//
// The radii a rounded shadow needs are NOT the box's own: CSS Backgrounds 3
// §6 says a spread of r adjusts each corner radius by r as well (growing it for
// an outer shadow, shrinking it for an inset one, floored at zero), so the
// caller must adjust them alongside the rectangle inflate in shadowShape.
func shadowOutline(p *render.Path, mat render.Matrix, x0, y0, x1, y1 float64) {
	moveTo(p, mat, x0, y0)
	lineTo(p, mat, x1, y0)
	lineTo(p, mat, x1, y1)
	lineTo(p, mat, x0, y1)
	p.Close()
}

// shadowRingPath builds the region one shadow actually fills, as a single
// even-odd path of two nested subpaths.
//
// OUTER: the outer subpath is the shadow's shape and the inner one is the box's
// own border box, so the fill is the shape MINUS the box. Cutting the box out
// matters because a box with a transparent (or partly transparent) background
// would otherwise show the shadow through itself — CSS explicitly clips an
// outer shadow to the outside of the border box, and a browser renders
// `background:transparent; box-shadow:0 0 20px black` as a ring, not a filled
// blob.
//
// INSET: the outer subpath is the PADDING box (the shadow box for an inset
// shadow, per layout.ShadowItem) and the inner one is the offset/spread shape,
// so the fill is the padding box MINUS the lit interior — the ring hugging the
// inside edges. This is why an inset shadow is a different code path and not a
// sign flip: its fill region is bounded by a rectangle that does not move with
// the offset, so an inset shadow can never escape the box.
//
// ok is false when the region is empty: an outer shadow whose shape has
// collapsed under a negative spread, or an inset shadow whose interior has
// grown to cover the whole padding box (a zero spread and a zero offset, which
// paints nothing).
func shadowRingPath(s *layout.ShadowItem, mat render.Matrix) (p *render.Path, ok bool) {
	x0, y0, x1, y1, shapeOK := shadowShape(s)
	p = &render.Path{}
	if s.Inset {
		if !shapeOK {
			// The interior has collapsed: the shadow fills the whole padding box.
			shadowOutline(p, mat, s.XPt, s.YPt, s.XPt+s.WPt, s.YPt+s.HPt)
			return p, true
		}
		// Clamp the interior to the padding box before subtracting it. An
		// interior that sticks out (a large offset) would otherwise contribute
		// winding OUTSIDE the padding box, and under the even-odd rule that
		// turns the escaping part into a HOLE punched in the surrounding page
		// rather than into nothing — a shadow that erases its neighbours.
		x0, y0 = math.Max(x0, s.XPt), math.Max(y0, s.YPt)
		x1, y1 = math.Min(x1, s.XPt+s.WPt), math.Min(y1, s.YPt+s.HPt)
		shadowOutline(p, mat, s.XPt, s.YPt, s.XPt+s.WPt, s.YPt+s.HPt)
		if x1 > x0 && y1 > y0 {
			shadowOutline(p, mat, x0, y0, x1, y1)
		}
		return p, true
	}
	if !shapeOK {
		return nil, false // shrunk away by a negative spread
	}
	shadowOutline(p, mat, x0, y0, x1, y1)
	// The box's own border box, cut out. It is NOT clamped the way the inset
	// interior is: the box always lies inside its own shadow's bounding region
	// only when the offset is small, and when it does NOT (a large offset moves
	// the shape clear of the box) the two subpaths simply do not overlap, which
	// even-odd already renders as "fill the shape, and the box contributes a
	// second disjoint region". That second region would be wrong — so it is
	// emitted only when it actually intersects the shape.
	bx0, by0 := math.Max(s.XPt, x0), math.Max(s.YPt, y0)
	bx1, by1 := math.Min(s.XPt+s.WPt, x1), math.Min(s.YPt+s.HPt, y1)
	if bx1 > bx0 && by1 > by0 {
		shadowOutline(p, mat, bx0, by0, bx1, by1)
	}
	return p, true
}

// fillShadowRing paints the shadow's region as a flat even-odd fill — the
// blur==0 path, and the degradation target when a blurred shadow cannot get an
// offscreen surface.
func fillShadowRing(dev render.Device, s *layout.ShadowItem, mat render.Matrix) {
	p, ok := shadowRingPath(s, mat)
	if !ok {
		return
	}
	dev.Fill(p, render.FillPaint{Color: s.Color, Rule: render.EvenOdd})
}

// paintBlurredShadow renders the shadow's shape into an offscreen surface,
// blurs it, and composites the result back, clipped so the shadow keeps its
// outer/inset containment. It reports false when the offscreen path is
// unavailable and the caller must degrade to a hard edge.
//
// The clip, not the shape, is what enforces containment after a blur: a blurred
// silhouette necessarily bleeds across the box's own edge, and CSS requires an
// outer shadow to be invisible under the border box and an inset one to be
// invisible outside the padding box. Blurring a RING and compositing it
// unclipped would leave a soft halo on the wrong side of that edge, which reads
// as a glow the author did not write.
func paintBlurredShadow(dev render.Device, s *layout.ShadowItem, mat render.Matrix) bool {
	devW, devH := dev.Size()
	if devW <= 0 || devH <= 0 {
		return false
	}
	scale := mat.ScaleFactor()
	if !(scale > 0) || math.IsNaN(scale) || math.IsInf(scale, 0) {
		return false
	}
	sigma := layout.ShadowSigma(s.Blur) * scale
	if !(sigma > 0) {
		return false
	}

	surf, ok := shadowSurface(s, mat, sigma, devW, devH)
	if !ok {
		return false
	}

	// Rasterize the SILHOUETTE — the solid shape, not the ring — because that
	// is what a blur is defined over. The ring's inner edge is a clip boundary,
	// and blurring a clip boundary would soften an edge the spec keeps sharp.
	shifted := mat.Mul(render.Translate(float64(-surf.origin.X), float64(-surf.origin.Y)))
	silhouette := dev.RenderOffscreen(surf.size, func(scratch render.Device) {
		x0, y0, x1, y1, shapeOK := shadowShape(s)
		if !shapeOK {
			return
		}
		p := &render.Path{}
		shadowOutline(p, shifted, x0, y0, x1, y1)
		scratch.Fill(p, render.FillPaint{Color: s.Color, Rule: render.NonZero})
	})
	if silhouette == nil {
		return false // no offscreen surface on this backend (a vector writer)
	}

	out := blurShadowSurface(silhouette, sigma, s.Inset)
	if out == nil {
		return false
	}
	b := out.Bounds()
	if b.Empty() {
		return false
	}

	dev.Save()
	shadowClip(dev, s, mat)
	// Place the blurred surface. The Y scale is NEGATED and the anchor taken at
	// the rect's BOTTOM edge because DrawImage maps the unit square in PDF IMAGE
	// space, where v runs Y-UP with the image's TOP row at v=1 — the identical
	// recipe paintFilterBracket uses, and getting it wrong lands the shadow
	// vertically mirrored, which on a symmetric rectangle looks merely offset.
	place := render.Scale(float64(b.Dx()), -float64(b.Dy())).
		Mul(render.Translate(
			float64(surf.origin.X+b.Min.X),
			float64(surf.origin.Y+b.Max.Y),
		))
	dev.DrawImage(out, place, 1, "")
	dev.Restore()
	return true
}

// blurShadowSurface blurs the rasterized silhouette, returning the pixels to
// composite.
//
// For an INSET shadow the blurred silhouette is INVERTED in alpha: an inner
// shadow's ink is where the interior is NOT, so the shadow's coverage at a
// pixel is 1 minus the interior's. Building it this way rather than rasterizing
// the ring and blurring that is what gives an inset shadow its correct soft
// gradient running inward from the padding edge — blurring the ring instead
// would soften BOTH of its edges and leave a bright seam at the padding box.
//
// The blur itself is pkg/svg/filter's feGaussianBlur, the spec's three
// successive box blurs. It is deliberately not a second implementation: this
// repo has exactly one blur, shared by SVG filters, the CSS `filter` shorthand,
// and now box-shadow, so a fix to any of them is a fix to all three.
func blurShadowSurface(silhouette *image.RGBA, sigma float64, inset bool) *image.RGBA {
	region := silhouette.Bounds()
	if region.Empty() {
		return nil
	}
	// The silhouette is a SOLID SHAPE in one uniform colour (the caller fills it
	// with exactly one Fill in s.Color), so its only varying channel is coverage.
	// Blur that channel alone and re-apply the colour afterwards: the RGB a full
	// RGBA blur would compute is the constant it started with, and the two
	// RGBA<->float32 conversions around it — measured as MORE expensive than the
	// blur itself — disappear with it.
	//
	// No colour space conversion is involved for the same reason, which also
	// settles the trap the RGBA path had to be careful about: a box-shadow is a
	// CSS effect and CSS composites in sRGB, and blurring coverage rather than
	// colour is sRGB-neutral, so there is no linearRGB default to opt out of.
	w, h := region.Dx(), region.Dy()
	if w <= 0 || h <= 0 {
		return nil
	}
	cov := make([]float32, w*h)
	var cr, cg, cb uint8
	var peak uint8
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := silhouette.RGBAAt(region.Min.X+x, region.Min.Y+y)
			cov[y*w+x] = float32(c.A) / 255
			if c.A > peak {
				// Straight (un-premultiplied) colour: image.RGBA is
				// premultiplied, and the shadow colour is what the silhouette
				// carries at full coverage.
				cr, cg, cb, peak = c.R, c.G, c.B, c.A
			}
		}
	}
	if peak == 0 {
		return nil // nothing was painted; no shadow to composite
	}
	cr, cg, cb = unpremul8(cr, peak), unpremul8(cg, peak), unpremul8(cb, peak)

	cov = svgfilter.BlurAlpha(cov, w, h, sigma, sigma)
	if cov == nil {
		return nil
	}

	peakF := float32(peak) / 255
	out := image.NewRGBA(region)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			a := cov[y*w+x]
			if inset {
				// An inner shadow's ink is where the interior is NOT: coverage
				// becomes (peak - coverage).
				a = peakF - a
			}
			if a <= 0 {
				continue
			}
			if a > 1 {
				a = 1
			}
			a8 := uint8(a*255 + 0.5)
			if a8 == 0 {
				continue
			}
			// image.RGBA is premultiplied. Premultiply in FLOAT and round once,
			// rather than rounding the alpha first and scaling the 8-bit colour
			// by it: quantizing twice loses up to a level per channel on every
			// soft pixel, which on a wide gradient is a visible extra step.
			out.SetRGBA(region.Min.X+x, region.Min.Y+y, color.RGBA{
				R: uint8(float32(cr)*a + 0.5),
				G: uint8(float32(cg)*a + 0.5),
				B: uint8(float32(cb)*a + 0.5),
				A: a8,
			})
		}
	}
	return out
}

// unpremul8 recovers a straight colour channel from a premultiplied one.
func unpremul8(v, a uint8) uint8 {
	if a == 0 {
		return 0
	}
	if a == 255 {
		return v
	}
	return uint8(min(uint32(v)*255/uint32(a), 255))
}

// shadowClip intersects the device clip with the region the shadow is allowed
// to paint in: OUTSIDE the border box for an outer shadow, INSIDE the padding
// box for an inset one.
//
// The outer case is expressed as an even-odd clip of the page rectangle minus
// the box, because render.Device.PushClip only INTERSECTS — there is no
// subtractive clip operator — so the "everything except the box" region has to
// be built as one path.
func shadowClip(dev render.Device, s *layout.ShadowItem, mat render.Matrix) {
	if s.Inset {
		clipRect(dev, mat, s.XPt, s.YPt, s.XPt+s.WPt, s.YPt+s.HPt)
		return
	}
	// The outer bound only has to enclose everything the shadow could paint;
	// the blurred surface is already bounded by shadowSurface, so a rectangle
	// grown generously around both the box and the shadow suffices and needs no
	// device-space reasoning.
	x0, y0, x1, y1, ok := shadowShape(s)
	if !ok {
		return
	}
	reach := 3*s.Blur + 1
	ox0 := math.Min(x0, s.XPt) - reach
	oy0 := math.Min(y0, s.YPt) - reach
	ox1 := math.Max(x1, s.XPt+s.WPt) + reach
	oy1 := math.Max(y1, s.YPt+s.HPt) + reach

	p := &render.Path{}
	shadowOutline(p, mat, ox0, oy0, ox1, oy1)
	shadowOutline(p, mat, s.XPt, s.YPt, s.XPt+s.WPt, s.YPt+s.HPt)
	dev.PushClip(p, render.EvenOdd)
}

// shadowSurface computes the offscreen geometry a blurred shadow rasterizes
// into: the shape's device-space extent grown by the blur's reach, clamped to
// the device, and shifted to the origin (RenderOffscreen always allocates from
// the origin — the same contract, and the same shift, cssFilterSurface
// documents at length).
//
// ok is false for a degenerate or fully off-device region, or one over
// maxShadowPixels; the caller then paints the shadow hard-edged.
func shadowSurface(s *layout.ShadowItem, mat matrixLike, sigma float64, devW, devH int) (fs filterSurface, ok bool) {
	x0, y0, x1, y1, shapeOK := shadowShape(s)
	if !shapeOK {
		return fs, false
	}
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, c := range [4][2]float64{{x0, y0}, {x1, y0}, {x0, y1}, {x1, y1}} {
		x, y := mat.Apply(c[0], c[1])
		if math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) {
			return fs, false
		}
		minX, maxX = math.Min(minX, x), math.Max(maxX, x)
		minY, maxY = math.Min(minY, y), math.Max(maxY, y)
	}
	// An INSET shadow's surface must also cover the whole padding box: its ink
	// is the COMPLEMENT of the interior, so every pixel of the padding box can
	// carry shadow even when the interior shape is small and far from an edge.
	if s.Inset {
		for _, c := range [4][2]float64{
			{s.XPt, s.YPt}, {s.XPt + s.WPt, s.YPt},
			{s.XPt, s.YPt + s.HPt}, {s.XPt + s.WPt, s.YPt + s.HPt},
		} {
			x, y := mat.Apply(c[0], c[1])
			if math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) {
				return fs, false
			}
			minX, maxX = math.Min(minX, x), math.Max(maxX, x)
			minY, maxY = math.Min(minY, y), math.Max(maxY, y)
		}
	}
	grow := shadowBlurMargin * sigma
	minX, minY = minX-grow, minY-grow
	maxX, maxY = maxX+grow, maxY+grow

	// Clamp in FLOAT, before the int conversion: a coordinate beyond int range
	// converts to an implementation-defined value (the int64 minimum on amd64,
	// regardless of sign), which would turn an off-to-the-right shape into one
	// starting far to the LEFT and yield a plausible-looking wrong rectangle
	// instead of an empty one. See cssFilterSurface, which carries the same guard.
	dw, dh := float64(devW), float64(devH)
	minX, minY = math.Max(math.Floor(minX), 0), math.Max(math.Floor(minY), 0)
	maxX, maxY = math.Min(math.Ceil(maxX), dw), math.Min(math.Ceil(maxY), dh)
	if !(maxX > minX) || !(maxY > minY) {
		return fs, false
	}
	rect := image.Rect(int(minX), int(minY), int(maxX), int(maxY)).
		Intersect(image.Rect(0, 0, devW, devH))
	if rect.Empty() || rect.Dx()*rect.Dy() > maxShadowPixels {
		return fs, false
	}
	fs.origin = rect.Min
	fs.rect = rect.Sub(rect.Min)
	fs.size = image.Point{X: fs.rect.Dx(), Y: fs.rect.Dy()}
	return fs, true
}
