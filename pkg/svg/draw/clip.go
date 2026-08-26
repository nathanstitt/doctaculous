package draw

import (
	"image"
	"image/color"

	"github.com/nathanstitt/doctaculous/pkg/render"
	"github.com/nathanstitt/doctaculous/pkg/svg"
)

// boundsFunc reports the bounding box (in the local space a clip-path's
// objectBoundingBox mapping composes against) of the element a clip-path is
// being applied to, and whether one could be established (false for a
// degenerate/empty target, matching resolveGradient's objectBoundingBox
// failure mode in pkg/svg/gradient.go).
type boundsFunc func() (minX, minY, maxX, maxY float64, ok bool)

// buildClipMask resolves cp (already known non-nil by the caller) into a
// device-space GroupMask, suitable for EndGroup. m is the accumulated
// user-space-to-device matrix in effect at the element cp clips (BEFORE
// cp's own transform/units mapping is composed on top — those compose
// first, innermost, exactly like a gradient's local-to-user matrix composes
// under the shape's own accumulated matrix in fillGradient). target reports
// the clipped element's own bounding box for objectBoundingBox units, in
// the same local space m maps from.
//
// This is the seam pkg/svg/draw needs but cannot implement itself: a
// <clipPath>'s children form a UNION, but this package holds only a
// render.Device (see the package doc) and cannot rasterize to flatten that
// union into a mask on its own — hence Device.BuildClipMask, which does the
// rasterizing inside whichever backend is active. This function's job is
// purely geometric: resolve units, compose matrices, and hand the backend a
// flat list of (device-space path, own rule) pairs per non-nested child,
// combining any child that carries ITS OWN clip-path (and the <clipPath>'s
// own self clip-path) via mask intersection/union rather than folding them
// into that flat list, since a single MaskPath cannot express "this
// geometry AND this other mask."
func (r *Renderer) buildClipMask(dev render.Device, cp *svg.ClipPath, m render.Matrix, target boundsFunc) render.GroupMask {
	cpM := clipUnitsMatrix(cp.Units, target).Mul(cp.M).Mul(m)

	var flat []render.MaskPath
	var nested []render.GroupMask
	for _, kid := range cp.Kids {
		if kid.Path == nil || kid.Path.Empty() {
			continue
		}
		dp := render.TransformPath(kid.Path, kid.M.Mul(cpM))
		if dp == nil || dp.Empty() {
			continue
		}
		if kid.Self == nil {
			flat = append(flat, render.MaskPath{Path: dp, Rule: kid.Rule})
			continue
		}
		// This child has its own clip-path: its contribution to the union is
		// ITS geometry intersected with that clip-path's resolved region —
		// computed as a one-child mask (via the same BuildClipMask the
		// top-level union uses) intersected against the child's own
		// resolved mask, then folded into the overall union by max()
		// alongside every other contribution (see unionMasks below). The
		// child's own clip-path is a userSpaceOnUse reference defined
		// relative to the CHILD's own user coordinate system — which
		// includes the child's own transform (kid.M), not just the parent
		// clipPath's units/transform matrix (cpM) the child's geometry
		// composes under. Composing kid.M.Mul(cpM) here (matching the "dp"
		// composition just above) is what makes clip-path-on-child-with-
		// transform resolve clip2 in path1's own rotated/translated space
		// rather than clip1's: this engine does not resolve a
		// per-nested-child objectBoundingBox target for this case (a
		// documented, narrow approximation — the common case, and most
		// corpus fixtures for this feature, apply clip-path to a
		// clipPath's direct children without a further nested reference).
		childMask := dev.BuildClipMask([]render.MaskPath{{Path: dp, Rule: kid.Rule}})
		selfMask := r.buildClipMask(dev, kid.Self, kid.M.Mul(cpM), nil)
		nested = append(nested, combineClipRegions(childMask, selfMask))
	}

	mask := dev.BuildClipMask(flat)
	for _, n := range nested {
		mask = unionMasks(mask, n)
	}

	if cp.Self != nil {
		selfMask := r.buildClipMask(dev, cp.Self, m, target)
		mask = combineClipRegions(mask, selfMask)
	}
	return mask
}

