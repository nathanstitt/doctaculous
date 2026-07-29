package doctaculous

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/crop"
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

func TestEncodeImageCropsToExactSize(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 400, 300))
	var buf bytes.Buffer
	opts := ImageOptions{
		Format: FormatPNG,
		Crop:   &crop.Options{Strategy: crop.StrategyCenter, Width: 100, Height: 100},
	}
	if err := EncodeImage(&buf, src, opts); err != nil {
		t.Fatalf("EncodeImage: %v", err)
	}
	img, err := png.Decode(&buf)
	if err != nil {
		t.Fatalf("output is not a decodable PNG: %v", err)
	}
	if img.Bounds().Dx() != 100 || img.Bounds().Dy() != 100 {
		t.Errorf("bounds = %v, want 100x100", img.Bounds())
	}
}

func TestEncodeImageWithoutCropIsUnchanged(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 40, 30))
	var withNil, absent bytes.Buffer
	if err := EncodeImage(&withNil, src, ImageOptions{Format: FormatPNG, Crop: nil}); err != nil {
		t.Fatalf("EncodeImage(Crop:nil): %v", err)
	}
	if err := EncodeImage(&absent, src, ImageOptions{Format: FormatPNG}); err != nil {
		t.Fatalf("EncodeImage(no Crop): %v", err)
	}
	if !bytes.Equal(withNil.Bytes(), absent.Bytes()) {
		t.Error("an explicit nil Crop differs from an unset one")
	}
	img, err := png.Decode(&withNil)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if img.Bounds().Dx() != 40 || img.Bounds().Dy() != 30 {
		t.Errorf("bounds = %v, want the source 40x30", img.Bounds())
	}
}

func TestEncodeImageCropErrorPropagates(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 40, 30))
	var buf bytes.Buffer
	err := EncodeImage(&buf, src, ImageOptions{
		Format: FormatPNG,
		Crop:   &crop.Options{Strategy: crop.StrategyCenter, Width: 0, Height: 10},
	})
	if !errors.Is(err, crop.ErrInvalidSize) {
		t.Errorf("err = %v, want crop.ErrInvalidSize", err)
	}
}
