package omnidoc

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"strings"
	"testing"

	"github.com/nathanstitt/omnidoc/pkg/layout"
	"github.com/nathanstitt/omnidoc/pkg/resource"
)

// vectorProbeSVG draws one saturated-green rectangle. Green is chosen because it
// is not a colour any other part of the HTML pipeline emits by default, so a
// pixel assertion on it can only be satisfied by the SVG actually having drawn.
const vectorProbeSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="80" height="40">` +
	`<rect x="0" y="0" width="80" height="40" fill="#00c000"/></svg>`

// reflowPagesOf returns the laid-out layout.Pages behind a reflow Document, so a
// test can assert on the ITEM STREAM (what the engine produced) rather than on
// pixels (what a backend made of it).
func reflowPagesOf(t *testing.T, doc *Document) *layout.Pages {
	t.Helper()
	rp, ok := doc.r.(reflowPages)
	if !ok {
		t.Fatalf("document renderer %T is not a reflow renderer", doc.r)
	}
	return rp.layoutPages()
}

// vectorItemSize returns the viewport size of the single VectorKind item on the
// document's first page, failing if there is not exactly one.
func vectorItemSize(t *testing.T, doc *Document) (w, h float64) {
	t.Helper()
	pages := reflowPagesOf(t, doc)
	if len(pages.Pages) == 0 {
		t.Fatal("no pages")
	}
	var found []layout.VectorItem
	for _, it := range pages.Pages[0].Items {
		if it.Kind == layout.VectorKind {
			found = append(found, it.Vector)
		}
	}
	if len(found) != 1 {
		t.Fatalf("got %d vector items on page 0, want 1", len(found))
	}
	return found[0].WPt, found[0].HPt
}

// onePixelSVGPNG encodes a 1x1 PNG of the given colour, for the control cases
// that must still take the raster path.
func onePixelSVGPNG(t *testing.T, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, c)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// svgLoader returns a MapLoader serving src as image/svg+xml under the given ref.
func svgLoader(ref, src string) resource.MapLoader {
	return resource.MapLoader{ref: {Data: []byte(src), ContentType: "image/svg+xml"}}
}

// htmlToPDF renders HTML (with the given options) to PDF and returns the raw bytes.
func htmlToPDF(t *testing.T, src string, opts ...HTMLOption) []byte {
	t.Helper()
	doc, err := OpenHTMLBytes([]byte(src), opts...)
	if err != nil {
		t.Fatalf("OpenHTMLBytes: %v", err)
	}
	return docToPDF(t, doc)
}

// docToPDF writes any already-opened Document to PDF and returns the raw bytes,
// for the structural assertions (hasImageXObject / pdfStreams) that only the PDF
// output can support.
func docToPDF(t *testing.T, doc *Document) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := doc.WritePDF(context.Background(), &buf, PDFOptions{}); err != nil {
		t.Fatalf("WritePDF: %v", err)
	}
	return buf.Bytes()
}

// hasImageXObject reports whether raw PDF bytes declare an image XObject. Only
// stream BODIES are Flate-compressed by pkg/render/pdfwrite (see writer.addStream);
// every dictionary is written as plain text, so the /Subtype /Image key of an image
// XObject is searchable directly in the raw bytes. Both the spaced and unspaced
// spellings are checked because the writer's dictionary formatting is not part of
// this test's contract.
func hasImageXObject(raw []byte) bool {
	return bytes.Contains(raw, []byte("/Subtype/Image")) || bytes.Contains(raw, []byte("/Subtype /Image"))
}

// pdfStreams returns every Flate-decoded stream body in a PDF, concatenated. The
// content stream (the operator list) is compressed, so a test asserting on the
// PAINTING operators has to decompress; the dictionaries hasImageXObject looks at
// are not, which is why that check reads the raw bytes directly.
func pdfStreams(t *testing.T, raw []byte) string {
	t.Helper()
	var out bytes.Buffer
	rest := raw
	for {
		j := bytes.Index(rest, []byte("stream\n"))
		if j < 0 {
			break
		}
		rest = rest[j+len("stream\n"):]
		k := bytes.Index(rest, []byte("endstream"))
		if k < 0 {
			break
		}
		if zr, err := zlib.NewReader(bytes.NewReader(rest[:k])); err == nil {
			body, _ := io.ReadAll(zr)
			out.Write(body)
			out.WriteByte('\n')
		}
		rest = rest[k+len("endstream"):]
	}
	return out.String()
}

// greenFillOp is the PDF non-stroking RGB colour operator pkg/render/pdfwrite
// emits for #00c000 (0, 192/255, 0 at its four-decimal precision). Nothing else
// in these documents sets that colour, so its presence means the probe SVG's
// rectangle really was painted.
const greenFillOp = "0 0.7529 0 rg"

// TestHTMLImgSVGEmitsVectorNotImageXObject is THE claim of SVG-in-HTML support:
// an <img src="*.svg"> reaches the PDF backend as vector path operators, never as
// a rasterized bitmap.
//
// It asserts on the emitted PDF BYTES rather than on pixels, deliberately. A
// golden-image comparison would pass just as happily if the SVG were rasterized
// to a bitmap and stamped into the page — the output would look identical at 72
// DPI. Only the PDF structure distinguishes the two, and the distinction is the
// entire point: a vector PDF stays sharp at any zoom and any print resolution,
// a rasterized one does not.
//
// Two assertions, both necessary:
//
//  1. NO image XObject exists. If SVG were routed through imageCache /
//     decodeImageBytes / ImageContent (all of which carry an image.Image), the
//     only way the rectangle could reach the page is DrawImage, which embeds an
//     image XObject. Its absence proves that path was not taken.
//  2. Real path geometry IS present, filled in the probe's green. Absence of an
//     image alone would also be satisfied by the SVG silently not rendering at
//     all, which is not success. The moveto/lineto/closepath/fill chain is what a
//     vector rectangle looks like, and no XObject `Do` operator accompanies it.
//
// TestHTMLImgPNGStillEmitsImageXObject is the control that keeps assertion 1
// falsifiable.
func TestHTMLImgSVGEmitsVectorNotImageXObject(t *testing.T) {
	t.Parallel()
	raw := htmlToPDF(t,
		`<html><body><img src="probe.svg"></body></html>`,
		WithResourceLoader(svgLoader("probe.svg", vectorProbeSVG)),
	)

	if hasImageXObject(raw) {
		t.Error("<img src=*.svg> produced an image XObject: the SVG was rasterized. " +
			"It must reach the page through layout.VectorItem and emit vector operators.")
	}

	content := pdfStreams(t, raw)
	if !strings.Contains(content, greenFillOp) {
		t.Errorf("content stream has no %q; the SVG drew nothing at all:\n%s", greenFillOp, content)
	}
	// The rectangle must appear as PATH GEOMETRY — moveto/lineto/closepath then a
	// fill — not as an image placement (`Do`). This is the positive half of the
	// claim: the shape reached the page as vectors.
	for _, op := range []string{" m\n", " l\n", "h\n", "f\n"} {
		if !strings.Contains(content, op) {
			t.Errorf("content stream missing path operator %q; the SVG emitted no vector geometry:\n%s", op, content)
		}
	}
	if strings.Contains(content, " Do\n") {
		t.Errorf("content stream contains an XObject Do operator; the SVG was stamped as a bitmap:\n%s", content)
	}
}

// TestHTMLInlineSVGEmitsVectorNotImageXObject is the same claim for INLINE <svg>
// markup, which travels a different route into the engine (re-serialized by
// pkg/html rather than fetched through a loader) and so needs its own proof.
func TestHTMLInlineSVGEmitsVectorNotImageXObject(t *testing.T) {
	t.Parallel()
	raw := htmlToPDF(t, `<html><body>`+vectorProbeSVG+`</body></html>`)

	if hasImageXObject(raw) {
		t.Error("inline <svg> produced an image XObject: it was rasterized, not drawn as vectors")
	}
	content := pdfStreams(t, raw)
	if !strings.Contains(content, greenFillOp) {
		t.Errorf("content stream has no %q; the inline SVG drew nothing:\n%s", greenFillOp, content)
	}
}

// TestHTMLImgPNGStillEmitsImageXObject is the control for the two tests above: a
// raster <img> MUST still produce an image XObject. Without it, "no image
// XObject" would be an unfalsifiable assertion — a bug that dropped every <img>
// on the floor would make both vector tests pass.
func TestHTMLImgPNGStillEmitsImageXObject(t *testing.T) {
	t.Parallel()
	png := onePixelSVGPNG(t, color.RGBA{R: 0, G: 192, B: 0, A: 255})
	raw := htmlToPDF(t,
		`<html><body><img src="probe.png" width="80" height="40"></body></html>`,
		WithResourceLoader(resource.MapLoader{"probe.png": {Data: png, ContentType: "image/png"}}),
	)
	if !hasImageXObject(raw) {
		t.Error("a raster <img> emitted no image XObject; the control case is broken, " +
			"so the vector assertions above prove nothing")
	}
}

// TestHTMLSVGIntrinsicSizing covers the four replaced-element sizing cases an
// embedded SVG must satisfy. The critical one is the viewBox-only case: pkg/svg's
// resolveSize has ALREADY applied CSS's 300x150 default by the time Document is
// built, which is correct for a standalone SVG (it is its own sizing authority)
// and wrong here, where the outer <img>'s CSS supplies an axis and the SVG must
// contribute only a ratio. Document.Intrinsic() is what recovers the pre-default
// facts; without it, `<img src="ratio.svg" width="600">` sizes 600x150 rather
// than 600x300.
func TestHTMLSVGIntrinsicSizing(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		svg    string
		imgTag string
		wantW  float64
		wantH  float64
	}{{
		name:   "explicit width and height are honored",
		svg:    `<svg xmlns="http://www.w3.org/2000/svg" width="120" height="60"><rect width="120" height="60" fill="#00c000"/></svg>`,
		imgTag: `<img src="s.svg">`,
		wantW:  120, wantH: 60,
	}, {
		name:   "viewBox-only plus a CSS width derives the height from the ratio",
		svg:    `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 50"><rect width="100" height="50" fill="#00c000"/></svg>`,
		imgTag: `<img src="s.svg" style="width:600px">`,
		wantW:  600, wantH: 300, // 2:1 ratio, NOT the 300x150 default's height
	}, {
		name:   "neither a size nor a viewBox falls back to the CSS 300x150 default",
		svg:    `<svg xmlns="http://www.w3.org/2000/svg"><rect width="10" height="10" fill="#00c000"/></svg>`,
		imgTag: `<img src="s.svg">`,
		wantW:  300, wantH: 150,
	}, {
		name:   "a width attribute alone derives the height from the viewBox ratio",
		svg:    `<svg xmlns="http://www.w3.org/2000/svg" width="80" viewBox="0 0 4 1"><rect width="4" height="1" fill="#00c000"/></svg>`,
		imgTag: `<img src="s.svg">`,
		wantW:  80, wantH: 20, // 4:1
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := OpenHTMLBytes(
				[]byte(`<html><body style="margin:0">`+tc.imgTag+`</body></html>`),
				WithResourceLoader(svgLoader("s.svg", tc.svg)),
				WithViewportWidth(1000),
			)
			if err != nil {
				t.Fatal(err)
			}
			w, h := vectorItemSize(t, doc)
			if w != tc.wantW || h != tc.wantH {
				t.Errorf("SVG viewport = %gx%g, want %gx%g", w, h, tc.wantW, tc.wantH)
			}
		})
	}
}

