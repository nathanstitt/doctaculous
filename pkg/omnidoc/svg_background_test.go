package omnidoc

import (
	"fmt"
	"image/color"
	"strings"
	"sync"
	"testing"

	"github.com/nathanstitt/omnidoc/pkg/internal/layout"
	"github.com/nathanstitt/omnidoc/pkg/resource"
)

// bgItemsOf returns every BackgroundImageKind item on the document's first page.
func bgItemsOf(t *testing.T, doc *Document) []layout.BackgroundImageItem {
	t.Helper()
	pages := reflowPagesOf(t, doc)
	if len(pages.Pages) == 0 {
		t.Fatal("no pages")
	}
	var out []layout.BackgroundImageItem
	for _, it := range pages.Pages[0].Items {
		if it.Kind == layout.BackgroundImageKind {
			out = append(out, it.BgImage)
		}
	}
	return out
}

// oneBgItem returns the single background-image item on the first page, failing
// if there is not exactly one.
func oneBgItem(t *testing.T, doc *Document) layout.BackgroundImageItem {
	t.Helper()
	items := bgItemsOf(t, doc)
	if len(items) != 1 {
		t.Fatalf("got %d background-image items on page 0, want 1", len(items))
	}
	return items[0]
}

// recordLogf returns a WithLogf option plus a func returning everything logged.
// Safe for concurrent use because the face cache's OS-font probing may log from
// another goroutine.
func recordLogf() (HTMLOption, func() []string) {
	var (
		mu   sync.Mutex
		msgs []string
	)
	opt := WithLogf(func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		msgs = append(msgs, fmt.Sprintf(format, args...))
	})
	return opt, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), msgs...)
	}
}

// TestSVGBackgroundEmitsVectorNotImageXObject is the background-image sibling of
// TestHTMLImgSVGEmitsVectorNotImageXObject, and carries the same weight: a CSS
// background whose url() names an SVG must reach the PDF as path operators, not
// as a bitmap stamped behind the box.
//
// resolveBackgroundImage used the SAME imageCache as <img>, so before this change
// an SVG background dead-ended exactly where an SVG <img> did — the difference is
// that a dead end there is invisible (the background simply does not paint), which
// is why the negative half of this assertion matters as much as the positive.
func TestSVGBackgroundEmitsVectorNotImageXObject(t *testing.T) {
	t.Parallel()
	raw := htmlToPDF(t,
		`<html><body><div style="width:80px;height:40px;background-image:url(probe.svg);background-repeat:no-repeat"></div></body></html>`,
		WithResourceLoader(svgLoader("probe.svg", vectorProbeSVG)),
	)

	if hasImageXObject(raw) {
		t.Error("an SVG background-image produced an image XObject: it was rasterized. " +
			"It must travel as a layout.VectorScene and emit vector operators.")
	}
	content := pdfStreams(t, raw)
	if !strings.Contains(content, greenFillOp) {
		t.Errorf("content stream has no %q; the SVG background drew nothing at all:\n%s", greenFillOp, content)
	}
	for _, op := range []string{" m\n", " l\n", "h\n", "f\n"} {
		if !strings.Contains(content, op) {
			t.Errorf("content stream missing path operator %q; the SVG background emitted no geometry:\n%s", op, content)
		}
	}
	if strings.Contains(content, " Do\n") {
		t.Errorf("content stream contains an XObject Do operator; the SVG background was stamped as a bitmap:\n%s", content)
	}
}

// TestRasterBackgroundStillEmitsImageXObject is the control for the test above:
// a PNG background MUST still produce an image XObject, so "no image XObject" is
// a falsifiable claim rather than one a dropped-background bug would satisfy.
func TestRasterBackgroundStillEmitsImageXObject(t *testing.T) {
	t.Parallel()
	pngBytes := onePixelSVGPNG(t, greenRGBA)
	raw := htmlToPDF(t,
		`<html><body><div style="width:80px;height:40px;background-image:url(probe.png)"></div></body></html>`,
		WithResourceLoader(resource.MapLoader{"probe.png": {Data: pngBytes, ContentType: "image/png"}}),
	)
	if !hasImageXObject(raw) {
		t.Error("a raster background emitted no image XObject; the control is broken, " +
			"so the vector assertion above proves nothing")
	}
}

