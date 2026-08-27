package draw

import (
	"image"
	"math"

	"github.com/nathanstitt/doctaculous/pkg/render"
	"github.com/nathanstitt/doctaculous/pkg/svg"
	svgfilter "github.com/nathanstitt/doctaculous/pkg/svg/filter"
)

// maxFilterPixels bounds how many pixels one filter region may allocate.
//
// A filter region is author-controlled through filterUnits/x/y/width/height,
// and every primitive in the graph allocates its own buffer over that region
// (16 bytes per pixel in float32 RGBA), so an enormous region is a direct
// memory-amplification path from a tiny input file — the same class of
// build-time DoS a prior PR found in this parser. This caps ONE region at
// roughly 64 MB per buffer, comfortably above any legitimate page (a 300 DPI
// A4 page is ~8.7 M pixels) while stopping a crafted `width="1e9"` from
// asking for terabytes. Exceeding it degrades to painting the element
// unfiltered, with a log — never to a crash or a blank.
const maxFilterPixels = 4 << 20

// maxFilterNestingDepth bounds how many filters may be applied at once (a
// filtered element inside another filter's source content). Each level holds
// a full-canvas offscreen RGBA alive while its graph runs, so unbounded
// nesting is unbounded live memory — the same hazard maxGroupNestingDepth
// guards, at a lower limit because a filter's buffers are far larger than a
// compositing group's. Real documents never nest filters at all.
const maxFilterNestingDepth = 4

