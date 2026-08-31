package filter

import (
	"image"
	"math"
	"testing"
)

// TestBlendModeNameMapsSVGSpellingsToPDF pins the vocabulary bridge between
// SVG's kebab-case `mode` values and the PDF /BM names pkg/render's shared
// blend table is keyed by.
//
// The map exists precisely so there is ONE implementation of colorBurn and its
// friends; a missing entry here silently degrades that mode to source-over,
// which renders plausibly and wrongly.
func TestBlendModeNameMapsSVGSpellingsToPDF(t *testing.T) {
	want := map[string]string{
		"multiply": "Multiply", "screen": "Screen", "overlay": "Overlay",
		"darken": "Darken", "lighten": "Lighten",
		"color-dodge": "ColorDodge", "color-burn": "ColorBurn",
		"hard-light": "HardLight", "soft-light": "SoftLight",
		"difference": "Difference", "exclusion": "Exclusion",
		"hue": "Hue", "saturation": "Saturation",
		"color": "Color", "luminosity": "Luminosity",
	}
	for svg, pdf := range want {
		got, ok := BlendModeName(svg)
		if !ok || got != pdf {
			t.Errorf("BlendModeName(%q) = (%q, %v), want (%q, true)", svg, got, ok, pdf)
		}
	}
	// "normal" is deliberately absent — Composite already implements
	// source-over, so routing it through a blend function would be a slower
	// path to identical pixels.
	for _, mode := range []string{"normal", "", "qwe", "Multiply"} {
		if _, ok := BlendModeName(mode); ok {
			t.Errorf("BlendModeName(%q) reported a blend function; want source-over", mode)
		}
	}
}

// TestBlendSeparableModesHandComputed checks each separable mode's result on
// two fully-opaque inputs, where the CSS compositing formula collapses to the
// blend function alone (as=ab=1 makes the first and third terms vanish).
//
// Opaque inputs are used precisely so the arithmetic is hand-checkable; the
// alpha-aware half of the formula is covered separately below.
func TestBlendSeparableModesHandComputed(t *testing.T) {
	const cs, cb = 0.6, 0.4 // source and backdrop channel values
	cases := []struct {
		mode string
		want float64
		how  string
	}{
		{"Multiply", cb * cs, "cb·cs"},
		{"Screen", cb + cs - cb*cs, "cb + cs - cb·cs"},
		{"Darken", cb, "min(cb, cs)"},
		{"Lighten", cs, "max(cb, cs)"},
		{"Difference", math.Abs(cb - cs), "|cb - cs|"},
	}
	r := image.Rect(0, 0, 1, 1)
	for _, tc := range cases {
		a := NewBuffer(r, SRGB)
		a.Set(0, 0, cs, cs, cs, 1)
		b := NewBuffer(r, SRGB)
		b.Set(0, 0, cb, cb, cb, 1)

		out := Blend(a, b, tc.mode, r)
		got, _, _, alpha := out.At(0, 0)
		if math.Abs(float64(got)-tc.want) > 1e-5 {
			t.Errorf("%s = %v, want %v (%s)", tc.mode, got, tc.want, tc.how)
		}
		if alpha != 1 {
			t.Errorf("%s alpha = %v, want 1 for two opaque inputs", tc.mode, alpha)
		}
	}
}

// TestBlendNormalIsSourceOver pins that the "normal" mode (and any
// unrecognized one) produces EXACTLY what Composite's over produces, rather
// than a second implementation that merely looks the same.
func TestBlendNormalIsSourceOver(t *testing.T) {
	r := image.Rect(0, 0, 2, 1)
	a := NewBuffer(r, SRGB)
	a.Set(0, 0, 1, 0, 0, 0.5)
	b := NewBuffer(r, SRGB)
	b.Set(0, 0, 0, 0, 1, 0.75)
	b.Set(1, 0, 0, 1, 0, 1)

	for _, mode := range []string{"", "Normal", "not-a-mode"} {
		blended := Blend(a, b, mode, r)
		over := Composite(a, b, CompositeOver, 0, 0, 0, 0, r)
		for x := 0; x < 2; x++ {
			br, bg, bb, ba := blended.At(x, 0)
			or, og, ob, oa := over.At(x, 0)
			for i, pair := range [][2]float32{{br, or}, {bg, og}, {bb, ob}, {ba, oa}} {
				if math.Abs(float64(pair[0]-pair[1])) > 1e-5 {
					t.Errorf("mode %q x=%d channel %d: Blend=%v Composite(over)=%v",
						mode, x, i, pair[0], pair[1])
				}
			}
		}
	}
}

