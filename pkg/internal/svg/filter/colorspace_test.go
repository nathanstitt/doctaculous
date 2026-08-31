package filter

import (
	"image"
	"image/color"
	"math"
	"testing"
)

// TestSRGBLinearRoundTrip proves the two transfer functions are actual
// inverses across the whole range, not merely similar. A conversion pair that
// drifts would corrupt every filter that converts in and back out, which is
// every filter running in the linearRGB default.
func TestSRGBLinearRoundTrip(t *testing.T) {
	for i := 0; i <= 1000; i++ {
		v := float64(i) / 1000
		got := linearToSRGB(srgbToLinear(v))
		if math.Abs(got-v) > 1e-12 {
			t.Fatalf("round trip at %v: got %v, want %v (drift %g)", v, got, v, got-v)
		}
	}
}

// TestSRGBTransferFunctionKnownValues pins the transfer function against
// values computed from the IEC 61966-2-1 definition by hand, including BOTH
// sides of the linear-segment boundary.
//
// The boundary is the discriminating part: a naive pow(v, 2.2)
// implementation — the classic mistake this test exists to catch — is wrong
// everywhere but wrong by the largest RELATIVE margin in the near-black
// values below the cutoff, which is exactly where a blurred shadow's falloff
// lives.
func TestSRGBTransferFunctionKnownValues(t *testing.T) {
	cases := []struct {
		name string
		srgb float64
		lin  float64
	}{
		{"black", 0, 0},
		{"white", 1, 1},
		// Below the cutoff the curve is exactly v/12.92.
		{"just below cutoff", 0.04, 0.04 / 12.92},
		// linearCutoff is the linear-space breakpoint, defined as
		// srgbLinearCutoff/12.92; asserting the pair here is what pins the
		// two segments meeting at the same point rather than the curve
		// having a step in it.
		{"at cutoff", srgbLinearCutoff, linearCutoff},
		// Just above, the offset power curve takes over. Continuity at the
		// boundary is what makes the two segments a single function.
		{"just above cutoff", 0.05, math.Pow((0.05+0.055)/1.055, 2.4)},
		{"mid gray", 0.5, math.Pow((0.5+0.055)/1.055, 2.4)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := srgbToLinear(c.srgb); math.Abs(got-c.lin) > 1e-12 {
				t.Errorf("srgbToLinear(%v) = %v, want %v", c.srgb, got, c.lin)
			}
			if got := linearToSRGB(c.lin); math.Abs(got-c.srgb) > 1e-12 {
				t.Errorf("linearToSRGB(%v) = %v, want %v", c.lin, got, c.srgb)
			}
		})
	}
}

// TestSRGBTransferIsNotGamma22 is the DISCRIMINATING test the design calls
// for: it proves this implementation is the real sRGB curve and not the
// pow(2.2) approximation a careless implementation would use.
//
// Mid-gray 0.5 differs between the two by more than a 1/255 step, and a
// near-black value differs by far more in relative terms, so a naive
// implementation cannot pass both assertions.
func TestSRGBTransferIsNotGamma22(t *testing.T) {
	// At mid-gray the two curves differ measurably in linear terms. The
	// assertion is that this implementation tracks the REAL curve, not that
	// the two are far apart: substituting pow(2.2) here would move the
	// value away from the exact expected one below.
	const mid = 0.5
	naive := math.Pow(mid, 2.2)
	exact := math.Pow((mid+0.055)/1.055, 2.4)
	got := srgbToLinear(mid)
	if math.Abs(got-exact) > 1e-12 {
		t.Errorf("srgbToLinear(0.5) = %v, want the exact sRGB curve value %v", got, exact)
	}
	if math.Abs(got-naive) < 1e-3 {
		t.Errorf("srgbToLinear(0.5) = %v is indistinguishable from pow(0.5, 2.2) = %v; "+
			"the test can no longer tell the two curves apart", got, naive)
	}

	// Near black, the linear segment dominates and the difference is large
	// in relative terms: pow(0.02, 2.2) is ~4x smaller than the true value.
	const dark = 0.02
	trueDark := dark / 12.92
	naiveDark := math.Pow(dark, 2.2)
	if got := srgbToLinear(dark); math.Abs(got-trueDark) > 1e-12 {
		t.Errorf("srgbToLinear(%v) = %v, want the LINEAR segment value %v (not %v)", dark, got, trueDark, naiveDark)
	}
	if naiveDark/trueDark > 0.5 {
		t.Fatalf("test premise broken: pow(2.2) at %v is not meaningfully different from the linear segment", dark)
	}
}

