package content

import (
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
	if content, res, matrix, ok := it.res.Form(name); ok {
		// A form XObject runs as if wrapped in q/Q with its /Matrix concatenated.
		saved := it.gs.clone()
		savedRes := it.res
		it.dev.Save()
		it.gs.ctm = matrix.Mul(it.gs.ctm)
		it.res = res
		_ = it.run(content, depth+1) // form errors are logged internally, never fatal
		it.res = savedRes
		it.gs = saved
		it.dev.Restore()
		return
	}
	it.logf("content: XObject %q not found", name)
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
