package svg

import (
	"math"
	"reflect"
	"testing"

	"github.com/nathanstitt/omnidoc/pkg/render"
)

func TestParsePathData(t *testing.T) {
	// Absolute + relative + implicit lineto repetition.
	p := parsePathData("M 10 10 L 20 10 30 10 l 0 10 Z")
	want := []render.Segment{
		{Kind: render.MoveTo, P0: render.Point{X: 10, Y: 10}},
		{Kind: render.LineTo, P0: render.Point{X: 20, Y: 10}},
		{Kind: render.LineTo, P0: render.Point{X: 30, Y: 10}},
		{Kind: render.LineTo, P0: render.Point{X: 30, Y: 20}},
		{Kind: render.Close},
	}
	if !reflect.DeepEqual(p.Segments, want) {
		t.Errorf("segments = %+v\nwant %+v", p.Segments, want)
	}

	// H/V, compact numbers, no separators.
	p = parsePathData("M.5.5H2V3")
	want = []render.Segment{
		{Kind: render.MoveTo, P0: render.Point{X: 0.5, Y: 0.5}},
		{Kind: render.LineTo, P0: render.Point{X: 2, Y: 0.5}},
		{Kind: render.LineTo, P0: render.Point{X: 2, Y: 3}},
	}
	if !reflect.DeepEqual(p.Segments, want) {
		t.Errorf("compact = %+v\nwant %+v", p.Segments, want)
	}

	// Quadratic elevates exactly: Q(10,0 -> ctrl 10,10 -> 20,10) from M 0 0? Use known values.
	p = parsePathData("M 0 0 Q 15 30 30 0")
	if len(p.Segments) != 2 || p.Segments[1].Kind != render.CubeTo {
		t.Fatalf("quad = %+v", p.Segments)
	}
	c := p.Segments[1]
	// C1 = P0 + 2/3(Q-P0) = (10,20); C2 = P2 + 2/3(Q-P2) = (20,20).
	if c.P0 != (render.Point{X: 10, Y: 20}) || c.P1 != (render.Point{X: 20, Y: 20}) || c.P2 != (render.Point{X: 30, Y: 0}) {
		t.Errorf("quad ctrl = %+v", c)
	}

	// T reflects the previous quad control.
	p = parsePathData("M 0 0 Q 15 30 30 0 T 60 0")
	if len(p.Segments) != 3 {
		t.Fatalf("T = %+v", p.Segments)
	}
	// Reflected quad ctrl = 2*(30,0)-(15,30) = (45,-30); elevated C1 = (30,0)+2/3*(15,-30) = (40,-20).
	if got := p.Segments[2].P0; got != (render.Point{X: 40, Y: -20}) {
		t.Errorf("T reflected C1 = %+v", got)
	}

	// Arc: quarter circle radius 10 from (10,0) to (0,10), sweep=1 stays on the
	// circle x^2+y^2=100. Endpoint must be exact; the curve itself must hug the
	// circle tightly.
	p = parsePathData("M 10 0 A 10 10 0 0 1 0 10")
	last := p.Segments[len(p.Segments)-1]
	if last.Kind != render.CubeTo || math.Abs(last.P2.X-0) > 1e-9 || math.Abs(last.P2.Y-10) > 1e-9 {
		t.Errorf("arc end = %+v", last)
	}
	// Sample points ON the curve (not the Bézier control points) against the
	// circle. Control points are handles, not points on the curve: only t=0
	// and t=1 lie on the target arc by construction, and for a 90°-per-cubic
	// slice a control point legitimately sits ~14% of the radius off the
	// circle (r*sec(22.5°) ≈ 11.42 for r=10) — that is correct cubic-Bézier
	// arc math, not an error, and every conforming implementation (browsers,
	// Skia, Cairo, Batik) produces the same offset. Sampling the curve itself
	// is what "stays on the circle" actually means; do not revert this to
	// checking P0/P1/P2 against the circle.
	cur := render.Point{X: 10, Y: 0}
	for _, s := range p.Segments[1:] {
		if s.Kind != render.CubeTo {
			continue
		}
		for _, tt := range []float64{0, 0.25, 0.5, 0.75, 1} {
			pt := cubicAt(cur, s.P0, s.P1, s.P2, tt)
			if r := math.Hypot(pt.X, pt.Y); math.Abs(r-10) > 0.05 {
				t.Errorf("arc curve point at t=%g %+v far off circle (r=%g)", tt, pt, r)
			}
		}
		cur = s.P2
	}

	// Error recovery: bad token mid-stream keeps the prefix.
	p = parsePathData("M 0 0 L 10 10 L x")
	if len(p.Segments) != 2 {
		t.Errorf("recovery = %+v", p.Segments)
	}
	// Degenerate/empty input: empty path, not nil.
	if p := parsePathData(""); p == nil || len(p.Segments) != 0 {
		t.Errorf("empty = %v", p)
	}
}

// cubicAt evaluates the cubic Bézier with start p0, controls c1/c2, and
// endpoint p3 at parameter t using the standard formula
// B(t) = (1-t)^3*p0 + 3(1-t)^2*t*c1 + 3(1-t)*t^2*c2 + t^3*p3.
func cubicAt(p0, c1, c2, p3 render.Point, t float64) render.Point {
	mt := 1 - t
	a := mt * mt * mt
	b := 3 * mt * mt * t
	c := 3 * mt * t * t
	d := t * t * t
	return render.Point{
		X: a*p0.X + b*c1.X + c*c2.X + d*p3.X,
		Y: a*p0.Y + b*c1.Y + c*c2.Y + d*p3.Y,
	}
}
