package paint

import (
	"image"
	"image/color"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/layout"
	"github.com/nathanstitt/doctaculous/pkg/render"
	"github.com/nathanstitt/doctaculous/pkg/render/raster"
)

// recordDevice is a render.Device that records the FillGlyph and Fill calls
// PaintPage makes, so tests can assert what was drawn and where without
// rasterizing. All other Device methods are no-ops.
type recordDevice struct {
	glyphs   []recordedGlyph
	fills    []recordedFill
	saves    int
	restores int
	clips    []*render.Path
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
func (d *recordDevice) PushClip(p *render.Path, _ render.FillRule)       { d.clips = append(d.clips, p) }
func (d *recordDevice) Save()                                            { d.saves++ }
func (d *recordDevice) Restore()                                         { d.restores++ }

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

// newRasterPage rasterizes a page at scale 1 (page points map 1:1 to pixels, so
// page-space coordinates are pixel coordinates) onto a w×h white canvas, returning
// the resulting image for pixel assertions. It mirrors the reflow backend's device
// setup (raster.New + a uniform scale matrix).
func newRasterPage(w, h int, page *layout.Page) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 0xff // opaque white background
	}
	dev := raster.New(img)
	PaintPage(dev, page, render.Scale(1, 1))
	return img
}

// isColor reports whether got matches want within a small per-channel tolerance,
// absorbing anti-aliasing jitter at fill edges (assertions target pixel centers
// well inside a fill, so this stays tight).
func isColor(got, want color.RGBA, tol uint8) bool {
	d := func(a, b uint8) uint8 {
		if a > b {
			return a - b
		}
		return b - a
	}
	return d(got.R, want.R) <= tol && d(got.G, want.G) <= tol &&
		d(got.B, want.B) <= tol && d(got.A, want.A) <= tol
}

func TestPaintBackground(t *testing.T) {
	red := color.RGBA{R: 0xc0, G: 0x10, B: 0x20, A: 0xff}
	// A background fill covering [10,40]×[10,30] on a 50×50 white canvas.
	page := &layout.Page{WidthPt: 50, HeightPt: 50, Items: []layout.Item{
		{Kind: layout.BackgroundKind, Rule: layout.RuleItem{
			XPt: 10, YPt: 10, WPt: 30, HPt: 20, Color: red,
		}},
	}}
	img := newRasterPage(50, 50, page)

	if got := img.RGBAAt(25, 20); !isColor(got, red, 2) {
		t.Errorf("center pixel = %v, want red %v", got, red)
	}
	white := color.RGBA{0xff, 0xff, 0xff, 0xff}
	if got := img.RGBAAt(2, 2); !isColor(got, white, 0) {
		t.Errorf("outside pixel = %v, want white", got)
	}
}

func TestPaintBorderSolid(t *testing.T) {
	blue := color.RGBA{R: 0x10, G: 0x20, B: 0xc0, A: 0xff}
	// A top edge strip [5,45]×[5,11] (6px thick) on a 50×50 white canvas.
	page := &layout.Page{WidthPt: 50, HeightPt: 50, Items: []layout.Item{
		{Kind: layout.BorderKind, Border: layout.BorderItem{
			XPt: 5, YPt: 5, WPt: 40, HPt: 6,
			Color: blue, Style: layout.BorderSolid, Side: layout.EdgeTop,
		}},
	}}
	img := newRasterPage(50, 50, page)

	// A pixel inside the strip is the border color.
	if got := img.RGBAAt(25, 8); !isColor(got, blue, 2) {
		t.Errorf("strip pixel = %v, want blue %v", got, blue)
	}
	// A pixel below the strip stays white.
	white := color.RGBA{0xff, 0xff, 0xff, 0xff}
	if got := img.RGBAAt(25, 20); !isColor(got, white, 0) {
		t.Errorf("below-strip pixel = %v, want white", got)
	}
}

func TestPaintBorderDoubleLeavesGap(t *testing.T) {
	green := color.RGBA{R: 0x10, G: 0xa0, B: 0x20, A: 0xff}
	// A top edge strip [5,45]×[5,14] (9px thick) → thirds are 3px each:
	// outer [5,8), middle [8,11), inner [11,14) along Y.
	page := &layout.Page{WidthPt: 50, HeightPt: 50, Items: []layout.Item{
		{Kind: layout.BorderKind, Border: layout.BorderItem{
			XPt: 5, YPt: 5, WPt: 40, HPt: 9,
			Color: green, Style: layout.BorderDouble, Side: layout.EdgeTop,
		}},
	}}
	img := newRasterPage(50, 50, page)

	// Outer third (y≈6) and inner third (y≈12) are filled.
	if got := img.RGBAAt(25, 6); !isColor(got, green, 2) {
		t.Errorf("outer-third pixel = %v, want green %v", got, green)
	}
	if got := img.RGBAAt(25, 12); !isColor(got, green, 2) {
		t.Errorf("inner-third pixel = %v, want green %v", got, green)
	}
	// Middle third (y≈9) is NOT the border color — the gap stays white.
	white := color.RGBA{0xff, 0xff, 0xff, 0xff}
	if got := img.RGBAAt(25, 9); !isColor(got, white, 0) {
		t.Errorf("middle-third pixel = %v, want white gap (got border color?)", got)
	}
}

