package filter

import (
	"image"
	"math"
	"testing"
)

// TestSaturateMatrixHandComputed checks the saturate coefficients against the
// spec's formula, evaluated by hand at s=0.5.
//
//	row0 col0 = 0.213 + 0.787·0.5 = 0.6065
//	row0 col1 = 0.715 - 0.715·0.5 = 0.3575
//	row0 col2 = 0.072 - 0.072·0.5 = 0.036
func TestSaturateMatrixHandComputed(t *testing.T) {
	m := SaturateMatrix(0.5)
	want := map[int]float32{0: 0.6065, 1: 0.3575, 2: 0.036}
	for i, w := range want {
		if math.Abs(float64(m[i]-w)) > 1e-5 {
			t.Errorf("m[%d] = %v, want %v", i, m[i], w)
		}
	}
	// Alpha row must be untouched: saturate never changes alpha.
	if m[15] != 0 || m[16] != 0 || m[17] != 0 || m[18] != 1 || m[19] != 0 {
		t.Errorf("alpha row = %v, want the identity row (0,0,0,1,0)", m[15:20])
	}
}

// TestSaturateAtOneIsIdentity is the cheapest check that the coefficients were
// transcribed correctly: every "0.213 + 0.787·s" style term must collapse to
// the identity at s=1, and a single mistyped constant breaks it.
func TestSaturateAtOneIsIdentity(t *testing.T) {
	assertIdentityMatrix(t, SaturateMatrix(1), "SaturateMatrix(1)")
}

// TestSaturateAtZeroIsFullGreyscale pins that s=0 collapses all three colour
// rows onto the SAME luminance weights — which is what makes grayscale() work,
// since it lowers to saturate(1-amount).
func TestSaturateAtZeroIsFullGreyscale(t *testing.T) {
	m := SaturateMatrix(0)
	for row := 0; row < 3; row++ {
		for col := 0; col < 3; col++ {
			want := [3]float32{0.213, 0.715, 0.072}[col]
			if got := m[row*5+col]; math.Abs(float64(got-want)) > 1e-5 {
				t.Errorf("row %d col %d = %v, want %v (all rows equal at s=0)", row, col, got, want)
			}
		}
	}
}

// TestHueRotateAtKeyAngles pins the corpus's edge angles.
//
// 0 and 360 degrees must both be EXACTLY the identity (cos=1, sin=0 makes the
// spec's three terms sum to it); 120 and 240 are the classic RGB permutation
// points and are checked for round-tripping rather than for a hand-computed
// matrix, since their coefficients are not memorable numbers.
func TestHueRotateAtKeyAngles(t *testing.T) {
	assertIdentityMatrix(t, HueRotateMatrix(0), "HueRotateMatrix(0)")
	assertIdentityMatrix(t, HueRotateMatrix(360), "HueRotateMatrix(360)")

	// Three 120-degree rotations must return a colour to (approximately)
	// itself, since the transform is a rotation about the luminance axis.
	//
	// The test colour is deliberately near mid-grey. The 120-degree matrix
	// carries coefficients outside [0,1] (its first is -0.365), so a saturated
	// colour drives an intermediate channel out of range, ApplyColorMatrix
	// clamps it, and the round trip stops being a round trip — through no
	// fault of the matrix. Choosing a colour whose intermediates stay in range
	// tests the coefficients rather than the clamp.
	r := image.Rect(0, 0, 1, 1)
	in := NewBuffer(r, SRGB)
	in.Set(0, 0, 0.5, 0.4, 0.3, 1)
	cur := in
	for i := 0; i < 3; i++ {
		cur = ApplyColorMatrix(cur, HueRotateMatrix(120), r)
	}
	gr, gg, gb, _ := cur.At(0, 0)
	for i, pair := range [][2]float32{{gr, 0.5}, {gg, 0.4}, {gb, 0.3}} {
		if math.Abs(float64(pair[0]-pair[1])) > 1e-3 {
			t.Errorf("channel %d after 3x120deg = %v, want %v", i, pair[0], pair[1])
		}
	}
}

