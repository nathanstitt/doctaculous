package doctaculous

import (
	"context"
	"testing"

	"github.com/nathanstitt/doctaculous/testdata/gen"
)

// TestRasterizeBundledFontsMode renders a PDF that uses a non-embedded base-14 font in
// bundled mode (BundledFonts:true). This must succeed and be hermetic (no system fonts
// consulted). It is the mode the golden tests rely on.
func TestRasterizeBundledFontsMode(t *testing.T) {
	doc, err := OpenBytes(gen.WeightedFontsPDF())
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	img, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: 72, BundledFonts: true})
	if err != nil {
		t.Fatalf("RasterizePage (bundled): %v", err)
	}
	if img == nil {
		t.Fatal("nil image")
	}
}

// TestRasterizeSystemFontsDefault renders in the default (system) mode. It must not
// error regardless of what fonts the host has (system match, or fall-through to the
// bundled safety net on a bare box).
func TestRasterizeSystemFontsDefault(t *testing.T) {
	doc, err := OpenBytes(gen.WeightedFontsPDF())
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	img, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: 72})
	if err != nil {
		t.Fatalf("RasterizePage (system default): %v", err)
	}
	if img == nil {
		t.Fatal("nil image")
	}
}

// missProvider is a font.Provider that never resolves — it simulates sysfont returning
// nil on a bare machine, so resolution must fall through to the bundled substitute.
type missProvider struct{}

func (missProvider) LoadStyled(string, bool, bool) ([]byte, bool) { return nil, false }

// TestSystemMissFallsBackToBundled: an explicit always-miss provider (the sysfont-nil
// case) still renders, because the bundled substitute is the fall-through in the
// resolution chain.
func TestSystemMissFallsBackToBundled(t *testing.T) {
	doc, err := OpenBytes(gen.WeightedFontsPDF())
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	img, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: 72, FontProvider: missProvider{}})
	if err != nil {
		t.Fatalf("RasterizePage: %v", err)
	}
	// The always-miss provider forces the bundled fall-through; the page's text must
	// actually render (ink drawn), not silently skip.
	if inked := countInkedPixels(img); inked == 0 {
		t.Fatal("page is blank; the bundled fallback did not render the text")
	}
}
