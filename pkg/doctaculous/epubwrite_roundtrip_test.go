package doctaculous

import (
	"archive/zip"
	"bytes"
	"context"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// epubOf converts an opened document to .epub bytes.
func epubOf(t *testing.T, doc *Document) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := doc.WriteEPUB(context.Background(), &buf, EPUBOptions{}); err != nil {
		t.Fatalf("WriteEPUB: %v", err)
	}
	return buf.Bytes()
}

// TestEPUBWriteRoundTripParity is the writer's core guarantee: for each
// construct, HTML → WriteEPUB → reopen → WriteMarkdown produces the same
// Markdown as converting the HTML directly (EPUB content documents are XHTML,
// so the trip is essentially lossless).
func TestEPUBWriteRoundTripParity(t *testing.T) {
	cases := []struct {
		name string
		html string
	}{
		{"headings", `<html><body><h1>Title</h1><h2>Sub</h2><h6>Deep</h6><p>Body text.</p></body></html>`},
		{"chapter split", `<html><body><h1>One</h1><p>alpha</p><h1>Two</h1><p>beta</p></body></html>`},
		{"preamble", `<html><body><p>before any heading</p><h1>First</h1><p>body</p></body></html>`},
		{"emphasis", `<html><body><p>a <strong>bold</strong> <em>ital</em> <s>gone</s> mix</p></body></html>`},
		{"inline code", `<html><body><p>run <code>go test</code> now</p></body></html>`},
		{"link", `<html><body><p>see <a href="https://x.test/page">the docs</a>.</p></body></html>`},
		{"bullet list", `<html><body><ul><li>one</li><li>two</li></ul></body></html>`},
		{"ordered list", `<html><body><ol><li>first</li><li>second</li></ol></body></html>`},
		{"nested list", `<html><body><ul><li>outer<ul><li>inner</li></ul></li></ul></body></html>`},
		{"blockquote", `<html><body><blockquote><p>a quoted line</p></blockquote></body></html>`},
		{"code block", "<html><body><pre>line one\nline two</pre></body></html>"},
		{"rule", `<html><body><p>above</p><hr><p>below</p></body></html>`},
		{"unicode text", `<html><body><p>naïve café — em•dash</p></body></html>`},
		{"table", `<html><body><table>
			<tr><th>Item</th><th>Qty</th></tr>
			<tr><td>Widgets</td><td>5</td></tr>
			</table></body></html>`},
		{"table colspan", `<html><body><table>
			<tr><th colspan="2">Wide Header</th></tr>
			<tr><td>a</td><td>b</td></tr>
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

			reopened, err := OpenBytes(epubOf(t, src), WithBundledFonts())
			if err != nil {
				t.Fatalf("reopening produced epub: %v", err)
			}
			if reopened.Format() != FormatEPUB {
				t.Fatalf("produced epub detected as %s", reopened.Format())
			}
			got := markdownOf(t, reopened)
			if got != want {
				t.Errorf("round trip diverged:\n--- direct ---\n%s\n--- via epub ---\n%s", want, got)
			}
		})
	}
}

// TestEPUBWritePackageShape pins the container invariants: the stored
// mimetype first entry, chapter split, the nav TOC, and the fetched-image
// manifest path.
func TestEPUBWritePackageShape(t *testing.T) {
	html := `<html><body><h1>One</h1><p>alpha</p><h1>Two</h1><p>beta</p></body></html>`
	src, err := OpenHTMLBytes([]byte(html), WithBundledFonts())
	if err != nil {
		t.Fatalf("OpenHTMLBytes: %v", err)
	}
	book := epubOf(t, src)

	zr, err := zip.NewReader(bytes.NewReader(book), int64(len(book)))
	if err != nil {
		t.Fatalf("produced epub is not a zip: %v", err)
	}
	first := zr.File[0]
	if first.Name != "mimetype" || first.Method != zip.Store {
		t.Errorf("first entry = %q (method %d), want stored mimetype", first.Name, first.Method)
	}
	names := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		var b bytes.Buffer
		if _, err := b.ReadFrom(rc); err != nil {
			t.Fatal(err)
		}
		_ = rc.Close()
		names[f.Name] = b.Bytes()
	}
	if string(names["mimetype"]) != "application/epub+zip" {
		t.Errorf("mimetype = %q", names["mimetype"])
	}
	for _, want := range []string{"META-INF/container.xml", "OEBPS/content.opf", "OEBPS/nav.xhtml", "OEBPS/chap1.xhtml", "OEBPS/chap2.xhtml"} {
		if _, ok := names[want]; !ok {
			t.Errorf("part %s missing", want)
		}
	}
	if !bytes.Contains(names["OEBPS/content.opf"], []byte("<dc:title>One</dc:title>")) {
		t.Errorf("title not derived from the first heading:\n%s", names["OEBPS/content.opf"])
	}
	nav := string(names["OEBPS/nav.xhtml"])
	if !strings.Contains(nav, `<a href="chap1.xhtml">One</a>`) || !strings.Contains(nav, `<a href="chap2.xhtml">Two</a>`) {
		t.Errorf("nav TOC missing chapter links:\n%s", nav)
	}
	// Two chapters => two paginated pages when opened with a page size.
	reopened, err := OpenBytes(book, WithBundledFonts(), WithPageSize(LetterWidthPt, LetterHeightPt))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if n := reopened.PageCount(); n != 2 {
		t.Errorf("pages = %d, want 2 (one per chapter)", n)
	}
}

// TestMarkdownToEPUBAndBack closes the loop from the Markdown frontend.
func TestMarkdownToEPUBAndBack(t *testing.T) {
	md := "# Doc Title\n\nBody with **bold** and _italic_ and a [link](https://x.test/p).\n\n" +
		"- one\n- two\n\n> quoted\n\n```\ncode here\n```\n"
	src, err := OpenMarkdownBytes([]byte(md), WithBundledFonts())
	if err != nil {
		t.Fatalf("OpenMarkdownBytes: %v", err)
	}
	reopened, err := OpenBytes(epubOf(t, src), WithBundledFonts())
	if err != nil {
		t.Fatalf("reopening produced epub: %v", err)
	}
	got := markdownOf(t, reopened)
	for _, want := range []string{
		"# Doc Title", "**bold**", "_italic_", "[link](https://x.test/p)",
		"- one", "> quoted", "```\ncode here\n```",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("md->epub->md lost %q:\n%s", want, got)
		}
	}
}

// TestEPUBOutputGolden renders a reopened writer-produced book — the visual
// smoke check that a generated .epub lays out and rasterizes sanely.
func TestEPUBOutputGolden(t *testing.T) {
	const src = `<html><body>
<h1>EPUB Output</h1>
<p>A paragraph with <strong>bold</strong>, <em>italic</em>, and <a href="https://x.test">a link</a>.</p>
<ul><li>bullet one</li><li>bullet two</li></ul>
<blockquote><p>a quoted line</p></blockquote>
<pre>code line</pre>
<table><tr><th>K</th><th>V</th></tr><tr><td>a</td><td>b</td></tr></table>
</body></html>`
	htmlDoc, err := OpenHTMLBytes([]byte(src), WithBundledFonts())
	if err != nil {
		t.Fatalf("OpenHTMLBytes: %v", err)
	}
	reopened, err := OpenBytes(epubOf(t, htmlDoc), WithBundledFonts())
	if err != nil {
		t.Fatalf("reopening produced epub: %v", err)
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
	path := filepath.Join(dir, "epubout-basic.png")
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
		t.Fatalf("missing golden %s; run: go test ./pkg/doctaculous -run TestEPUBOutputGolden -update", path)
	}
	if diff, n := compareImages(want, got); diff {
		t.Errorf("render differs from golden %s: %d pixels beyond tolerance", path, n)
	}
}
