package draw

import (
	"image"
	"testing"
)

// renderRect renders a single 40x40 rect carrying the given `filter` property
// (and optional extra attributes), on a 100x100 white canvas.
func renderRect(t *testing.T, filterValue, extra string) *image.RGBA {
	t.Helper()
	src := `<svg ` + filterHdr + ` width="100" height="100">
	  <rect x="30" y="30" width="40" height="40" fill="rgb(46,139,87)" ` + extra + `
	        filter="` + filterValue + `"/>
	</svg>`
	img, _ := renderFilterSVG(t, src, 100, 100)
	return img
}

// TestFilterFunctionBlur pins that the CSS shorthand reaches the same
// feGaussianBlur the <filter> element does.
func TestFilterFunctionBlur(t *testing.T) {
	img := renderRect(t, "blur(4)", "")
	if got := alphaOfInk(img, 50, 50); got < 100 {
		t.Errorf("centre ink = %d, want the element still painted", got)
	}
	if got := alphaOfInk(img, 26, 50); got <= 0 {
		t.Errorf("no ink outside the rect; blur(4) did not blur")
	}
}

// TestFilterFunctionInvalidValuesRenderUnfiltered pins the invalid inputs the
// corpus tests hardest. Each must leave the element rendered EXACTLY as if it
// carried no filter at all — not blurred, and not dropped.
//
// The reference implementation renders every one of these as the plain rect,
// so a parser that accepted any of them would produce visible output where the
// reference has none.
func TestFilterFunctionInvalidValuesRenderUnfiltered(t *testing.T) {
	plain := renderRect(t, "none", "")

	cases := []struct {
		value string
		why   string
	}{
		{"blur(-5)", "a negative radius"},
		{"blur(50%)", "a percentage where a <length> is required"},
		{"blur(4 2)", "two values where one is expected"},
		{"hue-rotate(45)", "a CSS <angle> requires a unit"},
		{"hue-rotate(random)", "not a number"},
		{"grayscale() hue-rotate(random) opacity(0.5)", "one bad function invalidates the whole list"},
		{"drop-shadow()", "no offsets"},
		{"drop-shadow(4)", "only one offset"},
		{"drop-shadow(blue 4 5 6 7)", "too many values"},
		{"drop-shadow(red, 10, 15)", "comma-separated offsets"},
		{"drop-shadow(blue 3% 4% 5%)", "percentage offsets"},
		{"brightness(-1)", "a negative amount"},
	}
	for _, tc := range cases {
		got := renderRect(t, tc.value, "")
		diff := 0
		for y := 0; y < 100; y++ {
			for x := 0; x < 100; x++ {
				if got.RGBAAt(x, y) != plain.RGBAAt(x, y) {
					diff++
				}
			}
		}
		if diff != 0 {
			t.Errorf("filter=%q differs from unfiltered in %d pixels; it is invalid (%s) and must render unfiltered",
				tc.value, diff, tc.why)
		}
	}
}

// TestFilterFunctionValidEdgeValuesStillFilter is the counterweight to the
// invalid table: these LOOK like edge cases but are valid, so a parser that
// rejected too eagerly would render them unfiltered and be caught here rather
// than only by a golden.
func TestFilterFunctionValidEdgeValuesStillFilter(t *testing.T) {
	plain := renderRect(t, "none", "")
	for _, value := range []string{
		"blur(1mm)",              // an absolute unit IS a length
		"hue-rotate(45deg)",      // the unit form is valid
		"hue-rotate(0.25turn)",   // as are grad/rad/turn
		"grayscale(2)",           // above 1 is valid, and clamps in the lowering
		"drop-shadow(4 5 6 red)", // colour last
	} {
		got := renderRect(t, value, "")
		same := true
		for y := 0; y < 100 && same; y++ {
			for x := 0; x < 100; x++ {
				if got.RGBAAt(x, y) != plain.RGBAAt(x, y) {
					same = false
					break
				}
			}
		}
		if same {
			t.Errorf("filter=%q rendered identically to unfiltered; it is VALID and must take effect", value)
		}
	}
}

// TestFilterFunctionsComposeInSequence pins that a list chains: each function
// consumes the previous one's output.
//
// grayscale() then opacity(0.5) must be BOTH grey AND half-transparent. A
// lowering that dropped the chaining (leaving every function reading
// SourceGraphic) would apply only the last one, which is grey-free.
func TestFilterFunctionsComposeInSequence(t *testing.T) {
	both := renderRect(t, "grayscale() opacity(0.5)", "")
	c := both.RGBAAt(50, 50)

	// Grey: the channels converged.
	if spread := maxInt(int(c.R), int(c.G), int(c.B)) - minInt(int(c.R), int(c.G), int(c.B)); spread > 12 {
		t.Errorf("centre = %v (channel spread %d), want desaturated — grayscale() was not applied", c, spread)
	}
	// Half-transparent over white: much lighter than the opaque grey would be.
	greyOnly := renderRect(t, "grayscale()", "").RGBAAt(50, 50)
	if int(c.R) <= int(greyOnly.R) {
		t.Errorf("with opacity = %v, without = %v; opacity(0.5) was not applied on top of grayscale()", c, greyOnly)
	}
}

