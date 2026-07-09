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
