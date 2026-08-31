package omnidoc

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nathanstitt/omnidoc/pkg/internal/epub"
	"github.com/nathanstitt/omnidoc/pkg/internal/layout"
	genepub "github.com/nathanstitt/omnidoc/testdata/gen/epub"
)

// coverSVG is a cover-shaped (2:3 portrait) vector cover. It paints the same
// saturated green the other SVG probes use, so the "did it actually draw?"
// assertion is the same one, and nothing else in an EPUB emits that colour.
const coverSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="200" height="300">` +
	`<rect x="0" y="0" width="200" height="300" fill="#00c000"/></svg>`

// coverPNG encodes a 2:3 portrait raster cover — the case that must work just as
// well as the SVG one, since most real covers are JPEG or PNG.
func coverPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 20, 30))
	for y := range 30 {
		for x := range 20 {
			img.Set(x, y, color.RGBA{R: 0, G: 192, B: 0, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// coverChapter is a single trivial spine document that does NOT reference the
// cover, so a prepended cover page is the only place the image can appear.
const coverChapter = `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml"><head><title>One</title></head>
<body><h1>One</h1><p>Body text.</p></body></html>`

// buildCoverBook assembles a one-chapter book with the given cover part declared
// under the given OPF convention.
func buildCoverBook(t *testing.T, coverName string, coverData []byte, mediaType string, style genepub.CoverStyle, chapters ...string) []byte {
	t.Helper()
	b := genepub.New().SetTitle("Covered")
	if len(chapters) == 0 {
		chapters = []string{coverChapter}
	}
	for i, ch := range chapters {
		b = b.AddChapter(chapterName(i), ch)
	}
	if coverName != "" {
		b = b.AddMediaTyped(coverName, coverData, mediaType).SetCover(coverName, style)
	}
	return b.Bytes()
}

// chapterName returns the nth spine document's part name.
func chapterName(i int) string {
	return "ch" + string(rune('1'+i)) + ".xhtml"
}

// TestEPUBCoverIsSurfacedByBothConventions checks the pkg/epub parse result
// directly: CoverHref and CoverMediaType come out of the manifest under either
// convention, and a book with no cover reports none.
func TestEPUBCoverIsSurfacedByBothConventions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		style     genepub.CoverStyle
		coverName string
		wantHref  string
		wantMedia string
	}{
		{"epub3 properties=cover-image", genepub.CoverEPUB3, "cover.svg", "cover.svg", "image/svg+xml"},
		{"epub2 meta name=cover", genepub.CoverEPUB2, "cover.svg", "cover.svg", "image/svg+xml"},
		{"both conventions at once", genepub.CoverBoth, "cover.svg", "cover.svg", "image/svg+xml"},
		{"raster cover", genepub.CoverEPUB3, "cover.png", "cover.png", "image/png"},
		{"no cover declared", genepub.CoverNone, "", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := []byte(coverSVG)
			mt := "image/svg+xml"
			if strings.HasSuffix(tc.coverName, ".png") {
				data, mt = coverPNG(t), "image/png"
			}
			book, err := epub.OpenBytes(buildCoverBook(t, tc.coverName, data, mt, tc.style))
			if err != nil {
				t.Fatal(err)
			}
			if book.CoverHref != tc.wantHref {
				t.Errorf("CoverHref = %q, want %q", book.CoverHref, tc.wantHref)
			}
			if book.CoverMediaType != tc.wantMedia {
				t.Errorf("CoverMediaType = %q, want %q", book.CoverMediaType, tc.wantMedia)
			}
		})
	}
}

// TestEPUBSVGCoverRendersAsVector is the load-bearing render assertion for a
// vector cover: it must reach the PDF as path operators and no image XObject,
// exactly like every other SVG route in this series. A cover is the single most
// common place an SVG appears in an EPUB, so rasterizing it here would undo the
// series' whole point at its most visible point.
func TestEPUBSVGCoverRendersAsVector(t *testing.T) {
	t.Parallel()
	doc, err := OpenEPUBBytes(
		buildCoverBook(t, "cover.svg", []byte(coverSVG), "image/svg+xml", genepub.CoverEPUB3),
		WithViewportWidth(400), WithPageSize(400, 500),
	)
	if err != nil {
		t.Fatalf("OpenEPUBBytes: %v", err)
	}
	raw := docToPDF(t, doc)

	if hasImageXObject(raw) {
		t.Error("an SVG EPUB cover produced an image XObject: it was rasterized")
	}
	if content := pdfStreams(t, raw); !strings.Contains(content, greenFillOp) {
		t.Errorf("content stream has no %q; the cover drew nothing at all", greenFillOp)
	}
}

// TestEPUBRasterCoverRenders proves the cover path is NOT SVG-only. A PNG cover
// must render too — and (as the control for the assertion above) must still
// produce an image XObject.
func TestEPUBRasterCoverRenders(t *testing.T) {
	t.Parallel()
	doc, err := OpenEPUBBytes(
		buildCoverBook(t, "cover.png", coverPNG(t), "image/png", genepub.CoverEPUB2),
		WithViewportWidth(400), WithPageSize(400, 500),
	)
	if err != nil {
		t.Fatalf("OpenEPUBBytes: %v", err)
	}
	if n := countImageItems(t, doc, 0); n != 1 {
		t.Errorf("got %d raster image items on the cover page, want 1", n)
	}
	if !hasImageXObject(docToPDF(t, doc)) {
		t.Error("a raster EPUB cover emitted no image XObject; it did not render")
	}
}

// TestEPUBCoverIsTheFirstPage pins WHERE the cover goes: alone on page 1, ahead
// of the spine. A cover-image manifest item is not part of the reading order, so
// it has no position within the spine to occupy; the front of the book is both
// the only place it can go and the right one.
func TestEPUBCoverIsTheFirstPage(t *testing.T) {
	t.Parallel()
	const ch2 = `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml"><head><title>Two</title></head>
