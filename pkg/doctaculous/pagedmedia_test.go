package doctaculous

import (
	"context"
	"image"
	"image/color"
	"math"
	"testing"
)

// atPageDocHTML embeds an @page rule (A4, 0.5in margins) and stacks enough block
// content (1500pt) to span multiple A4 (1123pt-tall) pages.
const atPageDocHTML = `<html><head><style>
	@page { size: A4; margin: 0.5in }
</style></head><body>
	<div style="height:500px;margin:0;background-color:rgb(11,11,11)">a</div>
	<div style="height:500px;margin:0;background-color:rgb(22,22,22)">b</div>
	<div style="height:500px;margin:0;background-color:rgb(33,33,33)">c</div>
</body></html>`

// TestWithDefaultPagedUsesAtPage proves WithDefaultPaged paginates using the
// document's @page size: A4 pages (794 x 1123 @96), multiple of them for 1500pt of
// content, each non-blank.
func TestWithDefaultPagedUsesAtPage(t *testing.T) {
	doc, err := OpenHTMLBytes([]byte(atPageDocHTML), WithDefaultPaged())
	if err != nil {
		t.Fatalf("OpenHTMLBytes: %v", err)
	}
	if got := doc.PageCount(); got <= 1 {
		t.Fatalf("PageCount() = %d, want > 1 (1500pt of content on A4 pages)", got)
	}
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	for i := 0; i < doc.PageCount(); i++ {
		img, err := doc.RasterizePage(context.Background(), i, RasterOptions{})
		if err != nil {
			t.Fatalf("RasterizePage(%d): %v", i, err)
		}
		rgba, ok := img.(*image.RGBA)
		if !ok {
			t.Fatalf("page %d: image is %T, want *image.RGBA", i, img)
		}
		// A4 width at 96dpi is ~794px; the raster default scale is 1.0 (72dpi → but
		// RasterOptions default scale renders points 1:1). Assert the page is portrait
		// (taller than wide), the A4 signature.
		b := rgba.Bounds()
		if b.Dy() <= b.Dx() {
			t.Errorf("page %d bounds %v not portrait; expected A4 portrait", i, b)
		}
		if n := countNonBackground(rgba, white); n == 0 {
			t.Fatalf("page %d: painted nothing", i)
		}
	}
}

// TestAtPageInertWithoutOptIn is the byte-identical guard: a document WITH an @page
// rule but opened WITHOUT WithDefaultPaged / WithPageSize is still a single tall page
// (the @page rule is inert until pagination is requested).
func TestAtPageInertWithoutOptIn(t *testing.T) {
	doc, err := OpenHTMLBytes([]byte(atPageDocHTML))
	if err != nil {
		t.Fatalf("OpenHTMLBytes: %v", err)
	}
	if got := doc.PageCount(); got != 1 {
		t.Fatalf("PageCount() = %d, want 1 (@page inert without opt-in)", got)
	}
	// The single page is taller than any A4 page (1500pt+ of content), proving it is
	// the single-tall path, not an A4 slice.
	pg := doc.r.(*reflowRenderer).pages.Pages[0]
	if pg.HeightPt < 1400 {
		t.Errorf("single page height = %.0f, want full content height (single-tall path)", pg.HeightPt)
	}
}

// tallNoAtPageHTML stacks 1500pt of content with NO @page rule (taller than a Letter
// page's 1056pt), so WithDefaultPaged must slice it into multiple Letter pages.
const tallNoAtPageHTML = `<html><body>` +
	`<div style="height:500px;margin:0;background-color:rgb(11,11,11)">a</div>` +
	`<div style="height:500px;margin:0;background-color:rgb(22,22,22)">b</div>` +
	`<div style="height:500px;margin:0;background-color:rgb(33,33,33)">c</div>` +
	`</body></html>`

// TestWithDefaultPagedNoAtPageFallsBackToLetter proves WithDefaultPaged on a document
// with NO @page rule falls back to US-Letter pages.
func TestWithDefaultPagedNoAtPageFallsBackToLetter(t *testing.T) {
	doc, err := OpenHTMLBytes([]byte(tallNoAtPageHTML), WithDefaultPaged())
	if err != nil {
		t.Fatalf("OpenHTMLBytes: %v", err)
	}
	if got := doc.PageCount(); got <= 1 {
		t.Fatalf("PageCount() = %d, want > 1 (1500pt on Letter pages)", got)
	}
	pg := doc.r.(*reflowRenderer).pages.Pages[0]
	if math.Abs(pg.WidthPt-LetterWidthPt) > 0.5 || math.Abs(pg.HeightPt-LetterHeightPt) > 0.5 {
		t.Errorf("page size = %.0f x %.0f, want Letter %d x %d (fallback)",
			pg.WidthPt, pg.HeightPt, LetterWidthPt, LetterHeightPt)
	}
}
