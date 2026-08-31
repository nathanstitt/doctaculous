package omnidoc

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/nathanstitt/omnidoc/pkg/crop"
	"github.com/nathanstitt/omnidoc/pkg/internal/webp"
	"github.com/nathanstitt/omnidoc/testdata/gen"
)

func TestWriteImagePNGDefault(t *testing.T) {
	t.Parallel()
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

// TestWriteImageWebP renders a real document page to WebP and verifies the
// bytes are a decodable WebP of the rasterized size — the full backend path,
// not just the encoder in isolation.
func TestWriteImageWebP(t *testing.T) {
	t.Parallel()
	doc, err := OpenBytes(gen.TextPDF())
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	var buf bytes.Buffer
	opts := ImageOptions{Format: FormatWebP, Raster: RasterOptions{DPI: 72, BundledFonts: true}}
	if err := doc.WriteImage(context.Background(), &buf, 0, opts); err != nil {
		t.Fatalf("WriteImage(webp): %v", err)
	}
	img, err := webp.Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("output is not a decodable WebP: %v", err)
	}
	if img.Bounds().Dx() < 1 || img.Bounds().Dy() < 1 {
		t.Errorf("decoded image is empty: %v", img.Bounds())
	}
}

// TestWriteImageWebPIgnoresQuality pins the documented contract that Quality
// is a no-op for WebP: the encoding is lossless, so two different quality
// values must produce byte-identical output rather than silently pretending
// the knob did something.
func TestWriteImageWebPIgnoresQuality(t *testing.T) {
	t.Parallel()
	doc, err := OpenBytes(gen.VectorPDF())
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	encode := func(q int) []byte {
		var buf bytes.Buffer
		opts := ImageOptions{
			Format:  FormatWebP,
			Quality: q,
			Raster:  RasterOptions{DPI: 72, BundledFonts: true},
		}
		if err := doc.WriteImage(context.Background(), &buf, 0, opts); err != nil {
			t.Fatalf("WriteImage(webp, quality=%d): %v", q, err)
		}
		return buf.Bytes()
	}
	if lo, hi := encode(1), encode(100); !bytes.Equal(lo, hi) {
		t.Errorf("quality changed WebP output (%d vs %d bytes); it is documented as ignored",
			len(lo), len(hi))
	}
}

func TestWriteImageJPEGQuality(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	var buf bytes.Buffer
	err := EncodeImage(&buf, img, ImageOptions{Format: FormatPDF})
	if !errors.Is(err, ErrUnsupportedFormat) {
		t.Errorf("EncodeImage(pdf): want ErrUnsupportedFormat, got %v", err)
	}
}

func TestEncodeImageCropsToExactSize(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

func TestEncodeImageRectReportsSaliencyCrop(t *testing.T) {
	t.Parallel()
	// The caller needs the coordinates the saliency scorer chose — to store
	// them, show them, or re-apply the same crop later with StrategyRect.
	src := image.NewRGBA(image.Rect(0, 0, 400, 200))
	for y := 0; y < 200; y++ {
		for x := 0; x < 400; x++ {
			src.Set(x, y, color.RGBA{128, 128, 128, 255})
		}
	}
	// Detail in the left third, so saliency should not pick the centre.
	for y := 10; y < 190; y++ {
		for x := 10; x < 190; x++ {
			c := color.RGBA{0, 0, 0, 255}
			if (x/2+y/2)%2 == 0 {
				c = color.RGBA{255, 255, 255, 255}
			}
			src.Set(x, y, c)
		}
	}

	var buf bytes.Buffer
	got, err := EncodeImageRect(&buf, src, ImageOptions{
		Format: FormatPNG,
		Crop:   &crop.Options{Strategy: crop.StrategySaliency, Width: 100, Height: 100},
	})
	if err != nil {
		t.Fatalf("EncodeImageRect: %v", err)
	}
	if !got.In(src.Bounds()) {
		t.Errorf("reported rect %v escapes source bounds %v", got, src.Bounds())
	}
	if got.Dx() != 200 || got.Dy() != 200 {
		t.Errorf("rect = %v (%dx%d), want the 200x200 window a 1:1 target takes from 400x200",
			got, got.Dx(), got.Dy())
	}
	if mid := (got.Min.X + got.Max.X) / 2; mid > 150 {
		t.Errorf("rect centre x = %d, want <=150 (over the detailed left third); rect=%v", mid, got)
	}

	// The reported rect must be exactly what StrategyRect reproduces, so a
	// caller can persist it and re-apply the identical crop later.
	var replay bytes.Buffer
	replayed, err := EncodeImageRect(&replay, src, ImageOptions{
		Format: FormatPNG,
		Crop:   &crop.Options{Strategy: crop.StrategyRect, Rect: got, Width: 100, Height: 100},
	})
	if err != nil {
		t.Fatalf("EncodeImageRect(replay): %v", err)
	}
	if replayed != got {
		t.Errorf("replayed rect = %v, want %v", replayed, got)
	}
	if !bytes.Equal(replay.Bytes(), buf.Bytes()) {
		t.Error("replaying the reported rect produced different bytes")
	}
}

func TestEncodeImageRectWithoutCropReportsFullBounds(t *testing.T) {
	t.Parallel()
	src := image.NewRGBA(image.Rect(20, 10, 60, 40))
	var buf bytes.Buffer
	got, err := EncodeImageRect(&buf, src, ImageOptions{Format: FormatPNG})
	if err != nil {
		t.Fatalf("EncodeImageRect: %v", err)
	}
	if got != src.Bounds() {
		t.Errorf("rect = %v, want the full source bounds %v", got, src.Bounds())
	}
}

func TestEncodeImageRectMatchesEncodeImageBytes(t *testing.T) {
	t.Parallel()
	// EncodeImage must stay a thin wrapper: same bytes, both paths.
	src := image.NewRGBA(image.Rect(0, 0, 80, 60))
	opts := ImageOptions{
		Format: FormatPNG,
		Crop:   &crop.Options{Strategy: crop.StrategyCenter, Width: 40, Height: 40},
	}
	var viaRect, viaPlain bytes.Buffer
	if _, err := EncodeImageRect(&viaRect, src, opts); err != nil {
		t.Fatalf("EncodeImageRect: %v", err)
	}
	if err := EncodeImage(&viaPlain, src, opts); err != nil {
		t.Fatalf("EncodeImage: %v", err)
	}
	if !bytes.Equal(viaRect.Bytes(), viaPlain.Bytes()) {
		t.Error("EncodeImage and EncodeImageRect produced different bytes")
	}
}
