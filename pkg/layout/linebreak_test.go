package layout

import "testing"

// mkGlyphs builds a shaped-glyph stream from a string where every rune has a
// uniform advance and spaces are break opportunities. This isolates the
// line-breaker from real font metrics.
func mkGlyphs(s string, adv float64) []shapedGlyph {
	out := make([]shapedGlyph, 0, len(s))
	for _, r := range s {
		out = append(out, shapedGlyph{
			advance:  adv,
			ascentPt: 8, descentPt: 2,
			isSpace: r == ' ',
		})
	}
	return out
}

func TestBreakLinesWrapsAtSpaces(t *testing.T) {
	// "aa bb cc" with advance 1 each. Visible widths: "aa"=2, "aa bb"=5 (trailing
	// space excluded -> "aa bb" visible = 5), etc. With width 5, "aa bb" fits but
	// "aa bb cc" (8) does not, so we expect a break.
	glyphs := mkGlyphs("aa bb cc", 1)
	lines := breakLines(glyphs, 5, 5)
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	// First line keeps "aa bb" (5 glyphs incl space), second "cc" (2 glyphs).
	if got := len(lines[0].glyphs); got != 5 {
		t.Errorf("line 0 glyph count = %d, want 5 (\"aa bb\")", got)
	}
	if got := len(lines[1].glyphs); got != 2 {
		t.Errorf("line 1 glyph count = %d, want 2 (\"cc\")", got)
	}
}

func TestBreakLinesSingleLineFits(t *testing.T) {
	glyphs := mkGlyphs("hello world", 1)
	lines := breakLines(glyphs, 100, 100)
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(lines))
	}
}

func TestBreakLinesLongWordOverflowsAlone(t *testing.T) {
	// A single 10-wide word with width 3: it cannot be split, so it occupies one
	// line and overflows rather than looping.
	glyphs := mkGlyphs("supercalifrag", 1)
	lines := breakLines(glyphs, 3, 3)
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want 1 (unbreakable word)", len(lines))
	}
	if lines[0].widthPt <= 3 {
		t.Errorf("expected overflow width > 3, got %v", lines[0].widthPt)
	}
}

func TestBreakLinesHardBreak(t *testing.T) {
	glyphs := mkGlyphs("ab", 1)
	glyphs = append(glyphs, shapedGlyph{hardBreak: true})
	glyphs = append(glyphs, mkGlyphs("cd", 1)...)
	lines := breakLines(glyphs, 100, 100)
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2 (hard break)", len(lines))
	}
}

func TestBreakLinesFirstLineNarrower(t *testing.T) {
	// First line width 2, rest width 100: "aa" fits the first line, "bb cc" flows
	// to the second.
	glyphs := mkGlyphs("aa bb cc", 1)
	lines := breakLines(glyphs, 100, 2)
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2 (first line narrow)", len(lines))
	}
	if len(lines[0].glyphs) != 2 {
		t.Errorf("first (narrow) line glyphs = %d, want 2 (\"aa\")", len(lines[0].glyphs))
	}
}
