package paint

import (
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/font"
	"github.com/nathanstitt/doctaculous/pkg/layout"
	"github.com/nathanstitt/doctaculous/pkg/render"
)

// glyphRouteDev embeds render.Device (so it satisfies the full interface) and
// counts only the two glyph methods; the other methods are nil and unused here.
type glyphRouteDev struct {
	render.Device
	drawGlyphs int
	fillGlyphs int
}

func (r *glyphRouteDev) DrawGlyph(render.GlyphRef)                    { r.drawGlyphs++ }
func (r *glyphRouteDev) FillGlyph(*render.Path, render.FillColor, string) { r.fillGlyphs++ }

// TestPaintGlyphPrefersDrawGlyph asserts a glyph carrying font identity routes
// through DrawGlyph (so a text-emitting backend can embed real text).
func TestPaintGlyphPrefersDrawGlyph(t *testing.T) {
	face, ok := font.LoadStandard("Helvetica", font.Style{})
	if !ok {
		t.Fatal("no Helvetica")
	}
	gid, _ := face.GID('A')
	pg := &layout.Page{
		WidthPt: 100, HeightPt: 100,
		Items: []layout.Item{{
			Kind: layout.GlyphKind,
			Glyph: layout.GlyphItem{
				Outline: face.Outline(gid), XPt: 10, YPt: 20, SizePt: 12,
				Face: face, GID: gid, Runes: []rune{'A'},
			},
		}},
	}
	dev := &glyphRouteDev{}
	PaintPage(dev, pg, render.Scale(1, 1))
	if dev.drawGlyphs != 1 || dev.fillGlyphs != 0 {
		t.Fatalf("DrawGlyph=%d FillGlyph=%d; want 1, 0", dev.drawGlyphs, dev.fillGlyphs)
	}
}

// TestPaintGlyphFallsBackToFillGlyph asserts a glyph WITHOUT font identity (only an
// outline) still routes through FillGlyph, so non-identity callers are unchanged.
func TestPaintGlyphFallsBackToFillGlyph(t *testing.T) {
	pg := &layout.Page{
		WidthPt: 100, HeightPt: 100,
		Items: []layout.Item{{
			Kind:  layout.GlyphKind,
			Glyph: layout.GlyphItem{Outline: triangle(), XPt: 10, YPt: 20, SizePt: 12},
		}},
	}
	dev := &glyphRouteDev{}
	PaintPage(dev, pg, render.Scale(1, 1))
	if dev.drawGlyphs != 0 || dev.fillGlyphs != 1 {
		t.Fatalf("DrawGlyph=%d FillGlyph=%d; want 0, 1", dev.drawGlyphs, dev.fillGlyphs)
	}
}
