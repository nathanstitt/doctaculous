package doctaculous

import (
	"context"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"
)

// sampleHTML is a minimal document with visible text used by several tests.
const sampleHTML = `<html><body><p style="color:black">Hello world</p></body></html>`

// countNonBackground reports how many pixels of img differ from the given
// background color. It is the proof that the end-to-end pipeline painted real
// content (text glyphs) onto the page rather than leaving it blank.
func countNonBackground(img *image.RGBA, bg color.RGBA) int {
	b := img.Bounds()
	n := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if img.RGBAAt(x, y) != bg {
				n++
			}
		}
	}
	return n
}

// countColor reports how many pixels of img exactly equal c.
func countColor(img *image.RGBA, c color.RGBA) int {
	b := img.Bounds()
	n := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if img.RGBAAt(x, y) == c {
				n++
			}
		}
	}
	return n
}

// TestOpenHTMLBytesSmoke drives the whole HTML pipeline from bytes to a rasterized
// image: parse → box generation → layout → paint. It asserts the single-tall-page
// model (one page) and a non-empty image.
func TestOpenHTMLBytesSmoke(t *testing.T) {
	doc, err := OpenHTMLBytes([]byte(sampleHTML))
	if err != nil {
		t.Fatalf("OpenHTMLBytes: %v", err)
	}
	if doc == nil {
		t.Fatal("OpenHTMLBytes returned nil Document")
	}
	if doc.PageCount() != 1 {
		t.Fatalf("PageCount = %d, want 1", doc.PageCount())
	}
	img, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: 96})
	if err != nil {
		t.Fatalf("RasterizePage: %v", err)
	}
	if img == nil {
		t.Fatal("RasterizePage returned nil image")
	}
	if b := img.Bounds(); b.Dx() <= 0 || b.Dy() <= 0 {
		t.Fatalf("image bounds = %v, want positive width and height", b)
	}
}

// TestOpenHTMLBytesPaintsContent proves the pipeline actually rasterizes glyphs:
// the default background is white, so any non-white pixel means text was drawn.
func TestOpenHTMLBytesPaintsContent(t *testing.T) {
	doc, err := OpenHTMLBytes([]byte(sampleHTML), WithViewportWidth(400))
	if err != nil {
		t.Fatalf("OpenHTMLBytes: %v", err)
	}
	img, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: 96})
	if err != nil {
		t.Fatalf("RasterizePage: %v", err)
	}
	rgba, ok := img.(*image.RGBA)
	if !ok {
		t.Fatalf("rasterized image is %T, want *image.RGBA", img)
	}
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	if n := countNonBackground(rgba, white); n == 0 {
		t.Fatalf("no non-background pixels: the pipeline painted nothing (want glyphs drawn)")
	}
}

// TestOpenHTMLBytesViewportWidth checks that WithViewportWidth changes the laid-out
// page width: a wider viewport yields a wider image for the same content and DPI.
// The assertion is on width ordering (robust) rather than exact pixel counts.
func TestOpenHTMLBytesViewportWidth(t *testing.T) {
	const dpi = 96
	narrow, err := OpenHTMLBytes([]byte(sampleHTML), WithViewportWidth(400))
	if err != nil {
		t.Fatalf("OpenHTMLBytes(400): %v", err)
	}
	wide, err := OpenHTMLBytes([]byte(sampleHTML), WithViewportWidth(800))
	if err != nil {
		t.Fatalf("OpenHTMLBytes(800): %v", err)
	}
	nImg, err := narrow.RasterizePage(context.Background(), 0, RasterOptions{DPI: dpi})
	if err != nil {
		t.Fatalf("RasterizePage(narrow): %v", err)
	}
	wImg, err := wide.RasterizePage(context.Background(), 0, RasterOptions{DPI: dpi})
	if err != nil {
		t.Fatalf("RasterizePage(wide): %v", err)
	}
	nw, ww := nImg.Bounds().Dx(), wImg.Bounds().Dx()
	if ww <= nw {
		t.Fatalf("viewport width had no effect: width@800=%d not greater than width@400=%d", ww, nw)
	}
}

