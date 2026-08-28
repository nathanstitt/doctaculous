package paint

import (
	"image"
	"image/color"
	"math"

	"github.com/nathanstitt/doctaculous/pkg/filtereffects"
	"github.com/nathanstitt/doctaculous/pkg/layout"
	svgfilter "github.com/nathanstitt/doctaculous/pkg/svg/filter"
)

// maxCSSFilterPixels bounds how many pixels one CSS `filter` may allocate for
// its offscreen surface and per-primitive buffers.
//
// Unlike SVG's filter region — which an author sizes directly through
// filterUnits/x/y/width/height — a CSS filter region IS the element's border
// box, so the size is bounded by layout rather than by a raw attribute. That
// makes an accidental blow-up far less likely but not impossible: a
// `width:1e7px` box is untrusted input just like a `width="1e9"` filter
// region, and every primitive in the chain allocates its own float32 RGBA
// buffer (16 bytes per pixel) over the surface.
//
// The cap is meaningful only because filterSurface INTERSECTS the region with
// the device's own bounds and then SHIFTS the result's origin to (0,0) before
// allocating: render.Device.RenderOffscreen always allocates from the origin,
// so without the shift a small box far from it would allocate up to its far
// corner while this check measured the box's own area. The two must stay in
// agreement — if the origin shift is ever removed, this stops bounding
// anything.
//
// 4M pixels matches pkg/svg/draw's maxFilterPixels for the same reason: it is
// roughly 64 MB per float32 buffer.
//
// It is NOT above every legitimate page region, and an earlier version of this
// comment wrongly claimed it was: a 300 DPI A4 page is ~8.7M pixels, i.e. more
// than twice this cap. A full-page filter at print resolution therefore
// degrades to unfiltered — measured: the same document renders filtered at 72
// and 150 DPI and unfiltered at 300.
//
// That degradation is currently SILENT, because pkg/layout/paint has no logger
// to report it through (PaintPage takes only a Device, a Page, and a Matrix).
// The SVG side logs the equivalent caps (pkg/svg/draw/filter.go's
// logFilterRegionCapOnce / logFilterNestingCapOnce) because a Renderer carries
// a Logf. Threading one into the paint path is a public API change and is
// tracked separately rather than smuggled in here; until then FEATURES.md must
// say plainly that these two paths do not log, and it does.
const maxCSSFilterPixels = 4 << 20

// filterMargin is how far, in multiples of a blur's standard deviation, the
// offscreen surface is grown beyond the filtered box's border box.
//
// A CSS filter has no filter region of its own — but blur() and drop-shadow()
// both move pixels OUTSIDE the border box, and CSS explicitly does not clip
// them there (unlike SVG's filter region, which does clip). Rendering into a
// surface exactly the size of the border box would cut a blur off dead at the
// box edge, which reads as a "half-blurred" box rather than as a missing
// margin. Three standard deviations captures ~99.7% of a Gaussian's mass,
// which is the same margin browsers use, and drop-shadow additionally needs
// its own offset added on top.
const filterMargin = 3

// filterSurface is the offscreen geometry one CSS filter bracket runs over:
// where the surface sits in device space, and how large it is.
type filterSurface struct {
	// rect is the surface's device-space extent, ALREADY intersected with the
	// device bounds — the region the chain's buffers cover once shifted to the
	// origin.
	rect image.Rectangle
	// size is the offscreen pixel surface RenderOffscreen allocates. It equals
	// rect's dimensions, since rect is shifted to the origin before use.
	size image.Point
	// origin is rect's original device-space top-left, i.e. the translation
	// that must be applied when painting into the surface (negated) and when
	// placing the result back (positive).
	origin image.Point
}

