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
}{
	{"pdflatex-4-pages.pdf", 4},       // xref stream + ObjStm, multi-page
	{"multicolumn.pdf", 3},            // xref stream + ObjStm, dense text/vector
	{"imagemagick-images.pdf", 6},     // classic xref, 6 image pages
	{"google-doc-document.pdf", 1},    // Skia/Chrome, Type0/Type3 fonts + images
	{"cropped-rotated-scaled.pdf", 4}, // all four /Rotate values, crop/scale
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
