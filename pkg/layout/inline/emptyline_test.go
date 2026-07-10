package inline

import (
	"testing"

	layoutfont "github.com/nathanstitt/doctaculous/pkg/layout/font"
)

// shapePre shapes one preserving-mode run for the empty-forced-line tests.
func shapePre(t *testing.T, text string) []Glyph {
	t.Helper()
	faces := layoutfont.NewFaceCache()
	return Shape(faces, []Run{{Text: text, Family: "monospace", SizePt: 12, WhiteSpace: "pre"}}, nil)
}

// TestPreservedBreakCarriesMetrics verifies a preserved newline's break glyph
// carries its run's font metrics — the strut an empty forced line's height
// comes from.
func TestPreservedBreakCarriesMetrics(t *testing.T) {
	glyphs := shapePre(t, "a\nb")
	var brk *Glyph
	for i := range glyphs {
		if glyphs[i].Break {
			brk = &glyphs[i]
			break
		}
	}
	if brk == nil {
		t.Fatalf("no break glyph shaped")
	}
	if brk.AscentPt <= 0 || brk.DescentPt < 0 || brk.SizePt != 12 {
		t.Errorf("break glyph missing strut metrics: %+v", *brk)
	}
}

// TestEmptyForcedLineGetsStrut verifies a blank line between two preserved
// newlines yields a line whose metrics give it real height (CSS 2.1 §10.8.1:
// every line box has a strut), in both the whole-paragraph and per-line
// breakers.
func TestEmptyForcedLineGetsStrut(t *testing.T) {
	glyphs := shapePre(t, "a\n\nb")

	lines := Break(glyphs, 1000, 1000)
	if len(lines) != 3 {
		t.Fatalf("Break: got %d lines, want 3 (a, blank, b)", len(lines))
	}
	blank := lines[1]
	if blank.WidthPt != 0 {
		t.Errorf("blank line has width %v, want 0", blank.WidthPt)
	}
	if blank.AscentPt <= 0 {
		t.Errorf("blank line has no strut ascent: %+v", blank)
	}

	// The per-line breaker (the CSS IFC driver) must agree.
	line1, rest := BreakNext(glyphs, 1000)
	if len(line1) == 0 {
		t.Fatalf("BreakNext: empty first line")
	}
	line2, rest2 := BreakNext(rest, 1000)
	l2 := MakeLine(line2)
	if l2.AscentPt <= 0 || l2.WidthPt != 0 {
		t.Errorf("BreakNext blank line lacks strut metrics: %+v", l2)
	}
	line3, _ := BreakNext(rest2, 1000)
	if MakeLine(line3).WidthPt <= 0 {
		t.Errorf("third line (b) lost its glyph")
	}

	// The non-wrapping driver (white-space: pre) must agree too.
	_, rest = BreakNextWrap(glyphs, 1000, false)
	line2, _ = BreakNextWrap(rest, 1000, false)
	if l := MakeLine(line2); l.AscentPt <= 0 {
		t.Errorf("BreakNextWrap(!wrap) blank line lacks strut metrics: %+v", l)
	}
}
