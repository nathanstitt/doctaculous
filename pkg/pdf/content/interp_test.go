package content

import (
	"image"
	"image/color"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/render"
)

// recDevice records draw calls for assertions.
type recDevice struct {
	fills        []render.FillPaint
	strokes      []render.StrokePaint
	glyphs       int
	images       int
	clips        int
	saves        int
	restores     int
	lastFillPath *render.Path
}

func (d *recDevice) Size() (int, int) { return 612, 792 }
func (d *recDevice) Fill(p *render.Path, paint render.FillPaint) {
	d.fills = append(d.fills, paint)
	d.lastFillPath = p.Clone()
}
func (d *recDevice) Stroke(p *render.Path, paint render.StrokePaint) {
	d.strokes = append(d.strokes, paint)
}
func (d *recDevice) DrawImage(img image.Image, ctm render.Matrix) { d.images++ }
func (d *recDevice) FillGlyph(o *render.Path, c render.FillColor) { d.glyphs++ }
func (d *recDevice) PushClip(p *render.Path, r render.FillRule)   { d.clips++ }
func (d *recDevice) Save()                                        { d.saves++ }
func (d *recDevice) Restore()                                     { d.restores++ }

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

type fakeRes struct{ font GlyphSource }

func (r fakeRes) Font(name string) GlyphSource { return r.font }
func (r fakeRes) Image(name string) (image.Image, bool) {
	return image.NewRGBA(image.Rect(0, 0, 2, 2)), true
}
func (r fakeRes) Form(name string) ([]byte, Resources, render.Matrix, bool) {
	return nil, nil, render.Identity, false
}

func TestShowText(t *testing.T) {
	res := fakeRes{font: fakeFont{}}
	dev := runContent(t, "BT /F1 12 Tf 72 700 Td (Hi) Tj ET", res)
	if dev.glyphs != 2 {
		t.Errorf("glyphs filled = %d, want 2", dev.glyphs)
	}
}

func TestShowTextInvisibleMode(t *testing.T) {
	res := fakeRes{font: fakeFont{}}
	dev := runContent(t, "BT /F1 12 Tf 3 Tr (Hi) Tj ET", res)
	if dev.glyphs != 0 {
		t.Errorf("invisible text drew %d glyphs, want 0", dev.glyphs)
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

func TestMaxOpsCap(t *testing.T) {
	dev := &recDevice{}
	it := New(nil, dev, nil, render.Identity, Options{MaxOps: 2})
	// Four fills, but the cap should stop after 2 operators.
	_ = it.Run([]byte("0 0 1 1 re f 0 0 1 1 re f 0 0 1 1 re f"))
	if len(dev.fills) > 2 {
		t.Errorf("MaxOps not enforced: %d fills", len(dev.fills))
	}
}
