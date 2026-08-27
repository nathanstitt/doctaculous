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

// TestParseAngle covers every unit form the resvg marker corpus's
// orient="<angle>" fixtures exercise: orient=-45 (bare number, degrees),
// orient=0.25turn, orient=1.5rad, orient=30, orient=40grad, orient=9999
// (an out-of-[0,360)-range but still valid angle — no wraparound is
// expected of the parser itself, only of whatever renders the result).
func TestParseAngle(t *testing.T) {
	tests := []struct {
		in   string
		want float64
	}{
		{"-45", -45 * math.Pi / 180},
		{"30", 30 * math.Pi / 180},
		{"9999", 9999 * math.Pi / 180},
		{"0deg", 0},
		{"180deg", math.Pi},
		{"40grad", 40 * math.Pi / 200},
		{"1.5rad", 1.5},
		{"0.25turn", 0.5 * math.Pi},
		{"1turn", 2 * math.Pi},
	}
	for _, tc := range tests {
		got, ok := parseAngle(tc.in)
		if !ok {
			t.Errorf("parseAngle(%q) failed", tc.in)
			continue
		}
		if math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("parseAngle(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestParseAngleInvalid(t *testing.T) {
	for _, in := range []string{"", "auto", "45xyz", "deg", "  "} {
		if _, ok := parseAngle(in); ok {
			t.Errorf("parseAngle(%q) succeeded, want failure", in)
		}
	}
}