// cssFilterSurface computes the offscreen surface for a filter bracket whose
// border box maps through mat to device space, on a device of devW x devH
// pixels.
//
// ok=false means the filter cannot run at all — a degenerate or fully
// off-device region, or one exceeding maxCSSFilterPixels — and the caller must
// degrade to painting the bracketed content unfiltered.
//
// scale is the device pixels per page point that the chain's lengths
// (blur's stdDeviation, drop-shadow's offsets) must be multiplied by, taken
// from mat's own scale factor so a filter rasterizes at the resolution the
// page is actually drawn at.
func cssFilterSurface(fi *layout.FilterItem, inner []layout.Item, mat matrixLike, devW, devH int) (fs filterSurface, scale float64, ok bool) {
	if devW <= 0 || devH <= 0 {
		return fs, 0, false
	}
	scale = mat.ScaleFactor()
	if scale <= 0 || math.IsNaN(scale) || math.IsInf(scale, 0) {
		return fs, 0, false
	}
	// A degenerate border box (zero or NEGATIVE extent) has no region to filter.
	// Rejecting it here rather than letting the hull below absorb it is what keeps
	// the degradation honest: a negative width still produces a non-empty hull
	// (the corners simply swap), so the chain would run over a surface that does
	// not contain the content and composite back a near-blank — a silent content
	// loss dressed up as a filter.
	if !(fi.WPt > 0) || !(fi.HPt > 0) {
		return fs, 0, false
	}

	// The surface covers the border box UNIONED with what the bracketed items
	// actually paint. The border box alone is not enough: CSS does not clip a
	// filter's input, so content that overflows its box (an oversized line, a
	// negative-margin child, a positioned descendant kept inside the bracket by
	// the box's stacking context) is still filtered by a browser. Rendering into
	// a border-box-sized surface would CROP that content away — content loss
	// dressed up as a filter, which is exactly the failure mode a degradation
	// must never take.
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	addRect := func(x0, y0, x1, y1 float64) bool {
		for _, c := range [4][2]float64{{x0, y0}, {x1, y0}, {x0, y1}, {x1, y1}} {
			x, y := mat.Apply(c[0], c[1])
			if math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) {
				return false
			}
			minX, maxX = math.Min(minX, x), math.Max(maxX, x)
			minY, maxY = math.Min(minY, y), math.Max(maxY, y)
		}
		return true
	}
	// Taking all four corners (rather than mapping the top-left and adding a
	// scaled size) keeps this correct under any matrix the page transform carries.
	if !addRect(fi.XPt, fi.YPt, fi.XPt+fi.WPt, fi.YPt+fi.HPt) {
		return fs, 0, false
	}
	addItemExtents(inner, addRect)

	// Grow by the chain's own spatial reach so a blur or shadow is not cut off
	// at the border box (see filterMargin).
	growX, growY := chainMargin(fi.Funcs, scale)
	minX, minY = minX-growX, minY-growY
	maxX, maxY = maxX+growX, maxY+growY

	// Clamp to the DEVICE's own extent BEFORE the float→int conversion, not only
	// after. A coordinate beyond int range (a `width:1e300px` box, or a huge blur
	// margin on top of a large one) converts to an implementation-defined int —
	// on amd64 that is the INT64 MINIMUM regardless of sign, which turns an
	// off-to-the-right box into one starting far to the LEFT and makes the
	// intersect below produce a plausible-looking wrong rectangle instead of an
	// empty one. Clamping in float, where the arithmetic is total, removes the
	// question.
	dw, dh := float64(devW), float64(devH)
	minX, minY = math.Max(math.Floor(minX), 0), math.Max(math.Floor(minY), 0)
	maxX, maxY = math.Min(math.Ceil(maxX), dw), math.Min(math.Ceil(maxY), dh)
	if !(maxX > minX) || !(maxY > minY) {
		return fs, 0, false
	}
	// Confine to what can actually land on the device, so an enormous box costs
	// only the pixels that could ever be seen. This is also what makes the pixel
	// cap below bound the ALLOCATION rather than merely the nominal region.
	rect := image.Rect(int(minX), int(minY), int(maxX), int(maxY)).
		Intersect(image.Rect(0, 0, devW, devH))
	if rect.Empty() {
		return fs, 0, false
	}
	if rect.Dx()*rect.Dy() > maxCSSFilterPixels {
		return fs, 0, false
	}

	fs.origin = rect.Min
	fs.rect = rect.Sub(rect.Min) // shifted to the origin; see maxCSSFilterPixels
	fs.size = image.Point{X: fs.rect.Dx(), Y: fs.rect.Dy()}
	return fs, scale, true
}

