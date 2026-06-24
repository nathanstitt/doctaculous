package inline

import "testing"

// mkGlyphs builds a shaped-glyph stream from a string where every rune has a
// uniform advance and spaces are break opportunities. This isolates the
// line-breaker from real font metrics.
func mkGlyphs(s string, adv float64) []Glyph {
	out := make([]Glyph, 0, len(s))
	for _, r := range s {
		out = append(out, Glyph{
			Advance:  adv,
			AscentPt: 8, DescentPt: 2,
			Space: r == ' ',
		})
	}
	return out
}

func TestBreakWrapsAtSpaces(t *testing.T) {
	// "aa bb cc" with advance 1 each. Visible widths: "aa"=2, "aa bb"=5 (trailing
	// space excluded -> "aa bb" visible = 5), etc. With width 5, "aa bb" fits but
	// "aa bb cc" (8) does not, so we expect a break.
	glyphs := mkGlyphs("aa bb cc", 1)
	lines := Break(glyphs, 5, 5)
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	// First line keeps "aa bb" (5 glyphs incl space), second "cc" (2 glyphs).
	if got := len(lines[0].Glyphs); got != 5 {
		t.Errorf("line 0 glyph count = %d, want 5 (\"aa bb\")", got)
	}
	if got := len(lines[1].Glyphs); got != 2 {
		t.Errorf("line 1 glyph count = %d, want 2 (\"cc\")", got)
	}
}

func TestBreakSingleLineFits(t *testing.T) {
	glyphs := mkGlyphs("hello world", 1)
	lines := Break(glyphs, 100, 100)
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(lines))
	}
}

func TestBreakLongWordOverflowsAlone(t *testing.T) {
	// A single 10-wide word with width 3: it cannot be split, so it occupies one
	// line and overflows rather than looping.
	glyphs := mkGlyphs("supercalifrag", 1)
	lines := Break(glyphs, 3, 3)
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want 1 (unbreakable word)", len(lines))
	}
	if lines[0].WidthPt <= 3 {
		t.Errorf("expected overflow width > 3, got %v", lines[0].WidthPt)
	}
}

func TestBreakHardBreak(t *testing.T) {
	glyphs := mkGlyphs("ab", 1)
	glyphs = append(glyphs, Glyph{Break: true})
	glyphs = append(glyphs, mkGlyphs("cd", 1)...)
	lines := Break(glyphs, 100, 100)
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2 (hard break)", len(lines))
	}
}

func TestBreakFirstLineNarrower(t *testing.T) {
	// First line width 2, rest width 100: "aa" fits the first line, "bb cc" flows
	// to the second.
	glyphs := mkGlyphs("aa bb cc", 1)
	lines := Break(glyphs, 100, 2)
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2 (first line narrow)", len(lines))
	}
	if len(lines[0].Glyphs) != 2 {
		t.Errorf("first (narrow) line glyphs = %d, want 2 (\"aa\")", len(lines[0].Glyphs))
	}
}

func TestBreakEmptyInputYieldsOneEmptyLine(t *testing.T) {
	lines := Break(nil, 100, 100)
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want exactly 1 empty line", len(lines))
	}
	if len(lines[0].Glyphs) != 0 {
		t.Errorf("empty line should have 0 glyphs, got %d", len(lines[0].Glyphs))
	}
}

func TestBreakAllTrailingSpacesWrapsWithoutPanic(t *testing.T) {
	// A stream that is entirely spaces has zero visible width on every prefix, so it
	// never overflows; it must not panic and produces a single line.
	glyphs := mkGlyphs("       ", 1)
	lines := Break(glyphs, 3, 3)
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want 1 (all-space stream stays on one line)", len(lines))
	}
	if lines[0].WidthPt != 0 {
		t.Errorf("all-space line visible width = %v, want 0", lines[0].WidthPt)
	}
}

func TestBreakAtomicWiderThanLinePlacedAlone(t *testing.T) {
	// An atomic glyph wider than the available width has no break opportunity around
	// it, so it occupies its own line and overflows rather than looping.
	atom := &AtomicItem{WidthPt: 50}
	glyphs := []Glyph{{Advance: 50, Atomic: atom}}
	lines := Break(glyphs, 10, 10)
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want 1 (oversized atomic placed alone)", len(lines))
	}
	if len(lines[0].Glyphs) != 1 || lines[0].Glyphs[0].Atomic == nil {
		t.Fatalf("expected the line to hold the single atomic glyph")
	}
	if lines[0].WidthPt <= 10 {
		t.Errorf("expected atomic overflow width > 10, got %v", lines[0].WidthPt)
	}
}

