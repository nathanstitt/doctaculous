package inline

import (
	"strings"
	"testing"
)

// mkModeGlyphs builds a uniform-advance glyph stream like mkGlyphs, but with a mid-word
// break mode and real Runes on every glyph (clusterStart needs rune identity to make a
// grapheme decision; a glyph with no runes is treated as its own cluster).
func mkModeGlyphs(s string, adv float64, mode WordBreakMode) []Glyph {
	out := make([]Glyph, 0, len(s))
	for _, r := range s {
		out = append(out, Glyph{
			Advance:  adv,
			AscentPt: 8, DescentPt: 2,
			Space:     r == ' ',
			Runes:     []rune{r},
			WordBreak: mode,
		})
	}
	return out
}

// lineText reassembles a line's glyphs into a string for readable assertions.
func lineTextOf(l Line) string {
	var b strings.Builder
	for _, g := range l.Glyphs {
		for _, r := range g.Runes {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func linesText(lines []Line) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = lineTextOf(l)
	}
	return out
}

func eqStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestBreakWordSplitsOverlongToken is the motivating case: a token far wider than the
// line box must actually be split under overflow-wrap: break-word, where the default
// leaves it overflowing on one line.
func TestBreakWordSplitsOverlongToken(t *testing.T) {
	const word = "AAAAAAAAAAAA" // 12 glyphs of advance 1

	normal := Break(mkModeGlyphs(word, 1, WordBreakNormal), 5, 5)
	if len(normal) != 1 {
		t.Fatalf("word-break normal: lines = %v, want 1 (overflowing)", linesText(normal))
	}

	broken := Break(mkModeGlyphs(word, 1, WordBreakWord), 5, 5)
	want := []string{"AAAAA", "AAAAA", "AA"}
	if got := linesText(broken); !eqStrings(got, want) {
		t.Errorf("break-word lines = %q, want %q", got, want)
	}
	for i, l := range broken {
		if l.WidthPt > 5 {
			t.Errorf("break-word line %d width = %v, want <= 5 (no overflow)", i, l.WidthPt)
		}
	}
}

// TestBreakWordIsLastResortButBreakAllIsNot is the distinction the two properties exist
// to draw, and the one implementations most often get wrong.
//
// "xx hello" at width 5: "hello" (width 5) does NOT fit after "xx " on line 1, but it
// DOES fit on a line of its own.
//   - break-word must leave it whole and push it down — it is a last resort, and the
//     ordinary space break already solved the overflow.
//   - break-all must chop it at the line edge regardless, because every cluster boundary
//     is an ordinary opportunity that greedy first-fit will take.
func TestBreakWordIsLastResortButBreakAllIsNot(t *testing.T) {
	const text = "xx hello"

	word := linesText(Break(mkModeGlyphs(text, 1, WordBreakWord), 5, 5))
	if want := []string{"xx", "hello"}; !eqStrings(word, want) {
		t.Errorf("break-word lines = %q, want %q — a word that fits on its own line "+
			"must NOT be broken", word, want)
	}

	all := linesText(Break(mkModeGlyphs(text, 1, WordBreakAll), 5, 5))
	if want := []string{"xx he", "llo"}; !eqStrings(all, want) {
		t.Errorf("break-all lines = %q, want %q — break-all is eager, not a last resort", all, want)
	}

	// The control: with neither property the word overflows its line rather than moving.
	normal := linesText(Break(mkModeGlyphs(text, 1, WordBreakNormal), 5, 5))
	if want := []string{"xx", "hello"}; !eqStrings(normal, want) {
		t.Errorf("normal lines = %q, want %q", normal, want)
	}
}

// TestBreakWordAppliesWhenWordCannotFit is the other half of the last-resort rule: once
// the word is alone on a line and STILL overflows, break-word does break it.
func TestBreakWordAppliesWhenWordCannotFit(t *testing.T) {
	// "xx AAAAAAAA": the token is 8 wide against a 5-wide box, so it cannot fit even
	// alone and must be split after being moved down.
	got := linesText(Break(mkModeGlyphs("xx AAAAAAAA", 1, WordBreakWord), 5, 5))
	want := []string{"xx", "AAAAA", "AAA"}
	if !eqStrings(got, want) {
		t.Errorf("break-word lines = %q, want %q", got, want)
	}
}

// TestKeepAllSuppressesMidWordBreaking: word-break: keep-all must not break mid-word.
func TestKeepAllSuppressesMidWordBreaking(t *testing.T) {
	got := linesText(Break(mkModeGlyphs("AAAAAAAAAAAA", 1, WordBreakKeepAll), 5, 5))
	if len(got) != 1 {
		t.Errorf("keep-all lines = %q, want 1 unbroken (overflowing) line", got)
	}
}

// TestNoWrapOutranksMidWordBreak: a white-space: nowrap span must stay on one line even
// when overflow-wrap would otherwise break it.
func TestNoWrapOutranksMidWordBreak(t *testing.T) {
	glyphs := mkModeGlyphs("AAAAAAAAAAAA", 1, WordBreakAll)
	for i := range glyphs {
		glyphs[i].NoWrap = true
	}
	if got := linesText(Break(glyphs, 5, 5)); len(got) != 1 {
		t.Errorf("nowrap + break-all lines = %q, want 1 unbroken line", got)
	}
}

// TestMidWordBreakConsumesNoGlyph: unlike a space break, a mid-word break must not drop
// a character — every input glyph has to survive into some line.
func TestMidWordBreakConsumesNoGlyph(t *testing.T) {
	const word = "ABCDEFGHIJKL"
	lines := Break(mkModeGlyphs(word, 1, WordBreakAll), 5, 5)
	var joined strings.Builder
	for _, l := range lines {
		joined.WriteString(lineTextOf(l))
	}
	if joined.String() != word {
		t.Errorf("reassembled = %q, want %q — a mid-word break must consume no glyph",
			joined.String(), word)
	}
}

// TestBreakNextWrapMatchesBreakForMidWord: the per-line driver the float path uses must
// reproduce the whole-paragraph breaker at a fixed width, mid-word breaking included.
func TestBreakNextWrapMatchesBreakForMidWord(t *testing.T) {
	for _, mode := range []WordBreakMode{WordBreakNormal, WordBreakWord, WordBreakAnywhere, WordBreakAll} {
		glyphs := mkModeGlyphs("xx AAAAAAAA yy", 1, mode)
		want := linesText(Break(glyphs, 5, 5))

		var got []string
		rest := mkModeGlyphs("xx AAAAAAAA yy", 1, mode)
		for len(rest) > 0 {
			var line []Glyph
			line, rest = BreakNextWrap(rest, 5, true)
			if len(line) == 0 && len(rest) > 0 {
				t.Fatalf("mode %d: BreakNextWrap made no progress", mode)
			}
			got = append(got, lineTextOf(MakeLine(line)))
		}
		if !eqStrings(got, want) {
			t.Errorf("mode %d: BreakNextWrap lines = %q, Break lines = %q", mode, got, want)
		}
	}
}

// TestMidWordBreakRespectsGraphemeClusters: a break must never land between a base
// character and its combining mark, nor inside an emoji ZWJ sequence or a flag.
func TestMidWordBreakRespectsGraphemeClusters(t *testing.T) {
	// Each "e" carries a combining acute (U+0301). The mark has zero advance, as a real
	// font would give it, so widths behave like the bare letters.
	var glyphs []Glyph
	base := Glyph{Advance: 1, AscentPt: 8, DescentPt: 2, WordBreak: WordBreakAll}
	for i := 0; i < 8; i++ {
		g := base
		g.Runes = []rune{'e'}
		glyphs = append(glyphs, g)
		m := base
		m.Advance = 0
		m.Runes = []rune{'́'}
		glyphs = append(glyphs, m)
	}
	for _, l := range Break(glyphs, 3, 3) {
		txt := []rune(lineTextOf(l))
		if len(txt) > 0 && txt[0] == '́' {
			t.Fatalf("line %q starts with a combining mark — a grapheme cluster was split",
				string(txt))
		}
	}
}

// TestGraphemeClusterRulesForEmojiAndFlags checks the context-sensitive UAX #29 rules
// directly, since they are the ones a naive implementation gets wrong.
func TestGraphemeClusterRulesForEmojiAndFlags(t *testing.T) {
	mk := func(runes ...[]rune) []Glyph {
		out := make([]Glyph, 0, len(runes))
		for _, rs := range runes {
			out = append(out, Glyph{Advance: 1, Runes: rs, WordBreak: WordBreakAll})
		}
		return out
	}
	tests := []struct {
		name   string
		glyphs []Glyph
		at     int
		want   bool // is index `at` a cluster START (a legal break position)?
	}{
		{"base then combining mark", mk([]rune{'e'}, []rune{'́'}), 1, false},
		{"two plain letters", mk([]rune{'a'}, []rune{'b'}), 1, true},
		// GB11: emoji ZWJ sequence (man + ZWJ + computer) is one cluster.
		{"emoji ZWJ join", mk([]rune{'\U0001F468'}, []rune{'‍'}, []rune{'\U0001F4BB'}), 2, false},
		// GB12/13: the two regional indicators of ONE flag do not split...
		{"flag halves", mk([]rune{'\U0001F1FA'}, []rune{'\U0001F1F8'}), 1, false},
		// ...but two adjacent flags may be split between them (RI run parity).
		{"between two flags", mk([]rune{'\U0001F1FA'}, []rune{'\U0001F1F8'},
			[]rune{'\U0001F1EB'}, []rune{'\U0001F1F7'}), 2, true},
		// GB6-GB8: Hangul jamo compose into one syllable cluster.
		{"hangul L then V", mk([]rune{'ᄀ'}, []rune{'ᅡ'}), 1, false},
	}
	for _, tc := range tests {
		if got := clusterStart(tc.glyphs, tc.at); got != tc.want {
			t.Errorf("%s: clusterStart(%d) = %v, want %v", tc.name, tc.at, got, tc.want)
		}
	}
}

// TestFlagNotHalvedByBreaking is the end-to-end version of the flag rule: breaking a run
// of flags at a narrow width must split BETWEEN flags, never inside one.
func TestFlagNotHalvedByBreaking(t *testing.T) {
	var glyphs []Glyph
	for i := 0; i < 4; i++ {
		for _, r := range []rune{'\U0001F1FA', '\U0001F1F8'} {
			glyphs = append(glyphs, Glyph{Advance: 1, Runes: []rune{r}, WordBreak: WordBreakAll})
		}
	}
	for _, l := range Break(glyphs, 3, 3) {
		txt := []rune(lineTextOf(l))
		if len(txt)%2 != 0 {
			t.Fatalf("line %q has an odd number of regional indicators — a flag was halved",
				string(txt))
		}
	}
}

// TestNormalModeIsUnchanged guards the hot path: with every glyph in the default mode
// the breaker must produce exactly what it did before mid-word breaking existed.
func TestNormalModeIsUnchanged(t *testing.T) {
	for _, text := range []string{
		"aa bb cc", "hello world", "supercalifrag", "a bb ccc dddd eeeee",
		"one  two   three", "averyveryverylongtoken and more",
	} {
		for _, w := range []float64{1, 3, 5, 8, 100} {
			plain := linesText(Break(mkGlyphsRunes(text, 1), w, w))
			moded := linesText(Break(mkModeGlyphs(text, 1, WordBreakNormal), w, w))
			if !eqStrings(plain, moded) {
				t.Errorf("text %q width %v: normal-mode lines %q differ from plain %q",
					text, w, moded, plain)
			}
		}
	}
}

// mkGlyphsRunes is mkGlyphs with Runes populated, so it is comparable against
// mkModeGlyphs (which needs runes for cluster decisions).
func mkGlyphsRunes(s string, adv float64) []Glyph {
	out := mkGlyphs(s, adv)
	i := 0
	for _, r := range s {
		out[i].Runes = []rune{r}
		i++
	}
	return out
}