// TestSVGBackgroundCarriesSceneNotImage asserts at the ITEM level that the SVG
// background travels as a scene: Img is nil, Scene is set, and SceneW/SceneH are
// the SVG's own authored viewport (which is what lets the painter scale it into
// the computed tile rect).
func TestSVGBackgroundCarriesSceneNotImage(t *testing.T) {
	t.Parallel()
	doc, err := OpenHTMLBytes(
		[]byte(`<html><body style="margin:0"><div style="width:80px;height:40px;background-image:url(probe.svg);background-repeat:no-repeat"></div></body></html>`),
		WithResourceLoader(svgLoader("probe.svg", vectorProbeSVG)),
	)
	if err != nil {
		t.Fatal(err)
	}
	bg := oneBgItem(t, doc)
	if bg.Img != nil {
		t.Error("an SVG background carried an image.Image; it must never be rasterized")
	}
	if bg.Scene == nil {
		t.Fatal("an SVG background carried no VectorScene; nothing would paint")
	}
	if bg.SceneW != 80 || bg.SceneH != 40 {
		t.Errorf("scene viewport = %vx%v, want 80x40 (the SVG's authored size)", bg.SceneW, bg.SceneH)
	}
}

// TestSVGBackgroundSizeUsesIntrinsicRatio pins the reason svg.Document.Intrinsic
// exists on this path too: cover/contain scale by the source's intrinsic RATIO,
// and for the very common viewBox-only SVG the document's own WidthPt/HeightPt
// have already been defaulted to CSS's 300x150 — a 2:1 ratio would come out 2:1
// only by accident. The intrinsic size the item carries is what the painter's
// backgroundTileSize divides by, so asserting on it is asserting on the ratio.
func TestSVGBackgroundSizeUsesIntrinsicRatio(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		svg            string
		wantIW, wantIH float64
	}{
		{
			name:   "explicit size",
			svg:    `<svg xmlns="http://www.w3.org/2000/svg" width="80" height="40"><rect width="80" height="40" fill="#00c000"/></svg>`,
			wantIW: 80, wantIH: 40,
		},
		{
			name: "viewBox only: the RATIO, not the 300x150 default",
			svg: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 50">` +
				`<rect width="200" height="50" fill="#00c000"/></svg>`,
			wantIW: 200, wantIH: 50,
		},
		{
			name: "width plus viewBox: height derived from the ratio",
			svg: `<svg xmlns="http://www.w3.org/2000/svg" width="120" viewBox="0 0 200 50">` +
				`<rect width="200" height="50" fill="#00c000"/></svg>`,
			wantIW: 120, wantIH: 30,
		},
		{
			name:   "neither size nor viewBox: CSS's 300x150 default",
			svg:    `<svg xmlns="http://www.w3.org/2000/svg"><rect width="10" height="10" fill="#00c000"/></svg>`,
			wantIW: 300, wantIH: 150,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := OpenHTMLBytes(
				[]byte(`<html><body style="margin:0"><div style="width:400px;height:200px;`+
					`background-image:url(bg.svg);background-repeat:no-repeat;background-size:cover"></div></body></html>`),
				WithResourceLoader(svgLoader("bg.svg", tc.svg)),
			)
			if err != nil {
				t.Fatal(err)
			}
			bg := oneBgItem(t, doc)
			if bg.SizeKind != layout.BgSizeCover {
				t.Errorf("SizeKind = %v, want BgSizeCover", bg.SizeKind)
			}
			if bg.IntrinsicW != tc.wantIW || bg.IntrinsicH != tc.wantIH {
				t.Errorf("intrinsic = %vx%v, want %vx%v", bg.IntrinsicW, bg.IntrinsicH, tc.wantIW, tc.wantIH)
			}
		})
	}
}

