package doctaculous

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"image"
	"image/color"
	"testing"
	"time"
)

func TestOpenSVGBytes(t *testing.T) {
	src := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="100" height="50">
	  <rect x="10" y="10" width="80" height="30" fill="#0000ff"/>
	</svg>`)
	doc, err := OpenSVGBytes(src)
	if err != nil {
		t.Fatal(err)
	}
	if doc.PageCount() != 1 {
		t.Fatalf("pages = %d", doc.PageCount())
	}
	w, h, err := doc.PageSize(0)
	if err != nil || w != 100 || h != 50 {
		t.Fatalf("size = %gx%g %v", w, h, err)
	}
	img, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: 72})
	if err != nil {
		t.Fatal(err)
	}
	rgba := img.(*image.RGBA)
	if got := rgba.RGBAAt(50, 25); got != (color.RGBA{0, 0, 255, 255}) {
		t.Errorf("center = %+v, want blue", got)
	}
	if got := rgba.RGBAAt(5, 5); got != (color.RGBA{255, 255, 255, 255}) {
		t.Errorf("corner = %+v, want white", got)
	}

	// svgz: same doc gzipped.
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(src); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	doc, err = OpenSVGBytes(buf.Bytes())
	if err != nil || doc.PageCount() != 1 {
		t.Fatalf("svgz: %v", err)
	}

	// Malformed XML with no root errors cleanly.
	if _, err := OpenSVGBytes([]byte("not xml at all")); err == nil {
		t.Error("garbage accepted")
	}
}

// TestSVGPDFRoundTrip proves vectors survive into PDF output: SVG -> PDF ->
// raster matches SVG -> raster within the golden tolerance.
func TestSVGPDFRoundTrip(t *testing.T) {
	src := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="120" height="120">
	  <circle cx="60" cy="60" r="40" fill="teal" stroke="black" stroke-width="4"/>
	  <path d="M 20 100 Q 60 20 100 100" fill="none" stroke="red" stroke-width="3"/>
	</svg>`)
	doc, err := OpenSVGBytes(src)
	if err != nil {
		t.Fatal(err)
	}
	direct, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: 96})
	if err != nil {
		t.Fatal(err)
	}
	var pdfBuf bytes.Buffer
	if err := doc.WritePDF(context.Background(), &pdfBuf, PDFOptions{}); err != nil {
		t.Fatal(err)
	}
	re, err := OpenBytes(pdfBuf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if re.Format() != FormatPDF || re.PageCount() != 1 {
		t.Fatalf("round-trip format/pages: %v/%d", re.Format(), re.PageCount())
	}
	viaPDF, err := re.RasterizePage(context.Background(), 0, RasterOptions{DPI: 96})
	if err != nil {
		t.Fatal(err)
	}
	if diff, n := compareImages(direct.(*image.RGBA), viaPDF.(*image.RGBA)); diff {
		t.Errorf("PDF round-trip drifted: %d pixels beyond tolerance", n)
	}
}