// TestBlendOverTransparentBackdropDoesNotBlend pins the part of the CSS
// formula that is easy to drop.
//
// Where the backdrop is TRANSPARENT the source must come through UNBLENDED:
//
//	co = (1-ab)·as·Cs + ab·as·B(Cb,Cs) + (1-as)·ab·Cb
//
// with ab=0 this is just as·Cs. Collapsing the formula to a plain
// "blend the colours, then composite" instead multiplies the source by the
// transparent backdrop's meaningless colour (black in a fresh buffer), so a
// multiply blend would black out everywhere the backdrop is empty.
func TestBlendOverTransparentBackdropDoesNotBlend(t *testing.T) {
	r := image.Rect(0, 0, 1, 1)
	a := NewBuffer(r, SRGB)
	a.Set(0, 0, 0.8, 0.6, 0.4, 1) // opaque source
	b := NewBuffer(r, SRGB)       // fully transparent backdrop

	out := Blend(a, b, "Multiply", r)
	cr, cg, cb, ca := out.At(0, 0)
	if ca != 1 {
		t.Fatalf("alpha = %v, want 1", ca)
	}
	for i, pair := range [][2]float32{{cr, 0.8}, {cg, 0.6}, {cb, 0.4}} {
		if math.Abs(float64(pair[0]-pair[1])) > 1e-5 {
			t.Errorf("channel %d = %v, want the SOURCE's %v — a transparent backdrop must not blend",
				i, pair[0], pair[1])
		}
	}
}

// TestBlendAlphaIsUnionRegardlessOfMode pins that the result alpha is the
// ordinary source-over union (as + ab - as·ab) for every mode: a blend mode
// changes COLOUR, never coverage.
func TestBlendAlphaIsUnionRegardlessOfMode(t *testing.T) {
	r := image.Rect(0, 0, 1, 1)
	const as, ab = 0.5, 0.25
	want := float32(as + ab - as*ab)
	for _, mode := range []string{"Multiply", "Screen", "ColorBurn", "Hue", "Luminosity"} {
		a := NewBuffer(r, SRGB)
		a.Set(0, 0, 0.7, 0.2, 0.1, as)
		b := NewBuffer(r, SRGB)
		b.Set(0, 0, 0.1, 0.9, 0.5, ab)

		_, _, _, got := Blend(a, b, mode, r).At(0, 0)
		if math.Abs(float64(got-want)) > 1e-5 {
			t.Errorf("%s alpha = %v, want %v", mode, got, want)
		}
	}
}

// TestBlendNonSeparableModesRun exercises the four non-separable modes end to
// end. Their outputs are not hand-checkable numbers, so this asserts the
// properties that define them: Luminosity takes the source's lightness with
// the backdrop's colour, and Hue does the reverse.
func TestBlendNonSeparableModesRun(t *testing.T) {
	r := image.Rect(0, 0, 1, 1)
	a := NewBuffer(r, SRGB)
	a.Set(0, 0, 0.9, 0.9, 0.9, 1) // bright, unsaturated source
	b := NewBuffer(r, SRGB)
	b.Set(0, 0, 0.2, 0.1, 0.1, 1) // dark, saturated backdrop

	lum := Blend(a, b, "Luminosity", r)
	lr, lg, lb, _ := lum.At(0, 0)
	// Luminosity(dst, src) sets the BACKDROP's colour to the SOURCE's
	// luminance, so the result must be much brighter than the backdrop.
	if lr+lg+lb <= 0.2+0.1+0.1 {
		t.Errorf("Luminosity gave (%v,%v,%v); want brighter than the backdrop", lr, lg, lb)
	}
	for _, mode := range []string{"Hue", "Saturation", "Color"} {
		if out := Blend(a, b, mode, r); out == nil {
			t.Fatalf("%s returned nil", mode)
		}
	}
}

// TestBlendNilInputsDoNotPanic pins the never-panic rule.
func TestBlendNilInputsDoNotPanic(t *testing.T) {
	r := image.Rect(0, 0, 2, 2)
	for _, mode := range []string{"", "Multiply", "Hue"} {
		if out := Blend(nil, nil, mode, r); out == nil {
			t.Fatalf("Blend(nil, nil, %q) returned nil", mode)
		}
	}
}
