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
