package draw

import (
	"image"
	"image/color"

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
//
// CALL-SITE INVARIANT (do not break without reading this): every caller of
// buildMask passes its result straight to EndGroup, optionally combining it
// with a clip mask via attenuateByMask FIRST (see draw.go's Group case and
// paintShapeGrouped) — never storing it, deferring the matching EndGroup, or
// routing it through anything else in between. This matters because at
// least one backend (pkg/render/pdfwrite) returns a SENTINEL pointer from
// BuildLuminanceMask rather than real per-pixel content, recognized only by
// exact pointer identity in its own EndGroup (see softmask.go's
// takePendingSoftMask); combining that sentinel with another mask (as
// attenuateByMask does) yields a plain *image.Alpha that no longer matches
// the sentinel, which pdfwrite then silently treats as a real (but
// wrong — a 1x1 stand-in) coverage buffer instead of erroring. A future PR
// that builds a mask here and defers its EndGroup, or introduces a THIRD
// mask-combining call between BuildLuminanceMask and EndGroup, must first
// resolve this by making pdfwrite's sentinel survive combination (or by
// rejecting the combination outright) — do not assume "it still compiles"
// means it still renders correctly on the PDF backend.
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
		mask = attenuateByMask(mask, selfMask)
	}
	return mask
}

// attenuateByMask returns a mask whose per-pixel coverage is the PRODUCT
// (not the min — contrast combineClipRegions in clip.go, used for
// clip-path's boolean-AND region semantics) of a and b's coverage, fractions
// in [0,1] scaled to [0,255]. Use this whenever EITHER operand is (or may
// be) a <mask> luminance/alpha result — a soft mask is deliberately
// fractional, that is the entire point of it — including a <mask> that
// itself carries a mask="url(#...)" self-reference, which composes as "this
// mask's value = this mask's own content x its referenced mask" (see
// TestNestedMaskOnMask's doc comment), the same multiplicative stacking as
// two alpha channels compositing, NOT a hard intersection of two regions.
// Using min here instead of a product is invisible whenever both masks are
// locally binary (0 or 255, as in every prior nested-mask test) but diverges
// visibly once either mask carries a soft gradient value, which is exactly
// the resvg mask-on-self-with-mixed-mask-type corpus fixture: min
// systematically UNDER-attenuates (produces more coverage than correct)
// wherever either operand alpha is fractional. This is why a clip mask
// combined with a luminance mask (see draw.go's Group- and Shape-level mask
// handling) must also go through attenuateByMask, not combineClipRegions:
// the clip operand may be binary, but the luminance operand generally is
// not, and combineClipRegions's min would under-attenuate exactly like the
// mask-on-mask case above.
func attenuateByMask(a, b render.GroupMask) render.GroupMask {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	r := a.Bounds().Union(b.Bounds())
	out := image.NewAlpha(r)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			av := a.AlphaAt(x, y).A
			bv := b.AlphaAt(x, y).A
			v := uint8((uint16(av) * uint16(bv)) / 255)
			if v != 0 {
				out.SetAlpha(x, y, color.Alpha{A: v})
			}
		}
	}
	return out
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