// TestSVGBackgroundContainVsCoverDiffer keeps the test above from being vacuous:
// it proves the intrinsic ratio actually REACHES the painted geometry, by showing
// that contain and cover of the same 4:1 SVG in a 1:1 box produce different tile
// sizes, each derived from that ratio. Asserting only on IntrinsicW/H would pass
// if the painter ignored them entirely.
func TestSVGBackgroundContainVsCoverDiffer(t *testing.T) {
	t.Parallel()
	const wideSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 100">` +
		`<rect width="400" height="100" fill="#00c000"/></svg>`

	sizeFor := func(t *testing.T, keyword string) (w, h float64) {
		t.Helper()
		doc, err := OpenHTMLBytes(
			[]byte(`<html><body style="margin:0"><div style="width:200px;height:200px;`+
				`background-image:url(bg.svg);background-repeat:no-repeat;background-size:`+keyword+`"></div></body></html>`),
			WithResourceLoader(svgLoader("bg.svg", wideSVG)),
		)
		if err != nil {
			t.Fatal(err)
		}
		return tileSizeOf(oneBgItem(t, doc))
	}

	// contain: fit the 4:1 image INSIDE the 200x200 box -> 200x50.
	if w, h := sizeFor(t, "contain"); w != 200 || h != 50 {
		t.Errorf("contain tile = %vx%v, want 200x50", w, h)
	}
	// cover: fill the box -> 800x200 (overflowing horizontally, clipped).
	if w, h := sizeFor(t, "cover"); w != 800 || h != 200 {
		t.Errorf("cover tile = %vx%v, want 800x200", w, h)
	}
}

// TestSVGBackgroundExplicitAndAutoSize covers the other two background-size modes
// on a vector source: an explicit length pair, and the initial `auto` (which uses
// the intrinsic size directly).
func TestSVGBackgroundExplicitAndAutoSize(t *testing.T) {
	t.Parallel()
	const src = `<html><body style="margin:0"><div style="width:200px;height:200px;` +
		`background-image:url(bg.svg);background-repeat:no-repeat;%s"></div></body></html>`

	for _, tc := range []struct {
		name           string
		decl           string
		wantW, wantH   float64
		wantSizeKind   layout.BgSizeKind
		wantSceneScale bool
	}{
		{name: "explicit both axes", decl: "background-size:60px 30px", wantW: 60, wantH: 30, wantSizeKind: layout.BgSizeExplicit},
		{name: "explicit width, auto height", decl: "background-size:160px auto", wantW: 160, wantH: 80, wantSizeKind: layout.BgSizeExplicit},
		{name: "auto (the initial value)", decl: "", wantW: 80, wantH: 40, wantSizeKind: layout.BgSizeAuto},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := OpenHTMLBytes(
				[]byte(fmt.Sprintf(src, tc.decl)),
				WithResourceLoader(svgLoader("bg.svg", vectorProbeSVG)),
			)
			if err != nil {
				t.Fatal(err)
			}
			bg := oneBgItem(t, doc)
			if bg.SizeKind != tc.wantSizeKind {
				t.Errorf("SizeKind = %v, want %v", bg.SizeKind, tc.wantSizeKind)
			}
			if w, h := tileSizeOf(bg); w != tc.wantW || h != tc.wantH {
				t.Errorf("tile = %vx%v, want %vx%v", w, h, tc.wantW, tc.wantH)
			}
		})
	}
}

