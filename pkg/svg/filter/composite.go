package filter

import "image"

// CompositeOperator selects which Porter-Duff (or arithmetic) rule feComposite
// applies.
type CompositeOperator int

const (
	// CompositeOver is the default: A over B, ordinary source-over
	// compositing.
	CompositeOver CompositeOperator = iota
	// CompositeIn keeps A only where B is opaque.
	CompositeIn
	// CompositeOut keeps A only where B is transparent.
	CompositeOut
	// CompositeAtop keeps A where B is opaque and B where A is not.
	CompositeAtop
	// CompositeXor keeps each input only where the other is transparent.
	CompositeXor
	// CompositeArithmetic is the non-Porter-Duff escape hatch:
	// result = k1*i1*i2 + k2*i1 + k3*i2 + k4, per channel, on PREMULTIPLIED
	// values, clamped to [0,1].
	CompositeArithmetic
)

// Composite combines a (the `in` input) with b (the `in2` input) under op,
// clipped to subregion — the feComposite primitive.
//
// Every operator here is defined on PREMULTIPLIED values, so the buffers'
// straight alpha is premultiplied on the way in and undone on the way out.
// The Porter-Duff algebra is only linear in premultiplied form; applying the
// same coefficients to straight color produces the classic "transparent areas
// pick up a colored haze" artifact.
//
// a and b must already be in the SAME color space (the caller converts both
// into the primitive's own space before calling); the result carries that
// space.
func Composite(a, b *Buffer, op CompositeOperator, k1, k2, k3, k4 float32, subregion image.Rectangle) *Buffer {
	space := bufferSpace(a, b)
	out := NewBuffer(subregion, space)
	for y := subregion.Min.Y; y < subregion.Max.Y; y++ {
		for x := subregion.Min.X; x < subregion.Max.X; x++ {
			ar, ag, ab, aa := premulAt(a, x, y)
			br, bg, bb, ba := premulAt(b, x, y)

			var rr, rg, rb, ra float32
			if op == CompositeArithmetic {
				rr = arith(k1, k2, k3, k4, ar, br)
				rg = arith(k1, k2, k3, k4, ag, bg)
				rb = arith(k1, k2, k3, k4, ab, bb)
				ra = arith(k1, k2, k3, k4, aa, ba)
			} else {
				fa, fb := porterDuffFactors(op, aa, ba)
				rr = ar*fa + br*fb
				rg = ag*fa + bg*fb
				rb = ab*fa + bb*fb
				ra = aa*fa + ba*fb
			}
			if ra <= 0 {
				continue
			}
			if ra > 1 {
				ra = 1
			}
			// Un-premultiply. Clamping the premultiplied colour to the alpha
			// first keeps a channel from exceeding 1 after the division, which
			// arithmetic can otherwise produce (k4 alone lifts colour without
			// lifting alpha).
			out.Set(x, y, clampChan(rr, ra), clampChan(rg, ra), clampChan(rb, ra), ra)
		}
	}
	return out
}

// porterDuffFactors returns the (Fa, Fb) coefficients of the Porter-Duff
// operator op for source alpha aa and destination alpha ba.
//
// These are the spec's own table (SVG 1.1 §feComposite, referencing Porter &
// Duff 1984): result = A*Fa + B*Fb on premultiplied values.
func porterDuffFactors(op CompositeOperator, aa, ba float32) (fa, fb float32) {
	switch op {
	case CompositeIn:
		return ba, 0
	case CompositeOut:
		return 1 - ba, 0
	case CompositeAtop:
		return ba, 1 - aa
	case CompositeXor:
		return 1 - ba, 1 - aa
	default: // CompositeOver
		return 1, 1 - aa
	}
}

// arith evaluates feComposite's arithmetic rule for one channel pair.
//
// The k coefficients are NOT clamped to [0,1] despite what the corpus fixture's
// <desc> claims: the reference renderer feeds them through verbatim and clamps
// only the RESULT, which is why the invalid-k1-4 fixture (k4="100") produces
// opaque white rather than being rejected. Clamping the coefficients instead
// would render that fixture as an ordinary composite and silently miss it.
func arith(k1, k2, k3, k4, i1, i2 float32) float32 {
	v := k1*i1*i2 + k2*i1 + k3*i2 + k4
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 1
	}
	return v
}

// clampChan un-premultiplies one channel by alpha, clamping the premultiplied
// value to alpha first so the straight result cannot exceed 1.
func clampChan(v, a float32) float32 {
	if v <= 0 {
		return 0
	}
	if v >= a {
		return 1
	}
	return v / a
}

// bufferSpace picks the colour space a two-input primitive's result carries,
// preferring a's. Both inputs are already in the same space by the time a
// primitive runs (the renderer converts each), so this only has to survive a
// nil input.
func bufferSpace(a, b *Buffer) ColorSpace {
	if a != nil {
		return a.Space
	}
	if b != nil {
		return b.Space
	}
	return LinearRGB
}

// Merge composites inputs in document order with source-over, clipped to
// subregion — the feMerge primitive.
//
// It is exactly a fold of CompositeOver over the list, and is written as one
// rather than as repeated Composite calls so a long merge allocates a single
// output buffer instead of one per node. The behavior must stay identical to
// that fold; the unit tests assert it against Composite directly.
//
// The FIRST node is the BOTTOM of the stack: feMergeNode order is painting
// order, so a later node covers an earlier one.
func Merge(inputs []*Buffer, subregion image.Rectangle, space ColorSpace) *Buffer {
	out := NewBuffer(subregion, space)
	for _, in := range inputs {
		if in == nil {
			continue
		}
		clip := in.Bounds().Intersect(subregion)
		for y := clip.Min.Y; y < clip.Max.Y; y++ {
			for x := clip.Min.X; x < clip.Max.X; x++ {
				sr, sg, sb, sa := premulAt(in, x, y)
				if sa <= 0 && sr == 0 && sg == 0 && sb == 0 {
					continue
				}
				dr, dg, db, da := premulAt(out, x, y)
				ia := 1 - sa
				rr, rg, rb := sr+dr*ia, sg+dg*ia, sb+db*ia
				ra := sa + da*ia
				if ra <= 0 {
					out.Set(x, y, 0, 0, 0, 0)
					continue
				}
				if ra > 1 {
					ra = 1
				}
				out.Set(x, y, clampChan(rr, ra), clampChan(rg, ra), clampChan(rb, ra), ra)
			}
		}
	}
	return out
}