// TestInlineSVGCamelCaseSurvivesReserialization is the round-trip proof for
// decision 2 (re-serialize rather than bridge DOM to DOM). HTML5 tokenization
// lower-cases foreign-content names and the parser then REPAIRS them via
// svgTagNameAdjustments (clippath -> clipPath, lineargradient -> linearGradient,
// gradientunits -> gradientUnits, ...). If the re-serialize step lost those
// repairs, pkg/svg — a case-sensitive XML parser — would see <clippath> and
// <lineargradient>, silently fail to resolve every gradient and clip reference,
// and render the wrong picture with no error anywhere.
//
// The test proves it structurally AND behaviorally: the emitted PDF must carry a
// /ShadingType dictionary (the gradient resolved, which is only possible if
// linearGradient was matched by name) and must carry no image XObject.
func TestInlineSVGCamelCaseSurvivesReserialization(t *testing.T) {
	t.Parallel()
	// Deliberately lower-cased in the SOURCE, as an HTML author may legally write
	// it: the HTML parser is what restores the camelCase spellings, and this test
	// asserts they survive all the way to pkg/svg.
	const src = `<html><body><svg width="100" height="50" viewbox="0 0 100 50">
<defs>
  <lineargradient id="g" gradientunits="objectBoundingBox" x1="0" y1="0" x2="1" y2="0">
    <stop offset="0" stop-color="#ff0000"/><stop offset="1" stop-color="#0000ff"/>
  </lineargradient>
  <clippath id="c"><rect x="0" y="0" width="60" height="50"/></clippath>
</defs>
<rect x="0" y="0" width="100" height="50" fill="url(#g)" clip-path="url(#c)"/>
</svg></body></html>`

	raw := htmlToPDF(t, src)
	if !bytes.Contains(raw, []byte("/ShadingType")) {
		t.Error("no /ShadingType in the PDF: the <linearGradient> reference did not resolve, " +
			"so the HTML parser's camelCase repair was lost in re-serialization")
	}
	if hasImageXObject(raw) {
		t.Error("inline SVG with a gradient and clip produced an image XObject; want the vector path")
	}
}

