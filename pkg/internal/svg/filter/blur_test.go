package filter

import (
	"image"
	"math"
	"testing"
)

// TestBoxSizesMatchTheSpecFormula checks the three box widths against the SVG
// filter spec's own formula, hand-evaluated, including the odd/even split.
//
// The offsets matter as much as the sizes: an even d uses two boxes centred on
// OPPOSITE pixel boundaries so their half-pixel shifts cancel, then one odd box
// to recentre. An implementation that ignores the offsets blurs correctly but
// TRANSLATES the result, which reads as a placement bug rather than a blur bug.
func TestBoxSizesMatchTheSpecFormula(t *testing.T) {
	// d = floor(s * 3 * sqrt(2*pi)/4 + 0.5); 3*sqrt(2*pi)/4 = 1.8799712...
	cases := []struct {
		s    float64
		want []boxPass
	}{
		// s=2 → d = floor(3.7599 + 0.5) = 4, EVEN.
		{2, []boxPass{{size: 4, left: 2}, {size: 4, left: 1}, {size: 5, left: 2}}},
		// s=4 → d = floor(7.5199 + 0.5) = 8, EVEN.
		{4, []boxPass{{size: 8, left: 4}, {size: 8, left: 3}, {size: 9, left: 4}}},
		// s=1.6 → d = floor(3.0080 + 0.5) = 3, ODD.
		{1.6, []boxPass{{size: 3, left: 1}, {size: 3, left: 1}, {size: 3, left: 1}}},
		// s=6 → d = floor(11.2798 + 0.5) = 11, ODD.
		{6, []boxPass{{size: 11, left: 5}, {size: 11, left: 5}, {size: 11, left: 5}}},
	}
	for _, tc := range cases {
		got := boxSizes(tc.s)
		if len(got) != len(tc.want) {
			t.Fatalf("boxSizes(%v) = %v, want %v", tc.s, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("boxSizes(%v)[%d] = %+v, want %+v", tc.s, i, got[i], tc.want[i])
			}
		}
	}
}

// TestBoxSizesDegenerateInputsProduceNoBlur pins every input that must leave
// the image untouched rather than producing a one-pixel box or a panic.
func TestBoxSizesDegenerateInputsProduceNoBlur(t *testing.T) {
	for _, s := range []float64{0, -1, -50, 0.01, math.NaN(), math.Inf(1), math.Inf(-1)} {
		if got := boxSizes(s); got != nil {
			t.Errorf("boxSizes(%v) = %v, want nil (no blur)", s, got)
		}
	}
}

// TestGaussianBlurZeroDeviationIsIdentity pins the spec's "a value of zero
// disables the effect of the given filter primitive (i.e., the result is the
// filter input image)" — an identity, NOT a blank. The corpus's
// no-stdDeviation, empty-stdDeviation and negative-stdDeviation fixtures all
// depend on this producing the unmodified element.
func TestGaussianBlurZeroDeviationIsIdentity(t *testing.T) {
	in := solidBuffer(image.Rect(0, 0, 8, 8), 0.25, 0.5, 0.75, 1)
	out := GaussianBlur(in, 0, 0, in.Bounds())
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			r, g, b, a := out.At(x, y)
			if r != 0.25 || g != 0.5 || b != 0.75 || a != 1 {
				t.Fatalf("(%d,%d) = %v %v %v %v, want the input unchanged", x, y, r, g, b, a)
			}
		}
	}
}

// TestGaussianBlurOneAxisOnly proves the two deviations are independent: a
// blur with sdy=0 must spread horizontally and NOT vertically. The corpus
// separates these with stdDeviation="0 5" and "5 0", which a single-deviation
// implementation renders identically.
func TestGaussianBlurOneAxisOnly(t *testing.T) {
	// A single opaque pixel in the middle of a transparent field.
	in := NewBuffer(image.Rect(0, 0, 21, 21), LinearRGB)
	in.Set(10, 10, 1, 1, 1, 1)

	out := GaussianBlur(in, 3, 0, in.Bounds())
	if _, _, _, a := out.At(13, 10); a <= 0 {
		t.Error("horizontal blur did not spread along x")
	}
	if _, _, _, a := out.At(10, 13); a != 0 {
		t.Errorf("vertical spread = %v with sdy=0, want 0 — the axes are not independent", a)
	}

	out = GaussianBlur(in, 0, 3, in.Bounds())
	if _, _, _, a := out.At(10, 13); a <= 0 {
		t.Error("vertical blur did not spread along y")
	}
	if _, _, _, a := out.At(13, 10); a != 0 {
		t.Errorf("horizontal spread = %v with sdx=0, want 0", a)
	}
}

