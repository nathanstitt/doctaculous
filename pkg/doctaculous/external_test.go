package doctaculous

import (
	"bytes"
	"context"
	"errors"
	"image"
	"os"
	"path/filepath"
	"testing"
)

// externalFixtureDir holds real-world third-party PDFs (CC-BY-SA-4.0, see its
// README). Unlike the generated gen.Core corpus these are committed binaries
// from varied producers, so this acts as a non-hermetic smoke layer over the
// full Open -> PageCount -> RasterizePage chain. It is kept separate from the
// gen.Core sweeps so the generated corpus stays the reproducible source of truth.
var externalFixtureDir = filepath.Join("..", "..", "testdata", "external", "pdf")

// externalFixtures lists each fixture with the page count verified at download
// time. The two pdfTeX files store their page objects inside object streams
// (xref-stream + ObjStm), so asserting their counts exercises ObjStm page
// traversal; the others use classic xref tables.
var externalFixtures = []struct {
	file  string
	pages int
	// nonBlank marks fixtures whose first page must render visible content (not a
	// blank page) — proving the relevant glyph/painting path actually emits ink.
	// cropped-rotated-scaled draws via form XObjects (Do); pdflatex-forms draws
	// text in an embedded classic Type1 font, which now renders. (The AcroForm
	// widget *appearance* streams are reached through annotations, out of scope
	// for v1, but these pages also carry real page-content text/forms.)
	nonBlank bool
}{
	{"pdflatex-4-pages.pdf", 4, false},      // xref stream + ObjStm, multi-page
	{"multicolumn.pdf", 3, false},           // xref stream + ObjStm, dense text/vector
	{"imagemagick-images.pdf", 6, true},     // 6 ICCBased-gray images (now decode)
	{"google-doc-document.pdf", 1, false},   // Skia/Chrome, Type0/Type3 fonts + images
	{"cropped-rotated-scaled.pdf", 4, true}, // /Rotate + crop/scale, Form-XObject-heavy (Do in content)
	{"pdflatex-forms.pdf", 1, true},         // page text in an embedded classic Type1 font (now renders)
	{"libreoffice-form.pdf", 1, true},       // labels in symbolic subset TrueType fonts (now render)
	{"jbig2-scan.pdf", 1, true},             // embedded JBIG2 (/JBIG2Decode) image — exercises the JBIG2 decode path
}

// imageHasInk reports whether img has any pixel darker/more-colored than near
// white — i.e. something was actually painted. Used to assert form-bearing pages
// are not blank.
func imageHasInk(img image.Image) bool {
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			// RGBA() returns 16-bit values; ~0xF000 ≈ 94% of full white.
			if r < 0xF000 || g < 0xF000 || bl < 0xF000 {
				return true
			}
		}
	}
	return false
}

// TestExternalCorpus enforces the same uniform contract gen.Core uses, against
// real third-party PDFs: each parses to a Document, reports its expected page
// count, and rasterizes its first page without error. If the fixtures are
// missing (e.g. a sparse checkout) the test skips rather than fails.
func TestExternalCorpus(t *testing.T) {
	if _, err := os.Stat(externalFixtureDir); errors.Is(err, os.ErrNotExist) {
		t.Skipf("external fixtures not present at %s", externalFixtureDir)
	}

	for _, f := range externalFixtures {
		t.Run(f.file, func(t *testing.T) {
			path := filepath.Join(externalFixtureDir, f.file)
			if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
				t.Skipf("fixture missing: %s", path)
			}

			doc, err := Open(path)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			if got := doc.PageCount(); got != f.pages {
				t.Errorf("PageCount = %d, want %d", got, f.pages)
			}

			img, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: 72})
			if err != nil {
				t.Fatalf("RasterizePage(0): %v", err)
			}
			if b := img.Bounds(); b.Dx() <= 0 || b.Dy() <= 0 {
				t.Errorf("rasterized image has empty bounds %v", b)
			}
			// Pages flagged nonBlank must produce visible content: a blank page here
			// would mean a glyph/painting path was silently skipped (e.g. the
			// pre-support form-XObject or classic-Type1 bugs).
			if f.nonBlank && !imageHasInk(img) {
				t.Errorf("page 0 rendered blank; expected visible content")
			}
		})
	}
}

