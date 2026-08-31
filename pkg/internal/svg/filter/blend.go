package filter

import (
	"image"

	"github.com/nathanstitt/omnidoc/pkg/internal/render"
)

// blendModePDFName maps an SVG feBlend `mode` value onto the PDF /BM name
// pkg/render's blend table is keyed by.
//
// The two vocabularies are the same set of functions under different spellings
// (CSS/SVG kebab-case versus PDF CamelCase), which is exactly why this is a
// NAME map rather than a second implementation: SVG 2 defines feBlend's modes
// by reference to the CSS compositing spec, and PDF's §11.3.5 blend functions
// are that same set. See render/blend.go for why one implementation is served
// to both.
//
// "normal" is deliberately ABSENT: it is plain source-over, which Composite
// already implements, so routing it through a blend function would be a slower
// path to the same pixels.
var blendModePDFName = map[string]string{
	"multiply":    "Multiply",
	"screen":      "Screen",
	"overlay":     "Overlay",
	"darken":      "Darken",
	"lighten":     "Lighten",
	"color-dodge": "ColorDodge",
	"color-burn":  "ColorBurn",
	"hard-light":  "HardLight",
	"soft-light":  "SoftLight",
	"difference":  "Difference",
	"exclusion":   "Exclusion",
	"hue":         "Hue",
	"saturation":  "Saturation",
	"color":       "Color",
	"luminosity":  "Luminosity",
}

// BlendModeName reports the PDF /BM blend name for an SVG feBlend `mode`
// value, and ok=false for "normal" or an unrecognized mode — both of which the
// caller renders as plain source-over, per SVG's "an unsupported value falls
// back to the initial value" error handling.
//
// Exported so the scene builder can VALIDATE a mode at parse time (keeping the
// renderer free of string handling) without duplicating the table.
func BlendModeName(mode string) (string, bool) {
	n, ok := blendModePDFName[mode]
	return n, ok
}

// Blend composites a (the `in` input) over b (the `in2` input) through the
// named blend mode, clipped to subregion — the feBlend primitive.
//
// mode is a PDF /BM name as BlendModeName returns; an empty or unrecognized
// name degrades to plain source-over, which is what SVG's "normal" mode is.
//
// The compositing algebra is the CSS/PDF one and it is NOT simply
// "blend the colors then composite":
//
//	co = (1 - ab)·as·Cs + ab·as·B(Cb, Cs) + (1 - as)·ab·Cb
//
// The middle term is the blended color, but it applies only where BOTH inputs
// have coverage; where the backdrop is transparent the source must come
// through unblended (blending against a transparent backdrop's meaningless
// color would tint the result). Collapsing this to a plain blend-then-over is
// the standard mistake, and it shows up precisely at a semi-transparent edge.
//
// Blending operates on STRAIGHT (un-premultiplied) color — B(Cb, Cs) is
// defined on actual colors, not alpha-scaled ones — while the compositing that
// surrounds it is premultiplied. Both conventions appear in this function on
// purpose.
func Blend(a, b *Buffer, mode string, subregion image.Rectangle) *Buffer {
	sep, isSep := render.SeparableBlendFunc(mode)
	nonsep, isNonsep := render.NonSeparableBlendFunc(mode)
	if !isSep && !isNonsep {
		// "normal" and every unrecognized mode: source-over.
		return Composite(a, b, CompositeOver, 0, 0, 0, 0, subregion)
	}

	space := bufferSpace(a, b)
	out := NewBuffer(subregion, space)
	for y := subregion.Min.Y; y < subregion.Max.Y; y++ {
		for x := subregion.Min.X; x < subregion.Max.X; x++ {
			sr, sg, sb, sa := a.At(x, y) // source: straight alpha
			br, bg, bb, ba := b.At(x, y) // backdrop: straight alpha
			if sa <= 0 && ba <= 0 {
				continue
			}

			// B(Cb, Cs), the blended colour, on straight channels.
			var xr, xg, xb float64
			if isSep {
				xr = sep(float64(br), float64(sr))
				xg = sep(float64(bg), float64(sg))
				xb = sep(float64(bb), float64(sb))
			} else {
				o := nonsep(
					[3]float64{float64(br), float64(bg), float64(bb)},
					[3]float64{float64(sr), float64(sg), float64(sb)},
				)
				xr, xg, xb = o[0], o[1], o[2]
			}

			ra := sa + ba - sa*ba
			if ra <= 0 {
				continue
			}
			mix := func(cs, cb, blended float64) float32 {
				// The CSS formula, in premultiplied form, divided back out by
				// the result alpha.
				co := (1-float64(ba))*float64(sa)*cs +
					float64(ba)*float64(sa)*blended +
					(1-float64(sa))*float64(ba)*cb
				v := co / float64(ra)
				if v <= 0 {
					return 0
				}
				if v >= 1 {
					return 1
				}
				return float32(v)
			}
			out.Set(x, y,
				mix(float64(sr), float64(br), xr),
				mix(float64(sg), float64(bg), xg),
				mix(float64(sb), float64(bb), xb),
				ra)
		}
	}
	return out
}
