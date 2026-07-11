package doctaculous

import (
	"bytes"
	"context"
	"image/png"
	"testing"

	"github.com/nathanstitt/doctaculous/testdata/gen"
)

// TestPageSize covers the geometry primitive on both backends: PDF MediaBox
// (post-/Rotate) and reflow laid-out pages.
func TestPageSize(t *testing.T) {
	pdfDoc, err := OpenBytes(gen.TextPDF())
	if err != nil {
		t.Fatalf("open text pdf: %v", err)
	}
	if w, h, err := pdfDoc.PageSize(0); err != nil || w != 612 || h != 792 {
		t.Errorf("text pdf PageSize(0) = %g x %g (%v), want 612 x 792", w, h, err)
	}

	// A /Rotate 90 page reports its post-rotation (landscape) size — the
	// aspect the rasterized image actually has.
	rotDoc, err := OpenBytes(gen.RotatedPDF(90))
	if err != nil {
		t.Fatalf("open rotated pdf: %v", err)
	}
	if w, h, err := rotDoc.PageSize(0); err != nil || w != 792 || h != 612 {
		t.Errorf("rotated pdf PageSize(0) = %g x %g (%v), want 792 x 612", w, h, err)
	}

	htmlDoc, err := OpenHTMLBytes([]byte("<p>hi</p>"), WithPageSize(LetterWidthPt, LetterHeightPt))
	if err != nil {
		t.Fatalf("open html: %v", err)
	}
	if w, h, err := htmlDoc.PageSize(0); err != nil || w != LetterWidthPt || h != LetterHeightPt {
		t.Errorf("html PageSize(0) = %g x %g (%v), want %d x %d", w, h, err, LetterWidthPt, LetterHeightPt)
	}

	for _, bad := range []int{-1, pdfDoc.PageCount()} {
		if _, _, err := pdfDoc.PageSize(bad); err == nil {
			t.Errorf("PageSize(%d): want error, got nil", bad)
		}
		if _, _, err := htmlDoc.PageSize(bad); err == nil {
			t.Errorf("html PageSize(%d): want error, got nil", bad)
		}
	}
}

// TestFitWithin pins the fit-sizing table: exact expected pixel dimensions for
// the letter fixture (612x792pt), one-axis constraints, the upscale-fill
// default, the DPI ceiling, exact-fit float safety, and a rotated PDF page.
func TestFitWithin(t *testing.T) {
	letter := mustOpen(t, gen.TextPDF())
	rotated := mustOpen(t, gen.RotatedPDF(90))

	cases := []struct {
		name         string
		doc          *Document
		opts         RasterOptions
		wantW, wantH int
	}{
		// scale = min(480/612, 360/792) = 360/792; 612*(360/792) = 278.18 -> 279.
		{"letter into 480x360", letter, RasterOptions{MaxWidthPx: 480, MaxHeightPx: 360}, 279, 360},
		// One axis: width only, scale = 306/612 = 0.5 exactly.
		{"width-only 306", letter, RasterOptions{MaxWidthPx: 306}, 306, 396},
		{"height-only 396", letter, RasterOptions{MaxHeightPx: 396}, 306, 396},
		// Default fills the box: a page smaller than the box at its natural
		// size upscales (vector re-render, no quality cost).
		{"upscale fills", letter, RasterOptions{MaxWidthPx: 1224, MaxHeightPx: 1584}, 1224, 1584},
		// A positive DPI is a ceiling: 72 DPI = 1:1 pt:px, so no upscale.
		{"dpi ceiling stops upscale", letter, RasterOptions{MaxWidthPx: 1224, MaxHeightPx: 1584, DPI: 72}, 612, 792},
		// The ceiling only caps; it never grows a fit (300 DPI >> the fit).
		{"high ceiling irrelevant", letter, RasterOptions{MaxWidthPx: 480, MaxHeightPx: 360, DPI: 300}, 279, 360},
		// Exact fit: ceil(pt*scale) must land exactly on the box, never one past.
		{"exact fit", letter, RasterOptions{MaxWidthPx: 612, MaxHeightPx: 792}, 612, 792},
		// Rotated page (792x612 post-rotation): scale = min(480/792, 360/612)
		// = 360/612; 792*(360/612) = 465.88 -> 466.
		{"rotated into 480x360", rotated, RasterOptions{MaxWidthPx: 480, MaxHeightPx: 360}, 466, 360},
	}
	for _, c := range cases {
		c.opts.BundledFonts = true
		img, err := c.doc.RasterizePage(context.Background(), 0, c.opts)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		b := img.Bounds()
		if b.Dx() != c.wantW || b.Dy() != c.wantH {
			t.Errorf("%s: got %dx%d, want %dx%d", c.name, b.Dx(), b.Dy(), c.wantW, c.wantH)
		}
		if c.opts.MaxWidthPx > 0 && b.Dx() > c.opts.MaxWidthPx {
			t.Errorf("%s: width %d exceeds the box %d", c.name, b.Dx(), c.opts.MaxWidthPx)
		}
		if c.opts.MaxHeightPx > 0 && b.Dy() > c.opts.MaxHeightPx {
			t.Errorf("%s: height %d exceeds the box %d", c.name, b.Dy(), c.opts.MaxHeightPx)
		}
	}
}