// TestOpenHTMLBytesBackground confirms the RasterOptions.Background fill reaches the
// page: a corner pixel (outside any drawn content) is the requested color.
func TestOpenHTMLBytesBackground(t *testing.T) {
	doc, err := OpenHTMLBytes([]byte(sampleHTML), WithViewportWidth(400))
	if err != nil {
		t.Fatalf("OpenHTMLBytes: %v", err)
	}
	bg := color.RGBA{R: 0, G: 128, B: 255, A: 255}
	img, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: 96, Background: bg})
	if err != nil {
		t.Fatalf("RasterizePage: %v", err)
	}
	rgba, ok := img.(*image.RGBA)
	if !ok {
		t.Fatalf("rasterized image is %T, want *image.RGBA", img)
	}
	if got := rgba.RGBAAt(rgba.Bounds().Min.X, rgba.Bounds().Min.Y); got != bg {
		t.Fatalf("corner pixel = %v, want background %v", got, bg)
	}
}

// TestOpenHTMLFile exercises the file-path entry point and its DirLoader: a
// <link rel=stylesheet> resolves relative to the file's directory and its
// background color is applied to the page, proving the loader is wired up.
func TestOpenHTMLFile(t *testing.T) {
	dir := t.TempDir()
	const themeBG = "#0080ff" // matches color.RGBA{0,128,255,255}
	if err := os.WriteFile(filepath.Join(dir, "theme.css"),
		[]byte("body { background-color: "+themeBG+"; }"), 0o644); err != nil {
		t.Fatal(err)
	}
	htmlPath := filepath.Join(dir, "page.html")
	page := `<html><head><link rel="stylesheet" href="theme.css"></head>` +
		`<body><p style="color:black">Hello world</p></body></html>`
	if err := os.WriteFile(htmlPath, []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}

	doc, err := OpenHTML(htmlPath)
	if err != nil {
		t.Fatalf("OpenHTML: %v", err)
	}
	if doc.PageCount() != 1 {
		t.Fatalf("PageCount = %d, want 1", doc.PageCount())
	}
	img, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: 96})
	if err != nil {
		t.Fatalf("RasterizePage: %v", err)
	}
	rgba, ok := img.(*image.RGBA)
	if !ok {
		t.Fatalf("rasterized image is %T, want *image.RGBA", img)
	}
	// The DirLoader resolved theme.css next to the file, so the <body> background
	// painted blue across its (margin-inset) box. We assert the linked color
	// dominates the image rather than probing one pixel: a UA body margin keeps the
	// page corner white, so a corner check would be wrong, and a single interior
	// point is fragile to layout shifts. If the link had not resolved the page would
	// be all-white (zero blue pixels).
	want := color.RGBA{R: 0, G: 128, B: 255, A: 255}
	b := rgba.Bounds()
	blue := countColor(rgba, want)
	if blue < b.Dx()*b.Dy()/4 {
		t.Fatalf("linked background covered only %d/%d pixels; DirLoader did not resolve theme.css",
			blue, b.Dx()*b.Dy())
	}
}

// TestOpenHTMLMissingFile reports a wrapped error (not a panic) for a path that
// does not exist.
func TestOpenHTMLMissingFile(t *testing.T) {
	if _, err := OpenHTML(filepath.Join(t.TempDir(), "does-not-exist.html")); err == nil {
		t.Fatal("expected an error opening a missing HTML file")
	}
}

// TestOpenHTMLBytesDegrades checks graceful degradation: empty and garbage input
// neither error nor panic (x/net/html is lenient and Build recovers), yielding a
// one-page Document that rasterizes (possibly blank).
func TestOpenHTMLBytesDegrades(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"empty", []byte("")},
		{"garbage", []byte("<<<not really html >>> <p><<")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := OpenHTMLBytes(tc.data)
			if err != nil {
				t.Fatalf("OpenHTMLBytes(%q): unexpected error: %v", tc.name, err)
			}
			if doc.PageCount() != 1 {
				t.Fatalf("PageCount = %d, want 1", doc.PageCount())
			}
			if _, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: 96}); err != nil {
				t.Fatalf("RasterizePage: %v", err)
			}
		})
	}
}
