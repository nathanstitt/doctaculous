package doctaculous

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

// tinyPNGBytes encodes a small solid-color PNG.
func tinyPNGBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: 0x33, G: 0x88, B: 0xCC, A: 0xFF})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestOpenImage pins the image-as-document contract: one page exactly the
// image's pixel size, format stamped from the encoding, rendered pixels the
// image's own.
func TestOpenImage(t *testing.T) {
	data := tinyPNGBytes(t, 40, 24)
	doc, err := OpenImageBytes(data)
	if err != nil {
		t.Fatalf("OpenImageBytes: %v", err)
	}
	if doc.Format() != FormatPNG {
		t.Errorf("Format() = %q, want png", doc.Format())
	}
	if doc.PageCount() != 1 {
		t.Errorf("PageCount = %d, want 1", doc.PageCount())
	}
	if w, h, err := doc.PageSize(0); err != nil || w != 40 || h != 24 {
		t.Errorf("PageSize = %g x %g (%v), want 40 x 24", w, h, err)
	}
	// At 72 DPI the page is pixel-for-pixel the image.
	img, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: 72, BundledFonts: true})
	if err != nil {
		t.Fatalf("RasterizePage: %v", err)
	}
	b := img.Bounds()
	if b.Dx() != 40 || b.Dy() != 24 {
		t.Fatalf("rendered %dx%d, want 40x24", b.Dx(), b.Dy())
	}
	if got := img.At(20, 12); !sameColor(got, color.RGBA{R: 0x33, G: 0x88, B: 0xCC, A: 0xFF}) {
		t.Errorf("center pixel = %v, want the image fill", got)
	}

	// Detection routes an image through the same path.
	det, err := OpenBytes(data)
	if err != nil || det.Format() != FormatPNG {
		t.Errorf("OpenBytes(png) = (%v, %v), want a FormatPNG document", det, err)
	}

	// Garbage is a decode error.
	if _, err := OpenImageBytes([]byte("not an image")); err == nil {
		t.Error("OpenImageBytes(garbage): want an error")
	}
}

// sameColor compares colors in 8-bit space with a small tolerance (JPEG or
// blend rounding).
func sameColor(a, b color.Color) bool {
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	diff := func(x, y uint32) uint32 {
		if x > y {
			return x - y
		}
		return y - x
	}
	const tol = 4 << 8
	return diff(ar, br) < tol && diff(ag, bg) < tol && diff(ab, bb) < tol
}

// TestImageConversions pins the any⇄any behavior images gained: image→PDF,
// image→JPEG transcode, and the same-format refusal.
func TestImageConversions(t *testing.T) {
	data := tinyPNGBytes(t, 30, 20)

	var pdf bytes.Buffer
	if err := Convert(context.Background(), bytes.NewReader(data), &pdf, ConvertOptions{To: FormatPDF}); err != nil {
		t.Fatalf("png→pdf: %v", err)
	}
	if !bytes.HasPrefix(pdf.Bytes(), []byte("%PDF-")) {
		t.Error("png→pdf output is not a PDF")
	}
	// The PDF page is exactly the image's size (30x20pt).
	pdfDoc, err := OpenBytes(pdf.Bytes())
	if err != nil {
		t.Fatalf("reopen pdf: %v", err)
	}
	if w, h, err := pdfDoc.PageSize(0); err != nil || w != 30 || h != 20 {
		t.Errorf("pdf page = %g x %g (%v), want 30 x 20", w, h, err)
	}

	var jpg bytes.Buffer
	if err := Convert(context.Background(), bytes.NewReader(data), &jpg, ConvertOptions{To: FormatJPEG}); err != nil {
		t.Fatalf("png→jpeg: %v", err)
	}
	if !bytes.HasPrefix(jpg.Bytes(), []byte("\xFF\xD8\xFF")) {
		t.Error("png→jpeg output is not a JPEG")
	}

	// Same-format stays a deliberate refusal on the generic path.
	var out bytes.Buffer
	if err := Convert(context.Background(), bytes.NewReader(data), &out, ConvertOptions{To: FormatPNG}); !errors.Is(err, ErrSameFormat) {
		t.Errorf("png→png: want ErrSameFormat, got %v", err)
	}

	// image→markdown degrades to an image reference (data URI), not an error.
	var md bytes.Buffer
	if err := Convert(context.Background(), bytes.NewReader(data), &md, ConvertOptions{To: FormatMarkdown}); err != nil {
		t.Fatalf("png→md: %v", err)
	}
	if !strings.Contains(md.String(), "![](data:image/png;base64,") {
		t.Errorf("png→md should carry the image:\n%s", md.String())
	}
}
