// Package filtereffects parses the CSS `filter` shorthand — the
// `blur()`/`drop-shadow()`/`grayscale()`… function list from the Filter
// Effects specification — into a neutral description a caller lowers into
// filter primitives.
//
// It is DELIBERATELY SHARED rather than living inside pkg/svg. The `filter`
// property is one property with one grammar, used by SVG elements (where it is
// a presentation attribute) and by HTML/CSS boxes alike; the SVG side is
// implemented here first because the SVG corpus tests the grammar's edge cases
// hardest, and the HTML/CSS side consumes the same parser rather than growing
// a second one. That is why this package knows nothing about SVG scenes, CSS
// boxes, colors-as-typed-values, or rendering: it takes a string and returns
// the parsed functions, and each caller resolves colors and lengths in its own
// terms.
//
// Every function's spec-defined lowering to an equivalent primitive chain
// belongs to the CALLER, not here, so the two callers cannot disagree about
// the grammar while still lowering into their own primitive representations.
package filtereffects

import (
	"math"
	"strconv"
	"strings"
)

// FunctionKind identifies one CSS filter function.
type FunctionKind int

const (
	// FuncURL is a `url(#id)` reference to an SVG <filter> element. It is
	// part of the same list grammar as the shorthand functions, so it is
	// parsed here rather than sniffed for separately by each caller.
	FuncURL FunctionKind = iota
	// FuncBlur is blur(<length>), lowering to feGaussianBlur.
	FuncBlur
	// FuncDropShadow is drop-shadow(<color>? <length>{2,3}), lowering to the
	// feDropShadow shorthand.
	FuncDropShadow
	// FuncBrightness is brightness(<number-percentage>), lowering to an
	// feComponentTransfer with linear transfer functions — expressed here as
	// a colour matrix scaling each channel, which is equivalent and needs no
	// feComponentTransfer implementation.
	FuncBrightness
	// FuncContrast is contrast(<number-percentage>).
	FuncContrast
	// FuncGrayscale is grayscale(<number-percentage>), lowering to
	// feColorMatrix type="saturate" with 1-amount.
	FuncGrayscale
	// FuncSepia is sepia(<number-percentage>), lowering to a fixed
	// feColorMatrix interpolated toward the identity by 1-amount.
	FuncSepia
	// FuncSaturate is saturate(<number-percentage>), lowering to
	// feColorMatrix type="saturate".
	FuncSaturate
	// FuncHueRotate is hue-rotate(<angle>), lowering to feColorMatrix
	// type="hueRotate".
	FuncHueRotate
	// FuncInvert is invert(<number-percentage>).
	FuncInvert
	// FuncOpacity is opacity(<number-percentage>), lowering to an
	// feComponentTransfer on alpha — expressed here as a colour matrix
	// scaling alpha.
	FuncOpacity
)

// Function is one parsed entry of a `filter` list.
type Function struct {
	Kind FunctionKind

	// Ref is FuncURL's raw reference text, exactly as written including the
	// `url(...)` wrapper, for the caller to resolve against its own document.
	// Resolution cannot happen here: this package has no document.
	Ref string

	// Amount is the resolved <number-percentage> for the colour-adjust
	// functions (brightness, contrast, grayscale, sepia, saturate, invert,
	// opacity), with a percentage already divided by 100.
	//
	// It is NOT clamped. brightness(2) and grayscale(2) are both in the
	// corpus and both render as the raw amount evaluates through the matrix,
	// with only the final channel values clamped — clamping the amount here
	// would quietly turn brightness(2) into brightness(1).
	Amount float64

	// Angle is FuncHueRotate's angle in DEGREES, with deg/grad/rad/turn units
	// already converted.
	Angle float64

	// StdDeviation is FuncBlur's and FuncDropShadow's blur radius as a LENGTH
	// in the units the caller's length resolver produced (user units for SVG).
	StdDeviation float64

	// Dx, Dy are FuncDropShadow's offsets, likewise resolved lengths.
	Dx, Dy float64

	// Color is FuncDropShadow's raw colour token ("" when the function
	// omitted one, in which case the caller substitutes its own `color`
	// property per the spec). It is unparsed for the same reason Ref is:
	// colour parsing belongs to the caller's own colour grammar, including
	// `currentColor`.
	Color string
}

