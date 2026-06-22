package raster

import (
	"image"
	"image/color"
	"math"

	"golang.org/x/image/vector"

	"github.com/nathanstitt/doctaculous/pkg/render"
)

// replay feeds a device-space path into a vector.Rasterizer, offsetting by the
// image origin (ox, oy) so non-zero-origin bounds work.
func replay(r *vector.Rasterizer, path *render.Path, ox, oy float32) {
	var started bool
	for _, s := range path.Segments {
		switch s.Kind {
		case render.MoveTo:
			r.MoveTo(float32(s.P0.X)-ox, float32(s.P0.Y)-oy)
			started = true
		case render.LineTo:
			if !started {
				r.MoveTo(float32(s.P0.X)-ox, float32(s.P0.Y)-oy)
				started = true
				continue
			}
			r.LineTo(float32(s.P0.X)-ox, float32(s.P0.Y)-oy)
		case render.CubeTo:
			if !started {
				r.MoveTo(float32(s.P0.X)-ox, float32(s.P0.Y)-oy)
				started = true
			}
			r.CubeTo(
				float32(s.P0.X)-ox, float32(s.P0.Y)-oy,
				float32(s.P1.X)-ox, float32(s.P1.Y)-oy,
				float32(s.P2.X)-ox, float32(s.P2.Y)-oy,
			)
		case render.Close:
			r.ClosePath()
		}
	}
}

// flattenCubic subdivides a cubic Bézier from (x0,y0) through control points to
// emit line endpoints via emit. Fixed subdivision keeps it simple and
// deterministic; the count is enough for the small device-space curves here.
func flattenCubic(x0, y0 float64, p0, p1, p2 render.Point, emit func(x, y float64)) {
	const steps = 16
	for i := 1; i <= steps; i++ {
		t := float64(i) / steps
		mt := 1 - t
		// Cubic: start=(x0,y0), c1=p0, c2=p1, end=p2.
		b0 := mt * mt * mt
		b1 := 3 * mt * mt * t
		b2 := 3 * mt * t * t
		b3 := t * t * t
		x := b0*x0 + b1*p0.X + b2*p1.X + b3*p2.X
		y := b0*y0 + b1*p0.Y + b2*p1.Y + b3*p2.Y
		emit(x, y)
	}
}

// flattenToPolygons converts path into a set of closed polygons in device space,
// subdividing cubic Béziers to line segments. Each subpath becomes one polygon
// (implicitly closed). Used by the even-odd rasterizer, which needs explicit edge
// lists rather than a streaming rasterizer.
func flattenToPolygons(path *render.Path) [][]render.Point {
	var polys [][]render.Point
	var cur []render.Point
	var startPt, lastPt render.Point

	flush := func() {
		if len(cur) >= 2 {
			polys = append(polys, cur)
		}
		cur = nil
	}
	for _, s := range path.Segments {
		switch s.Kind {
		case render.MoveTo:
			flush()
			startPt, lastPt = s.P0, s.P0
			cur = []render.Point{s.P0}
		case render.LineTo:
			if cur == nil {
				startPt = s.P0
				cur = []render.Point{s.P0}
			} else {
				cur = append(cur, s.P0)
			}
			lastPt = s.P0
		case render.CubeTo:
			if cur == nil {
				cur = []render.Point{lastPt}
			}
			flattenCubic(lastPt.X, lastPt.Y, s.P0, s.P1, s.P2, func(x, y float64) {
				cur = append(cur, render.Point{X: x, Y: y})
			})
			lastPt = s.P2
		case render.Close:
			if cur != nil {
				cur = append(cur, startPt)
				lastPt = startPt
			}
		}
	}
	flush()
	return polys
}

// evenOddCoverage rasterizes the given polygons into bb-sized alpha coverage
// using the even-odd fill rule. It supersamples ss subscanlines per pixel row and
// computes fractional horizontal coverage at span edges, so edges are
// anti-aliased without any external rasterizer (golang.org/x/image/vector only
// offers nonzero winding). Returns nil for an empty bounding box.
func evenOddCoverage(polys [][]render.Point, bb image.Rectangle, ss int) *image.Alpha {
	if bb.Empty() {
		return nil
	}
	if ss < 1 {
		ss = 1
	}
	mask := image.NewAlpha(bb)
	w := bb.Dx()
	// cov accumulates fractional coverage (0..ss) per pixel for the current row.
	cov := make([]float64, w)
	xs := make([]float64, 0, 16)

	for py := bb.Min.Y; py < bb.Max.Y; py++ {
		for i := range cov {
			cov[i] = 0
		}
		for s := 0; s < ss; s++ {
			// Sample y at the center of each subscanline within this pixel row.
			sy := float64(py) + (float64(s)+0.5)/float64(ss)
			xs = xs[:0]
			// Collect x-intersections of all edges with the horizontal line y=sy.
			for _, poly := range polys {
				for i := 0; i+1 < len(poly); i++ {
					a, b := poly[i], poly[i+1]
					y0, y1 := a.Y, b.Y
					if y0 == y1 {
						continue // horizontal edge: no crossing
					}
					// Half-open rule [min,max) avoids double-counting shared vertices.
					if (sy >= y0 && sy < y1) || (sy >= y1 && sy < y0) {
						t := (sy - y0) / (y1 - y0)
						xs = append(xs, a.X+t*(b.X-a.X))
					}
				}
			}
			if len(xs) < 2 {
				continue
			}
			sortFloats(xs)
			// Even-odd: fill between consecutive crossing pairs.
			for i := 0; i+1 < len(xs); i += 2 {
				addSpan(cov, bb.Min.X, xs[i], xs[i+1])
			}
		}
		for i := 0; i < w; i++ {
			a := cov[i] / float64(ss)
			if a <= 0 {
				continue
			}
			if a > 1 {
				a = 1
			}
			mask.SetAlpha(bb.Min.X+i, py, color.Alpha{A: uint8(a*255 + 0.5)})
		}
	}
	return mask
}

