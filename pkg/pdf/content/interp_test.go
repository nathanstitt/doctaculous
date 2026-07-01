package content

import (
	"image"
	"image/color"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/pdf"
	"github.com/nathanstitt/doctaculous/pkg/render"
)

// recDevice records draw calls for assertions.
type recDevice struct {
	fills          []render.FillPaint
	strokes        []render.StrokePaint
	glyphs         int
	images         int
	clips          int
	saves          int
	restores       int
	shadings       int // FillShading call count
	lastFillPath   *render.Path
	lastImageAlpha float64
}

func (d *recDevice) Size() (int, int) { return 612, 792 }
func (d *recDevice) Fill(p *render.Path, paint render.FillPaint) {
	d.fills = append(d.fills, paint)
	d.lastFillPath = p.Clone()
}
func (d *recDevice) Stroke(p *render.Path, paint render.StrokePaint) {
	d.strokes = append(d.strokes, paint)
}
func (d *recDevice) DrawImage(img image.Image, ctm render.Matrix, alpha float64, blendMode string) {
	d.images++
	d.lastImageAlpha = alpha
}
func (d *recDevice) FillGlyph(o *render.Path, c render.FillColor, blendMode string) { d.glyphs++ }
func (d *recDevice) DrawGlyph(render.GlyphRef)                                      { d.glyphs++ }
func (d *recDevice) FillShading(shader render.Shader, ctm render.Matrix, blendMode string) {
	d.shadings++
}
func (d *recDevice) PushClip(p *render.Path, r render.FillRule) { d.clips++ }
func (d *recDevice) Save()                                      { d.saves++ }
func (d *recDevice) Restore()                                   { d.restores++ }

