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

// pptxOf converts an opened document to .pptx bytes.
func pptxOf(t *testing.T, doc *Document) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := doc.WritePPTX(context.Background(), &buf, PPTXOptions{}); err != nil {
		t.Fatalf("WritePPTX: %v", err)
	}
	return buf.Bytes()
}

// TestPPTXWriteRoundTrip pins the writer's content contract: HTML → WritePPTX
// → reopen through our own reader → Markdown preserves every construct's
// content. Unlike the DOCX/RTF writers this is not byte-parity with the
// direct conversion — a document REFLOWS into the slide model (h1/h2 both
// become slide titles, quote/code semantics flatten) — so the pin is
// content + structure fragments, per construct.
func TestPPTXWriteRoundTrip(t *testing.T) {
	cases := []struct {
		name  string
		html  string
		wants []string
	}{
		{
			"slides split at headings",
			`<html><body><h1>First Slide</h1><p>alpha body</p><h2>Second Slide</h2><p>beta body</p></body></html>`,
			[]string{"## First Slide", "alpha body", "## Second Slide", "beta body"},
		},
		{
			"emphasis",
			`<html><body><p>a <strong>bold</strong> and <em>ital</em> mix</p></body></html>`,
			[]string{"**bold**", "_ital_"},
		},
		{
			"bullet list",
			`<html><body><ul><li>one</li><li>two</li></ul></body></html>`,
			[]string{"- one", "- two"},
		},
		{
			"ordered list",
			`<html><body><ol><li>first</li><li>second</li></ol></body></html>`,
			[]string{"1. first", "2. second"},
		},
		{
			"nested list",
			`<html><body><ul><li>outer<ul><li>inner</li></ul></li></ul></body></html>`,
			[]string{"- outer", "  - inner"},
		},
		{
			"table with spans",
			`<html><body><table>
			<tr><th>Item</th><th>Qty</th></tr>
			<tr><td>Widgets</td><td>5</td></tr>
			<tr><td colspan="2">wide total</td></tr>
			</table></body></html>`,
			[]string{"Item", "Qty", "| Widgets | 5 |", "| wide total | wide total |"},
		},
		{
			"untitled content before a heading",
			`<html><body><p>preamble text</p><h1>Titled</h1><p>body</p></body></html>`,
			[]string{"preamble text", "## Titled", "body"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src, err := OpenHTMLBytes([]byte(c.html), WithBundledFonts())
			if err != nil {
				t.Fatalf("OpenHTMLBytes: %v", err)
			}
			reopened, err := OpenBytes(pptxOf(t, src), WithBundledFonts())
			if err != nil {
				t.Fatalf("reopening produced pptx: %v", err)
			}
			if reopened.Format() != FormatPPTX {
				t.Fatalf("produced pptx detected as %s", reopened.Format())
			}
			got := markdownOf(t, reopened)
			for _, want := range c.wants {
				if !strings.Contains(got, want) {
					t.Errorf("round trip lost %q:\n%s", want, got)
				}
			}
		})
	}
}

// TestPPTXWriteImage pins that an embedded image survives into the deck as a
// media part and back out through the reader.
func TestPPTXWriteImage(t *testing.T) {
	html := `<html><body><h1>Pics</h1><p><img src="` + parityImageURI(t) + `" alt="blue square"></p></body></html>`
	src, err := OpenHTMLBytes([]byte(html), WithBundledFonts())
	if err != nil {
		t.Fatalf("OpenHTMLBytes: %v", err)
	}
	deck := pptxOf(t, src)
	reopened, err := OpenBytes(deck, WithBundledFonts())
	if err != nil {
		t.Fatalf("reopening produced pptx: %v", err)
	}
	got := markdownOf(t, reopened)
	if !strings.Contains(got, "data:image/png;base64,") {
		t.Errorf("image did not survive the round trip:\n%.400s", got)
	}
}

// TestPPTXWriteSlideCount pins the slide-splitting rule at the package level:
// three h1/h2 headings produce three slides.
func TestPPTXWriteSlideCount(t *testing.T) {
	html := `<html><body><h1>A</h1><p>1</p><h2>B</h2><p>2</p><h1>C</h1><p>3</p></body></html>`
	src, err := OpenHTMLBytes([]byte(html), WithBundledFonts())
	if err != nil {
		t.Fatalf("OpenHTMLBytes: %v", err)
	}
	reopened, err := OpenBytes(pptxOf(t, src), WithBundledFonts())
	if err != nil {
		t.Fatalf("reopening produced pptx: %v", err)
	}
	if n := reopened.PageCount(); n != 3 {
		t.Errorf("slides = %d, want 3", n)
	}
}

// TestPPTXOutputGolden renders a reopened writer-produced deck — the visual
// smoke check that a generated .pptx lays out and rasterizes sanely.
func TestPPTXOutputGolden(t *testing.T) {
	const src = `<html><body>
<h1>PPTX Output</h1>
<p>A paragraph with <strong>bold</strong> and <em>italic</em>.</p>
<ul><li>bullet one</li><li>bullet two</li></ul>
<ol><li>number one</li><li>number two</li></ol>
<table><tr><th>K</th><th>V</th></tr><tr><td>a</td><td>b</td></tr></table>
</body></html>`
	htmlDoc, err := OpenHTMLBytes([]byte(src), WithBundledFonts())
	if err != nil {
		t.Fatalf("OpenHTMLBytes: %v", err)
	}
	reopened, err := OpenBytes(pptxOf(t, htmlDoc), WithBundledFonts())
	if err != nil {
		t.Fatalf("reopening produced pptx: %v", err)
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
	path := filepath.Join(dir, "pptxout-basic.png")
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
		t.Fatalf("missing golden %s; run: go test ./pkg/doctaculous -run TestPPTXOutputGolden -update", path)
	}
	if diff, n := compareImages(want, got); diff {
		t.Errorf("render differs from golden %s: %d pixels beyond tolerance", path, n)
	}
}
