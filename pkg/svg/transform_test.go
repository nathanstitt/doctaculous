package svg

import (
	"math"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/render"
)

func TestParseTransform(t *testing.T) {
	apply := func(s string, x, y float64) (float64, float64) {
		m, ok := parseTransform(s)
		if !ok {
			t.Fatalf("parseTransform(%q) failed", s)
		}
		return m.Apply(x, y)
	}
	// translate then scale: scale applies to the point first.
	if x, y := apply("translate(10,0) scale(2)", 1, 0); x != 12 || y != 0 {
		t.Errorf("translate·scale (1,0) = (%g,%g), want (12,0)", x, y)
	}
	// scale(sx) single-arg means uniform; translate(tx) means ty=0.
	if x, y := apply("scale(3)", 1, 1); x != 3 || y != 3 {
		t.Errorf("scale(3) = (%g,%g)", x, y)
	}
	if x, y := apply("translate(5)", 0, 0); x != 5 || y != 0 {
		t.Errorf("translate(5) = (%g,%g)", x, y)
	}
	// rotate(90) about origin, degrees.
	x, y := apply("rotate(90)", 1, 0)
	if math.Abs(x) > 1e-12 || math.Abs(y-1) > 1e-12 {
		t.Errorf("rotate(90)(1,0) = (%g,%g), want (0,1)", x, y)
	}
	// rotate about a center: rotate(90, 5, 5) maps (5,0) -> (10,5).
	x, y = apply("rotate(90 5 5)", 5, 0)
	if math.Abs(x-10) > 1e-9 || math.Abs(y-5) > 1e-9 {
		t.Errorf("rotate(90,5,5)(5,0) = (%g,%g), want (10,5)", x, y)
	}
	// matrix(a b c d e f).
	x, y = apply("matrix(1 0 0 1 7 8)", 1, 1)
	if x != 8 || y != 9 {
		t.Errorf("matrix = (%g,%g)", x, y)
	}
	// skewX(45): (0,1) -> (1,1).
	x, y = apply("skewX(45)", 0, 1)
	if math.Abs(x-1) > 1e-12 || math.Abs(y-1) > 1e-12 {
		t.Errorf("skewX(45)(0,1) = (%g,%g)", x, y)
	}
	// Malformed drops everything.
	if _, ok := parseTransform("translate(10) bogus(1)"); ok {
		t.Error("bogus accepted")
	}
	// Empty is identity.
	if m, ok := parseTransform(""); !ok || m != render.Identity {
		t.Errorf("empty = %+v,%v", m, ok)
	}
}