func runContent(t *testing.T, src string, res Resources) *recDevice {
	t.Helper()
	dev := &recDevice{}
	it := New(nil, dev, res, render.Identity, Options{})
	if err := it.Run([]byte(src)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return dev
}

func TestFillRectangle(t *testing.T) {
	dev := runContent(t, "1 0 0 rg 100 100 200 150 re f", nil)
	if len(dev.fills) != 1 {
		t.Fatalf("got %d fills, want 1", len(dev.fills))
	}
	if dev.fills[0].Color != (color.RGBA{255, 0, 0, 255}) {
		t.Errorf("fill color = %v, want red", dev.fills[0].Color)
	}
	if dev.fills[0].Rule != render.NonZero {
		t.Errorf("fill rule = %v, want NonZero", dev.fills[0].Rule)
	}
	// The rectangle should have produced a closed 4-corner subpath.
	if dev.lastFillPath == nil || len(dev.lastFillPath.Segments) < 5 {
		t.Errorf("rectangle path = %+v", dev.lastFillPath)
	}
}

func TestStrokeLine(t *testing.T) {
	dev := runContent(t, "0 0 1 RG 5 w 10 10 m 100 100 l S", nil)
	if len(dev.strokes) != 1 {
		t.Fatalf("got %d strokes, want 1", len(dev.strokes))
	}
	if dev.strokes[0].Color != (color.RGBA{0, 0, 255, 255}) {
		t.Errorf("stroke color = %v, want blue", dev.strokes[0].Color)
	}
	if dev.strokes[0].Width != 5 { // identity CTM => width unchanged
		t.Errorf("stroke width = %v, want 5", dev.strokes[0].Width)
	}
}

func TestEvenOddFill(t *testing.T) {
	dev := runContent(t, "0 0 100 100 re f*", nil)
	if len(dev.fills) != 1 || dev.fills[0].Rule != render.EvenOdd {
		t.Fatalf("expected one even-odd fill, got %+v", dev.fills)
	}
}

func TestQQBalance(t *testing.T) {
	dev := runContent(t, "q q Q Q", nil)
	if dev.saves != 2 || dev.restores != 2 {
		t.Errorf("save/restore = %d/%d, want 2/2", dev.saves, dev.restores)
	}
}

func TestExtraQIgnored(t *testing.T) {
	// Unbalanced Q must not panic or underflow.
	dev := runContent(t, "Q Q q Q", nil)
	if dev.restores < 1 {
		t.Errorf("expected at least one restore, got %d", dev.restores)
	}
}

func TestClipAfterPaint(t *testing.T) {
	dev := runContent(t, "0 0 100 100 re W n", nil)
	if dev.clips != 1 {
		t.Errorf("clips = %d, want 1", dev.clips)
	}
}

func TestUnknownOperatorSkipped(t *testing.T) {
	// A made-up operator must be skipped without affecting the following fill.
	dev := runContent(t, "1 0 0 rg zz 0 0 50 50 re f", nil)
	if len(dev.fills) != 1 {
		t.Errorf("unknown op disrupted fill: got %d fills", len(dev.fills))
	}
}

func TestCMYKColor(t *testing.T) {
	dev := runContent(t, "0 1 1 0 k 0 0 10 10 re f", nil)
	// CMYK (0,1,1,0) = red.
	if got := dev.fills[0].Color; got != (color.RGBA{255, 0, 0, 255}) {
		t.Errorf("cmyk fill = %v, want red", got)
	}
}

// TestSeparationTintTransform pins J1: a Separation color set via scn must map its tint
// through the tint-transform /Function, not be mistaken for gray. Here the tint transform
// maps a 1-component tint t to CMYK (0,0,0,t) — so a full-ink tint of 1.0 is BLACK. Before
// the fix, scn 1 under csOther treated 1 component as gray 1.0 = WHITE.
func TestSeparationTintTransform(t *testing.T) {
	// tint t -> CMYK (0,0,0,t): 4 alternate components.
	tint := &TintTransform{
		Eval:           func(in []float64) []float64 { return []float64{0, 0, 0, in[0]} },
		AlternateComps: 4,
	}
	res := fakeRes{colorSpaces: map[string]*TintTransform{"Spot": tint}}
	// Select the Separation space by name, then set full ink (scn 1) and fill.
	dev := runContent(t, "/Spot cs 1 scn 0 0 10 10 re f", res)
	if len(dev.fills) != 1 {
		t.Fatalf("got %d fills, want 1", len(dev.fills))
	}
	if got := dev.fills[0].Color; got != (color.RGBA{0, 0, 0, 255}) {
		t.Errorf("Separation full-ink fill = %v, want black (CMYK k=1); the J1 bug gave white", got)
	}
	// And a zero tint (no ink) maps to CMYK (0,0,0,0) = white.
	dev2 := runContent(t, "/Spot cs 0 scn 0 0 10 10 re f", res)
	if got := dev2.fills[0].Color; got != (color.RGBA{255, 255, 255, 255}) {
		t.Errorf("Separation no-ink fill = %v, want white", got)
	}
}

// TestSeparationTintTransformStroke pins J1 for the stroke side (SCN + SC).
func TestSeparationTintTransformStroke(t *testing.T) {
	tint := &TintTransform{
		Eval:           func(in []float64) []float64 { return []float64{0, 0, 0, in[0]} },
		AlternateComps: 4,
	}
	res := fakeRes{colorSpaces: map[string]*TintTransform{"Spot": tint}}
	dev := runContent(t, "/Spot CS 1 SCN 0 0 m 10 10 l S", res)
	if len(dev.strokes) != 1 {
		t.Fatalf("got %d strokes, want 1", len(dev.strokes))
	}
	if got := dev.strokes[0].Color; got != (color.RGBA{0, 0, 0, 255}) {
		t.Errorf("Separation stroke = %v, want black", got)
	}
}

// --- text + font ---

type fakeFont struct{}

func (fakeFont) DecodeString(s []byte) []Glyph {
	glyphs := make([]Glyph, len(s))
	for i, b := range s {
		// Give each glyph a tiny square outline so FillGlyph is exercised.
		out := &render.Path{}
		out.MoveTo(0, 0)
		out.LineTo(0.5, 0)
		out.LineTo(0.5, 0.5)
		out.Close()
		glyphs[i] = Glyph{Code: int(b), Width: 0.5, Rune: rune(b), IsSpace: b == ' ', Outline: out}
	}
	return glyphs
}

// constShader is a trivial render.Shader that paints one solid color
// everywhere, used to assert that the sh/scn paths reach the device.
type constShader struct{ c color.RGBA }

func (s constShader) ColorAt(float64, float64) (color.RGBA, bool) { return s.c, true }

type fakeRes struct {
	font        GlyphSource
	extGS       map[string]ExtGStateParams
	shadings    map[string]render.Shader
	patterns    map[string]render.Shader
	colorSpaces map[string]*TintTransform
}

func (r fakeRes) Font(name string) GlyphSource { return r.font }
func (r fakeRes) Image(name string, fill render.FillColor) (image.Image, bool) {
	return image.NewRGBA(image.Rect(0, 0, 2, 2)), true
}
func (r fakeRes) InlineImage(dict pdf.Dict, data []byte, fill render.FillColor) (image.Image, bool) {
	return image.NewRGBA(image.Rect(0, 0, 2, 2)), true
}
func (r fakeRes) Form(name string) ([]byte, Resources, render.Matrix, *[4]float64, bool) {
	return nil, nil, render.Identity, nil, false
}
func (r fakeRes) Shading(name string) (render.Shader, bool) {
	s, ok := r.shadings[name]
	return s, ok
}
func (r fakeRes) Pattern(name string) (render.Shader, render.Matrix, bool) {
	s, ok := r.patterns[name]
	return s, render.Identity, ok
}
func (r fakeRes) ExtGState(name string) (ExtGStateParams, bool) {
	p, ok := r.extGS[name]
	return p, ok
}
func (r fakeRes) ColorSpace(name string) (*TintTransform, bool) {
	t, ok := r.colorSpaces[name]
	return t, ok
}

func TestShowText(t *testing.T) {
	res := fakeRes{font: fakeFont{}}
	dev := runContent(t, "BT /F1 12 Tf 72 700 Td (Hi) Tj ET", res)
	if dev.glyphs != 2 {
		t.Errorf("glyphs filled = %d, want 2", dev.glyphs)
	}
}

func TestExtGStateFillAlpha(t *testing.T) {
	res := fakeRes{extGS: map[string]ExtGStateParams{
		"GS0": {FillAlpha: 0.5, HasFillAlpha: true},
	}}
	// Opaque black fill (g 0 sets A=255), then gs sets /ca 0.5, then fill a rect.
	dev := runContent(t, "0 g /GS0 gs 0 0 100 100 re f", res)
	if len(dev.fills) != 1 {
		t.Fatalf("got %d fills, want 1", len(dev.fills))
	}
	if a := dev.fills[0].Color.A; a < 120 || a > 135 {
		t.Errorf("fill alpha = %d, want ~128 (0.5 × 255)", a)
	}
}

func TestExtGStateImageAlpha(t *testing.T) {
	res := fakeRes{extGS: map[string]ExtGStateParams{
		"GS0": {FillAlpha: 0.5, HasFillAlpha: true},
	}}
	dev := runContent(t, "/GS0 gs q 1 0 0 1 0 0 cm /Im0 Do Q", res)
	if dev.images != 1 {
		t.Fatalf("images drawn = %d, want 1", dev.images)
	}
	if dev.lastImageAlpha != 0.5 {
		t.Errorf("image alpha = %v, want 0.5", dev.lastImageAlpha)
	}
}

func TestShowTextInvisibleMode(t *testing.T) {
	res := fakeRes{font: fakeFont{}}
	dev := runContent(t, "BT /F1 12 Tf 3 Tr (Hi) Tj ET", res)
	if dev.glyphs != 0 {
		t.Errorf("invisible text drew %d glyphs, want 0", dev.glyphs)
	}
}

// TestShowTextStrokeMode pins J4: text render mode 1 STROKES each glyph (not fills).
// Two glyphs ⇒ two strokes, zero fills. The stroke uses the stroke color (set by RG).
// Mutation-verify: before the fix, mode 1 filled (glyphs==2, strokes==0).
func TestShowTextStrokeMode(t *testing.T) {
	res := fakeRes{font: fakeFont{}}
	dev := runContent(t, "BT 1 0 0 RG /F1 12 Tf 1 Tr (Hi) Tj ET", res)
	if dev.glyphs != 0 {
		t.Errorf("stroke-mode text filled %d glyphs, want 0", dev.glyphs)
	}
	if len(dev.strokes) != 2 {
		t.Fatalf("stroke-mode text produced %d strokes, want 2 (one per glyph)", len(dev.strokes))
	}
	if got := dev.strokes[0].Color; got != (color.RGBA{255, 0, 0, 255}) {
		t.Errorf("glyph stroke color = %v, want red (the stroke color)", got)
	}
}

// TestShowTextFillStrokeMode pins J4 mode 2: fill AND stroke each glyph.
func TestShowTextFillStrokeMode(t *testing.T) {
	res := fakeRes{font: fakeFont{}}
	dev := runContent(t, "BT /F1 12 Tf 2 Tr (Hi) Tj ET", res)
	if dev.glyphs != 2 {
		t.Errorf("fill+stroke text filled %d glyphs, want 2", dev.glyphs)
	}
	if len(dev.strokes) != 2 {
		t.Errorf("fill+stroke text produced %d strokes, want 2", len(dev.strokes))
	}
}

// TestShowTextClipOnlyMode pins J4 mode 7: clip-only paints nothing (no fill, no stroke).
func TestShowTextClipOnlyMode(t *testing.T) {
	res := fakeRes{font: fakeFont{}}
	dev := runContent(t, "BT /F1 12 Tf 7 Tr (Hi) Tj ET", res)
	if dev.glyphs != 0 || len(dev.strokes) != 0 {
		t.Errorf("clip-only text painted (glyphs=%d strokes=%d), want 0/0", dev.glyphs, len(dev.strokes))
	}
}

func TestDrawImageXObject(t *testing.T) {
	res := fakeRes{font: fakeFont{}}
	dev := runContent(t, "q 100 0 0 100 0 0 cm /Im0 Do Q", res)
	if dev.images != 1 {
		t.Errorf("images drawn = %d, want 1", dev.images)
	}
}

// TestMalformedOperandsNoPanic asserts that operators invoked with too few
// operands degrade gracefully (no panic), per the project's malformed-input rule.
func TestMalformedOperandsNoPanic(t *testing.T) {
	streams := []string{
		"1 2 c",       // c with 2 of 6 operands
		"c",           // c with none
		"\"",          // " with none
		"5 \"",        // " with one
		"v",           // v with none
		"y",           // y with none
		"re f",        // re with none, then fill
		"cm",          // cm with none
		"1 0 0 rg sc", // sc with no components after rg
		"BT Tj ET",    // Tj with no string
		"[ TJ",        // malformed TJ
	}
	for _, s := range streams {
		t.Run(s, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panicked on %q: %v", s, r)
				}
			}()
			runContent(t, s, fakeRes{font: fakeFont{}})
		})
	}
}

