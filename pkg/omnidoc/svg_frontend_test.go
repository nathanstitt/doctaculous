package omnidoc

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"image"
	"image/color"
	"strings"
	"testing"
	"time"
)

func TestOpenSVGBytes(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

// TestHardBreakStillRendersAsBreak proves pkg/render/pdfwrite's coincident-
// offset nudge (minStopSpan, widened from 1e-6 to 5e-4 so it survives
// formatReal's 4-decimal-place rounding — see shading.go) does not visibly
// smear a hard two-tone color break (two stops at the same offset) into a
// ramp: sampling just before and just after the break in the round-tripped
// PDF render must show the two solid colors, not an interpolated blend.
func TestHardBreakStillRendersAsBreak(t *testing.T) {
	t.Parallel()
	src := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="100" height="20">
	  <defs>
	    <linearGradient id="g1" x1="0" y1="0" x2="1" y2="0">
	      <stop offset="0" stop-color="red"/>
	      <stop offset="0.5" stop-color="red"/>
	      <stop offset="0.5" stop-color="blue"/>
	      <stop offset="1" stop-color="blue"/>
	    </linearGradient>
	  </defs>
	  <rect x="0" y="0" width="100" height="20" fill="url(#g1)"/>
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
	img, err := re.RasterizePage(context.Background(), 0, RasterOptions{DPI: 72})
	if err != nil {
		t.Fatal(err)
	}
	rgba := img.(*image.RGBA)
	// Just before the break (x=48 of 100) should be solid red; just after
	// (x=52) should be solid blue. Neither should show a purple blend.
	before := rgba.RGBAAt(48, 10)
	after := rgba.RGBAAt(52, 10)
	t.Logf("before break: %+v; after break: %+v", before, after)
	if before.R < 200 || before.B > 20 {
		t.Errorf("pixel just before break = %+v, want solid red (no smear)", before)
	}
	if after.B < 200 || after.R > 20 {
		t.Errorf("pixel just after break = %+v, want solid blue (no smear)", after)
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
	t.Parallel()
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

// TestSVGTransparentGradientEmitsShadingUnderSoftMask proves a gradient with
// a stop-opacity < 1 stop now emits VECTOR content instead of rasterizing:
// with luminosity soft masks available (pkg/render/pdfwrite's group.go/
// softmask.go), the color ramp emits as a native /Shading dictionary paired
// with a /DeviceGray alpha shading under an /SMask, per the SVG groups/clip/
// mask design doc's decision 4 ("lift the alpha-gradient fallback"). The PDF
// must carry NO image XObject (the vector path, not the old raster
// fallback), and the round-tripped raster must match direct SVG rendering —
// the equivalence proof that the emitted alpha shading and soft mask are
// correct, not merely present (same technique as
// TestSVGOpaqueGradientVisualEquivalence).
func TestSVGTransparentGradientEmitsShadingUnderSoftMask(t *testing.T) {
	t.Parallel()
	src := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="100" height="50">
	  <rect width="100" height="50" fill="white"/>
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
	// DPI 72 keeps 1 PDF point == 1 device pixel exactly (the SVG's 100x50
	// viewport has no fractional pixel edge at this DPI). A non-1:1 DPI here
	// would land the shape's right edge mid-pixel, and BOTH the shading's own
	// clip (reapplied once at EndGroup) AND the /SMask form's own MANDATORY
	// /BBox clip (ISO 32000-1 SS8.10.1 — every form XObject, including a soft
	// mask's /G, is clipped to its BBox) independently antialias that same
	// physical edge; compounding two honest partial-coverage values at one
	// boundary pixel is expected PDF behavior (confirmed independently: real
	// Poppler renders the identical edge softening at a fractional-pixel
	// boundary here), not a bug in this equivalence check, so the test avoids
	// the fractional edge entirely rather than loosening the tolerance.
	direct, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: 72})
	if err != nil {
		t.Fatal(err)
	}

	var pdfBuf bytes.Buffer
	if err := doc.WritePDF(context.Background(), &pdfBuf, PDFOptions{}); err != nil {
		t.Fatal(err)
	}
	raw := pdfBuf.Bytes()
	if !bytes.Contains(raw, []byte("/ShadingType")) {
		t.Error("gradient with stop-opacity emitted no /ShadingType dict; want the vector path")
	}
	if bytes.Contains(raw, []byte("/Subtype/Image")) || bytes.Contains(raw, []byte("/Subtype /Image")) {
		t.Error("gradient with stop-opacity produced an image XObject; want vector /Shading + /SMask, not the raster fallback")
	}
	if !bytes.Contains(raw, []byte("/SMask")) {
		t.Error("gradient with stop-opacity emitted no /SMask; alpha cannot survive as vector content without one")
	}

	re, err := OpenBytes(raw)
	if err != nil {
		t.Fatal(err)
	}
	viaPDF, err := re.RasterizePage(context.Background(), 0, RasterOptions{DPI: 72})
	if err != nil {
		t.Fatal(err)
	}
	if diff, n := compareImages(direct.(*image.RGBA), viaPDF.(*image.RGBA)); diff {
		t.Errorf("alpha-gradient PDF round-trip drifted: %d pixels beyond tolerance", n)
	}
}

// TestSVGGradientReflectSpreadStillRasterizes proves a non-pad spreadMethod
// (PDF /Extend models only "pad") also falls back to rasterizing rather than
// emitting a /Shading dict that would render the reflected/repeated ramp as a
// flat pad instead.
func TestSVGGradientReflectSpreadStillRasterizes(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	data := gzipSVGWithFiller(svgzMaxSize + 1024)
	if _, err := OpenSVGBytes(data); err == nil {
		t.Fatal("oversized svgz accepted without error")
	}
}

// TestOpenSVGBytesSVGZUnderCap proves the cap guard doesn't over-trigger: a
// payload decompressing to just under svgzMaxSize still opens successfully.
// This is the assertion that would catch an off-by-one at the boundary.
func TestOpenSVGBytesSVGZUnderCap(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	data := []byte{0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0xff, 0xff, 0xff}
	if _, err := OpenSVGBytes(data); err == nil {
		t.Fatal("corrupt gzip accepted without error")
	}
}

// TestOpenSVGFileMissing proves a missing path returns a clean wrapped error.
func TestOpenSVGFileMissing(t *testing.T) {
	t.Parallel()
	if _, err := OpenSVGFile("/nonexistent/path/does-not-exist.svg"); err == nil {
		t.Fatal("missing file accepted without error")
	}
}

// TestOpenSVGBytesLogf proves WithLogf reaches svg.Parse's degradation
// diagnostics (an <image> element is in pkg/svg's unsupportedElements set and
// is skipped with one logged line), and that omitting WithLogf entirely
// (the nil-logf path) still opens the same document without panicking.
func TestOpenSVGBytesLogf(t *testing.T) {
	t.Parallel()
	src := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="100" height="50">
	  <image href="nope.png" x="10" y="10" width="10" height="10"/>
	  <rect x="10" y="10" width="80" height="30" fill="#0000ff"/>
	</svg>`)

	var lines []string
	if _, err := OpenSVGBytes(src, WithLogf(func(format string, args ...any) {
		lines = append(lines, fmt.Sprintf(format, args...))
	})); err != nil {
		t.Fatal(err)
	}
	if len(lines) == 0 {
		t.Error("WithLogf captured no diagnostics for an unsupported <image> element")
	}

	// No options: the nil-logf path must still work.
	if _, err := OpenSVGBytes(src); err != nil {
		t.Fatalf("open with no options (nil logf): %v", err)
	}
}

func TestSVGDetectAndFormat(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

// TestSVGFillOpacityRoundTripsThroughPDF is the regression guard for a bug
// where pdfwrite emitted no /ExtGState at all, so Fill/Stroke/FillGlyph/
// DrawImage silently discarded the paint's alpha and every partially
// transparent SVG element rendered fully opaque in PDF output. A 50%-alpha
// red rectangle over a white background must rasterize to ~#ff7f7f (50% red
// over white), not #ff0000 (fully opaque red), both directly (SVG -> raster)
// and through a PDF round trip (SVG -> PDF -> reopen -> raster). The two
// values matching is possible only because pkg/pdf/content's ExtGState
// support (fillAlpha/strokeAlpha in its gstate) already honors /ca and /CA,
// so this test also proves the reader half of the loop, not just the writer.
func TestSVGFillOpacityRoundTripsThroughPDF(t *testing.T) {
	t.Parallel()
	src := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100">
	  <rect width="100" height="100" fill="white"/>
	  <rect width="100" height="100" fill="red" fill-opacity="0.5"/>
	</svg>`)

	doc, err := OpenSVGBytes(src)
	if err != nil {
		t.Fatal(err)
	}

	// Direct raster (no PDF involved): the ground truth for the correct color.
	directImg, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: 72})
	if err != nil {
		t.Fatal(err)
	}
	directRGBA := directImg.(*image.RGBA).RGBAAt(50, 50)

	// Through PDF: SVG -> PDF bytes -> reopen -> rasterize.
	var pdfBuf bytes.Buffer
	if err := doc.WritePDF(context.Background(), &pdfBuf, PDFOptions{}); err != nil {
		t.Fatal(err)
	}
	raw := pdfBuf.Bytes()

	if !bytes.Contains(raw, []byte("/ExtGState")) {
		t.Error("PDF has no /ExtGState resource; alpha cannot survive without it")
	}
	if !bytes.Contains(raw, []byte("/ca ")) {
		t.Error("PDF has no /ca entry; fill alpha was not emitted")
	}

	pdfDoc, err := OpenBytes(raw)
	if err != nil {
		t.Fatal(err)
	}
	viaImg, err := pdfDoc.RasterizePage(context.Background(), 0, RasterOptions{DPI: 72})
	if err != nil {
		t.Fatal(err)
	}
	viaRGBA := viaImg.(*image.RGBA).RGBAAt(50, 50)

	t.Logf("direct raster: %s", hexColor(directRGBA))
	t.Logf("via PDF round-trip: %s", hexColor(viaRGBA))

	near := func(got, want uint8, tol int) bool {
		d := int(got) - int(want)
		return d >= -tol && d <= tol
	}
	// #ff7f7f: 50% red (255) blended over white (255) = 255; 50% of 0 blended
	// over white (255) for G/B = 127.5 ~ 128.
	if !near(viaRGBA.R, 0xff, 4) || !near(viaRGBA.G, 0x7f, 4) || !near(viaRGBA.B, 0x7f, 4) {
		t.Errorf("via PDF round-trip = %s, want ~#ff7f7f (50%% alpha survived), NOT #ff0000 (bug: alpha discarded)", hexColor(viaRGBA))
	}
	if !near(viaRGBA.R, directRGBA.R, 4) || !near(viaRGBA.G, directRGBA.G, 4) || !near(viaRGBA.B, directRGBA.B, 4) {
		t.Errorf("PDF round-trip %s does not match direct raster %s", hexColor(viaRGBA), hexColor(directRGBA))
	}
}

