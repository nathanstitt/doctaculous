package raster

import (
	"image"
	"image/color"
	"math"

	"github.com/nathanstitt/doctaculous/pkg/render"
)

// Blend modes implement the PDF (ISO 32000-1 §11.3.5) separable and
// non-separable blend functions. A blend mode produces a blended source color
// B(Cd, Cs) from the backdrop (destination) and source colors; that blended
// color is then composited over the backdrop with the source's coverage alpha.
// "Normal" is plain source-over and uses the fast over() path.

// sepBlend is a separable blend: it maps one destination and one source channel
// (each in [0,1]) to a result channel. The same function applies to R, G, B.
type sepBlend func(cd, cs float64) float64

// nonsepBlend is a non-separable blend operating on whole RGB triples in [0,1].
type nonsepBlend func(dst, src [3]float64) [3]float64

// separableBlends maps PDF separable blend-mode names to their channel function.
var separableBlends = map[string]sepBlend{
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
var nonSeparableBlends = map[string]nonsepBlend{
	"Hue":        blendHue,
	"Saturation": blendSaturation,
	"Color":      blendColor,
	"Luminosity": blendLuminosity,
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

// compositeBlend blends src color through the coverage mask (and active clip)
// onto the image using the named blend mode. "Normal"/"Compatible"/"" use plain
// source-over (the fast path); any other recognized mode applies its blend
// function before compositing. Unknown modes fall back to Normal.
func (d *Device) compositeBlend(mask *image.Alpha, c color.RGBA, blendMode string) {
	sep, isSep := separableBlends[blendMode]
	nonsep, isNonsep := nonSeparableBlends[blendMode]
	if !isSep && !isNonsep {
		d.composite(mask, c) // Normal / Compatible / unknown
		return
	}

	b := mask.Bounds()
	clip := d.activeClip()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			cov := mask.AlphaAt(x, y).A
			if cov == 0 {
				continue
			}
			if clip != nil {
				cov = mulU8(cov, clip.AlphaAt(x, y).A)
				if cov == 0 {
					continue
				}
			}
			a := mulU8(c.A, cov)
			if a == 0 {
				continue
			}
			dst := d.img.RGBAAt(x, y)
			blended := blendSource(dst, c, sep, nonsep, isSep)
			over(d.img, x, y, blended, a)
		}
	}
}

// blendSource computes the blended source color B(dst, src) for one pixel using
// either a separable or non-separable blend, preserving src's alpha. The result
// is the color to composite over the backdrop (the alpha compositing happens in
// the caller). isSep selects which blend function applies.
func blendSource(dst, src color.RGBA, sep sepBlend, nonsep nonsepBlend, isSep bool) color.RGBA {
	dr, dg, db := float64(dst.R)/255, float64(dst.G)/255, float64(dst.B)/255
	sr, sg, sb := float64(src.R)/255, float64(src.G)/255, float64(src.B)/255
	var br, bg, bb float64
	if isSep {
		br, bg, bb = sep(dr, sr), sep(dg, sg), sep(db, sb)
	} else {
		out := nonsep([3]float64{dr, dg, db}, [3]float64{sr, sg, sb})
		br, bg, bb = out[0], out[1], out[2]
	}
	return color.RGBA{R: render.Clamp8(br), G: render.Clamp8(bg), B: render.Clamp8(bb), A: src.A}
}
