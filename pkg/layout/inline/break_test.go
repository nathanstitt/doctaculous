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
