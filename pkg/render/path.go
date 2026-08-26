package render

import "math"

// Point is a 2-D point in device space (pixels), origin at top-left, y down.
type Point struct {
	X, Y float64
}

// SegmentKind identifies a path segment type.
type SegmentKind int

const (
	// MoveTo starts a new subpath at P0.
	MoveTo SegmentKind = iota
	// LineTo draws a straight line to P0.
	LineTo
	// CubeTo draws a cubic Bézier with control points P0, P1 and endpoint P2.
	CubeTo
	// Close closes the current subpath.
	Close
)

// Segment is one element of a path. Which Point fields are meaningful depends on
// Kind: MoveTo/LineTo use P0; CubeTo uses P0,P1,P2; Close uses none.
type Segment struct {
	Kind       SegmentKind
	P0, P1, P2 Point
}

// Path is a sequence of segments already transformed into device space.
type Path struct {
	Segments []Segment
}

// MoveTo appends a move-to segment.
func (p *Path) MoveTo(x, y float64) {
	p.Segments = append(p.Segments, Segment{Kind: MoveTo, P0: Point{x, y}})
}

// LineTo appends a line-to segment.
func (p *Path) LineTo(x, y float64) {
	p.Segments = append(p.Segments, Segment{Kind: LineTo, P0: Point{x, y}})
}

// CubeTo appends a cubic Bézier segment.
func (p *Path) CubeTo(x0, y0, x1, y1, x2, y2 float64) {
	p.Segments = append(p.Segments, Segment{
		Kind: CubeTo,
		P0:   Point{x0, y0}, P1: Point{x1, y1}, P2: Point{x2, y2},
	})
}

// Close appends a close-subpath segment.
func (p *Path) Close() {
	p.Segments = append(p.Segments, Segment{Kind: Close})
}

// Empty reports whether the path has no segments.
func (p *Path) Empty() bool { return len(p.Segments) == 0 }

// Reset clears the path for reuse, retaining capacity.
func (p *Path) Reset() { p.Segments = p.Segments[:0] }

// Clone returns a deep copy of the path.
func (p *Path) Clone() *Path {
	if p == nil {
		return nil
	}
	segs := make([]Segment, len(p.Segments))
	copy(segs, p.Segments)
	return &Path{Segments: segs}
}

// Bounds returns the tight axis-aligned bounding box of the path's geometry: the
// true extent of the drawn lines and curves, not the control-point hull. For a
// CubeTo this solves each axis's derivative (a quadratic in t) for roots in
// (0,1) and includes the curve position at those roots, so a curve whose control
// points overshoot the curve itself (e.g. a circle built from four cubics with
// kappa ≈ 0.5523) still yields the true curve bounds.
//
// This is the SVG objectBoundingBox definition: geometry only, excluding stroke
// width, markers, filter regions, and clipping. Callers needing a stroked or
// clipped extent must expand this box themselves.
//
// ok is false for a path with no segments, or one whose only segments are
// MoveTo (no drawable extent — nothing is ever painted).
func (p *Path) Bounds() (minX, minY, maxX, maxY float64, ok bool) {
	if p == nil {
		return 0, 0, 0, 0, false
	}

	first := true
	consider := func(pt Point) {
		if math.IsNaN(pt.X) || math.IsNaN(pt.Y) || math.IsInf(pt.X, 0) || math.IsInf(pt.Y, 0) {
			return
		}
		if first {
			minX, minY, maxX, maxY = pt.X, pt.Y, pt.X, pt.Y
			first = false
			return
		}
		minX = math.Min(minX, pt.X)
		minY = math.Min(minY, pt.Y)
		maxX = math.Max(maxX, pt.X)
		maxY = math.Max(maxY, pt.Y)
	}

	var cur Point  // current pen position (subpath cursor)
	drawn := false // whether any LineTo/CubeTo has been seen (drawable extent exists)
	for _, s := range p.Segments {
		switch s.Kind {
		case MoveTo:
			cur = s.P0
		case LineTo:
			consider(cur)
			consider(s.P0)
			cur = s.P0
			drawn = true
		case CubeTo:
			consider(cur)
			consider(s.P2)
			cubicAxisExtrema(cur.X, s.P0.X, s.P1.X, s.P2.X, func(t float64) {
				consider(Point{X: cubicEval(cur.X, s.P0.X, s.P1.X, s.P2.X, t), Y: cubicEval(cur.Y, s.P0.Y, s.P1.Y, s.P2.Y, t)})
			})
			cubicAxisExtrema(cur.Y, s.P0.Y, s.P1.Y, s.P2.Y, func(t float64) {
				consider(Point{X: cubicEval(cur.X, s.P0.X, s.P1.X, s.P2.X, t), Y: cubicEval(cur.Y, s.P0.Y, s.P1.Y, s.P2.Y, t)})
			})
			cur = s.P2
			drawn = true
		case Close:
			// No new geometry beyond the implicit line back to the subpath start,
			// which is already bounded by the start/end points already considered.
		}
	}

	if !drawn || first {
		return 0, 0, 0, 0, false
	}
	return minX, minY, maxX, maxY, true
}

