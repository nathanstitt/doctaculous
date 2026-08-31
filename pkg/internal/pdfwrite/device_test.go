package pdfwrite

import (
	"bytes"
	"compress/zlib"
	"image/color"
	"io"
	"testing"

	"github.com/nathanstitt/omnidoc/pkg/internal/font"
	"github.com/nathanstitt/omnidoc/pkg/internal/raster"
	"github.com/nathanstitt/omnidoc/pkg/render"
)

// TestDeviceEmitsFillAndGlyphOps feeds a fill and a glyph, then asserts the content
// stream carries the expected operators and the glyph was recorded for embedding.
func TestDeviceEmitsFillAndGlyphOps(t *testing.T) {
	dev := newPageDevice(200, 200)

	p := &render.Path{}
	p.MoveTo(10, 10)
	p.LineTo(50, 10)
	p.LineTo(50, 40)
	p.LineTo(10, 40)
	p.Close()
	dev.Fill(p, render.FillPaint{Color: color.RGBA{R: 255, A: 255}})

	face, _ := font.LoadStandard("Helvetica", font.Style{})
	gid, _ := face.GID('A')
	dev.DrawGlyph(render.GlyphRef{
		Face: face, GID: gid, Runes: []rune{'A'},
		Transform: render.Scale(12, -12).Mul(render.Translate(20, 100)),
		Color:     render.FillColor{A: 255},
	})

	content := decompress(t, dev.contentStream())
	for _, want := range []string{"f\n", "BT", "Tj", "ET"} {
		if !bytes.Contains(content, []byte(want)) {
			t.Errorf("content stream missing %q\n%s", want, content)
		}
	}
	if len(dev.fonts().uses) == 0 {
		t.Error("glyph not recorded for embedding")
	}
}

// TestDeviceGlyphFallbackFillsOutline asserts a glyph with no embeddable face falls
// back to a fill (path ops), not a text op.
func TestDeviceGlyphFallbackFillsOutline(t *testing.T) {
	dev := newPageDevice(100, 100)
	// A GlyphRef whose Face is a bare render.GlyphFace (not *font.Face) exercises the
	// fallback: it cannot be embedded, so the device fills its outline.
	tri := &render.Path{}
	tri.MoveTo(0, 0)
	tri.LineTo(1, 0)
	tri.LineTo(0, 1)
	tri.Close()
	dev.DrawGlyph(render.GlyphRef{
		Face:      stubFace{tri},
		Transform: render.Scale(10, 10),
		Color:     render.FillColor{A: 255},
	})
	content := decompress(t, dev.contentStream())
	if bytes.Contains(content, []byte("Tj")) {
		t.Errorf("fallback glyph should not emit a text op:\n%s", content)
	}
	if !bytes.Contains(content, []byte("f\n")) {
		t.Errorf("fallback glyph should fill its outline:\n%s", content)
	}
}

// TestDeviceStrokeEmitsLineStyleOps proves Stroke emits PDF's line-style operators
// (J cap, j join, M miter, d dash) alongside color/width, not just "w"/"S": a stroke
// carrying a dash pattern used to render solid because these were never written.
func TestDeviceStrokeEmitsLineStyleOps(t *testing.T) {
	dev := newPageDevice(100, 100)
	p := &render.Path{}
	p.MoveTo(0, 0)
	p.LineTo(50, 0)
	dev.Stroke(p, render.StrokePaint{
		Color:      color.RGBA{B: 255, A: 255},
		Width:      4,
		Cap:        render.RoundCap,
		Join:       render.BevelJoin,
		MiterLimit: 6,
		DashArray:  []float64{8, 4},
		DashPhase:  2,
	})
	content := decompress(t, dev.contentStream())
	for _, want := range []string{"4 w\n", "1 J\n", "2 j\n", "6 M\n", "[8 4] 2 d\n", "S\n"} {
		if !bytes.Contains(content, []byte(want)) {
			t.Errorf("content stream missing %q\n%s", want, content)
		}
	}
}

// TestDeviceStrokeSolidDashNormalizes proves a nil, empty, or all-non-positive dash
// array all normalize to PDF's "solid line" encoding ("[] 0 d"), never an array of
// zeros (undefined per the PDF spec).
func TestDeviceStrokeSolidDashNormalizes(t *testing.T) {
	for name, dashes := range map[string][]float64{
		"nil":      nil,
		"empty":    {},
		"all-zero": {0, 0},
		"negative": {-1, -2},
	} {
		t.Run(name, func(t *testing.T) {
			dev := newPageDevice(100, 100)
			p := &render.Path{}
			p.MoveTo(0, 0)
			p.LineTo(10, 0)
			dev.Stroke(p, render.StrokePaint{Color: color.RGBA{A: 255}, Width: 1, DashArray: dashes})
			content := decompress(t, dev.contentStream())
			if !bytes.Contains(content, []byte("[] 0 d\n")) {
				t.Errorf("%s: want solid dash encoding, got:\n%s", name, content)
			}
		})
	}
}

// TestDeviceStrokeDefaultMiterLimit proves a MiterLimit under 1 (including the Go
// zero value) falls back to PDF's own default of 10, matching the raster backend's
// convention in pkg/render/raster/stroke.go rather than emitting an invalid "0 M".
func TestDeviceStrokeDefaultMiterLimit(t *testing.T) {
	dev := newPageDevice(100, 100)
	p := &render.Path{}
	p.MoveTo(0, 0)
	p.LineTo(10, 0)
	dev.Stroke(p, render.StrokePaint{Color: color.RGBA{A: 255}, Width: 1})
	content := decompress(t, dev.contentStream())
	if !bytes.Contains(content, []byte("10 M\n")) {
		t.Errorf("want default miter limit 10, got:\n%s", content)
	}
}