// paintFiltered renders node's content through its SVG filter and composites
// the result, or paints it unfiltered when filtering is unavailable.
//
// The pipeline is: rasterize the source into an isolated offscreen buffer
// (render.Device.RenderOffscreen), convert those pixels into the filter's
// color space, run the primitive graph, convert back, and draw the result as
// an image at the filter region's device-space position.
//
// paintSource paints the unfiltered element; it is called EITHER into the
// offscreen buffer (the filtered path) or directly onto dev (every
// degradation path), so a caller never has to duplicate its paint logic.
// target reports the element's user-space bounding box for objectBoundingBox
// units — for text this must be the real placed-glyph extent (see
// textUserBounds), never pkg/svg's build-time estimate.
//
// m is the accumulated user-space-to-device matrix at the filtered element,
// i.e. AFTER its own transform, matching what buildClipMask/buildMask
// receive: a filter region is defined in the referencing element's own user
// coordinate system.
//
// Returns false when nothing was painted at all — the SVG "element is not
// rendered" outcome, which is distinct from both "painted filtered" and
// "painted unfiltered" and must not be confused with them.
// outAlpha scales the FILTERED RESULT (the element's own opacity times the
// caller's accumulated alpha), applied when the result is composited rather
// than folded into paintSource — see paintShape's filter branch for why the
// distinction is observable.
func (r *Renderer) paintFilteredAlpha(dev render.Device, f *svg.Filter, m render.Matrix, target boundsFunc, warned *warnFlags, outAlpha float64, paintSource func(render.Device)) bool {
	if f == nil {
		paintUnfiltered(dev, outAlpha, paintSource)
		return true
	}
	if outAlpha <= 0 {
		return true // fully transparent result: nothing to composite
	}
	if f.RegionInvalid {
		// A zero/negative filter region disables the element entirely per
		// SVG — it is NOT rendered, filtered or otherwise.
		return false
	}
	for i := range f.Primitives {
		if f.Primitives[i].SubregionInvalid {
			// A negative primitive subregion is likewise a
			// rendering-disabling error, not a skipped primitive.
			return false
		}
	}
	if warned.filterDepth >= maxFilterNestingDepth {
		// Every level holds a full-canvas offscreen RGBA plus the graph's
		// own float32 buffers live at once, so this bounds concurrently-live
		// memory rather than merely CPU time — the same rationale as
		// maxGroupNestingDepth. Degrade to unfiltered rather than dropping
		// the subtree.
		r.logFilterRegionCapOnce(warned)
		paintUnfiltered(dev, outAlpha, paintSource)
		return true
	}
	if f.Unsupported != "" {
		// A primitive this engine does not implement: paint the element
		// UNFILTERED rather than letting it vanish, and name the primitive
		// so the user knows exactly which feature degraded. This is the
		// deliberate "a visible approximation beats a blank" rule — an
		// unimplemented primitive must never silently produce an empty
		// result.
		r.logFilterUnsupportedOnce(warned, f.Unsupported)
		paintUnfiltered(dev, outAlpha, paintSource)
		return true
	}

	// SVG runs a filter in "filter space": a coordinate system in which the
	// filter region is an AXIS-ALIGNED rectangle, with the element's own
	// transform applied to the filtered RESULT rather than to the pixels the
	// primitives see. That distinction only shows up under a rotation or
	// skew, but there it is total: flooding the region's device-space
	// bounding box paints an axis-aligned rectangle where the correct
	// output is a rotated parallelogram (the resvg feFlood and feOffset
	// complex-transform fixtures pin exactly this, and an earlier revision
	// of this code failed both).
	//
	// So the source is rendered through filterM — m with the region's
	// rotation/skew factored OUT, leaving only scale and translation — into
	// a buffer whose pixel grid is aligned to the region. The graph runs
	// there, and postM (the factored-out part) transforms the finished
	// result back on the way out.
	fs, ok := filterSpace(f, m, target, dev)
	if !ok {
		// No usable region: an objectBoundingBox filter on an element with
		// no bounding box (an empty group). Per SVG the element is not
		// rendered — matching the resvg on-an-empty-group-2 fixture, where
		// the flood produces nothing at all.
		return false
	}
	if fs.buffer.Empty() {
		// The region maps to no pixels (fully off-canvas, or degenerate).
		// The element still counts as rendered; its output simply lands
		// nowhere.
		return true
	}
	if px := fs.buffer.Dx() * fs.buffer.Dy(); px > maxFilterPixels {
		r.logFilterRegionCapOnce(warned)
		paintUnfiltered(dev, outAlpha, paintSource)
		return true
	}

	warned.filterDepth++
	src := dev.RenderOffscreen(fs.size, func(scratch render.Device) {
		// The source is painted through filterM, so its pixels land in the
		// region-aligned grid the primitives operate on.
		r.paintInFilterSpace(scratch, fs, paintSource)
	})
	warned.filterDepth--
	if src == nil {
		// The backend cannot rasterize offscreen (pdfwrite). Degrade to the
		// unfiltered element rather than dropping it: a filter has no vector
		// representation, so this is the documented, permanent behavior for
		// a vector backend, not a temporary gap.
		r.logFilterNoRasterOnce(warned)
		paintUnfiltered(dev, outAlpha, paintSource)
		return true
	}

	out := r.runFilterGraph(f, src, fs.buffer, fs.filterM, target)
	if out == nil {
		return true // graph produced nothing; the element's own paint is consumed
	}
	b := out.Bounds()
	if b.Empty() {
		return true
	}
	// Place the result: map its unit square onto the buffer rect, then
	// through postM (the rotation/skew factored out of filterM) to reach
	// device space.
	//
	// The Y scale is NEGATED and the anchor taken at the rect's BOTTOM
	// edge because DrawImage maps the unit square in PDF IMAGE SPACE, where
	// v runs Y-UP with the image's TOP row at v=1 (see
	// render.Device.DrawImage and the raster backend's `1-v` sampling). A
	// plain Scale.Mul(Translate) lands the result VERTICALLY MIRRORED —
	// which reads as a plausible-looking but wrong offset rather than an
	// obvious error, and cost a debugging round when feOffset's dy came out
	// negated.
	place := render.Scale(float64(b.Dx()), -float64(b.Dy())).
		Mul(render.Translate(float64(b.Min.X), float64(b.Max.Y))).
		Mul(fs.postM)
	// outAlpha (the element's own opacity times the caller's accumulated
	// alpha) applies HERE, to the filtered result, which is what SVG's
	// filter-then-opacity order requires — see this function's doc comment.
	dev.DrawImage(out, place, outAlpha, "")
	return true
}

