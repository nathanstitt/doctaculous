package css

import (
	"image/color"
	"testing"
)

// TestParseColor covers the whole CSS Color 4 grammar through the tokenizer
// wrapper, which is the shape every cascade/shorthand call site uses. It keeps
// the pre-existing cases (the parser this replaced accepted #rgb, #rrggbb,
// rgb(r,g,b) and a handful of names) so the merge is provably a superset, and
// adds the alpha-bearing, HSL, space-syntax and full-keyword-table cases the old
// parser dropped.
func TestParseColor(t *testing.T) {
	cases := []struct {
		in   string
		want color.RGBA
		ok   bool
	}{
		// --- forms the previous parser already accepted (must not regress) ---
		{"#000000", color.RGBA{0, 0, 0, 255}, true},
		{"#fff", color.RGBA{255, 255, 255, 255}, true},
		{"#ff0000", color.RGBA{255, 0, 0, 255}, true},
		{"red", color.RGBA{255, 0, 0, 255}, true},
		{"white", color.RGBA{255, 255, 255, 255}, true},
		{"transparent", color.RGBA{0, 0, 0, 0}, true},
		{"rgb(0,128,255)", color.RGBA{0, 128, 255, 255}, true},
		{"RED", color.RGBA{255, 0, 0, 255}, true},              // named colours are case-insensitive
		{"RGB(0,128,255)", color.RGBA{0, 128, 255, 255}, true}, // rgb() is case-insensitive
		{"#f00", color.RGBA{255, 0, 0, 255}, true},             // 3-digit hex, red channel
		{"rgb(-1,300,0)", color.RGBA{0, 255, 0, 255}, true},    // components clamp to [0,255]

		// --- alpha-bearing hex: #rgba and #rrggbbaa ---
		{"#000000e6", color.RGBA{0, 0, 0, 0xe6}, true},
		{"#4f9cff59", color.RGBA{0x4f, 0x9c, 0xff, 0x59}, true},
		{"#abcd", color.RGBA{0xaa, 0xbb, 0xcc, 0xdd}, true}, // each nibble doubles
		{"#f008", color.RGBA{255, 0, 0, 136}, true},
		{"#FF000080", color.RGBA{255, 0, 0, 128}, true}, // hex digits are case-insensitive

		// --- rgba() / rgb() with alpha ---
		{"rgba(0,0,0,0.9)", color.RGBA{0, 0, 0, 230}, true},
		{"rgba(255, 0, 0, 0.5)", color.RGBA{255, 0, 0, 128}, true},
		{"rgba(0,0,0,0)", color.RGBA{0, 0, 0, 0}, true},
		{"rgba(0,0,0,1)", color.RGBA{0, 0, 0, 255}, true},
		{"rgba(0,0,0,2)", color.RGBA{0, 0, 0, 255}, true}, // alpha clamps to [0,1]
		{"rgba(0,0,0,-1)", color.RGBA{0, 0, 0, 0}, true},  // ditto, lower end
		// CSS Color 4 §5.1 makes rgb() and rgba() literal aliases: the alpha
		// argument is optional in BOTH, so a 3-argument rgba() is valid opaque
		// colour (and rgb() with a 4th argument is valid too).
		{"rgba(0,0,0)", color.RGBA{0, 0, 0, 255}, true},
		{"rgb(0,0,0,0.5)", color.RGBA{0, 0, 0, 128}, true},
		{"hsla(210,100%,50%)", color.RGBA{0, 128, 255, 255}, true},
		{"rgb(0 0 0 / 0.5)", color.RGBA{0, 0, 0, 128}, true},          // space syntax, number alpha
		{"rgb(79 156 255 / 35%)", color.RGBA{79, 156, 255, 89}, true}, // space syntax, percent alpha
		{"rgb(100%, 0%, 0%)", color.RGBA{255, 0, 0, 255}, true},       // percentage channels
		{"rgba(100% 0% 0% / 50%)", color.RGBA{255, 0, 0, 128}, true},

		// --- hsl() / hsla() ---
		{"hsl(210,100%,50%)", color.RGBA{0, 128, 255, 255}, true},
		{"hsl(0, 100%, 50%)", color.RGBA{255, 0, 0, 255}, true},
		{"hsl(120 100% 25%)", color.RGBA{0, 128, 0, 255}, true},
		{"hsla(210,100%,50%,0.5)", color.RGBA{0, 128, 255, 128}, true},
		{"hsla(240,100%,50%,0.5)", color.RGBA{0, 0, 255, 128}, true},
		{"hsl(210deg 100% 50% / 50%)", color.RGBA{0, 128, 255, 128}, true}, // deg unit + slash alpha
		{"hsl(-150,100%,50%)", color.RGBA{0, 128, 255, 255}, true},         // hue wraps into [0,360)
		{"hsl(570,100%,50%)", color.RGBA{0, 128, 255, 255}, true},          // and wraps from above
		{"hsl(0,0%,0%)", color.RGBA{0, 0, 0, 255}, true},

		// --- the full CSS Color 4 keyword table, not the old 8-entry stub ---
		{"rebeccapurple", color.RGBA{0x66, 0x33, 0x99, 255}, true},
		{"cornflowerblue", color.RGBA{100, 149, 237, 255}, true},
		{"teal", color.RGBA{0, 128, 128, 255}, true},
		{"goldenrod", color.RGBA{218, 165, 32, 255}, true},
		{"lavenderblush", color.RGBA{255, 240, 245, 255}, true},
		{"darkgrey", color.RGBA{169, 169, 169, 255}, true}, // British spelling alias
		{"darkgray", color.RGBA{169, 169, 169, 255}, true},

		// --- REJECTION: each of these must yield ok=false so the declaration
		// drops per CSS whole-declaration error handling, rather than painting
		// some guessed colour.
		{"notacolor", color.RGBA{}, false},
		{"blurple", color.RGBA{}, false},
		{"", color.RGBA{}, false},
		{"   ", color.RGBA{}, false},
		{"rgb(a,b,c)", color.RGBA{}, false},
		{"rgb(0,128", color.RGBA{}, false}, // truncated, must not panic
		{"rgb(", color.RGBA{}, false},
		{"rgb()", color.RGBA{}, false},
		{"rgb(0,0)", color.RGBA{}, false},       // too few channels
		{"rgb(0,0,0,0,0)", color.RGBA{}, false}, // too many
		{"rgb(0 0 0 /)", color.RGBA{}, false},   // slash with no alpha
		{"hsl(1)", color.RGBA{}, false},
		{"hsl(210,100,50)", color.RGBA{}, false},  // hsl s/l MUST be percentages
		{"hsl(210,100%,50)", color.RGBA{}, false}, // ...both of them
		{"#", color.RGBA{}, false},
		{"#f", color.RGBA{}, false},
		{"#12", color.RGBA{}, false},
		{"#12345", color.RGBA{}, false},   // 5 nibbles is not a hex colour
		{"#1234567", color.RGBA{}, false}, // nor is 7
		{"#gg0000", color.RGBA{}, false},  // non-hex digit
		{"#00ff00gg", color.RGBA{}, false},
		{"url(#grad)", color.RGBA{}, false}, // paint-server reference, not a colour
		{"rgb(nan,0,0)", color.RGBA{}, false},
		{"rgb(1e400,0,0)", color.RGBA{}, false}, // overflows to +Inf
		{"ééé", color.RGBA{}, false},
	}
	for _, c := range cases {
		got, ok := parseColor(newTokenizer(c.in))
		if ok != c.ok {
			t.Errorf("parseColor(%q) ok = %v, want %v", c.in, ok, c.ok)
			continue
		}
		if got != c.want {
			t.Errorf("parseColor(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestParseColorValueMatchesTokenizerPath pins the invariant that makes one
// grammar possible: the exported string entry point and the tokenizer wrapper
// the cascade uses must never disagree. If they ever diverge, a colour would be
// accepted in `filter: drop-shadow(...)` but dropped in `background-color`, which
// is the exact class of bug this merge removed.
func TestParseColorValueMatchesTokenizerPath(t *testing.T) {
	for _, in := range []string{
		"rgba(0,0,0,0.9)", "#000000e6", "hsl(210,100%,50%)", "rebeccapurple",
		"rgb(79 156 255 / 35%)", "notacolor", "#12345", "",
	} {
		wantC, wantOK := ParseColorValue(in)
		gotC, gotOK := parseColor(newTokenizer(in))
		if gotC != wantC || gotOK != wantOK {
			t.Errorf("parseColor(%q) = %v,%v; ParseColorValue = %v,%v", in, gotC, gotOK, wantC, wantOK)
		}
	}
}

// TestParseColorAlphaReachesComputedStyle proves the alpha survives the cascade
// rather than being flattened on the way to the computed style — parsing alpha
// correctly is worthless if the property assignment throws it away.
func TestParseColorAlphaReachesComputedStyle(t *testing.T) {
	cases := []struct {
		decl  string
		wantA uint8
	}{
		{"rgba(0,0,0,0.9)", 230},
		{"#000000e6", 0xe6},
		{"hsla(210,100%,50%,0.5)", 128},
		{"black", 255},
	}
	for _, c := range cases {
		var cs ComputedStyle
		applyDeclaration(&cs, Declaration{Property: "background-color", Value: c.decl})
		if cs.BackgroundColor.A != c.wantA {
			t.Errorf("background-color:%s => alpha %d, want %d", c.decl, cs.BackgroundColor.A, c.wantA)
		}
	}
}

// TestParseColorDropsDeclarationOnMalformedValue is the other half of the
// contract: a value the grammar rejects must leave the previous value standing,
// not overwrite it with a zero colour (which would paint transparent black).
func TestParseColorDropsDeclarationOnMalformedValue(t *testing.T) {
	cs := ComputedStyle{BackgroundColor: color.RGBA{255, 0, 0, 255}}
	for _, bad := range []string{"rgba(0,0)", "#12345", "hsl(210,100,50)", "notacolor"} {
		applyDeclaration(&cs, Declaration{Property: "background-color", Value: bad})
		if cs.BackgroundColor != (color.RGBA{255, 0, 0, 255}) {
			t.Fatalf("background-color:%s changed the colour to %v; malformed values must drop", bad, cs.BackgroundColor)
		}
	}
}
