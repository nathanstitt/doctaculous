package doctaculous

import (
	"context"
	"image"
	"os"
	"path/filepath"
	"testing"
)

// mdTextGoldens render the Markdown and plain-text frontends end to end
// (frontend → HTML pipeline → CSS layout → raster) and compare to a committed
// PNG — the visual entry for the two input formats. The Markdown fixture is
// the same construct-covering specimen the round-trip test uses, so what the
// parity tests prove structurally is also eyeballed. Narrow viewports keep the
// PNGs small.
var mdTextGoldens = []struct {
	name       string
	viewportPx float64
	open       func(data []byte, opts ...HTMLOption) (*Document, error)
	src        string
}{
	{
		// Every GFM construct: headings (with the default sheet's underline),
		// emphasis/strike/code, nested + ordered + task lists, blockquote (left
		// border + muted color), fenced code (shaded), an aligned table (full
		// cell borders), a thematic break, and a link (UA blue + underline).
		name:       "md-specimen",
		viewportPx: 420,
		open:       OpenMarkdownBytes,
		src:        specimenMD,
	},
	{
		// Plain text: hard line breaks and column alignment preserved verbatim
		// in monospace; the over-long line soft-wraps (pre-wrap) instead of
		// clipping at the right edge.
		name:       "text-pre",
		viewportPx: 300,
		open:       OpenTextBytes,
		src: "PLAIN TEXT SPECIMEN\n\ncol one    col two\n1          2\n\n" +
			"a deliberately over-long line that must soft-wrap at this narrow viewport instead of clipping\n\n" +
			"markup stays literal: <b> & </b>\n",
	},
}

// TestMarkdownTextGolden mirrors TestHTMLGolden for the Markdown/plain-text
// frontends. Run with -update to regenerate, then eyeball the PNGs in review.
func TestMarkdownTextGolden(t *testing.T) {
	dir := filepath.Join("testdata", "golden")
	if *update {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range mdTextGoldens {
		t.Run(f.name, func(t *testing.T) {
			doc, err := f.open([]byte(f.src), WithViewportWidth(f.viewportPx), WithBundledFonts())
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			if doc.PageCount() != 1 {
				t.Errorf("PageCount = %d, want 1", doc.PageCount())
			}
			img, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: goldenDPI, BundledFonts: true})
			if err != nil {
				t.Fatalf("RasterizePage: %v", err)
			}
			got, ok := img.(*image.RGBA)
			if !ok {
				t.Fatalf("rasterized image is %T, want *image.RGBA", img)
			}

			path := filepath.Join(dir, f.name+".png")
			if *update {
				writePNG(t, path, got)
				t.Logf("updated %s", path)
				return
			}
			want := readPNG(t, path)
			if want == nil {
				t.Fatalf("missing golden %s; run: go test ./pkg/doctaculous -run TestMarkdownTextGolden -update", path)
			}
			if diff, n := compareImages(want, got); diff {
				t.Errorf("render differs from golden %s: %d pixels beyond tolerance (max %d)",
					path, n, int(maxDifferingFraction*float64(got.Bounds().Dx()*got.Bounds().Dy())))
			}
		})
	}
}
