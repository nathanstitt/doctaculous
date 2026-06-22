package doctaculous

import (
	"context"
	"errors"
	"image"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	{"imagemagick-images.pdf", 6, false},    // classic xref, 6 image pages
	{"google-doc-document.pdf", 1, false},   // Skia/Chrome, Type0/Type3 fonts + images
	{"cropped-rotated-scaled.pdf", 4, true}, // /Rotate + crop/scale, Form-XObject-heavy (Do in content)
	{"pdflatex-forms.pdf", 1, true},         // page text in an embedded classic Type1 font (now renders)
	{"libreoffice-form.pdf", 1, false},      // AcroForm (Tx/Btn/Ch), different producer; forms via annotations
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

// TestExternalBlendingDegradesGracefully covers the v1 contract that unsupported
// transparency degrades gracefully: it must skip + debug-log, not panic or error.
// cropped-rotated-scaled.pdf carries real blend state (/BM /Multiply and /ca 0.5)
// applied via the ExtGState "gs" operator, which v1 does not interpret. The page
// must still rasterize, and the interpreter must report the skip through Logf.
func TestExternalBlendingDegradesGracefully(t *testing.T) {
	path := filepath.Join(externalFixtureDir, "cropped-rotated-scaled.pdf")
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		t.Skipf("fixture missing: %s", path)
	}

	doc, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Logf is called from multiple goroutines by RasterizePages, so guard it.
	var (
		mu   sync.Mutex
		msgs []string
	)
	logf := func(format string, args ...any) {
		mu.Lock()
		msgs = append(msgs, format)
		mu.Unlock()
	}

	results := doc.RasterizePages(context.Background(), doc.AllPages(), RasterOptions{DPI: 72, Logf: logf})
	for _, r := range results {
		if r.Err != nil {
			t.Errorf("page %d errored instead of degrading gracefully: %v", r.Index, r.Err)
		}
	}

	// The "gs" operator carrying the blend mode / alpha must be reported skipped.
	const wantSubstr = "/ExtGState (gs) not applied"
	found := false
	mu.Lock()
	for _, m := range msgs {
		if strings.Contains(m, wantSubstr) {
			found = true
			break
		}
	}
	mu.Unlock()
	if !found {
		t.Errorf("expected a debug log containing %q for unsupported blend state; got %v", wantSubstr, msgs)
	}
}