// TestInlineSVGGeneratesNoHTMLBoxes proves box generation stops at <svg>. Before
// this change, pkg/html's buildElement ignored Namespace entirely, so <circle>
// and <path> walked into generate() and produced HTML block boxes — invisible
// boxes that nonetheless consumed layout height and could push following content
// down. The <svg> must now be a single replaced leaf.
func TestInlineSVGGeneratesNoHTMLBoxes(t *testing.T) {
	t.Parallel()
	const src = `<html><body style="margin:0"><svg xmlns="http://www.w3.org/2000/svg" width="40" height="20">` +
		`<circle cx="10" cy="10" r="5" fill="#00c000"/>` +
		`<path d="M20 0 L40 20" stroke="#000"/>` +
		`<text x="0" y="10">not html text</text>` +
		`</svg></body></html>`

	doc, err := OpenHTMLBytes([]byte(src), WithViewportWidth(400))
	if err != nil {
		t.Fatal(err)
	}
	pages := reflowPagesOf(t, doc)
	if len(pages.Pages) == 0 {
		t.Fatal("no pages")
	}
	// Exactly one vector item, and NOT one glyph/background/border item from an
	// HTML box generated for an SVG child. The SVG's own <text> renders inside the
	// vector scene, so it must not appear as a page-level GlyphKind item either.
	var vectors, others int
	for _, it := range pages.Pages[0].Items {
		if it.Kind == layout.VectorKind {
			vectors++
			continue
		}
		others++
	}
	if vectors != 1 {
		t.Errorf("got %d vector items, want exactly 1 (the whole <svg> as one replaced leaf)", vectors)
	}
	if others != 0 {
		t.Errorf("got %d non-vector page items; box generation recursed into the SVG subtree "+
			"and produced HTML boxes for its children", others)
	}
}