// hexColor formats an opaque color.RGBA as "#rrggbb" for readable test output.
func hexColor(c color.RGBA) string {
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}

// TestSVGGroupOpacityRoundTripsThroughPDFNoSeam is the discriminating
// regression test for pkg/pdf/content's transparency-group support: a
// <g opacity="0.5"> containing two OVERLAPPING opaque shapes must render
// IDENTICALLY at the overlap and at a non-overlapping single-shape point,
// both directly (SVG -> raster) and through a PDF round trip
// (SVG -> PDF -> reopen -> raster).
//
// Sampling only a single-shape point would NOT catch the bug this guards
// against: pkg/render/pdfwrite always emitted a spec-correct isolated
// transparency Form XObject (verified independently against Poppler), but
// pkg/pdf/content's reader used to run a group Form's content directly
// against the ambient constant alpha instead of recognizing /Group and
// compositing it as an isolated unit — so each of the form's two overlapping
// fills was individually dimmed to 50%, and the overlap (50% blue over 50%
// blue over white) came out TWICE as dark as either shape alone. Only a
// point INSIDE the overlap distinguishes "composited once, correctly" from
// "double-darkened by per-primitive alpha."
func TestSVGGroupOpacityRoundTripsThroughPDFNoSeam(t *testing.T) {
	t.Parallel()
	src := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100">
	  <rect width="100" height="100" fill="white"/>
	  <g opacity="0.5">
	    <rect x="5" y="5" width="40" height="40" fill="blue"/>
	    <rect x="20" y="20" width="40" height="40" fill="blue"/>
	  </g>
	</svg>`)
	doc, err := OpenSVGBytes(src)
	if err != nil {
		t.Fatal(err)
	}

	directImg, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: 72})
	if err != nil {
		t.Fatal(err)
	}
	direct := directImg.(*image.RGBA)
	directOverlap := direct.RGBAAt(30, 30) // inside both rects
	directSingle := direct.RGBAAt(10, 10)  // inside only the first rect

	var pdfBuf bytes.Buffer
	if err := doc.WritePDF(context.Background(), &pdfBuf, PDFOptions{}); err != nil {
		t.Fatal(err)
	}
	raw := pdfBuf.Bytes()
	if !bytes.Contains(raw, []byte("/Subtype/Form")) && !bytes.Contains(raw, []byte("/Subtype /Form")) {
		t.Error("grouped opacity PDF has no Form XObject; want a transparency group")
	}
	if !bytes.Contains(raw, []byte("/S/Transparency")) && !bytes.Contains(raw, []byte("/S /Transparency")) {
		t.Error("grouped opacity PDF has no /Group /S /Transparency; the writer must emit an isolated group")
	}

	pdfDoc, err := OpenBytes(raw)
	if err != nil {
		t.Fatal(err)
	}
	viaImg, err := pdfDoc.RasterizePage(context.Background(), 0, RasterOptions{DPI: 72})
	if err != nil {
		t.Fatal(err)
	}
	via := viaImg.(*image.RGBA)
	viaOverlap := via.RGBAAt(30, 30)
	viaSingle := via.RGBAAt(10, 10)

	t.Logf("direct: overlap=%s single=%s", hexColor(directOverlap), hexColor(directSingle))
	t.Logf("via PDF: overlap=%s single=%s", hexColor(viaOverlap), hexColor(viaSingle))

	near := func(got, want uint8, tol int) bool {
		d := int(got) - int(want)
		return d >= -tol && d <= tol
	}
	// #7f7fff: 50% blue over white. Both overlap and single must land here —
	// a double-darkened overlap would read ~#3f3fff instead.
	if !near(viaOverlap.R, 0x7f, 4) || !near(viaOverlap.G, 0x7f, 4) || !near(viaOverlap.B, 0xff, 4) {
		t.Errorf("via PDF overlap = %s, want ~#7f7fff (composited once); a double-darkened seam reads ~#3f3fff", hexColor(viaOverlap))
	}
	if viaOverlap != viaSingle {
		t.Errorf("SEAM: via PDF overlap = %s != single = %s; group opacity must composite once, not per child", hexColor(viaOverlap), hexColor(viaSingle))
	}
	if !near(viaOverlap.R, directOverlap.R, 4) || !near(viaOverlap.G, directOverlap.G, 4) || !near(viaOverlap.B, directOverlap.B, 4) {
		t.Errorf("PDF round-trip overlap %s does not match direct raster overlap %s", hexColor(viaOverlap), hexColor(directOverlap))
	}
}

// TestClipPathAndMaskBothApplyThroughPDF is the discriminating regression
// test for a bug where an element carrying BOTH clip-path AND mask lost ALL
// content when rendered through the PDF backend, while rendering correctly
// direct-to-raster. Root cause: pkg/svg/draw used to pre-combine the clip
// mask and the <mask> luminance result into a single GroupMask before
// handing it to EndGroup; pkg/render/pdfwrite's BuildLuminanceMask returns a
// SENTINEL pointer recognized only by exact identity in its own EndGroup
// (see softmask.go's takePendingSoftMask), so combining it with a clip mask
// produced a new value neither backend built, which pdfwrite then silently
// mis-decoded as a real (but wrong, near-empty) coverage buffer — erasing
// the element entirely. The fix passes clipMask and softMask to EndGroup as
// two SEPARATE parameters (see render.Device's doc comment) so pdfwrite's
// sentinel always reaches its own EndGroup unmodified.
//
// A clip-path ALONE, or a mask ALONE, could not have caught this — only the
// combination trips the bug, so this test exercises exactly that
// combination and nothing less.
func TestClipPathAndMaskBothApplyThroughPDF(t *testing.T) {
	t.Parallel()
	src := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100">
	  <mask id="m1"><rect x="0" y="0" width="100" height="100" fill="white"/></mask>
	  <clipPath id="c1"><rect x="0" y="0" width="60" height="100"/></clipPath>
	  <rect x="0" y="0" width="100" height="100" fill="red" mask="url(#m1)" clip-path="url(#c1)"/>
	</svg>`)
	doc, err := OpenSVGBytes(src)
	if err != nil {
		t.Fatal(err)
	}

	// Direct raster: the ground truth. (40,20) is inside the clip rect (x<60)
	// and inside the (fully white, fully permissive) mask -> red. (80,60) is
	// outside the clip rect (x>=60) -> unpainted white regardless of the mask.
	directImg, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: 72})
	if err != nil {
		t.Fatal(err)
	}
	direct := directImg.(*image.RGBA)
	directInside := direct.RGBAAt(40, 20)
	directOutside := direct.RGBAAt(80, 60)

	var pdfBuf bytes.Buffer
	if err := doc.WritePDF(context.Background(), &pdfBuf, PDFOptions{}); err != nil {
		t.Fatal(err)
	}
	pdfDoc, err := OpenBytes(pdfBuf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	viaImg, err := pdfDoc.RasterizePage(context.Background(), 0, RasterOptions{DPI: 72})
	if err != nil {
		t.Fatal(err)
	}
	via := viaImg.(*image.RGBA)
	viaInside := via.RGBAAt(40, 20)
	viaOutside := via.RGBAAt(80, 60)

	t.Logf("direct: inside-clip=%s outside-clip=%s", hexColor(directInside), hexColor(directOutside))
	t.Logf("via PDF: inside-clip=%s outside-clip=%s", hexColor(viaInside), hexColor(viaOutside))

	near := func(got, want uint8, tol int) bool {
		d := int(got) - int(want)
		return d >= -tol && d <= tol
	}
	if !near(viaInside.R, 0xff, 4) || viaInside.G > 4 || viaInside.B > 4 {
		t.Errorf("via PDF inside clip+mask = %s, want ~#ff0000 (red) -- content vanished (bug: clip mask and soft mask combined into a value pdfwrite could not recognize)", hexColor(viaInside))
	}
	if !near(viaOutside.R, 0xff, 4) || !near(viaOutside.G, 0xff, 4) || !near(viaOutside.B, 0xff, 4) {
		t.Errorf("via PDF outside clip (80,60) = %s, want ~#ffffff (unpainted; clip must still restrict content)", hexColor(viaOutside))
	}
	if !near(viaInside.R, directInside.R, 4) || !near(viaInside.G, directInside.G, 4) || !near(viaInside.B, directInside.B, 4) {
		t.Errorf("PDF round-trip inside-clip %s does not match direct raster %s", hexColor(viaInside), hexColor(directInside))
	}
	if !near(viaOutside.R, directOutside.R, 4) || !near(viaOutside.G, directOutside.G, 4) || !near(viaOutside.B, directOutside.B, 4) {
		t.Errorf("PDF round-trip outside-clip %s does not match direct raster %s", hexColor(viaOutside), hexColor(directOutside))
	}
}