// TestExternalRasterizeAllPages renders every page of each fixture concurrently
// and requires all pages to succeed, exercising the parallel render path over
// real documents (rotation, composite fonts, multiple image filters).
func TestExternalRasterizeAllPages(t *testing.T) {
	if _, err := os.Stat(externalFixtureDir); errors.Is(err, os.ErrNotExist) {
		t.Skipf("external fixtures not present at %s", externalFixtureDir)
	}

	for _, f := range externalFixtures {
		t.Run(f.file, func(t *testing.T) {
			path := filepath.Join(externalFixtureDir, f.file)
			if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
				t.Skipf("fixture missing: %s", path)
			}

			doc, err := Open(path)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			results := doc.RasterizePages(context.Background(), doc.AllPages(), RasterOptions{DPI: 72})
			if len(results) != f.pages {
				t.Fatalf("got %d results, want %d", len(results), f.pages)
			}
			for _, r := range results {
				if r.Err != nil {
					t.Errorf("page %d: %v", r.Index, r.Err)
					continue
				}
				if _, ok := r.Image.(*image.RGBA); !ok || r.Image == nil {
					t.Errorf("page %d: missing or unexpected image type %T", r.Index, r.Image)
				}
			}
		})
	}
}

// TestExternalConstantAlphaApplied verifies that ExtGState constant alpha is
// honored on a real document. cropped-rotated-scaled.pdf draws its content under
// an ExtGState with /ca 0.5 (50% fill opacity). Every page must rasterize without
// error, and the drawn green form boxes must come out semi-transparent (blended
// toward the white background) rather than fully saturated.
func TestExternalConstantAlphaApplied(t *testing.T) {
	path := filepath.Join(externalFixtureDir, "cropped-rotated-scaled.pdf")
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		t.Skipf("fixture missing: %s", path)
	}

	doc, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	results := doc.RasterizePages(context.Background(), doc.AllPages(), RasterOptions{DPI: 72})
	for _, r := range results {
		if r.Err != nil {
			t.Errorf("page %d errored: %v", r.Index, r.Err)
		}
	}

	// On page 0, find the most-saturated green pixel. With /ca 0.5 over white it
	// should be a tint (G high, but R and B lifted well above 0 by the 50% blend),
	// not pure green (which would mean alpha was ignored).
	img := results[0].Image
	b := img.Bounds()
	var best struct{ r, g, bl uint32 }
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			rr, gg, bb, _ := img.At(x, y).RGBA()
			if gg > rr && gg > bb && gg > best.g {
				best.r, best.g, best.bl = rr, gg, bb
			}
		}
	}
	if best.g == 0 {
		t.Fatal("no green pixels found; expected the form boxes")
	}
	// 8-bit channels. Pure green would be ~(0,255,0); a 50% tint over white is
	// ~(128,191,128). Require the red channel to be clearly lifted off zero.
	r8 := best.r >> 8
	if r8 < 64 {
		t.Errorf("green pixel R=%d too saturated; /ca 0.5 alpha not applied (want a tint)", r8)
	}
}

// TestExternalOfficeCorpus runs the committed real-world DOCX and XLSX
// fixtures (testdata/external/{docx,xlsx} — Word-, Excel-, Mac-Office-, and
// LibreOffice-authored; see their READMEs for provenance and licensing)
// through the full pipeline: content-detected Open, layout + first-page
// raster, and plain-text conversion. Files whose only content lives in
// headers, comments, deleted revisions, or group shapes legitimately convert
// to empty text (those renders are pinned invisible by design), so only the
// conversion CALL must succeed for them.
func TestExternalOfficeCorpus(t *testing.T) {
	// Content invisible to the structure writers by design.
	emptyTextOK := map[string]bool{
		"comment.docx":                             true, // body is one comment-reference run
		"headerFooter.docx":                        true, // content only in headers/footers
		"groupshape-trackedchanges.docx":           true, // text inside a group shape (not extracted)
		"redline-range-comment.docx":               true, // content deleted by revision + comment
		"55406_Conditional_formatting_sample.xlsx": true, // style-only cells (a pure CF-fill demo, no values)
	}
	for _, dir := range []string{"docx", "xlsx"} {
		root := filepath.Join("..", "..", "testdata", "external", dir)
		files, err := filepath.Glob(filepath.Join(root, "*."+dir))
		if err != nil || len(files) == 0 {
			t.Skipf("external %s corpus not present", dir)
		}
		for _, path := range files {
			t.Run(filepath.Base(path), func(t *testing.T) {
				doc, err := Open(path, WithBundledFonts())
				if err != nil {
					t.Fatalf("Open: %v", err)
				}
				if doc.PageCount() < 1 {
					t.Fatalf("PageCount = %d, want >= 1", doc.PageCount())
				}
				img, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: 36, BundledFonts: true})
				if err != nil {
					t.Fatalf("RasterizePage(0): %v", err)
				}
				if b := img.Bounds(); b.Dx() <= 0 || b.Dy() <= 0 {
					t.Errorf("rasterized image has empty bounds %v", b)
				}
				var txt bytes.Buffer
				if err := doc.WriteText(context.Background(), &txt, MarkdownOptions{}); err != nil {
					t.Fatalf("WriteText: %v", err)
				}
				if txt.Len() == 0 && !emptyTextOK[filepath.Base(path)] {
					t.Errorf("text conversion is empty")
				}
			})
		}
	}
}