// LengthResolver converts one CSS length token (a number with an optional unit
// suffix) into the caller's own coordinate units, reporting ok=false for a
// value this context rejects.
//
// It is a parameter rather than built in because the two callers resolve
// lengths against different bases: SVG resolves `em` against the element's
// font size and rejects a percentage outright (the corpus's
// blur-function-percent-value fixture renders UNFILTERED, because `blur()`
// takes a <length> and a percentage is not one), while CSS boxes resolve
// percentages against a containing block. Handing the resolver in keeps that
// difference at the call site instead of encoding one context's rules here.
type LengthResolver func(token string) (float64, bool)

// Parse parses a `filter` property value into its function list.
//
// ok=false means the value is INVALID as a whole. Per CSS error handling an
// invalid value makes the declaration ignored — the element renders with no
// filter at all rather than with the functions that did parse. The corpus pins
// this precisely: `grayscale() hue-rotate(random) opacity(0.5)` renders
// completely UNFILTERED, not as grayscale-plus-opacity with the bad function
// skipped. A caller must therefore not fall back to a partial list.
//
// An empty value, "none", or a whitespace-only value returns (nil, true): no
// filter, validly.
//
// A `url()` entry naming nothing resolvable is NOT this function's business
// and does not make the list invalid — the corpus's one-invalid-url-in-list
// fixture keeps the surrounding grayscale and opacity, so an unresolvable
// reference is dropped by the caller at resolution time while the list stays
// valid here.
func Parse(value string, resolve LengthResolver) ([]Function, bool) {
	value = strings.TrimSpace(value)
	if value == "" || value == "none" {
		return nil, true
	}

	var out []Function
	for i := 0; i < len(value); {
		// Skip separators. Commas are not part of the `filter` grammar
		// (the list is space-separated), but tolerating them costs nothing
		// and no fixture depends on rejecting one BETWEEN functions.
		if isSpaceOrComma(value[i]) {
			i++
			continue
		}
		open := strings.IndexByte(value[i:], '(')
		if open < 0 {
			return nil, false // a bare token that is not a function call
		}
		name := strings.ToLower(strings.TrimSpace(value[i : i+open]))
		// Find the MATCHING close paren, counting nesting so a colour
		// function inside drop-shadow (rgb(1,2,3)) does not terminate it
		// early.
		body, end, ok := readBalanced(value, i+open)
		if !ok {
			return nil, false
		}
		f, ok := parseOne(name, body, resolve)
		if !ok {
			return nil, false
		}
		out = append(out, f)
		i = end
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

// isSpaceOrComma reports whether c separates two entries of the list.
func isSpaceOrComma(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == ','
}

// readBalanced returns the text between the '(' at index open and its matching
// ')', plus the index just past that ')'. ok=false for an unbalanced value,
// which makes the whole declaration invalid.
func readBalanced(s string, open int) (body string, end int, ok bool) {
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[open+1 : i], i + 1, true
			}
		}
	}
	return "", 0, false
}

// parseOne parses one already-split function name and body.
func parseOne(name, body string, resolve LengthResolver) (Function, bool) {
	switch name {
	case "url":
		ref := strings.TrimSpace(body)
		if ref == "" {
			return Function{}, false
		}
		return Function{Kind: FuncURL, Ref: "url(" + body + ")"}, true
	case "blur":
		return parseBlur(body, resolve)
	case "drop-shadow":
		return parseDropShadow(body, resolve)
	case "hue-rotate":
		return parseHueRotate(body)
	}
	kind, def, ok := colorAdjustKind(name)
	if !ok {
		return Function{}, false
	}
	return parseColorAdjust(kind, def, body)
}

