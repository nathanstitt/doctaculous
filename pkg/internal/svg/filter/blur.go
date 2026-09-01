package filter

import (
	"image"
	"math"
)

// MaxBlurStdDeviation bounds one axis's standard deviation, in DEVICE pixels,
// after the element's transform has scaled it.
//
// A box blur pass is O(pixels) regardless of the box width (the running sum
// costs the same per output pixel however wide the window), so the CPU cost of
// a blur is bounded by the region alone — which maxFilterPixels already caps.
// What an unbounded stdDeviation actually buys an attacker is the INTEGER box
// arithmetic: the spec's d = floor(s * 3 * sqrt(2*pi)/4 + 0.5) grows without
// limit, and a d far larger than the region makes every output pixel average a
// window that is almost entirely off-buffer — pure waste, and at extreme values
// (a crafted stdDeviation="1e300", or one scaled up by a large transform) the
// box half-widths overflow int arithmetic.
//
// Clamping instead of rejecting is deliberate and visually free: once the box
// exceeds the region's own extent the result is already a flat average of
// everything in it, so a larger deviation changes nothing a viewer can see.
// The corpus's huge-stdDeviation fixture (stdDeviation="1000" on a 200x200
// viewBox) sits far above this and renders identically either way.
const MaxBlurStdDeviation = 500

// GaussianBlur approximates a Gaussian blur of in with standard deviations
// (sdx, sdy) in DEVICE pixels, clipped to subregion — the feGaussianBlur
// primitive.
//
// It uses the approximation the SVG filter spec PRESCRIBES rather than a true
// Gaussian convolution: three successive box blurs, whose sizes the spec gives
// exactly (see boxSizes). That is not a shortcut taken for speed — it is the
// defined behavior, it is what the reference renderers implement, and a true
// Gaussian would MISS the corpus's committed reference pixels rather than
// matching them more closely.
//
// A non-positive or non-finite deviation on an axis means "no blur on that
// axis" (the identity), per the spec's "a value of zero disables the effect of
// the given filter primitive (i.e., the result is the filter input image)".
// The corpus pins that a NEGATIVE stdDeviation is an error which likewise
// leaves the element unblurred rather than blanking it; the negative case is
// rejected at PARSE time (see pkg/svg's readGaussianBlur), so a negative value
// never reaches here as a radius.
//
// The blur runs on PREMULTIPLIED values even though Buffer stores straight
// alpha. This is the primitive where that distinction is most visible:
// averaging straight color across an edge where alpha falls to zero weights a
// transparent pixel's meaningless color equally with an opaque one, which
// fringes every blurred edge toward whatever color the transparent pixels
// happen to carry (black, in a freshly-allocated buffer — so edges darken).
// Premultiplying, blurring, then un-premultiplying is the correct order and is
// what makes a blur over a semi-transparent edge match the reference.
func GaussianBlur(in *Buffer, sdx, sdy float64, subregion image.Rectangle) *Buffer {
	space := LinearRGB
	if in != nil {
		space = in.Space
	}
	if in == nil {
		return NewBuffer(subregion, space)
	}

	// Bound each axis's deviation by the extent it will actually blur across.
	// See clampToExtent: this is what keeps a huge stdDeviation from averaging
	// the whole region down to nothing.
	work0 := in.Bounds().Union(subregion)
	sdx = clampToExtent(sdx, work0.Dx())
	sdy = clampToExtent(sdy, work0.Dy())

	bx := boxSizes(sdx)
	by := boxSizes(sdy)
	if bx == nil && by == nil {
		// Identity on both axes: the spec's "result is the filter input
		// image". Cropping (rather than returning in) still applies the
		// primitive's own subregion, which a zero-deviation blur does not
		// escape.
		return Crop(in, subregion)
	}

	// Work over the input's own extent UNION the subregion: a blur spreads
	// beyond its input, and clipping to the input first would shave the very
	// falloff the primitive exists to produce. The union is then cropped to
	// subregion at the end.
	work := in.Bounds().Union(subregion)
	if work.Empty() {
		return NewBuffer(subregion, space)
	}

	// Premultiplied working planes, one float32 per channel per pixel.
	w, h := work.Dx(), work.Dy()
	buf := make([]float32, 4*w*h)
	tmp := make([]float32, 4*w*h)
	for y := 0; y < h; y++ {
		row := y * 4 * w
		for x := 0; x < w; x++ {
			r, g, b, a := in.At(work.Min.X+x, work.Min.Y+y)
			i := row + 4*x
			buf[i], buf[i+1], buf[i+2], buf[i+3] = r*a, g*a, b*a, a
		}
	}

	for _, d := range bx {
		boxBlurH(buf, tmp, w, h, d)
		buf, tmp = tmp, buf
	}
	for _, d := range by {
		boxBlurV(buf, tmp, w, h, d)
		buf, tmp = tmp, buf
	}

	out := NewBuffer(subregion, space)
	clip := work.Intersect(subregion)
	for y := clip.Min.Y; y < clip.Max.Y; y++ {
		row := (y - work.Min.Y) * 4 * w
		for x := clip.Min.X; x < clip.Max.X; x++ {
			i := row + 4*(x-work.Min.X)
			a := buf[i+3]
			if a <= 0 {
				continue
			}
			// Back to the straight alpha Buffer stores.
			out.Set(x, y, buf[i]/a, buf[i+1]/a, buf[i+2]/a, a)
		}
	}
	return out
}