// paintInFilterSpace runs paintSource against scratch with the filter-space
// transform in effect.
//
// paintSource closes over the ORIGINAL device matrix (it re-enters
// paintShape/paintGroupBody/paintText with the accumulated matrix those
// already hold), so the filter-space mapping cannot be threaded through it as
// a parameter. It is instead applied as a device-level transform: the scratch
// device is handed a wrapper that pre-multiplies every incoming path and
// image placement by fs.deviceToFilter, which is exactly the change of basis
// from the canvas grid to the region-aligned grid.
func (r *Renderer) paintInFilterSpace(scratch render.Device, fs filterSpaceInfo, paintSource func(render.Device)) {
	paintSource(&transformDevice{Device: scratch, m: fs.deviceToFilter})
}

// paintUnfiltered paints the filter's source directly onto dev at alpha,
// the shared degradation path for every reason filtering is unavailable.
//
// alpha still has to reach the output even though no filter ran, so an
// alpha < 1 goes through a compositing group: paintSource may issue several
// paint calls (a fill plus a stroke, or a whole subtree), and folding the
// factor into each one independently would double-darken wherever they
// overlap — the exact artifact BeginGroup/EndGroup exists to prevent. At
// alpha == 1 (the common case) the group is skipped entirely, so an
// unfiltered degradation costs no offscreen buffer.
func paintUnfiltered(dev render.Device, alpha float64, paintSource func(render.Device)) {
	if alpha >= 1 {
		paintSource(dev)
		return
	}
	dev.Save()
	dev.BeginGroup()
	paintSource(dev)
	dev.EndGroup(alpha, "", nil, nil)
	dev.Restore()
}

// runFilterGraph evaluates f's primitive chain over the rasterized source,
// returning the final result as a premultiplied device-space image, or nil
// when the graph yields nothing.
//
// Each primitive's output is stored so a later `in` can name it by index
// (svg.InputResult). Because `in` may only reference an EARLIER result (see
// svg.FilterInput.Index), this is a single forward pass with no cycle
// possible and no revisiting — the bound on work is simply the primitive
// count, itself capped at parse time.
func (r *Renderer) runFilterGraph(f *svg.Filter, src *image.RGBA, region image.Rectangle, m render.Matrix, target boundsFunc) *image.RGBA {
	if len(f.Primitives) == 0 {
		// A <filter> with no primitives produces transparent black per SVG:
		// the element disappears. Returning nil here (rather than the
		// source) is what implements that — see svg.Filter.Primitives.
		return nil
	}

	primM := filterPrimitiveMatrix(f, m, target)
	results := make([]*svgfilter.Buffer, len(f.Primitives))
	// sourceGraphic/sourceAlpha are built lazily and per color space: a
	// graph that never reads SourceAlpha never pays to build it, and a graph
	// mixing linearRGB and sRGB primitives needs the source in each.
	var srcCache [2]*svgfilter.Buffer
	sourceIn := func(space svgfilter.ColorSpace) *svgfilter.Buffer {
		if srcCache[space] == nil {
			srcCache[space] = svgfilter.FromRGBA(src, region, space)
		}
		return srcCache[space]
	}

	var last *svgfilter.Buffer
	for i := range f.Primitives {
		p := &f.Primitives[i]
		space := filterColorSpace(p.Space)

		var in *svgfilter.Buffer
		switch p.In.Kind {
		case svg.InputSourceGraphic:
			in = sourceIn(space)
		case svg.InputSourceAlpha:
			in = svgfilter.AlphaOnly(sourceIn(space))
		case svg.InputResult:
			in = results[p.In.Index]
		}
		if in == nil {
			in = sourceIn(space)
		}
		if in.Space != space {
			// A graph may mix color-interpolation-filters values, so each
			// primitive's input is brought into ITS OWN space. Copy first:
			// the buffer may be another primitive's stored result, which
			// must keep the space it was produced in.
			converted := svgfilter.Crop(in, in.Bounds())
			if converted == in {
				converted = cloneBuffer(in)
			}
			converted.ConvertTo(space)
			in = converted
		}

		sub := primitiveSubregion(p, f, region, in, primM)

		var out *svgfilter.Buffer
		switch p.Kind {
		case svg.PrimitiveFlood:
			cr, cg, cb, ca := floodChannels(p, space)
			out = svgfilter.Flood(sub, cr, cg, cb, ca, space)
		case svg.PrimitiveOffset:
			dx, dy := primM.ApplyVector(p.Dx, p.Dy)
			out = svgfilter.Offset(in, dx, dy, sub)
		}
		if out == nil {
			out = svgfilter.NewBuffer(sub, space)
		}
		results[i] = out
		last = out
	}
	if last == nil {
		return nil
	}
	return last.ToRGBA()
}