// TestGaussianBlurConservesTotalAlpha checks the box passes against a property
// no single sampled pixel can: a blur redistributes coverage, it does not
// create or destroy it. The working buffer is grown to hold the whole spread,
// so nothing legitimately falls off the edge.
//
// A normalization bug (dividing by the wrong box width, or priming the running
// sum incorrectly) changes the total while leaving the result looking
// plausibly blurred, which is exactly the failure a spot check misses.
func TestGaussianBlurConservesTotalAlpha(t *testing.T) {
	in := NewBuffer(image.Rect(0, 0, 41, 41), LinearRGB)
	for y := 18; y < 23; y++ {
		for x := 18; x < 23; x++ {
			in.Set(x, y, 1, 0, 0, 1)
		}
	}
	before := totalAlpha(in)

	out := GaussianBlur(in, 2, 2, in.Bounds())
	after := totalAlpha(out)
	if math.Abs(float64(after-before)) > 0.01*float64(before) {
		t.Errorf("total alpha %v → %v, want conserved within 1%%", before, after)
	}
}

// TestGaussianBlurIsCentred pins that a symmetric input stays symmetric: the
// even-d box offsets must cancel. A missing offset shifts the result by half a
// pixel per pass, which this detects as an asymmetric spread.
func TestGaussianBlurIsCentred(t *testing.T) {
	in := NewBuffer(image.Rect(0, 0, 41, 41), LinearRGB)
	in.Set(20, 20, 1, 1, 1, 1)

	// s=2 gives an EVEN d (4), the case whose offsets have to cancel.
	out := GaussianBlur(in, 2, 2, in.Bounds())
	for d := 1; d <= 6; d++ {
		_, _, _, left := out.At(20-d, 20)
		_, _, _, right := out.At(20+d, 20)
		if math.Abs(float64(left-right)) > 1e-6 {
			t.Errorf("at distance %d: left=%v right=%v, want symmetric (the even-d box offsets did not cancel)", d, left, right)
		}
		_, _, _, up := out.At(20, 20-d)
		_, _, _, down := out.At(20, 20+d)
		if math.Abs(float64(up-down)) > 1e-6 {
			t.Errorf("at distance %d: up=%v down=%v, want symmetric", d, up, down)
		}
	}
}

// TestGaussianBlurOnSemiTransparentEdgeStaysPremultiplied is THE
// premultiplication discriminator this primitive needs.
//
// The setup is an opaque WHITE block against a fully-transparent field whose
// colour channels hold BLACK (the value a freshly-allocated buffer carries,
// and the value any straight-alpha representation of "transparent" carries).
// Blurring premultiplied, the transparent pixels contribute nothing to colour —
// only to alpha — so the blurred edge stays WHITE and merely fades. Blurring
// STRAIGHT colour instead averages the transparent pixels' black in with equal
// weight, so the edge darkens toward grey while its alpha does the same thing
// either way.
//
// That is why the test asserts on COLOUR at a partially-covered pixel and not
// on alpha: alpha is identical under both implementations, so an alpha-only
// assertion passes with the premultiplication removed. Mutation check: making
// GaussianBlur operate on straight values fails this test and no other.
func TestGaussianBlurOnSemiTransparentEdgeStaysPremultiplied(t *testing.T) {
	// The fixture has to put TWO DIFFERENT COLOURS at TWO DIFFERENT ALPHAS on
	// either side of the edge. A single colour fading to a transparent
	// backdrop does NOT discriminate: un-premultiplying on the way out divides
	// by exactly the alpha that was multiplied in, so a uniform colour is
	// recovered perfectly whether or not the blur premultiplied. (Measured:
	// opaque-red-to-transparent gives rgb=(1,0,0) at every partial pixel under
	// both implementations.)
	//
	// Opaque red beside 10%-alpha green is the case that separates them. The
	// green side must contribute to the average IN PROPORTION TO ITS COVERAGE,
	// which is precisely what premultiplying encodes; averaging straight
	// colour weights the barely-present green as heavily as the fully opaque
	// red and pulls the blend far too green.
	in := NewBuffer(image.Rect(0, 0, 41, 41), LinearRGB)
	for y := 0; y < 41; y++ {
		for x := 0; x < 20; x++ {
			in.Set(x, y, 1, 0, 0, 1.0) // opaque red
		}
		for x := 20; x < 41; x++ {
			in.Set(x, y, 0, 1, 0, 0.1) // barely-there green
		}
	}

	out := GaussianBlur(in, 3, 3, in.Bounds())

	// Two pixels into the green side, coverage-weighted averaging still leaves
	// red clearly dominant (measured r=0.741 g=0.259). Straight averaging
	// discards the coverage weighting and lets green take over far sooner.
	r, g, _, _ := out.At(22, 20)
	if r <= g {
		t.Fatalf("at x=22 colour=(%.3f,%.3f): a 10%%-alpha green outweighed an opaque red, so the blur averaged STRAIGHT colour — it must premultiply", r, g)
	}
	if r < 0.70 || r > 0.78 {
		t.Fatalf("at x=22 red=%.3f, want ~0.741: the coverage weighting is off", r)
	}
}

