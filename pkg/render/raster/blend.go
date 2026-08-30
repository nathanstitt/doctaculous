package raster

import (
	"image"
	"image/color"

	"github.com/nathanstitt/omnidoc/pkg/render"
)

// The blend FUNCTIONS themselves moved to pkg/render (see render.SeparableBlend
// and render/blend.go) once SVG's <feBlend> primitive needed the same set: two
// consumers with nothing else in common must not carry two copies of
// colorBurn's zero cases. What stays here is only this backend's compositing
// glue — looking a mode up, and applying it per pixel against the surface —
// which is inherently raster-specific.

// sepBlend is a separable blend: it maps one destination and one source channel
// (each in [0,1]) to a result channel. The same function applies to R, G, B.
type sepBlend = render.SeparableBlend

// nonsepBlend is a non-separable blend operating on whole RGB triples in [0,1].
type nonsepBlend = render.NonSeparableBlend

// lookupBlend resolves a PDF /BM blend-mode name into whichever of the two
// blend function kinds implements it. Both ok flags false means the mode is
// Normal, Compatible, or unrecognized — all of which composite plain
// source-over.
func lookupBlend(name string) (sep sepBlend, isSep bool, nonsep nonsepBlend, isNonsep bool) {
	sep, isSep = render.SeparableBlendFunc(name)
	nonsep, isNonsep = render.NonSeparableBlendFunc(name)
	return sep, isSep, nonsep, isNonsep
}

// compositeBlend blends src color through the coverage mask (and active clip)
// onto the image using the named blend mode. "Normal"/"Compatible"/"" use plain
// source-over (the fast path); any other recognized mode applies its blend
// function before compositing. Unknown modes fall back to Normal.
func (d *Device) compositeBlend(mask *image.Alpha, c color.RGBA, blendMode string) {
	sep, isSep, nonsep, isNonsep := lookupBlend(blendMode)
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