// clampToExtent bounds one axis's standard deviation so the resulting box
// never spans more than HALF the extent it is blurring across.
//
// This is a correctness bound as much as a resource one. Past that point the
// three box passes average almost entirely off-buffer — every sample beyond
// the edge reads transparent black — so the result decays toward NOTHING: at
// stdDeviation="1000" on a 200-unit viewBox the element vanished completely,
// while the reference still shows a soft green wash. A blur that erases its
// input is not "more blurred", it is wrong, and no viewer can distinguish
// between deviations past this bound anyway because the window already covers
// everything in range.
//
// Halving the extent is the point at which the centre pixel's window still
// draws mostly from real content. It also caps the work: the box passes are
// O(pixels) whatever d is, but d itself is integer arithmetic that overflows
// on a crafted stdDeviation="1e300" or one a large transform scales up.
//
// The absolute MaxBlurStdDeviation still applies on top, for a degenerate
// extent.
func clampToExtent(s float64, extent int) float64 {
	if !(s > 0) || math.IsNaN(s) {
		return 0
	}
	if s > MaxBlurStdDeviation {
		s = MaxBlurStdDeviation
	}
	if extent <= 0 {
		return 0
	}
	// Invert the spec's d = s·3·sqrt(2π)/4 for the largest d we allow.
	maxS := float64(extent) / 2 * 4 / (3 * math.Sqrt(2*math.Pi))
	if s > maxS {
		return maxS
	}
	return s
}

// boxSizes returns the three box widths the SVG filter spec prescribes for a
// standard deviation of s device pixels, or nil when s produces no blur.
//
// The spec's text, verbatim in structure:
//
//	d = floor(s * 3 * sqrt(2*PI)/4 + 0.5)
//	if d is odd:  three box blurs of size d, centred on the output pixel
//	if d is even: two box blurs of size d (the first centred on the pixel
//	              boundary between the output pixel and the one to its left,
//	              the second on the boundary to its right) and one of size d+1
//	              centred on the output pixel
//
// The odd/even split is not cosmetic. An even-sized box has no centre pixel,
// so centring it on the output pixel would shift the image by half a pixel per
// pass; the spec's alternating-boundary pairing cancels that shift between the
// first two passes, and the odd third pass is what recentres the result. An
// implementation that ignores it produces a blur that is correct in shape but
// visibly TRANSLATED, which reads as a placement bug rather than a blur bug.
//
// The returned sizes are encoded as (size, offset) pairs consumed by
// boxBlurH/boxBlurV; see boxPass.
func boxSizes(s float64) []boxPass {
	if !(s > 0) || math.IsNaN(s) || math.IsInf(s, 0) {
		return nil
	}
	if s > MaxBlurStdDeviation {
		s = MaxBlurStdDeviation
	}
	d := int(math.Floor(s*3*math.Sqrt(2*math.Pi)/4 + 0.5))
	if d < 1 {
		// A deviation small enough to round to a zero-width box: the spec's
		// formula degenerates, and the reference renderers leave such an input
		// unblurred rather than inventing a one-pixel box. tiny-stdDeviation
		// (0.01) lands here.
		return nil
	}
	if d%2 == 1 {
		// Odd d: three identical boxes, each centred on the output pixel.
		half := d / 2
		return []boxPass{{size: d, left: half}, {size: d, left: half}, {size: d, left: half}}
	}
	// Even d: the first two boxes are the same width but offset to OPPOSITE
	// sides by one pixel, so their half-pixel shifts cancel; the third is
	// d+1 (odd) and centred.
	half := d / 2
	return []boxPass{
		{size: d, left: half},
		{size: d, left: half - 1},
		{size: d + 1, left: half},
	}
}