// cloneBuffer returns a deep copy of b, so converting a copy's color space
// cannot disturb a stored primitive result another input still refers to.
func cloneBuffer(b *svgfilter.Buffer) *svgfilter.Buffer {
	out := svgfilter.NewBuffer(b.Bounds(), b.Space)
	copy(out.Pix, b.Pix)
	return out
}

// filterColorSpace maps the scene's color-space enum onto the filter package's.
func filterColorSpace(s svg.FilterColorSpace) svgfilter.ColorSpace {
	if s == svg.FilterSRGB {
		return svgfilter.SRGB
	}
	return svgfilter.LinearRGB
}

// floodChannels converts an feFlood's straight sRGB flood color into the
// primitive's own working space, as [0,1] channels.
//
// flood-color is authored in sRGB like every other SVG color, so a linearRGB
// primitive must CONVERT it rather than reinterpret its numbers — skipping
// this makes every flood too bright, which is the most visible symptom of
// getting filter color spaces wrong.
func floodChannels(p *svg.FilterPrimitive, space svgfilter.ColorSpace) (r, g, b, a float32) {
	c := p.FloodColor
	a = float32(c.A) / 255
	if space == svgfilter.LinearRGB {
		return float32(svgfilter.SRGBToLinear8(c.R)),
			float32(svgfilter.SRGBToLinear8(c.G)),
			float32(svgfilter.SRGBToLinear8(c.B)), a
	}
	return float32(c.R) / 255, float32(c.G) / 255, float32(c.B) / 255, a
}

// filterSpaceInfo carries the change of basis between the canvas pixel grid
// and the region-aligned grid a filter's primitives operate in.
type filterSpaceInfo struct {
	// filterM maps the filtered element's USER space into filter space (the
	// pixel grid the primitives see). It is m with any rotation/skew
	// removed, so the filter region is axis-aligned under it.
	filterM render.Matrix
	// postM maps filter space back to DEVICE space, carrying exactly the
	// rotation/skew filterM dropped. filterM.Mul(postM) == m.
	postM render.Matrix
	// deviceToFilter maps the canvas device grid into filter space, i.e.
	// the inverse of postM. The source subtree is painted through it.
	deviceToFilter render.Matrix
	// buffer is the filter region as an axis-aligned rect in filter space,
	// and size is the offscreen surface that holds it.
	buffer image.Rectangle
	size   image.Point
}