// TestNestedMaskOnMaskThroughPDF is the PDF-backend companion to
// pkg/svg/draw's TestNestedMaskOnMask: a <mask> that itself carries a
// mask="url(#...)" self-reference (mask-on-mask) combines two independent
// BuildLuminanceMask results, which is the SAME underlying hazard as
// TestClipPathAndMaskBothApplyThroughPDF (a pdfwrite sentinel losing its
// identity when combined with another mask) — just triggered entirely
// within pkg/svg/draw's buildMask instead of at the clip+mask call sites.
// The fix applies msk.Self's mask INSIDE the same BuildLuminanceMask call as
// msk's own content, via a nested BeginGroup/EndGroup on the scratch device,
// so at most one BuildLuminanceMask call (and therefore at most one
// sentinel) is ever produced per buildMask invocation.
func TestNestedMaskOnMaskThroughPDF(t *testing.T) {
	t.Parallel()
	src := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="200" height="200">
	  <mask id="inner"><rect x="40" y="40" width="120" height="120" fill="white"/></mask>
	  <mask id="outer" mask="url(#inner)" maskUnits="userSpaceOnUse" x="0" y="0" width="200" height="200">
	    <rect x="20" y="20" width="160" height="160" fill="white"/>
	  </mask>
	  <rect x="20" y="20" width="160" height="160" fill="green" mask="url(#outer)"/>
	</svg>`)
	doc, err := OpenSVGBytes(src)
	if err != nil {
		t.Fatal(err)
	}

	directImg, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: 72})
	if err != nil {
		t.Fatal(err)
	}
	direct := directImg.(*image.RGBA)
	directBoth := direct.RGBAAt(50, 50)  // inside both inner and outer's own content
	directOuter := direct.RGBAAt(15, 15) // inside outer only, outside inner

	var pdfBuf bytes.Buffer
	if err := doc.WritePDF(context.Background(), &pdfBuf, PDFOptions{}); err != nil {
		t.Fatal(err)
	}
	pdfDoc, err := OpenBytes(pdfBuf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	viaImg, err := pdfDoc.RasterizePage(context.Background(), 0, RasterOptions{DPI: 72})
	if err != nil {
		t.Fatal(err)
	}
	via := viaImg.(*image.RGBA)
	viaBoth := via.RGBAAt(50, 50)
	viaOuter := via.RGBAAt(15, 15)

	t.Logf("direct: both=%s outer-only=%s", hexColor(directBoth), hexColor(directOuter))
	t.Logf("via PDF: both=%s outer-only=%s", hexColor(viaBoth), hexColor(viaOuter))

	near := func(got, want uint8, tol int) bool {
		d := int(got) - int(want)
		return d >= -tol && d <= tol
	}
	if viaBoth.G < 100 || viaBoth.R > 5 {
		t.Errorf("via PDF inside both masks = %s, want green -- content vanished (mask-on-mask combined into a value pdfwrite could not recognize)", hexColor(viaBoth))
	}
	if !near(viaOuter.R, 0xff, 4) || !near(viaOuter.G, 0xff, 4) || !near(viaOuter.B, 0xff, 4) {
		t.Errorf("via PDF outer-only (15,15) = %s, want ~#ffffff (inner mask must still restrict outer)", hexColor(viaOuter))
	}
	if !near(viaBoth.R, directBoth.R, 4) || !near(viaBoth.G, directBoth.G, 4) || !near(viaBoth.B, directBoth.B, 4) {
		t.Errorf("PDF round-trip (both) %s does not match direct raster %s", hexColor(viaBoth), hexColor(directBoth))
	}
}

// TestSVGOpaquePDFByteIdenticalAfterExtGState is the primary regression guard
// for the ExtGState plumbing: a fully-opaque document (every fill/stroke at
// alpha 1.0, no blend modes) must emit byte-identical PDF output to before
// ExtGState support existed. extGState.needed() returning false for the
// opaque case is what this proves — no "/GSn gs" operator and no /ExtGState
// resource may appear.
func TestSVGOpaquePDFByteIdenticalAfterExtGState(t *testing.T) {
	t.Parallel()
	src := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="120" height="80">
	  <rect x="0" y="0" width="120" height="80" fill="#0000ff"/>
	  <rect x="10" y="10" width="40" height="30" fill="#00ff00" stroke="#000000" stroke-width="2"/>
	  <circle cx="90" cy="40" r="20" fill="red"/>
	</svg>`)
	doc, err := OpenSVGBytes(src)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := doc.WritePDF(context.Background(), &buf, PDFOptions{}); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	if bytes.Contains(raw, []byte("/ExtGState")) {
		t.Error("fully-opaque document emitted /ExtGState; want none")
	}
	if bytes.Contains(raw, []byte(" gs\n")) {
		t.Error("fully-opaque document emitted a \"gs\" operator; want none")
	}
	if bytes.Contains(raw, []byte("/ca ")) || bytes.Contains(raw, []byte("/CA ")) {
		t.Error("fully-opaque document emitted /ca or /CA; want none")
	}
}