// clipUnitsMatrix returns the matrix mapping a <clipPath>'s own local
// coordinate space into the space its M and the caller's accumulated matrix
// compose against: Identity for userSpaceOnUse, or the bbox-fraction
// mapping for objectBoundingBox — the exact same
// Scale(w,h).Mul(Translate(minX,minY)) composition resolveGradient uses in
// pkg/svg/gradient.go for a gradient's own objectBoundingBox mapping,
// reused here per the design's explicit instruction to share that
// machinery. A degenerate or unavailable target bbox under
// objectBoundingBox degrades to Identity, mirroring resolveGradient's
// "gradient cannot be established" handling (the caller's geometry then
// maps through Identity instead of a garbage inverted-scale matrix; SVG
// requires the CLIP not be established at all in this case, which the
// zero-Kids-mapped-through-Identity-but-target-zero-area combination
// approximates by producing an empty/degenerate mask in practice for every
// realistic case, since a zero-area target also means nothing sensible was
// being clipped in the first place).
func clipUnitsMatrix(units string, target boundsFunc) render.Matrix {
	if units != "objectBoundingBox" || target == nil {
		return render.Identity
	}
	minX, minY, maxX, maxY, ok := target()
	w, h := maxX-minX, maxY-minY
	if !ok || w == 0 || h == 0 {
		return render.Identity
	}
	return render.Scale(w, h).Mul(render.Translate(minX, minY))
}

// combineClipRegions returns a mask covering the boolean-AND (min, per
// pixel) of a and b's coverage: genuine region intersection, correct ONLY
// when both operands are clip regions (geometry-derived masks that are
// locally binary in intent — "this pixel is inside the shape" or not, with
// antialiasing as the only source of fractional edge values). Both are
// always non-nil in this file's call sites (BuildClipMask never returns nil
// — see its doc comment), but nil is still handled defensively (treated as
// "no restriction", i.e. returns the other operand) so this helper's
// contract doesn't silently depend on that.
//
// Do NOT use this to combine a clip region with a <mask> luminance result —
// a luminance mask is deliberately fractional (that is the entire point of
// a soft mask), and min() systematically UNDER-attenuates wherever it holds
// a fractional value, producing more coverage than correct (this was a real,
// shipped bug: see git history for "use product not min when combining a
// clip mask with a luminance mask"). A clip mask and a <mask> luminance
// result are never combined by pkg/svg/draw itself anymore — they reach
// render.Device.EndGroup as two SEPARATE parameters (clipMask, softMask; see
// its doc comment), and each backend combines them on its own terms: the
// raster backend multiplies their per-pixel coverage at composite time
// (pkg/render/raster/device.go's EndGroup), pdfwrite represents each with
// its own native PDF construct (a `W n` clip vs. an ExtGState /SMask) and
// never needs to combine them into one value at all. Keeping the two mask
// kinds apart end-to-end, rather than pre-combining them behind a helper
// like this one, is what fixed the bug: a backend that recognizes a mask by
// pointer identity (pdfwrite's own luminosity-soft-mask fast path) cannot
// survive being handed a value neither it nor render.Device's contract ever
// produced.
func combineClipRegions(a, b render.GroupMask) render.GroupMask {
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
			v := a.AlphaAt(x, y).A
			if bv := b.AlphaAt(x, y).A; bv < v {
				v = bv
			}
			if v != 0 {
				out.SetAlpha(x, y, color.Alpha{A: v})
			}
		}
	}
	return out
}

// unionMasks returns a mask covering the union of a and b's coverage
// (per-pixel max), the same combine BuildClipMask itself uses internally —
// needed here because a nested child-with-its-own-clip-path mask (see
// buildClipMask) is computed separately from the flat BuildClipMask call
// and must be folded back into the overall union afterward.
func unionMasks(a, b render.GroupMask) render.GroupMask {
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
			v := a.AlphaAt(x, y).A
			if bv := b.AlphaAt(x, y).A; bv > v {
				v = bv
			}
			if v != 0 {
				out.SetAlpha(x, y, color.Alpha{A: v})
			}
		}
	}
	return out
}