// TestHueRotateAt240IsTheInverseOf120 pins the sign of the sin terms, which a
// transcription error flips without disturbing the 0/360 identity check.
func TestHueRotateAt240IsTheInverseOf120(t *testing.T) {
	r := image.Rect(0, 0, 1, 1)
	// Near mid-grey for the same clamping reason as TestHueRotateAtKeyAngles.
	in := NewBuffer(r, SRGB)
	in.Set(0, 0, 0.5, 0.45, 0.35, 1)

	fwd := ApplyColorMatrix(in, HueRotateMatrix(120), r)
	back := ApplyColorMatrix(fwd, HueRotateMatrix(240), r)
	gr, gg, gb, _ := back.At(0, 0)
	for i, pair := range [][2]float32{{gr, 0.5}, {gg, 0.45}, {gb, 0.35}} {
		if math.Abs(float64(pair[0]-pair[1])) > 1e-3 {
			t.Errorf("channel %d after 120deg then 240deg = %v, want %v (the sin terms' sign is wrong)", i, pair[0], pair[1])
		}
	}
}

// TestLuminanceToAlphaUsesTheSVGWeights pins the BT.709 filter-luminance
// coefficients, NOT the 0.3/0.59/0.11 set PDF's blend functions use for the
// same-sounding quantity. Substituting one for the other shifts every result
// slightly and uniformly — invisible by eye, and easy to introduce by reaching
// for whichever constant is nearest to hand.
func TestLuminanceToAlphaUsesTheSVGWeights(t *testing.T) {
	m := LuminanceToAlphaMatrix
	for i, want := range map[int]float32{15: 0.2125, 16: 0.7154, 17: 0.0721} {
		if math.Abs(float64(m[i]-want)) > 1e-6 {
			t.Errorf("m[%d] = %v, want %v (SVG's filter luminance weights)", i, m[i], want)
		}
	}
	// The colour rows must be entirely zero: the result is black with the
	// luminance moved into alpha.
	for i := 0; i < 15; i++ {
		if m[i] != 0 {
			t.Errorf("m[%d] = %v, want 0 (luminanceToAlpha zeroes colour)", i, m[i])
		}
	}
}

// TestLuminanceToAlphaEndToEnd checks the primitive, not just the matrix:
// pure green (0,1,0) must produce alpha 0.7154 and black colour.
func TestLuminanceToAlphaEndToEnd(t *testing.T) {
	r := image.Rect(0, 0, 1, 1)
	in := NewBuffer(r, SRGB)
	in.Set(0, 0, 0, 1, 0, 1)

	out := ApplyColorMatrix(in, LuminanceToAlphaMatrix, r)
	cr, cg, cb, ca := out.At(0, 0)
	if math.Abs(float64(ca)-0.7154) > 1e-5 {
		t.Errorf("alpha = %v, want 0.7154", ca)
	}
	if cr != 0 || cg != 0 || cb != 0 {
		t.Errorf("colour = (%v,%v,%v), want black", cr, cg, cb)
	}
}