// TestSVGManySameAlphaShapesDedupeExtGState proves a document with many shapes
// at the same alpha emits ONE /ExtGState resource, not one per shape.
func TestSVGManySameAlphaShapesDedupeExtGState(t *testing.T) {
	t.Parallel()
	var rects strings.Builder
	for i := 0; i < 50; i++ {
		fmt.Fprintf(&rects, `<rect x="%d" y="0" width="2" height="10" fill="red" fill-opacity="0.5"/>`, i*2)
	}
	src := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="100" height="10">` + rects.String() + `</svg>`)
	doc, err := OpenSVGBytes(src)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := doc.WritePDF(context.Background(), &buf, PDFOptions{}); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	n := bytes.Count(raw, []byte("/Type/ExtGState")) + bytes.Count(raw, []byte("/Type /ExtGState"))
	if n == 0 {
		// The writer does not tag ExtGState dicts with /Type; count the resource
		// dict's GS entries instead (GS0, GS1, ... — one per distinct name).
		n = bytes.Count(raw, []byte("/GS0 "))
		if n == 0 {
			t.Fatal("no /GS0 resource name found in PDF; ExtGState not emitted at all")
		}
		if extra := bytes.Count(raw, []byte("/GS1 ")); extra != 0 {
			t.Errorf("found /GS1 resource name; 50 same-alpha shapes should dedupe to a single GS0, got a second distinct name")
		}
		return
	}
	if n != 1 {
		t.Errorf("found %d /ExtGState-typed objects; want exactly 1 (50 same-alpha shapes should dedupe)", n)
	}
}

// TestFilteredElementSurvivesPDFRoundTrip is the SVG -> PDF -> reopen ->
// raster guard for the filter path's pdfwrite contract.
//
// pkg/render/pdfwrite returns nil from RenderOffscreen — it emits vector
// operators and has no pixel buffer, and a filter (pure pixel math, with no
// PDF operator) has no vector form to fall back on. pkg/svg/draw must then
// paint the element UNFILTERED: content stays visible and correctly placed,
// losing only the filter's visual effect.
//
// That contract is invisible to the SVG-only tests (which all run on the
// raster backend, where RenderOffscreen succeeds), and the failure modes it
// guards are severe and silent — a nil mishandled one way DROPS the element
// from the PDF entirely, and mishandled the other way (invoking the paint
// callback before returning nil) paints it TWICE, once into the discarded
// offscreen pass and once for real, double-darkening every overlap.
//
// All three element kinds are covered because each reaches the filter
// through its own branch (paintShape, paintGroupBody, paintText) and each had
// to strip element opacity separately — a per-kind regression would otherwise
// only surface in one of them.
func TestFilteredElementSurvivesPDFRoundTrip(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		body   string
		probeX int
		probeY int
	}{
		{
			name:   "shape",
			body:   `<rect x="10" y="10" width="80" height="80" fill="red" filter="url(#f)"/>`,
			probeX: 50, probeY: 50,
		},
		{
			name:   "group",
			body:   `<g filter="url(#f)"><rect x="10" y="10" width="80" height="80" fill="red"/></g>`,
			probeX: 50, probeY: 50,
		},
		{
			name:   "text",
			body:   `<text x="10" y="60" font-size="50" fill="red" filter="url(#f)">III</text>`,
			probeX: 20, probeY: 40,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100">
			  <filter id="f" filterUnits="userSpaceOnUse" x="0" y="0" width="100" height="100">
			    <feOffset dx="0" dy="0"/>
			  </filter>
			  ` + c.body + `
			</svg>`)
			doc, err := OpenSVGBytes(src)
			if err != nil {
				t.Fatal(err)
			}

			directImg, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: 72})
			if err != nil {
				t.Fatal(err)
			}
			directInk := countRedInk(directImg.(*image.RGBA))

			var pdfBuf bytes.Buffer
			if err := doc.WritePDF(context.Background(), &pdfBuf, PDFOptions{}); err != nil {
				t.Fatal(err)
			}
			pdfDoc, err := OpenBytes(pdfBuf.Bytes())
			if err != nil {
				t.Fatal(err)
			}
			viaImg, err := pdfDoc.RasterizePage(context.Background(), 0, RasterOptions{DPI: 72})
			if err != nil {
				t.Fatal(err)
			}
			viaInk := countRedInk(viaImg.(*image.RGBA))

			t.Logf("red ink: direct=%d px, via PDF=%d px", directInk, viaInk)

			if directInk == 0 {
				t.Fatal("the direct raster painted nothing; the fixture is wrong, not the PDF path")
			}
			// The filter is an identity feOffset, so pdfwrite's unfiltered
			// fallback must cover essentially the same pixels the raster
			// backend's filtered path does. A DROPPED element leaves no ink
			// at all; coverage this far off would mean the fallback placed or
			// scaled it wrongly.
			if viaInk == 0 {
				t.Fatalf("no ink in the PDF render: the filtered %s was dropped entirely", c.name)
			}
			if ratio := float64(viaInk) / float64(directInk); ratio < 0.8 || ratio > 1.25 {
				t.Errorf("PDF ink %d px vs direct %d px (ratio %.2f): the unfiltered fallback is not landing where the filtered raster does",
					viaInk, directInk, ratio)
			}
		})
	}
}

