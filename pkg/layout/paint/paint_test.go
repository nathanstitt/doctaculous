package paint

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"strings"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/filtereffects"

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
	// groups records BeginGroup/EndGroup in call order ("begin"/"end"), so a test can
	// assert both the count AND the nesting a filter bracket must produce.
	groups      []string
	groupAlphas []float64
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
func (d *recordDevice) DrawGlyph(g render.GlyphRef) {
	// Mirror the rasterizer: record the face's transformed outline so glyph-count
	// and geometry assertions hold whether paint routes via FillGlyph or DrawGlyph.
	var o *render.Path
	if g.Face != nil {
		o = render.TransformPath(g.Face.Outline(g.GID), g.Transform)
	}
	d.glyphs = append(d.glyphs, recordedGlyph{outline: o, color: g.Color})
}
func (d *recordDevice) FillShading(render.Shader, render.Matrix, string) {}
func (d *recordDevice) PushClip(p *render.Path, _ render.FillRule)       { d.clips = append(d.clips, p) }
func (d *recordDevice) BeginGroup()                                      { d.groups = append(d.groups, "begin") }
func (d *recordDevice) EndGroup(alpha float64, _ string, _, _ render.GroupMask) {
	d.groups = append(d.groups, "end")
	d.groupAlphas = append(d.groupAlphas, alpha)
}
func (d *recordDevice) Save()    { d.saves++ }
func (d *recordDevice) Restore() { d.restores++ }
func (d *recordDevice) BuildClipMask([]render.MaskPath) render.GroupMask {
	return image.NewAlpha(image.Rectangle{})
}
func (d *recordDevice) BuildLuminanceMask(image.Point, bool, func(render.Device)) render.GroupMask {
	return image.NewAlpha(image.Rectangle{})
}

func (d *recordDevice) RenderOffscreen(image.Point, func(render.Device)) *image.RGBA {
	return nil
}

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

// TestPaintBorderOutsetShadesBySide pins F5: an outset edge lights the top/left edges
// (base color) and darkens the bottom/right edges (~half). Two strips, a top and a
// bottom, of the same base color must paint different shades.
func TestPaintBorderOutsetShadesBySide(t *testing.T) {
	base := color.RGBA{R: 0x80, G: 0x80, B: 0x80, A: 0xff}
	page := &layout.Page{WidthPt: 50, HeightPt: 50, Items: []layout.Item{
		{Kind: layout.BorderKind, Border: layout.BorderItem{ // top edge
			XPt: 5, YPt: 5, WPt: 40, HPt: 6,
			Color: base, Style: layout.BorderOutset, Side: layout.EdgeTop,
		}},
		{Kind: layout.BorderKind, Border: layout.BorderItem{ // bottom edge
			XPt: 5, YPt: 39, WPt: 40, HPt: 6,
			Color: base, Style: layout.BorderOutset, Side: layout.EdgeBottom,
		}},
	}}
	img := newRasterPage(50, 50, page)
	// Top edge: lit → base color (0x80).
	if got := img.RGBAAt(25, 8); !isColor(got, base, 2) {
		t.Errorf("outset top = %v, want base %v (lit side)", got, base)
	}
	// Bottom edge: dark → ~half (0x40).
	dark := color.RGBA{R: 0x40, G: 0x40, B: 0x40, A: 0xff}
	if got := img.RGBAAt(25, 42); !isColor(got, dark, 2) {
		t.Errorf("outset bottom = %v, want darkened %v (shadow side)", got, dark)
	}
}