func TestShOperatorPaintsShading(t *testing.T) {
	res := fakeRes{shadings: map[string]render.Shader{
		"Sh1": constShader{c: color.RGBA{0, 0, 255, 255}},
	}}
	// Clip to a rect, then paint the shading. The device should see one
	// FillShading call.
	dev := runContent(t, "q 0 0 100 100 re W n /Sh1 sh Q", res)
	if dev.shadings != 1 {
		t.Fatalf("sh: FillShading called %d times, want 1", dev.shadings)
	}
}

func TestShOperatorMissingShadingSkips(t *testing.T) {
	// An unknown shading name must skip gracefully (no panic, no FillShading).
	dev := runContent(t, "/Nope sh", fakeRes{shadings: map[string]render.Shader{}})
	if dev.shadings != 0 {
		t.Fatalf("sh with missing shading: FillShading called %d times, want 0", dev.shadings)
	}
}

func TestShadingPatternFill(t *testing.T) {
	res := fakeRes{patterns: map[string]render.Shader{
		"P1": constShader{c: color.RGBA{0, 0, 255, 255}},
	}}
	// Select the Pattern color space, set the shading pattern, fill a rect. The
	// fill must paint via FillShading (clipped to the path), not a solid Fill.
	dev := runContent(t, "/Pattern cs /P1 scn 0 0 100 100 re f", res)
	if dev.shadings != 1 {
		t.Fatalf("pattern fill: FillShading called %d times, want 1", dev.shadings)
	}
	if len(dev.fills) != 0 {
		t.Fatalf("pattern fill: solid Fill called %d times, want 0", len(dev.fills))
	}
	if dev.clips != 1 {
		t.Fatalf("pattern fill: PushClip called %d times, want 1 (path clip)", dev.clips)
	}
}