// matrixLike is the slice of render.Matrix cssFilterSurface needs, named so the
// geometry above can be unit-tested without a device.
type matrixLike interface {
	Apply(x, y float64) (float64, float64)
	ScaleFactor() float64
}

// addItemExtents reports each bracketed item's page-space extent to add, so the
// filter surface covers content that overflows the filtered box (see
// cssFilterSurface).
//
// A glyph contributes a conservative EM BOX around its pen origin rather than its
// real outline extent: measuring every outline would mean transforming every
// point of every glyph on the page just to size a buffer, and an em box on each
// side comfortably contains any glyph's ink (ascender, descender, and side
// bearings all sit well inside one em of the pen). Over-covering costs a few
// pixels of surface; under-covering would crop ink.
//
// The walk is FLAT (it does not recurse into a nested bracket's own items,
// which the caller passes in the same slice anyway) and touches each item once,
// so it is O(items) — the same bound the paint pass itself carries.
func addItemExtents(items []layout.Item, add func(x0, y0, x1, y1 float64) bool) {
	for i := range items {
		it := &items[i]
		switch it.Kind {
		case layout.GlyphKind:
			g := &it.Glyph
			s := g.SizePt
			if s <= 0 {
				continue
			}
			add(g.XPt-s, g.YPt-s, g.XPt+s, g.YPt+s)
		case layout.RuleKind, layout.BackgroundKind, layout.ClipPushKind:
			r := &it.Rule
			add(r.XPt, r.YPt, r.XPt+r.WPt, r.YPt+r.HPt)
		case layout.BorderKind:
			b := &it.Border
			add(b.XPt, b.YPt, b.XPt+b.WPt, b.YPt+b.HPt)
		case layout.ImageKind:
			m := &it.Image
			add(m.XPt, m.YPt, m.XPt+m.WPt, m.YPt+m.HPt)
		case layout.BackgroundImageKind:
			// A background image is confined to its own clip box, which is the
			// only part of it that can paint.
			b := &it.BgImage
			add(b.ClipX, b.ClipY, b.ClipX+b.ClipW, b.ClipY+b.ClipH)
		case layout.VectorKind:
			// A vector scene is clipped to its viewport box (see paintVector).
			v := &it.Vector
			add(v.XPt, v.YPt, v.XPt+v.WPt, v.YPt+v.HPt)
		case layout.FilterPushKind:
			f := &it.Filter
			add(f.XPt, f.YPt, f.XPt+f.WPt, f.YPt+f.HPt)
		case layout.ShadowKind:
			// A box-shadow reaches outside its box by its offset, its spread and
			// its blur, and an INSET one never leaves the padding box at all.
			// Covering the union of the shadow box and the shape's own blurred
			// extent keeps a filtered box's shadow from being cropped at the
			// surface edge, which would read as a shadow that fades out early.
			sh := &it.Shadow
			add(sh.XPt, sh.YPt, sh.XPt+sh.WPt, sh.YPt+sh.HPt)
			if !sh.Inset {
				reach := sh.Spread + filterMargin*layout.ShadowSigma(sh.Blur)
				add(sh.XPt+sh.OffsetX-reach, sh.YPt+sh.OffsetY-reach,
					sh.XPt+sh.WPt+sh.OffsetX+reach, sh.YPt+sh.HPt+sh.OffsetY+reach)
			}
		}
	}
}

