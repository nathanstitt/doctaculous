package filter

import (
	"image"
	"math"
	"testing"
)

// TestPorterDuffFactorsHandComputed checks each operator's (Fa, Fb) pair
// against the SVG spec's own table, so the algebra is verified independently of
// any rendered pixel.
func TestPorterDuffFactorsHandComputed(t *testing.T) {
	const aa, ba = 0.25, 0.5
	cases := []struct {
		op         CompositeOperator
		name       string
		wantA      float64
		wantB      float64
		wantReason string
	}{
		// Fb for `over` is 1 - aa (the SOURCE's alpha): the backdrop shows
		// through wherever the SOURCE does not cover. Writing 1 - ba here
		// instead is the natural typo and it is wrong in exactly the way that
		// double-counts the backdrop.
		{CompositeOver, "over", 1, 1 - aa, "source in full, backdrop where the source is not"},
		{CompositeIn, "in", ba, 0, "source only where the backdrop covers"},
		{CompositeOut, "out", 1 - ba, 0, "source only where the backdrop does NOT cover"},
		{CompositeAtop, "atop", ba, 1 - aa, "source inside the backdrop, backdrop outside the source"},
		{CompositeXor, "xor", 1 - ba, 1 - aa, "each only where the other is absent"},
	}
	for _, tc := range cases {
		fa, fb := porterDuffFactors(tc.op, aa, ba)
		if math.Abs(float64(fa)-tc.wantA) > 1e-6 || math.Abs(float64(fb)-tc.wantB) > 1e-6 {
			t.Errorf("%s: (Fa,Fb) = (%v,%v), want (%v,%v) — %s",
				tc.name, fa, fb, tc.wantA, tc.wantB, tc.wantReason)
		}
	}
}

// TestCompositeOperatorsOnDisjointCoverage checks each Porter-Duff operator at
// the four corners of the coverage square (A only, B only, both, neither),
// which is where the operators actually differ from one another. A test using
// only overlapping opaque input cannot tell `in` from `over`.
func TestCompositeOperatorsOnDisjointCoverage(t *testing.T) {
	// a covers x<2; b covers x>=1. So x=0 is A-only, x=1 is both, x=2 is
	// B-only, x=3 is neither.
	r := image.Rect(0, 0, 4, 1)
	a := NewBuffer(r, LinearRGB)
	a.Set(0, 0, 1, 0, 0, 1)
	a.Set(1, 0, 1, 0, 0, 1)
	b := NewBuffer(r, LinearRGB)
	b.Set(1, 0, 0, 0, 1, 1)
	b.Set(2, 0, 0, 0, 1, 1)

	// Expected alpha at (A-only, both, B-only, neither) per operator.
	cases := []struct {
		op   CompositeOperator
		name string
		want [4]float32
	}{
		{CompositeOver, "over", [4]float32{1, 1, 1, 0}},
		{CompositeIn, "in", [4]float32{0, 1, 0, 0}},
		{CompositeOut, "out", [4]float32{1, 0, 0, 0}},
		{CompositeAtop, "atop", [4]float32{0, 1, 1, 0}},
		{CompositeXor, "xor", [4]float32{1, 0, 1, 0}},
	}
	for _, tc := range cases {
		out := Composite(a, b, tc.op, 0, 0, 0, 0, r)
		for x := 0; x < 4; x++ {
			_, _, _, got := out.At(x, 0)
			if math.Abs(float64(got-tc.want[x])) > 1e-6 {
				t.Errorf("%s at x=%d: alpha = %v, want %v", tc.name, x, got, tc.want[x])
			}
		}
	}
}

