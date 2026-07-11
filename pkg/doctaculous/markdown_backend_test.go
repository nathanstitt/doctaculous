package doctaculous

import (
	"bytes"
	"context"
	"image"
	"strings"
	"testing"
	"unicode/utf8"
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

// TestWriteTextMaxBytes pins the truncation contract: for every budget the
// output is a prefix of the untruncated output, within the budget, and valid
// UTF-8 (the crossing write backs up over a partial rune); a zero budget means
// no truncation.
func TestWriteTextMaxBytes(t *testing.T) {
	// Multibyte content (em dashes, CJK) so budgets land mid-rune.
	doc, err := OpenHTMLBytes([]byte("<html><body><h1>Title — 標題</h1><p>Body text — 本文です。More prose follows here.</p></body></html>"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	var full bytes.Buffer
	if err := doc.WriteText(context.Background(), &full, MarkdownOptions{}); err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	if full.Len() == 0 {
		t.Fatal("untruncated output is empty")
	}

	for _, max := range []int{1, 2, 3, 5, 7, 10, full.Len() / 2, full.Len() - 1, full.Len(), full.Len() + 100} {
		var out bytes.Buffer
		if err := doc.WriteText(context.Background(), &out, MarkdownOptions{MaxBytes: max}); err != nil {
			t.Fatalf("MaxBytes=%d: %v", max, err)
		}
		if out.Len() > max {
			t.Errorf("MaxBytes=%d: wrote %d bytes", max, out.Len())
		}
		if !bytes.HasPrefix(full.Bytes(), out.Bytes()) {
			t.Errorf("MaxBytes=%d: output is not a prefix of the full output", max)
		}
		if !utf8.Valid(out.Bytes()) {
			t.Errorf("MaxBytes=%d: output is not valid UTF-8", max)
		}
		if max >= full.Len() && out.Len() != full.Len() {
			t.Errorf("MaxBytes=%d (>= full %d): truncated to %d", max, full.Len(), out.Len())
		}
	}

	// Zero is the default: byte-identical to the uncapped output.
	var out bytes.Buffer
	if err := doc.WriteText(context.Background(), &out, MarkdownOptions{MaxBytes: 0}); err != nil {
		t.Fatalf("MaxBytes=0: %v", err)
	}
	if !bytes.Equal(out.Bytes(), full.Bytes()) {
		t.Errorf("MaxBytes=0 differs from default output")
	}
}

// TestWriteMarkdownMaxBytes verifies the cap applies to Markdown output too
// (WriteText shares the path via Plain).
func TestWriteMarkdownMaxBytes(t *testing.T) {
	doc, err := OpenHTMLBytes([]byte("<html><body><h1>Heading</h1><p>Some paragraph content.</p></body></html>"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	var full, capped bytes.Buffer
	if err := doc.WriteMarkdown(context.Background(), &full, MarkdownOptions{}); err != nil {
		t.Fatalf("WriteMarkdown: %v", err)
	}
	if err := doc.WriteMarkdown(context.Background(), &capped, MarkdownOptions{MaxBytes: 9}); err != nil {
		t.Fatalf("WriteMarkdown capped: %v", err)
	}
	if capped.Len() > 9 || !bytes.HasPrefix(full.Bytes(), capped.Bytes()) {
		t.Errorf("capped output %q (%d bytes) is not a <=9-byte prefix of %q", capped.String(), capped.Len(), full.String())
	}
}

// pdfStub is a minimal renderer that is not a reflowTree, standing in for an opened PDF.
type pdfStub struct{}

func (pdfStub) pageCount() int { return 0 }
func (pdfStub) renderPage(context.Context, int, RasterOptions) (image.Image, error) {
	return nil, nil
}
func (pdfStub) pageSize(int) (float64, float64, error) { return 0, 0, nil }