// TestFilteredElementWithOpacitySurvivesPDFRoundTrip pins the element-opacity
// half of the same contract: pdfwrite's unfiltered fallback must still apply
// the element's own opacity, which pkg/svg/draw strips from the source pass
// and re-applies to the result. Dropping it on the fallback path would render
// the element fully opaque in PDF while the raster backend renders it faded.
func TestFilteredElementWithOpacitySurvivesPDFRoundTrip(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, body string }{
		{"shape", `<rect x="10" y="10" width="80" height="80" fill="red" opacity="0.5" filter="url(#f)"/>`},
		{"group", `<g opacity="0.5" filter="url(#f)"><rect x="10" y="10" width="80" height="80" fill="red"/></g>`},
		{"text", `<text x="10" y="60" font-size="50" fill="red" opacity="0.5" filter="url(#f)">III</text>`},
	} {
		t.Run(c.name, func(t *testing.T) {
			src := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100">
			  <filter id="f" filterUnits="userSpaceOnUse" x="0" y="0" width="100" height="100">
			    <feOffset dx="0" dy="0"/>
			  </filter>
			  ` + c.body + `
			</svg>`)
			doc, err := OpenSVGBytes(src)
			if err != nil {
				t.Fatal(err)
			}
			var pdfBuf bytes.Buffer
			if err := doc.WritePDF(context.Background(), &pdfBuf, PDFOptions{}); err != nil {
				t.Fatal(err)
			}
			pdfDoc, err := OpenBytes(pdfBuf.Bytes())
			if err != nil {
				t.Fatal(err)
			}
			viaImg, err := pdfDoc.RasterizePage(context.Background(), 0, RasterOptions{DPI: 72})
			if err != nil {
				t.Fatal(err)
			}
			via := viaImg.(*image.RGBA)

			// Scan for the most-saturated red the element produced. At 50%
			// opacity over white it must be visibly pink, never full red.
			var best uint8 = 255
			for y := 0; y < 100; y++ {
				for x := 0; x < 100; x++ {
					if p := via.RGBAAt(x, y); p.R > 0xa0 && p.G < best {
						best = p.G
					}
				}
			}
			t.Logf("%s: strongest red has G=%d (want ~128 for 50%% over white, 0 would mean opacity was lost)", c.name, best)
			if best < 90 {
				t.Errorf("G=%d is too saturated: the element's opacity was lost on the unfiltered PDF fallback", best)
			}
		})
	}
}

// countRedInk counts pixels that are recognisably the fixtures' red fill,
// the placement-independent way the filter round-trip tests compare a raster
// render against the same content routed through PDF.
func countRedInk(img *image.RGBA) int {
	n := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if c := img.RGBAAt(x, y); c.R > 0xa0 && c.G < 0x80 && c.B < 0x80 {
				n++
			}
		}
	}
	return n
}