// chainMargin reports how far, in DEVICE pixels, the chain's output can reach
// beyond its input on each axis.
//
// Only blur() and drop-shadow() move pixels (layout.FilterItem.Spatial names
// exactly those two); every colour adjustment leaves each pixel where it is, so
// an unspatial chain needs no margin at all and costs no extra surface.
//
// A drop-shadow contributes its OFFSET plus its own blur reach, and the offset
// may be negative, so the absolute value is taken per axis. The margins of
// successive functions ADD, because each consumes the previous one's output.
func chainMargin(funcs []filtereffects.Function, scale float64) (x, y float64) {
	for _, f := range funcs {
		switch f.Kind {
		case filtereffects.FuncBlur:
			r := filterMargin * f.StdDeviation * scale
			x, y = x+r, y+r
		case filtereffects.FuncDropShadow:
			r := filterMargin * f.StdDeviation * scale
			x += r + math.Abs(f.Dx)*scale
			y += r + math.Abs(f.Dy)*scale
		}
	}
	if math.IsNaN(x) || math.IsInf(x, 0) {
		x = 0
	}
	if math.IsNaN(y) || math.IsInf(y, 0) {
		y = 0
	}
	return x, y
}

// applyCSSFilterChain runs a CSS `filter` function list over src, returning the
// filtered pixels, or nil when the chain produced nothing.
//
// src is the bracketed content already rasterized into an isolated,
// fully-transparent surface (render.Device.RenderOffscreen's contract), with
// its own bounds as the region every primitive covers. scale converts the
// chain's page-point lengths into the device pixels src is measured in.
//
// Every function is lowered to the Filter Effects specification's own
// equivalent primitive and run through pkg/svg/filter, which is where the blur
// premultiplication, the colour-matrix arithmetic, and the colour-space
// handling live. Nothing here computes pixels itself: the mapping is the whole
// contribution, so the two callers of that pixel math (SVG and CSS) cannot
// drift apart.
//
// The CSS filter functions are defined to operate in sRGB, NOT the linearRGB
// SVG's own primitives default to. Getting that wrong makes every blur() and
// drop-shadow() visibly lighter than a browser's — the same colour-space trap
// the <filter> path carries in the opposite direction.
func applyCSSFilterChain(src *image.RGBA, funcs []filtereffects.Function, colors []color.RGBA, scale float64) *image.RGBA {
	if src == nil || len(funcs) == 0 {
		return src
	}
	region := src.Bounds()
	if region.Empty() {
		return src
	}
	buf := svgfilter.FromRGBA(src, region, svgfilter.SRGB)
	for i, f := range funcs {
		shadow := color.RGBA{A: 255}
		if i < len(colors) {
			shadow = colors[i]
		}
		buf = applyCSSFilterFunction(buf, f, shadow, region, scale)
	}
	if buf == nil {
		return nil
	}
	return buf.ToRGBA()
}

