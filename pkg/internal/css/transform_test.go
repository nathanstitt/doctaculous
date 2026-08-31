package css

import (
	"math"
	"testing"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

// The 2D transform functions parse into the right matrix.
func TestParseTransformFunctions(t *testing.T) {
	cases := []struct {
		in               string
		a, b, c, d, e, f float64
		pctX, pctY       float64
	}{
		{"translateX(60px)", 1, 0, 0, 1, 60, 0, 0, 0},
		{"translateY(30px)", 1, 0, 0, 1, 0, 30, 0, 0},
		{"translate(60px,30px)", 1, 0, 0, 1, 60, 30, 0, 0},
		{"translate(60px)", 1, 0, 0, 1, 60, 0, 0, 0},
		{"scale(2)", 2, 0, 0, 2, 0, 0, 0, 0},
		{"scale(2,3)", 2, 0, 0, 3, 0, 0, 0, 0},
		{"scaleX(2)", 2, 0, 0, 1, 0, 0, 0, 0},
		{"matrix(1,0,0,1,60,30)", 1, 0, 0, 1, 60, 30, 0, 0},
		// A percentage translate cannot resolve until the box's size is known.
		{"translateX(50%)", 1, 0, 0, 1, 0, 0, 0.5, 0},
	}
	for _, c := range cases {
		got, ok := parseTransform(c.in, 16)
		if !ok {
			t.Errorf("%s: did not parse", c.in)
			continue
		}
		if !approx(got.A, c.a) || !approx(got.B, c.b) || !approx(got.C, c.c) ||
			!approx(got.D, c.d) || !approx(got.E, c.e) || !approx(got.F, c.f) ||
			!approx(got.PctX, c.pctX) || !approx(got.PctY, c.pctY) {
			t.Errorf("%s = %+v, want a=%v b=%v c=%v d=%v e=%v f=%v pctX=%v",
				c.in, got, c.a, c.b, c.c, c.d, c.e, c.f, c.pctX)
		}
	}
}

// rotate() accepts every CSS angle unit.
func TestParseTransformAngles(t *testing.T) {
	for _, in := range []string{"rotate(90deg)", "rotate(1.5707963268rad)", "rotate(100grad)", "rotate(0.25turn)"} {
		got, ok := parseTransform(in, 16)
		if !ok {
			t.Errorf("%s: did not parse", in)
			continue
		}
		// 90 degrees: [0 1; -1 0].
		if !approx(got.B, 1) || !approx(got.C, -1) || math.Abs(got.A) > 1e-6 || math.Abs(got.D) > 1e-6 {
			t.Errorf("%s = %+v, want a 90-degree rotation", in, got)
		}
	}
}

// A function list composes left to right, each in the previous one's space.
func TestParseTransformComposes(t *testing.T) {
	got, ok := parseTransform("translateX(40px) scale(2)", 16)
	if !ok {
		t.Fatal("did not parse")
	}
	// scale applies inside the translate: the offset is unchanged, the scale is 2.
	if !approx(got.A, 2) || !approx(got.D, 2) || !approx(got.E, 40) {
		t.Errorf("composed = %+v, want scale 2 with a 40px offset", got)
	}
}

// 3D and unknown functions are REFUSED, not approximated by dropping their Z terms:
// this engine has no 3D pipeline, and a silently flattened transform paints the wrong
// thing. The declaration then drops and the previous value stands.
func TestParseTransformRefusesUnsupported(t *testing.T) {
	for _, in := range []string{
		"translate3d(60px,0,0)", "rotateX(45deg)", "rotateY(45deg)", "perspective(500px)",
		"matrix3d(1,0,0,0,0,1,0,0,0,0,1,0,0,0,0,1)", "bogus(1)", "translateX(60)",
	} {
		if _, ok := parseTransform(in, 16); ok {
			t.Errorf("%s parsed, want a refusal", in)
		}
	}
}

// "none" and an empty value yield no transform.
func TestParseTransformNone(t *testing.T) {
	for _, in := range []string{"none", "NONE", "", "   "} {
		tr, ok := parseTransform(in, 16)
		if ok {
			t.Errorf("%q parsed as a transform", in)
		}
		if !tr.IsIdentity() {
			t.Errorf("%q = %+v, want the identity", in, tr)
		}
	}
}

// An unimplemented function leaves the cascade's previous value rather than resetting
// it, per CSS error handling.
func TestTransformInvalidKeepsPreviousValue(t *testing.T) {
	cs := initialStyle()
	cs.FontSizePt = 16
	applyDeclaration(&cs, Declaration{Property: "transform", Value: "translateX(60px)"})
	applyDeclaration(&cs, Declaration{Property: "transform", Value: "rotate3d(1,1,1,45deg)"})
	if !approx(cs.Transform.E, 60) {
		t.Errorf("transform = %+v, want the previous translateX(60px) preserved", cs.Transform)
	}
	// But "none" DOES clear it.
	applyDeclaration(&cs, Declaration{Property: "transform", Value: "none"})
	if !cs.Transform.IsIdentity() {
		t.Errorf("transform after none = %+v, want the identity", cs.Transform)
	}
}
