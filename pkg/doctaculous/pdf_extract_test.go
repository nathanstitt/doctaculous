package doctaculous

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// roundTripHTML converts HTML to a PDF (via the existing writer), then opens that PDF and
// extracts it back. It is the strongest end-to-end signal for extraction: real PDF bytes,
// real glyphs and rules, driven through the full interpreter → sink → extract pipeline.
func roundTripPDF(t *testing.T, html string) []byte {
	t.Helper()
	var pdfBuf bytes.Buffer
	err := ConvertHTMLToPDF(context.Background(), strings.NewReader(html), &pdfBuf, PDFOptions{})
	if err != nil {
		t.Fatalf("ConvertHTMLToPDF: %v", err)
	}
	if pdfBuf.Len() == 0 {
		t.Fatal("produced empty PDF")
	}
	return pdfBuf.Bytes()
}

func TestPDFToMarkdownRoundTrip(t *testing.T) {
	html := `<!DOCTYPE html><html><head><style>body{margin:0}
		h1{font-size:32px}</style></head><body>
		<h1>Quarterly Report</h1>
		<p>This is the introductory paragraph of the report body text.</p>
	</body></html>`
	pdf := roundTripPDF(t, html)

	var out bytes.Buffer
	if err := ConvertPDFToMarkdown(context.Background(), bytes.NewReader(pdf), &out, MarkdownOptions{}); err != nil {
		t.Fatalf("ConvertPDFToMarkdown: %v", err)
	}
	got := out.String()
	// The extracted text should contain the heading and body words. Exact structure
	// (heading level) depends on the size-classification heuristic, so assert on content
	// survival rather than exact markdown.
	for _, want := range []string{"Quarterly Report", "introductory paragraph"} {
		if !strings.Contains(got, want) {
			t.Errorf("extracted markdown missing %q\n---\n%s", want, got)
		}
	}
}

func TestPDFToHTMLRoundTrip(t *testing.T) {
	html := `<!DOCTYPE html><html><head><style>body{margin:0}</style></head><body>
		<h1>Title Here</h1>
		<p>Some body content in a paragraph.</p>
	</body></html>`
	pdf := roundTripPDF(t, html)

	var out bytes.Buffer
	if err := ConvertPDFToHTML(context.Background(), bytes.NewReader(pdf), &out, HTMLWriteOptions{}); err != nil {
		t.Fatalf("ConvertPDFToHTML: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "<!DOCTYPE html>") || !strings.Contains(got, "<body>") {
		t.Errorf("HTML output missing document scaffold:\n%s", got)
	}
	for _, want := range []string{"Title Here", "Some body content"} {
		if !strings.Contains(got, want) {
			t.Errorf("extracted HTML missing %q\n---\n%s", want, got)
		}
	}
}

func TestPDFDocumentSatisfiesWriteMarkdown(t *testing.T) {
	// An opened PDF must now support WriteMarkdown/WriteText/WriteHTML (it satisfies
	// reflowTree via lazy extraction), where previously it errored.
	pdf := roundTripPDF(t, `<html><body><p>hello world</p></body></html>`)
	doc, err := OpenBytes(pdf)
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	var md, txt, html bytes.Buffer
	if err := doc.WriteMarkdown(context.Background(), &md, MarkdownOptions{}); err != nil {
		t.Fatalf("WriteMarkdown on PDF: %v", err)
	}
	if err := doc.WriteText(context.Background(), &txt, MarkdownOptions{}); err != nil {
		t.Fatalf("WriteText on PDF: %v", err)
	}
	if err := doc.WriteHTML(context.Background(), &html, HTMLWriteOptions{}); err != nil {
		t.Fatalf("WriteHTML on PDF: %v", err)
	}
	if !strings.Contains(md.String(), "hello world") {
		t.Errorf("markdown missing text: %q", md.String())
	}
}