// TestSVGBackgroundRepeatDegradesToOnePaintAndLogs pins the deferral honestly.
// Tiling a vector background is out of scope (it interacts with the SVG's own
// viewBox mapping), and the requirement is that it degrade VISIBLY: paint once,
// and say so. Both halves are asserted — a silent degradation and a blank box
// are each failures.
func TestSVGBackgroundRepeatDegradesToOnePaintAndLogs(t *testing.T) {
	t.Parallel()
	logOpt, logged := recordLogf()
	doc, err := OpenHTMLBytes(
		[]byte(`<html><body style="margin:0"><div style="width:400px;height:200px;`+
			`background-image:url(bg.svg);background-repeat:repeat"></div></body></html>`),
		WithResourceLoader(svgLoader("bg.svg", vectorProbeSVG)), logOpt,
	)
	if err != nil {
		t.Fatal(err)
	}

	bg := oneBgItem(t, doc)
	if bg.Scene == nil {
		t.Fatal("the SVG background did not paint at all; degrading to a blank box is not acceptable")
	}
	if bg.RepeatX || bg.RepeatY {
		t.Errorf("RepeatX=%v RepeatY=%v; an SVG background must degrade to a single paint", bg.RepeatX, bg.RepeatY)
	}
	if !anyContains(logged(), "background-repeat") {
		t.Errorf("no diagnostic mentioning background-repeat; the degradation was silent. Logged: %q", logged())
	}
}

// TestRasterBackgroundStillTiles is the control for the degradation above: the
// tiling suppression must be specific to the vector path. A raster background
// with the same declaration keeps its repeat flags and logs nothing.
func TestRasterBackgroundStillTiles(t *testing.T) {
	t.Parallel()
	logOpt, logged := recordLogf()
	pngBytes := onePixelSVGPNG(t, greenRGBA)
	doc, err := OpenHTMLBytes(
		[]byte(`<html><body style="margin:0"><div style="width:400px;height:200px;`+
			`background-image:url(bg.png);background-repeat:repeat"></div></body></html>`),
		WithResourceLoader(resource.MapLoader{"bg.png": {Data: pngBytes, ContentType: "image/png"}}), logOpt,
	)
	if err != nil {
		t.Fatal(err)
	}
	bg := oneBgItem(t, doc)
	if !bg.RepeatX || !bg.RepeatY {
		t.Error("a raster background stopped tiling; the SVG degradation leaked onto the raster path")
	}
	if anyContains(logged(), "background-repeat") {
		t.Errorf("a raster tiling background logged the SVG degradation: %q", logged())
	}
}

// TestSVGBackgroundRepeatWarnsOnce proves the diagnostic is a WARN-ONCE: three
// boxes with the same unsupported declaration produce one line, not three. A
// per-box log on a page full of icons would be worse than useless.
func TestSVGBackgroundRepeatWarnsOnce(t *testing.T) {
	t.Parallel()
	logOpt, logged := recordLogf()
	const box = `<div style="width:400px;height:60px;background-image:url(bg.svg);background-repeat:repeat"></div>`
	_, err := OpenHTMLBytes(
		[]byte(`<html><body style="margin:0">`+box+box+box+`</body></html>`),
		WithResourceLoader(svgLoader("bg.svg", vectorProbeSVG)), logOpt,
	)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, m := range logged() {
		if strings.Contains(m, "background-repeat") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("got %d background-repeat diagnostics for 3 identical boxes, want exactly 1: %q", n, logged())
	}
}

// TestBrokenSVGBackgroundDegradesGracefully covers the untrusted-input half: a
// ref whose content type IS image/svg+xml but whose bytes are not parseable must
// not fall through to image.Decode (which would misreport it as an image
// failure), must not panic, and must leave the box's background COLOR painting.
func TestBrokenSVGBackgroundDegradesGracefully(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, body string }{
		{"not xml at all", "this is not markup"},
		{"truncated", `<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><rect`},
		{"empty", ""},
		{"wrong root element", `<html><body>hi</body></html>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := OpenHTMLBytes(
				[]byte(`<html><body style="margin:0"><div style="width:80px;height:40px;`+
					`background-color:#ff0000;background-image:url(bad.svg)"></div></body></html>`),
				WithResourceLoader(svgLoader("bad.svg", tc.body)),
			)
			if err != nil {
				t.Fatalf("a broken SVG background failed the whole layout: %v", err)
			}
			// No background-image item is fine (nothing to paint); an item carrying
			// NEITHER a scene nor an image is also fine. What must not happen is a
			// panic, an error, or a rasterized fallback.
			for _, bg := range bgItemsOf(t, doc) {
				if bg.Img != nil {
					t.Error("a broken SVG background fell through to the raster path")
				}
			}
			// The background COLOR must still reach the page.
			if !hasBackgroundColorItem(t, doc) {
				t.Error("the box's background color stopped painting because its background-image was broken")
			}
		})
	}
}

