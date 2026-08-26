package render

import (
	"math"
	"testing"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestIdentityApply(t *testing.T) {
	x, y := Identity.Apply(3, 4)
	if !approx(x, 3) || !approx(y, 4) {
		t.Errorf("Identity.Apply(3,4) = (%v,%v), want (3,4)", x, y)
	}
}

func TestTranslateApply(t *testing.T) {
	x, y := Translate(10, 20).Apply(1, 2)
	if !approx(x, 11) || !approx(y, 22) {
		t.Errorf("Translate.Apply = (%v,%v), want (11,22)", x, y)
	}
}

func TestMulOrder(t *testing.T) {
	// Apply scale-then-translate: a point (1,1) scaled by 2 -> (2,2), then
	// translated by (5,0) -> (7,2). Mul(scale, translate) must do scale first.
	m := Scale(2, 2).Mul(Translate(5, 0))
	x, y := m.Apply(1, 1)
	if !approx(x, 7) || !approx(y, 2) {
		t.Errorf("Mul order = (%v,%v), want (7,2)", x, y)
	}
}

func TestApplyVectorIgnoresTranslation(t *testing.T) {
	m := Translate(100, 100).Mul(Scale(2, 3))
	dx, dy := m.ApplyVector(1, 1)
	if !approx(dx, 2) || !approx(dy, 3) {
		t.Errorf("ApplyVector = (%v,%v), want (2,3)", dx, dy)
	}
}

func TestScaleFactor(t *testing.T) {
	if got := Scale(2, 8).ScaleFactor(); !approx(got, 4) {
		t.Errorf("ScaleFactor = %v, want 4 (sqrt(2*8))", got)
	}
}

func TestRotateSkew(t *testing.T) {
	m := Rotate(math.Pi / 2)
	x, y := m.Apply(1, 0)
	if math.Abs(x-0) > 1e-12 || math.Abs(y-1) > 1e-12 {
		t.Errorf("Rotate(pi/2)(1,0) = (%g,%g), want (0,1)", x, y)
	}
	s := Skew(math.Tan(math.Pi/4), 0) // skewX(45deg)
	x, y = s.Apply(0, 1)
	if math.Abs(x-1) > 1e-12 || math.Abs(y-1) > 1e-12 {
		t.Errorf("SkewX(45)(0,1) = (%g,%g), want (1,1)", x, y)
	}
	s = Skew(0, math.Tan(math.Pi/4)) // skewY(45deg)
	x, y = s.Apply(1, 0)
	if math.Abs(x-1) > 1e-12 || math.Abs(y-1) > 1e-12 {
		t.Errorf("SkewY(45)(1,0) = (%g,%g), want (1,1)", x, y)
	}
}

func TestInvertRoundTrips(t *testing.T) {
	m := Translate(10, -5).Mul(Scale(2, 3)).Mul(Rotate(0.7))
	inv, ok := m.Invert()
	if !ok {
		t.Fatal("Invert reported not-invertible for a non-degenerate matrix")
	}
	x, y := m.Apply(3, 4)
	bx, by := inv.Apply(x, y)
	if !approx(bx, 3) || !approx(by, 4) {
		t.Errorf("Invert round trip = (%v,%v), want (3,4)", bx, by)
	}
}

func TestInvertSingularReportsNotOK(t *testing.T) {
	if _, ok := Scale(0, 1).Invert(); ok {
		t.Error("Invert of a zero-scale matrix reported ok=true, want false")
	}
}
