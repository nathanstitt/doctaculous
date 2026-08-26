package render

import (
	"math"
	"testing"
)

func TestPathBoundsEmpty(t *testing.T) {
	p := &Path{}
	_, _, _, _, ok := p.Bounds()
	if ok {
		t.Fatalf("empty path: expected ok=false")
	}
}

func TestPathBoundsMoveToOnly(t *testing.T) {
	p := &Path{}
	p.MoveTo(5, 5)
	_, _, _, _, ok := p.Bounds()
	if ok {
		t.Fatalf("MoveTo-only path: expected ok=false")
	}
}

func TestPathBoundsRectExact(t *testing.T) {
	p := &Path{}
	p.MoveTo(10, 20)
	p.LineTo(110, 20)
	p.LineTo(110, 220)
	p.LineTo(10, 220)
	p.Close()

	minX, minY, maxX, maxY, ok := p.Bounds()
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if minX != 10 || minY != 20 || maxX != 110 || maxY != 220 {
		t.Fatalf("got bounds (%v,%v,%v,%v), want (10,20,110,220)", minX, minY, maxX, maxY)
	}
}

// TestPathBoundsCubicBulge uses a cubic whose control points lie far outside the
// actual curve extent. The control hull would be a poor bbox; Bounds must report
// the true curve extrema, which are tighter than the hull.
func TestPathBoundsCubicBulge(t *testing.T) {
	p := &Path{}
	// Start and end both at y=0; control points pulled far up in y and wide in x,
	// but the true curve only bulges modestly.
	p.MoveTo(0, 0)
	p.CubeTo(0, -1000, 100, -1000, 100, 0)

	minX, minY, maxX, maxY, ok := p.Bounds()
	if !ok {
		t.Fatalf("expected ok=true")
	}
	// Control hull would give minY = -1000. True curve extremum at t=0.5:
	// B(0.5) y = -1000 * 3*0.5*0.5*0.5 * 2 = ... compute via De Casteljau below.
	// Just assert it's tighter than the hull and matches known cubic evaluation.
	if minY <= -1000 {
		t.Fatalf("bounds not tighter than control hull: minY=%v", minY)
	}
	// Analytic: y(t) = (1-t)^3*0 + 3(1-t)^2 t*(-1000) + 3(1-t) t^2*(-1000) + t^3*0
	// = -3000 t (1-t) [(1-t) + t] ... actually simplify directly at t=0.5.
	wantY := cubicAxis(0, -1000, -1000, 0, 0.5)
	if math.Abs(minY-wantY) > 1e-9 {
		t.Fatalf("minY = %v, want %v", minY, wantY)
	}
	if minX != 0 || maxX != 100 {
		t.Fatalf("x bounds should be exact endpoints: got minX=%v maxX=%v", minX, maxX)
	}
	if maxY != 0 {
		t.Fatalf("maxY should be endpoint value 0, got %v", maxY)
	}
}

// cubicAxis evaluates one axis of a cubic Bezier at parameter t. Test helper only.
func cubicAxis(p0, p1, p2, p3, t float64) float64 {
	mt := 1 - t
	return mt*mt*mt*p0 + 3*mt*mt*t*p1 + 3*mt*t*t*p2 + t*t*t*p3
}

// TestPathBoundsCircleFromCubics approximates a full unit circle with four cubic
// Beziers (the standard kappa=0.5522847498 construction) and checks the bounds
// come out within 1e-9 of the true circle, not the ~10% larger control hull.
func TestPathBoundsCircleFromCubics(t *testing.T) {
	const k = 0.5522847498307936
	const r = 100.0
	const cx, cy = 500.0, 500.0

	p := &Path{}
	p.MoveTo(cx+r, cy)
	// Quadrant 1: (r,0) -> (0,r)
	p.CubeTo(cx+r, cy+r*k, cx+r*k, cy+r, cx, cy+r)
	// Quadrant 2: (0,r) -> (-r,0)
	p.CubeTo(cx-r*k, cy+r, cx-r, cy+r*k, cx-r, cy)
	// Quadrant 3: (-r,0) -> (0,-r)
	p.CubeTo(cx-r, cy-r*k, cx-r*k, cy-r, cx, cy-r)
	// Quadrant 4: (0,-r) -> (r,0)
	p.CubeTo(cx+r*k, cy-r, cx+r, cy-r*k, cx+r, cy)
	p.Close()

	minX, minY, maxX, maxY, ok := p.Bounds()
	if !ok {
		t.Fatalf("expected ok=true")
	}
	const tol = 1e-9
	if math.Abs(minX-(cx-r)) > tol || math.Abs(maxX-(cx+r)) > tol ||
		math.Abs(minY-(cy-r)) > tol || math.Abs(maxY-(cy+r)) > tol {
		t.Fatalf("circle bounds = (%v,%v,%v,%v), want (%v,%v,%v,%v) within %v",
			minX, minY, maxX, maxY, cx-r, cy-r, cx+r, cy+r, tol)
	}
}