// hasBackgroundColorItem reports whether the first page carries any solid
// background fill (BackgroundKind).
func hasBackgroundColorItem(t *testing.T, doc *Document) bool {
	t.Helper()
	pages := reflowPagesOf(t, doc)
	if len(pages.Pages) == 0 {
		return false
	}
	for _, it := range pages.Pages[0].Items {
		if it.Kind == layout.BackgroundKind {
			return true
		}
	}
	return false
}

// tileSizeOf resolves an item's painted tile size, the same computation the
// painter runs, so a test can assert on the geometry background-size produced
// rather than on the inputs to it.
func tileSizeOf(bg layout.BackgroundImageItem) (w, h float64) { return bg.TileSize() }

// greenRGBA is the probe SVG's fill, reused for the raster control cases so the
// two paths are compared on identical colour.
var greenRGBA = color.RGBA{R: 0, G: 192, B: 0, A: 255}

// TestInlineSVGUnsupportedSelectorWarns is the end-to-end case that motivates
// the selector diagnostic, and the reason it lands in this PR rather than with
// the selector engine itself: design-tool SVG exports carry their own <style>
// leaning on `[class^="cls-"]` and `.icon > path`, so an inline <svg> in an HTML
// document silently loses those rules and renders in the wrong colours with no
// hint why.
//
// The engine still does not SUPPORT those selectors — that is its own project
// (CLAUDE.md roadmap item 8). What must no longer happen is losing them in
// silence.
func TestInlineSVGUnsupportedSelectorWarns(t *testing.T) {
	t.Parallel()
	logOpt, logged := recordLogf()
	const src = `<html><body><svg xmlns="http://www.w3.org/2000/svg" width="80" height="40">
<style>
  .icon > path { fill: #00c000; }
  [class^="cls-"] { stroke: #000; }
</style>
<g class="icon"><path d="M0 0 L80 40"/></g>
</svg></body></html>`

	if _, err := OpenHTMLBytes([]byte(src), logOpt); err != nil {
		t.Fatal(err)
	}
	msgs := logged()
	for _, want := range []string{"child combinator", "attribute selector"} {
		if !anyContains(msgs, want) {
			t.Errorf("no diagnostic naming %q; an SVG export's rules were dropped silently. Logged: %q",
				want, msgs)
		}
	}
}

// TestOrdinaryDocumentLogsNoSelectorDiagnostic is the negative half at the
// document layer. Every HTML render carries the UA stylesheet and whatever the
// author wrote; if either produced a selector diagnostic, the warning would fire
// on essentially every document and become noise nobody reads.
func TestOrdinaryDocumentLogsNoSelectorDiagnostic(t *testing.T) {
	t.Parallel()
	logOpt, logged := recordLogf()
	const src = `<html><head><style>
  body { margin: 0 }
  h1, h2 { color: #333 }
  div p.lead:first-child { font-weight: bold }
  li:nth-child(2n+1) { background: #eee }
</style></head><body><h1>Title</h1><div><p class="lead">Lead.</p></div>
<ul><li>a</li><li>b</li></ul></body></html>`

	if _, err := OpenHTMLBytes([]byte(src), logOpt); err != nil {
		t.Fatal(err)
	}
	for _, m := range logged() {
		if strings.Contains(m, "is not supported; rules using it are ignored") {
			t.Errorf("an ordinary, fully-supported document logged a selector diagnostic: %q", m)
		}
	}
}

// anyContains reports whether any string in msgs contains sub.
func anyContains(msgs []string, sub string) bool {
	for _, m := range msgs {
		if strings.Contains(m, sub) {
			return true
		}
	}
	return false
}
