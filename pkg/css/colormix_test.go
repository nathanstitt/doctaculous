package css

import (
	"image/color"
	"testing"
)

// rgba is a terse constructor for the expected values below.
func rgba(r, g, b, a uint8) color.RGBA { return color.RGBA{R: r, G: g, B: b, A: a} }

// TestColorMixMatchesChrome pins color-mix() against values CAPTURED FROM CHROME
// (rendered to a 1x1 canvas and read back), not derived here. That direction matters:
// a transposed conversion matrix or a swapped white point yields colours that look
// plausible and are quietly wrong, and a test written from the same arithmetic as the
// implementation would agree with the bug. Mixing red and blue lands on a different
// colour in every space — (128,0,128) in srgb, (188,0,188) in srgb-linear,
// (255,0,255) in hsl, (140,83,162) in oklab — so these cases also prove the space
// keyword is actually honored rather than silently ignored.
func TestColorMixMatchesChrome(t *testing.T) {
	cases := []struct {
		in   string
		want color.RGBA
	}{
		// The reported real-world case: a 24% blue wash over white.
		{"color-mix(in srgb, #4a90d9 24%, white)", rgba(212, 228, 246, 255)},
		{"color-mix(in srgb, red, blue)", rgba(128, 0, 128, 255)},
		{"color-mix(in srgb, red 30%, blue)", rgba(77, 0, 179, 255)},
		// Percentages summing to <100% normalize the weights AND scale the alpha.
		{"color-mix(in srgb, red 20%, blue 20%)", rgba(128, 0, 128, 102)},
		{"color-mix(in srgb-linear, red, blue)", rgba(188, 0, 188, 255)},
		{"color-mix(in hsl, red, blue)", rgba(255, 0, 255, 255)},
		{"color-mix(in hwb, red, blue)", rgba(255, 0, 255, 255)},
		{"color-mix(in lab, red, blue)", rgba(193, 0, 136, 255)},
		{"color-mix(in oklab, red, blue)", rgba(140, 83, 162, 255)},
		{"color-mix(in lch, red, blue)", rgba(245, 0, 134, 255)},
		{"color-mix(in oklch, red, blue)", rgba(186, 0, 194, 255)},
		{"color-mix(in xyz, red, blue)", rgba(188, 0, 188, 255)},
		{"color-mix(in xyz-d65, red, blue)", rgba(188, 0, 188, 255)},
		{"color-mix(in xyz-d50, red, blue)", rgba(188, 0, 188, 255)},
		{"color-mix(in oklch, white, black)", rgba(99, 99, 99, 255)},
		// Premultiplied alpha: the half-transparent red contributes proportionally
		// less hue, so this is NOT (128,0,128).
		{"color-mix(in srgb, rgba(255,0,0,0.5), blue)", rgba(85, 0, 170, 192)},
		// A 0% weight yields the other colour untouched, not black.
		{"color-mix(in srgb, red 0%, blue)", rgba(0, 0, 255, 255)},
		{"color-mix(in srgb, red 100%, blue)", rgba(255, 0, 0, 255)},
	}
	for _, c := range cases {
		got, ok := ParseColorValue(c.in)
		if !ok {
			t.Errorf("%s: did not parse", c.in)
			continue
		}
		if got != c.want {
			t.Errorf("%s = %v, want %v", c.in, got, c.want)
		}
	}
}

// All four hue-interpolation modes, again pinned to Chrome. Red->blue is the case
// that separates them: the short arc goes through magenta, the long one through
// green, and increasing/decreasing force a direction regardless of which is shorter.
func TestColorMixHueModes(t *testing.T) {
	cases := []struct {
		in   string
		want color.RGBA
	}{
		{"color-mix(in hsl, red, blue)", rgba(255, 0, 255, 255)},
		{"color-mix(in hsl shorter hue, red, blue)", rgba(255, 0, 255, 255)},
		{"color-mix(in hsl longer hue, red, blue)", rgba(0, 255, 0, 255)},
		{"color-mix(in lch longer hue, red, blue)", rgba(0, 130, 64, 255)},
		{"color-mix(in oklch increasing hue, red, blue)", rgba(0, 147, 0, 255)},
		{"color-mix(in oklch decreasing hue, red, blue)", rgba(186, 0, 194, 255)},
	}
	for _, c := range cases {
		got, ok := ParseColorValue(c.in)
		if !ok {
			t.Errorf("%s: did not parse", c.in)
			continue
		}
		if got != c.want {
			t.Errorf("%s = %v, want %v", c.in, got, c.want)
		}
	}
}