func TestPaintBorderDashedAlternates(t *testing.T) {
	black := color.RGBA{A: 0xff}
	// A top edge strip starting at x=5, 4px thick. Dash = gap = 3×4 = 12px, so the
	// first dash spans x∈[5,17) and the first gap x∈[17,29). Make it long enough
	// (W=40) to hold a dash + gap + more.
	page := &layout.Page{WidthPt: 60, HeightPt: 30, Items: []layout.Item{
		{Kind: layout.BorderKind, Border: layout.BorderItem{
			XPt: 5, YPt: 5, WPt: 40, HPt: 4,
			Color: black, Style: layout.BorderDashed, Side: layout.EdgeTop,
		}},
	}}
	img := newRasterPage(60, 30, page)

	// A pixel inside the first dash (x≈10) is black.
	if got := img.RGBAAt(10, 7); !isColor(got, black, 2) {
		t.Errorf("first-dash pixel = %v, want black", got)
	}
	// A pixel inside the first gap (x≈23) stays white.
	white := color.RGBA{0xff, 0xff, 0xff, 0xff}
	if got := img.RGBAAt(23, 7); !isColor(got, white, 0) {
		t.Errorf("first-gap pixel = %v, want white", got)
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

// TestPaintClipPushPop: a ClipPushKind item drives Save()+PushClip(rect); a
// ClipPopKind drives Restore(). The pushed clip rect's corners map through the page
// matrix (here a 1:1 scale), so a 10,20,30,40 clip becomes a path at those coords.
func TestPaintClipPushPop(t *testing.T) {
	page := &layout.Page{
		WidthPt: 100, HeightPt: 100,
		Items: []layout.Item{
			{Kind: layout.ClipPushKind, Rule: layout.RuleItem{XPt: 10, YPt: 20, WPt: 30, HPt: 40}},
			{Kind: layout.GlyphKind, Glyph: layout.GlyphItem{Outline: triangle(), XPt: 12, YPt: 22, SizePt: 4, Color: color.RGBA{A: 0xff}}},
			{Kind: layout.ClipPopKind},
		},
	}
	dev := &recordDevice{}
	PaintPage(dev, page, render.Scale(1, 1))

	if dev.saves != 1 || dev.restores != 1 {
		t.Errorf("saves=%d restores=%d, want 1/1", dev.saves, dev.restores)
	}
	if len(dev.clips) != 1 {
		t.Fatalf("pushed %d clips, want 1", len(dev.clips))
	}
	minX, minY, maxX, maxY := pathBounds(dev.clips[0])
	if minX != 10 || minY != 20 || maxX != 40 || maxY != 60 {
		t.Errorf("clip bounds = (%v,%v)-(%v,%v), want (10,20)-(40,60)", minX, minY, maxX, maxY)
	}
	if len(dev.glyphs) != 1 {
		t.Errorf("painted %d glyphs, want 1 (between push and pop)", len(dev.glyphs))
	}
}

// TestClipCutsPixels: a background that extends past a clip rect is painted only
// inside the clip. A 100x100 page, clip rect [0,0,50,50], a background covering the
// whole page: a pixel at (25,25) is the background color; a pixel at (75,75) is the
// white page background (clipped out).
func TestClipCutsPixels(t *testing.T) {
	bg := color.RGBA{0x33, 0x66, 0x99, 0xff}
	page := &layout.Page{
		WidthPt: 100, HeightPt: 100,
		Items: []layout.Item{
			{Kind: layout.ClipPushKind, Rule: layout.RuleItem{XPt: 0, YPt: 0, WPt: 50, HPt: 50}},
			{Kind: layout.BackgroundKind, Rule: layout.RuleItem{XPt: 0, YPt: 0, WPt: 100, HPt: 100, Color: bg}},
			{Kind: layout.ClipPopKind},
		},
	}
	img := newRasterPage(100, 100, page)

	if got := img.RGBAAt(25, 25); !isColor(got, bg, 2) {
		t.Errorf("pixel (25,25) = %v, want background %v (inside clip)", got, bg)
	}
	white := color.RGBA{0xff, 0xff, 0xff, 0xff}
	if got := img.RGBAAt(75, 75); !isColor(got, white, 0) {
		t.Errorf("pixel (75,75) = %v, want white %v (clipped out)", got, white)
	}
}

// pathBounds returns the axis-aligned bounding box of a path's MoveTo/LineTo points.
func pathBounds(p *render.Path) (minX, minY, maxX, maxY float64) {
	first := true
	for _, s := range p.Segments {
		if s.Kind != render.MoveTo && s.Kind != render.LineTo {
			continue
		}
		if first {
			minX, minY, maxX, maxY = s.P0.X, s.P0.Y, s.P0.X, s.P0.Y
			first = false
			continue
		}
		if s.P0.X < minX {
			minX = s.P0.X
		}
		if s.P0.Y < minY {
			minY = s.P0.Y
		}
		if s.P0.X > maxX {
			maxX = s.P0.X
		}
		if s.P0.Y > maxY {
			maxY = s.P0.Y
		}
	}
	return
}
