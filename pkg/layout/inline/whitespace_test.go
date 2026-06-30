package inline

import (
	"testing"

	layoutfont "github.com/nathanstitt/doctaculous/pkg/layout/font"
)

func TestFlagsFor(t *testing.T) {
	cases := []struct {
		ws                   string
		cSpaces, preNL, wrap bool
	}{
		{"normal", true, false, true},
		{"nowrap", true, false, false},
		{"pre", false, true, false},
		{"pre-wrap", false, true, true},
		{"pre-line", true, true, true},
		{"", true, false, true},
	}
	for _, c := range cases {
		cs, nl, w := flagsFor(c.ws)
		if cs != c.cSpaces || nl != c.preNL || w != c.wrap {
			t.Errorf("flagsFor(%q) = (%v,%v,%v), want (%v,%v,%v)", c.ws, cs, nl, w, c.cSpaces, c.preNL, c.wrap)
		}
	}
}

// In a preserving mode a literal '\n' in the run text becomes a hard-break glyph.
func TestShapePreservedNewlineBecomesBreak(t *testing.T) {
	faces := layoutfont.NewFaceCache()
	glyphs := Shape(faces, []Run{{Text: "a\nb", Family: "serif", SizePt: 12, WhiteSpace: "pre"}}, nil)
	breaks := 0
	for _, g := range glyphs {
		if g.Break {
			breaks++
		}
	}
	if breaks != 1 {
		t.Errorf("pre run %q produced %d break glyphs, want 1", "a\\nb", breaks)
	}
}

// In a collapsing mode a '\n' that somehow survives to the shaper is NOT a break
// (box-gen would normally have collapsed it; this is the defensive path).
func TestShapeNormalNewlineNotBreak(t *testing.T) {
	faces := layoutfont.NewFaceCache()
	glyphs := Shape(faces, []Run{{Text: "a\nb", Family: "serif", SizePt: 12, WhiteSpace: "normal"}}, nil)
	for _, g := range glyphs {
		if g.Break {
			t.Error("normal run should not produce a break glyph from a newline")
		}
	}
}

// A tab in a preserving mode advances to the next tab stop: the tab's advance plus
// the preceding column lands on a multiple of (tabSize × space-advance).
func TestShapeTabAdvancesToTabStop(t *testing.T) {
	faces := layoutfont.NewFaceCache()
	// One leading tab from column 0 must advance a full tab-stop interval.
	g := Shape(faces, []Run{{Text: "\t", Family: "serif", SizePt: 12, WhiteSpace: "pre"}}, nil)
	if len(g) != 1 || !g[0].Space {
		t.Fatalf("tab produced %d glyphs (want 1 space-type)", len(g))
	}
	// The space advance for the face.
	sp := Shape(faces, []Run{{Text: " ", Family: "serif", SizePt: 12, WhiteSpace: "pre"}}, nil)
	if len(sp) != 1 {
		t.Fatalf("space produced %d glyphs", len(sp))
	}
	want := tabSize * sp[0].Advance
	if d := g[0].Advance - want; d < -0.01 || d > 0.01 {
		t.Errorf("leading tab advance = %.3f, want one tab stop %.3f", g[0].Advance, want)
	}
}

// A non-wrapping run does not break at the width: the whole run is one line, both via
// the breaker's wrap=false mode AND because its glyphs carry NoWrap (so even a
// wrap=true breaker won't break inside it — the inline-span-nowrap-in-a-wrapping-block
// case).
func TestBreakNextNoWrap(t *testing.T) {
	faces := layoutfont.NewFaceCache()
	nw := Shape(faces, []Run{{Text: "one two three four", Family: "serif", SizePt: 12, WhiteSpace: "nowrap"}}, nil)
	// A tiny width that WOULD wrap a normal run.
	if _, rest := BreakNextWrap(nw, 1, false); len(rest) != 0 {
		t.Errorf("nowrap (wrap=false) left %d glyphs; want the whole run on one line", len(rest))
	}
	// Even with wrap=true the NoWrap glyphs are not break opportunities → one line.
	if _, rest := BreakNextWrap(nw, 1, true); len(rest) != 0 {
		t.Errorf("nowrap glyphs under wrap=true left %d; a nowrap span must not break inside a wrapping block", len(rest))
	}
	// Sanity: a NORMAL run at width 1 DOES wrap (leaves a remainder).
	norm := Shape(faces, []Run{{Text: "one two three four", Family: "serif", SizePt: 12, WhiteSpace: "normal"}}, nil)
	if _, rest := BreakNextWrap(norm, 1, true); len(rest) == 0 {
		t.Error("normal run at width 1 should leave a remainder (sanity check)")
	}
}

// No-wrap still ends a line at a hard break (preserved newline).
func TestBreakNextNoWrapHonorsHardBreak(t *testing.T) {
	faces := layoutfont.NewFaceCache()
	glyphs := Shape(faces, []Run{{Text: "a\nb", Family: "serif", SizePt: 12, WhiteSpace: "pre"}}, nil)
	line, rest := BreakNextWrap(glyphs, 1000, false)
	if len(rest) == 0 {
		t.Error("nowrap should still split at a hard break")
	}
	for _, g := range line {
		if g.Break {
			t.Error("the break glyph should be consumed, not kept on the line")
		}
	}
}