// TestGaussianBlurHugeDeviationStillProducesOutput pins the corpus's
// huge-stdDeviation fixture ("Should not crash", and visibly a soft wash
// rather than a blank).
//
// Without clampToExtent the box grows far wider than the region, every window
// reads almost entirely off-buffer, and the element decays to NOTHING — which
// is not "more blurred", it is gone.
func TestGaussianBlurHugeDeviationStillProducesOutput(t *testing.T) {
	in := NewBuffer(image.Rect(0, 0, 60, 60), LinearRGB)
	for y := 10; y < 50; y++ {
		for x := 10; x < 50; x++ {
			in.Set(x, y, 0, 1, 0, 1)
		}
	}
	for _, s := range []float64{1000, 1e6, 1e300} {
		out := GaussianBlur(in, s, s, in.Bounds())
		if _, _, _, a := out.At(30, 30); a < 0.1 {
			t.Errorf("stdDeviation=%v left centre alpha %v; the element effectively vanished", s, a)
		}
	}
}

// TestGaussianBlurNonFiniteDeviationDoesNotPanic pins the never-panic rule for
// values a crafted document can reach through a scaled transform.
func TestGaussianBlurNonFiniteDeviationDoesNotPanic(t *testing.T) {
	in := solidBuffer(image.Rect(0, 0, 8, 8), 1, 0, 0, 1)
	for _, s := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), -1} {
		out := GaussianBlur(in, s, s, in.Bounds())
		if out == nil {
			t.Fatalf("stdDeviation=%v returned nil", s)
		}
	}
}

// TestGaussianBlurNilInput pins that a missing input is "nothing to do" rather
// than a crash.
func TestGaussianBlurNilInput(t *testing.T) {
	out := GaussianBlur(nil, 3, 3, image.Rect(0, 0, 4, 4))
	if out == nil || out.Bounds() != image.Rect(0, 0, 4, 4) {
		t.Fatalf("GaussianBlur(nil) = %v, want an empty buffer over the subregion", out)
	}
}

// TestBoxBlurHandComputed checks one horizontal box pass against values
// computed by hand, so the running-sum machinery is verified independently of
// any composed three-pass result.
//
// Input alpha row: 0 0 4 0 0, box size 3 centred (left=1).
// Windows, with out-of-range pixels reading 0:
//
//	x=0: [-1,0,1] = 0+0+0     → 0
//	x=1: [ 0,1,2] = 0+0+4     → 4/3
//	x=2: [ 1,2,3] = 0+4+0     → 4/3
//	x=3: [ 2,3,4] = 4+0+0     → 4/3
//	x=4: [ 3,4,5] = 0+0+0     → 0
func TestBoxBlurHandComputed(t *testing.T) {
	const w, h = 5, 1
	src := make([]float32, 4*w*h)
	src[4*2+3] = 4 // alpha 4 at x=2
	dst := make([]float32, 4*w*h)

	boxBlurH(src, dst, w, h, boxPass{size: 3, left: 1})

	want := []float32{0, 4.0 / 3, 4.0 / 3, 4.0 / 3, 0}
	for x := 0; x < w; x++ {
		got := dst[4*x+3]
		if math.Abs(float64(got-want[x])) > 1e-6 {
			t.Errorf("x=%d alpha = %v, want %v", x, got, want[x])
		}
	}
}

// TestBoxBlurVIsTheTransposeOfH pins that the two passes agree, since they are
// written out separately (for speed) rather than expressed via a transpose.
func TestBoxBlurVIsTheTransposeOfH(t *testing.T) {
	const n = 9
	p := boxPass{size: 4, left: 2}

	row := make([]float32, 4*n)
	row[4*4+3] = 1
	rowOut := make([]float32, 4*n)
	boxBlurH(row, rowOut, n, 1, p)

	col := make([]float32, 4*n)
	col[4*4+3] = 1
	colOut := make([]float32, 4*n)
	boxBlurV(col, colOut, 1, n, p)

	for i := 0; i < n; i++ {
		if math.Abs(float64(rowOut[4*i+3]-colOut[4*i+3])) > 1e-6 {
			t.Errorf("index %d: H=%v V=%v, want the vertical pass to be the horizontal pass transposed",
				i, rowOut[4*i+3], colOut[4*i+3])
		}
	}
}

// solidBuffer builds a buffer filled with one straight-alpha colour.
func solidBuffer(r image.Rectangle, cr, cg, cb, ca float32) *Buffer {
	b := NewBuffer(r, LinearRGB)
	for i := 0; i+3 < len(b.Pix); i += 4 {
		b.Pix[i], b.Pix[i+1], b.Pix[i+2], b.Pix[i+3] = cr, cg, cb, ca
	}
	return b
}

// totalAlpha sums a buffer's alpha channel.
func totalAlpha(b *Buffer) float32 {
	var sum float32
	for i := 3; i < len(b.Pix); i += 4 {
		sum += b.Pix[i]
	}
	return sum
}