// TestSVGGradientPDFRoundTrip proves an SVG linear gradient survives into PDF
// output: FillShading used to be a no-op stub, so a gradient fill converted to
// PDF rendered as nothing (worse than an honest "no fill", since it silently
// swallowed the paint). An opaque, pad-spread gradient like this one now emits
// a native /Shading dictionary (see TestSVGOpaqueGradientEmitsNativeShading for
// the structural assertion); either way the reopened PDF's raster shows the
// gradient ramp: reddish on the left, blueish on the right.
func TestSVGGradientPDFRoundTrip(t *testing.T) {
	src := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="100" height="50">
	  <defs>
	    <linearGradient id="g1" x1="0" y1="0" x2="1" y2="0">
	      <stop offset="0" stop-color="red"/>
	      <stop offset="1" stop-color="blue"/>
	    </linearGradient>
	  </defs>
	  <rect x="0" y="0" width="100" height="50" fill="url(#g1)"/>
	</svg>`)
	doc, err := OpenSVGBytes(src)
	if err != nil {
		t.Fatal(err)
	}

	var pdfBuf bytes.Buffer
	if err := doc.WritePDF(context.Background(), &pdfBuf, PDFOptions{}); err != nil {
		t.Fatal(err)
	}

	re, err := OpenBytes(pdfBuf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if re.Format() != FormatPDF || re.PageCount() != 1 {
		t.Fatalf("round-trip format/pages: %v/%d", re.Format(), re.PageCount())
	}

	img, err := re.RasterizePage(context.Background(), 0, RasterOptions{DPI: 72})
	if err != nil {
		t.Fatal(err)
	}
	rgba := img.(*image.RGBA)

	left := rgba.RGBAAt(2, 25)
	right := rgba.RGBAAt(97, 25)
	t.Logf("left pixel (near red stop): %+v", left)
	t.Logf("right pixel (near blue stop): %+v", right)

	if left.R <= left.B {
		t.Errorf("left edge should be reddish (R>B), got %+v", left)
	}
	if right.B <= right.R {
		t.Errorf("right edge should be blueish (B>R), got %+v", right)
	}
	if left == (color.RGBA{255, 255, 255, 255}) || right == (color.RGBA{255, 255, 255, 255}) {
		t.Errorf("gradient rendered blank white: left=%+v right=%+v", left, right)
	}
}

// multiStopGradientSVG is a three-stop, fully opaque linear gradient (pad
// spread, the default) — the shape pkg/render/pdfwrite's FillShading must
// convert into a native /Shading dictionary rather than rasterizing.
const multiStopGradientSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="120" height="60">
  <defs>
    <linearGradient id="g1" x1="0" y1="0" x2="1" y2="0">
      <stop offset="0" stop-color="red"/>
      <stop offset="0.5" stop-color="lime"/>
      <stop offset="1" stop-color="blue"/>
    </linearGradient>
  </defs>
  <rect x="0" y="0" width="120" height="60" fill="url(#g1)"/>
</svg>`

// TestSVGOpaqueGradientVisualEquivalence is the acceptance test for native PDF
// shadings: an opaque multi-stop SVG gradient is rendered two ways — (a)
// directly to raster, and (b) through PDF (SVG -> PDF -> OpenBytes -> raster).
// A well-formed but wrong /Shading dictionary would still produce a plausible-
// looking gradient, so pixel-for-pixel equivalence with the direct render
// (within the project's standard golden tolerance) is the only real proof the
// emitted /Function and /Coords are correct rather than merely present.
func TestSVGOpaqueGradientVisualEquivalence(t *testing.T) {
	doc, err := OpenSVGBytes([]byte(multiStopGradientSVG))
	if err != nil {
		t.Fatal(err)
	}
	direct, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: 96})
	if err != nil {
		t.Fatal(err)
	}

	var pdfBuf bytes.Buffer
	if err := doc.WritePDF(context.Background(), &pdfBuf, PDFOptions{}); err != nil {
		t.Fatal(err)
	}
	re, err := OpenBytes(pdfBuf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	viaPDF, err := re.RasterizePage(context.Background(), 0, RasterOptions{DPI: 96})
	if err != nil {
		t.Fatal(err)
	}

	if diff, n := compareImages(direct.(*image.RGBA), viaPDF.(*image.RGBA)); diff {
		t.Errorf("native shading render drifted from direct SVG render: %d pixels beyond tolerance", n)
	}
}

// TestSVGOpaqueGradientEmitsNativeShading is the structural counterpart to
// TestSVGOpaqueGradientVisualEquivalence: an opaque, pad-spread gradient's PDF
// output must contain a /ShadingType dictionary and must NOT contain an image
// XObject (/Subtype /Image) — proving the vector path was taken, not silently
// replaced by a rasterized fallback that happens to look right. Only the
// stream *bodies* pdfwrite emits are Flate-compressed (see writer.addStream);
// every dictionary, including a /Shading dict added via writer.put1, is
// written as plain text, so both names are searchable directly in the raw
// PDF bytes.
func TestSVGOpaqueGradientEmitsNativeShading(t *testing.T) {
	doc, err := OpenSVGBytes([]byte(multiStopGradientSVG))
	if err != nil {
		t.Fatal(err)
	}
	var pdfBuf bytes.Buffer
	if err := doc.WritePDF(context.Background(), &pdfBuf, PDFOptions{}); err != nil {
		t.Fatal(err)
	}
	raw := pdfBuf.Bytes()
	if !bytes.Contains(raw, []byte("/ShadingType")) {
		t.Error("opaque gradient PDF has no /ShadingType dictionary; want a native shading")
	}
	if bytes.Contains(raw, []byte("/Subtype/Image")) || bytes.Contains(raw, []byte("/Subtype /Image")) {
		t.Error("opaque gradient PDF contains an image XObject; want the vector path, not a rasterized fallback")
	}
}