// colorAdjustKind maps a colour-adjust function name to its kind and the
// amount an EMPTY argument list means.
//
// The defaults are the spec's and they are not uniform: the "do nothing"
// amount differs per function (grayscale() with no argument means FULL
// greyscale, while brightness() with none means unchanged), so a single
// shared default would be wrong for half of them. The corpus's
// color-adjust-functions-default-value fixture exercises all seven at once.
func colorAdjustKind(name string) (FunctionKind, float64, bool) {
	switch name {
	case "brightness":
		return FuncBrightness, 1, true
	case "contrast":
		return FuncContrast, 1, true
	case "grayscale":
		return FuncGrayscale, 1, true
	case "sepia":
		return FuncSepia, 1, true
	case "saturate":
		return FuncSaturate, 1, true
	case "invert":
		return FuncInvert, 1, true
	case "opacity":
		return FuncOpacity, 1, true
	}
	return 0, 0, false
}

// parseColorAdjust parses a <number-percentage> argument, using def for an
// empty body.
func parseColorAdjust(kind FunctionKind, def float64, body string) (Function, bool) {
	body = strings.TrimSpace(body)
	if body == "" {
		return Function{Kind: kind, Amount: def}, true
	}
	if strings.ContainsAny(body, " \t\n\r\f,") {
		return Function{}, false // more than one value where one is expected
	}
	n, ok := parseNumberPercentage(body)
	if !ok {
		return Function{}, false
	}
	return Function{Kind: kind, Amount: n}, true
}

// parseNumberPercentage parses a <number-percentage>, dividing a percentage by
// 100. A negative value is REJECTED (making the whole declaration invalid),
// which is what the corpus's color-adjust-functions-negative fixture asserts.
func parseNumberPercentage(s string) (float64, bool) {
	pct := strings.HasSuffix(s, "%")
	n, ok := parseFinite(strings.TrimSuffix(s, "%"))
	if !ok {
		return 0, false
	}
	if pct {
		n /= 100
	}
	if n < 0 {
		return 0, false
	}
	return n, true
}

// parseBlur parses blur(<length>).
//
// An empty body means a zero radius (no blur, but a VALID declaration), while
// a negative radius, a percentage, or two values are all invalid and take the
// whole declaration down — the corpus renders each of those three fixtures
// completely unfiltered.
func parseBlur(body string, resolve LengthResolver) (Function, bool) {
	body = strings.TrimSpace(body)
	if body == "" {
		return Function{Kind: FuncBlur, StdDeviation: 0}, true
	}
	if len(strings.FieldsFunc(body, isSpaceOrCommaRune)) != 1 {
		return Function{}, false
	}
	v, ok := resolve(body)
	if !ok || v < 0 {
		return Function{}, false
	}
	return Function{Kind: FuncBlur, StdDeviation: v}, true
}

// parseHueRotate parses hue-rotate(<angle>), accepting the bare number SVG
// allows alongside deg/grad/rad/turn. An empty body means 0 (no rotation).
func parseHueRotate(body string) (Function, bool) {
	body = strings.TrimSpace(body)
	if body == "" {
		return Function{Kind: FuncHueRotate, Angle: 0}, true
	}
	if len(strings.FieldsFunc(body, isSpaceOrCommaRune)) != 1 {
		return Function{}, false
	}
	deg, ok := parseAngleDegrees(body)
	if !ok {
		return Function{}, false
	}
	return Function{Kind: FuncHueRotate, Angle: deg}, true
}

