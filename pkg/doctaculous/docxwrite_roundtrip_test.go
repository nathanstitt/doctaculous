package doctaculous

import (
	"bytes"
	"context"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// docxOf converts an opened document to .docx bytes.
func docxOf(t *testing.T, doc *Document) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := doc.WriteDOCX(context.Background(), &buf, DOCXOptions{}); err != nil {
		t.Fatalf("WriteDOCX: %v", err)
	}
	return buf.Bytes()
}

// markdownOf converts an opened document to Markdown.
func markdownOf(t *testing.T, doc *Document) string {
	t.Helper()
	var buf bytes.Buffer
	if err := doc.WriteMarkdown(context.Background(), &buf, MarkdownOptions{}); err != nil {
		t.Fatalf("WriteMarkdown: %v", err)
	}
	return buf.String()
}

// TestDOCXWriteRoundTripParity is the writer's core guarantee: for each
// construct, HTML → WriteDOCX → reopen → WriteMarkdown produces the same
// Markdown as converting the HTML directly. Anything the .docx failed to carry
// (a lost heading level, a doubled list marker, a dropped link) shows up as a
// diff.
func TestDOCXWriteRoundTripParity(t *testing.T) {
	cases := []struct {
		name string
		html string
	}{
		{"headings", `<html><body><h1>Title</h1><h2>Sub</h2><h6>Deep</h6><p>Body text.</p></body></html>`},
		{"emphasis", `<html><body><p>a <strong>bold</strong> <em>ital</em> <s>gone</s> mix</p></body></html>`},
		{"inline code", `<html><body><p>run <code>go test</code> now</p></body></html>`},
		{"link", `<html><body><p>see <a href="https://x.test/page">the docs</a>.</p></body></html>`},
		{"bullet list", `<html><body><ul><li>one</li><li>two</li></ul></body></html>`},
		{"ordered list", `<html><body><ol><li>first</li><li>second</li></ol></body></html>`},
		{"nested list", `<html><body><ul><li>outer<ul><li>inner</li></ul></li></ul></body></html>`},
		{"two ordered lists", `<html><body><ol><li>a</li></ol><p>gap</p><ol><li>b</li></ol></body></html>`},
		{"blockquote", `<html><body><blockquote><p>a quoted line</p></blockquote></body></html>`},
		{"code block", "<html><body><pre>line one\nline two</pre></body></html>"},
		{"rule", `<html><body><p>above</p><hr><p>below</p></body></html>`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src, err := OpenHTMLBytes([]byte(c.html), WithBundledFonts())
			if err != nil {
				t.Fatalf("OpenHTMLBytes: %v", err)
			}
			want := markdownOf(t, src)

			reopened, err := OpenDOCXBytes(docxOf(t, src))
			if err != nil {
				t.Fatalf("OpenDOCXBytes: %v", err)
			}
			got := markdownOf(t, reopened)
			if got != want {
				t.Errorf("round trip diverged:\n--- direct ---\n%s\n--- via docx ---\n%s", want, got)
			}
		})
	}
}

// TestMarkdownToDOCXAndBack closes the loop from the Markdown frontend: a GFM
// specimen → .docx → Markdown must preserve every construct's content.
func TestMarkdownToDOCXAndBack(t *testing.T) {
	md := "# Doc Title\n\nBody with **bold** and _italic_ and a [link](https://x.test/p).\n\n" +
		"- one\n- two\n\n1. first\n2. second\n\n> quoted\n\n```\ncode here\n```\n"
	src, err := OpenMarkdownBytes([]byte(md), WithBundledFonts())
	if err != nil {
		t.Fatalf("OpenMarkdownBytes: %v", err)
	}
	reopened, err := OpenDOCXBytes(docxOf(t, src))
	if err != nil {
		t.Fatalf("OpenDOCXBytes: %v", err)
	}
	got := markdownOf(t, reopened)
	for _, want := range []string{
		"# Doc Title",
		"**bold**",
		"_italic_",
		"[link](https://x.test/p)",
		"- one",
		"1. first",
		"2. second",
		"> quoted",
		"```\ncode here\n```",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("md->docx->md lost %q:\n%s", want, got)
		}
	}
}

// TestPDFToDOCX proves the extraction path: a PDF's recovered structure writes
// to .docx and the text survives a reopen.
func TestPDFToDOCX(t *testing.T) {
	pdfDoc, err := OpenBytes(matrixPDF(t))
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	reopened, err := OpenDOCXBytes(docxOf(t, pdfDoc))
	if err != nil {
		t.Fatalf("OpenDOCXBytes: %v", err)
	}
	got := markdownOf(t, reopened)
	if !strings.Contains(got, "Matrix Title") {
		t.Errorf("pdf->docx lost the extracted text:\n%s", got)
	}
}

// TestDOCXOutputGolden renders a reopened writer-produced document — the visual
// smoke check that a generated .docx lays out and rasterizes sanely.
func TestDOCXOutputGolden(t *testing.T) {
	const src = `<html><body>
<h1>DOCX Output</h1>
<p>A paragraph with <strong>bold</strong>, <em>italic</em>, and <a href="https://x.test">a link</a>.</p>
<ul><li>bullet one</li><li>bullet two</li></ul>
<ol><li>number one</li><li>number two</li></ol>
<blockquote><p>a quoted line</p></blockquote>
<pre>code line</pre>
</body></html>`
	htmlDoc, err := OpenHTMLBytes([]byte(src), WithBundledFonts())
	if err != nil {
		t.Fatalf("OpenHTMLBytes: %v", err)
	}
	reopened, err := OpenDOCXBytes(docxOf(t, htmlDoc))
	if err != nil {
		t.Fatalf("OpenDOCXBytes: %v", err)
	}
	img, err := reopened.RasterizePage(context.Background(), 0, RasterOptions{DPI: goldenDPI, BundledFonts: true})
	if err != nil {
		t.Fatalf("RasterizePage: %v", err)
	}
	got, ok := img.(*image.RGBA)
	if !ok {
		t.Fatalf("rasterized image is %T, want *image.RGBA", img)
	}

	dir := filepath.Join("testdata", "golden")
	path := filepath.Join(dir, "docxout-basic.png")
	if *update {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		writePNG(t, path, got)
		t.Logf("updated %s", path)
		return
	}
	want := readPNG(t, path)
	if want == nil {
		t.Fatalf("missing golden %s; run: go test ./pkg/doctaculous -run TestDOCXOutputGolden -update", path)
	}
	if diff, n := compareImages(want, got); diff {
		t.Errorf("render differs from golden %s: %d pixels beyond tolerance", path, n)
	}
}
