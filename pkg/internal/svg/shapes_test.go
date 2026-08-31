package svg

import (
	"math"
	"testing"

	"github.com/nathanstitt/omnidoc/pkg/internal/render"
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

// TestShapePathEllipseAutoRadius covers SVG 2 §10.4's <ellipse> rx/ry
// auto-defaulting: when exactly one of rx/ry is present, the missing one
// takes the other's value (an ellipse degenerates to what a circle would be
// for that radius). Missing both is still degenerate (SVG 1 behavior): with
// no radius to default from, both resolve to 0 and the shape drops. rectPath
// already implements the equivalent rule for <rect>'s rx/ry (see its own
// test above); this exercises the same rule on ellipseRadii.
func TestShapePathEllipseAutoRadius(t *testing.T) {
	el := func(name string, attrs map[string]string) *element {
		return &element{space: svgNS, local: name, attrs: attrs}
	}
	// rx present, ry absent: ry defaults to rx.
	p := shapePath(el("ellipse", map[string]string{"cx": "100", "cy": "100", "rx": "60"}), nil)
	if p == nil || p.Segments[0].P0 != (render.Point{X: 160, Y: 100}) {
		t.Fatalf("ellipse missing ry = %+v", p)
	}
	for _, s := range p.Segments {
		if s.Kind == render.CubeTo {
			if r := math.Hypot(s.P2.X-100, s.P2.Y-100); math.Abs(r-60) > 1e-9 {
				t.Errorf("ellipse missing ry: endpoint off circle of r=60: %+v", s.P2)
			}
		}
	}
	// ry present, rx absent: rx defaults to ry.
	p = shapePath(el("ellipse", map[string]string{"cx": "100", "cy": "100", "ry": "60"}), nil)
	if p == nil || p.Segments[0].P0 != (render.Point{X: 160, Y: 100}) {
		t.Fatalf("ellipse missing rx = %+v", p)
	}
	// Both absent: 0/0, degenerate, dropped (SVG 1 behavior for this case).
	if p := shapePath(el("ellipse", map[string]string{"cx": "100", "cy": "100"}), nil); p != nil {
		t.Errorf("ellipse with neither rx nor ry should be nil, got %+v", p)
	}
	// Explicit rx="0" is NOT "absent": it must not trigger the ry substitution.
	if p := shapePath(el("ellipse", map[string]string{"cx": "100", "cy": "100", "rx": "0", "ry": "60"}), nil); p != nil {
		t.Errorf("ellipse with explicit rx=0 should stay degenerate, got %+v", p)
	}
}

// assertFinitePath fails t if any coordinate in p's segments is NaN or ±Inf.
// A non-finite coordinate would poison downstream bounding-box, transform,
// and rasterization math just as badly as a non-finite radius, so every
// shapePath guard is expected to keep them out of the emitted path entirely.
func assertFinitePath(t *testing.T, p *render.Path) {
	t.Helper()
	if p == nil {
		return
	}
	check := func(label string, pt render.Point) {
		if math.IsNaN(pt.X) || math.IsInf(pt.X, 0) || math.IsNaN(pt.Y) || math.IsInf(pt.Y, 0) {
			t.Errorf("non-finite coordinate in %s: %+v", label, pt)
		}
	}
	for _, s := range p.Segments {
		switch s.Kind {
		case render.MoveTo, render.LineTo:
			check("P0", s.P0)
		case render.CubeTo:
			check("P0", s.P0)
			check("P1", s.P1)
			check("P2", s.P2)
		}
	}
}

// TestShapePathNonFiniteGuards covers the position/center/point coordinates
// that a "nan"/"inf" attribute value can reach. parseNumber/parseLength
// reject those literals outright (SVG's <number> grammar has no NaN/Infinity
// spelling), so shapePath's length() helper surfaces a NaN sentinel for a
// present-but-unparseable attribute instead of silently defaulting to 0;
// every coordinate that can reach a path — rect x/y (in addition to the
// already-guarded width/height/rx/ry), circle/ellipse cx/cy (in addition to
// r/rx/ry), line x1/y1/x2/y2, and polygon/polyline points — must still
// reject that sentinel rather than let it through: a non-finite center or
// point poisons downstream bounding-box, transform, and rasterization math
// exactly like a non-finite dimension does.
func TestShapePathNonFiniteGuards(t *testing.T) {
	el := func(name string, attrs map[string]string) *element {
		return &element{space: svgNS, local: name, attrs: attrs}
	}
	var logged []string
	logf := func(format string, args ...any) { logged = append(logged, format) }
	reset := func() { logged = nil }

	// rect: non-finite x/y drops the shape (position is undrawable).
	reset()
	if p := shapePath(el("rect", map[string]string{"x": "inf", "width": "10", "height": "5"}), logf); p != nil {
		t.Errorf("rect x=inf should be nil, got %+v", p)
	}
	if len(logged) == 0 {
		t.Error("rect x=inf should log")
	}
	reset()
	if p := shapePath(el("rect", map[string]string{"y": "nan", "width": "10", "height": "5"}), logf); p != nil {
		t.Errorf("rect y=nan should be nil, got %+v", p)
	}
	if len(logged) == 0 {
		t.Error("rect y=nan should log")
	}

	// circle: non-finite cx/cy drops the shape even with a valid r.
	reset()
	if p := shapePath(el("circle", map[string]string{"cx": "nan", "r": "5"}), logf); p != nil {
		t.Errorf("circle cx=nan should be nil, got %+v", p)
	}
	if len(logged) == 0 {
		t.Error("circle cx=nan should log")
	}
	reset()
	if p := shapePath(el("circle", map[string]string{"cx": "inf", "r": "5"}), logf); p != nil {
		t.Errorf("circle cx=inf should be nil, got %+v", p)
	}

	// ellipse: same guard as circle.
	reset()
	if p := shapePath(el("ellipse", map[string]string{"cx": "nan", "rx": "5", "ry": "3"}), logf); p != nil {
		t.Errorf("ellipse cx=nan should be nil, got %+v", p)
	}
	if len(logged) == 0 {
		t.Error("ellipse cx=nan should log")
	}

	// line: non-finite endpoint on either end drops the shape; zero
	// guards previously.
	reset()
	if p := shapePath(el("line", map[string]string{"x1": "nan", "y1": "0", "x2": "3", "y2": "4"}), logf); p != nil {
		t.Errorf("line x1=nan should be nil, got %+v", p)
	}
	if len(logged) == 0 {
		t.Error("line x1=nan should log")
	}
	reset()
	if p := shapePath(el("line", map[string]string{"x1": "0", "y1": "0", "x2": "inf", "y2": "4"}), logf); p != nil {
		t.Errorf("line x2=inf should be nil, got %+v", p)
	}

	// polygon/polyline: a non-finite token in points truncates to the
	// valid prefix rather than dropping the whole shape (consistent with
	// the odd-trailing-number rule and parsePathData's error handling).
	reset()
	p := shapePath(el("polyline", map[string]string{"points": "0,0 10,0 nan,10"}), logf)
	if p == nil || len(p.Segments) != 2 {
		t.Errorf("polyline with nan token = %+v", p)
	}
	assertFinitePath(t, p)
	if len(logged) == 0 {
		t.Error("polyline nan token should log")
	}
	reset()
	p = shapePath(el("polygon", map[string]string{"points": "0,0 10,0 10,inf 5,5"}), logf)
	if p == nil || len(p.Segments) != 3 { // M(0,0) L(10,0) Close
		t.Errorf("polygon with inf token = %+v", p)
	}
	assertFinitePath(t, p)

	// A non-finite x coordinate (rather than y) in a pair truncates the
	// same pair.
	reset()
	p = shapePath(el("polyline", map[string]string{"points": "0,0 inf,0 10,10"}), logf)
	if p == nil || len(p.Segments) != 1 { // only the initial M(0,0) survives
		t.Errorf("polyline with inf x = %+v", p)
	}
	assertFinitePath(t, p)

	// Sweep every shape kind's happy path through the finite-path
	// assertion too, so a future regression that reintroduces a leak
	// anywhere is caught even without a dedicated non-finite input.
	for _, tc := range []*element{
		el("rect", map[string]string{"x": "1", "y": "2", "width": "10", "height": "5", "rx": "2"}),
		el("circle", map[string]string{"cx": "10", "cy": "10", "r": "5"}),
		el("ellipse", map[string]string{"cx": "10", "cy": "20", "rx": "7", "ry": "3"}),
		el("line", map[string]string{"x1": "0", "y1": "0", "x2": "3", "y2": "4"}),
		el("polygon", map[string]string{"points": "0,0 10,0 10,10"}),
	} {
		assertFinitePath(t, shapePath(tc, nil))
	}
}
