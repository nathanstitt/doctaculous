package css

import (
	"math"
	"testing"
)

// TestNonFiniteNumbersRejected covers CSS numbers that strconv.ParseFloat is
// happy to return but the CSS grammar has no token for. An infinite flex-grow
// is not merely inaccurate: it makes the free-space distribution loop in
// layout/css never converge, and that path has no context check, so a caller's
// deadline cannot interrupt it. The layout hang was reachable from a 66-byte
// HTML file through OpenHTMLBytes.
//
// A rejected value leaves the property at its previous value, which is the
// same behaviour as any other malformed declaration.
func TestNonFiniteNumbersRejected(t *testing.T) {
	for _, prop := range []string{"flex-grow", "flex-shrink"} {
		for _, val := range []string{"inf", "Inf", "+Inf", "infinity", "INFINITY", "nan", "NaN", "-inf"} {
			t.Run(prop+":"+val, func(t *testing.T) {
				cs := initialStyle()
				// A known-good value first, so a rejected one is visible as
				// "unchanged" rather than "happens to match the initial value".
				applyOne(&cs, prop, "3")
				applyOne(&cs, prop, val)

				got := cs.FlexGrow
				if prop == "flex-shrink" {
					got = cs.FlexShrink
				}
				if math.IsInf(got, 0) || math.IsNaN(got) {
					t.Fatalf("%s:%s produced non-finite %v", prop, val, got)
				}
				if got != 3 {
					t.Errorf("%s:%s = %v, want the prior value 3 to survive", prop, val, got)
				}
			})
		}
	}
}

// TestNonFiniteNumberOverflow covers the same class arriving as a plain digit
// string. readNumeric only scans digits, "." and a leading "-", so "inf" can
// never be spelled through the tokenizer -- but a long enough run of digits
// still overflows float64 to +Inf, reaching the identical hang.
func TestNonFiniteNumberOverflow(t *testing.T) {
	huge := ""
	for range 400 {
		huge += "9"
	}
	cs := initialStyle()
	applyOne(&cs, "flex-grow", "3")
	applyOne(&cs, "flex-grow", huge)
	if math.IsInf(cs.FlexGrow, 0) || math.IsNaN(cs.FlexGrow) {
		t.Fatalf("flex-grow with 400 digits produced non-finite %v", cs.FlexGrow)
	}
}

// TestParseFloatNonFinite pins the tokenizer helper directly: every consumer of
// Token.Num depends on it never handing back a non-finite value.
func TestParseFloatNonFinite(t *testing.T) {
	huge := ""
	for range 400 {
		huge += "9"
	}
	cases := []struct {
		in   string
		want float64
	}{
		{"1", 1},
		{"1.5", 1.5},
		{"-2", -2},
		{"", 0},
		{"abc", 0},
		{huge, 0},
		{"-" + huge, 0},
		{"inf", 0},
		{"nan", 0},
	}
	for _, c := range cases {
		if got := parseFloat(c.in); got != c.want {
			t.Errorf("parseFloat(%.20q) = %v, want %v", c.in, got, c.want)
		}
	}
}
