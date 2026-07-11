package doctaculous

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// rtfOf converts an opened document to RTF bytes.
func rtfOf(t *testing.T, doc *Document) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := doc.WriteRTF(context.Background(), &buf, RTFOptions{}); err != nil {
		t.Fatalf("WriteRTF: %v", err)
	}
	return buf.Bytes()
}

// parityImageURI is a tiny blue PNG as a data URI (standard base64, so the
// round trip re-encodes to the identical string).
func parityImageURI(t *testing.T) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for i := range img.Pix {
		img.Pix[i] = 0xFF
	}
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.SetRGBA(x, y, color.RGBA{B: 0xC8, A: 0xFF})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

// TestRTFWriteRoundTripParity is the writer's core guarantee: for each
// construct, HTML → WriteRTF → reopen → WriteMarkdown produces the same
// Markdown as converting the HTML directly. Anything the RTF failed to carry
// (a lost heading level, a doubled list marker, a dropped link) shows up as a
// diff.
func TestRTFWriteRoundTripParity(t *testing.T) {
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
		{"unicode text", `<html><body><p>naïve café — em•dash</p></body></html>`},
		{"table", `<html><body><table>
			<tr><th>Item</th><th>Qty</th></tr>
			<tr><td>Widgets</td><td>5</td></tr>
			<tr><td>Gadgets</td><td>7</td></tr>
			</table></body></html>`},
		{"table colspan", `<html><body><table>
			<tr><th colspan="2">Wide Header</th></tr>
			<tr><td>a</td><td>b</td></tr>
			</table></body></html>`},
		{"table rowspan", `<html><body><table>
			<tr><th>K</th><th>V</th></tr>
			<tr><td rowspan="2">tall</td><td>r1</td></tr>
			<tr><td>r2</td></tr>
			</table></body></html>`},
		{"table caption", `<html><body><table>
			<caption>Quarterly</caption>
			<tr><th>Q</th></tr><tr><td>1</td></tr>
			</table></body></html>`},
		{"image", `<html><body><p>before <img src="` + parityImageURI(t) + `"> after</p></body></html>`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src, err := OpenHTMLBytes([]byte(c.html), WithBundledFonts())
			if err != nil {
				t.Fatalf("OpenHTMLBytes: %v", err)
			}
			want := markdownOf(t, src)

			reopened, err := OpenBytes(rtfOf(t, src), WithBundledFonts())
			if err != nil {
				t.Fatalf("reopening produced rtf: %v", err)
			}
			if reopened.Format() != FormatRTF {
				t.Fatalf("produced rtf detected as %s", reopened.Format())
			}
			got := markdownOf(t, reopened)
			if got != want {
				t.Errorf("round trip diverged:\n--- direct ---\n%s\n--- via rtf ---\n%s", want, got)
			}
		})
	}
}

// TestMarkdownToRTFAndBack closes the loop from the Markdown frontend: a GFM
// specimen → RTF → Markdown must preserve every construct's content.
func TestMarkdownToRTFAndBack(t *testing.T) {
	md := "# Doc Title\n\nBody with **bold** and _italic_ and a [link](https://x.test/p).\n\n" +
		"- one\n- two\n\n1. first\n2. second\n\n> quoted\n\n```\ncode here\n```\n"
	src, err := OpenMarkdownBytes([]byte(md), WithBundledFonts())
	if err != nil {
		t.Fatalf("OpenMarkdownBytes: %v", err)
	}
	reopened, err := OpenBytes(rtfOf(t, src), WithBundledFonts())
	if err != nil {
		t.Fatalf("reopening produced rtf: %v", err)
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
			t.Errorf("md->rtf->md lost %q:\n%s", want, got)
		}
	}
}

// TestPDFToRTF proves the extraction path: a PDF's recovered structure writes
// to RTF and the text survives a reopen.
func TestPDFToRTF(t *testing.T) {
	pdfDoc, err := OpenBytes(matrixPDF(t))
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	reopened, err := OpenBytes(rtfOf(t, pdfDoc))
	if err != nil {
		t.Fatalf("reopening produced rtf: %v", err)
	}
	got := markdownOf(t, reopened)
	if !strings.Contains(got, "Matrix Title") {
		t.Errorf("pdf->rtf lost the extracted text:\n%s", got)
	}
}

// TestRTFOutputGolden renders a reopened writer-produced document — the visual
// smoke check that generated RTF lays out and rasterizes sanely.
func TestRTFOutputGolden(t *testing.T) {
	const src = `<html><body>
<h1>RTF Output</h1>
<p>A paragraph with <strong>bold</strong>, <em>italic</em>, and <a href="https://x.test">a link</a>.</p>
<ul><li>bullet one</li><li>bullet two</li></ul>
<ol><li>number one</li><li>number two</li></ol>
<blockquote><p>a quoted line</p></blockquote>
<pre>code line</pre>
<table><tr><th>K</th><th>V</th></tr><tr><td>a</td><td>b</td></tr></table>
</body></html>`
	htmlDoc, err := OpenHTMLBytes([]byte(src), WithBundledFonts())
	if err != nil {
		t.Fatalf("OpenHTMLBytes: %v", err)
	}
	reopened, err := OpenBytes(rtfOf(t, htmlDoc), WithBundledFonts())
	if err != nil {
		t.Fatalf("reopening produced rtf: %v", err)
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
	path := filepath.Join(dir, "rtfout-basic.png")
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
		t.Fatalf("missing golden %s; run: go test ./pkg/doctaculous -run TestRTFOutputGolden -update", path)
	}
	if diff, n := compareImages(want, got); diff {
		t.Errorf("render differs from golden %s: %d pixels beyond tolerance", path, n)
	}
}