// TestSVGTransparentGradientStillRasterizes is the inverse of
// TestSVGOpaqueGradientEmitsNativeShading: a gradient with a stop-opacity < 1
// stop has no native PDF /Shading equivalent (no alpha channel without a soft
// mask, out of scope here — see docs/superpowers/specs/2026-08-26-shader-
// describe-design.md), so it must still rasterize into an image XObject and
// must NOT emit a /ShadingType dictionary that would silently drop the
// transparency.
func TestSVGTransparentGradientStillRasterizes(t *testing.T) {
	src := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="100" height="50">
	  <defs>
	    <linearGradient id="g1" x1="0" y1="0" x2="1" y2="0">
	      <stop offset="0" stop-color="red" stop-opacity="0.2"/>
	      <stop offset="1" stop-color="blue"/>
	    </linearGradient>
	  </defs>
	  <rect x="0" y="0" width="100" height="50" fill="url(#g1)"/>
	</svg>`)
	doc, err := OpenSVGBytes(src)
	if err != nil {
		t.Fatal(err)
	}
	var pdfBuf bytes.Buffer
	if err := doc.WritePDF(context.Background(), &pdfBuf, PDFOptions{}); err != nil {
		t.Fatal(err)
	}
	raw := pdfBuf.Bytes()
	if bytes.Contains(raw, []byte("/ShadingType")) {
		t.Error("gradient with stop-opacity emitted a native /Shading dict; alpha has no native PDF equivalent")
	}
	if !bytes.Contains(raw, []byte("/Subtype/Image")) && !bytes.Contains(raw, []byte("/Subtype /Image")) {
		t.Error("gradient with stop-opacity produced no image XObject; want the rasterized fallback")
	}
}

// TestSVGGradientReflectSpreadStillRasterizes proves a non-pad spreadMethod
// (PDF /Extend models only "pad") also falls back to rasterizing rather than
// emitting a /Shading dict that would render the reflected/repeated ramp as a
// flat pad instead.
func TestSVGGradientReflectSpreadStillRasterizes(t *testing.T) {
	src := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="100" height="50">
	  <defs>
	    <linearGradient id="g1" x1="0.25" y1="0" x2="0.75" y2="0" spreadMethod="reflect">
	      <stop offset="0" stop-color="red"/>
	      <stop offset="1" stop-color="blue"/>
	    </linearGradient>
	  </defs>
	  <rect x="0" y="0" width="100" height="50" fill="url(#g1)"/>
	</svg>`)
	doc, err := OpenSVGBytes(src)
	if err != nil {
		t.Fatal(err)
	}
	var pdfBuf bytes.Buffer
	if err := doc.WritePDF(context.Background(), &pdfBuf, PDFOptions{}); err != nil {
		t.Fatal(err)
	}
	raw := pdfBuf.Bytes()
	if bytes.Contains(raw, []byte("/ShadingType")) {
		t.Error("reflect-spread gradient emitted a native /Shading dict; PDF /Extend has no reflect equivalent")
	}
	if !bytes.Contains(raw, []byte("/Subtype/Image")) && !bytes.Contains(raw, []byte("/Subtype /Image")) {
		t.Error("reflect-spread gradient produced no image XObject; want the rasterized fallback")
	}
}

