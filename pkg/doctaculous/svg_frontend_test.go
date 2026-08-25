package doctaculous

import (
	"bytes"
	"compress/gzip"
	"context"
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
	zw.Write(src)
	zw.Close()
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