// parseAngleDegrees converts one CSS <angle> to degrees.
//
// A UNITLESS number is REJECTED unless it is zero. CSS requires a unit on an
// <angle> — `hue-rotate(45)` is not `hue-rotate(45deg)` — and the corpus pins
// it: hue-rotate-function-45 renders completely UNFILTERED while every unit
// form (45deg, 45grad, 45rad, 0.25turn) rotates. This is the one place SVG's
// own presentation-attribute grammar (where feColorMatrix's hueRotate values
// IS a bare number) and the CSS function grammar disagree, so accepting a bare
// number here to match the attribute would render a fixture the reference
// leaves alone.
//
// Zero is exempt because CSS allows a unitless zero for every dimension, and
// it is the identity either way.
//
// The suffixes are checked LONGEST FIRST: "grad" ends in "rad", so testing
// "rad" first would parse 45grad as 45g radians and silently produce a
// different angle.
func parseAngleDegrees(s string) (float64, bool) {
	lower := strings.ToLower(s)
	for _, u := range []struct {
		suffix string
		scale  float64
	}{
		{"turn", 360},
		{"grad", 360.0 / 400},
		{"deg", 1},
		{"rad", 180 / math.Pi},
	} {
		if strings.HasSuffix(lower, u.suffix) {
			n, ok := parseFinite(lower[:len(lower)-len(u.suffix)])
			if !ok {
				return 0, false
			}
			return n * u.scale, true
		}
	}
	n, ok := parseFinite(lower)
	if !ok || n != 0 {
		return 0, false
	}
	return 0, true
}

// parseDropShadow parses drop-shadow(<color>? <length>{2,3} <color>?).
//
// The colour may appear FIRST or LAST (both forms are in the corpus), and it
// is left unparsed for the caller — see Function.Color. Exactly two or three
// lengths are required: none, one, four, or a comma-separated list are all
// invalid, and each of those is a corpus fixture that renders unfiltered.
func parseDropShadow(body string, resolve LengthResolver) (Function, bool) {
	if strings.Contains(body, ",") {
		// The corpus's comma-separated fixture renders UNFILTERED, so a comma
		// inside drop-shadow is a hard error rather than a tolerated
		// separator. (A comma inside a colour function is impossible to reach
		// here: it would sit inside its own parens, which are consumed by
		// splitTokens below — hence this check runs on the RAW body only
		// after that possibility is excluded.)
		if !strings.Contains(body, "(") {
			return Function{}, false
		}
	}
	toks := splitTokens(body)
	if len(toks) == 0 {
		return Function{}, false
	}

	color := ""
	// A leading token that is not a length is the colour.
	if _, ok := resolve(toks[0]); !ok {
		color, toks = toks[0], toks[1:]
	} else if len(toks) > 1 {
		// A trailing token that is not a length is likewise the colour.
		if _, ok := resolve(toks[len(toks)-1]); !ok {
			color, toks = toks[len(toks)-1], toks[:len(toks)-1]
		}
	}
	if len(toks) < 2 || len(toks) > 3 {
		return Function{}, false
	}

	vals := make([]float64, len(toks))
	for i, t := range toks {
		v, ok := resolve(t)
		if !ok {
			return Function{}, false
		}
		vals[i] = v
	}
	f := Function{Kind: FuncDropShadow, Dx: vals[0], Dy: vals[1], Color: color}
	if len(vals) == 3 {
		if vals[2] < 0 {
			return Function{}, false
		}
		f.StdDeviation = vals[2]
	}
	return f, true
}

// splitTokens splits a function body on whitespace and commas, keeping a
// parenthesised group (a colour function like rgb(1, 2, 3)) intact.
func splitTokens(body string) []string {
	var out []string
	depth, start := 0, -1
	for i := 0; i < len(body); i++ {
		c := body[i]
		switch c {
		case '(':
			depth++
		case ')':
			depth--
		}
		if depth == 0 && isSpaceOrComma(c) {
			if start >= 0 {
				out = append(out, body[start:i])
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		out = append(out, body[start:])
	}
	return out
}

// isSpaceOrCommaRune is isSpaceOrComma for strings.FieldsFunc.
func isSpaceOrCommaRune(r rune) bool {
	return r < 128 && isSpaceOrComma(byte(r))
}

// parseFinite parses a plain number, rejecting NaN and infinities (which
// strconv accepts by their Go spellings and which an overflowing literal like
// "1e400" also produces) so they can never reach downstream pixel math.
func parseFinite(s string) (float64, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return v, true
}
