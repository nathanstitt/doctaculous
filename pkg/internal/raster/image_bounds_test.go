package raster

import (
	"strings"
	"testing"
)

// TestDecodeRawImageBounded covers image dimensions taken from a file's /Width
// and /Height, which reach decodeRawImage with only a > 0 check.
//
// The row arithmetic (w*nComps*bpc) and the short-data guard (rowBytes*h) are
// int multiplies that wrap. At h = 2^60 the product goes negative, the
// "short sample data" check passes, and image.NewRGBA is reached with an
// impossible height -- which PANICS rather than returning an error. It was
// caught only by the per-page recover, which cost the whole page.
//
// The bound is checked by division rather than multiplication so the check
// itself cannot overflow.
func TestDecodeRawImageBounded(t *testing.T) {
	data := make([]byte, 64)
	gray := imageCS{kind: csGray, nComps: 1}

	cases := []struct {
		name string
		w, h int
	}{
		{"height overflows the row product", 8, 1 << 60},
		{"both dimensions enormous", 1 << 40, 1 << 40},
		{"height alone enormous", 1, 1 << 62},
		{"width alone enormous", 1 << 62, 1},
		{"just past the pixel cap", maxPixels + 1, 1},
		{"product just past the cap", 1 << 14, 1 << 14},
		{"negative height", 8, -1},
		{"zero width", 0, 8},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// A panic here fails the test; before the bound, image.NewRGBA
			// panicked on exactly these inputs.
			img, err := decodeRawImage(data, c.w, c.h, 8, gray)
			if err == nil {
				t.Fatalf("%dx%d was accepted (allocated %v); it must be refused", c.w, c.h, img.Bounds())
			}
		})
	}
}

// TestDecodeRawImageOrdinarySizesStillWork is the guard against the bound being
// too tight: a normal image must still decode, and its pixels must be intact.
func TestDecodeRawImageOrdinarySizesStillWork(t *testing.T) {
	const w, h = 4, 3
	// 8-bit grayscale: one byte per pixel, rows already byte-aligned.
	data := make([]byte, w*h)
	for i := range data {
		data[i] = byte(i * 8)
	}
	img, err := decodeRawImage(data, w, h, 8, imageCS{kind: csGray, nComps: 1})
	if err != nil {
		t.Fatalf("a %dx%d image was refused: %v", w, h, err)
	}
	if b := img.Bounds(); b.Dx() != w || b.Dy() != h {
		t.Fatalf("bounds = %v, want %dx%d", b, w, h)
	}
	// Spot-check that the samples landed, not just that an image came back.
	if got := img.RGBAAt(0, 0).R; got != 0 {
		t.Errorf("pixel (0,0) R = %d, want 0", got)
	}
	if got := img.RGBAAt(1, 0).R; got != 8 {
		t.Errorf("pixel (1,0) R = %d, want 8", got)
	}
}

// TestDecodeRawImageAtTheCap checks the boundary is inclusive: an image of
// exactly maxPixels is legal, one pixel more is not. Only the dimension check is
// exercised (the data is deliberately short, so it fails the NEXT guard), which
// keeps the test from allocating half a gigabyte.
func TestDecodeRawImageAtTheCap(t *testing.T) {
	gray := imageCS{kind: csGray, nComps: 1}
	_, err := decodeRawImage(nil, maxPixels, 1, 8, gray)
	if err == nil {
		t.Fatal("expected the short-data error")
	}
	if strings.Contains(err.Error(), "pixel cap") {
		t.Errorf("an image of exactly maxPixels was rejected by the cap: %v", err)
	}
	_, err = decodeRawImage(nil, maxPixels+1, 1, 8, gray)
	if err == nil || !strings.Contains(err.Error(), "pixel cap") {
		t.Errorf("maxPixels+1 error = %v, want it to name the pixel cap", err)
	}
}