// TestPaintBorderRidgeSplitsThickness pins F5: a ridge edge splits the strip across its
// thickness — the outer half is lit, the inner half dark (for a top edge). A 10px-thick
// top strip [5,15] has its outer half [5,10) lit and inner half [10,15) dark.
func TestPaintBorderRidgeSplitsThickness(t *testing.T) {
	base := color.RGBA{R: 0x80, G: 0x80, B: 0x80, A: 0xff}
	dark := color.RGBA{R: 0x40, G: 0x40, B: 0x40, A: 0xff}
	page := &layout.Page{WidthPt: 50, HeightPt: 50, Items: []layout.Item{
		{Kind: layout.BorderKind, Border: layout.BorderItem{
			XPt: 5, YPt: 5, WPt: 40, HPt: 10,
			Color: base, Style: layout.BorderRidge, Side: layout.EdgeTop,
		}},
	}}
	img := newRasterPage(50, 50, page)
	// Outer half (y≈7) lit; inner half (y≈12) dark.
	if got := img.RGBAAt(25, 7); !isColor(got, base, 2) {
		t.Errorf("ridge outer half = %v, want base %v (lit)", got, base)
	}
	if got := img.RGBAAt(25, 12); !isColor(got, dark, 2) {
		t.Errorf("ridge inner half = %v, want dark %v", got, dark)
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

// TestPaintFilterPushPopDrivesGroup: on a backend with NO offscreen raster
// surface (recordDevice.RenderOffscreen returns nil, exactly as pkg/render/pdfwrite
// does), a filter bracket degrades to a plain compositing group — BeginGroup, the
// bracketed content, EndGroup at alpha 1 with no blend mode and no masks — so the
// content is present and correctly placed, just unfiltered.
//
// This is the documented, permanent behavior for a vector backend, not a gap: PDF
// has no filter operator and a blur has no vector representation.
func TestPaintFilterPushPopDrivesGroup(t *testing.T) {
	chain := []filtereffects.Function{{Kind: filtereffects.FuncGrayscale, Amount: 1}}
	page := &layout.Page{
		WidthPt: 100, HeightPt: 100,
		Items: []layout.Item{
			{Kind: layout.FilterPushKind, Filter: layout.FilterItem{Funcs: chain, XPt: 10, YPt: 20, WPt: 30, HPt: 40}},
			{Kind: layout.GlyphKind, Glyph: layout.GlyphItem{Outline: triangle(), XPt: 12, YPt: 22, SizePt: 4, Color: color.RGBA{A: 0xff}}},
			{Kind: layout.FilterPopKind},
		},
	}
	dev := &recordDevice{}
	PaintPage(dev, page, render.Scale(1, 1))

	if got := strings.Join(dev.groups, ","); got != "begin,end" {
		t.Errorf("group calls = %q, want \"begin,end\"", got)
	}
	if len(dev.groupAlphas) != 1 || dev.groupAlphas[0] != 1 {
		t.Errorf("EndGroup alphas = %v, want [1] (pass-through composite)", dev.groupAlphas)
	}
	if len(dev.glyphs) != 1 {
		t.Errorf("painted %d glyphs, want 1 (inside the group)", len(dev.glyphs))
	}
}

// TestPaintFilterNests: nested filter brackets produce nested groups, in order.
func TestPaintFilterNests(t *testing.T) {
	page := &layout.Page{
		WidthPt: 100, HeightPt: 100,
		Items: []layout.Item{
			{Kind: layout.FilterPushKind},
			{Kind: layout.FilterPushKind},
			{Kind: layout.FilterPopKind},
			{Kind: layout.FilterPopKind},
		},
	}
	dev := &recordDevice{}
	PaintPage(dev, page, render.Identity)
	if got := strings.Join(dev.groups, ","); got != "begin,begin,end,end" {
		t.Errorf("group calls = %q, want \"begin,begin,end,end\"", got)
	}
}

// filterProbe is the source colour every mapping assertion below filters. Its
// three channels are deliberately distinct and none is 0 or 255, so a matrix that
// mixes channels, one that scales them, and one that leaves them alone all produce
// visibly different results.
var filterProbe = color.RGBA{0x33, 0x66, 0x99, 0xff} // 51, 102, 153

// rasterFiltered paints a 40x40 square of filterProbe inside a filter bracket
// carrying chain, at 1 device pixel per point, and returns the resulting page.
func rasterFiltered(chain []filtereffects.Function, shadows []color.RGBA) *image.RGBA {
	return newRasterPage(100, 100, &layout.Page{WidthPt: 100, HeightPt: 100, Items: []layout.Item{
		{Kind: layout.FilterPushKind, Filter: layout.FilterItem{
			Funcs: chain, ShadowColors: shadows,
			XPt: 10, YPt: 10, WPt: 40, HPt: 40,
		}},
		{Kind: layout.BackgroundKind, Rule: layout.RuleItem{XPt: 10, YPt: 10, WPt: 40, HPt: 40, Color: filterProbe}},
		{Kind: layout.FilterPopKind},
	}})
}

// TestPaintFilterFunctionMapping pins each CSS filter function's MAPPING onto its
// spec-defined primitive, against HAND-COMPUTED pixel values rather than merely
// asserting that the output changed.
//
// Every `want` below is derived from the Filter Effects specification's own
// formula applied to filterProbe (51, 102, 153) by hand:
//
//   - grayscale(1) is saturate(0), i.e. the luminance 0.213R + 0.715G + 0.072B =
//     10.86 + 72.93 + 11.02 = 94.8 on every channel.
//   - hue-rotate(0deg) reduces exactly to the identity (cos=1, sin=0), which is
//     the cheapest check that the spec's constants were transcribed correctly.
//   - invert(1) flips each channel: 255-51, 255-102, 255-153.
//   - brightness(0.5) halves each channel; contrast(0) collapses every channel to
//     the 0.5 midpoint (128); saturate(1) and sepia(0) are the identity.
//   - sepia(1) uses the spec's fixed matrix: R' = 0.393·51 + 0.769·102 + 0.189·153
//     = 20.04 + 78.44 + 28.92 = 127.4, and likewise for G' and B'.
func TestPaintFilterFunctionMapping(t *testing.T) {
	fn := func(k filtereffects.FunctionKind, amount float64) []filtereffects.Function {
		return []filtereffects.Function{{Kind: k, Amount: amount}}
	}
	for _, tc := range []struct {
		name  string
		chain []filtereffects.Function
		want  color.RGBA
	}{
		{"grayscale(1)", fn(filtereffects.FuncGrayscale, 1), color.RGBA{95, 95, 95, 255}},
		// grayscale(2) must not over-rotate past greyscale: it is clamped to 1.
		{"grayscale(2)", fn(filtereffects.FuncGrayscale, 2), color.RGBA{95, 95, 95, 255}},
		{"grayscale(0)", fn(filtereffects.FuncGrayscale, 0), filterProbe},
		{"invert(1)", fn(filtereffects.FuncInvert, 1), color.RGBA{204, 153, 102, 255}},
		{"invert(0)", fn(filtereffects.FuncInvert, 0), filterProbe},
		// invert(0.5) sends every channel to the 0.5 midpoint: v·(1-2a)+a = 0.5.
		{"invert(0.5)", fn(filtereffects.FuncInvert, 0.5), color.RGBA{128, 128, 128, 255}},
		{"brightness(1)", fn(filtereffects.FuncBrightness, 1), filterProbe},
		{"brightness(0.5)", fn(filtereffects.FuncBrightness, 0.5), color.RGBA{26, 51, 77, 255}},
		{"brightness(0)", fn(filtereffects.FuncBrightness, 0), color.RGBA{0, 0, 0, 255}},
		{"contrast(1)", fn(filtereffects.FuncContrast, 1), filterProbe},
		{"contrast(0)", fn(filtereffects.FuncContrast, 0), color.RGBA{128, 128, 128, 255}},
		// contrast(2): 2v - 0.5, so 51→0 (clamped from -25.5), 102→76.5, 153→178.5.
		{"contrast(2)", fn(filtereffects.FuncContrast, 2), color.RGBA{0, 77, 179, 255}},
		{"saturate(1)", fn(filtereffects.FuncSaturate, 1), filterProbe},
		{"saturate(0)", fn(filtereffects.FuncSaturate, 0), color.RGBA{95, 95, 95, 255}},
		{"sepia(0)", fn(filtereffects.FuncSepia, 0), filterProbe},
		{"sepia(1)", fn(filtereffects.FuncSepia, 1), color.RGBA{127, 113, 88, 255}},
		{"hue-rotate(0)", []filtereffects.Function{{Kind: filtereffects.FuncHueRotate}}, filterProbe},
	} {
		got := rasterFiltered(tc.chain, nil).RGBAAt(30, 30)
		// A tolerance of 1 absorbs only the float32→uint8 rounding of the
		// sRGB round trip, not any difference in the matrix itself.
		if !isColor(got, tc.want, 1) {
			t.Errorf("%s produced %v, want %v (the spec's own formula on %v)", tc.name, got, tc.want, filterProbe)
		}
	}
}

// TestPaintFilterOpacityMapping: opacity(a) scales the ALPHA channel only, so the
// filtered square composites over the white page toward white rather than changing
// hue. opacity(0.5) over white gives (51+255)/2, (102+255)/2, (153+255)/2.
func TestPaintFilterOpacityMapping(t *testing.T) {
	got := rasterFiltered([]filtereffects.Function{{Kind: filtereffects.FuncOpacity, Amount: 0.5}}, nil).RGBAAt(30, 30)
	if want := (color.RGBA{153, 179, 204, 255}); !isColor(got, want, 2) {
		t.Errorf("opacity(0.5) produced %v, want %v (half coverage over the white page)", got, want)
	}
	// opacity(0) removes the box entirely: the white page shows through.
	got = rasterFiltered([]filtereffects.Function{{Kind: filtereffects.FuncOpacity}}, nil).RGBAAt(30, 30)
	if want := (color.RGBA{255, 255, 255, 255}); !isColor(got, want, 1) {
		t.Errorf("opacity(0) produced %v, want the bare page %v", got, want)
	}
}

// TestPaintFilterComposesInWrittenOrder: two functions apply LEFT TO RIGHT, each
// consuming the previous one's output.
//
// The pair is chosen so the order is observable. `invert(1) grayscale(1)` inverts
// first (51,102,153 → 204,153,102) and then takes that result's luminance
// (0.213·204 + 0.715·153 + 0.072·102 = 43.5 + 109.4 + 7.3 = 160). The reverse
// greys first (→ 95,95,95) and then inverts (→ 160,160,160) — numerically the same
// here, which is exactly why a SECOND, asymmetric pair is needed.
//
// `brightness(2) grayscale(1)` doubles first (102,204,255 after clamping) and then
// greys (0.213·102 + 0.715·204 + 0.072·255 = 21.7 + 145.9 + 18.4 = 186), while
// `grayscale(1) brightness(2)` greys first (95) and then doubles to 190. Those two
// differ, and by more than the rounding tolerance.
func TestPaintFilterComposesInWrittenOrder(t *testing.T) {
	bright := filtereffects.Function{Kind: filtereffects.FuncBrightness, Amount: 2}
	gray := filtereffects.Function{Kind: filtereffects.FuncGrayscale, Amount: 1}

	forward := rasterFiltered([]filtereffects.Function{bright, gray}, nil).RGBAAt(30, 30)
	if want := (color.RGBA{186, 186, 186, 255}); !isColor(forward, want, 1) {
		t.Errorf("brightness(2) grayscale(1) = %v, want %v (brighten THEN grey)", forward, want)
	}
	reverse := rasterFiltered([]filtereffects.Function{gray, bright}, nil).RGBAAt(30, 30)
	if want := (color.RGBA{190, 190, 190, 255}); !isColor(reverse, want, 1) {
		t.Errorf("grayscale(1) brightness(2) = %v, want %v (grey THEN brighten)", reverse, want)
	}
	if forward == reverse {
		t.Errorf("both orders produced %v; the pair was chosen to make order observable", forward)
	}
}

// TestPaintFilterBlurSpreadsPastTheBorderBox: blur() moves pixels, so the filtered
// output must reach OUTSIDE the box's border box — CSS, unlike SVG, does not clip a
// filter to a region. A surface sized to the border box alone would cut the blur off
// dead at the edge, which reads as a half-blurred box rather than a missing margin.
func TestPaintFilterBlurSpreadsPastTheBorderBox(t *testing.T) {
	img := rasterFiltered([]filtereffects.Function{{Kind: filtereffects.FuncBlur, StdDeviation: 3}}, nil)
	// Just OUTSIDE the box (which spans x,y in [10,50)): a blur must tint it.
	if got := img.RGBAAt(7, 30); got.R == 0xff && got.G == 0xff && got.B == 0xff {
		t.Errorf("pixel (7,30) is still bare white; blur(3) must spread past the border box")
	}
	// The box's own centre stays close to the source colour (a 3px blur over a
	// 40px box barely touches the middle).
	if got := img.RGBAAt(30, 30); !isColor(got, filterProbe, 4) {
		t.Errorf("blurred centre = %v, want ≈%v", got, filterProbe)
	}
	// Far from the box, nothing is painted at all — the margin is bounded.
	if got := img.RGBAAt(90, 90); !isColor(got, color.RGBA{255, 255, 255, 255}, 0) {
		t.Errorf("pixel (90,90) = %v, want bare white; the blur margin must be bounded", got)
	}
}

// TestPaintFilterDropShadowUsesItsResolvedColor: drop-shadow(dx dy blur color)
// lowers to blur → offset → flood → composite("in") → merge, with the shadow
// BEHIND the source. A hard-edged shadow (stdDeviation 0) offset clear of the box
// lets the flood colour be read directly.
func TestPaintFilterDropShadowUsesItsResolvedColor(t *testing.T) {
	red := color.RGBA{0xff, 0, 0, 0xff}
	img := rasterFiltered(
		[]filtereffects.Function{{Kind: filtereffects.FuncDropShadow, Dx: 12, Dy: 12}},
		[]color.RGBA{red},
	)
	// (56,56) is inside the shadow (the box shifted by 12) but outside the box.
	if got := img.RGBAAt(56, 56); !isColor(got, red, 2) {
		t.Errorf("shadow pixel (56,56) = %v, want the resolved shadow colour %v", got, red)
	}
	// The source still paints ON TOP of its own shadow: feMerge's node order is
	// painting order, so the composite (shadow) is first and the source second.
	if got := img.RGBAAt(30, 30); !isColor(got, filterProbe, 2) {
		t.Errorf("source pixel (30,30) = %v, want the unshadowed source %v (shadow must be BEHIND)", got, filterProbe)
	}
}

// TestPaintFilterEmptyChainIsByteIdentical: a bracket carrying no functions must
// leave the page byte-identical to the same content with no bracket at all. This is
// the regression guard for every unfiltered document — `filter: none` and an
// unparseable value both reach the painter as no bracket, but a hand-built or
// future emission path could produce an empty one.
func TestPaintFilterEmptyChainIsByteIdentical(t *testing.T) {
	rule := layout.RuleItem{XPt: 10, YPt: 10, WPt: 40, HPt: 40, Color: filterProbe}
	plain := newRasterPage(100, 100, &layout.Page{WidthPt: 100, HeightPt: 100, Items: []layout.Item{
		{Kind: layout.BackgroundKind, Rule: rule},
	}})
	empty := newRasterPage(100, 100, &layout.Page{WidthPt: 100, HeightPt: 100, Items: []layout.Item{
		{Kind: layout.FilterPushKind, Filter: layout.FilterItem{XPt: 10, YPt: 10, WPt: 40, HPt: 40}},
		{Kind: layout.BackgroundKind, Rule: rule},
		{Kind: layout.FilterPopKind},
	}})
	if !bytes.Equal(plain.Pix, empty.Pix) {
		t.Error("an empty filter chain did not render byte-identically to no bracket at all")
	}
}

// TestPaintFilterDegenerateRegionDegrades: a filter whose region cannot produce a
// surface — a zero-area box, a box entirely off the device, or one so large it
// exceeds maxCSSFilterPixels — must still PAINT its content, unfiltered, rather
// than dropping it or panicking. A visible approximation beats a blank.
func TestPaintFilterDegenerateRegionDegrades(t *testing.T) {
	gray := []filtereffects.Function{{Kind: filtereffects.FuncGrayscale, Amount: 1}}
	for _, tc := range []struct {
		name       string
		x, y, w, h float64
	}{
		{name: "zero-area box", x: 10, y: 10, w: 0, h: 0},
		{name: "NaN box", x: math.NaN(), y: 0, w: 10, h: 10},
		{name: "infinite box", x: 0, y: 0, w: math.Inf(1), h: math.Inf(1)},
		{name: "negative-extent box", x: 10, y: 10, w: -5, h: -5},
	} {
		img := newRasterPage(100, 100, &layout.Page{WidthPt: 100, HeightPt: 100, Items: []layout.Item{
			{Kind: layout.FilterPushKind, Filter: layout.FilterItem{
				Funcs: gray, XPt: tc.x, YPt: tc.y, WPt: tc.w, HPt: tc.h,
			}},
			{Kind: layout.BackgroundKind, Rule: layout.RuleItem{XPt: 10, YPt: 10, WPt: 40, HPt: 40, Color: filterProbe}},
			{Kind: layout.FilterPopKind},
		}})
		if got := img.RGBAAt(30, 30); !isColor(got, filterProbe, 1) {
			t.Errorf("%s: centre = %v, want the UNFILTERED source %v (degrade, never drop)", tc.name, got, filterProbe)
		}
	}
}

// TestPaintFilterCoversContentOverflowingTheBox: CSS does not clip a filter's
// input to the element's box, so content overflowing the border box must still be
// filtered — not cropped away. A surface sized to the border box alone would drop
// the overflowing part entirely, which is content loss dressed up as a filter.
func TestPaintFilterCoversContentOverflowingTheBox(t *testing.T) {
	img := newRasterPage(100, 100, &layout.Page{WidthPt: 100, HeightPt: 100, Items: []layout.Item{
		{Kind: layout.FilterPushKind, Filter: layout.FilterItem{
			Funcs: []filtereffects.Function{{Kind: filtereffects.FuncGrayscale, Amount: 1}},
			// The declared border box is a 10x10 corner...
			XPt: 10, YPt: 10, WPt: 10, HPt: 10,
		}},
		// ...but the painted content is 40x40, overflowing it on both axes.
		{Kind: layout.BackgroundKind, Rule: layout.RuleItem{XPt: 10, YPt: 10, WPt: 40, HPt: 40, Color: filterProbe}},
		{Kind: layout.FilterPopKind},
	}})
	// A point well outside the border box but inside the content: present AND
	// filtered.
	if got, want := img.RGBAAt(40, 40), (color.RGBA{95, 95, 95, 255}); !isColor(got, want, 1) {
		t.Errorf("overflowing content at (40,40) = %v, want the filtered %v", got, want)
	}
}

// TestCSSFilterSurfaceIsBounded pins the two properties that make
// maxCSSFilterPixels actually bound the allocation rather than a nominal region:
// the surface is INTERSECTED with the device before it is measured, and its
// origin is SHIFTED to (0,0) so RenderOffscreen (which always allocates from the
// origin) allocates the surface's own area rather than up to its far corner.
//
// Without the intersect, a `width:1e9px` box would exceed the cap and degrade to
// unfiltered even though only a page-sized sliver is visible. Without the shift, a
// small box far from the origin would allocate its far corner's area while this
// check measured its own — silently, since the output would look correct.
func TestCSSFilterSurfaceIsBounded(t *testing.T) {
	gray := []filtereffects.Function{{Kind: filtereffects.FuncGrayscale, Amount: 1}}

	// A colossal box on a page-sized device clips to the device and still runs.
	fs, _, ok := cssFilterSurface(
		&layout.FilterItem{Funcs: gray, WPt: 1e9, HPt: 1e9}, nil, render.Identity, 800, 600)
	if !ok {
		t.Fatal("a 1e9-point box on an 800x600 device should clip to the device, not be rejected")
	}
	if fs.size != (image.Point{X: 800, Y: 600}) {
		t.Errorf("surface size = %v, want the device's own 800x600 (clipped)", fs.size)
	}

	// The same box on a device larger than the cap IS rejected, so no
	// multi-gigabyte buffer is ever allocated.
	if _, _, ok := cssFilterSurface(
		&layout.FilterItem{Funcs: gray, WPt: 1e9, HPt: 1e9}, nil, render.Identity, 4000, 4000); ok {
		t.Errorf("a 16M-pixel surface should exceed the %d-pixel cap", maxCSSFilterPixels)
	}

	// A coordinate beyond int range must be REJECTED, not converted. On amd64 a
	// float64 outside int64's range converts to the int64 MINIMUM regardless of
	// sign, so an off-to-the-right box would come back starting far to the LEFT
	// and intersect the device to a plausible-looking wrong rectangle.
	for _, fi := range []layout.FilterItem{
		{Funcs: gray, XPt: 1e300, YPt: 1e300, WPt: 10, HPt: 10},
		{Funcs: gray, XPt: -1e300, YPt: -1e300, WPt: 10, HPt: 10},
	} {
		if _, _, ok := cssFilterSurface(&fi, nil, render.Identity, 800, 600); ok {
			t.Errorf("a box at (%g,%g), entirely beyond int range and off the device, was accepted", fi.XPt, fi.YPt)
		}
	}
	// A colossal blur MARGIN is a different case and must NOT be rejected: the
	// margin is clamped to the device, and pkg/svg/filter caps the deviation
	// itself (MaxBlurStdDeviation), so the result is a full-device blur rather
	// than an unbounded allocation.
	if _, _, ok := cssFilterSurface(
		&layout.FilterItem{Funcs: []filtereffects.Function{{Kind: filtereffects.FuncBlur, StdDeviation: 1e300}},
			WPt: 10, HPt: 10}, nil, render.Identity, 800, 600); !ok {
		t.Error("a huge blur margin should clamp to the device, not be rejected")
	}

	// A small box FAR from the origin allocates only its own area, not its far
	// corner's: the shift is what makes the two agree.
	fs, _, ok = cssFilterSurface(
		&layout.FilterItem{Funcs: gray, XPt: 3900, YPt: 3900, WPt: 50, HPt: 50}, nil, render.Identity, 4000, 4000)
	if !ok {
		t.Fatal("a 50x50 box at (3900,3900) should be filterable")
	}
	if fs.size.X > 64 || fs.size.Y > 64 {
		t.Errorf("surface size = %v for a 50x50 box; the origin shift is not being applied", fs.size)
	}
	if fs.origin.X < 3800 || fs.origin.Y < 3800 {
		t.Errorf("surface origin = %v, want ≈(3900,3900) so the result is placed back correctly", fs.origin)
	}
}

// TestPaintFilterNestedChainsBothApply: a filtered box inside another filtered box
// runs BOTH chains, inner first. invert(1) inside brightness(0.5) gives
// (204,153,102) halved to (102,77,51).
func TestPaintFilterNestedChainsBothApply(t *testing.T) {
	img := newRasterPage(100, 100, &layout.Page{WidthPt: 100, HeightPt: 100, Items: []layout.Item{
		{Kind: layout.FilterPushKind, Filter: layout.FilterItem{
			Funcs: []filtereffects.Function{{Kind: filtereffects.FuncBrightness, Amount: 0.5}},
			XPt:   10, YPt: 10, WPt: 40, HPt: 40,
		}},
		{Kind: layout.FilterPushKind, Filter: layout.FilterItem{
			Funcs: []filtereffects.Function{{Kind: filtereffects.FuncInvert, Amount: 1}},
			XPt:   10, YPt: 10, WPt: 40, HPt: 40,
		}},
		{Kind: layout.BackgroundKind, Rule: layout.RuleItem{XPt: 10, YPt: 10, WPt: 40, HPt: 40, Color: filterProbe}},
		{Kind: layout.FilterPopKind},
		{Kind: layout.FilterPopKind},
	}})
	if got, want := img.RGBAAt(30, 30), (color.RGBA{102, 77, 51, 255}); !isColor(got, want, 2) {
		t.Errorf("nested invert(1) inside brightness(0.5) = %v, want %v", got, want)
	}
}

// TestPaintFilterNestingIsCapped: nesting past maxFilterNestingDepth degrades to
// painting the content unfiltered rather than allocating an unbounded stack of live
// offscreen surfaces. Each level holds an RGBA plus its chain's own float32 buffers
// alive at once, so the per-level pixel cap does not bound the product of N levels.
//
// The chain used is opacity(0.5): stacking N of them multiplies coverage, so the
// depth at which filtering stops is directly readable from the pixel — a cap that
// silently did nothing would keep halving.
func TestPaintFilterNestingIsCapped(t *testing.T) {
	build := func(n int) *layout.Page {
		var items []layout.Item
		for i := 0; i < n; i++ {
			items = append(items, layout.Item{Kind: layout.FilterPushKind, Filter: layout.FilterItem{
				Funcs: []filtereffects.Function{{Kind: filtereffects.FuncOpacity, Amount: 0.5}},
				XPt:   10, YPt: 10, WPt: 40, HPt: 40,
			}})
		}
		items = append(items, layout.Item{
			Kind: layout.BackgroundKind,
			Rule: layout.RuleItem{XPt: 10, YPt: 10, WPt: 40, HPt: 40, Color: color.RGBA{0, 0, 0, 0xff}},
		})
		for i := 0; i < n; i++ {
			items = append(items, layout.Item{Kind: layout.FilterPopKind})
		}
		return &layout.Page{WidthPt: 100, HeightPt: 100, Items: items}
	}
	// At the cap, every level still applies: black at coverage 0.5^4 over white.
	atCap := newRasterPage(100, 100, build(maxFilterNestingDepth)).RGBAAt(30, 30)
	if want := uint8(239); !isColor(atCap, color.RGBA{want, want, want, 255}, 2) {
		t.Errorf("%d nested opacity(0.5) = %v, want ≈%d on every channel (0.5^%d coverage)",
			maxFilterNestingDepth, atCap, want, maxFilterNestingDepth)
	}
	// One level PAST the cap, the innermost bracket degrades to a plain group, so
	// the result is LIGHTER than a further halving would give — and, critically,
	// the content is still there rather than dropped.
	past := newRasterPage(100, 100, build(maxFilterNestingDepth+1)).RGBAAt(30, 30)
	if past.R == 0xff && past.G == 0xff && past.B == 0xff {
		t.Error("nesting past the cap dropped the content; it must degrade to unfiltered, never to a blank")
	}
	if past != atCap {
		t.Errorf("past the cap = %v, want the same %v: the extra level must degrade to a pass-through group, "+
			"not apply another opacity", past, atCap)
	}
}

// TestPaintFilterUnmatchedPushStillPaints: an UNMATCHED FilterPushKind (which the
// emission side makes impossible, but a hand-built or corrupted stream could carry)
// must still paint the rest of the item list rather than swallowing it.
func TestPaintFilterUnmatchedPushStillPaints(t *testing.T) {
	img := newRasterPage(100, 100, &layout.Page{WidthPt: 100, HeightPt: 100, Items: []layout.Item{
		{Kind: layout.FilterPushKind, Filter: layout.FilterItem{
			Funcs: []filtereffects.Function{{Kind: filtereffects.FuncGrayscale, Amount: 1}},
			XPt:   10, YPt: 10, WPt: 40, HPt: 40,
		}},
		{Kind: layout.BackgroundKind, Rule: layout.RuleItem{XPt: 10, YPt: 10, WPt: 40, HPt: 40, Color: filterProbe}},
		// no matching pop
	}})
	if got, want := img.RGBAAt(30, 30), (color.RGBA{95, 95, 95, 255}); !isColor(got, want, 1) {
		t.Errorf("unmatched push: centre = %v, want the filtered %v (content must not be dropped)", got, want)
	}
}

// TestPaintUnbalancedFilterPopIsNoOp: a stray FilterPopKind (which the emission side
// makes impossible, but a hand-built or corrupted stream could carry) must not panic.
// render.Device documents EndGroup with no matching BeginGroup as a no-op.
func TestPaintUnbalancedFilterPopIsNoOp(t *testing.T) {
	page := &layout.Page{WidthPt: 100, HeightPt: 100, Items: []layout.Item{
		{Kind: layout.FilterPopKind},
		{Kind: layout.BackgroundKind, Rule: layout.RuleItem{XPt: 0, YPt: 0, WPt: 10, HPt: 10, Color: color.RGBA{A: 0xff}}},
	}}
	// Must not panic on either the recording device or the real rasterizer.
	PaintPage(&recordDevice{}, page, render.Identity)
	newRasterPage(100, 100, page)
}