// cubicEval evaluates one axis of a cubic Bézier at parameter t using Horner's
// form of the Bernstein polynomial.
func cubicEval(p0, p1, p2, p3, t float64) float64 {
	mt := 1 - t
	return mt*mt*mt*p0 + 3*mt*mt*t*p1 + 3*mt*t*t*p2 + t*t*t*p3
}

// cubicAxisExtrema calls fn(t) for each root t in (0,1) of the derivative of the
// cubic Bézier defined by p0,p1,p2,p3 along one axis. The derivative of a cubic
// Bézier is a quadratic in t:
//
//	B'(t)/3 = a*t^2 + b*t + c
//	a = -p0 + 3p1 - 3p2 + p3
//	b = 2*(p0 - 2p1 + p2)
//	c = p1 - p0
//
// Degenerate cases are handled: a ~= 0 reduces to the linear equation b*t+c=0
// (at most one root); a negative discriminant yields no real roots.
func cubicAxisExtrema(p0, p1, p2, p3 float64, fn func(t float64)) {
	a := -p0 + 3*p1 - 3*p2 + p3
	b := 2 * (p0 - 2*p1 + p2)
	c := p1 - p0

	const epsilon = 1e-12
	if math.Abs(a) < epsilon {
		// Linear: b*t + c = 0.
		if math.Abs(b) < epsilon {
			return
		}
		t := -c / b
		if t > 0 && t < 1 {
			fn(t)
		}
		return
	}

	disc := b*b - 4*a*c
	if disc < 0 {
		return
	}
	sqrtDisc := math.Sqrt(disc)
	t1 := (-b + sqrtDisc) / (2 * a)
	t2 := (-b - sqrtDisc) / (2 * a)
	if t1 > 0 && t1 < 1 {
		fn(t1)
	}
	if t2 > 0 && t2 < 1 {
		fn(t2)
	}
}

// TransformPath returns a copy of p with every point mapped through m. It is used
// by backends that need a path in a different coordinate space (e.g. a glyph
// outline moved into device space). It returns nil for a nil p.
func TransformPath(p *Path, m Matrix) *Path {
	if p == nil {
		return nil
	}
	ap := func(pt Point) Point {
		x, y := m.Apply(pt.X, pt.Y)
		return Point{X: x, Y: y}
	}
	out := &Path{Segments: make([]Segment, len(p.Segments))}
	for i, s := range p.Segments {
		ns := Segment{Kind: s.Kind}
		switch s.Kind {
		case MoveTo, LineTo:
			ns.P0 = ap(s.P0)
		case CubeTo:
			ns.P0 = ap(s.P0)
			ns.P1 = ap(s.P1)
			ns.P2 = ap(s.P2)
		case Close:
			// no points
		}
		out.Segments[i] = ns
	}
	return out
}
