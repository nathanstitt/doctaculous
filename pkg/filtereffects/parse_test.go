package filtereffects

import (
	"math"
	"strings"
	"testing"
)

// userUnits is a LengthResolver with SVG's rules: absolute units convert, a
// bare number is a user unit, and a PERCENTAGE is rejected (a percentage is
// not a <length>).
func userUnits(token string) (float64, bool) {
	token = strings.TrimSpace(token)
	if token == "" || strings.HasSuffix(token, "%") {
		return 0, false
	}
	for _, u := range []struct {
		suffix string
		scale  float64
	}{
		{"mm", 96 / 25.4}, {"cm", 96 / 2.54}, {"in", 96},
		{"pt", 96.0 / 72}, {"pc", 16}, {"px", 1}, {"em", 16}, {"ex", 8},
	} {
		if strings.HasSuffix(token, u.suffix) {
			n, ok := parseFinite(strings.TrimSuffix(token, u.suffix))
			if !ok {
				return 0, false
			}
			return n * u.scale, true
		}
	}
	return parseFinite(token)
}

func mustParse(t *testing.T, v string) []Function {
	t.Helper()
	f, ok := Parse(v, userUnits)
	if !ok {
		t.Fatalf("Parse(%q) reported the value INVALID; want it to parse", v)
	}
	return f
}

func mustNotParse(t *testing.T, v, why string) {
	t.Helper()
	if f, ok := Parse(v, userUnits); ok {
		t.Errorf("Parse(%q) = %+v, ok; want INVALID because %s", v, f, why)
	}
}

// TestParseNoneAndEmpty pins that the absent value is valid-with-no-filter,
// not an error.
func TestParseNoneAndEmpty(t *testing.T) {
	for _, v := range []string{"", "   ", "none"} {
		f, ok := Parse(v, userUnits)
		if !ok || len(f) != 0 {
			t.Errorf("Parse(%q) = (%v, %v), want (nil, true)", v, f, ok)
		}
	}
}

// TestParseBlur covers the whole blur() grammar, including the three invalid
// forms the corpus renders completely UNFILTERED.
func TestParseBlur(t *testing.T) {
	f := mustParse(t, "blur(4)")
	if len(f) != 1 || f[0].Kind != FuncBlur || f[0].StdDeviation != 4 {
		t.Errorf("blur(4) = %+v", f)
	}

	// An empty argument list is VALID and means a zero radius.
	f = mustParse(t, "blur()")
	if len(f) != 1 || f[0].StdDeviation != 0 {
		t.Errorf("blur() = %+v, want a valid zero-radius blur", f)
	}

	// mm is a real length and converts.
	f = mustParse(t, "blur(1mm)")
	if math.Abs(f[0].StdDeviation-96/25.4) > 1e-9 {
		t.Errorf("blur(1mm) = %v, want %v user units", f[0].StdDeviation, 96/25.4)
	}

	mustNotParse(t, "blur(-5)", "a negative radius is invalid")
	mustNotParse(t, "blur(50%)", "blur() takes a <length>, and a percentage is not one")
	mustNotParse(t, "blur(4 2)", "blur() takes exactly one value")
}

// TestParseColorAdjustFunctions covers the seven <number-percentage>
// functions, their per-function empty-argument defaults, and the values the
// corpus feeds them.
func TestParseColorAdjustFunctions(t *testing.T) {
	names := []struct {
		name string
		kind FunctionKind
	}{
		{"brightness", FuncBrightness}, {"contrast", FuncContrast},
		{"grayscale", FuncGrayscale}, {"sepia", FuncSepia},
		{"saturate", FuncSaturate}, {"invert", FuncInvert},
		{"opacity", FuncOpacity},
	}
	for _, n := range names {
		f := mustParse(t, n.name+"(0.5)")
		if f[0].Kind != n.kind || f[0].Amount != 0.5 {
			t.Errorf("%s(0.5) = %+v", n.name, f[0])
		}
		// A percentage is divided by 100.
		f = mustParse(t, n.name+"(50%)")
		if math.Abs(f[0].Amount-0.5) > 1e-9 {
			t.Errorf("%s(50%%) amount = %v, want 0.5", n.name, f[0].Amount)
		}
		// An empty argument list takes the function's own default, which is 1
		// for all seven.
		f = mustParse(t, n.name+"()")
		if f[0].Amount != 1 {
			t.Errorf("%s() amount = %v, want 1", n.name, f[0].Amount)
		}
		// Above 1 is VALID and NOT clamped here: the corpus's
		// color-adjust-functions-2 and -200percent fixtures both render, and
		// clamping at parse time would quietly turn brightness(2) into
		// brightness(1). Any per-function clamping belongs to the lowering.
		f = mustParse(t, n.name+"(2)")
		if f[0].Amount != 2 {
			t.Errorf("%s(2) amount = %v, want 2 (unclamped at parse time)", n.name, f[0].Amount)
		}
		mustNotParse(t, n.name+"(-1)", "a negative amount is invalid")
		mustNotParse(t, n.name+"(1 2)", "one value is expected")
	}
}

