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
		// child's own clip-path is resolved as userSpaceOnUse against the
		// SAME device-space CTM the child's geometry now lives in: this
		// engine does not resolve a per-nested-child objectBoundingBox
		// target for this case (a documented, narrow approximation — the
		// common case, and every corpus fixture for this feature, applies
		// clip-path to a clipPath's direct children without a further
		// nested reference).
		childMask := dev.BuildClipMask([]render.MaskPath{{Path: dp, Rule: kid.Rule}})
		selfMask := r.buildClipMask(dev, kid.Self, cpM, nil)
		nested = append(nested, intersectMasks(childMask, selfMask))
	}

	mask := dev.BuildClipMask(flat)
	for _, n := range nested {
		mask = unionMasks(mask, n)
	}

	if cp.Self != nil {
		selfMask := r.buildClipMask(dev, cp.Self, m, target)
		mask = intersectMasks(mask, selfMask)
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

// intersectMasks returns a mask covering the intersection of a and b's
// coverage. Both are always non-nil in this file's call sites (BuildClipMask
// never returns nil — see its doc comment), but nil is still handled
// defensively (treated as "no restriction", i.e. returns the other operand)
// so this helper's contract doesn't silently depend on that.
func intersectMasks(a, b render.GroupMask) render.GroupMask {
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