// TestDropShadowFunctionDefaultsToTheElementColour pins that an omitted colour
// means the element's own `color` property, not black.
//
// The corpus's drop-shadow-function-color-as-attribute fixture sets
// color="blue" and writes drop-shadow(10 20) with no colour, and the shadow
// comes out blue.
func TestDropShadowFunctionDefaultsToTheElementColour(t *testing.T) {
	img := renderRect(t, "drop-shadow(10 20)", `color="rgb(0,0,255)"`)
	// Below-right of the rect (which ends at 70,70), where only the shadow is.
	c := img.RGBAAt(60, 85)
	if c.B <= c.R || c.B <= c.G {
		t.Errorf("shadow at (60,85) = %v, want blue from the element's `color`", c)
	}

	// With no `color` at all the shadow is black.
	black := renderRect(t, "drop-shadow(10 20)", "")
	bc := black.RGBAAt(60, 85)
	if int(bc.R) > 200 || bc.R != bc.G || bc.G != bc.B {
		t.Errorf("shadow with no color = %v, want a neutral black shadow", bc)
	}
}

// TestFilterFunctionHasNoFilterRegion pins the corpus's
// drop-shadow-function-filter-region fixture, whose <desc> states it outright:
// "Filter function doesn't have a filter region, like the `filter` element."
//
// Applying the <filter> element's -10%/120% default here would clip a large
// blur to a box barely bigger than the element — which looks like a too-small
// blur rather than like a clip, and is easy to misdiagnose.
func TestFilterFunctionHasNoFilterRegion(t *testing.T) {
	img := renderRect(t, "drop-shadow(0 0 20)", `color="rgb(0,0,255)"`)
	// The rect is 30..70 with a bbox of 40, so the <filter> default region
	// would stop at 74. A 20-unit blur must reach well past that.
	if got := alphaOfInk(img, 85, 50); got <= 0 {
		t.Errorf("no ink at x=85; the shadow was clipped to a filter region it should not have")
	}
}

// TestFilterFunctionListWithAnUnresolvableURLKeepsTheRest pins that an
// unresolvable url() INSIDE A LIST is dropped while the surrounding functions
// still apply — the opposite of a bare filter="url(#missing)", which makes the
// element not render at all.
func TestFilterFunctionListWithAnUnresolvableURLKeepsTheRest(t *testing.T) {
	withMissing := renderRect(t, "grayscale(0.5) url(#missing) opacity(0.5)", "")
	withoutURL := renderRect(t, "grayscale(0.5) opacity(0.5)", "")

	if got := alphaOfInk(withMissing, 50, 50); got == 0 {
		t.Fatal("the element vanished; an unresolvable url() in a LIST must be dropped, not fatal")
	}
	diff := 0
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			if withMissing.RGBAAt(x, y) != withoutURL.RGBAAt(x, y) {
				diff++
			}
		}
	}
	if diff != 0 {
		t.Errorf("differs from the same list without the bad url() in %d pixels", diff)
	}
}

// TestFilterFunctionURLChainsIntoTheNextFunction pins that a referenced
// <filter> splices into the lowered graph with its indices SHIFTED and its
// SourceGraphic REBOUND, so `url(#a) grayscale()` greys a's output rather than
// the original element.
func TestFilterFunctionURLChainsIntoTheNextFunction(t *testing.T) {
	src := `<svg ` + filterHdr + ` width="100" height="100">
	  <filter id="f1"><feGaussianBlur stdDeviation="4"/></filter>
	  <rect x="30" y="30" width="40" height="40" fill="rgb(46,139,87)"
	        filter="url(#f1) grayscale()"/>
	</svg>`
	img, logs := renderFilterSVG(t, src, 100, 100)
	if len(logs) != 0 {
		t.Fatalf("logged a degradation: %v", logs)
	}
	c := img.RGBAAt(50, 50)
	// Blurred (the centre is still solid) AND grey (channels converged).
	if spread := maxInt(int(c.R), int(c.G), int(c.B)) - minInt(int(c.R), int(c.G), int(c.B)); spread > 12 {
		t.Errorf("centre = %v (spread %d), want grey — grayscale() did not consume the blur's output", c, spread)
	}
	if got := alphaOfInk(img, 26, 50); got <= 0 {
		t.Errorf("no ink outside the rect; the url(#f1) blur was dropped from the chain")
	}
}

func maxInt(vs ...int) int {
	m := vs[0]
	for _, v := range vs[1:] {
		if v > m {
			m = v
		}
	}
	return m
}

func minInt(vs ...int) int {
	m := vs[0]
	for _, v := range vs[1:] {
		if v < m {
			m = v
		}
	}
	return m
}