// TestCompositeInKeepsTheSourceColour pins that `in` takes the SOURCE's colour
// where both cover, not the backdrop's — the half of the operator an
// alpha-only assertion cannot see, and the half feDropShadow depends on (the
// flood's colour must survive the composite with the offset blur's alpha).
func TestCompositeInKeepsTheSourceColour(t *testing.T) {
	r := image.Rect(0, 0, 1, 1)
	a := NewBuffer(r, LinearRGB)
	a.Set(0, 0, 1, 0, 0, 1) // red source
	b := NewBuffer(r, LinearRGB)
	b.Set(0, 0, 0, 0, 1, 0.5) // half-covered blue backdrop

	out := Composite(a, b, CompositeIn, 0, 0, 0, 0, r)
	cr, cg, cb, ca := out.At(0, 0)
	if math.Abs(float64(ca-0.5)) > 1e-6 {
		t.Errorf("alpha = %v, want the backdrop's 0.5", ca)
	}
	if cr < 0.99 || cg > 0.01 || cb > 0.01 {
		t.Errorf("colour = (%v,%v,%v), want the SOURCE's red", cr, cg, cb)
	}
}

// TestArithmeticHandComputed evaluates the arithmetic rule against values
// worked out by hand, on PREMULTIPLIED inputs.
//
// i1 = 0.5 (premultiplied: colour 1.0 at alpha 0.5), i2 = 0.25.
// k1..k4 = 0.1, 0.2, 0.3, 0.4:
//
//	0.1·0.5·0.25 + 0.2·0.5 + 0.3·0.25 + 0.4
//	= 0.0125 + 0.1 + 0.075 + 0.4 = 0.5875
func TestArithmeticHandComputed(t *testing.T) {
	got := arith(0.1, 0.2, 0.3, 0.4, 0.5, 0.25)
	if math.Abs(float64(got)-0.5875) > 1e-6 {
		t.Errorf("arith = %v, want 0.5875", got)
	}
}

// TestArithmeticClampsTheResultNotTheCoefficients pins the behavior the corpus
// forces: k1..k4 are used VERBATIM and only the per-channel result is clamped.
//
// The operator=arithmetic-and-invalid-k1-4 fixture writes k4="100" and renders
// OPAQUE WHITE. Clamping the coefficients into [0,1] first — which that
// fixture's own <desc> claims is required — would instead render an ordinary
// composite and miss it entirely.
func TestArithmeticClampsTheResultNotTheCoefficients(t *testing.T) {
	if got := arith(0, 0, 0, 100, 0, 0); got != 1 {
		t.Errorf("k4=100 gave %v, want 1 (the RESULT clamps; the coefficient does not)", got)
	}
	if got := arith(0, 0, 0, -10, 0, 0); got != 0 {
		t.Errorf("k4=-10 gave %v, want 0", got)
	}
	// A large positive k1 with both inputs at 1 also saturates rather than
	// overflowing.
	if got := arith(50, 0, 0, 0, 1, 1); got != 1 {
		t.Errorf("k1=50 gave %v, want 1", got)
	}
}

// TestCompositeArithmeticClampsEveryChannel drives the clamping through the
// full primitive, including the un-premultiply on the way out, since a channel
// can exceed its alpha there and produce a straight value above 1.
func TestCompositeArithmeticClampsEveryChannel(t *testing.T) {
	r := image.Rect(0, 0, 1, 1)
	a := NewBuffer(r, LinearRGB)
	a.Set(0, 0, 1, 1, 1, 1)
	b := NewBuffer(r, LinearRGB)
	b.Set(0, 0, 1, 1, 1, 1)

	out := Composite(a, b, CompositeArithmetic, 0, 0, 0, 100, r)
	cr, cg, cb, ca := out.At(0, 0)
	for _, v := range []float32{cr, cg, cb, ca} {
		if v < 0 || v > 1 {
			t.Fatalf("channel %v escaped [0,1]; got (%v,%v,%v,%v)", v, cr, cg, cb, ca)
		}
	}
	if ca != 1 || cr != 1 {
		t.Errorf("got (%v,%v,%v,%v), want opaque white — the corpus's invalid-k1-4 result", cr, cg, cb, ca)
	}
}