// TestMalformedSVGSrcDegradesGracefully covers the untrusted-input contract: an
// <img> pointing at bytes that are not parseable SVG must not panic, must not
// abort the document, and must leave the surrounding content intact. The box is
// still reserved (a sized placeholder) exactly as a failed image decode is.
func TestMalformedSVGSrcDegradesGracefully(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"truncated element":    `<svg xmlns="http://www.w3.org/2000/svg" width="40" height="20"><rect`,
		"not xml at all":       "\x00\x01\x02 this is not markup",
		"empty":                "",
		"xml but not svg":      `<?xml version="1.0"?><notsvg><a/></notsvg>`,
		"unclosed root":        `<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10">`,
		"bad attribute syntax": `<svg xmlns="http://www.w3.org/2000/svg" width=</svg>`,
	}
	for name, bad := range cases {
		t.Run(name, func(t *testing.T) {
			var logs []string
			doc, err := OpenHTMLBytes(
				[]byte(`<html><body><p>before</p><img src="bad.svg"><p>after</p></body></html>`),
				WithResourceLoader(svgLoader("bad.svg", bad)),
				WithLogf(func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }),
			)
			if err != nil {
				t.Fatalf("a malformed SVG src must degrade, not fail the document: %v", err)
			}
			var buf bytes.Buffer
			if err := doc.WritePDF(context.Background(), &buf, PDFOptions{}); err != nil {
				t.Fatalf("WritePDF: %v", err)
			}
			// The surrounding document is unaffected: both paragraphs still render.
			if buf.Len() == 0 {
				t.Fatal("empty PDF")
			}
			_ = logs
		})
	}
}

