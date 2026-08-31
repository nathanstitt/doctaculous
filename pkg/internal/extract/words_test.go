package extract

import "testing"

// mkGlyph builds a glyph at (x,y) with the given rune and advance, defaulting size
// to 10 and x1 to x+advance. It is the tests' synthetic-glyph constructor so table/
// block/word tests need no real PDF.
func mkGlyph(r rune, x, y, advance float64) glyph {
	return glyph{r: r, x: x, y: y, x1: x + advance, size: 10, advance: advance}
}

// glyphString lays out a string of runes on one baseline starting at x0, each glyph
// `advance` wide with `gap` extra space before it (so a gap >= wordGap splits a
// word). It is a convenience for building word-grouping fixtures.
func glyphString(s string, x0, y, advance, gap float64) []glyph {
	var out []glyph
	x := x0
	for _, r := range s {
		out = append(out, mkGlyph(r, x, y, advance))
		x += advance + gap
	}
	return out
}

func TestBuildLinesGroupsWords(t *testing.T) {
	// "AB" then a wide gap then "CD" on one baseline should be two words on one line.
	var g []glyph
	g = append(g, mkGlyph('A', 0, 100, 6))
	g = append(g, mkGlyph('B', 6, 100, 6))
	// wide gap (> 0.25*10 = 2.5): next word starts at x=20.
	g = append(g, mkGlyph('C', 20, 100, 6))
	g = append(g, mkGlyph('D', 26, 100, 6))

	lines := buildLines(g)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	if len(lines[0].words) != 2 {
		t.Fatalf("got %d words, want 2: %+v", len(lines[0].words), lines[0].words)
	}
	if lines[0].words[0].text != "AB" || lines[0].words[1].text != "CD" {
		t.Errorf("words = %q,%q; want AB,CD", lines[0].words[0].text, lines[0].words[1].text)
	}
}

func TestBuildLinesSplitsLines(t *testing.T) {
	// Two baselines far apart (y=100 and y=120) => two lines.
	var g []glyph
	g = append(g, mkGlyph('X', 0, 100, 6))
	g = append(g, mkGlyph('Y', 0, 120, 6))
	lines := buildLines(g)
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	// Reading order: y=100 line first (smaller y is higher on the page).
	if lines[0].y > lines[1].y {
		t.Errorf("lines not in top-to-bottom order: %v then %v", lines[0].y, lines[1].y)
	}
}

func TestBuildLinesExplicitSpaceSplitsWords(t *testing.T) {
	// A glyph flagged isSpace is a hard word break even without a wide gap.
	g := []glyph{
		mkGlyph('A', 0, 100, 6),
		{r: ' ', x: 6, y: 100, x1: 9, size: 10, advance: 3, isSpace: true},
		mkGlyph('B', 9, 100, 6),
	}
	lines := buildLines(g)
	if len(lines) != 1 || len(lines[0].words) != 2 {
		t.Fatalf("got %d lines / words; want 1 line, 2 words: %+v", len(lines), lines)
	}
}

func TestBuildLinesRuneZeroPlaceholder(t *testing.T) {
	// A glyph with rune 0 must not crash and should render as U+FFFD.
	g := []glyph{mkGlyph(0, 0, 100, 6), mkGlyph('x', 6, 100, 6)}
	lines := buildLines(g)
	if len(lines) != 1 || len(lines[0].words) != 1 {
		t.Fatalf("unexpected grouping: %+v", lines)
	}
	if got := lines[0].words[0].text; got != "�x" {
		t.Errorf("text = %q, want %q", got, "�x")
	}
}

func TestBodyFontSize(t *testing.T) {
	// Three body lines at size 10 and one heading line at size 24: the body size is 10.
	var lines []line
	for i := 0; i < 3; i++ {
		g := glyphString("hello world", 0, float64(100+i*12), 6, 0)
		for j := range g {
			g[j].size = 10
		}
		lines = append(lines, buildLines(g)...)
	}
	head := glyphString("Big", 0, 60, 14, 0)
	for j := range head {
		head[j].size = 24
	}
	lines = append(lines, buildLines(head)...)

	if bs := bodyFontSize(lines); bs != 10 {
		t.Errorf("bodyFontSize = %v, want 10", bs)
	}
}