// addSpan adds horizontal coverage for the span [x0,x1) to cov, where cov[i]
// corresponds to the pixel at device x = originX+i. Partially covered end pixels
// receive fractional coverage so vertical and near-vertical edges anti-alias.
func addSpan(cov []float64, originX int, x0, x1 float64) {
	if x1 <= x0 {
		return
	}
	lo := x0 - float64(originX)
	hi := x1 - float64(originX)
	if hi <= 0 || lo >= float64(len(cov)) {
		return
	}
	if lo < 0 {
		lo = 0
	}
	if hi > float64(len(cov)) {
		hi = float64(len(cov))
	}
	iLo := int(math.Floor(lo))
	iHi := int(math.Floor(hi))
	if iLo == iHi {
		cov[iLo] += hi - lo
		return
	}
	cov[iLo] += float64(iLo+1) - lo // partial first pixel
	for i := iLo + 1; i < iHi; i++ {
		cov[i] += 1
	}
	if iHi < len(cov) {
		cov[iHi] += hi - float64(iHi) // partial last pixel
	}
}

// sortFloats sorts a small slice of float64 ascending (insertion sort; crossing
// counts per scanline are tiny, so this beats sort.Float64s' overhead).
func sortFloats(a []float64) {
	for i := 1; i < len(a); i++ {
		v := a[i]
		j := i - 1
		for j >= 0 && a[j] > v {
			a[j+1] = a[j]
			j--
		}
		a[j+1] = v
	}
}

// invert returns the inverse of an affine matrix, ok=false if singular.
func invert(m render.Matrix) (render.Matrix, bool) {
	det := m.A*m.D - m.B*m.C
	if math.Abs(det) < 1e-12 {
		return render.Matrix{}, false
	}
	id := 1 / det
	return render.Matrix{
		A: m.D * id,
		B: -m.B * id,
		C: -m.C * id,
		D: m.A * id,
		E: (m.C*m.F - m.D*m.E) * id,
		F: (m.B*m.E - m.A*m.F) * id,
	}, true
}

// unitQuadBounds returns the device-space bounding box of the unit square's four
// corners transformed by m.
func unitQuadBounds(m render.Matrix) (minX, minY, maxX, maxY float64) {
	xs := make([]float64, 0, 4)
	ys := make([]float64, 0, 4)
	for _, c := range [4][2]float64{{0, 0}, {1, 0}, {0, 1}, {1, 1}} {
		x, y := m.Apply(c[0], c[1])
		xs = append(xs, x)
		ys = append(ys, y)
	}
	minX, maxX = xs[0], xs[0]
	minY, maxY = ys[0], ys[0]
	for i := 1; i < 4; i++ {
		minX = math.Min(minX, xs[i])
		maxX = math.Max(maxX, xs[i])
		minY = math.Min(minY, ys[i])
		maxY = math.Max(maxY, ys[i])
	}
	return
}

// pathDeviceBounds returns the integer pixel rectangle enclosing path's control
// points (a conservative superset of the rendered area; control points bound the
// curve hull). It returns the empty rectangle for an empty path.
func pathDeviceBounds(path *render.Path) image.Rectangle {
	first := true
	var minX, minY, maxX, maxY float64
	consider := func(p render.Point) {
		if first {
			minX, minY, maxX, maxY = p.X, p.Y, p.X, p.Y
			first = false
			return
		}
		minX = math.Min(minX, p.X)
		minY = math.Min(minY, p.Y)
		maxX = math.Max(maxX, p.X)
		maxY = math.Max(maxY, p.Y)
	}
	for _, s := range path.Segments {
		switch s.Kind {
		case render.MoveTo, render.LineTo:
			consider(s.P0)
		case render.CubeTo:
			consider(s.P0)
			consider(s.P1)
			consider(s.P2)
		case render.Close:
		}
	}
	if first {
		return image.Rectangle{}
	}
	// Expand by one pixel each side to cover anti-aliased edges.
	return image.Rect(
		int(math.Floor(minX))-1, int(math.Floor(minY))-1,
		int(math.Ceil(maxX))+1, int(math.Ceil(maxY))+1,
	)
}

// intersectClips returns a new alpha mask whose coverage is the product of a and
// b, bounded to the intersection of their rectangles. Coverage outside either
// input is zero, so the intersection rectangle is the only region that can be
// non-zero.
func intersectClips(a, b *image.Alpha) *image.Alpha {
	r := a.Bounds().Intersect(b.Bounds())
	out := image.NewAlpha(r)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			out.SetAlpha(x, y, color.Alpha{A: mulU8(a.AlphaAt(x, y).A, b.AlphaAt(x, y).A)})
		}
	}
	return out
}