// boxPass is one box-blur pass: a window of `size` pixels extending `left`
// pixels to the left/above the output pixel (and size-1-left to the
// right/below). Carrying the offset explicitly is what expresses the spec's
// even-d boundary-centred pair — see boxSizes.
type boxPass struct {
	size int
	left int
}

// boxBlurH runs one horizontal box pass from src into dst, both premultiplied
// RGBA planes of w x h pixels.
//
// It uses a running sum rather than re-summing the window per pixel, so the
// cost is O(pixels) INDEPENDENT of the box width — which is why a large
// stdDeviation is not itself a CPU attack (the region's pixel count, capped by
// the caller, is the whole bound). Pixels outside the plane count as
// transparent black, matching Buffer.At's documented out-of-range value, so a
// blur at the buffer edge fades out rather than smearing the edge pixel.
func boxBlurH(src, dst []float32, w, h int, p boxPass) {
	if p.size <= 1 {
		copy(dst, src)
		return
	}
	inv := float32(1) / float32(p.size)
	for y := 0; y < h; y++ {
		row := y * 4 * w
		var sr, sg, sb, sa float32
		// Prime the window for x = 0: it spans [-left, size-1-left).
		for k := -p.left; k < p.size-p.left; k++ {
			if k < 0 || k >= w {
				continue
			}
			i := row + 4*k
			sr, sg, sb, sa = sr+src[i], sg+src[i+1], sb+src[i+2], sa+src[i+3]
		}
		for x := 0; x < w; x++ {
			o := row + 4*x
			dst[o], dst[o+1], dst[o+2], dst[o+3] = sr*inv, sg*inv, sb*inv, sa*inv
			// Slide: drop the leftmost sample, add the one entering on the
			// right.
			out := x - p.left
			in := x + p.size - p.left
			if out >= 0 && out < w {
				i := row + 4*out
				sr, sg, sb, sa = sr-src[i], sg-src[i+1], sb-src[i+2], sa-src[i+3]
			}
			if in >= 0 && in < w {
				i := row + 4*in
				sr, sg, sb, sa = sr+src[i], sg+src[i+1], sb+src[i+2], sa+src[i+3]
			}
		}
	}
}

// boxBlurV runs one vertical box pass, the transpose of boxBlurH. It is
// written out rather than expressed as transpose-blur-transpose because the
// two transposes would cost more than the pass itself on a large region.
func boxBlurV(src, dst []float32, w, h int, p boxPass) {
	if p.size <= 1 {
		copy(dst, src)
		return
	}
	inv := float32(1) / float32(p.size)
	stride := 4 * w
	for x := 0; x < w; x++ {
		col := 4 * x
		var sr, sg, sb, sa float32
		for k := -p.left; k < p.size-p.left; k++ {
			if k < 0 || k >= h {
				continue
			}
			i := k*stride + col
			sr, sg, sb, sa = sr+src[i], sg+src[i+1], sb+src[i+2], sa+src[i+3]
		}
		for y := 0; y < h; y++ {
			o := y*stride + col
			dst[o], dst[o+1], dst[o+2], dst[o+3] = sr*inv, sg*inv, sb*inv, sa*inv
			out := y - p.left
			in := y + p.size - p.left
			if out >= 0 && out < h {
				i := out*stride + col
				sr, sg, sb, sa = sr-src[i], sg-src[i+1], sb-src[i+2], sa-src[i+3]
			}
			if in >= 0 && in < h {
				i := in*stride + col
				sr, sg, sb, sa = sr+src[i], sg+src[i+1], sb+src[i+2], sa+src[i+3]
			}
		}
	}
}

