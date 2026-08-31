package draw

import (
	"image"

	"github.com/nathanstitt/omnidoc/pkg/render"
)

// transformDevice wraps a render.Device to pre-multiply every piece of
// geometry passing through it by a fixed matrix.
//
// It exists for one job: painting a filter's source subtree into FILTER SPACE
// (the region-aligned pixel grid a filter's primitives operate in) rather than
// the canvas grid. The paint closure that draws the source has the element's
// device matrix already baked in — it re-enters paintShape/paintGroupBody with
// the accumulated matrix those functions already hold — so the change of basis
// cannot be threaded through as a parameter and is applied here, at the Device
// seam, instead.
//
// Every method that carries device-space geometry (paths, image placements,
// glyph transforms, shading CTMs, clips) composes m in; everything else
// forwards unchanged. m is a change of basis between two device-space grids,
// so stroke widths and dash lengths scale with it exactly as they would have
// under the original matrix.
type transformDevice struct {
	render.Device
	m render.Matrix
}

// Fill transforms the path into the wrapped device's grid.
//
// Size is deliberately NOT overridden: the wrapped scratch surface is already
// allocated to hold the filter region in filter space, so its own pixel
// extent is exactly what callers must see.
func (d *transformDevice) Fill(path *render.Path, paint render.FillPaint) {
	d.Device.Fill(render.TransformPath(path, d.m), paint)
}

// Stroke transforms the path and scales the stroke's width, dash pattern, and
// dash phase by m's scale factor, so a stroke keeps the same visual weight
// relative to the geometry it outlines.
func (d *transformDevice) Stroke(path *render.Path, paint render.StrokePaint) {
	sf := d.m.ScaleFactor()
	if sf > 0 && sf != 1 {
		paint.Width *= sf
		paint.DashPhase *= sf
		if paint.DashArray != nil {
			scaled := make([]float64, len(paint.DashArray))
			for i, v := range paint.DashArray {
				scaled[i] = v * sf
			}
			paint.DashArray = scaled
		}
	}
	d.Device.Stroke(render.TransformPath(path, d.m), paint)
}

// DrawImage composes m into the image's placement matrix.
func (d *transformDevice) DrawImage(img image.Image, ctm render.Matrix, alpha float64, blendMode string) {
	d.Device.DrawImage(img, ctm.Mul(d.m), alpha, blendMode)
}

// FillGlyph transforms the already-device-space glyph outline.
func (d *transformDevice) FillGlyph(outline *render.Path, c render.FillColor, blendMode string) {
	d.Device.FillGlyph(render.TransformPath(outline, d.m), c, blendMode)
}

// DrawGlyph composes m into the glyph's em-space-to-device transform.
func (d *transformDevice) DrawGlyph(g render.GlyphRef) {
	g.Transform = g.Transform.Mul(d.m)
	d.Device.DrawGlyph(g)
}

// FillShading composes m into the shading's user-space-to-device CTM, so the
// gradient lands on the same geometry it would have without the wrapper.
func (d *transformDevice) FillShading(shader render.Shader, ctm render.Matrix, blendMode string) {
	d.Device.FillShading(shader, ctm.Mul(d.m), blendMode)
}

// PushClip transforms the clip path.
func (d *transformDevice) PushClip(path *render.Path, rule render.FillRule) {
	d.Device.PushClip(render.TransformPath(path, d.m), rule)
}

// BuildClipMask transforms each clip child's path before handing the set to
// the wrapped backend, which rasterizes in ITS pixel grid — the filter-space
// grid here.
func (d *transformDevice) BuildClipMask(paths []render.MaskPath) render.GroupMask {
	out := make([]render.MaskPath, len(paths))
	for i, mp := range paths {
		out[i] = render.MaskPath{Path: render.TransformPath(mp.Path, d.m), Rule: mp.Rule}
	}
	return d.Device.BuildClipMask(out)
}

// BuildLuminanceMask paints the mask's content through this same wrapper, so
// a <mask> inside a filtered subtree lands in filter space alongside the
// content it masks.
func (d *transformDevice) BuildLuminanceMask(size image.Point, alphaOnly bool, paint func(dev render.Device)) render.GroupMask {
	if paint == nil {
		return d.Device.BuildLuminanceMask(size, alphaOnly, nil)
	}
	return d.Device.BuildLuminanceMask(size, alphaOnly, func(scratch render.Device) {
		paint(&transformDevice{Device: scratch, m: d.m})
	})
}

// RenderOffscreen paints through this same wrapper, so a NESTED filter inside
// an already-filtered subtree composes both changes of basis rather than
// losing the outer one.
func (d *transformDevice) RenderOffscreen(size image.Point, paint func(dev render.Device)) *image.RGBA {
	if paint == nil {
		return d.Device.RenderOffscreen(size, nil)
	}
	return d.Device.RenderOffscreen(size, func(scratch render.Device) {
		paint(&transformDevice{Device: scratch, m: d.m})
	})
}
