package doctaculous

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// TestPDFWriteTextRoundTrip renders markdown to PDF with the project's own writer,
// re-opens the PDF with the project's own parser, and asserts the extracted text
// survives. The fixture includes a heading (bold face) and body text so multiple
// embedded fonts are exercised.
func TestPDFWriteTextRoundTrip(t *testing.T) {
	ctx := context.Background()
	md := "# Fixture Heading\n\nHello doctaculous world.\n"

	doc, err := OpenBytesAs(FormatMarkdown, []byte(md))
	if err != nil {
		t.Fatalf("OpenBytesAs(markdown): %v", err)
	}
	var pdf bytes.Buffer
	if err := doc.Write(ctx, &pdf, FormatPDF, ConvertOptions{}); err != nil {
		t.Fatalf("Write(pdf): %v", err)
	}

	doc2, err := OpenBytesAs(FormatPDF, pdf.Bytes())
	if err != nil {
		t.Fatalf("reopen generated PDF: %v", err)
	}
	var txt bytes.Buffer
	if err := doc2.WriteText(ctx, &txt, MarkdownOptions{}); err != nil {
		t.Fatalf("WriteText: %v", err)
	}

	got := txt.String()
	for _, want := range []string{"Fixture Heading", "Hello doctaculous world"} {
		if !strings.Contains(got, want) {
			t.Errorf("extracted text missing %q; got:\n%s", want, got)
		}
	}
}