type stubFace struct{ o *render.Path }

func (s stubFace) Outline(uint16) *render.Path { return s.o }

// rampFunc is a minimal function.Func implementing a straight-line red (t=0) to
// blue (t=1) ramp, fully opaque, so tests can build a render.Shader without
// depending on PDF function-dictionary parsing.
type rampFunc struct{}

func (rampFunc) NumOutputs() int { return 4 }

func (rampFunc) Eval(in []float64) []float64 {
	t := 0.0
	if len(in) > 0 {
		t = in[0]
	}
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	return []float64{1 - t, 0, t, 1} // R,G,B,A straight alpha
}

// TestDeviceFillShadingRasterizesIntoImageXObject proves FillShading (once
// implemented) samples the shader into an RGBA image and draws it via
// DrawImage, so the page records an image XObject where today it records none.
func TestDeviceFillShadingRasterizesIntoImageXObject(t *testing.T) {
	dev := newPageDevice(100, 50)

	clip := &render.Path{}
	clip.MoveTo(0, 0)
	clip.LineTo(100, 0)
	clip.LineTo(100, 50)
	clip.LineTo(0, 50)
	clip.Close()
	dev.Save()
	dev.PushClip(clip, render.NonZero)

	shader := raster.NewAxialShader(0, 0, 100, 0, rampFunc{}, nil, raster.SpreadPad)
	dev.FillShading(shader, render.Matrix{A: 1, D: 1}, "")
	dev.Restore()

	if len(dev.images) == 0 {
		t.Fatal("FillShading did not record an image XObject")
	}
	content := decompress(t, dev.contentStream())
	if !bytes.Contains(content, []byte(" Do\n")) {
		t.Errorf("content stream missing image Do operator:\n%s", content)
	}
}

// TestDeviceFillShadingPlacesImageAtClipOrigin proves the rasterized shading
// image is placed at the clip's own (minX,minY) origin, not warped by
// composing the placement Translate and Scale in the wrong order. Composing
// Translate(minX,minY).Mul(Scale(w,h)) instead of
// Scale(w,h).Mul(Translate(minX,minY)) scales the translation by (w,h) too
// (Matrix.Mul applies its LEFT operand first), which is invisible whenever
// the clip happens to start at the page origin (Translate(0,0) is inert
// either way) — exactly like TestDeviceFillShadingRasterizesIntoImageXObject's
// clip does. This test pins a clip away from the origin so that regression
// fails loudly instead of only manifesting on off-origin shapes.
func TestDeviceFillShadingPlacesImageAtClipOrigin(t *testing.T) {
	dev := newPageDevice(100, 100)

	clip := &render.Path{}
	clip.MoveTo(20, 30)
	clip.LineTo(70, 30)
	clip.LineTo(70, 80)
	clip.LineTo(20, 80)
	clip.Close()
	dev.Save()
	dev.PushClip(clip, render.NonZero)

	shader := raster.NewAxialShader(20, 30, 70, 30, rampFunc{}, nil, raster.SpreadPad)
	dev.FillShading(shader, render.Matrix{A: 1, D: 1}, "")
	dev.Restore()

	if len(dev.images) == 0 {
		t.Fatal("FillShading did not record an image XObject")
	}
	ctm := dev.images[len(dev.images)-1].ctm
	// The image's unit square [0,1]x[0,1] must map to exactly the clip
	// bounds [20,70]x[30,80]: apply(0,0) -> (20,30), apply(1,1) -> (70,80).
	x0, y0 := ctm.A*0+ctm.C*0+ctm.E, ctm.B*0+ctm.D*0+ctm.F
	x1, y1 := ctm.A*1+ctm.C*1+ctm.E, ctm.B*1+ctm.D*1+ctm.F
	const tol = 1e-6
	if abs(x0-20) > tol || abs(y0-30) > tol {
		t.Errorf("image origin = (%v,%v), want (20,30)", x0, y0)
	}
	if abs(x1-70) > tol || abs(y1-80) > tol {
		t.Errorf("image far corner = (%v,%v), want (70,80)", x1, y1)
	}
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// TestDeviceSolidFillHasNoImageXObject proves a solid fill (no shading involved)
// never records an image, so documents that don't use gradients stay
// byte-identical to before this feature existed.
func TestDeviceSolidFillHasNoImageXObject(t *testing.T) {
	dev := newPageDevice(100, 50)
	p := &render.Path{}
	p.MoveTo(0, 0)
	p.LineTo(100, 0)
	p.LineTo(100, 50)
	p.LineTo(0, 50)
	p.Close()
	dev.Fill(p, render.FillPaint{Color: color.RGBA{R: 255, A: 255}})

	if len(dev.images) != 0 {
		t.Fatalf("solid fill recorded %d image(s), want 0", len(dev.images))
	}
	content := decompress(t, dev.contentStream())
	if bytes.Contains(content, []byte(" Do\n")) {
		t.Errorf("solid fill content stream should not contain an image Do operator:\n%s", content)
	}
}

func decompress(t *testing.T, data []byte) []byte {
	t.Helper()
	zr, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return data
	}
	out, _ := io.ReadAll(zr)
	return out
}
