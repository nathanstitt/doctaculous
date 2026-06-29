package inline

import (
	"image/color"
	"testing"

	layoutfont "github.com/nathanstitt/doctaculous/pkg/layout/font"
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

func TestShapeMissingFamilySkippedAndLogged(t *testing.T) {
	faces := layoutfont.NewFaceCache()
	logged := 0
	logf := func(string, ...any) { logged++ }
	glyphs := Shape(faces, []Run{{
		Text:   "hello",
		Family: "NoSuchFontXYZ",
		SizePt: 12,
		Color:  color.RGBA{A: 0xff},
	}}, logf)
	if len(glyphs) != 0 {
		t.Errorf("glyphs = %d, want 0 (missing family yields nothing)", len(glyphs))
	}
	if logged == 0 {
		t.Errorf("expected logf to be invoked for the missing family")
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
