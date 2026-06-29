package doctaculous

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/resource"
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
	// loader resolves the fixture's external refs (e.g. <img src>); nil for
	// fixtures with no external resources.
	loader resource.ResourceLoader
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
	{
		// A decoded <img> rendered in a box: an inline image sized by width/height
		// (object-fit:fill stretches the 4-quadrant source into the box), plus a
		// block image below it at intrinsic-derived size. Eyeball that the image
		// renders upright (red top-left quadrant) and right-side-up.
		name:       "image-basic",
		viewportPx: 200,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .frame { padding: 10px; background: #eeeeee; }
  img.big { width: 120px; height: 60px; }
  img.block { display: block; width: 80px; height: 80px; margin-top: 10px; }
</style></head><body>
  <div class="frame">
    <img class="big" src="quad.png">
    <img class="block" src="quad.png">
  </div>
</body></html>`,
		loader: quadLoader(),
	},
	{
		// object-fit variants of the same square image stretched into wide boxes:
		// fill (stretch), contain (letterbox), cover (crop). Eyeball that contain
		// shows the whole image centered and cover fills the box edge-to-edge.
		name:       "image-object-fit",
		viewportPx: 200,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  img { display: block; width: 160px; height: 40px; margin: 6px; background: #cccccc; }
  .fill { object-fit: fill; }
  .contain { object-fit: contain; }
  .cover { object-fit: cover; }
</style></head><body>
  <img class="fill" src="quad.png">
  <img class="contain" src="quad.png">
  <img class="cover" src="quad.png">
</body></html>`,
		loader: quadLoader(),
	},
	{
		// A left-floated figure box with paragraph text wrapping beside it, then a
		// cleared block below. Eyeball: text hugs the float's right edge for the first
		// lines, returns to full width below the float, and the cleared block sits
		// under the float.
		name:       "float-figure",
		viewportPx: 240,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .fig { float: left; width: 70px; height: 60px; background: #cc3333; margin: 0 8px 4px 0; }
  .cap { clear: left; background: #eeeeee; }
</style></head><body>
  <div class="fig"></div>
  <p>This paragraph wraps its text beside the floated red figure box and then continues below it once the lines drop past the figure's bottom edge.</p>
  <div class="cap">A cleared caption sits below the float.</div>
</body></html>`,
	},
	// NOTE: the planned "float-row" golden (a body whose ONLY children are three
	// left-floated swatches) is intentionally omitted. Per CSS 2.1, floats do not
	// extend the height of a non-BFC parent, and the body here establishes no BFC,
	// so a float-only body has zero in-flow content height and the page collapses to
	// a degenerate 1×1 bitmap (paints nothing). That matches the float model the
	// geometry tests lock down (TestFloatPlacedOutOfFlow: a float consumes no
	// vertical space). The multi-float "two-on-a-row-then-wrap" behavior is covered
	// instead by pkg/layout/css/floats_test.go (TestPlaceStacksThenWraps stacking/wrap
	// geometry) and is visible in the float-figure golden's figure placement.
}

// quadLoader serves a 40x40 four-quadrant PNG at "quad.png" (TL red, TR green, BL
// blue, BR yellow) so a rendered image's orientation is visually unambiguous.
func quadLoader() resource.MapLoader {
	return resource.MapLoader{"quad.png": {Data: quadPNG(40), ContentType: "image/png"}}
}

// quadPNG returns a size×size PNG split into four solid color quadrants. It panics
// on encode failure (encoding a tiny in-memory RGBA never fails in practice); this
// runs only in tests.
func quadPNG(size int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	half := size / 2
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var c color.RGBA
			switch {
			case x < half && y < half:
				c = color.RGBA{220, 50, 50, 255} // top-left red
			case x >= half && y < half:
				c = color.RGBA{50, 180, 50, 255} // top-right green
			case x < half && y >= half:
				c = color.RGBA{50, 80, 220, 255} // bottom-left blue
			default:
				c = color.RGBA{230, 200, 40, 255} // bottom-right yellow
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
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
			opts := []HTMLOption{WithViewportWidth(f.viewportPx)}
			if f.loader != nil {
				opts = append(opts, WithResourceLoader(f.loader))
			}
			doc, err := OpenHTMLBytes([]byte(f.html), opts...)
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
