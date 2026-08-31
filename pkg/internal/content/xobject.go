package content

import (
	"image/color"

	"github.com/nathanstitt/omnidoc/pkg/internal/render"
	"github.com/nathanstitt/omnidoc/pkg/pdf"
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
		it.withSoftMask(func() {
			it.dev.DrawImage(img, it.gs.ctm, it.gs.fillAlpha, it.gs.blendMode)
		})
		return
	}
	if content, res, matrix, bbox, isGroup, ok := it.res.Form(name); ok {
		it.runForm(content, res, matrix, bbox, isGroup, depth)
		return
	}
	it.logf("content: XObject %q not found", name)
}

// runForm executes a form XObject's content (as if wrapped in q/Q with its
// /Matrix concatenated). It composites the form as an isolated transparency
// group — via Device.BeginGroup/EndGroup — whenever doing so can change the
// result:
//
//   - isGroup (the form declares /Group << /S /Transparency >>, ISO 32000-1
//     §8.10.3) AND the invocation carries a constant alpha < 1 or a
//     non-Normal blend mode: a group's whole point is that its own content
//     composites against a transparent backdrop first, so the group's alpha/
//     blend apply exactly ONCE to the flattened result, not per child paint.
//   - an active soft mask is set, REGARDLESS of isGroup: /SMask scoping (ISO
//     32000-1 §11.6.4.3) applies to "every subsequent painting operation"
//     until cleared, which includes a plain (non-group) form's content just
//     as much as a single fill — masking needs an isolated composite to
//     apply per-pixel coverage to a flattened result, the same mechanism a
//     group uses, independent of whether the form declares one itself.
//
// THE BUG THIS GUARDS AGAINST (the alpha/blend half): running a group form's
// content directly against the current alpha (the pre-existing behavior)
// folds that alpha into EVERY paint call inside the form individually, so
// two overlapping opaque children under 50% alpha double-darken at the
// overlap — the exact seam a transparency group exists to prevent. This is
// pkg/render/pdfwrite's OWN EndGroup output being misread: the writer emits
// a spec-correct isolated /Group Form composited via one "/GSn gs /Fmn Do",
// but until this fix nothing here ever inspected /Group, so the constant
// alpha from that "gs" leaked into each of the form's own fills instead of
// applying once to the form as a whole (confirmed independently: Poppler
// renders this writer's own group output correctly, so the emitted PDF was
// always right — only this reader's interpretation of it was wrong).
//
// A form invoked at full opacity/Normal blend/no mask skips the group
// entirely even when isGroup is true: an isolated group with nothing to
// apply produces an identical flattened result to painting its content
// directly, so this keeps the overwhelmingly common case (an opaque group,
// or a non-group form) on the cheap no-offscreen-allocation path. A
// non-group form under alpha < 1 with NO mask also skips the group (alpha
// applies per-primitive there, which is correct: a non-group form has no
// isolated backdrop of its own to flatten against).
func (it *Interpreter) runForm(content []byte, res Resources, matrix render.Matrix, bbox *[4]float64, isGroup bool, depth int) {
	sm := it.gs.softMask
	groupAlpha := isGroup && (it.gs.fillAlpha < 1 || (it.gs.blendMode != "" && it.gs.blendMode != "Normal"))
	if !groupAlpha && sm == nil {
		it.runFormContent(content, res, matrix, bbox, depth)
		return
	}
	it.dev.Save()
	it.dev.BeginGroup()
	// The mask is suspended for the DURATION of the form's own content
	// regardless of groupAlpha: it applies ONCE, at EndGroup, to the
	// flattened result below — exactly like withSoftMask suspends
	// gs.softMask for the same reason (see that function's doc comment).
	// Without this, a nested form or shape inside content would see the
	// same still-set mask and open a SECOND, redundant group for it.
	//
	// Alpha/blend are suspended ONLY when groupAlpha is true (an isolated
	// /Group form composited under alpha < 1 or a non-Normal blend): that is
	// the case where they must apply ONCE to the flattened group, not
	// per-primitive inside it (see this function's doc comment on why). A
	// non-group form reaching this branch purely because a mask is active
	// (groupAlpha false) has no isolated backdrop of its own — its content
	// still sees the ambient alpha/blend per-primitive exactly as it would
	// ungrouped, and only the mask's coverage is applied once at EndGroup
	// (with alpha 1, below), matching withSoftMask's identical reasoning.
	savedAlpha, savedStrokeAlpha, savedBlend := it.gs.fillAlpha, it.gs.strokeAlpha, it.gs.blendMode
	if groupAlpha {
		it.gs.fillAlpha, it.gs.strokeAlpha, it.gs.blendMode = 1, 1, "Normal"
	}
	it.gs.softMask = nil
	it.runFormContent(content, res, matrix, bbox, depth)
	it.gs.fillAlpha, it.gs.strokeAlpha, it.gs.blendMode = savedAlpha, savedStrokeAlpha, savedBlend
	it.gs.softMask = sm

	var mask render.GroupMask
	if sm != nil {
		mask = it.renderSoftMask(sm, it.gs.softMaskCTM)
	}
	// /ca (not /CA) is the constant alpha PDF applies to a group's own
	// composite (ISO 32000-1 §11.4.7.2: a group is always composited as if
	// filled, using the current non-stroking alpha) — fillAlpha, not a
	// stroke/fill blend, is the correct single alpha for the flattened form.
	// A mask-only (non-group) composite has no group alpha of its own to
	// apply beyond the mask, so it always passes 1 here regardless of
	// savedAlpha — matching withSoftMask's identical reasoning for its own
	// EndGroup call.
	alpha := 1.0
	if groupAlpha {
		alpha = savedAlpha
	}
	// mask here is always a PDF /SMask (luminosity/alpha soft mask, via
	// renderSoftMask) — a PDF clip is tracked separately (PushClip/W n, never
	// routed through EndGroup's mask parameter), so this is a softMask, not a
	// clipMask; see render.Device.EndGroup's doc comment on why the two are
	// now distinct parameters.
	it.dev.EndGroup(alpha, savedBlend, nil, mask)
	it.dev.Restore()
}

// runFormContent runs a form's content stream in form-relative space,
// clipped to its mandatory /BBox, restoring the interpreter's graphics state
// and resources afterward. Factored out of doXObject/runForm so both the
// grouped and ungrouped paths share identical form-execution semantics.
func (it *Interpreter) runFormContent(content []byte, res Resources, matrix render.Matrix, bbox *[4]float64, depth int) {
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
	it.withSoftMask(func() {
		it.dev.DrawImage(img, it.gs.ctm, it.gs.fillAlpha, it.gs.blendMode)
	})
}
