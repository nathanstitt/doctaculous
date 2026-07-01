package doctaculous

import (
	"bytes"
	"context"
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