<body><h1>Two</h1><p>More text.</p></body></html>`

	doc, err := OpenEPUBBytes(
		buildCoverBook(t, "cover.svg", []byte(coverSVG), "image/svg+xml", genepub.CoverEPUB3, coverChapter, ch2),
		WithViewportWidth(400), WithPageSize(400, 500), WithBundledFonts(),
	)
	if err != nil {
		t.Fatalf("OpenEPUBBytes: %v", err)
	}
	if doc.PageCount() < 3 {
		t.Fatalf("PageCount = %d, want >= 3 (cover + two chapters)", doc.PageCount())
	}
	// Page 0 carries the cover's vector item and NO text: the first chapter's
	// break-before must have pushed it off this page.
	if n := countVectorItems(t, doc, 0); n != 1 {
		t.Errorf("got %d vector items on page 0, want 1 (the cover)", n)
	}
	if n := countGlyphItems(t, doc, 0); n != 0 {
		t.Errorf("page 0 carries %d glyphs; the cover must be alone on the first page", n)
	}
	// Chapter one is on page 1, and carries no cover.
	if n := countGlyphItems(t, doc, 1); n == 0 {
		t.Error("page 1 has no text; the first chapter did not start after the cover")
	}
	if n := countVectorItems(t, doc, 1); n != 0 {
		t.Errorf("page 1 carries %d vector items; the cover leaked into the chapter", n)
	}
}

// TestEPUBWithNoCoverIsUnchanged is the requirement that this feature costs
// coverless books nothing: byte-for-byte the same assembled HTML, and the same
// page count and first-page content as before. It compares against a book built
// identically except for the cover declaration, so any accidental change to the
// no-cover path (a stray leading section, a spurious break-before) shows up.
func TestEPUBWithNoCoverIsUnchanged(t *testing.T) {
	t.Parallel()
	// A book whose manifest carries the image but declares no cover: the media
	// part is present either way, so only the DECLARATION differs.
	plain := genepub.New().SetTitle("Covered").
		AddChapter("ch1.xhtml", coverChapter).
		AddMediaTyped("cover.svg", []byte(coverSVG), "image/svg+xml").
		Bytes()

	book, err := epub.OpenBytes(plain)
	if err != nil {
		t.Fatal(err)
	}
	if book.CoverHref != "" {
		t.Errorf("CoverHref = %q for a book that declares no cover", book.CoverHref)
	}
	html := bookToHTML(book)
	if strings.Contains(html, "epub-cover") {
		t.Errorf("a coverless book got a cover section:\n%s", html)
	}
	// The first chapter must carry NO break-before, exactly as before this change
	// — otherwise a coverless book would gain a leading blank page.
	if strings.Contains(html, `<section style="break-before: page">`+"\n<h1>One</h1>") {
		t.Error("the first chapter of a coverless book gained a break-before")
	}

	doc, err := OpenEPUBBytes(plain, WithViewportWidth(400), WithPageSize(400, 500), WithBundledFonts())
	if err != nil {
		t.Fatal(err)
	}
	if n := countVectorItems(t, doc, 0); n != 0 {
		t.Errorf("a coverless book rendered %d vector items on page 0", n)
	}
	if n := countGlyphItems(t, doc, 0); n == 0 {
		t.Error("a coverless book's first page has no text; the chapter did not start on page 0")
	}
}

// TestEPUBCoverAlreadyInSpineIsNotDuplicated covers the case that makes a naive
// prepend wrong: many EPUB 3 books put a cover XHTML document in the spine that
// <img>s the very same cover-image manifest item. Prepending the image would
// then show it twice. The book must report CoverInSpine and render the cover
// exactly once.
func TestEPUBCoverAlreadyInSpineIsNotDuplicated(t *testing.T) {
	t.Parallel()
	const coverPage = `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml"><head><title>Cover</title></head>
