package css

import (
	"image/color"
	"testing"
)

// px is a shorthand for a pixel Length, which every case below is written in.
func px(v float64) Length { return Length{Value: v, Unit: UnitPx} }

// TestParseBoxShadowAcceptsTheFullGrammar walks the spec's grammar
// (`inset? && <length>{2,4} && <color>?`) case by case, including the orderings
// a positional parser would reject.
func TestParseBoxShadowAcceptsTheFullGrammar(t *testing.T) {
	red := color.RGBA{255, 0, 0, 255}
	for _, tc := range []struct {
		name  string
		value string
		want  []BoxShadow
	}{
		{"two lengths only", "2px 3px", []BoxShadow{{OffsetX: px(2), OffsetY: px(3)}}},
		{"three lengths adds blur", "2px 3px 4px",
			[]BoxShadow{{OffsetX: px(2), OffsetY: px(3), Blur: px(4)}}},
		{"four lengths adds spread", "2px 3px 4px 5px",
			[]BoxShadow{{OffsetX: px(2), OffsetY: px(3), Blur: px(4), Spread: px(5)}}},
		{"trailing colour", "2px 3px red",
			[]BoxShadow{{OffsetX: px(2), OffsetY: px(3), Color: red, HasColor: true}}},
		{"leading colour", "red 2px 3px",
			[]BoxShadow{{OffsetX: px(2), OffsetY: px(3), Color: red, HasColor: true}}},
		{"hex colour", "2px 3px #ff0000",
			[]BoxShadow{{OffsetX: px(2), OffsetY: px(3), Color: red, HasColor: true}}},
		{"short hex colour", "2px 3px #f00",
			[]BoxShadow{{OffsetX: px(2), OffsetY: px(3), Color: red, HasColor: true}}},
		{"rgb() colour", "2px 3px rgb(255, 0, 0)",
			[]BoxShadow{{OffsetX: px(2), OffsetY: px(3), Color: red, HasColor: true}}},
		{"leading inset", "inset 2px 3px",
			[]BoxShadow{{OffsetX: px(2), OffsetY: px(3), Inset: true}}},
		{"trailing inset", "2px 3px inset",
			[]BoxShadow{{OffsetX: px(2), OffsetY: px(3), Inset: true}}},
		{"inset between lengths and colour", "2px 3px inset red",
			[]BoxShadow{{OffsetX: px(2), OffsetY: px(3), Color: red, HasColor: true, Inset: true}}},
		{"inset before colour before lengths", "inset red 2px 3px",
			[]BoxShadow{{OffsetX: px(2), OffsetY: px(3), Color: red, HasColor: true, Inset: true}}},
		{"negative offsets", "-2px -3px",
			[]BoxShadow{{OffsetX: px(-2), OffsetY: px(-3)}}},
		{"negative spread shrinks", "0 0 0 -4px",
			[]BoxShadow{{OffsetX: px(0), OffsetY: px(0), Blur: px(0), Spread: px(-4)}}},
		{"unitless zero is a length", "0 0",
			[]BoxShadow{{OffsetX: px(0), OffsetY: px(0)}}},
		{"em lengths survive unresolved", "1em 2em",
			[]BoxShadow{{OffsetX: Length{1, UnitEm}, OffsetY: Length{2, UnitEm}}}},
		{"pt lengths", "1pt 2pt",
			[]BoxShadow{{OffsetX: Length{1, UnitPt}, OffsetY: Length{2, UnitPt}}}},
		{"case-insensitive inset", "2px 3px INSET",
			[]BoxShadow{{OffsetX: px(2), OffsetY: px(3), Inset: true}}},
		{"currentColor leaves HasColor false", "2px 3px currentColor",
			[]BoxShadow{{OffsetX: px(2), OffsetY: px(3)}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseBoxShadow(tc.value)
			if !ok {
				t.Fatalf("parseBoxShadow(%q) rejected a valid value", tc.value)
			}
			assertShadows(t, tc.value, got, tc.want)
		})
	}
}

// TestParseBoxShadowCommaLists proves a comma-separated list yields one entry
// per shadow IN SOURCE ORDER — which is the reverse of paint order, since the
// spec puts the first shadow on top. Reversing here rather than at paint time
// would double-reverse.
func TestParseBoxShadowCommaLists(t *testing.T) {
	got, ok := parseBoxShadow("1px 1px red, inset 2px 2px 3px blue, 4px 4px 5px 6px")
	if !ok {
		t.Fatal("parseBoxShadow rejected a valid comma list")
	}
	assertShadows(t, "comma list", got, []BoxShadow{
		{OffsetX: px(1), OffsetY: px(1), Color: color.RGBA{255, 0, 0, 255}, HasColor: true},
		{OffsetX: px(2), OffsetY: px(2), Blur: px(3), Color: color.RGBA{0, 0, 255, 255}, HasColor: true, Inset: true},
		{OffsetX: px(4), OffsetY: px(4), Blur: px(5), Spread: px(6)},
	})
}

