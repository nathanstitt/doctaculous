package content

import (
	"image/color"

	"github.com/nathanstitt/doctaculous/pkg/pdf"
	"github.com/nathanstitt/doctaculous/pkg/render"
)

// colorSpaceByName resolves a "cs"/"CS" operand to a simplified colorSpace. It
// recognizes the device names directly and otherwise classifies by the page's
// /ColorSpace resource component count, falling back to csOther.
func (it *Interpreter) colorSpaceByName(operands []pdf.Object) colorSpace {
	name := nameOperand(operands)
	switch name {
	case "DeviceGray", "G", "CalGray":
		return deviceGray
	case "DeviceRGB", "RGB", "CalRGB", "Lab":
		return deviceRGB
	case "DeviceCMYK", "CMYK":
		return deviceCMYK
	case "Pattern":
		return csPattern
	default:
		// Unknown named space: we cannot easily resolve component counts without
		// the resource dict here, so approximate as RGB-like. sc/scn tolerate
		// wrong counts.
		return csOther
	}
}

// tintTransform returns the tint transform for a cs/CS operand naming a Separation or
// DeviceN color space (resolved via the backend, which owns the /Function parsing), or
// nil for a device/Pattern/other space. The interpreter stores it on the graphics state
// so a later sc/scn maps the tint through it instead of mistaking a 1-component full-ink
// tint (1.0) for gray (which rendered white).
func (it *Interpreter) tintTransform(operands []pdf.Object) *TintTransform {
	name := nameOperand(operands)
	if name == "" || it.res == nil {
		return nil
	}
	if it.colorSpaceByName(operands) != csOther {
		return nil // device/pattern spaces carry no tint transform
	}
	if t, ok := it.res.ColorSpace(name); ok {
		return t
	}
	return nil
}

// resolveColorN maps sc/scn components to an RGBA color. When a tint transform is set
// (a Separation/DeviceN space) the components are tints: they are run through the tint
// /Function to alternate-space components, which are then converted by their count.
// Otherwise the components are converted directly per the color space.
func (it *Interpreter) resolveColorN(cs colorSpace, tint *TintTransform, comps []float64) color.RGBA {
	if tint != nil && tint.Eval != nil {
		alt := tint.Eval(comps)
		return colorFromComponents(csForComps(tint.AlternateComps), alt)
	}
	return colorFromComponents(cs, comps)
}

// csForComps picks the device color space matching an alternate-space component count
// (1→gray, 4→cmyk, else rgb), used to convert a Separation/DeviceN tint-transform result.
func csForComps(n int) colorSpace {
	switch n {
	case 1:
		return deviceGray
	case 4:
		return deviceCMYK
	default:
		return deviceRGB
	}
}

// fillColor returns the current fill color as a render.FillColor, used to paint
// /ImageMask stencils through the current color.
func (it *Interpreter) fillColor() render.FillColor {
	c := it.gs.fill
	return render.FillColor{R: c.R, G: c.G, B: c.B, A: c.A}
}

// doXObject handles "Do": draw an image XObject or recurse into a form XObject.
func (it *Interpreter) doXObject(operands []pdf.Object, depth int) {
	name := nameOperand(operands)
	if name == "" || it.res == nil {
		return
	}
	if img, ok := it.res.Image(name, it.fillColor()); ok {
		// Image space maps the unit square to device space via the CTM. PDF image
		// space has (0,0) at the top-left of the image with y down within the unit
		// square, which our DrawImage contract already expects.
		it.dev.DrawImage(img, it.gs.ctm, it.gs.fillAlpha, it.gs.blendMode)
		return
	}
	if content, res, matrix, bbox, ok := it.res.Form(name); ok {
		// A form XObject runs as if wrapped in q/Q with its /Matrix concatenated.
		saved := it.gs.clone()
		savedRes := it.res
		it.dev.Save()
		it.gs.ctm = matrix.Mul(it.gs.ctm)
		// ISO 32000 §8.10.1: the form's /BBox is a mandatory clip. Clip to the BBox
		// rectangle mapped through the (now form-relative) CTM before running content,
		// so the form cannot paint outside its declared box. A missing/malformed BBox
		// (bbox == nil) degrades to no clip.
		it.clipFormBBox(bbox)
		it.res = res
		_ = it.run(content, depth+1) // form errors are logged internally, never fatal
		it.res = savedRes
		it.gs = saved
		it.dev.Restore()
		return
	}
	it.logf("content: XObject %q not found", name)
}

// clipFormBBox intersects the device clip with a form XObject's /BBox
// ([minX minY maxX maxY] in form space), mapped through the current CTM (which already
// includes the form's /Matrix). All four corners are transformed and connected so the
// clip is correct under a rotating/skewing matrix (not just an axis-aligned min/max box).
// A nil bbox (absent/malformed /BBox) is a no-op.
func (it *Interpreter) clipFormBBox(bbox *[4]float64) {
	if bbox == nil {
		return
	}
	corners := [4][2]float64{
		{bbox[0], bbox[1]}, {bbox[2], bbox[1]}, {bbox[2], bbox[3]}, {bbox[0], bbox[3]},
	}
	var path render.Path
	for i, c := range corners {
		x, y := it.gs.ctm.Apply(c[0], c[1])
		if i == 0 {
			path.MoveTo(x, y)
		} else {
			path.LineTo(x, y)
		}
	}
	path.Close()
	it.dev.PushClip(&path, render.NonZero)
}

// inlineImage consumes a BI...ID...EI inline image from the scanner and draws it
// like an image XObject. Decoding is delegated to the backend's Resources so this
// package stays free of pixel/color-space logic. A decode failure degrades
// gracefully: the image is skipped with a debug log, never a fatal error.
func (it *Interpreter) inlineImage(tok *contentTokenizer) {
	dict, data, err := tok.readInlineImage()
	if err != nil {
		it.logf("content: inline image: %v", err)
		return
	}
	if it.res == nil {
		return
	}
	img, ok := it.res.InlineImage(dict, data, it.fillColor())
	if !ok {
		it.logf("content: inline image not decoded")
		return
	}
	it.dev.DrawImage(img, it.gs.ctm, it.gs.fillAlpha, it.gs.blendMode)
}
