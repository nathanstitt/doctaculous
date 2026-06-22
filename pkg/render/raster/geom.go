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