// gzipSVGWithFiller builds a gzip-compressed SVG document whose decompressed
// size is exactly base + fillerLen: a valid <svg> root followed by an XML
// comment padded with fillerLen bytes of a repeated character. Comment filler
// compresses to a tiny fraction of its size, so this stays fast and
// low-memory even when fillerLen approaches svgzMaxSize.
func gzipSVGWithFiller(fillerLen int) []byte {
	head := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><rect width="10" height="10"/></svg><!--`)
	tail := []byte(`-->`)
	total := len(head) + fillerLen + len(tail)
	var buf bytes.Buffer
	buf.Grow(total / 100)
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(head); err != nil {
		panic(err)
	}
	filler := bytes.Repeat([]byte(" "), 1<<16)
	remaining := fillerLen
	for remaining > 0 {
		n := remaining
		if n > len(filler) {
			n = len(filler)
		}
		if _, err := zw.Write(filler[:n]); err != nil {
			panic(err)
		}
		remaining -= n
	}
	if _, err := zw.Write(tail); err != nil {
		panic(err)
	}
	if err := zw.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// TestOpenSVGBytesOversizedSVGZ proves an .svgz that decompresses to more
// than svgzMaxSize is rejected with a clean error rather than silently
// truncated (a truncated stream looks, to svg.Parse, like ordinary malformed
// real-world SVG and returns a partial tree with a nil error).
func TestOpenSVGBytesOversizedSVGZ(t *testing.T) {
	data := gzipSVGWithFiller(svgzMaxSize + 1024)
	if _, err := OpenSVGBytes(data); err == nil {
		t.Fatal("oversized svgz accepted without error")
	}
}

// TestOpenSVGBytesSVGZUnderCap proves the cap guard doesn't over-trigger: a
// payload decompressing to just under svgzMaxSize still opens successfully.
// This is the assertion that would catch an off-by-one at the boundary.
func TestOpenSVGBytesSVGZUnderCap(t *testing.T) {
	data := gzipSVGWithFiller(svgzMaxSize - 1024)
	doc, err := OpenSVGBytes(data)
	if err != nil {
		t.Fatalf("svgz just under cap rejected: %v", err)
	}
	if doc.PageCount() != 1 {
		t.Fatalf("pages = %d", doc.PageCount())
	}
}

// TestOpenSVGBytesCorruptGzip proves a truncated/corrupt gzip stream (the
// magic bytes are present but the stream is not valid gzip) returns a clean
// error instead of panicking or hanging.
func TestOpenSVGBytesCorruptGzip(t *testing.T) {
	data := []byte{0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0xff, 0xff, 0xff}
	if _, err := OpenSVGBytes(data); err == nil {
		t.Fatal("corrupt gzip accepted without error")
	}
}

// TestOpenSVGFileMissing proves a missing path returns a clean wrapped error.
func TestOpenSVGFileMissing(t *testing.T) {
	if _, err := OpenSVGFile("/nonexistent/path/does-not-exist.svg"); err == nil {
		t.Fatal("missing file accepted without error")
	}
}

// TestOpenSVGBytesLogf proves WithLogf reaches svg.Parse's degradation
// diagnostics (a <text> element is in pkg/svg's unsupportedElements set and
// is skipped with one logged line), and that omitting WithLogf entirely
// (the nil-logf path) still opens the same document without panicking.
func TestOpenSVGBytesLogf(t *testing.T) {
	src := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="100" height="50">
	  <text x="10" y="10">hello</text>
	  <rect x="10" y="10" width="80" height="30" fill="#0000ff"/>
	</svg>`)

	var lines []string
	if _, err := OpenSVGBytes(src, WithLogf(func(format string, args ...any) {
		lines = append(lines, fmt.Sprintf(format, args...))
	})); err != nil {
		t.Fatal(err)
	}
	if len(lines) == 0 {
		t.Error("WithLogf captured no diagnostics for an unsupported <text> element")
	}

	// No options: the nil-logf path must still work.
	if _, err := OpenSVGBytes(src); err != nil {
		t.Fatalf("open with no options (nil logf): %v", err)
	}
}

