package raster

import (
	"context"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/pdf"
	"github.com/nathanstitt/doctaculous/testdata/gen"
)

// TestInlineImageRenders renders a page whose only content is a BI...ID...EI
// inline image and asserts the image actually painted: the rasterized page must
// contain saturated red, green, and blue pixels (the inline image's colors), not
// just the white background. This proves the full inline path — scanner blob
// capture, abbreviated-key normalization, decode, and DrawImage — works end to
// end, where previously inline images were skipped.
func TestInlineImageRenders(t *testing.T) {
	doc, err := pdf.Parse(gen.InlineImagePDF())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pg, err := doc.Page(0)
	if err != nil {
		t.Fatalf("page: %v", err)
	}
	img, err := RenderPage(context.Background(), pg, Options{DPI: 72})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	var sawRed, sawGreen, sawBlue bool
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			r8, g8, b8 := r>>8, g>>8, bl>>8
			switch {
			case r8 > 0xC0 && g8 < 0x40 && b8 < 0x40:
				sawRed = true
			case g8 > 0xC0 && r8 < 0x40 && b8 < 0x40:
				sawGreen = true
			case b8 > 0xC0 && r8 < 0x40 && g8 < 0x40:
				sawBlue = true
			}
		}
	}
	if !sawRed || !sawGreen || !sawBlue {
		t.Errorf("inline image not rendered: red=%v green=%v blue=%v", sawRed, sawGreen, sawBlue)
	}
}