// applyCSSFilterFunction runs ONE CSS filter function over in, returning the
// result. shadow is the already-resolved drop-shadow colour (unused by every
// other function); region is the surface each primitive's output is clipped to.
//
// Each mapping below is the Filter Effects specification's own lowering:
//
//	grayscale(a)     feColorMatrix type="saturate" values="1-a"
//	sepia(a)         feColorMatrix with the spec's fixed sepia matrix, lerped
//	                 toward the identity by 1-a
//	saturate(a)      feColorMatrix type="saturate" values="a"
//	hue-rotate(deg)  feColorMatrix type="hueRotate" values="deg"
//	invert(a)        per-channel v' = a·(1-v) + (1-a)·v
//	brightness(a)    per-channel v' = a·v
//	contrast(a)      per-channel v' = a·v + (0.5 - a/2)
//	opacity(a)       alpha multiply
//	blur(len)        feGaussianBlur stdDeviation=len
//	drop-shadow(...) blur → offset → flood → composite(in) → merge
//
// invert/brightness/contrast/opacity are the four the spec expresses as an
// feComponentTransfer with LINEAR transfer functions. feComponentTransfer is a
// primitive the SVG series deliberately deferred (it degrades with a log
// there), so rather than reviving it, each is written here as the equivalent
// affine per-channel map and evaluated through feColorMatrix — which computes
// exactly `slope·v + intercept` per channel and is already corpus-tested. A
// linear transfer function IS an affine map, so this is an exact
// reformulation, not an approximation.
//
// It is the same lowering pkg/svg/filterfunc.go uses, and the two agree today
// across every function and argument (verified by rendering an HTML box and an
// inline <svg> rect side by side: byte-identical). But they agree by being
// KEPT IN STEP, not by construction — the matrix helpers below are duplicated
// from that file rather than shared, so nothing structurally prevents drift.
// An earlier version of this comment claimed the two "cannot disagree", which
// overstated it. Moving the five helpers to a package both can import would
// make the claim true; until then, a change to either side must be mirrored.
func applyCSSFilterFunction(in *svgfilter.Buffer, f filtereffects.Function, shadow color.RGBA, region image.Rectangle, scale float64) *svgfilter.Buffer {
	switch f.Kind {
	case filtereffects.FuncURL:
		// An HTML box cannot resolve an SVG <filter> element; such an entry is
		// dropped at parse time (see pkg/layout/css's filterChain), so reaching
		// here means a hand-built chain. Treat it as the identity rather than
		// blanking the content.
		return in

	case filtereffects.FuncBlur:
		if f.StdDeviation <= 0 {
			return in // blur(0) is the identity
		}
		sd := f.StdDeviation * scale
		return svgfilter.GaussianBlur(in, sd, sd, region)

	case filtereffects.FuncDropShadow:
		return dropShadowChain(in, f, shadow, region, scale)

	case filtereffects.FuncHueRotate:
		return svgfilter.ApplyColorMatrix(in, svgfilter.HueRotateMatrix(f.Angle), region)

	case filtereffects.FuncSaturate:
		return svgfilter.ApplyColorMatrix(in, svgfilter.SaturateMatrix(float32(f.Amount)), region)

	case filtereffects.FuncGrayscale:
		// grayscale(a) is saturate(1-a). The amount is clamped to 1 HERE (unlike
		// saturate's own coefficient) because grayscale(2) must not over-rotate
		// past greyscale into inverted saturation: the spec defines the function
		// only on [0,1] and browsers render grayscale(2) identically to
		// grayscale(1).
		a := math.Min(f.Amount, 1)
		return svgfilter.ApplyColorMatrix(in, svgfilter.SaturateMatrix(float32(1-a)), region)

	case filtereffects.FuncSepia:
		return svgfilter.ApplyColorMatrix(in, sepiaMatrix(math.Min(f.Amount, 1)), region)

	case filtereffects.FuncInvert:
		return svgfilter.ApplyColorMatrix(in, invertMatrix(math.Min(f.Amount, 1)), region)

	case filtereffects.FuncBrightness:
		return svgfilter.ApplyColorMatrix(in, brightnessMatrix(f.Amount), region)

	case filtereffects.FuncContrast:
		return svgfilter.ApplyColorMatrix(in, contrastMatrix(f.Amount), region)

	case filtereffects.FuncOpacity:
		return svgfilter.ApplyColorMatrix(in, opacityMatrix(math.Min(f.Amount, 1)), region)
	}
	return in
}

// dropShadowChain lowers drop-shadow(<color>? dx dy blur?) into the same
// five-step chain <feDropShadow> expands to, so the two spellings of the same
// effect cannot drift apart:
//
//	blur      = feGaussianBlur(in, stdDeviation)
//	offset    = feOffset(blur, dx, dy)
//	flood     = feFlood(shadow-color)
//	composite = feComposite(flood, offset, operator="in")   → the tinted shadow
//	merge     = feMerge(composite, in)                      → shadow BEHIND
//
// The merge order is load-bearing: feMergeNode order is painting order, so the
// composite (the shadow) must be the FIRST node and the source the second, or
// the shadow paints over the element it belongs to.
func dropShadowChain(in *svgfilter.Buffer, f filtereffects.Function, shadow color.RGBA, region image.Rectangle, scale float64) *svgfilter.Buffer {
	sd := f.StdDeviation * scale
	blurred := in
	if sd > 0 {
		blurred = svgfilter.GaussianBlur(in, sd, sd, region)
	}
	offset := svgfilter.Offset(blurred, f.Dx*scale, f.Dy*scale, region)
	// flood-color is authored in sRGB and the chain runs in sRGB, so the
	// channels pass through unconverted (see pkg/svg/draw's floodChannels for
	// the linearRGB case this deliberately is not).
	flood := svgfilter.Flood(region,
		float32(shadow.R)/255, float32(shadow.G)/255, float32(shadow.B)/255, float32(shadow.A)/255,
		svgfilter.SRGB)
	tinted := svgfilter.Composite(flood, offset, svgfilter.CompositeIn, 0, 0, 0, 0, region)
	return svgfilter.Merge([]*svgfilter.Buffer{tinted, in}, region, svgfilter.SRGB)
}

