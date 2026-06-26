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
	{
		// position:relative shifts a box at paint time WITHOUT moving its neighbors:
		// three stacked block boxes, the middle (green) one relatively offset
		// down+right so it overlaps the blue box below and paints ON TOP of it
		// (positioned content paints after in-flow content). The red and blue boxes
		// hold their in-flow column positions; the blue box does NOT slide up into the
		// green box's vacated row (relative reserves its in-flow space). Block-level
		// boxes are used deliberately: relative offset on an inline-block atom is a
		// documented no-op in this slice, so a block fixture is what exercises it.
		name:       "position-relative",
		viewportPx: 240,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .box { width: 90px; height: 45px; }
  .a { background: #cc3333; }
  .b { background: #33aa33; position: relative; top: 18px; left: 70px; }
  .c { background: #3355cc; }
</style></head><body>
  <div class="box a"></div><div class="box b"></div><div class="box c"></div>
</body></html>`,
	},
	{
		// position:absolute pins a child to a corner of its relatively-positioned
		// container, painted ABOVE the container's own content. Eyeball: the small
		// box sits at the container's top-right corner, on top of the container's
		// background/text.
		name:       "position-absolute",
		viewportPx: 240,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .box { position: relative; width: 200px; height: 120px; background: #dddddd; }
  .pin { position: absolute; top: 0; right: 0; width: 40px; height: 40px; background: #cc3333; }
</style></head><body>
  <div class="box">Container text<div class="pin"></div></div>
</body></html>`,
	},
	{
		// overflow:hidden clips an oversized child to the box's padding box. A 120x70
		// box with a 12px border and overflow:hidden contains a child that is far taller
		// and wider; eyeball that the child (green) is cut at the padding-box edge while
		// the box's own border (navy) paints at full size around it.
		name:       "overflow-hidden",
		viewportPx: 200,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .clip { width: 120px; height: 70px; border: 12px solid #002255; overflow: hidden; }
  .big { width: 300px; height: 300px; background: #33aa33; }
</style></head><body>
  <div class="clip"><div class="big"></div></div>
</body></html>`,
	},
	{
		// Float-height enclosure (the overflow:hidden "clearfix"): three left-floated
		// swatches inside an overflow:hidden wrapper. Eyeball that the wrapper has real
		// height (encloses the floats) and shows the three swatches in a row — the case
		// 5a had to drop because a non-BFC float-only body collapsed to a 1x1 page.
		name:       "float-row",
		viewportPx: 240,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .wrap { overflow: hidden; background: #eeeeee; }
  .sw { float: left; width: 60px; height: 60px; }
  .a { background: #cc3333; }
  .b { background: #33aa33; }
  .c { background: #3355cc; }
</style></head><body>
  <div class="wrap"><div class="sw a"></div><div class="sw b"></div><div class="sw c"></div></div>
</body></html>`,
	},
	{
		// Negative z-index: a box with z-index:-1 paints BEHIND in-flow content. The
		// in-flow green block overlaps the (red) negative box; green must cover red.
		name:       "zindex-negative",
		viewportPx: 200,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .neg { position: relative; z-index: -1; width: 120px; height: 120px; background: #cc2222; }
  .flow { width: 120px; height: 60px; background: #22aa22; margin-top: -60px; }
</style></head><body>
  <div class="neg"></div>
  <div class="flow"></div>
</body></html>`,
	},
	{
		// Positive z-index ordering: three overlapping absolutely-positioned boxes with
		// z-index 1/2/3; the higher z paints on top. Blue(3) over green(2) over red(1).
		name:       "zindex-stack",
		viewportPx: 200,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .wrap { position: relative; height: 160px; }
  .box { position: absolute; width: 90px; height: 90px; }
  .r { left: 10px;  top: 10px;  background: #cc2222; z-index: 1; }
  .g { left: 40px;  top: 40px;  background: #22aa22; z-index: 2; }
  .b { left: 70px;  top: 70px;  background: #2244cc; z-index: 3; }
</style></head><body>
  <div class="wrap">
    <div class="box r"></div>
    <div class="box g"></div>
    <div class="box b"></div>
  </div>
</body></html>`,
	},
	{
		// z-index inside a clip: an absolutely-positioned z-index box whose containing
		// block is an overflow:hidden box is clipped to that box AND ordered by z against
		// the clip's other content. The orange box spills past the clip edge but is cut.
		name:       "zindex-clip",
		viewportPx: 200,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .clip { position: relative; overflow: hidden; width: 100px; height: 100px; background: #dddddd; }
  .under { position: absolute; left: 10px; top: 10px; width: 80px; height: 80px; background: #2244cc; z-index: 1; }
  .over  { position: absolute; left: 40px; top: 40px; width: 120px; height: 120px; background: #ee8822; z-index: 2; }
</style></head><body>
  <div class="clip">
    <div class="under"></div>
    <div class="over"></div>
  </div>
</body></html>`,
	},
	{
		// Sub-case B (positioned-clip-box relative escape): a position:relative child of
		// a position:relative + overflow:hidden box, offset down+right past the box edge.
		// Because the box is BOTH the child's containing block (it is in flow within it)
		// AND a stacking context that consumes the relative child, the child is clipped to
		// the box's padding box. Eyeball: the green child is CUT at the gray box's
		// bottom-right edge — it does NOT spill outside (an unclipped render would show the
		// green overhanging the box). The navy border marks the box's full extent.
		name:       "clip-relative-escape",
		viewportPx: 200,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .clip { position: relative; overflow: hidden; width: 90px; height: 90px;
          border: 4px solid #002255; background: #dddddd; }
  .child { position: relative; left: 40px; top: 40px; width: 80px; height: 80px; background: #33aa33; }
</style></head><body>
  <div class="clip"><div class="child"></div></div>
</body></html>`,
	},
	{
		// z-index ∘ float: a left float (step 4) and a positive-z positioned box (step 7)
		// overlap; the positioned box paints OVER the float per Appendix E.
		name:       "zindex-float",
		viewportPx: 200,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .wrap { position: relative; height: 140px; }
  .fl { float: left; width: 90px; height: 90px; background: #22aa22; }
  .ov { position: absolute; left: 40px; top: 30px; width: 90px; height: 90px; background: #cc2222; z-index: 1; }
</style></head><body>
  <div class="wrap">
    <div class="fl"></div>
    <div class="ov"></div>
  </div>
</body></html>`,
	},
	{
		// A 2x3 table with per-cell borders + alternating row backgrounds (separate
		// borders, default border-spacing). Eyeball: a clean grid, gaps between cells.
		name:       "table-basic",
		viewportPx: 240,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  table { border-spacing: 4px; }
  td { border: 2px solid #335; padding: 6px; background: #dde; }
  tr:nth-child(2) td { background: #cce; }
</style></head><body>
  <table>
    <tr><td>R1C1</td><td>R1C2</td><td>R1C3</td></tr>
    <tr><td>R2C1</td><td>R2C2</td><td>R2C3</td></tr>
  </table>
</body></html>`,
	},
	{
		// A header cell spanning two columns over a 2-column body. Eyeball: the header
		// stretches across both columns; the body cells sit beneath each half.
		name:       "table-colspan",
		viewportPx: 240,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  table { border-spacing: 0; }
  td, th { border: 1px solid #444; padding: 6px; }
  th { background: #ccd; }
</style></head><body>
  <table>
    <tr><th colspan="2">Header</th></tr>
    <tr><td>A</td><td>B</td></tr>
  </table>
</body></html>`,
	},
	{
		// Auto layout: columns sized by their content (a short and a long column).
		// Eyeball: the long-text column is visibly wider than the short one.
		name:       "table-auto",
		viewportPx: 300,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  table { border-spacing: 0; }
  td { border: 1px solid #555; padding: 4px; }
</style></head><body>
  <table>
    <tr><td>Hi</td><td>A considerably longer cell of content</td></tr>
    <tr><td>Yo</td><td>Short</td></tr>
  </table>
</body></html>`,
	},
	{
		// border-collapse:collapse: shared single edges between cells. Eyeball: no gaps,
		// single (not doubled) lines between cells, the wider border winning at shared edges.
		name:       "table-collapse",
		viewportPx: 240,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  table { border-collapse: collapse; }
  td { border: 2px solid #336; padding: 6px; }
  td.thick { border: 5px solid #933; }
</style></head><body>
  <table>
    <tr><td>A</td><td class="thick">B</td></tr>
    <tr><td>C</td><td>D</td></tr>
  </table>
</body></html>`,
	},
	{
		// A captioned table (caption-side:top). Eyeball: the caption sits above the grid.
		name:       "table-caption",
		viewportPx: 240,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  table { border-spacing: 0; }
  caption { font-weight: bold; padding: 4px; }
  td { border: 1px solid #444; padding: 6px; }
</style></head><body>
  <table>
    <caption>Quarterly Results</caption>
    <tr><td>Q1</td><td>Q2</td></tr>
  </table>
</body></html>`,
	},
	{
		// Web font: text rendered with an @font-face family served from memory. The
		// Pacifico glyphs are visibly distinct from the base-14 substitutes, proving
		// the downloaded face is used (not LoadStandard). The WOFF2 source exercises
		// the full Brotli + glyf-transform decode path.
		name:       "webfont",
		viewportPx: 360,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  @font-face { font-family: "Web Face"; src: url(web.woff2) format("woff2"); }
  p { font-family: "Web Face", sans-serif; font-size: 48px; color: #202020; }
</style></head><body>
  <p>Web Font AaGg</p>
</body></html>`,
		loader: webfontGoldenLoader(),
	},
}

// webfontGoldenLoader serves the committed Pacifico WOFF2 fixture as web.woff2 for
// the web-font golden. It panics on a missing fixture (a test-setup error). The
// WOFF2 exercises the full decode path (Brotli + glyf transform).
func webfontGoldenLoader() resource.ResourceLoader {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fonts", "webfont.woff2"))
	if err != nil {
		panic("webfont golden fixture: " + err.Error())
	}
	return resource.MapLoader{"web.woff2": {Data: data}}
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
