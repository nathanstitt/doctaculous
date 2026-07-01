package raster

import (
	"image"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/font"
	"github.com/nathanstitt/doctaculous/pkg/render"
)

// TestDrawGlyphMatchesFillGlyph asserts the raster backend renders DrawGlyph by
// filling the face's outline, producing pixels identical to the equivalent
// FillGlyph call — so existing goldens stay byte-identical after the seam lands.
func TestDrawGlyphMatchesFillGlyph(t *testing.T) {
	face, ok := font.LoadStandard("Helvetica", font.Style{})
	if !ok {
		t.Fatal("LoadStandard Helvetica: not available")
	}
	gid, ok := face.GID('A')
	if !ok {
		t.Fatal("face has no glyph for 'A'")
	}
	outline := face.Outline(gid)
	if outline == nil {
		t.Fatal("nil outline for 'A'")
	}
	// em -> device: scale by 40, flip Y, translate down so the glyph is on-canvas.
	m := render.Scale(40, -40).Mul(render.Translate(5, 45))

	want := image.NewRGBA(image.Rect(0, 0, 50, 50))
	devWant := New(want)
	devWant.FillGlyph(render.TransformPath(outline, m), render.FillColor{A: 255}, "")

	got := image.NewRGBA(image.Rect(0, 0, 50, 50))
	devGot := New(got)
	devGot.DrawGlyph(render.GlyphRef{
		Face: face, GID: gid, Runes: []rune{'A'},
		Transform: m, Color: render.FillColor{A: 255},
	})

	for i := range want.Pix {
		if want.Pix[i] != got.Pix[i] {
			t.Fatalf("pixel %d differs: FillGlyph=%d DrawGlyph=%d", i, want.Pix[i], got.Pix[i])
		}
	}
}
