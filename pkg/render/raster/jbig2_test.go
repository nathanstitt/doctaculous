package raster

import (
	"context"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/pdf"
	"github.com/nathanstitt/doctaculous/testdata/gen"
)

func countInkedJBIG2(t *testing.T, data []byte) int {
	t.Helper()
	doc, err := pdf.Parse(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pg, err := doc.Page(0)
	if err != nil {
		t.Fatalf("page: %v", err)
	}
	img, err := RenderPage(context.Background(), pg, Options{DPI: 36})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	n := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := img.RGBAAt(x, y).RGBA()
			if a > 0 && (r < 0xf000 || g < 0xf000 || bl < 0xf000) {
				n++
			}
		}
	}
	return n
}

// TestJBIG2ImageMaskRenders: a JBIG2-compressed /ImageMask stencil paints in the fill
// color — proving the decode-before-mask ordering (undecoded bytes would draw noise/blank).
func TestJBIG2ImageMaskRenders(t *testing.T) {
	if n := countInkedJBIG2(t, gen.JBIG2ImageMaskPDF()); n == 0 {
		t.Fatal("JBIG2 ImageMask rendered blank; expected the stencil to paint")
	}
}

// TestJBIG2GarbageSkipsGracefully: a page whose JBIG2 image is corrupt must still render
// (the image is skipped, no error/panic escapes to the page).
func TestJBIG2GarbageSkipsGracefully(t *testing.T) {
	doc, err := pdf.Parse(gen.JBIG2GarbagePDF())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pg, err := doc.Page(0)
	if err != nil {
		t.Fatalf("page: %v", err)
	}
	if _, err := RenderPage(context.Background(), pg, Options{DPI: 36}); err != nil {
		t.Fatalf("render should not fail on a skippable bad JBIG2 image: %v", err)
	}
}
