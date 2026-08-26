package draw

import (
	"image"

	"github.com/nathanstitt/doctaculous/pkg/render"
	"github.com/nathanstitt/doctaculous/pkg/svg"
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
func (r *Renderer) buildMask(dev render.Device, msk *svg.Mask, m render.Matrix, target boundsFunc) render.GroupMask {
	regionM := maskUnitsMatrix(msk.Units, target).Mul(m)
	regionPath := maskRegionPath(msk, regionM)

	contentM := clipUnitsMatrix(msk.ContentUnits, target).Mul(m)

	w, h := dev.Size()
	alphaOnly := msk.Type == svg.MaskAlpha
	mask := dev.BuildLuminanceMask(image.Point{X: w, Y: h}, alphaOnly, func(scratch render.Device) {
		if regionPath != nil {
			scratch.PushClip(regionPath, render.NonZero)
		}
		if msk.Kids != nil {
			warned := &warnFlags{}
			for _, kid := range msk.Kids.Kids {
				r.paint(scratch, kid, contentM, 1.0, warned)
			}
		}
	})
	if mask == nil {
		// A backend that cannot rasterize offscreen (see
		// render.Device.BuildLuminanceMask's doc comment on graceful
		// degradation, e.g. pdfwrite today): "no masking" is the only
		// available fallback, since there is no per-pixel result to apply.
		return nil
	}

	if msk.Self != nil {
		selfMask := r.buildMask(dev, msk.Self, m, target)
		mask = intersectMasks(mask, selfMask)
	}
	return mask
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
