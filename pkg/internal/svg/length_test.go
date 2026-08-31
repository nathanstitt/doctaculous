package svg

import (
	"math"
	"reflect"
	"testing"
)

func TestParseLength(t *testing.T) {
	cases := []struct {
		in   string
		ref  float64
		want float64
		ok   bool
	}{
		{"100", 0, 100, true}, {"1.5e2", 0, 150, true}, {".5", 0, 0.5, true},
		{"10px", 0, 10, true}, {"72pt", 0, 96, true}, {"1in", 0, 96, true},
		{"2.54cm", 0, 96, true}, {"25.4mm", 0, 96, true}, {"6pc", 0, 96, true},
		{"50%", 200, 100, true}, {"2em", 0, 32, true}, {"2ex", 0, 16, true},
		{"", 0, 0, false}, {"abc", 0, 0, false}, {"10 px", 0, 0, false},
	}
	for _, c := range cases {
		got, ok := parseLength(c.in, c.ref)
		if ok != c.ok || (ok && math.Abs(got-c.want) > 1e-9) {
			t.Errorf("parseLength(%q,%g) = %g,%v; want %g,%v", c.in, c.ref, got, ok, c.want, c.ok)
		}
	}
	if got := parseNumberList(" 1,2  3\n4 "); !reflect.DeepEqual(got, []float64{1, 2, 3, 4}) {
		t.Errorf("parseNumberList = %v", got)
	}
	if got := parseNumberList("1,x"); got != nil {
		t.Errorf("parseNumberList bad token = %v, want nil", got)
	}
}