// filterSpace computes the region-aligned coordinate system a filter runs in.
//
// The element's accumulated matrix m is split into filterM (scale and
// translation, under which the region is axis-aligned) and postM (the
// rotation/skew remainder, applied to the finished result). The split is what
// makes a filter on a rotated or skewed element produce a rotated result
// rather than an axis-aligned one — see paintFilteredAlpha's doc comment.
//
// The scale kept in filterM is m's own scale factor, so the filter rasterizes
// at the resolution the element is actually drawn at: filtering at a lower
// resolution and scaling up would blur the result, and a higher one would
// waste memory proportional to the square of the excess.
func filterSpace(f *svg.Filter, m render.Matrix, target boundsFunc, dev render.Device) (filterSpaceInfo, bool) {
	var fs filterSpaceInfo
	if f.Units == "objectBoundingBox" {
		if target == nil {
			return fs, false
		}
		if _, _, _, _, ok := target(); !ok {
			return fs, false
		}
	}

	// The region's four corners in device space give the scale m applies
	// along each region axis; dividing m by that scale leaves the pure
	// rotation/skew, which becomes postM.
	sf := m.ScaleFactor()
	if sf <= 0 || math.IsNaN(sf) || math.IsInf(sf, 0) {
		return fs, false
	}
	// filterM: the element's user space scaled by sf, with no rotation.
	fs.filterM = render.Scale(sf, sf)
	// postM: whatever remains of m after filterM, i.e. filterM⁻¹ · m.
	invFilter, ok := fs.filterM.Invert()
	if !ok {
		return fs, false
	}
	fs.postM = invFilter.Mul(m)
	fs.deviceToFilter, ok = fs.postM.Invert()
	if !ok {
		return fs, false
	}

	// The region rect in filter space, axis-aligned by construction.
	regionFilterM := clipUnitsMatrix(f.Units, target).Mul(fs.filterM)
	x0, y0 := regionFilterM.Apply(f.RegionX, f.RegionY)
	x1, y1 := regionFilterM.Apply(f.RegionX+f.RegionW, f.RegionY+f.RegionH)
	if math.IsNaN(x0) || math.IsNaN(y0) || math.IsNaN(x1) || math.IsNaN(y1) ||
		math.IsInf(x0, 0) || math.IsInf(y0, 0) || math.IsInf(x1, 0) || math.IsInf(y1, 0) {
		return fs, false
	}
	minX, maxX := math.Min(x0, x1), math.Max(x0, x1)
	minY, maxY := math.Min(y0, y1), math.Max(y0, y1)
	rect := image.Rect(
		int(math.Floor(minX)), int(math.Floor(minY)),
		int(math.Ceil(maxX)), int(math.Ceil(maxY)),
	)
	if rect.Empty() {
		return fs, false
	}

	// Clip the buffer to what can actually land on the canvas, so an
	// enormous region costs only the pixels that could ever be seen. The
	// canvas rect is mapped INTO filter space (through deviceToFilter) and
	// its axis-aligned hull taken, since under postM the canvas is not
	// axis-aligned in filter space.
	dw, dh := dev.Size()
	visible := hullOf(fs.deviceToFilter, 0, 0, float64(dw), float64(dh))
	// Grow by a pixel so an antialiased edge exactly on the boundary is not
	// trimmed by the floor/ceil rounding.
	visible = visible.Inset(-1)
	fs.buffer = rect.Intersect(visible)
	if fs.buffer.Empty() {
		// Nothing of the region can reach the canvas. Report success with an
		// empty buffer so the caller distinguishes this from "no region".
		fs.buffer = image.Rectangle{}
		fs.size = image.Point{}
		return fs, true
	}
	// RenderOffscreen allocates from the origin, so the surface must span
	// from (0,0) to the buffer's far corner.
	fs.size = image.Point{X: fs.buffer.Max.X, Y: fs.buffer.Max.Y}
	if fs.size.X <= 0 || fs.size.Y <= 0 {
		fs.buffer = image.Rectangle{}
		return fs, true
	}
	return fs, true
}

// hullOf returns the axis-aligned integer hull of the rect (x0,y0)-(x1,y1)
// mapped through m, rounded outward so no covered pixel is trimmed.
func hullOf(m render.Matrix, x0, y0, x1, y1 float64) image.Rectangle {
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, c := range [4][2]float64{{x0, y0}, {x1, y0}, {x0, y1}, {x1, y1}} {
		px, py := m.Apply(c[0], c[1])
		if math.IsNaN(px) || math.IsNaN(py) {
			return image.Rectangle{}
		}
		minX, maxX = math.Min(minX, px), math.Max(maxX, px)
		minY, maxY = math.Min(minY, py), math.Max(maxY, py)
	}
	return image.Rect(
		int(math.Floor(minX)), int(math.Floor(minY)),
		int(math.Ceil(maxX)), int(math.Ceil(maxY)),
	)
}

