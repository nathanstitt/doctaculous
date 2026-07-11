package doctaculous

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/nathanstitt/doctaculous/testdata/gen"
)

func TestWriteImagePNGDefault(t *testing.T) {
	doc, err := OpenBytes(gen.TextPDF())
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	var buf bytes.Buffer
	opts := ImageOptions{Raster: RasterOptions{DPI: 72, BundledFonts: true}}
	if err := doc.WriteImage(context.Background(), &buf, 0, opts); err != nil {
		t.Fatalf("WriteImage: %v", err)
	}
	img, err := png.Decode(&buf)
	if err != nil {
		t.Fatalf("output is not a decodable PNG: %v", err)
	}
	if img.Bounds().Dx() < 1 || img.Bounds().Dy() < 1 {
		t.Errorf("decoded image is empty: %v", img.Bounds())
	}
}

func TestWriteImageJPEGQuality(t *testing.T) {
	doc, err := OpenBytes(gen.VectorPDF())
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	encode := func(quality int) int {
		t.Helper()
		var buf bytes.Buffer
		opts := ImageOptions{
			Format:  FormatJPEG,
			Quality: quality,
			Raster:  RasterOptions{DPI: 72, BundledFonts: true},
		}
		if err := doc.WriteImage(context.Background(), &buf, 0, opts); err != nil {
			t.Fatalf("WriteImage(q=%d): %v", quality, err)
		}
		if _, err := jpeg.Decode(bytes.NewReader(buf.Bytes())); err != nil {
			t.Fatalf("output (q=%d) is not a decodable JPEG: %v", quality, err)
		}
		return buf.Len()
	}
	low, high := encode(10), encode(95)
	if low >= high {
		t.Errorf("JPEG quality ignored: q10 output (%d bytes) not smaller than q95 (%d bytes)", low, high)
	}
}

func TestWriteImageBadPage(t *testing.T) {
	doc, err := OpenBytes(gen.TextPDF())
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	var buf bytes.Buffer
	if err := doc.WriteImage(context.Background(), &buf, 99, ImageOptions{}); err == nil {
		t.Errorf("WriteImage(page 99): want error, got nil")
	}
}

func TestEncodeImageRejectsNonImageFormat(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	var buf bytes.Buffer
	err := EncodeImage(&buf, img, ImageOptions{Format: FormatPDF})
	if !errors.Is(err, ErrUnsupportedFormat) {
		t.Errorf("EncodeImage(pdf): want ErrUnsupportedFormat, got %v", err)
	}
}
