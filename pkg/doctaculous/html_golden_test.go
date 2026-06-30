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
		// Negative z-index behind the HOST's own background (CSS 2.1 Appendix E: a box
		// paints its own background first, then its negative-z descendants). The teal host
		// has a background; its z-index:-1 red child is offset down-right so half of it
		// sits OVER the host background region and half spills below. Eyeball: the red
		// child shows through everywhere it is NOT covered by a later-painted box — i.e.
		// the host's own teal background does NOT hide it (the host bg paints behind it),
		// but the in-flow yellow block (painted after negatives) DOES cover the part of
		// red beneath the yellow. Pre-fix, the teal host background painted over the red.
		name:       "zindex-neg-behind-own-bg",
		viewportPx: 200,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .host { position: relative; width: 120px; height: 120px; background: #2aa0a0; }
  .neg { position: relative; z-index: -1; top: 60px; left: 60px; width: 100px; height: 100px; background: #cc2222; }
  .flow { width: 60px; height: 60px; background: #d0d000; }
</style></head><body>
  <div class="host"><div class="neg"></div><div class="flow"></div></div>
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
		// flex justify-content:space-between: three fixed-width blue boxes across a
		// 320px row, first flush-left, last flush-right, equal gaps between them.
		name:       "flex-justify-between",
		viewportPx: 320,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .row { display: flex; justify-content: space-between; }
  .box { width: 60px; height: 40px; background: #4477aa; }
</style></head><body>
  <div class="row"><div class="box"></div><div class="box"></div><div class="box"></div></div>
</body></html>`,
	},
	{
		// flex-grow: two bars filling the full row, green (flex:2) twice as wide as
		// red (flex:1). Both start at flex-basis:0 so all free space is distributed.
		name:       "flex-grow",
		viewportPx: 320,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .row { display: flex; }
  .a { flex: 1 1 0; height: 40px; background: #aa4444; }
  .b { flex: 2 1 0; height: 40px; background: #44aa44; }
</style></head><body>
  <div class="row"><div class="a"></div><div class="b"></div></div>
</body></html>`,
	},
	{
		// align-items:center: a short (30px) blue box and a tall (70px) orange box
		// both vertically centered in a 100px grey row. Eyeball: both are centred in
		// the strip, gaps above short box equal gaps below it.
		name:       "flex-align-center",
		viewportPx: 320,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .row { display: flex; align-items: center; height: 100px; background: #eeeeee; }
  .s { width: 50px; height: 30px; background: #4477aa; }
  .t { width: 50px; height: 70px; background: #aa7744; }
</style></head><body>
  <div class="row"><div class="s"></div><div class="t"></div></div>
</body></html>`,
	},
	{
		// flex-direction:column: two boxes stacked vertically (purple then teal),
		// the second narrower than the first. Eyeball: purple on top, teal below,
		// no horizontal spreading.
		name:       "flex-column",
		viewportPx: 200,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .col { display: flex; flex-direction: column; }
  .box { width: 120px; height: 40px; background: #6644aa; }
  .box2 { width: 80px; height: 60px; background: #44aa88; }
</style></head><body>
  <div class="col"><div class="box"></div><div class="box2"></div></div>
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
	{
		// grid-2x2: a 2x2 explicit grid with four distinct colored cells at fixed
		// 100x60 px tracks (gap:0). Eyeball: red top-left, green top-right, blue
		// bottom-left, orange bottom-right — a clean 2x2 mosaic.
		name:       "grid-2x2",
		viewportPx: 240,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .grid {
    display: grid;
    grid-template-columns: 100px 100px;
    grid-template-rows: 60px 60px;
    gap: 0;
  }
  .a { background: #cc3333; }
  .b { background: #33aa33; }
  .c { background: #3355cc; }
  .d { background: #cc8822; }
</style></head><body>
  <div class="grid">
    <div class="a"></div>
    <div class="b"></div>
    <div class="c"></div>
    <div class="d"></div>
  </div>
</body></html>`,
	},
	{
		// grid-fr: a 2-column fr grid (1fr 2fr) at viewport 300. The second column
		// is exactly twice as wide as the first (100px vs 200px). Eyeball: blue
		// narrow left, teal wide right.
		name:       "grid-fr",
		viewportPx: 300,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .grid {
    display: grid;
    grid-template-columns: 1fr 2fr;
    grid-template-rows: 80px;
  }
  .a { background: #3355cc; }
  .b { background: #33aa88; }
</style></head><body>
  <div class="grid">
    <div class="a"></div>
    <div class="b"></div>
  </div>
</body></html>`,
	},
	{
		// grid-span: a 3-column grid (80px each) where the first item spans two
		// columns (grid-column:1/3), taking up the full top row width of 160px,
		// with two regular cells below it. Eyeball: purple bar spanning columns 1-2,
		// then green in col 1 and red in col 2 on the second row, with an empty
		// cell in col 3 row 1 (orange).
		name:       "grid-span",
		viewportPx: 280,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .grid {
    display: grid;
    grid-template-columns: 80px 80px 80px;
    grid-template-rows: 50px 50px;
  }
  .span2 { grid-column: 1 / 3; background: #7744cc; }
  .c3r1  { background: #cc8822; }
  .c1r2  { background: #33aa33; }
  .c2r2  { background: #cc3333; }
</style></head><body>
  <div class="grid">
    <div class="span2"></div>
    <div class="c3r1"></div>
    <div class="c1r2"></div>
    <div class="c2r2"></div>
  </div>
</body></html>`,
	},
	{
		// grid-areas: a named-area layout with a header spanning both columns and
		// two cells (main + side) below. Eyeball: blue header bar across full width,
		// green main area on the left, orange side on the right.
		name:       "grid-areas",
		viewportPx: 240,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .grid {
    display: grid;
    grid-template-columns: 160px 80px;
    grid-template-rows: 40px 80px;
    grid-template-areas: "hd hd" "main side";
  }
  .hd   { grid-area: hd;   background: #3355cc; }
  .main { grid-area: main; background: #33aa33; }
  .side { grid-area: side; background: #cc8822; }
</style></head><body>
  <div class="grid">
    <div class="hd"></div>
    <div class="main"></div>
    <div class="side"></div>
  </div>
</body></html>`,
	},
	{
		// grid-auto: a 2-column auto-placement grid with 5 colored cells. The first
		// four fill two rows; the fifth wraps to a third row alone in column 1.
		// Eyeball: 4 cells in a 2x2 block (red, green, blue, orange), then purple
		// alone in the leftmost column of row 3.
		name:       "grid-auto",
		viewportPx: 240,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .grid {
    display: grid;
    grid-template-columns: 100px 100px;
    grid-auto-rows: 50px;
  }
  .a { background: #cc3333; }
  .b { background: #33aa33; }
  .c { background: #3355cc; }
  .d { background: #cc8822; }
  .e { background: #7744cc; }
</style></head><body>
  <div class="grid">
    <div class="a"></div>
    <div class="b"></div>
    <div class="c"></div>
    <div class="d"></div>
    <div class="e"></div>
  </div>
</body></html>`,
	},
	{
		// An auto-width inline-block WITH text inside a line of text. Exercises four
		// inline/font fidelity fixes together: B2 (the highlighted "BOX" sits on the SAME
		// baseline as "before"/"after", not dropped below), E4 (the inline-block
		// SHRINKS TO FIT "BOX" instead of filling the line), E5 (the line is a sane height,
		// not ~2× inflated), and E6 (the `font: 20px monospace` shorthand applies the size +
		// monospace family). Eyeball: "before BOX after" on one line, the yellow box hugging
		// "BOX", everything on one baseline.
		name:       "inline-block-baseline",
		viewportPx: 300,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; font: 20px monospace; }
  .ib { display: inline-block; background: #ffdd55; }
</style></head><body>
  <p>before <span class="ib">BOX</span> after</p>
</body></html>`,
	},
	{
		// Absolute-positioning fidelity (C2 shrink-to-fit + C3 margin:auto centering).
		// Eyeball: box A (left:0, width:auto) hugs its short text at the top-left (NOT
		// stretched to the container); box B (left:0;right:0;width:60;margin:auto) is
		// horizontally CENTERED in the 200px container.
		name:       "abs-fidelity",
		viewportPx: 240,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .cb { position: relative; width: 200px; height: 90px; background: #eeeeee; }
  .shrink { position: absolute; left: 0; top: 0; background: #cc7777; color: #fff; }
  .center { position: absolute; left: 0; right: 0; top: 40px; width: 60px;
            margin-left: auto; margin-right: auto; height: 24px; background: #7777cc; }
</style></head><body>
  <div class="cb">
    <div class="shrink">Hi</div>
    <div class="center"></div>
  </div>
</body></html>`,
	},
	{
		// object-position (CSS, fidelity fix D1): a small square image inside a larger box
		// with object-fit:none, positioned at three corners. Eyeball: the gray image sits
		// at top-left, center, and bottom-right of its three boxes respectively (the box
		// background is light blue so the free space is visible).
		name:       "object-position",
		viewportPx: 260,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  img { width: 70px; height: 70px; object-fit: none; background: #cce0ff; margin: 6px; display: inline-block; }
  .tl { object-position: left top; }
  .ctr { object-position: center; }
  .br { object-position: right bottom; }
</style></head><body>
  <img class="tl" src="quad.png"><img class="ctr" src="quad.png"><img class="br" src="quad.png">
</body></html>`,
		loader: quadLoader(),
	},
	{
		// Table background LAYERS (CSS 17.5.1, fidelity fix F2): a column background and
		// row backgrounds paint behind the cells. Eyeball: column 1 has a blue tint behind
		// all its cells; rows 1 and 3 have an orange tint; the cell text sits on top; where
		// a tinted row crosses the tinted column, the ROW wins (rows paint after columns).
		name:       "table-bg-layers",
		viewportPx: 240,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  table { border-collapse: separate; border-spacing: 0; }
  td { width: 50px; height: 22px; padding: 2px; }
  col.hi { background: #aaccff; }
  tr.stripe { background: #ffcc99; }
</style></head><body>
  <table>
    <colgroup><col><col class="hi"><col></colgroup>
    <tr class="stripe"><td>a1</td><td>b1</td><td>c1</td></tr>
    <tr><td>a2</td><td>b2</td><td>c2</td></tr>
    <tr class="stripe"><td>a3</td><td>b3</td><td>c3</td></tr>
  </table>
</body></html>`,
	},
	{
		// The four 3D border styles (CSS, fidelity fix F5). Each box has a thick gray
		// border in one 3D style. Eyeball the bevel: outset = raised (light top/left, dark
		// bottom/right), inset = sunken (inverse), ridge = a raised ridge (outer light /
		// inner dark on top-left), groove = a carved groove (inverse of ridge). Before the
		// fix these all painted as flat solid (or nothing for non-collapse borders).
		name:       "border-3d-styles",
		viewportPx: 240,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  div { width: 80px; height: 30px; margin: 8px; border: 12px gray; background: #dddddd; }
  .outset { border-style: outset; }
  .inset  { border-style: inset; }
  .ridge  { border-style: ridge; }
  .groove { border-style: groove; }
</style></head><body>
  <div class="outset"></div>
  <div class="inset"></div>
  <div class="ridge"></div>
  <div class="groove"></div>
</body></html>`,
	},
	{
		// Form controls painted as static native widgets: text/password fields, a
		// checked + unchecked checkbox and radio, two buttons, a textarea, and a select.
		// A disabled field shows the muted chrome. The page background is light gray so
		// the white field faces are visible. Eyeball: each control sits in its own row,
		// values/labels render, the password shows bullets, the checked checkbox shows a
		// tick and the checked radio a dot, the select shows "Two" + a triangle, and
		// nothing renders as leaked inline text.
		name:       "forms",
		viewportPx: 300,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 8px; font-family: sans-serif; background: #f0f0f0; }
  div { margin: 6px 0; }
</style></head><body>
  <div><input type="text" value="typed text"></div>
  <div><input type="text" placeholder="placeholder"></div>
  <div><input type="password" value="secret"></div>
  <div>chk <input type="checkbox" checked> <input type="checkbox"></div>
  <div>rad <input type="radio" checked> <input type="radio"></div>
  <div><button>Click Me</button> <input type="submit" value="Submit"></div>
  <div><textarea>two
line area</textarea></div>
  <div><select><option>One</option><option selected>Two</option></select></div>
  <div><input type="text" value="disabled" disabled></div>
</body></html>`,
	},
	{
		// background-image tiling: the quad PNG repeats across a tall box (background
		// color shows nowhere since the tile fully covers). Eyeball: a regular grid of
		// the four-quadrant tile, top-left aligned.
		name:       "bg-tiled",
		viewportPx: 200,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .panel { height: 160px; background: #eee url(quad.png); }
</style></head><body><div class="panel"></div></body></html>`,
		loader: quadLoader(),
	},
	{
		// background-size: cover, no-repeat: the 40x40 tile scales (preserving ratio) to
		// cover the 200x120 box, clipped to it. Eyeball: one enlarged quad filling the
		// box, the four colors in their corners (cover crops the longer axis).
		name:       "bg-cover",
		viewportPx: 200,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .hero { height: 120px; background: url(quad.png) no-repeat center / cover; }
</style></head><body><div class="hero"></div></body></html>`,
		loader: quadLoader(),
	},
	{
		// background-position bottom-right, no-repeat, over a colored box. Eyeball: a
		// single quad tile in the bottom-right corner; the rest is the #cde fill.
		name:       "bg-position",
		viewportPx: 200,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  .badge { height: 120px; background: #cde url(quad.png) no-repeat bottom right; }
</style></head><body><div class="badge"></div></body></html>`,
		loader: quadLoader(),
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
