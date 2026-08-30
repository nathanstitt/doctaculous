package inline

import (
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/render"
)

// glyphRun builds n identical-advance glyphs for the width arithmetic below.
func glyphRun(n int, adv float64) []Glyph {
	g := make([]Glyph, n)
	for i := range g {
		g[i] = Glyph{Advance: adv, SizePt: 10, Outline: &render.Path{}}
	}
	return g
}

func TestTruncateWithEllipsisLeavesFittingLineAlone(t *testing.T) {
	line := glyphRun(3, 10)
	out, changed := TruncateWithEllipsis(line, 100, Glyph{Advance: 10})
	if changed {
		t.Error("changed=true for a line that already fits")
	}
	if len(out) != 3 {
		t.Errorf("len = %d, want 3 (untouched)", len(out))
	}
}

// The cut is by WHOLE glyphs and leaves room for the ellipsis, so the result fits.
func TestTruncateWithEllipsisFitsWithinWidth(t *testing.T) {
	ell := Glyph{Advance: 10}
	out, changed := TruncateWithEllipsis(glyphRun(20, 10), 100, ell)
	if !changed {
		t.Fatal("changed=false for an overflowing line")
	}
	var w float64
	for _, g := range out {
		w += g.Advance
	}
	if w > 100 {
		t.Errorf("truncated width %v exceeds the 100 available", w)
	}
	if out[len(out)-1].Advance != ell.Advance {
		t.Error("last glyph is not the ellipsis")
	}
}

// Trailing whitespace is dropped before the ellipsis: "foo …" reads as a gap, and
// browsers cut it.
func TestTruncateWithEllipsisDropsTrailingSpace(t *testing.T) {
	// Word-like: a space every 4th glyph, so wherever the cut lands there is a space
	// near it and the "space immediately before the ellipsis" case is actually
	// exercised rather than assumed.
	for _, width := range []float64{100, 95, 90, 85, 80, 75, 70, 65} {
		line := glyphRun(20, 10)
		for i := range line {
			line[i].Space = i%4 == 3
		}
		out, _ := TruncateWithEllipsis(line, width, Glyph{Advance: 10})
		if n := len(out); n >= 2 && out[n-2].Space {
			t.Errorf("width %v: a space survives immediately before the ellipsis", width)
		}
	}
}

// Trailing whitespace does not count toward overflow: a line whose TEXT fits but
// whose trailing spaces run past the box is not truncated, because VisibleWidth
// excludes them — the same rule the line breaker uses, so the two agree about what
// "too wide" means.
func TestTruncateIgnoresTrailingWhitespaceForFit(t *testing.T) {
	line := glyphRun(20, 10)
	for i := 5; i < len(line); i++ {
		line[i].Space = true
	}
	if _, changed := TruncateWithEllipsis(line, 100, Glyph{Advance: 10}); changed {
		t.Error("changed=true: trailing spaces must not make a fitting line overflow")
	}
}

// Forcing the ellipsis onto that same line (the clamp case) drops the whole trailing
// space run rather than leaving a gap before the ellipsis.
func TestAppendEllipsisDropsAWholeTrailingRun(t *testing.T) {
	line := glyphRun(20, 10)
	for i := 5; i < len(line); i++ {
		line[i].Space = true
	}
	out, changed := AppendEllipsis(line, 100, Glyph{Advance: 10})
	if !changed {
		t.Fatal("changed=false")
	}
	if n := len(out); n != 6 {
		t.Fatalf("len = %d, want 6 (5 glyphs + ellipsis)", n)
	}
	if out[len(out)-2].Space {
		t.Error("a space survives before the ellipsis")
	}
}

// CSS Overflow 3 §5: when even the ellipsis alone does not fit, it is still rendered.
// Returning an empty line instead would silently erase the text's only indication
// that something was cut.
func TestTruncateWithEllipsisRendersEvenWhenTooNarrow(t *testing.T) {
	out, changed := TruncateWithEllipsis(glyphRun(20, 10), 2, Glyph{Advance: 10})
	if !changed {
		t.Fatal("changed=false")
	}
	if len(out) != 1 {
		t.Fatalf("len = %d, want 1 (the lone ellipsis)", len(out))
	}
}

// AppendEllipsis marks a line that ALREADY FITS — the -webkit-line-clamp case, where
// the ellipsis signals text cut AFTER this line rather than an overflow of it.
// TruncateWithEllipsis cannot serve that: by its own measure nothing overflows.
func TestAppendEllipsisMarksAFittingLine(t *testing.T) {
	line := glyphRun(3, 10)
	out, changed := AppendEllipsis(line, 100, Glyph{Advance: 10})
	if !changed {
		t.Fatal("changed=false; a clamped line must be marked even though it fits")
	}
	if len(out) != 4 {
		t.Errorf("len = %d, want 4 (3 glyphs + ellipsis)", len(out))
	}
	// Contrast: the same line through TruncateWithEllipsis is untouched.
	if plain, _ := TruncateWithEllipsis(line, 100, Glyph{Advance: 10}); len(plain) != 3 {
		t.Errorf("TruncateWithEllipsis len = %d, want 3", len(plain))
	}
}

// A full-width line being clamped still makes room, so the ellipsis does not push the
// line past the box.
func TestAppendEllipsisMakesRoomWhenFull(t *testing.T) {
	out, _ := AppendEllipsis(glyphRun(10, 10), 100, Glyph{Advance: 10})
	var w float64
	for _, g := range out {
		w += g.Advance
	}
	if w > 100 {
		t.Errorf("width %v exceeds the 100 available", w)
	}
}

// The ellipsis inherits the styling of the glyph it follows, so a line ending in a
// differently-coloured span gets a matching ellipsis rather than the block's default.
func TestEllipsisInheritsPrecedingGlyphStyle(t *testing.T) {
	line := glyphRun(20, 10)
	want := Color{R: 1, G: 2, B: 3, A: 255}
	for i := range line {
		line[i].Color = want
	}
	out, _ := TruncateWithEllipsis(line, 100, Glyph{Advance: 10})
	if got := out[len(out)-1].Color; got != want {
		t.Errorf("ellipsis colour = %v, want %v (should match the text it truncates)", got, want)
	}
}