// TestParseHueRotate pins that CSS requires a UNIT on an <angle>.
//
// The bare-number case is the discriminator: SVG's own feColorMatrix
// hueRotate `values` IS a bare number, so accepting one here is the natural
// mistake — and the corpus's hue-rotate-function-45 fixture renders completely
// unfiltered, proving the CSS function grammar differs.
func TestParseHueRotate(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"hue-rotate(45deg)", 45},
		{"hue-rotate(45grad)", 45 * 360.0 / 400},
		{"hue-rotate(45rad)", 45 * 180 / math.Pi},
		{"hue-rotate(0.25turn)", 90},
		{"hue-rotate(999deg)", 999},
		{"hue-rotate()", 0},
		{"hue-rotate(0)", 0}, // a unitless ZERO is allowed for every dimension
	}
	for _, tc := range cases {
		f := mustParse(t, tc.in)
		if f[0].Kind != FuncHueRotate || math.Abs(f[0].Angle-tc.want) > 1e-9 {
			t.Errorf("%s angle = %v, want %v", tc.in, f[0].Angle, tc.want)
		}
	}
	mustNotParse(t, "hue-rotate(45)", "CSS <angle> requires a unit")
	mustNotParse(t, "hue-rotate(random)", "not a number at all")
}

// TestParseHueRotateGradIsNotRad guards the suffix-matching order: "grad" ends
// in "rad", so a shortest-first scan parses 45grad as 45g radians. The two
// differ by a factor of ~63, which is visible but easy to write.
func TestParseHueRotateGradIsNotRad(t *testing.T) {
	grad := mustParse(t, "hue-rotate(100grad)")[0].Angle
	if math.Abs(grad-90) > 1e-9 {
		t.Errorf("100grad = %v degrees, want 90 — the suffix scan matched \"rad\" inside \"grad\"", grad)
	}
}

// TestParseDropShadow covers the colour-first and colour-last forms, the
// optional blur radius, and every invalid shape the corpus renders unfiltered.
func TestParseDropShadow(t *testing.T) {
	f := mustParse(t, "drop-shadow(blue 4 5 6)")
	if f[0].Kind != FuncDropShadow || f[0].Color != "blue" ||
		f[0].Dx != 4 || f[0].Dy != 5 || f[0].StdDeviation != 6 {
		t.Errorf("colour-first form = %+v", f[0])
	}

	// The colour may come LAST instead.
	f = mustParse(t, "drop-shadow(4 5 6 blue)")
	if f[0].Color != "blue" || f[0].Dx != 4 || f[0].Dy != 5 || f[0].StdDeviation != 6 {
		t.Errorf("colour-last form = %+v", f[0])
	}

	// The blur radius is optional; two lengths is the minimum.
	f = mustParse(t, "drop-shadow(10 15)")
	if f[0].Dx != 10 || f[0].Dy != 15 || f[0].StdDeviation != 0 || f[0].Color != "" {
		t.Errorf("two-length form = %+v", f[0])
	}

	// currentColor is passed through UNPARSED for the caller to resolve; this
	// package has no notion of the element's `color`.
	f = mustParse(t, "drop-shadow(currentColor 10 20)")
	if f[0].Color != "currentColor" {
		t.Errorf("currentColor = %q, want it passed through verbatim", f[0].Color)
	}

	// em resolves through the caller's resolver (16 per em here).
	f = mustParse(t, "drop-shadow(blue 0.2em 0.3em 0.1em)")
	if math.Abs(f[0].Dx-3.2) > 1e-9 || math.Abs(f[0].Dy-4.8) > 1e-9 {
		t.Errorf("em form = %+v, want dx=3.2 dy=4.8", f[0])
	}

	mustNotParse(t, "drop-shadow()", "at least two lengths are required")
	mustNotParse(t, "drop-shadow(4)", "one length is not enough")
	mustNotParse(t, "drop-shadow(blue 4 5 6 7)", "four lengths is too many")
	mustNotParse(t, "drop-shadow(red, 10, 15)", "the offsets are space-separated, not comma-separated")
	mustNotParse(t, "drop-shadow(blue 3% 4% 5%)", "dx/dy are <length>, and a percentage is not one")
}