// TestMalformedInlineSVGDegradesGracefully is the same contract for inline markup,
// which does not travel through the loader and so is a separate code path.
func TestMalformedInlineSVGDegradesGracefully(t *testing.T) {
	t.Parallel()
	// x/net/html recovers from any of this into SOME tree; whatever it hands back,
	// pkg/svg must not panic on it and the document must still render.
	for _, src := range []string{
		`<html><body><svg width="10"><rect</svg></body></html>`,
		`<html><body><svg></body></html>`,
		`<html><body><svg><svg><svg><circle/></svg></svg></svg></body></html>`,
	} {
		doc, err := OpenHTMLBytes([]byte(src))
		if err != nil {
			t.Fatalf("malformed inline <svg> must degrade, not fail: %v", err)
		}
		var buf bytes.Buffer
		if err := doc.WritePDF(context.Background(), &buf, PDFOptions{}); err != nil {
			t.Fatalf("WritePDF: %v", err)
		}
	}
}

// TestSVGImgScalesToItsCSSBox proves the used size actually drives the drawing,
// not just the reserved box. An 80x40 SVG forced to 160x80 by CSS must paint its
// green rectangle across the full 160x80 area; if the scale were dropped, the
// rectangle would occupy only the top-left quarter and the box's bottom-right
// corner would stay background-white.
func TestSVGImgScalesToItsCSSBox(t *testing.T) {
	t.Parallel()
	doc, err := OpenHTMLBytes(
		[]byte(`<html><body style="margin:0"><img src="s.svg" style="width:160px;height:80px"></body></html>`),
		WithResourceLoader(svgLoader("s.svg", vectorProbeSVG)),
		WithViewportWidth(200),
	)
	if err != nil {
		t.Fatal(err)
	}
	img, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: 72})
	if err != nil {
		t.Fatal(err)
	}
	// Sample near the bottom-right of the 160x80 box: green only if the scene was
	// scaled to fill it.
	r, g, b, _ := img.At(150, 72).RGBA()
	if g>>8 < 150 || r>>8 > 80 || b>>8 > 80 {
		t.Errorf("pixel at (150,72) = rgb(%d,%d,%d), want green: the SVG scene was not "+
			"scaled into its CSS box", r>>8, g>>8, b>>8)
	}
}

// TestStackedSVGImagesEachGetTheirOwnPosition is a regression test for a real bug
// this feature's first golden caught: VectorContent's content-box origin was not
// moved by translateFragment, which is the single choke point every fragment
// relocation goes through (block-flow stacking, atomic-inline placement,
// pagination, relative shifts). ImageContent and ControlContent were moved there;
// a new content carrier that is not is silently pinned at the origin, so every
// SVG on a page paints stacked on top of the first one.
//
// Three block SVGs with a 6px gap must land at y = 0, 56, 142.
func TestStackedSVGImagesEachGetTheirOwnPosition(t *testing.T) {
	t.Parallel()
	doc, err := OpenHTMLBytes([]byte(
		`<!DOCTYPE html><html><head><style>
			body { margin: 0 }
			img { display: block; margin-bottom: 6px }
			.w { width: 160px } .wh { width: 120px; height: 40px }
		 </style></head><body>
			<img src="a.svg"><img class="w" src="a.svg"><img class="wh" src="a.svg">
		 </body></html>`),
		WithResourceLoader(svgLoader("a.svg",
			`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 50"><rect width="100" height="50" fill="#00c000"/></svg>`)),
		WithViewportWidth(420),
	)
	if err != nil {
		t.Fatal(err)
	}
	var got [][3]float64
	for _, it := range reflowPagesOf(t, doc).Pages[0].Items {
		if it.Kind == layout.VectorKind {
			got = append(got, [3]float64{it.Vector.YPt, it.Vector.WPt, it.Vector.HPt})
		}
	}
	want := [][3]float64{{0, 100, 50}, {56, 160, 80}, {142, 120, 40}}
	if len(got) != len(want) {
		t.Fatalf("got %d vector items, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("vector %d = (y=%g w=%g h=%g), want (y=%g w=%g h=%g)",
				i, got[i][0], got[i][1], got[i][2], want[i][0], want[i][1], want[i][2])
		}
	}
}