// filterPrimitiveMatrix returns the matrix mapping a primitive's own lengths
// (a subregion rect, feOffset's dx/dy) into device space, honoring
// primitiveUnits.
//
// For the userSpaceOnUse default this is just m (the element's user space).
// For objectBoundingBox it additionally scales by the target's bounding box,
// so dx="0.1" means a tenth of the box — the same composition
// clipUnitsMatrix performs for a clipPath's units.
func filterPrimitiveMatrix(f *svg.Filter, m render.Matrix, target boundsFunc) render.Matrix {
	return clipUnitsMatrix(f.PrimitiveUnits, target).Mul(m)
}

// primitiveSubregion computes the device-space rect a primitive's output is
// clipped to (the x/y/width/height on the <fe*> element itself).
//
// An unspecified edge does NOT default to the filter region for a primitive
// that HAS an input: it defaults to the input's own extent, which is what
// keeps a chained primitive from silently expanding back to the whole region.
// A primitive with NO input (feFlood) defaults to the filter region on every
// unspecified edge — the resvg subregion-inheritance fixture asserts exactly
// this, and it is why feFlood is the primitive that proves the region math.
//
// The result is always intersected with the filter region: SVG clips every
// primitive's output to it, so a subregion reaching outside (the
// subregion-bigger-that-region fixture) is trimmed rather than honored.
func primitiveSubregion(p *svg.FilterPrimitive, f *svg.Filter, region image.Rectangle, in *svgfilter.Buffer, primM render.Matrix) image.Rectangle {
	base := region
	if p.Kind != svg.PrimitiveFlood && in != nil && !in.Bounds().Empty() {
		base = in.Bounds()
	}
	if !p.HasSubregion {
		return base.Intersect(region)
	}

	// Each specified edge replaces its default, in device space. The rect is
	// built in the primitive's own units then transformed, so a rotated CTM
	// is handled the same way the filter region itself is.
	x0, y0 := float64(base.Min.X), float64(base.Min.Y)
	x1, y1 := float64(base.Max.X), float64(base.Max.Y)
	if p.HasX || p.HasY || p.HasW || p.HasH {
		ux0, uy0 := 0.0, 0.0
		ux1, uy1 := 0.0, 0.0
		// Recover the base rect in primitive units so an unspecified edge
		// keeps its default while a specified one overrides it.
		if inv, ok := primM.Invert(); ok {
			ux0, uy0 = inv.Apply(x0, y0)
			ux1, uy1 = inv.Apply(x1, y1)
		}
		if p.HasX {
			ux0 = p.X
		}
		if p.HasY {
			uy0 = p.Y
		}
		if p.HasW {
			ux1 = ux0 + p.W
		}
		if p.HasH {
			uy1 = uy0 + p.H
		}
		corners := [4][2]float64{{ux0, uy0}, {ux1, uy0}, {ux0, uy1}, {ux1, uy1}}
		minX, minY := math.Inf(1), math.Inf(1)
		maxX, maxY := math.Inf(-1), math.Inf(-1)
		for _, c := range corners {
			dx, dy := primM.Apply(c[0], c[1])
			if math.IsNaN(dx) || math.IsNaN(dy) {
				return base.Intersect(region)
			}
			minX, maxX = math.Min(minX, dx), math.Max(maxX, dx)
			minY, maxY = math.Min(minY, dy), math.Max(maxY, dy)
		}
		return image.Rect(
			int(math.Floor(minX)), int(math.Floor(minY)),
			int(math.Ceil(maxX)), int(math.Ceil(maxY)),
		).Intersect(region)
	}
	return base.Intersect(region)
}

