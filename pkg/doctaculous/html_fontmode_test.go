package doctaculous

import (
	"context"
	"testing"
)

// TestOpenHTMLBundledFonts renders HTML naming a base-14 family in bundled mode and in
// the default system mode. Both must lay out without error; the assertion is that the
// option compiles and the pipeline runs (hermetic bundled path + system default path).
func TestOpenHTMLBundledFonts(t *testing.T) {
	src := []byte(`<html><body style="font-family:Helvetica"><p>Hello fonts</p></body></html>`)

	docB, err := OpenHTMLBytes(src, WithBundledFonts())
	if err != nil {
		t.Fatalf("OpenHTMLBytes (bundled): %v", err)
	}
	if _, err := docB.RasterizePage(context.Background(), 0, RasterOptions{DPI: 72, BundledFonts: true}); err != nil {
		t.Fatalf("rasterize bundled: %v", err)
	}

	docS, err := OpenHTMLBytes(src) // default: system mode
	if err != nil {
		t.Fatalf("OpenHTMLBytes (system default): %v", err)
	}
	if _, err := docS.RasterizePage(context.Background(), 0, RasterOptions{DPI: 72, BundledFonts: true}); err != nil {
		t.Fatalf("rasterize system: %v", err)
	}
}