// Mixing with `transparent` preserves the opaque colour's channels EXACTLY and scales
// only its alpha, because premultiplication weights a zero-alpha colour's channels by
// zero. This is the one case where the engine deliberately does not match Chrome
// byte-for-byte: Chrome reports (75,142,217) — off by up to 2/255 — from rounding
// through an intermediate float space, while the exact premultiplied result is the
// input colour unchanged. Verified by hand: (0x4a/255*1*0.24 + 0)/0.24 == 0x4a/255.
//
// It also confirms the equivalence `color-mix(in srgb, X N%, transparent)` ==
// `rgba(X, N/100)`, which callers rely on as a substitution.
func TestColorMixWithTransparentIsExact(t *testing.T) {
	got, ok := ParseColorValue("color-mix(in srgb, #4a90d9 24%, transparent)")
	if !ok {
		t.Fatal("did not parse")
	}
	if want := rgba(0x4a, 0x90, 0xd9, 61); got != want {
		t.Errorf("got %v, want %v (channels must survive premultiplied mixing untouched)", got, want)
	}
	equiv, ok := ParseColorValue("rgba(74,144,217,0.24)")
	if !ok {
		t.Fatal("rgba equivalent did not parse")
	}
	if got != equiv {
		t.Errorf("color-mix with transparent = %v but the rgba() equivalent = %v; they must agree", got, equiv)
	}
}

// Malformed values fail to parse so the DECLARATION is dropped and the previous value
// stands, per CSS error handling. Silently substituting a wrong colour (or black)
// would be the failure mode this whole property was reported for.
func TestColorMixRejectsMalformed(t *testing.T) {
	bad := []string{
		"color-mix(in bogusspace, red, blue)",       // unknown interpolation space
		"color-mix(in srgb, red)",                   // only one colour
		"color-mix(in srgb, red, blue, green)",      // three colours
		"color-mix(red, blue)",                      // missing the "in <space>"
		"color-mix(in srgb, notacolor, blue)",       // unparseable component
		"color-mix(in srgb)",                        // no colours at all
		"color-mix(in srgb longer hue, red, blue)",  // hue mode on a rectangular space
		"color-mix(in lab longer hue, red, blue)",   // ditto
		"color-mix(in hsl sideways hue, red, blue)", // unknown hue mode
		"color-mix(in srgb, red -10%, blue)",        // negative percentage
		"color-mix(in srgb, red 150%, blue)",        // percentage above 100
		"color-mix(in srgb, red 0%, blue 0%)",       // both weights zero
		"color-mix(",                                // truncated
	}
	for _, s := range bad {
		if c, ok := ParseColorValue(s); ok {
			t.Errorf("%s parsed as %v, want a rejection so the declaration is dropped", s, c)
		}
	}
}

// A dropped color-mix() leaves the cascade's previous value in place rather than
// resetting the property — the end-to-end consequence of the rejections above.
func TestColorMixMalformedDropsDeclarationOnly(t *testing.T) {
	cs := initialStyle()
	applyDeclaration(&cs, Declaration{Property: "background-color", Value: "red"})
	applyDeclaration(&cs, Declaration{Property: "background-color", Value: "color-mix(in nope, red, blue)"})
	if want := rgba(255, 0, 0, 255); cs.BackgroundColor != want {
		t.Errorf("background-color = %v, want %v preserved after an invalid color-mix", cs.BackgroundColor, want)
	}
}

// color-mix() works wherever a colour does, because it is evaluated inside the single
// shared colour grammar rather than bolted onto one property. Nesting is likewise
// free: a color-mix() argument is parsed by that same grammar.
func TestColorMixComposesWithTheGrammar(t *testing.T) {
	cs := initialStyle()
	applyDeclaration(&cs, Declaration{Property: "color", Value: "color-mix(in srgb, red, blue)"})
	if want := rgba(128, 0, 128, 255); cs.Color != want {
		t.Errorf("color = %v, want %v", cs.Color, want)
	}
	// Nested: the inner mix is 50/50 red/blue, then mixed 50/50 with white.
	got, ok := ParseColorValue("color-mix(in srgb, color-mix(in srgb, red, blue), white)")
	if !ok {
		t.Fatal("nested color-mix did not parse")
	}
	if want := rgba(191, 128, 191, 255); got != want {
		t.Errorf("nested color-mix = %v, want %v", got, want)
	}
}

// The percentage may precede the colour, and a colour containing its own spaces and
// percent signs must not be mistaken for the weight.
func TestColorMixComponentSyntax(t *testing.T) {
	cases := []struct {
		in   string
		want color.RGBA
	}{
		{"color-mix(in srgb, 30% red, blue)", rgba(77, 0, 179, 255)},
		{"color-mix(in srgb, red, 70% blue)", rgba(77, 0, 179, 255)},
		{"color-mix(in srgb, rgb(255 0 0 / 100%) 30%, blue)", rgba(77, 0, 179, 255)},
		{"COLOR-MIX(IN SRGB, RED, BLUE)", rgba(128, 0, 128, 255)},
	}
	for _, c := range cases {
		got, ok := ParseColorValue(c.in)
		if !ok {
			t.Errorf("%s: did not parse", c.in)
			continue
		}
		if got != c.want {
			t.Errorf("%s = %v, want %v", c.in, got, c.want)
		}
	}
}