func TestPatternFillClearedByDeviceColor(t *testing.T) {
	res := fakeRes{patterns: map[string]render.Shader{
		"P1": constShader{c: color.RGBA{0, 0, 255, 255}},
	}}
	// After a shading pattern, an rg device color must revert fills to solid.
	dev := runContent(t, "/Pattern cs /P1 scn 1 0 0 rg 0 0 100 100 re f", res)
	if dev.shadings != 0 {
		t.Fatalf("after rg: FillShading called %d times, want 0", dev.shadings)
	}
	if len(dev.fills) != 1 {
		t.Fatalf("after rg: solid Fill called %d times, want 1", len(dev.fills))
	}
}

func TestUnsupportedPatternFallsBack(t *testing.T) {
	// A pattern name that the backend cannot resolve (e.g. a tiling pattern) must
	// not paint a shading and must not panic.
	dev := runContent(t, "/Pattern cs /P1 scn 0 0 100 100 re f",
		fakeRes{patterns: map[string]render.Shader{}})
	if dev.shadings != 0 {
		t.Fatalf("unsupported pattern: FillShading called %d times, want 0", dev.shadings)
	}
}

func TestMaxOpsCap(t *testing.T) {
	dev := &recDevice{}
	it := New(nil, dev, nil, render.Identity, Options{MaxOps: 2})
	// Four fills, but the cap should stop after 2 operators.
	_ = it.Run([]byte("0 0 1 1 re f 0 0 1 1 re f 0 0 1 1 re f"))
	if len(dev.fills) > 2 {
		t.Errorf("MaxOps not enforced: %d fills", len(dev.fills))
	}
}
