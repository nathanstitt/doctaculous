package css

import "testing"

func TestParseLength(t *testing.T) {
	cases := []struct {
		in   string
		val  float64
		unit LengthUnit
		ok   bool
	}{
		{"12px", 12, UnitPx, true},
		{"1.5em", 1.5, UnitEm, true},
		{"50%", 50, UnitPercent, true},
		{"0", 0, UnitPx, true}, // unitless zero is a length of 0
		{"10pt", 10, UnitPt, true},
		{"auto", 0, UnitAuto, true},
		{"red", 0, UnitPx, false},  // not a length
		{"5", 0, UnitPx, false},    // non-zero unitless number is not a length
		{"10vw", 0, UnitPx, false}, // unrecognized unit is not a length
	}
	for _, c := range cases {
		got, ok := parseLength(newTokenizer(c.in).next())
		if ok != c.ok {
			t.Fatalf("parseLength(%q) ok = %v, want %v", c.in, ok, c.ok)
		}
		if ok && (got.Value != c.val || got.Unit != c.unit) {
			t.Fatalf("parseLength(%q) = {%v %v}, want {%v %v}", c.in, got.Value, got.Unit, c.val, c.unit)
		}
	}
}