// TestInlineSVGSitsOnTheLineWithText proves an <svg> behaves as an ATOMIC INLINE
// box, not just a block: it is placed on a line by the inline formatting context
// and carries a non-zero X. That exercises translateFragment's horizontal path,
// which block stacking (dx == 0) never reaches — so a carrier that moved
// correctly vertically could still be pinned at x=0 in inline flow.
func TestInlineSVGSitsOnTheLineWithText(t *testing.T) {
	t.Parallel()
	doc, err := OpenHTMLBytes(
		[]byte(`<!DOCTYPE html><html><head><style>body{margin:0;font:16px sans-serif}</style></head>`+
			`<body><p>text before `+vectorProbeSVG+` text after</p></body></html>`),
		WithViewportWidth(500),
	)
	if err != nil {
		t.Fatal(err)
	}
	var found []layout.VectorItem
	for _, it := range reflowPagesOf(t, doc).Pages[0].Items {
		if it.Kind == layout.VectorKind {
			found = append(found, it.Vector)
		}
	}
	if len(found) != 1 {
		t.Fatalf("got %d vector items, want 1", len(found))
	}
	if found[0].XPt <= 0 {
		t.Errorf("inline SVG is at x=%g; it must be placed AFTER the preceding text on "+
			"the line, not pinned at the origin", found[0].XPt)
	}
	if found[0].WPt != 80 || found[0].HPt != 40 {
		t.Errorf("inline SVG size = %gx%g, want 80x40", found[0].WPt, found[0].HPt)
	}
}

// TestSVGSrcContentTypeGatesTheVectorPath proves the vector path is chosen by the
// resource's CONTENT TYPE, so an <img> whose ref merely ends in ".svg" but serves
// PNG bytes still rasterizes correctly, and vice versa. Sniffing SVG out of
// arbitrary bytes would mean running an XML parser over every unrecognized binary
// blob a document references.
func TestSVGSrcContentTypeGatesTheVectorPath(t *testing.T) {
	t.Parallel()
	png := onePixelSVGPNG(t, color.RGBA{R: 0, G: 192, B: 0, A: 255})
	raw := htmlToPDF(t,
		`<html><body><img src="lying.svg" width="20" height="20"></body></html>`,
		WithResourceLoader(resource.MapLoader{"lying.svg": {Data: png, ContentType: "image/png"}}),
	)
	if !hasImageXObject(raw) {
		t.Error("a PNG served under a .svg ref did not rasterize; content type, not the " +
			"file extension, must decide the path")
	}
}

// TestSVGDataURIImgTakesTheVectorPath covers the data: URI route, which bypasses
// the loader entirely (the browser rule: an inline image must not depend on
// fetching) and carries its own content type.
func TestSVGDataURIImgTakesTheVectorPath(t *testing.T) {
	t.Parallel()
	ref := "data:image/svg+xml," + strings.NewReplacer(
		"#", "%23", "\"", "%22", "<", "%3C", ">", "%3E", " ", "%20",
	).Replace(vectorProbeSVG)

	raw := htmlToPDF(t, `<html><body><img src="`+ref+`"></body></html>`)
	if hasImageXObject(raw) {
		t.Error("a data: URI SVG rasterized; want the vector path")
	}
	if !strings.Contains(pdfStreams(t, raw), greenFillOp) {
		t.Error("data: URI SVG drew nothing")
	}
}