// BlurAlpha blurs a single-channel COVERAGE plane and returns the result,
// applying exactly the three box passes GaussianBlur applies (the same
// boxSizes, the same extent clamping, the same treatment of samples off the
// edge as zero). The alpha it produces therefore matches the alpha channel
// GaussianBlur would produce for the same input — an equivalence the tests
// assert directly, because the box-shadow path depends on it.
//
// It exists for that path, whose input is a SOLID SHAPE in one uniform colour.
// Such an input carries no colour variation to blur: every covered pixel has
// the same RGB, so a full RGBA blur spends four channels of arithmetic, plus
// two whole RGBA<->float32 conversions, to recompute three channels that were
// already known. Measured on a shadow-heavy page, those conversions cost MORE
// than the blur itself.
//
// src is a w*h plane of coverage in [0,1] — not premultiplied RGBA, because
// with a single uniform colour there is nothing to premultiply against and
// coverage is the entire signal. It is not modified. A nil or short src, or a
// degenerate size, returns nil.
func BlurAlpha(src []float32, w, h int, sdx, sdy float64) []float32 {
	if w <= 0 || h <= 0 || len(src) < w*h {
		return nil
	}
	// Bound each axis by the extent it blurs across, exactly as GaussianBlur
	// does; see clampToExtent for why a deviation past this erases its input.
	sdx = clampToExtent(sdx, w)
	sdy = clampToExtent(sdy, h)
	bx, by := boxSizes(sdx), boxSizes(sdy)

	buf := make([]float32, w*h)
	copy(buf, src[:w*h])
	if bx == nil && by == nil {
		return buf // identity on both axes: unblurred, matching GaussianBlur
	}
	tmp := make([]float32, w*h)
	for _, d := range bx {
		boxBlurAlphaH(buf, tmp, w, h, d)
		buf, tmp = tmp, buf
	}
	for _, d := range by {
		boxBlurAlphaV(buf, tmp, w, h, d)
		buf, tmp = tmp, buf
	}
	return buf
}

// boxBlurAlphaH is boxBlurH over a single-channel plane: one horizontal box
// pass with a running sum, so the cost is O(pixels) whatever the box width.
func boxBlurAlphaH(src, dst []float32, w, h int, p boxPass) {
	if p.size <= 1 {
		copy(dst, src)
		return
	}
	inv := float32(1) / float32(p.size)
	for y := range h {
		row := y * w
		var s float32
		// Prime the window for x = 0: it spans [-left, size-left).
		for k := -p.left; k < p.size-p.left; k++ {
			if k >= 0 && k < w {
				s += src[row+k]
			}
		}
		for x := range w {
			dst[row+x] = s * inv
			if o := x - p.left; o >= 0 && o < w {
				s -= src[row+o]
			}
			if in := x + p.size - p.left; in >= 0 && in < w {
				s += src[row+in]
			}
		}
	}
}

// boxBlurAlphaV is boxBlurV over a single-channel plane.
func boxBlurAlphaV(src, dst []float32, w, h int, p boxPass) {
	if p.size <= 1 {
		copy(dst, src)
		return
	}
	inv := float32(1) / float32(p.size)
	for x := range w {
		var s float32
		for k := -p.left; k < p.size-p.left; k++ {
			if k >= 0 && k < h {
				s += src[k*w+x]
			}
		}
		for y := range h {
			dst[y*w+x] = s * inv
			if o := y - p.left; o >= 0 && o < h {
				s -= src[o*w+x]
			}
			if in := y + p.size - p.left; in >= 0 && in < h {
				s += src[in*w+x]
			}
		}
	}
}