// mkRun builds a glyph run from words, with a per-character advance and a space
// advance. Spaces between words are marked so the breaker can break there.
func mkRun(words []string, advancePerChar, spaceAdvance float64) []Glyph {
	var gs []Glyph
	for wi, w := range words {
		if wi > 0 {
			gs = append(gs, Glyph{Advance: spaceAdvance, Space: true})
		}
		for range w {
			gs = append(gs, Glyph{Advance: advancePerChar})
		}
	}
	return gs
}

// TestBreakNextOneLine: BreakNext returns the glyphs that fit on one line plus the
// remainder, breaking at the last space before overflow.
func TestBreakNextOneLine(t *testing.T) {
	// "aa bb cc" with char advance 10, space 10: "aa"=20, " "=10, "bb"=20, ...
	gs := mkRun([]string{"aa", "bb", "cc"}, 10, 10)
	// Width 45: "aa"(20) fits, "aa "(30) fits, "aa b"(40) fits, "aa bb"(50) overflows
	// -> break at the space after "aa", so the line is "aa" (visible width 20).
	line, rest := BreakNext(gs, 45)
	if got := VisibleWidth(line); got != 20 {
		t.Errorf("line visible width = %v, want 20 (\"aa\")", got)
	}
	if len(rest) == 0 {
		t.Fatalf("rest is empty, want \"bb cc\" remainder")
	}
	if rest[0].Space {
		t.Errorf("rest[0] is a space; the breaking space should be consumed")
	}
}

// TestBreakNextEquivalence: repeatedly calling BreakNext at a fixed width yields the
// same lines as one Break call at that width (guards the non-float path unchanged).
func TestBreakNextEquivalence(t *testing.T) {
	gs := mkRun([]string{"alpha", "beta", "gamma", "delta", "epsilon"}, 8, 8)
	const w = 80

	want := Break(gs, w, w)

	var got []Line
	rest := gs
	for len(rest) > 0 {
		var line []Glyph
		line, rest = BreakNext(rest, w)
		got = append(got, MakeLine(line))
	}
	if len(got) != len(want) {
		t.Fatalf("BreakNext produced %d lines, Break produced %d", len(got), len(want))
	}
	for i := range want {
		if !floatEq(got[i].WidthPt, want[i].WidthPt) {
			t.Errorf("line %d width: BreakNext %v vs Break %v", i, got[i].WidthPt, want[i].WidthPt)
		}
		if len(got[i].Glyphs) != len(want[i].Glyphs) {
			t.Errorf("line %d glyph count: BreakNext %d vs Break %d", i, len(got[i].Glyphs), len(want[i].Glyphs))
		}
	}
}

func floatEq(a, b float64) bool { d := a - b; return d < 1e-9 && d > -1e-9 }

// TestBreakNextOverlongWord: a single word wider than the width is taken alone
// (overflows) and the remainder is empty.
func TestBreakNextOverlongWord(t *testing.T) {
	gs := mkRun([]string{"superlongword"}, 10, 10) // 130 wide
	line, rest := BreakNext(gs, 50)
	if len(line) != len(gs) {
		t.Errorf("overlong word: line has %d glyphs, want all %d", len(line), len(gs))
	}
	if len(rest) != 0 {
		t.Errorf("overlong word: rest has %d glyphs, want 0", len(rest))
	}
}

// TestBreakNextForcedBreak: a Break glyph ends the line; the remainder continues
// after it (the break glyph is consumed).
func TestBreakNextForcedBreak(t *testing.T) {
	gs := []Glyph{
		{Advance: 10}, {Advance: 10}, // "aa"
		{Break: true},
		{Advance: 10}, {Advance: 10}, // "bb"
	}
	line, rest := BreakNext(gs, 1000) // width large; only the forced break splits
	if len(line) != 2 {
		t.Errorf("forced break: line has %d glyphs, want 2", len(line))
	}
	if len(rest) != 2 {
		t.Errorf("forced break: rest has %d glyphs, want 2 (after the break glyph)", len(rest))
	}
}
