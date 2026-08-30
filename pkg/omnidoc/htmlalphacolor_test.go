package omnidoc

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

// alphaProbeHTML places a 120x60 swatch of `decl` on an opaque backdrop, with no
// margin so the swatch's own origin is the page origin. The backdrop is a solid
// mid-grey rather than white so a blend is distinguishable in BOTH directions:
// over white, a lost alpha and a lost colour can look alike.
func alphaProbeHTML(decl string) string {
	return `<html><body style="margin:0;background:#808080">` +
		`<div style="width:120px;height:60px;background-color:` + decl + `"></div>` +
		`</body></html>`
}

// alphaProbeInk samples well inside the 120x60 swatch, away from every edge.
func alphaProbeInk(img *image.RGBA) color.RGBA { return img.RGBAAt(60, 30) }

// TestHTMLAlphaColorComposites is the end-to-end proof that a parsed alpha is a
// LIVE alpha: it drives real HTML through parse → cascade → layout → paint →
// raster and asserts on the composited pixel.
//
// This is the regression that motivated the shared colour grammar. Before it, an
// alpha-bearing value failed the cascade's parser, the whole declaration was
// dropped per CSS error handling, and the element painted NOTHING — measured at
// zero non-backdrop pixels where the same box with an opaque colour painted a
// full swatch. A test that only checked "something was painted" would have passed
// once parsing was fixed even if the alpha were being flattened to opaque, so the
// expectations below are the source-over blend computed by hand:
//
//	out = round(src*a + dst*(1-a))   over the #808080 (128,128,128) backdrop
//
// For src #4f9cff = (79,156,255) at a = 0x59/255 = 0.35 (35 %):
//
//	R = 79*0.35  + 128*0.65 = 27.7 + 83.2  = 110.9 -> 111
//	G = 156*0.35 + 128*0.65 = 54.6 + 83.2  = 137.8 -> 138
//	B = 255*0.35 + 128*0.65 = 89.3 + 83.2  = 172.5 -> 172/173
//
// Half-alpha black is the cleanest single check: 0*0.5 + 128*0.5 = 64.
func TestHTMLAlphaColorComposites(t *testing.T) {
	t.Parallel()
	// Guard the sample point first: if it ever drifts off the swatch, every
	// expectation below would silently be measuring the backdrop instead.
	if got, want := alphaProbeInk(rasterHTML(t, alphaProbeHTML("#4f9cff"))), (color.RGBA{0x4f, 0x9c, 0xff, 0xff}); got != want {
		t.Fatalf("opaque probe sampled %v at (60,30), want the swatch's own %v — the sample point has drifted", got, want)
	}

	const blend35 = 0x59 // the alpha byte the 35 % spellings all resolve to

	for _, tc := range []struct {
		decl string
		want color.RGBA
	}{
		// Half-alpha black over mid-grey: the unambiguous blend check. Neither
		// "opaque" (0,0,0) nor "nothing painted" (128,128,128) can be mistaken
		// for the correct (64,64,64).
		{"rgba(0,0,0,0.5)", color.RGBA{64, 64, 64, 255}},
		{"#00000080", color.RGBA{64, 64, 64, 255}},

		// The same 35 %-alpha brand blue in every CSS Color 4 spelling. They
		// must all land on the same composited pixel, which is what proves the
		// spellings agree rather than merely each parsing.
		{"rgba(79,156,255,0.35)", color.RGBA{111, 138, 173, 255}},
		{"#4f9cff59", color.RGBA{111, 138, 173, 255}},
		{"rgb(79 156 255 / 35%)", color.RGBA{111, 138, 173, 255}},

		// Opaque forms must NOT be blended — the alpha channel has to be
		// carried faithfully in both directions, not merely made non-opaque.
		{"hsl(210,100%,50%)", color.RGBA{0, 128, 255, 255}},
		{"rebeccapurple", color.RGBA{0x66, 0x33, 0x99, 255}},

		// alpha 0 and alpha 1 are the endpoints: fully transparent must leave
		// the backdrop untouched, fully opaque must replace it outright.
		{"rgba(255,0,0,0)", color.RGBA{128, 128, 128, 255}},
		{"rgba(255,0,0,1)", color.RGBA{255, 0, 0, 255}},
	} {
		got := alphaProbeInk(rasterHTML(t, alphaProbeHTML(tc.decl)))
		if !closeRGBA(got, tc.want, 2) {
			t.Errorf("background-color:%s sampled %v at (60,30), want %v", tc.decl, got, tc.want)
		}
	}

	// The hex and functional 35 % spellings must be not merely close but
	// pixel-identical to each other, since they name the same colour.
	ref := rasterHTML(t, alphaProbeHTML("#4f9cff59"))
	for _, decl := range []string{
		"rgba(79,156,255,0.35)",
		"rgb(79 156 255 / 35%)",
		"rgba(79 156 255 / 35%)",
		"rgba(31%,61.2%,100%,0.35)",
	} {
		if !bytes.Equal(ref.Pix, rasterHTML(t, alphaProbeHTML(decl)).Pix) {
			t.Errorf("%s did not render identically to the equivalent #4f9cff%02x", decl, blend35)
		}
	}
}

// TestHTMLMalformedColorDropsDeclaration is the honest-degradation half. CSS
// requires a declaration whose value does not parse to be ignored ENTIRELY,
// leaving whatever the cascade decided before it. The failure mode this guards
// against is a parser that returns a zero color.RGBA alongside ok=true: that
// would paint transparent black, which over most backdrops is indistinguishable
// from "nothing happened" by eye but is a different pixel from the correct one.
func TestHTMLMalformedColorDropsDeclaration(t *testing.T) {
	t.Parallel()
	// The earlier declaration is opaque red; the later, malformed one must not
	// disturb it.
	probe := func(bad string) string {
		return `<html><body style="margin:0;background:#808080">` +
			`<div style="width:120px;height:60px;background-color:#ff0000;background-color:` + bad + `"></div>` +
			`</body></html>`
	}
	red := color.RGBA{255, 0, 0, 255}
	for _, bad := range []string{
		"rgb(nope,0,0)",
		"#12345",
		"#gg0000",
		"hsl(210,100,50)", // hsl saturation/lightness must be percentages
		"rgb(0 0 0 /)",
		"notacolor",
		"url(#grad)",
	} {
		if got := alphaProbeInk(rasterHTML(t, probe(bad))); got != red {
			t.Errorf("background-color:%s sampled %v, want the earlier %v to stand (malformed values must drop)", bad, got, red)
		}
	}

	// `transparent` is a VALID colour, not a parse failure: it must win over the
	// earlier red and paint nothing, letting the backdrop through.
	if got, want := alphaProbeInk(rasterHTML(t, probe("transparent"))), (color.RGBA{128, 128, 128, 255}); got != want {
		t.Errorf("background-color:transparent sampled %v, want the backdrop %v", got, want)
	}
}