// TestParseDropShadowKeepsAColourFunctionIntact pins that a parenthesised
// colour survives the token split — rgb(1, 2, 3) contains the very commas and
// spaces the splitter breaks on.
func TestParseDropShadowKeepsAColourFunctionIntact(t *testing.T) {
	f := mustParse(t, "drop-shadow(rgb(10, 20, 30) 4 5)")
	if f[0].Color != "rgb(10, 20, 30)" {
		t.Errorf("colour = %q, want the whole rgb() call", f[0].Color)
	}
	if f[0].Dx != 4 || f[0].Dy != 5 {
		t.Errorf("offsets = (%v, %v), want (4, 5)", f[0].Dx, f[0].Dy)
	}
}

// TestParseComposesSeveralFunctions pins that a list parses in order — the
// caller chains each function's output into the next.
func TestParseComposesSeveralFunctions(t *testing.T) {
	f := mustParse(t, "grayscale() opacity(0.5)")
	if len(f) != 2 || f[0].Kind != FuncGrayscale || f[1].Kind != FuncOpacity {
		t.Fatalf("got %+v, want [grayscale, opacity]", f)
	}
	if f[1].Amount != 0.5 {
		t.Errorf("opacity amount = %v, want 0.5", f[1].Amount)
	}
}

// TestParseUnknownFunctionInvalidatesTheWholeList is the CSS error-handling
// rule, and it is the one a partial-list fallback gets wrong.
//
// The corpus's one-invalid-function-in-list fixture writes
// `grayscale() hue-rotate(random) opacity(0.5)` and renders the plain,
// COMPLETELY UNFILTERED rect — not a greyed and half-transparent one. A parser
// that returned the two good functions would render that fixture visibly
// filtered.
func TestParseUnknownFunctionInvalidatesTheWholeList(t *testing.T) {
	mustNotParse(t, "grayscale() hue-rotate(random) opacity(0.5)", "one bad function invalidates the declaration")
	mustNotParse(t, "grayscale() notafunction(1)", "an unknown function name is invalid")
	mustNotParse(t, "blur", "a bare token is not a function call")
	mustNotParse(t, "blur(4", "an unbalanced paren is invalid")
}

// TestParseURLEntries pins that url() is part of the same list grammar, and
// that its reference is handed back UNRESOLVED — this package has no document
// to resolve against.
func TestParseURLEntries(t *testing.T) {
	f := mustParse(t, "url(#filter1)")
	if len(f) != 1 || f[0].Kind != FuncURL || f[0].Ref != "url(#filter1)" {
		t.Errorf("url(#filter1) = %+v", f)
	}

	f = mustParse(t, "url(#a) url(#b)")
	if len(f) != 2 || f[0].Ref != "url(#a)" || f[1].Ref != "url(#b)" {
		t.Errorf("two urls = %+v", f)
	}

	f = mustParse(t, "url(#filter1) grayscale()")
	if len(f) != 2 || f[0].Kind != FuncURL || f[1].Kind != FuncGrayscale {
		t.Errorf("url + function = %+v", f)
	}

	mustNotParse(t, "url()", "an empty reference is invalid")
}

// TestParseIsCaseInsensitiveOnFunctionNames pins CSS's ASCII
// case-insensitivity for identifiers.
func TestParseIsCaseInsensitiveOnFunctionNames(t *testing.T) {
	f := mustParse(t, "BLUR(4) GrayScale()")
	if len(f) != 2 || f[0].Kind != FuncBlur || f[1].Kind != FuncGrayscale {
		t.Errorf("got %+v, want [blur, grayscale]", f)
	}
}

// TestParseRejectsNonFiniteNumbers pins that NaN and the infinities — which
// strconv accepts by their Go spellings, and which an overflowing literal like
// 1e400 also produces — never reach downstream pixel math.
func TestParseRejectsNonFiniteNumbers(t *testing.T) {
	for _, v := range []string{
		"blur(NaN)", "blur(Inf)", "blur(+Inf)", "blur(infinity)", "blur(1e400)",
		"hue-rotate(NaNdeg)", "opacity(nan)",
	} {
		mustNotParse(t, v, "a non-finite number must never reach pixel math")
	}
}
