package doctaculous

import (
	"bytes"
	"context"
	"image"
	"strings"
	"testing"
)

func TestConvertHTMLToMarkdown(t *testing.T) {
	src := `<html><body>
		<h1>Report</h1>
		<p>Intro with a <a href="https://x.test">link</a> and <strong>bold</strong>.</p>
		<table>
			<tr><th>Name</th><th>Qty</th></tr>
			<tr><td>Apples</td><td>3</td></tr>
		</table>
	</body></html>`
	var out bytes.Buffer
	if err := ConvertHTMLToMarkdown(context.Background(), strings.NewReader(src), &out, MarkdownOptions{}); err != nil {
		t.Fatalf("ConvertHTMLToMarkdown: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"# Report",
		"[link](https://x.test)",
		"**bold**",
		"| Name | Qty |",
		"| --- | --- |",
		"| Apples | 3 |",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\n---\n%s", want, got)
		}
	}
}

func TestConvertHTMLToText(t *testing.T) {
	src := `<html><body><h1>Title</h1><p>Body <strong>word</strong>.</p></body></html>`
	var out bytes.Buffer
	if err := ConvertHTMLToText(context.Background(), strings.NewReader(src), &out, MarkdownOptions{}); err != nil {
		t.Fatalf("ConvertHTMLToText: %v", err)
	}
	got := out.String()
	if strings.Contains(got, "#") || strings.Contains(got, "**") {
		t.Errorf("plain text should have no markdown syntax:\n%s", got)
	}
	if !strings.Contains(got, "Title") || !strings.Contains(got, "Body word.") {
		t.Errorf("plain text missing content:\n%s", got)
	}
}

func TestWriteMarkdownRejectsPDF(t *testing.T) {
	// A PDF document has no reflow tree; WriteMarkdown must error rather than panic.
	d := &Document{r: pdfStub{}}
	var out bytes.Buffer
	if err := d.WriteMarkdown(context.Background(), &out, MarkdownOptions{}); err == nil {
		t.Fatal("expected error converting a non-reflow document")
	}
}

// pdfStub is a minimal renderer that is not a reflowTree, standing in for an opened PDF.
type pdfStub struct{}

func (pdfStub) pageCount() int { return 0 }
func (pdfStub) renderPage(context.Context, int, RasterOptions) (image.Image, error) {
	return nil, nil
}
