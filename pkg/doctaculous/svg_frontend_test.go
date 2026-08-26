package doctaculous

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"image"
	"image/color"
	"testing"
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