// TestApplyColorMatrixOperatesUnpremultiplied is the counterpart to
// TestGaussianBlurOnSemiTransparentEdgeStaysPremultiplied, and it exists
// because getting EITHER of the two backwards is the classic bug: blur is
// premultiplied, feColorMatrix is not.
//
// A saturate on a pixel must give the SAME colour whatever that pixel's alpha
// is. Premultiplying first would scale the colour by alpha before the matrix
// mixes the channels, so a half-transparent pixel would come out a different
// colour from an opaque one carrying the identical colour value.
//
// Mutation check: premultiplying inside ApplyColorMatrix fails this test.
func TestApplyColorMatrixOperatesUnpremultiplied(t *testing.T) {
	r := image.Rect(0, 0, 2, 1)
	in := NewBuffer(r, SRGB)
	in.Set(0, 0, 0.8, 0.4, 0.2, 1.0)  // opaque
	in.Set(1, 0, 0.8, 0.4, 0.2, 0.25) // the SAME colour, mostly transparent

	out := ApplyColorMatrix(in, SaturateMatrix(0.3), r)
	r0, g0, b0, a0 := out.At(0, 0)
	r1, g1, b1, a1 := out.At(1, 0)

	if a0 != 1 || math.Abs(float64(a1)-0.25) > 1e-6 {
		t.Fatalf("alphas changed: %v, %v — saturate must not touch alpha", a0, a1)
	}
	for i, pair := range [][2]float32{{r0, r1}, {g0, g1}, {b0, b1}} {
		if math.Abs(float64(pair[0]-pair[1])) > 1e-5 {
			t.Errorf("channel %d: opaque gave %v but semi-transparent gave %v — the matrix ran on PREMULTIPLIED values; feColorMatrix is defined on straight ones",
				i, pair[0], pair[1])
		}
	}
}

// TestApplyColorMatrixClampsEveryChannel pins that a non-normalized matrix (or
// a large/negative saturate coefficient, both of which the corpus contains)
// produces clamped output rather than out-of-range values that later
// primitives would propagate.
func TestApplyColorMatrixClampsEveryChannel(t *testing.T) {
	r := image.Rect(0, 0, 1, 1)
	in := NewBuffer(r, SRGB)
	in.Set(0, 0, 0.5, 0.5, 0.5, 1)

	// Every output channel is driven far out of range in both directions.
	m := ColorMatrix{
		9, 9, 9, 0, 9,
		-9, -9, -9, 0, -9,
		9, 9, 9, 0, 9,
		0, 0, 0, 9, 9,
	}
	out := ApplyColorMatrix(in, m, r)
	cr, cg, cb, ca := out.At(0, 0)
	for i, v := range []float32{cr, cg, cb, ca} {
		if v < 0 || v > 1 {
			t.Errorf("channel %d = %v, escaped [0,1]", i, v)
		}
	}
	if cg != 0 {
		t.Errorf("green = %v, want 0 (clamped from a large negative)", cg)
	}
}

// TestIdentityColorMatrixLeavesPixelsAlone pins the value every "no usable
// values" case resolves to, which the corpus's without-attributes and
// matrix-without-values fixtures render as an untouched source.
func TestIdentityColorMatrixLeavesPixelsAlone(t *testing.T) {
	r := image.Rect(0, 0, 3, 3)
	in := NewBuffer(r, SRGB)
	in.Set(1, 1, 0.3, 0.6, 0.9, 0.7)

	out := ApplyColorMatrix(in, IdentityColorMatrix, r)
	cr, cg, cb, ca := out.At(1, 1)
	for i, pair := range [][2]float32{{cr, 0.3}, {cg, 0.6}, {cb, 0.9}, {ca, 0.7}} {
		if math.Abs(float64(pair[0]-pair[1])) > 1e-6 {
			t.Errorf("channel %d = %v, want %v", i, pair[0], pair[1])
		}
	}
}

// TestApplyColorMatrixNilInput pins the never-panic rule.
func TestApplyColorMatrixNilInput(t *testing.T) {
	out := ApplyColorMatrix(nil, IdentityColorMatrix, image.Rect(0, 0, 2, 2))
	if out == nil {
		t.Fatal("ApplyColorMatrix(nil) returned nil")
	}
}

// assertIdentityMatrix fails unless m is the identity to within float32 noise.
func assertIdentityMatrix(t *testing.T, m ColorMatrix, what string) {
	t.Helper()
	for i := range m {
		if math.Abs(float64(m[i]-IdentityColorMatrix[i])) > 1e-5 {
			t.Errorf("%s: m[%d] = %v, want %v (the whole matrix must be the identity)",
				what, i, m[i], IdentityColorMatrix[i])
		}
	}
}