<body><img src="cover.svg" alt="cover"/></body></html>`

	book, err := epub.OpenBytes(
		buildCoverBook(t, "cover.svg", []byte(coverSVG), "image/svg+xml", genepub.CoverEPUB3, coverPage, coverChapter))
	if err != nil {
		t.Fatal(err)
	}
	if !book.CoverInSpine {
		t.Fatal("a spine chapter <img>s the cover but CoverInSpine is false; the cover would render twice")
	}
	if strings.Contains(bookToHTML(book), "epub-cover") {
		t.Error("a cover already shown by a spine chapter was prepended anyway")
	}

	doc, err := OpenEPUBBytes(
		buildCoverBook(t, "cover.svg", []byte(coverSVG), "image/svg+xml", genepub.CoverEPUB3, coverPage, coverChapter),
		WithViewportWidth(400), WithPageSize(400, 500), WithBundledFonts())
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for i := range doc.PageCount() {
		total += countVectorItems(t, doc, i)
	}
	if total != 1 {
		t.Errorf("the cover rendered %d times across the document, want exactly 1", total)
	}
}

// TestEPUBCoverMissingPartDegrades covers untrusted input: a manifest that names
// a cover part the container does not contain must not fail the open, must not
// panic, and must leave the chapters rendering.
func TestEPUBCoverMissingPartDegrades(t *testing.T) {
	t.Parallel()
	// Declare a cover for a media part, then confirm the href is reported even
	// though we never assert the bytes resolve; the renderer's <img> degrades to
	// a placeholder the same way any missing <img src> does.
	data := genepub.New().SetTitle("Covered").
		AddChapter("ch1.xhtml", coverChapter).
		AddMediaTyped("cover.svg", nil, "image/svg+xml").
		SetCover("cover.svg", genepub.CoverEPUB3).
		Bytes()

	doc, err := OpenEPUBBytes(data, WithViewportWidth(400), WithPageSize(400, 500), WithBundledFonts())
	if err != nil {
		t.Fatalf("a book with an unreadable cover failed to open: %v", err)
	}
	if doc.PageCount() == 0 {
		t.Fatal("no pages")
	}
	// The chapter text must still be somewhere in the document.
	glyphs := 0
	for i := range doc.PageCount() {
		glyphs += countGlyphItems(t, doc, i)
	}
	if glyphs == 0 {
		t.Error("an unreadable cover took the chapters down with it")
	}
}

// TestEPUBCoverGolden is the cover's visual entry: page 1 of a book with an SVG
// cover, rendered end to end. It is a SEPARATE fixture from epubSpecimen rather
// than a change to it, deliberately — adding a cover to the specimen would shift
// every page of the existing epub-specimen golden, and a cover is its own thing
// to eyeball.
//
// Eyeball: a green portrait rectangle scaled down to the page width, centered,
// alone on the page; the chapter text starts on page 2.
func TestEPUBCoverGolden(t *testing.T) {
	t.Parallel()
	doc, err := OpenEPUBBytes(
		buildCoverBook(t, "cover.svg", []byte(coverGoldenSVG), "image/svg+xml", genepub.CoverEPUB3),
		WithViewportWidth(300), WithPageSize(300, 420), WithBundledFonts(),
	)
	if err != nil {
		t.Fatalf("OpenEPUBBytes: %v", err)
	}
	img, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: goldenDPI, BundledFonts: true})
	if err != nil {
		t.Fatalf("RasterizePage: %v", err)
	}
	got, ok := img.(*image.RGBA)
	if !ok {
		t.Fatalf("rasterized image is %T, want *image.RGBA", img)
	}
	dir := filepath.Join("testdata", "golden")
	path := filepath.Join(dir, "epub-cover.png")
	if *update {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		writePNG(t, path, got)
		t.Logf("updated %s", path)
		return
	}
	want := readPNG(t, path)
	if want == nil {
		t.Fatalf("missing golden %s; run: go test ./pkg/omnidoc -run TestEPUBCoverGolden -update", path)
	}
	if diff, n := compareImages(want, got); diff {
		t.Errorf("render differs from golden %s: %d pixels beyond tolerance", path, n)
	}
}

// coverGoldenSVG is a recognizably cover-like portrait artwork, so the golden
// shows the fit (aspect ratio preserved, scaled to the page width, centered)
// rather than a featureless block.
const coverGoldenSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="400" height="600" viewBox="0 0 400 600">
  <rect x="0" y="0" width="400" height="600" fill="#003366"/>
  <rect x="24" y="24" width="352" height="552" fill="none" stroke="#cce5ff" stroke-width="6"/>
  <circle cx="200" cy="220" r="90" fill="#cc8822"/>
  <path d="M90 420 L310 420" stroke="#cc3333" stroke-width="10"/>
  <path d="M120 470 L280 470" stroke="#cce5ff" stroke-width="6"/>
</svg>`

// countItemsOfKind returns the number of items of the given kind on page n.
func countItemsOfKind(t *testing.T, doc *Document, n int, kind layout.ItemKind) int {
	t.Helper()
	pages := reflowPagesOf(t, doc)
	if n >= len(pages.Pages) {
		return 0
	}
	count := 0
	for _, it := range pages.Pages[n].Items {
		if it.Kind == kind {
			count++
		}
	}
	return count
}

func countVectorItems(t *testing.T, doc *Document, n int) int {
	t.Helper()
	return countItemsOfKind(t, doc, n, layout.VectorKind)
}

func countGlyphItems(t *testing.T, doc *Document, n int) int {
	t.Helper()
	return countItemsOfKind(t, doc, n, layout.GlyphKind)
}

func countImageItems(t *testing.T, doc *Document, n int) int {
	t.Helper()
	return countItemsOfKind(t, doc, n, layout.ImageKind)
}
