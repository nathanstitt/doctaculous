package draw

import (
	"image"

	"github.com/nathanstitt/omnidoc/pkg/internal/render"
	"github.com/nathanstitt/omnidoc/pkg/internal/svg"
)

// buildMask resolves msk (already known non-nil by the caller) into a
// device-space GroupMask, suitable for EndGroup. m is the accumulated
// user-space-to-device matrix in effect at the element msk masks (the same
// "post own-transform" matrix buildClipMask receives — a mask, like a
// clip-path, is defined relative to the referencing element's own user
// coordinate system). target reports the masked element's own bounding box
// for objectBoundingBox maskUnits/maskContentUnits.
//
// This is the seam pkg/svg/draw needs but cannot implement itself: a
// <mask>'s content must be RENDERED (not just rasterized as geometry) so its
// LUMINANCE (or alpha) becomes coverage — hence
// render.Device.BuildLuminanceMask, which hands back a Device wrapping a
// scratch surface for this function to paint msk.Kids into, then converts
// the result. dev is also the device the FINAL mask composites against (via
// EndGroup), so its Size() gives the scratch its pixel dimensions.
//
// An empty msk.Kids (an empty <mask>, or one whose children all resolved to
// nothing) still calls BuildLuminanceMask with a paint func that paints
// nothing: an all-transparent scratch converts to an all-zero mask, which is
// exactly "fully masked out" — the correct SVG behavior for an empty mask
// (see svg.Mask.Kids's doc comment), achieved for free without a special
// case here.
//
// CALL-SITE INVARIANT (do not break without reading this): every caller of
// buildMask passes its result straight to EndGroup's softMask parameter,
// SEPARATELY from any clip mask (see draw.go's Group case and
// paintShapeGrouped, and render.Device.EndGroup's doc comment on why the two
// are distinct parameters, never pre-combined into one GroupMask) — never
// storing buildMask's result, deferring the matching EndGroup, or combining
// it with another mask in between. This matters because at least one
// backend (pkg/render/pdfwrite) returns a SENTINEL pointer from
// BuildLuminanceMask rather than real per-pixel content, recognized only by
// exact pointer identity in its own EndGroup (see softmask.go's
// takePendingSoftMask); combining that sentinel with another mask (the way
// an earlier revision of this function combined msk.Self's mask with msk's
// own via attenuateByMask, and the way an earlier revision of draw.go
// combined a clip mask with this function's result) yields a plain
// *image.Alpha that no longer matches the sentinel, which pdfwrite then
// silently treats as a real (but wrong — a 1x1 stand-in) coverage buffer
// instead of erroring, erasing content. This is why msk.Self (below) is
// applied INSIDE the same BuildLuminanceMask call as msk's own content, via
// a nested BeginGroup/EndGroup on the scratch device, rather than as a
// second, independent buildMask call whose result gets multiplied in
// afterward: this function must return AT MOST ONE GroupMask value, built by
// AT MOST ONE BuildLuminanceMask call, so a backend's sentinel (if any)
// always reaches its own EndGroup unmodified.
func (r *Renderer) buildMask(dev render.Device, msk *svg.Mask, m render.Matrix, target boundsFunc) render.GroupMask {
	regionM := maskUnitsMatrix(msk.Units, target).Mul(m)
	regionPath := maskRegionPath(msk, regionM)

	contentM := clipUnitsMatrix(msk.ContentUnits, target).Mul(m)

	w, h := dev.Size()
	alphaOnly := msk.Type == svg.MaskAlpha
	return dev.BuildLuminanceMask(image.Point{X: w, Y: h}, alphaOnly, func(scratch render.Device) {
		if regionPath != nil {
			scratch.PushClip(regionPath, render.NonZero)
		}
		paintKids := func(target render.Device) {
			if msk.Kids == nil {
				return
			}
			warned := &warnFlags{}
			for _, kid := range msk.Kids.Kids {
				r.paint(target, kid, contentM, 1.0, warned)
			}
		}
		if msk.Self == nil {
			paintKids(scratch)
			return
		}
		// msk carries its own mask="url(#...)" self-reference: "this mask's
		// value = this mask's own content x its referenced mask" (see
		// TestNestedMaskOnMask's doc comment) — the same multiplicative
		// stacking as two alpha channels compositing, NOT a hard
		// intersection of two regions, so this is expressed as a nested
		// compositing GROUP (opacity 1, softMask = msk.Self's resolved
		// mask) around msk's own content, letting the scratch device's
		// OWN EndGroup do the per-pixel multiply — exactly the mechanism
		// this whole file already trusts for every other group+mask
		// composite, and the only way to combine two masks without ever
		// materializing an intermediate GroupMask value a backend sentinel
		// could fail to recognize.
		selfMask := r.buildMask(scratch, msk.Self, m, target)
		scratch.Save()
		scratch.BeginGroup()
		paintKids(scratch)
		scratch.EndGroup(1, "", nil, selfMask)
		scratch.Restore()
	})
}

// maskUnitsMatrix returns the matrix mapping a <mask>'s REGION rect
// (RegionX/Y/W/H, already in maskUnits-relative coordinates) into the space
// the caller's accumulated matrix composes against: Identity for
// userSpaceOnUse (the region is already an absolute user-unit rect), or the
// bbox-fraction mapping for objectBoundingBox (the default) — reusing
// clipUnitsMatrix's exact composition, since a mask region's
// objectBoundingBox mapping is defined identically to a clipPath's.
func maskUnitsMatrix(units string, target boundsFunc) render.Matrix {
	return clipUnitsMatrix(units, target)
}

// maskRegionPath builds the mask region rect as a device-space path (already
// transformed by regionM), or nil when the region is degenerate
// (objectBoundingBox with no usable target, or a zero/negative-area rect) —
// nil means "cannot establish a region", so buildMask paints without an
// extra clip rather than clipping to a garbage rect; a genuinely-empty
// region is vanishingly rare in practice (it would require an explicit
// zero-or-negative width/height attribute) and this degrades to "no region
// restriction" rather than silently hiding content, matching
// clipUnitsMatrix's own degrade-to-Identity behavior for the same case.
func maskRegionPath(msk *svg.Mask, regionM render.Matrix) *render.Path {
	if msk.RegionW <= 0 || msk.RegionH <= 0 {
		return nil
	}
	p := &render.Path{}
	p.MoveTo(msk.RegionX, msk.RegionY)
	p.LineTo(msk.RegionX+msk.RegionW, msk.RegionY)
	p.LineTo(msk.RegionX+msk.RegionW, msk.RegionY+msk.RegionH)
	p.LineTo(msk.RegionX, msk.RegionY+msk.RegionH)
	p.Close()
	dp := render.TransformPath(p, regionM)
	if dp == nil || dp.Empty() {
		return nil
	}
	return dp
}