// TestSRGBToLinear8MatchesExact confirms the 8-bit lookup table agrees with
// the float function it was built from, so the fast path used on every pixel
// cannot drift from the reference implementation used on authored colors.
func TestSRGBToLinear8MatchesExact(t *testing.T) {
	for i := 0; i < 256; i++ {
		want := srgbToLinear(float64(i) / 255)
		if got := srgbToLinear8(uint8(i)); math.Abs(got-want) > 1e-15 {
			t.Fatalf("srgbToLinear8(%d) = %v, want %v", i, got, want)
		}
	}
}

// TestLinearToSRGB8NoDarkBanding guards the reason linearToSRGB8 computes
// rather than reading a table: distinct linear values in the dark end must
// map to DISTINCT 8-bit sRGB values. A 256-entry inverse table indexed by a
// quantized linear value would collapse several of these together, banding
// exactly the near-black falloff filters produce most.
func TestLinearToSRGB8NoDarkBanding(t *testing.T) {
	seen := map[uint8]float64{}
	for _, lin := range []float64{0.0005, 0.001, 0.002, 0.004, 0.008} {
		got := linearToSRGB8(lin)
		if prev, dup := seen[got]; dup {
			t.Errorf("linear %v and %v both map to sRGB %d; the dark end is being quantized away", prev, lin, got)
		}
		seen[got] = lin
	}
}

// TestBufferColorSpaceConversionChangesPixels is the discriminating
// color-space test at the BUFFER level: the same source pixels carried
// through linearRGB and through sRGB must differ, and must differ in the
// specific direction the transfer function dictates.
//
// A test that only asserted "the two outputs are not equal" would pass with
// the conversion applied backwards, so this pins the actual value: a
// mid-gray's linear representation is DARKER than its sRGB encoding.
func TestBufferColorSpaceConversionChangesPixels(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 1, 1))
	src.Set(0, 0, mustOpaque(128, 128, 128))

	lin := FromRGBA(src, image.Rect(0, 0, 1, 1), LinearRGB)
	srgb := FromRGBA(src, image.Rect(0, 0, 1, 1), SRGB)

	lr, _, _, _ := lin.At(0, 0)
	sr, _, _, _ := srgb.At(0, 0)
	if lr == sr {
		t.Fatal("linearRGB and sRGB buffers hold identical values; no conversion happened")
	}
	if lr >= sr {
		t.Errorf("linear value %v is not darker than the sRGB value %v; the conversion looks inverted", lr, sr)
	}
	// The exact expected value, so an approximate conversion is caught too.
	want := float32(srgbToLinear(128.0 / 255))
	if math.Abs(float64(lr-want)) > 1e-6 {
		t.Errorf("linear channel = %v, want %v", lr, want)
	}
}

// TestBufferRoundTripPreservesPixels proves a Buffer can go from an
// *image.RGBA into linear space and back without drifting, which is what
// makes a filter graph that touches no pixel a visual no-op.
func TestBufferRoundTripPreservesPixels(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 4, 1))
	src.Set(0, 0, mustOpaque(0, 0, 0))
	src.Set(1, 0, mustOpaque(255, 255, 255))
	src.Set(2, 0, mustOpaque(46, 139, 87))
	src.Set(3, 0, mustOpaque(1, 2, 3))

	for _, space := range []ColorSpace{LinearRGB, SRGB} {
		buf := FromRGBA(src, src.Bounds(), space)
		got := buf.ToRGBA()
		for x := 0; x < 4; x++ {
			w, g := src.RGBAAt(x, 0), got.RGBAAt(x, 0)
			if absDiff(w.R, g.R) > 1 || absDiff(w.G, g.G) > 1 || absDiff(w.B, g.B) > 1 || w.A != g.A {
				t.Errorf("space %v pixel %d: got %v, want %v", space, x, g, w)
			}
		}
	}
}