// brightnessMatrix returns the matrix for brightness(a): a linear scale of every
// colour channel, leaving alpha alone.
//
// The spec defines it as an feComponentTransfer with type="linear" slope="a"
// intercept="0" on R, G and B; a diagonal colour matrix is exactly that, which
// is what lets this ship without an feComponentTransfer.
func brightnessMatrix(a float64) svgfilter.ColorMatrix {
	f := float32(a)
	return svgfilter.ColorMatrix{
		f, 0, 0, 0, 0,
		0, f, 0, 0, 0,
		0, 0, f, 0, 0,
		0, 0, 0, 1, 0,
	}
}

// contrastMatrix returns the matrix for contrast(a): slope a about the 0.5
// midpoint, i.e. the spec's linear transfer with intercept (0.5 - a/2).
func contrastMatrix(a float64) svgfilter.ColorMatrix {
	f := float32(a)
	c := float32(0.5 - a/2)
	return svgfilter.ColorMatrix{
		f, 0, 0, 0, c,
		0, f, 0, 0, c,
		0, 0, f, 0, c,
		0, 0, 0, 1, 0,
	}
}

// invertMatrix returns the matrix for invert(a), interpolating each channel
// between itself and its complement: v' = a·(1-v) + (1-a)·v = v·(1-2a) + a.
func invertMatrix(a float64) svgfilter.ColorMatrix {
	f := float32(1 - 2*a)
	c := float32(a)
	return svgfilter.ColorMatrix{
		f, 0, 0, 0, c,
		0, f, 0, 0, c,
		0, 0, f, 0, c,
		0, 0, 0, 1, 0,
	}
}

// opacityMatrix returns the matrix for opacity(a): a linear scale of the ALPHA
// channel only, the spec's feComponentTransfer type="linear" on funcA.
func opacityMatrix(a float64) svgfilter.ColorMatrix {
	f := float32(a)
	return svgfilter.ColorMatrix{
		1, 0, 0, 0, 0,
		0, 1, 0, 0, 0,
		0, 0, 1, 0, 0,
		0, 0, 0, f, 0,
	}
}

// sepiaMatrix returns the matrix for sepia(a), interpolating between the
// identity (a=0) and the spec's fixed full-sepia matrix (a=1).
//
// The full-sepia coefficients are the Filter Effects spec's verbatim; they are
// NOT derivable from a saturation or hue formula, which is why they are written
// out rather than computed.
func sepiaMatrix(a float64) svgfilter.ColorMatrix {
	full := [9]float64{
		0.393, 0.769, 0.189,
		0.349, 0.686, 0.168,
		0.272, 0.534, 0.131,
	}
	identity := [9]float64{1, 0, 0, 0, 1, 0, 0, 0, 1}
	var c [9]float32
	for i := range c {
		c[i] = float32(identity[i] + a*(full[i]-identity[i]))
	}
	return svgfilter.ColorMatrix{
		c[0], c[1], c[2], 0, 0,
		c[3], c[4], c[5], 0, 0,
		c[6], c[7], c[8], 0, 0,
		0, 0, 0, 1, 0,
	}
}
