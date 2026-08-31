package content

import (
	"image"

	"github.com/nathanstitt/omnidoc/pkg/internal/render"
)

// withSoftMask runs paint (one or more Device paint calls) under the current
// soft mask, if any (ISO 32000-1 §11.6.4.3: an ExtGState /SMask applies to
// every subsequent painting operation until replaced or cleared). When no
// soft mask is set this is a direct call with no extra state — the common,
// unmasked case pays no BeginGroup/EndGroup overhead and produces identical
// output to a reader with no soft-mask support at all.
//
// Call sites: fillPath, strokePath, paintShading ("sh"), doXObject's image
// branch ("Do"), and inlineImage. A form XObject's "Do" is handled
// separately by runForm (xobject.go), which composes mask handling with its
// own /Group alpha/blend logic in one BeginGroup/EndGroup rather than
// nesting two independent group-opening helpers. Together these cover every
// PAINTING operator this package implements except glyph fill/stroke
// (showText/showTextArray). Text
// is intentionally NOT wrapped per glyph: a text run paints many glyphs in
// sequence, and wrapping each one individually would (a) re-render the mask
// form once per glyph, a real performance cliff for masked text, and (b)
// open and close a SEPARATE isolated group per glyph, reintroducing exactly
// the overlap-seam artifact BeginGroup/EndGroup exists to prevent (adjacent
// glyphs' antialiased edges would each composite against the mask
// independently instead of once as a flattened run). Masking a text run
// correctly needs the mask applied once across the whole BT/ET span, which
// is a different scoping problem than the single-call wrap here; this
// writer never emits an /SMask around text (see pdfwrite's DrawGlyph), so
// this gap does not affect this engine's own PDF output, only the fidelity
// of an arbitrary third-party PDF that masks text specifically.
//
// When a mask IS set, paint runs inside an isolated offscreen group (exactly
// like an SVG mask's own compositing — see render.Device.BeginGroup) so the
// mask's per-pixel coverage applies to paint's flattened result once, via
// EndGroup's mask parameter, rather than needing per-primitive alpha
// modulation this package has no way to express for an arbitrary Device.
// alpha 1 is correct here (not it.gs.fillAlpha/strokeAlpha): paint's own
// calls already fold the constant alpha into their FillPaint/StrokePaint
// colors as usual (see fillPath/strokePath), so EndGroup must not apply it a
// second time.
//
// it.gs.softMask is cleared for the DURATION of paint (restored after): paint
// may recurse into a form XObject whose own nested content stream paints
// again, and every nested paint call would otherwise see the same
// still-set gs.softMask and wrap ITSELF in a second, redundant group —
// applying the mask twice (once correctly here, around the whole form, and
// once incorrectly per nested paint call inside it). Clearing it here is
// exactly the mask analogue of Save/Restore: this call is the ONE place the
// mask applies; nested painting during it is unmasked because it is already
// inside the masked group.
func (it *Interpreter) withSoftMask(paint func()) {
	sm := it.gs.softMask
	if sm == nil {
		paint()
		return
	}
	it.dev.Save()
	it.dev.BeginGroup()
	it.gs.softMask = nil
	paint()
	it.gs.softMask = sm
	mask := it.renderSoftMask(sm, it.gs.softMaskCTM)
	// mask is a PDF /SMask (soft mask), not a clip region — see
	// xobject.go's matching EndGroup call for why this is the softMask
	// parameter, not clipMask.
	it.dev.EndGroup(1, it.gs.blendMode, nil, mask)
	it.dev.Restore()
}

// renderSoftMask turns sm into a render.GroupMask by running its content
// through a NESTED Interpreter against the Device BuildLuminanceMask hands
// back — the same "backend supplies a scratch Device, this package paints
// into it" seam pkg/svg/draw uses for an SVG <mask> (see
// render.Device.BuildLuminanceMask's doc comment). ctm is the CTM active
// when the "gs" that set sm ran (captured in gstate.softMaskCTM), composed
// with the mask group's own /Matrix — the space the mask form is defined in
// per ISO 32000-1 §11.6.5.2, NOT the CTM active at paint time.
//
// A malformed/degenerate mask (zero device size, a Device that cannot
// rasterize offscreen) degrades to nil ("no masking") rather than panicking
// or blocking the paint it wraps, matching BuildLuminanceMask's own
// documented contract.
func (it *Interpreter) renderSoftMask(sm *SoftMask, ctm render.Matrix) render.GroupMask {
	w, h := it.dev.Size()
	if w <= 0 || h <= 0 {
		return nil
	}
	formCTM := sm.Matrix.Mul(ctm)
	return it.dev.BuildLuminanceMask(image.Point{X: w, Y: h}, sm.AlphaOnly, func(scratch render.Device) {
		sub := New(it.doc, scratch, sm.Res, it.base, Options{
			Logf:   it.logf,
			MaxOps: it.maxOps,
		})
		sub.gs.ctm = formCTM
		if sm.BBox != nil {
			sub.clipFormBBox(sm.BBox)
		}
		_ = sub.run(sm.Content, 0)
	})
}
