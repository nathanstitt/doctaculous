package inline

import (
	"testing"
)

// synth builds one glyph per rune of s, as the shaper would for a single run. No font
// is needed: the reorder operates on Runes, so order can be asserted without any RTL
// glyph coverage (no bundled font has Hebrew or Arabic).
func synth(s string) []Glyph {
	var out []Glyph
	for _, r := range s {
		out = append(out, Glyph{
			Runes:   []rune{r},
			Advance: 10,
			Space:   r == ' ',
		})
	}
	return out
}

// visual renders a glyph slice back to a string, in slice order.
func visual(gs []Glyph) string {
	var out []rune
	for _, g := range gs {
		out = append(out, g.Runes...)
	}
	return string(out)
}

func TestReorderLTRLatinIsNoOp(t *testing.T) {
	// The overwhelmingly common case: an LTR paragraph with no RTL characters must not
	// be touched at all — same order, and the SAME BACKING SLICE (no allocation), which
	// is what keeps Latin-only documents byte-identical to the pre-bidi engine.
	in := synth("hello world")
	out, changed := reorder(in, DirLTR)
	if changed {
		t.Error("LTR Latin text should report no reordering")
	}
	if visual(out) != "hello world" {
		t.Errorf("LTR Latin reordered to %q, want unchanged", visual(out))
	}
	if len(out) != len(in) || &out[0] != &in[0] {
		t.Error("LTR Latin should return the input slice unchanged (no allocation)")
	}
}

func TestReorderRTLRunInLTRParagraph(t *testing.T) {
	// "abc אבג def" — an LTR paragraph with a Hebrew island. The Hebrew reverses in
	// place; the Latin around it stays put.
	in := synth("abc אבג def")
	out, changed := reorder(in, DirLTR)
	if !changed {
		t.Fatal("a Hebrew run in an LTR paragraph must reorder")
	}
	if got, want := visual(out), "abc גבא def"; got != want {
		t.Errorf("reorder = %q, want %q (the Hebrew run reverses, the Latin does not)", got, want)
	}
}

func TestReorderRTLParagraph(t *testing.T) {
	// A wholly Hebrew paragraph reverses entirely.
	in := synth("אבג")
	out, changed := reorder(in, DirRTL)
	if !changed {
		t.Fatal("an RTL paragraph of Hebrew must reorder")
	}
	if got, want := visual(out), "גבא"; got != want {
		t.Errorf("reorder = %q, want %q", got, want)
	}
}

func TestReorderLTRIslandInRTLParagraph(t *testing.T) {
	// "אב english גד" in an RTL paragraph: the Latin island keeps its internal
	// left-to-right order while the Hebrew around it reverses and the runs lay out
	// right-to-left overall.
	in := synth("אב english גד")
	out, changed := reorder(in, DirRTL)
	if !changed {
		t.Fatal("mixed text in an RTL paragraph must reorder")
	}
	got := visual(out)
	// The embedded Latin word must survive intact and readable.
	if !contains(got, "english") {
		t.Errorf("reorder = %q; the embedded LTR word must keep its internal order", got)
	}
	// The Hebrew must have reversed somewhere in the result.
	if contains(got, "אב") && contains(got, "גד") {
		t.Errorf("reorder = %q; the Hebrew runs should be reversed, not verbatim", got)
	}
}

func TestReorderPreservesGlyphCount(t *testing.T) {
	// Whatever the ordering, no glyph may be dropped or duplicated — a mapping bug
	// would otherwise silently lose text.
	for _, s := range []string{
		"abc אבג def",
		"אב english גד",
		"abc (אב) def",
		"12 אבג 34",
		"אבג",
		"plain latin",
	} {
		for _, dir := range []ParagraphDirection{DirLTR, DirRTL} {
			in := synth(s)
			out, _ := reorder(in, dir)
			if len(out) != len(in) {
				t.Errorf("%q dir=%v: got %d glyphs, want %d", s, dir, len(out), len(in))
			}
			if got, want := sortedRunes(visual(out)), sortedRunes(s); got != want {
				t.Errorf("%q dir=%v: glyph MULTISET changed (%q vs %q)", s, dir, got, want)
			}
		}
	}
}

func TestReorderKeepsAtomicsAndBreaks(t *testing.T) {
	// A glyph with no runes (an atomic inline box) participates as a neutral object and
	// must survive the round trip rather than being dropped by the rune mapping.
	in := []Glyph{
		{Runes: []rune{'a'}, Advance: 10},
		{Atomic: &AtomicItem{WidthPt: 20}, Advance: 20},
		{Runes: []rune{'b'}, Advance: 10},
	}
	out, _ := reorder(in, DirLTR)
	if len(out) != 3 {
		t.Fatalf("got %d glyphs, want 3 (the atomic must be preserved)", len(out))
	}
	atomics := 0
	for _, g := range out {
		if g.Atomic != nil {
			atomics++
		}
	}
	if atomics != 1 {
		t.Errorf("got %d atomic glyphs, want exactly 1", atomics)
	}
}

// TestMakeVisualLineMetricsAreLogical pins the subtle part of the split: a line's
// width and justification gap count are LOGICAL properties (they exclude the space
// that ENDS the text), but VisibleWidth/CountSpaces find that space by scanning from
// the end of the slice. In an RTL line the trailing space reorders to the visual
// START, so measuring the reordered slice would count it toward the ink width.
func TestMakeVisualLineMetricsAreLogical(t *testing.T) {
	// Hebrew followed by a trailing space: 3 glyphs of ink + 1 trailing space.
	in := synth("אבג ")
	l := MakeVisualLine(in, DirRTL)
	if got, want := l.WidthPt, 30.0; got != want {
		t.Errorf("WidthPt = %v, want %v (the trailing space must be excluded even though "+
			"it reorders to the visual start)", got, want)
	}
	if len(l.Glyphs) != 4 {
		t.Fatalf("line has %d glyphs, want 4", len(l.Glyphs))
	}
	// And the glyphs themselves ARE in visual order.
	if got := visual(l.Glyphs); got != " גבא" {
		t.Errorf("line glyphs = %q, want %q (visual order)", got, " גבא")
	}
}

func TestHasBidiControl(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"plain ascii", false},
		{"café naïve", false}, // Latin-1 supplement
		{"Ελληνικά", false},   // Greek
		{"Кириллица", false},  // Cyrillic
		{"שלום", true},        // Hebrew
		{"مرحبا", true},       // Arabic
		{"mixed שלום here", true},
		{"\u200fmark", true}, // RLM with no strong RTL letter
	}
	for _, c := range cases {
		if got := hasBidiControl([]rune(c.text)); got != c.want {
			t.Errorf("hasBidiControl(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// sortedRunes returns the runes of s in sorted order, for multiset comparison.
func sortedRunes(s string) string {
	rs := []rune(s)
	for i := 1; i < len(rs); i++ {
		for j := i; j > 0 && rs[j] < rs[j-1]; j-- {
			rs[j], rs[j-1] = rs[j-1], rs[j]
		}
	}
	return string(rs)
}
