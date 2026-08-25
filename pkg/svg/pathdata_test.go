package svg

import (
	"math"
	"reflect"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/render"
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
	// circle x^2+y^2=100. Endpoint must be exact; midpoints within 0.1.
	p = parsePathData("M 10 0 A 10 10 0 0 1 0 10")
	last := p.Segments[len(p.Segments)-1]
	if last.Kind != render.CubeTo || math.Abs(last.P2.X-0) > 1e-9 || math.Abs(last.P2.Y-10) > 1e-9 {
		t.Errorf("arc end = %+v", last)
	}
	for _, s := range p.Segments[1:] {
		for _, pt := range []render.Point{s.P0, s.P1, s.P2} {
			if r := math.Hypot(pt.X, pt.Y); math.Abs(r-10) > 0.6 {
				t.Errorf("arc control point %v far off circle (r=%g)", pt, r)
			}
		}
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
