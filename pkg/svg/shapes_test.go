package svg

import (
	"math"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/render"
)

func TestShapePath(t *testing.T) {
	el := func(name string, attrs map[string]string) *element {
		return &element{space: svgNS, local: name, attrs: attrs}
	}
	// Plain rect: M,L,L,L,Z.
	p := shapePath(el("rect", map[string]string{"x": "1", "y": "2", "width": "10", "height": "5"}), nil)
	if p == nil || len(p.Segments) != 5 || p.Segments[0].P0 != (render.Point{X: 1, Y: 2}) {
		t.Fatalf("rect = %+v", p)
	}
	if p.Segments[2].P0 != (render.Point{X: 11, Y: 7}) {
		t.Errorf("rect corner = %+v", p.Segments[2].P0)
	}
	// Zero-size rect: nil (not rendered).
	if p := shapePath(el("rect", map[string]string{"width": "0", "height": "5"}), nil); p != nil {
		t.Error("zero rect rendered")
	}
	// rx clamps to width/2; ry mirrors rx when absent.
	p = shapePath(el("rect", map[string]string{"width": "10", "height": "10", "rx": "20"}), nil)
	if p == nil || p.Segments[0].P0 != (render.Point{X: 5, Y: 0}) {
		t.Fatalf("rounded rect start = %+v", p.Segments[0])
	}
	// Circle r=5 at (10,10): starts at (15,10), stays on the circle.
	p = shapePath(el("circle", map[string]string{"cx": "10", "cy": "10", "r": "5"}), nil)
	if p == nil || p.Segments[0].P0 != (render.Point{X: 15, Y: 10}) {
		t.Fatalf("circle = %+v", p.Segments[:1])
	}
	for _, s := range p.Segments {
		if s.Kind == render.CubeTo {
			if r := math.Hypot(s.P2.X-10, s.P2.Y-10); math.Abs(r-5) > 1e-9 {
				t.Errorf("circle endpoint off circle: %+v", s.P2)
			}
		}
	}
	// line / polyline open, polygon closed.
	p = shapePath(el("line", map[string]string{"x1": "0", "y1": "0", "x2": "3", "y2": "4"}), nil)
	if len(p.Segments) != 2 || p.Segments[1].Kind != render.LineTo {
		t.Errorf("line = %+v", p.Segments)
	}
	p = shapePath(el("polygon", map[string]string{"points": "0,0 10,0 10,10"}), nil)
	if p.Segments[len(p.Segments)-1].Kind != render.Close {
		t.Error("polygon not closed")
	}
	p = shapePath(el("polyline", map[string]string{"points": "0,0 10,0 10,10"}), nil)
	if p.Segments[len(p.Segments)-1].Kind == render.Close {
		t.Error("polyline closed")
	}
	// Odd points list: trailing number dropped, prefix rendered (SVG error rule).
	p = shapePath(el("polyline", map[string]string{"points": "0,0 10,0 10"}), nil)
	if p == nil || len(p.Segments) != 2 {
		t.Errorf("odd points = %+v", p)
	}
	// path element delegates to parsePathData.
	p = shapePath(el("path", map[string]string{"d": "M 0 0 L 1 1"}), nil)
	if len(p.Segments) != 2 {
		t.Errorf("path = %+v", p)
	}
}
