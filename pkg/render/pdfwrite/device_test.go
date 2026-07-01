package pdfwrite

import (
	"bytes"
	"compress/zlib"
	"image/color"
	"io"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/font"
	"github.com/nathanstitt/doctaculous/pkg/render"
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

type stubFace struct{ o *render.Path }

func (s stubFace) Outline(uint16) *render.Path { return s.o }

func decompress(t *testing.T, data []byte) []byte {
	t.Helper()
	zr, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return data
	}
	out, _ := io.ReadAll(zr)
	return out
}
