package paint

import (
	"image"
	"image/color"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/layout"
	"github.com/nathanstitt/doctaculous/pkg/render"
)

// recordDevice is a render.Device that records the FillGlyph and Fill calls
// PaintPage makes, so tests can assert what was drawn and where without
// rasterizing. All other Device methods are no-ops.
type recordDevice struct {
	glyphs []recordedGlyph
	fills  []recordedFill
}

type recordedGlyph struct {
	outline *render.Path
	color   render.FillColor
}

type recordedFill struct {
	path  *render.Path
	paint render.FillPaint
}

func (d *recordDevice) Size() (int, int) { return 0, 0 }
func (d *recordDevice) Fill(p *render.Path, paint render.FillPaint) {
	d.fills = append(d.fills, recordedFill{path: p, paint: paint})
}
func (d *recordDevice) Stroke(*render.Path, render.StrokePaint)               {}
func (d *recordDevice) DrawImage(image.Image, render.Matrix, float64, string) {}
func (d *recordDevice) FillGlyph(outline *render.Path, c render.FillColor, _ string) {
	d.glyphs = append(d.glyphs, recordedGlyph{outline: outline, color: c})
}
func (d *recordDevice) FillShading(render.Shader, render.Matrix, string) {}
func (d *recordDevice) PushClip(*render.Path, render.FillRule)           {}
func (d *recordDevice) Save()                                            {}
func (d *recordDevice) Restore()                                         {}

// triangle returns a small closed outline in em units so a glyph is non-empty.
func triangle() *render.Path {
	p := &render.Path{}
	p.MoveTo(0, 0)
	p.LineTo(1, 0)
	p.LineTo(0, 1)
	p.Close()
	return p
}

func TestPaintGlyphSkipsEmptyOutlines(t *testing.T) {
	page := &layout.Page{
		WidthPt:  100,
		HeightPt: 100,
		Items: []layout.Item{
			{Kind: layout.GlyphKind, Glyph: layout.GlyphItem{Outline: nil, SizePt: 12}},
			{Kind: layout.GlyphKind, Glyph: layout.GlyphItem{Outline: &render.Path{}, SizePt: 12}},
			{Kind: layout.GlyphKind, Glyph: layout.GlyphItem{
				Outline: triangle(), XPt: 10, YPt: 20, SizePt: 12,
				Color: color.RGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xff},
			}},
		},
	}
	dev := &recordDevice{}
	PaintPage(dev, page, render.Identity)

	if len(dev.glyphs) != 1 {
		t.Fatalf("painted %d glyphs, want 1 (nil and empty outlines skipped)", len(dev.glyphs))
	}
	if got := dev.glyphs[0].color; got != (render.FillColor{R: 0x11, G: 0x22, B: 0x33, A: 0xff}) {
		t.Errorf("glyph color = %+v, want {0x11,0x22,0x33,0xff}", got)
	}
}

func TestPaintGlyphTransform(t *testing.T) {
	// A glyph at baseline (10,20), size 2: the em-space point (1,0) maps to
	// (10+1*2, 20-0*2) = (12,20) and (0,1) maps to (10, 20-1*2) = (10,18) under the
	// identity page matrix (em→points flips Y, then translate to baseline).
	page := &layout.Page{
		WidthPt:  100,
		HeightPt: 100,
		Items: []layout.Item{{Kind: layout.GlyphKind, Glyph: layout.GlyphItem{
			Outline: triangle(), XPt: 10, YPt: 20, SizePt: 2,
			Color: color.RGBA{A: 0xff},
		}}},
	}
	dev := &recordDevice{}
	PaintPage(dev, page, render.Identity)

	if len(dev.glyphs) != 1 {
		t.Fatalf("painted %d glyphs, want 1", len(dev.glyphs))
	}
	segs := dev.glyphs[0].outline.Segments
	// Segment 0 is MoveTo(0,0) -> (10,20); segment 1 LineTo(1,0) -> (12,20);
	// segment 2 LineTo(0,1) -> (10,18).
	wantPts := []render.Point{{X: 10, Y: 20}, {X: 12, Y: 20}, {X: 10, Y: 18}}
	for i, want := range wantPts {
		if got := segs[i].P0; got != want {
			t.Errorf("segment %d origin = %+v, want %+v", i, got, want)
		}
	}
}

func TestPaintRuleSkipsDegenerate(t *testing.T) {
	page := &layout.Page{
		WidthPt:  100,
		HeightPt: 100,
		Items: []layout.Item{
			{Kind: layout.RuleKind, Rule: layout.RuleItem{XPt: 0, YPt: 0, WPt: 0, HPt: 1}},  // zero width
			{Kind: layout.RuleKind, Rule: layout.RuleItem{XPt: 0, YPt: 0, WPt: 1, HPt: 0}},  // zero height
			{Kind: layout.RuleKind, Rule: layout.RuleItem{XPt: 1, YPt: 2, WPt: 10, HPt: 1}}, // real
		},
	}
	dev := &recordDevice{}
	PaintPage(dev, page, render.Identity)

	if len(dev.fills) != 1 {
		t.Fatalf("filled %d rules, want 1 (degenerate rects skipped)", len(dev.fills))
	}
	if dev.fills[0].paint.Rule != render.NonZero {
		t.Errorf("rule fill uses %v, want NonZero", dev.fills[0].paint.Rule)
	}
}