func TestSVGDetectAndFormat(t *testing.T) {
	plain := []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)
	prolog := []byte("\xEF\xBB\xBF<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n" +
		"<!-- generator -->\n<!DOCTYPE svg PUBLIC \"-//W3C//DTD SVG 1.1//EN\" \"x\">\n" +
		"<svg xmlns=\"http://www.w3.org/2000/svg\"/>")
	if f := DetectFormat(plain, ""); f != FormatSVG {
		t.Errorf("plain = %v", f)
	}
	// THE BUG FIX: an XML-prologed SVG used to sniff as HTML.
	if f := DetectFormat(prolog, ""); f != FormatSVG {
		t.Errorf("prolog = %v", f)
	}
	// Non-SVG XML still sniffs HTML (XHTML behavior preserved).
	if f := DetectFormat([]byte(`<?xml version="1.0"?><html xmlns="http://www.w3.org/1999/xhtml"/>`), ""); f != FormatHTML {
		t.Errorf("xhtml = %v", f)
	}
	// HTML containing an inline <svg> is still HTML (sniff only looks at the first tag).
	if f := DetectFormat([]byte(`<!DOCTYPE html><html><body><svg/></body></html>`), ""); f != FormatHTML {
		t.Errorf("html-with-svg = %v", f)
	}
	// Extension hint.
	if f := DetectFormat([]byte("garbage"), "icon.svg"); f != FormatSVG {
		t.Errorf("ext = %v", f)
	}
	// svgz magic.
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(plain); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if f := DetectFormat(buf.Bytes(), ""); f != FormatSVG {
		t.Errorf("svgz = %v", f)
	}
	// Gzip of something else stays unknown.
	buf.Reset()
	zw = gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte("just text")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if f := DetectFormat(buf.Bytes(), ""); f != FormatUnknown {
		t.Errorf("gz-text = %v", f)
	}

	// Format table entries.
	if f, err := ParseFormat("svg"); err != nil || f != FormatSVG {
		t.Errorf("ParseFormat = %v %v", f, err)
	}
	if FormatFromMIME("image/svg+xml") != FormatSVG || FormatSVG.MIME() != "image/svg+xml" {
		t.Error("MIME mapping")
	}
	if FormatFromPath("a.svgz") != FormatSVG {
		t.Error("svgz ext")
	}
	if !FormatSVG.ValidInput() || FormatSVG.ValidOutput() {
		t.Error("caps: want input-only")
	}

	// End-to-end dispatch.
	doc, err := OpenBytes(prolog)
	if err != nil || doc.PageCount() != 1 {
		t.Fatalf("OpenBytes: %v", err)
	}

	// Namespace-prefixed root: the "svg:" convention is recognized, but the
	// colon terminator must not over-match an arbitrary "svg:"-prefixed local
	// name that merely starts with "svg" as a string.
	if f := DetectFormat([]byte(`<svg:svg xmlns:svg="http://www.w3.org/2000/svg"/>`), ""); f != FormatSVG {
		t.Errorf("svg:svg = %v", f)
	}
	if f := DetectFormat([]byte(`<svg:zzz/>`), ""); f == FormatSVG {
		t.Errorf("svg:zzz = %v, want not FormatSVG (over-match)", f)
	}
	// Truncated prefix forms must not panic and must not match.
	if f := DetectFormat([]byte(`<svg:`), ""); f == FormatSVG {
		t.Errorf("truncated svg: = %v, want not FormatSVG", f)
	}
	if f := DetectFormat([]byte(`<svg:sv`), ""); f == FormatSVG {
		t.Errorf("truncated svg:sv = %v, want not FormatSVG", f)
	}
}

// TestRasterizeSVGNonFinitePageSizeErrorsCleanly proves an attacker-controlled
// SVG root size that scales to a non-finite/huge pixel dimension (width/height
// are unclamped SVG attributes, unlike CSS-derived sizes elsewhere in the
// reflow pipeline) returns a clean error from RasterizePage instead of
// panicking inside image.NewRGBA ("Rectangle has huge or negative dimensions").
func TestRasterizeSVGNonFinitePageSizeErrorsCleanly(t *testing.T) {
	src := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="1e300" height="1e300"/>`)
	doc, err := OpenSVGBytes(src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: 72}); err == nil {
		t.Fatal("1e300x1e300 svg rasterized without error; want clean error, not a panic/success")
	}
}

// TestRasterizeSVGHugeFinitePageSizeErrorsFast proves a huge-but-finite SVG
// page size (which would attempt a multi-terabyte allocation, e.g. 1e6 x 1e6
// px at 72 DPI) is rejected by the pixel-count cap quickly, rather than
// hanging the process trying to allocate it.
func TestRasterizeSVGHugeFinitePageSizeErrorsFast(t *testing.T) {
	src := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="1e6" height="1e6"/>`)
	doc, err := OpenSVGBytes(src)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: 72})
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("1e6x1e6 svg rasterized without error; want a clean pixel-cap error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RasterizePage did not return within 5s; want a fast, clean error, not a hang")
	}
}

// TestRasterizeSVGNormalPageSizeUnaffected proves the new size guard does not
// over-trigger: an ordinary small SVG still rasterizes successfully.
func TestRasterizeSVGNormalPageSizeUnaffected(t *testing.T) {
	src := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="200" height="100">
	  <rect width="200" height="100" fill="#00ff00"/>
	</svg>`)
	doc, err := OpenSVGBytes(src)
	if err != nil {
		t.Fatal(err)
	}
	img, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: 72})
	if err != nil {
		t.Fatalf("normal svg rejected: %v", err)
	}
	rgba := img.(*image.RGBA)
	if got := rgba.RGBAAt(100, 50); got != (color.RGBA{0, 255, 0, 255}) {
		t.Errorf("center = %+v, want green", got)
	}
}
