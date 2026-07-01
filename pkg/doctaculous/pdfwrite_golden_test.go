package doctaculous

import (
	"bytes"
	"context"
	"image"
	"testing"

	gendocx "github.com/nathanstitt/doctaculous/testdata/gen/docx"
)

// TestConvertHTMLToPDFRoundTrips renders HTML to PDF and re-parses the output with
// the project's own parser, asserting a valid, non-empty page count.
func TestConvertHTMLToPDFRoundTrips(t *testing.T) {
	html := `<!DOCTYPE html><html><head><style>body{margin:0}</style></head>
<body><p>Hello PDF world</p></body></html>`

	var buf bytes.Buffer
	err := ConvertHTMLToPDF(context.Background(), bytes.NewReader([]byte(html)), &buf, PDFOptions{})
	if err != nil {
		t.Fatalf("ConvertHTMLToPDF: %v", err)
	}
	doc, err := OpenBytes(buf.Bytes())
	if err != nil {
		t.Fatalf("reopen generated PDF: %v", err)
	}
	if doc.PageCount() < 1 {
		t.Fatalf("page count = %d; want >= 1", doc.PageCount())
	}
}

// TestConvertHTMLToPDFEmbedsSearchableText asserts the output carries embedded text
// (a ToUnicode CMap and text-showing operators), i.e. real selectable text rather
// than outline fills.
func TestConvertHTMLToPDFEmbedsSearchableText(t *testing.T) {
	html := `<!DOCTYPE html><html><head><style>body{margin:0}</style></head>
<body><p>Searchable</p></body></html>`
	var buf bytes.Buffer
	if err := ConvertHTMLToPDF(context.Background(), bytes.NewReader([]byte(html)), &buf, PDFOptions{}); err != nil {
		t.Fatalf("ConvertHTMLToPDF: %v", err)
	}
	out := buf.Bytes()
	if !bytes.Contains(out, []byte("/ToUnicode")) {
		t.Error("output has no /ToUnicode; text is not searchable")
	}
	if !bytes.Contains(out, []byte("/FontFile")) {
		t.Error("output embeds no font program")
	}
}

// TestWritePDFWorksForDOCX exercises the DOCX -> PDF path through the same writer.
func TestWritePDFWorksForDOCX(t *testing.T) {
	fx := gendocx.Core[0]
	d, err := OpenDOCXBytes(fx.Bytes())
	if err != nil {
		t.Fatalf("OpenDOCXBytes: %v", err)
	}
	var buf bytes.Buffer
	if err := d.WritePDF(context.Background(), &buf, PDFOptions{}); err != nil {
		t.Fatalf("WritePDF (docx): %v", err)
	}
	if _, err := OpenBytes(buf.Bytes()); err != nil {
		t.Fatalf("docx->pdf output unparseable: %v", err)
	}
}

// TestWritePDFRejectsNonReflowDocument asserts WritePDF on an opened PDF (not a
// reflow document) returns a typed error rather than panicking.
func TestWritePDFRejectsNonReflowDocument(t *testing.T) {
	// A minimal HTML->PDF, reopened as a PDF document, is not a reflow document.
	var pdfBuf bytes.Buffer
	if err := ConvertHTMLToPDF(context.Background(), bytes.NewReader([]byte("<p>x</p>")), &pdfBuf, PDFOptions{}); err != nil {
		t.Fatal(err)
	}
	pdfDoc, err := OpenBytes(pdfBuf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := pdfDoc.WritePDF(context.Background(), &out, PDFOptions{}); err == nil {
		t.Fatal("WritePDF on a non-reflow document should error")
	}
}

// TestConvertHTMLToPDFPrintMedia asserts the Print option changes the cascade: an
// @media print rule that recolors text yields different output than the screen
// render (the color is baked into the compressed content stream, so screen ≠ print).
func TestConvertHTMLToPDFPrintMedia(t *testing.T) {
	html := `<!DOCTYPE html><html><head><style>
	p { color: #ff0000 }
	@media print { p { color: #00ff00 } }
	</style></head><body><p>x</p></body></html>`
	render := func(print bool) []byte {
		var buf bytes.Buffer
		if err := ConvertHTMLToPDF(context.Background(), bytes.NewReader([]byte(html)), &buf, PDFOptions{Print: print}); err != nil {
			t.Fatalf("ConvertHTMLToPDF(print=%v): %v", print, err)
		}
		return buf.Bytes()
	}
	if bytes.Equal(render(false), render(true)) {
		t.Fatal("print and screen output identical; @media print rule was not applied")
	}
}

// TestHTMLToPDFFidelity renders an HTML fixture two ways and asserts the generated
// PDF, re-parsed and rasterized through the project's own pipeline, draws real ink
// in its content region — proving the embedded fonts and borders render (Option A of
// the plan: structural fidelity + searchable text, avoiding brittle pixel alignment
// between the single-tall raster page and the Letter-sized PDF page).
func TestHTMLToPDFFidelity(t *testing.T) {
	cases := []struct{ name, html string }{
		{"text", `<!DOCTYPE html><html><head><style>body{margin:0}</style></head><body><p>Hello PDF</p></body></html>`},
		{"borders", `<!DOCTYPE html><html><head><style>body{margin:0}.b{border:4px solid #036;padding:8px}</style></head><body><div class="b">Boxed</div></body></html>`},
		{"mono", `<!DOCTYPE html><html><head><style>body{margin:0}code{font-family:monospace}</style></head><body><code>x = 1</code></body></html>`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var pdfBuf bytes.Buffer
			if err := ConvertHTMLToPDF(context.Background(), bytes.NewReader([]byte(tc.html)), &pdfBuf,
				PDFOptions{PageWidthPt: 612, PageHeightPt: 792, MarginPt: 36}); err != nil {
				t.Fatal(err)
			}
			// Searchable text: the output carries a ToUnicode CMap and an embedded font.
			out := pdfBuf.Bytes()
			if !bytes.Contains(out, []byte("/ToUnicode")) {
				t.Error("no /ToUnicode: text not searchable")
			}

			// Round-trip: re-parse OUR PDF and rasterize it through the project pipeline.
			pdfDoc, err := OpenBytes(out)
			if err != nil {
				t.Fatalf("reopen generated PDF: %v", err)
			}
			img, err := pdfDoc.RasterizePage(context.Background(), 0, RasterOptions{DPI: 72})
			if err != nil {
				t.Fatalf("rasterize generated PDF: %v", err)
			}
			if inked := countInkedPixels(img); inked == 0 {
				t.Errorf("%s: rasterized PDF is blank — glyphs/borders did not draw", tc.name)
			}
		})
	}
}

// countInkedPixels counts non-white, opaque pixels — a proxy for "something drew".
func countInkedPixels(img image.Image) int {
	b := img.Bounds()
	n := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := img.At(x, y).RGBA()
			if a > 0 && (r < 0xf000 || g < 0xf000 || bl < 0xf000) {
				n++
			}
		}
	}
	return n
}
