package render

import "math"

// Blend modes implement the PDF (ISO 32000-1 §11.3.5) separable and
// non-separable blend functions: a blend produces a blended source color
// B(Cd, Cs) from the backdrop (destination) and source colors, which the caller
// then composites over the backdrop with the source's coverage alpha.
// "Normal" is plain source-over and has no blend function.
//
// The functions live HERE rather than in a backend because they have TWO
// consumers with nothing else in common: the raster backend's compositing (PDF
// /BM blend modes, where they originated) and SVG's <feBlend> filter primitive
// (pkg/svg/filter), which is defined on exactly the same CSS/PDF blend set.
// Keeping one implementation is what stops the two from drifting — a
// second-guessed copy of colorBurn's zero cases is precisely the kind of
// divergence that shows up only as a handful of off-by-one pixels in one of
// the two paths.
//
// They are pure channel math on [0,1] values, with no notion of alpha,
// coverage, or a device: alpha compositing is entirely the caller's business.

// SeparableBlend is a separable blend function: it maps one destination and
// one source channel (each in [0,1]) to a result channel. The same function
// applies independently to R, G and B.
type SeparableBlend func(cd, cs float64) float64

// NonSeparableBlend is a non-separable blend operating on whole RGB triples in
// [0,1]. These four modes (Hue, Saturation, Color, Luminosity) cannot be
// expressed per channel because each derives its result from the triple's
// luminosity or saturation as a whole.
type NonSeparableBlend func(dst, src [3]float64) [3]float64

// separableBlends maps PDF separable blend-mode names to their channel
// function. Lookup goes through SeparableBlendFunc so callers cannot mutate
// the table.
var separableBlends = map[string]SeparableBlend{
	"Multiply":   func(cd, cs float64) float64 { return cd * cs },
	"Screen":     func(cd, cs float64) float64 { return cd + cs - cd*cs },
	"Overlay":    func(cd, cs float64) float64 { return hardLight(cs, cd) }, // Overlay(b,s)=HardLight(s,b)
	"Darken":     math.Min,
	"Lighten":    math.Max,
	"ColorDodge": colorDodge,
	"ColorBurn":  colorBurn,
	"HardLight":  func(cd, cs float64) float64 { return hardLight(cd, cs) },
	"SoftLight":  softLight,
	"Difference": func(cd, cs float64) float64 { return math.Abs(cd - cs) },
	"Exclusion":  func(cd, cs float64) float64 { return cd + cs - 2*cd*cs },
}

// nonSeparableBlends maps PDF non-separable blend-mode names to their function.
var nonSeparableBlends = map[string]NonSeparableBlend{
	"Hue":        blendHue,
	"Saturation": blendSaturation,
	"Color":      blendColor,
	"Luminosity": blendLuminosity,
}

// SeparableBlendFunc looks up a separable blend mode by its PDF /BM name,
// reporting ok=false for a non-separable or unrecognized name.
func SeparableBlendFunc(name string) (SeparableBlend, bool) {
	f, ok := separableBlends[name]
	return f, ok
}

// NonSeparableBlendFunc looks up a non-separable blend mode by its PDF /BM
// name, reporting ok=false for a separable or unrecognized name.
func NonSeparableBlendFunc(name string) (NonSeparableBlend, bool) {
	f, ok := nonSeparableBlends[name]
	return f, ok
}

func colorDodge(cd, cs float64) float64 {
	switch {
	case cd == 0:
		return 0
	case cs == 1:
		return 1
	default:
		return math.Min(1, cd/(1-cs))
	}
}

func colorBurn(cd, cs float64) float64 {
	switch {
	case cd == 1:
		return 1
	case cs == 0:
		return 0
	default:
		return 1 - math.Min(1, (1-cd)/cs)
	}
}

func hardLight(cd, cs float64) float64 {
	if cs <= 0.5 {
		return cd * (2 * cs) // multiply
	}
	return cd + (2*cs - 1) - cd*(2*cs-1) // screen with 2*cs-1
}

func softLight(cd, cs float64) float64 {
	if cs <= 0.5 {
		return cd - (1-2*cs)*cd*(1-cd)
	}
	var d float64
	if cd <= 0.25 {
		d = ((16*cd-12)*cd + 4) * cd
	} else {
		d = math.Sqrt(cd)
	}
	return cd + (2*cs-1)*(d-cd)
}

// --- Non-separable helpers (PDF §11.3.5.3) ---

func lum(c [3]float64) float64 { return 0.3*c[0] + 0.59*c[1] + 0.11*c[2] }

func clipColor(c [3]float64) [3]float64 {
	l := lum(c)
	n := math.Min(c[0], math.Min(c[1], c[2]))
	x := math.Max(c[0], math.Max(c[1], c[2]))
	if n < 0 {
		for i := range c {
			c[i] = l + (c[i]-l)*l/(l-n)
		}
	}
	if x > 1 {
		for i := range c {
			c[i] = l + (c[i]-l)*(1-l)/(x-l)
		}
	}
	return c
}

func setLum(c [3]float64, l float64) [3]float64 {
	d := l - lum(c)
	return clipColor([3]float64{c[0] + d, c[1] + d, c[2] + d})
}

func sat(c [3]float64) float64 {
	return math.Max(c[0], math.Max(c[1], c[2])) - math.Min(c[0], math.Min(c[1], c[2]))
}

// setSat sets the saturation of c to s, preserving the relative ordering of
// channels (PDF SetSat algorithm).
func setSat(c [3]float64, s float64) [3]float64 {
	// Indices of min, mid, max channels.
	idx := [3]int{0, 1, 2}
	// Sort idx by channel value ascending (3 elements; explicit).
	if c[idx[0]] > c[idx[1]] {
		idx[0], idx[1] = idx[1], idx[0]
	}
	if c[idx[1]] > c[idx[2]] {
		idx[1], idx[2] = idx[2], idx[1]
	}
	if c[idx[0]] > c[idx[1]] {
		idx[0], idx[1] = idx[1], idx[0]
	}
	lo, mid, hi := idx[0], idx[1], idx[2]
	var out [3]float64
	if c[hi] > c[lo] {
		out[mid] = (c[mid] - c[lo]) * s / (c[hi] - c[lo])
		out[hi] = s
	}
	// out[lo] stays 0.
	return out
}

func blendHue(dst, src [3]float64) [3]float64 {
	return setLum(setSat(src, sat(dst)), lum(dst))
}

func blendSaturation(dst, src [3]float64) [3]float64 {
	return setLum(setSat(dst, sat(src)), lum(dst))
}

func blendColor(dst, src [3]float64) [3]float64 {
	return setLum(src, lum(dst))
}

func blendLuminosity(dst, src [3]float64) [3]float64 {
	return setLum(dst, lum(src))
}