// groupUserBounds reports a bounds function over the union of node's
// descendants' geometry, in node's OWN user space (each child's local
// transform applied, but not node's own M — matching the space every other
// objectBoundingBox target in this package is measured in).
//
// A group has no single Path of its own, so this is what lets an
// objectBoundingBox filter region on a <g> resolve instead of degrading to
// the identity mapping clip-path and mask fall back to. It matters far more
// for filters than for those two: a filter's DEFAULT units are
// objectBoundingBox, so the common case depends on it, whereas a clip-path
// or mask on a group usually names userSpaceOnUse explicitly.
//
// ok=false for a group with no drawable descendant (an empty <g>), which per
// SVG means an objectBoundingBox filter cannot be applied and the element is
// not rendered — the resvg on-an-empty-group-2 behavior.
func groupUserBounds(node *svg.Group, warned *warnFlags) boundsFunc {
	return func() (minX, minY, maxX, maxY float64, ok bool) {
		var walk func(n svg.Node, m render.Matrix, depth int)
		walk = func(n svg.Node, m render.Matrix, depth int) {
			if depth > maxGroupNestingDepth {
				// Bound the walk the same way painting is bounded, so a
				// pathological tree cannot make bbox measurement itself the
				// expensive operation.
				return
			}
			switch k := n.(type) {
			case *svg.Group:
				if k == nil {
					return
				}
				gm := k.M.Mul(m)
				for _, kid := range k.Kids {
					walk(kid, gm, depth+1)
				}
			case *svg.Shape:
				if k == nil || k.Path == nil {
					return
				}
				dp := render.TransformPath(k.Path, k.M.Mul(m))
				if dp == nil {
					return
				}
				x0, y0, x1, y1, got := dp.Bounds()
				if !got {
					return
				}
				if !ok {
					minX, minY, maxX, maxY, ok = x0, y0, x1, y1, true
					return
				}
				minX, minY = math.Min(minX, x0), math.Min(minY, y0)
				maxX, maxY = math.Max(maxX, x1), math.Max(maxY, y1)
			case *svg.Text:
				// A <text> descendant contributes NOTHING to the union
				// today: its extent is only known after layoutText places
				// the glyphs (see textUserBounds), which this measurement
				// pass has no cheap access to. The consequence is narrow and
				// documented rather than silent: an objectBoundingBox filter
				// on a group whose ONLY content is text resolves to no
				// bounding box and the element is not rendered. A filter on
				// the <text> element ITSELF — the overwhelmingly common
				// authoring form, and the one the spec's text-and-filters
				// case is about — goes through paintText's own
				// textUserBounds seam and is exact.
				_ = k
			}
		}
		for _, kid := range node.Kids {
			walk(kid, render.Identity, 0)
		}
		return minX, minY, maxX, maxY, ok
	}
}

// logFilterUnsupportedOnce emits the unsupported-primitive notice the first
// time it is needed for the current DrawVector call, naming the primitive.
func (r *Renderer) logFilterUnsupportedOnce(warned *warnFlags, name string) {
	if warned.filterUnsupported || r.Logf == nil {
		return
	}
	warned.filterUnsupported = true
	r.Logf("svg: <%s> filter primitive is not supported; the element was rendered unfiltered", name)
}

// logFilterNoRasterOnce emits the no-offscreen-raster notice the first time
// it is needed for the current DrawVector call.
func (r *Renderer) logFilterNoRasterOnce(warned *warnFlags) {
	if warned.filterNoRaster || r.Logf == nil {
		return
	}
	warned.filterNoRaster = true
	r.Logf("svg: this backend cannot rasterize offscreen; filtered elements were rendered unfiltered")
}

// logFilterRegionCapOnce emits the filter-region-too-large notice the first
// time it is needed for the current DrawVector call.
func (r *Renderer) logFilterRegionCapOnce(warned *warnFlags) {
	if warned.filterRegionCap || r.Logf == nil {
		return
	}
	warned.filterRegionCap = true
	r.Logf("svg: filter region exceeded %d pixels; the element was rendered unfiltered", maxFilterPixels)
}