// TestCompositeOverIsAssociativeWithMerge pins that Merge is exactly a fold of
// CompositeOver, which is the assumption that lets Merge allocate one buffer
// rather than one per node. If the two ever disagree, an feMerge silently
// composites differently from the equivalent feComposite chain.
func TestCompositeOverIsAssociativeWithMerge(t *testing.T) {
	r := image.Rect(0, 0, 3, 1)
	mk := func(cr, cg, cb, ca float32, xs ...int) *Buffer {
		b := NewBuffer(r, LinearRGB)
		for _, x := range xs {
			b.Set(x, 0, cr, cg, cb, ca)
		}
		return b
	}
	// Three overlapping semi-transparent layers, so the fold order matters.
	l0 := mk(1, 0, 0, 0.5, 0, 1)
	l1 := mk(0, 1, 0, 0.5, 1, 2)
	l2 := mk(0, 0, 1, 0.5, 0, 2)

	merged := Merge([]*Buffer{l0, l1, l2}, r, LinearRGB)

	// The equivalent chain: each layer composited OVER the accumulated result,
	// in the same order (the LAST node is on top, so it is the `in` of the
	// final over).
	step := Composite(l1, l0, CompositeOver, 0, 0, 0, 0, r)
	chained := Composite(l2, step, CompositeOver, 0, 0, 0, 0, r)

	for x := 0; x < 3; x++ {
		mr, mg, mb, ma := merged.At(x, 0)
		cr, cg, cb, ca := chained.At(x, 0)
		for i, pair := range [][2]float32{{mr, cr}, {mg, cg}, {mb, cb}, {ma, ca}} {
			if math.Abs(float64(pair[0]-pair[1])) > 1e-5 {
				t.Errorf("x=%d channel %d: Merge=%v chained-over=%v — feMerge must equal the equivalent feComposite chain",
					x, i, pair[0], pair[1])
			}
		}
	}
}

// TestMergeOrderIsPaintingOrder pins that the FIRST feMergeNode is the BOTTOM
// of the stack. Reversing it renders a drop shadow ON TOP of the element it
// belongs to, which is the whole point of the primitive.
func TestMergeOrderIsPaintingOrder(t *testing.T) {
	r := image.Rect(0, 0, 1, 1)
	bottom := NewBuffer(r, LinearRGB)
	bottom.Set(0, 0, 1, 0, 0, 1) // opaque red
	top := NewBuffer(r, LinearRGB)
	top.Set(0, 0, 0, 0, 1, 1) // opaque blue

	out := Merge([]*Buffer{bottom, top}, r, LinearRGB)
	cr, _, cb, _ := out.At(0, 0)
	if cb < 0.99 || cr > 0.01 {
		t.Errorf("got (r=%v, b=%v), want BLUE — the last feMergeNode paints on top", cr, cb)
	}
}

// TestMergeOfNothingIsTransparent pins that an feMerge with no nodes produces
// transparent black rather than passing its input through.
func TestMergeOfNothingIsTransparent(t *testing.T) {
	r := image.Rect(0, 0, 2, 2)
	out := Merge(nil, r, LinearRGB)
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			if _, _, _, a := out.At(x, y); a != 0 {
				t.Fatalf("(%d,%d) alpha = %v, want 0", x, y, a)
			}
		}
	}
}

// TestCompositeNilInputsDoNotPanic pins the never-panic rule for a graph whose
// input could not be produced.
func TestCompositeNilInputsDoNotPanic(t *testing.T) {
	r := image.Rect(0, 0, 2, 2)
	if out := Composite(nil, nil, CompositeOver, 0, 0, 0, 0, r); out == nil {
		t.Fatal("Composite(nil, nil) returned nil")
	}
	if out := Merge([]*Buffer{nil, nil}, r, SRGB); out == nil {
		t.Fatal("Merge of nils returned nil")
	}
}