// TestConvertToIsIdempotent confirms converting a buffer to the space it is
// already in does nothing, so a graph that re-asserts a primitive's space
// cannot double-convert and darken the result.
func TestConvertToIsIdempotent(t *testing.T) {
	b := NewBuffer(image.Rect(0, 0, 1, 1), LinearRGB)
	b.Set(0, 0, 0.25, 0.5, 0.75, 1)
	b.ConvertTo(LinearRGB)
	r, g, bl, _ := b.At(0, 0)
	if r != 0.25 || g != 0.5 || bl != 0.75 {
		t.Errorf("converting to the current space changed pixels: got %v %v %v", r, g, bl)
	}
}

func absDiff(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}

// TestFromRGBAUnpremultipliesSemiTransparentPixels is the test whose ABSENCE
// let a broken un-premultiply survive the whole suite.
//
// image.RGBA is PREMULTIPLIED; a Buffer holds STRAIGHT alpha. Skipping the
// un-premultiply leaves every semi-transparent pixel's color scaled by its
// own alpha — a 50%-alpha pure red arrives as (0.5, 0, 0) instead of
// (1, 0, 0). No primitive shipped so far reveals it (feFlood discards its
// input, and feOffset's integer path copies pixels verbatim without ever
// looking at a channel), but feGaussianBlur reads and weights those channels
// directly, so a partially transparent edge is exactly where it shows.
//
// The color channels are asserted INDEPENDENTLY of alpha, which is what makes
// this catch the omission: a round-trip-only test passes either way, since
// re-premultiplying on the way out undoes the error.
func TestFromRGBAUnpremultipliesSemiTransparentPixels(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 3, 1))
	// Premultiplied: a 50%-alpha pure red is stored as R=128, A=128.
	src.SetRGBA(0, 0, color.RGBA{R: 128, G: 0, B: 0, A: 128})
	// A 25%-alpha pure white: every channel scaled to 64.
	src.SetRGBA(1, 0, color.RGBA{R: 64, G: 64, B: 64, A: 64})
	// Fully opaque needs no division and must pass through untouched.
	src.SetRGBA(2, 0, color.RGBA{R: 200, G: 100, B: 50, A: 255})

	buf := FromRGBA(src, src.Bounds(), SRGB)

	cases := []struct {
		x                          int
		wantR, wantG, wantB, wantA float32
		name                       string
	}{
		{0, 1, 0, 0, 0.5, "50% red must un-premultiply to FULL red"},
		{1, 1, 1, 1, 0.25, "25% white must un-premultiply to FULL white"},
		{2, 200.0 / 255, 100.0 / 255, 50.0 / 255, 1, "opaque passes through"},
	}
	for _, c := range cases {
		r, g, b, a := buf.At(c.x, 0)
		const tol = 0.01
		if absF(r-c.wantR) > tol || absF(g-c.wantG) > tol || absF(b-c.wantB) > tol || absF(a-c.wantA) > tol {
			t.Errorf("%s: got (%.3f, %.3f, %.3f, a=%.3f), want (%.3f, %.3f, %.3f, a=%.3f)",
				c.name, r, g, b, a, c.wantR, c.wantG, c.wantB, c.wantA)
		}
	}
}

// TestSemiTransparentRoundTripPreservesPixels confirms the premultiplied ->
// straight -> premultiplied path is lossless for partially transparent
// pixels in BOTH color spaces, which is what lets a filter graph carry an
// antialiased edge through without darkening or lightening it.
func TestSemiTransparentRoundTripPreservesPixels(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 5, 1))
	for i, c := range []color.RGBA{
		{R: 128, G: 0, B: 0, A: 128},
		{R: 64, G: 64, B: 64, A: 64},
		{R: 20, G: 10, B: 5, A: 32},
		{R: 0, G: 0, B: 0, A: 0},
		{R: 46, G: 139, B: 87, A: 255},
	} {
		src.SetRGBA(i, 0, c)
	}
	for _, space := range []ColorSpace{LinearRGB, SRGB} {
		got := FromRGBA(src, src.Bounds(), space).ToRGBA()
		for x := 0; x < 5; x++ {
			w, g := src.RGBAAt(x, 0), got.RGBAAt(x, 0)
			if absDiff(w.R, g.R) > 2 || absDiff(w.G, g.G) > 2 || absDiff(w.B, g.B) > 2 || absDiff(w.A, g.A) > 1 {
				t.Errorf("space %v pixel %d: got %v, want %v", space, x, g, w)
			}
		}
	}
}

func absF(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