// TestParseBoxShadowRejectsMalformedValues pins CSS error handling: an invalid
// declaration is ignored ENTIRELY, so a list with one bad entry yields NO
// shadows rather than the entries that happened to parse.
func TestParseBoxShadowRejectsMalformedValues(t *testing.T) {
	for _, tc := range []struct{ name, value string }{
		{"none is not a shadow", "none"},
		{"empty", ""},
		{"whitespace only", "   "},
		{"one length is below the minimum", "2px"},
		{"one length with a colour", "red 2px"},
		{"five lengths exceed the maximum", "1px 2px 3px 4px 5px"},
		{"negative blur is an error", "2px 3px -4px"},
		{"percentage is not a length", "10% 3px"},
		{"unitless non-zero is not a length", "2 3"},
		{"auto is not a length", "auto 3px"},
		{"unknown colour", "2px 3px notacolour"},
		{"two colours", "red blue 2px 3px"},
		{"duplicate inset", "inset inset 2px 3px"},
		{"a valid entry followed by an invalid one", "2px 2px, garbage"},
		{"an invalid entry followed by a valid one", "garbage, 2px 2px"},
		{"trailing comma leaves an empty shadow", "2px 2px,"},
		{"leading comma", ", 2px 2px"},
		{"currentColor plus a colour fills the slot twice", "currentColor red 2px 3px"},
		{"malformed rgb()", "2px 3px rgb(255 0 0)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, ok := parseBoxShadow(tc.value); ok {
				t.Fatalf("parseBoxShadow(%q) = %+v, ok; want rejected", tc.value, got)
			}
		})
	}
}

// TestBoxShadowCascades checks the property reaches ComputedStyle, and that a
// LATER `none` or a later invalid value both clear an earlier valid one (the
// documented divergence at the cascade's `box-shadow` case: a browser would
// restore the earlier value for the invalid case, this clears it).
func TestBoxShadowCascades(t *testing.T) {
	for _, tc := range []struct {
		name  string
		decls []string
		want  int
	}{
		{"a valid declaration lands", []string{"2px 3px 4px red"}, 1},
		{"a comma list lands whole", []string{"1px 1px, 2px 2px, 3px 3px"}, 3},
		{"a later none clears it", []string{"2px 3px", "none"}, 0},
		{"a later invalid value clears it", []string{"2px 3px", "10% 3px"}, 0},
		{"an invalid first declaration leaves nothing", []string{"garbage"}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cs := initialStyle()
			if len(cs.BoxShadow) != 0 {
				t.Fatalf("initial box-shadow = %+v, want none", cs.BoxShadow)
			}
			for _, v := range tc.decls {
				applyDeclaration(&cs, Declaration{Property: "box-shadow", Value: v})
			}
			if got := len(cs.BoxShadow); got != tc.want {
				t.Errorf("len(BoxShadow) = %d, want %d (%+v)", got, tc.want, cs.BoxShadow)
			}
		})
	}
}

// TestBoxShadowIsNotInherited pins the spec's "Inherited: no". inheritFrom is
// the single source of truth for which fields carry over, so a child derived
// from a shadowed parent must come back with an empty list.
func TestBoxShadowIsNotInherited(t *testing.T) {
	parent := initialStyle()
	applyDeclaration(&parent, Declaration{Property: "box-shadow", Value: "2px 3px 4px red"})
	if len(parent.BoxShadow) != 1 {
		t.Fatalf("parent lost its shadow: %+v", parent.BoxShadow)
	}
	if child := inheritFrom(parent); len(child.BoxShadow) != 0 {
		t.Errorf("box-shadow inherited to the child: %+v; the property is not inherited", child.BoxShadow)
	}
}

// assertShadows compares two shadow lists field by field, reporting the first
// mismatch with enough context to locate it.
func assertShadows(t *testing.T, label string, got, want []BoxShadow) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %d shadows, want %d (%+v)", label, len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s: shadow %d =\n got %+v\nwant %+v", label, i, got[i], want[i])
		}
	}
}
