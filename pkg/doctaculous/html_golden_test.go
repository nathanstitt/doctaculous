package doctaculous

import (
	"context"
	"image"
	"os"
	"path/filepath"
	"testing"
)

// htmlGoldens are small HTML fixtures rendered end to end (parse -> box generation
// -> CSS layout -> paint -> raster) and compared to a committed PNG. Each exercises
// a distinct, eyeball-able slice of the normal-flow feature set. A fixed small
// viewport keeps the PNGs small and quick to review. body{margin:0} keeps the page
// geometry flush to the top-left corner.
var htmlGoldens = []struct {
	name string
	// viewportPx is the layout viewport width this fixture renders at.
	viewportPx float64
	html       string
}{
	{
		// Background + border + padding + centered text in one styled block.
		name:       "styled-box",
		viewportPx: 300,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .card {
    background: #cce5ff;
    border: 5px solid #003366;
    padding: 20px;
    text-align: center;
    color: #003366;
  }
</style></head><body>
  <div class="card">Hello, boxes!</div>
</body></html>`,
	},
	{
		// Block stacking + inline text wrapping + paragraph spacing from the UA
		// margins (p { margin: 16px 0 }, collapsing to a 16px gap between paragraphs).
		name:       "paragraphs",
		viewportPx: 300,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
</style></head><body>
  <p>The first paragraph has enough text to wrap across more than one line at this narrow width.</p>
  <p>A second paragraph sits below the first with the usual gap.</p>
  <p>A third, short one.</p>
</body></html>`,
	},
	{
		// inline-block boxes flow horizontally side by side (the Task 6/6b feature):
		// three fixed-size boxes with distinct backgrounds on one row.
		name:       "inline-block-row",
		viewportPx: 300,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .swatch {
    display: inline-block;
    width: 70px;
    height: 70px;
  }
  .a { background: #cc3333; }
  .b { background: #33aa33; }
  .c { background: #3355cc; }
</style></head><body>
  <div><span class="swatch a"></span><span class="swatch b"></span><span class="swatch c"></span></div>
</body></html>`,
	},
	{
		// Border STYLES: solid / dashed / dotted / double on stacked divs, to eyeball
		// that each border style renders distinctly.
		name:       "border-styles",
		viewportPx: 300,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  div { height: 24px; margin: 8px; border-width: 4px; border-color: #222266; }
  .s { border-style: solid; }
  .da { border-style: dashed; }
  .do { border-style: dotted; }
  .db { border-style: double; }
</style></head><body>
  <div class="s"></div>
  <div class="da"></div>
  <div class="do"></div>
  <div class="db"></div>
</body></html>`,
	},
}

// TestHTMLGolden renders each small HTML fixture's single page end to end and
// compares it to a committed PNG, mirroring TestDOCXGolden. Run with -update to
// regenerate the goldens, then eyeball every changed PNG in review.
func TestHTMLGolden(t *testing.T) {
	dir := filepath.Join("testdata", "golden")
	if *update {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range htmlGoldens {
		t.Run(f.name, func(t *testing.T) {
			doc, err := OpenHTMLBytes([]byte(f.html), WithViewportWidth(f.viewportPx))
			if err != nil {
				t.Fatalf("OpenHTMLBytes: %v", err)
			}
			if doc.PageCount() != 1 {
				t.Errorf("PageCount = %d, want 1", doc.PageCount())
			}
			img, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: goldenDPI})
			if err != nil {
				t.Fatalf("RasterizePage: %v", err)
			}
			got, ok := img.(*image.RGBA)
			if !ok {
				t.Fatalf("rasterized image is %T, want *image.RGBA", img)
			}

			path := filepath.Join(dir, "html-"+f.name+".png")
			if *update {
				writePNG(t, path, got)
				t.Logf("updated %s", path)
				return
			}
			want := readPNG(t, path)
			if want == nil {
				t.Fatalf("missing golden %s; run: go test ./pkg/doctaculous -run TestHTMLGolden -update", path)
			}
			if diff, n := compareImages(want, got); diff {
				t.Errorf("render differs from golden %s: %d pixels beyond tolerance (max %d)",
					path, n, int(maxDifferingFraction*float64(got.Bounds().Dx()*got.Bounds().Dy())))
			}
		})
	}
}