// TestFitMatchesExplicitDPI pins that fit sizing is pure DPI resolution: the
// fit render is pixel-identical to rendering at the DPI fitRaster resolves —
// the painting path is untouched, only sizing changes.
func TestFitMatchesExplicitDPI(t *testing.T) {
	doc := mustOpen(t, gen.TextPDF())
	fitOpts := RasterOptions{MaxWidthPx: 480, MaxHeightPx: 360, BundledFonts: true}

	resolved, err := doc.fitRaster(0, fitOpts)
	if err != nil {
		t.Fatalf("fitRaster: %v", err)
	}
	if resolved.MaxWidthPx != 0 || resolved.MaxHeightPx != 0 {
		t.Fatalf("fitRaster left Max fields set: %+v", resolved)
	}

	fitImg, err := doc.RasterizePage(context.Background(), 0, fitOpts)
	if err != nil {
		t.Fatalf("fit render: %v", err)
	}
	dpiImg, err := doc.RasterizePage(context.Background(), 0, resolved)
	if err != nil {
		t.Fatalf("explicit-DPI render: %v", err)
	}
	var fitPNG, dpiPNG bytes.Buffer
	if err := png.Encode(&fitPNG, fitImg); err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(&dpiPNG, dpiImg); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(fitPNG.Bytes(), dpiPNG.Bytes()) {
		t.Error("fit render differs from the equivalent explicit-DPI render")
	}
}

// TestRasterizePagesFit verifies fit sizing resolves per page inside the
// worker fan-out.
func TestRasterizePagesFit(t *testing.T) {
	doc := mustOpen(t, gen.MultiPagePDF(3))
	results := doc.RasterizePages(context.Background(), doc.AllPages(),
		RasterOptions{MaxWidthPx: 200, MaxHeightPx: 200, BundledFonts: true})
	for _, r := range results {
		if r.Err != nil {
			t.Errorf("page %d: %v", r.Index, r.Err)
			continue
		}
		b := r.Image.Bounds()
		if b.Dx() > 200 || b.Dy() > 200 {
			t.Errorf("page %d: %dx%d exceeds the 200x200 box", r.Index, b.Dx(), b.Dy())
		}
	}
}

// TestWriteImageFit verifies the fit fields ride through ImageOptions.Raster.
func TestWriteImageFit(t *testing.T) {
	doc := mustOpen(t, gen.TextPDF())
	var buf bytes.Buffer
	err := doc.WriteImage(context.Background(), &buf, 0, ImageOptions{
		Raster: RasterOptions{MaxWidthPx: 100, MaxHeightPx: 100, BundledFonts: true},
	})
	if err != nil {
		t.Fatalf("WriteImage: %v", err)
	}
	img, err := png.Decode(&buf)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	b := img.Bounds()
	if b.Dx() > 100 || b.Dy() > 100 {
		t.Errorf("encoded image %dx%d exceeds the 100x100 box", b.Dx(), b.Dy())
	}
}

func mustOpen(t *testing.T, data []byte) *Document {
	t.Helper()
	doc, err := OpenBytes(data)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return doc
}
