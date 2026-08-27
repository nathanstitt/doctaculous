// Package filter implements the pixel math behind SVG <filter> primitives:
// the color space filters operate in, the RGBA buffer they operate on, and
// each primitive's transform.
//
// It is deliberately free of any dependency on pkg/svg (the scene types) and
// pkg/render (the Device): it takes pixels in and hands pixels back, so every
// primitive is unit-testable against hand-computed values with no renderer,
// no document, and no golden image involved. pkg/svg/draw owns the glue that
// rasterizes the source, drives this package, and composites the result.
package filter

import "math"

// ColorSpace selects the space a filter's pixel math runs in, per the
// color-interpolation-filters property.
type ColorSpace int

const (
	// LinearRGB is the SVG DEFAULT for filters, and the one place this
	// engine departs from sRGB everywhere else. Filter math (blurring,
	// compositing, offsetting a partially transparent edge) is defined on
	// light-linear values; running it on gamma-encoded sRGB values instead
	// produces visibly wrong results — most obviously, a blur between two
	// colors passes through a too-dark midpoint.
	LinearRGB ColorSpace = iota
	// SRGB is the color-interpolation-filters:sRGB opt-out, which authors
	// use precisely because it is what a naive implementation does and some
	// designs were tuned against it.
	SRGB
)

// srgbLinearCutoff is the sRGB transfer function's breakpoint in ENCODED
// (gamma) space, below which the curve is a plain linear segment rather than
// a power function. The linear segment exists to keep the slope finite at
// zero, where a pure power curve's derivative is infinite; ignoring it (the
// classic "gamma 2.2" approximation) is wrong across the whole range and
// worst in the near-black values that dominate a blurred shadow's edge.
const srgbLinearCutoff = 0.04045

// linearCutoff is srgbLinearCutoff's counterpart in LINEAR space: the value
// srgbLinearCutoff maps to, and therefore the breakpoint the inverse
// transform switches on.
//
// It is DERIVED from srgbLinearCutoff rather than written as the usual
// rounded 0.0031308 literal, so the two segments of the curve always meet at
// exactly the same point. With independently-rounded constants the forward
// and inverse transforms disagree by a hair right at the boundary, and the
// round trip stops being exact there — which is precisely where a filter's
// near-black values sit.
const linearCutoff = srgbLinearCutoff / 12.92

// srgbToLinear converts one sRGB channel in [0,1] to linear-light [0,1] using
// the exact IEC 61966-2-1 transfer function — the linear segment near zero
// followed by the offset power curve, NOT a naive pow(v, 2.2).
//
// Values outside [0,1] are passed through the same formula rather than
// clamped, so a caller working in a wider intermediate range stays monotonic;
// callers that need clamping do it at the boundary where pixels are written.
func srgbToLinear(v float64) float64 {
	if v <= srgbLinearCutoff {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}

// linearToSRGB converts one linear-light channel in [0,1] back to sRGB,
// the exact inverse of srgbToLinear.
func linearToSRGB(v float64) float64 {
	if v <= linearCutoff {
		return v * 12.92
	}
	return 1.055*math.Pow(v, 1/2.4) - 0.055
}

// srgbToLinearLUT and linearToSRGBLUT map every 8-bit channel value through
// the transfer function once, at package init, so converting a filter buffer
// costs a table lookup per channel instead of a math.Pow call. A filter
// region is easily a million pixels and each conversion touches three
// channels twice (in and out), which is several million pow() calls per
// filtered element otherwise.
//
// The forward table is exact for 8-bit input. The INVERSE table is indexed by
// a linear value quantized to 8 bits, which would lose precision in the dark
// end where the linear encoding is densest, so linearToSRGB8 does not use a
// table at all — see its doc comment.
var srgbToLinearLUT [256]float64

func init() {
	for i := range srgbToLinearLUT {
		srgbToLinearLUT[i] = srgbToLinear(float64(i) / 255)
	}
}

// srgbToLinear8 converts an 8-bit sRGB channel to linear [0,1] via the
// precomputed table.
func srgbToLinear8(v uint8) float64 { return srgbToLinearLUT[v] }

// SRGBToLinear8 converts an 8-bit sRGB channel to linear-light [0,1].
//
// It is exported for callers that must convert an authored COLOR (rather than
// a rasterized pixel) into a filter's working space — feFlood's flood-color
// being the case that exists today. Skipping this conversion is the single
// most common way to get filter color spaces wrong, so the helper is exposed
// rather than left for each caller to reimplement.
func SRGBToLinear8(v uint8) float64 { return srgbToLinear8(v) }

// linearToSRGB8 converts a linear [0,1] channel back to an 8-bit sRGB value,
// rounding to nearest and clamping.
//
// This computes the transfer function directly rather than reading a table:
// a table would have to be indexed by the linear value quantized to 8 bits,
// but linear encoding packs most of its precision into the dark end (linear
// 0.002 and 0.008 are four sRGB steps apart yet land in the same 8-bit
// linear bucket), so a 256-entry inverse table would visibly band exactly
// the near-black gradients — a drop shadow's falloff — that filters produce
// most often.
func linearToSRGB8(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 255
	}
	return clamp8(linearToSRGB(v) * 255)
}

// clamp8 rounds v (a 0..255-scaled channel value) to the nearest uint8,
// clamping out-of-range input rather than letting it wrap.
func clamp8(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(v + 0.5)
}
