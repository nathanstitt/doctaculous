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
