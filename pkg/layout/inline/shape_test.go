package inline

import (
	"image/color"
	"testing"

	layoutfont "github.com/nathanstitt/omnidoc/pkg/layout/font"
)

func TestShapeHardBreakRun(t *testing.T) {
	faces := layoutfont.NewFaceCache()
	glyphs := Shape(faces, []Run{{Break: true}}, nil)
	if len(glyphs) != 1 {
		t.Fatalf("glyphs = %d, want 1", len(glyphs))
	}
	if !glyphs[0].Break {
		t.Errorf("expected a hard-break glyph (Break==true)")
	}
}

// A run naming a family the host does not have still SHAPES, using the face cache's
// bundled terminal fallback. It used to yield no glyphs at all, which rendered a page
// whose every font-family was unavailable as an empty box — a font failure that read
// as a layout failure. The substitution is logged by the face cache (see
// pkg/layout/font.TestResolveTerminalFallbackLogsOnce), not here.
func TestShapeMissingFamilyFallsBackToBundled(t *testing.T) {
	faces := layoutfont.NewFaceCache()
	glyphs := Shape(faces, []Run{{
		Text:   "hello",
		Family: "NoSuchFontXYZ",
		SizePt: 12,
		Color:  color.RGBA{A: 0xff},
	}}, nil)
	if len(glyphs) != len("hello") {
		t.Fatalf("glyphs = %d, want %d (missing family falls back to the bundled face)", len(glyphs), len("hello"))
	}
	// The glyphs must be drawable, not just present: real advances and real outlines.
	for i, g := range glyphs {
		if g.Advance <= 0 {
			t.Errorf("glyph %d has advance %v; want > 0", i, g.Advance)
		}
		if g.Outline == nil {
			t.Errorf("glyph %d has no outline; the fallback face produced no ink", i)
		}
	}
}

func TestShapeZeroAlphaColorBecomesOpaque(t *testing.T) {
	faces := layoutfont.NewFaceCache()
	glyphs := Shape(faces, []Run{{
		Text:   "A",
		Family: "Arial",
		SizePt: 12,
		Color:  color.RGBA{R: 10, G: 20, B: 30, A: 0}, // zero alpha => treated as opaque
	}}, nil)
	if len(glyphs) == 0 {
		t.Fatal("expected at least one glyph for 'A'")
	}
	if glyphs[0].Color.A != 0xff {
		t.Errorf("Color.A = %d, want 0xff (zero-alpha fixup)", glyphs[0].Color.A)
	}
}

func TestShapeSpaceRune(t *testing.T) {
	faces := layoutfont.NewFaceCache()
	glyphs := Shape(faces, []Run{{
		Text:   " ",
		Family: "Arial",
		SizePt: 12,
		Color:  color.RGBA{A: 0xff},
	}}, nil)
	if len(glyphs) != 1 {
		t.Fatalf("glyphs = %d, want 1 (one space)", len(glyphs))
	}
	if !glyphs[0].Space {
		t.Errorf("expected Space==true for a space rune")
	}
	if glyphs[0].Outline != nil {
		t.Errorf("expected a space to carry no outline")
	}
}

func TestShapeAtomicRun(t *testing.T) {
	faces := layoutfont.NewFaceCache()
	atom := &AtomicItem{WidthPt: 42, HeightPt: 10, BaselinePt: 8}
	glyphs := Shape(faces, []Run{{Atomic: atom}}, nil)
	if len(glyphs) != 1 {
		t.Fatalf("glyphs = %d, want 1 (one atomic box)", len(glyphs))
	}
	if glyphs[0].Atomic == nil {
		t.Fatalf("expected the glyph to carry its AtomicItem")
	}
	if glyphs[0].Advance != atom.WidthPt {
		t.Errorf("Advance = %v, want %v (atomic width)", glyphs[0].Advance, atom.WidthPt)
	}
}
