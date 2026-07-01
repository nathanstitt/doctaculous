package inline

import (
	"image/color"
	"testing"

	layoutfont "github.com/nathanstitt/doctaculous/pkg/layout/font"
)

// TestShapeCarriesFaceAndRune asserts every inked glyph carries the font identity
// (Face + source runes) a text-emitting backend needs.
func TestShapeCarriesFaceAndRune(t *testing.T) {
	faces := layoutfont.NewFaceCache()
	glyphs := Shape(faces, []Run{{Text: "Hi", Family: "Helvetica", SizePt: 12, Color: color.RGBA{A: 255}}}, nil)
	var inked int
	for _, g := range glyphs {
		if g.Outline == nil {
			continue
		}
		inked++
		if g.Face == nil {
			t.Error("inked glyph has nil Face")
		}
		if len(g.Runes) == 0 {
			t.Error("inked glyph has no Runes")
		}
	}
	if inked != 2 {
		t.Fatalf("inked glyphs = %d; want 2", inked)
	}
}

// TestShapeSyntheticBulletClearsFace pins the regression where a synthesized marker
// glyph (a bullet the face lacks, e.g. ▪ U+25AA) kept a Face+.notdef GID, so paint's
// DrawGlyph route re-fetched glyph 0 and drew nothing. A synthetic glyph must carry a
// NIL Face so paint fills its attached Outline directly.
func TestShapeSyntheticBulletClearsFace(t *testing.T) {
	faces := layoutfont.NewFaceCache()
	// ▪ (U+25AA) is absent from the bundled faces; the shaper synthesizes its outline.
	glyphs := Shape(faces, []Run{{Text: "▪", Family: "serif", SizePt: 12, Color: color.RGBA{A: 255}}}, nil)
	var inked int
	for _, g := range glyphs {
		if g.Outline == nil {
			continue
		}
		inked++
		if g.Face != nil {
			t.Error("synthetic bullet glyph kept a non-nil Face; paint would draw .notdef")
		}
	}
	if inked != 1 {
		t.Fatalf("inked glyphs = %d; want 1 (the synthesized square)", inked)
	}
}
