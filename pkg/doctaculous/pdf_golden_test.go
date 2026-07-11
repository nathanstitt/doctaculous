package doctaculous

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pdfxGoldens are HTML fixtures rendered to a PDF (via the convertHTMLToPDF helper) and then
// extracted back to Markdown and HTML, compared to committed pdfx-*.md / pdfx-*.html
// goldens. Each exercises one slice of the PDF structure-recovery extractor: heading/
// paragraph classification, list detection, the lattice (ruled) table detector, and a
// two-column (floated) layout's reading order. Run with -update to regenerate, then
// eyeball every golden in review (text present, tables rectangular, headings marked).
var pdfxGoldens = []struct {
	name string
	html string
}{
	{"headings", `<!DOCTYPE html><html><head><style>body{margin:0}
		h1{font-size:32px} h2{font-size:24px}</style></head><body>
		<h1>Quarterly Report</h1>
		<p>This is the introductory paragraph of the report body text.</p>
		<h2>Revenue</h2>
		<p>Revenue grew across every region this quarter.</p>
	</body></html>`},
	{"list", `<!DOCTYPE html><html><head><style>body{margin:0}</style></head><body>
		<p>Shopping list:</p>
		<ul><li>Apples</li><li>Pears</li><li>Cherries</li></ul>
		<p>Steps:</p>
		<ol><li>First step</li><li>Second step</li><li>Third step</li></ol>
	</body></html>`},
	{"table-ruled", `<!DOCTYPE html><html><head><style>body{margin:0}
		table{border-collapse:collapse} td,th{border:1px solid black;padding:6px}</style></head><body>
		<table>
			<tr><th>Name</th><th>Qty</th></tr>
			<tr><td>Apples</td><td>3</td></tr>
			<tr><td>Pears</td><td>7</td></tr>
		</table>
	</body></html>`},
	{"two-column", `<!DOCTYPE html><html><head><style>body{margin:0}
		.col{float:left;width:45%;padding:0 2%}</style></head><body>
		<div class="col"><p>Left column first paragraph.</p><p>Left column second paragraph.</p></div>
		<div class="col"><p>Right column first paragraph.</p><p>Right column second paragraph.</p></div>
	</body></html>`},
	// The specimen: one document exercising the whole extractor end to end (heading,
	// paragraphs, a ruled table, and a list), the PDF-extraction counterpart to the
	// htmldoc raster showcase. Its committed .md/.html are the reviewable artifact.
	{"specimen", `<!DOCTYPE html><html><head><style>body{margin:0}
		h1{font-size:30px} h2{font-size:22px}
		table{border-collapse:collapse} td,th{border:1px solid black;padding:6px}</style></head><body>
		<h1>Field Report</h1>
		<p>This specimen exercises every part of the PDF extraction pipeline in one document.</p>
		<h2>Measurements</h2>
		<table>
			<tr><th>Site</th><th>Depth</th><th>Reading</th></tr>
			<tr><td>North</td><td>10m</td><td>4.2</td></tr>
			<tr><td>South</td><td>15m</td><td>5.8</td></tr>
		</table>
		<h2>Next steps</h2>
		<ol><li>Review the anomalies.</li><li>Schedule a follow-up survey.</li></ol>
	</body></html>`},
}

// TestPDFExtractMarkdownGolden renders each fixture to a PDF, extracts it to Markdown, and
// compares to a committed pdfx-<name>.md golden.
func TestPDFExtractMarkdownGolden(t *testing.T) {
	dir := filepath.Join("testdata", "golden")
	if *update {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range pdfxGoldens {
		t.Run(f.name, func(t *testing.T) {
			pdf := roundTripPDF(t, f.html)
			var out bytes.Buffer
			if err := convertPDFToMarkdown(context.Background(), bytes.NewReader(pdf), &out, MarkdownOptions{}); err != nil {
				t.Fatalf("convertPDFToMarkdown: %v", err)
			}
			checkPDFXGolden(t, filepath.Join(dir, "pdfx-"+f.name+".md"), out.Bytes())
		})
	}
}

// TestPDFExtractHTMLGolden renders each fixture to a PDF, extracts it to HTML, and compares
// to a committed pdfx-<name>.html golden.
func TestPDFExtractHTMLGolden(t *testing.T) {
	dir := filepath.Join("testdata", "golden")
	if *update {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range pdfxGoldens {
		t.Run(f.name, func(t *testing.T) {
			pdf := roundTripPDF(t, f.html)
			var out bytes.Buffer
			if err := convertPDFToHTML(context.Background(), bytes.NewReader(pdf), &out, HTMLWriteOptions{}); err != nil {
				t.Fatalf("convertPDFToHTML: %v", err)
			}
			checkPDFXGolden(t, filepath.Join(dir, "pdfx-"+f.name+".html"), out.Bytes())
		})
	}
}

// checkPDFXGolden writes (with -update) or compares got against the golden at path.
func checkPDFXGolden(t *testing.T, path string, got []byte) {
	t.Helper()
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("updated %s", path)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden %s; run: go test ./pkg/doctaculous -run 'TestPDFExtract.*Golden' -update", path)
	}
	if !bytes.Equal(want, got) {
		t.Errorf("output differs from golden %s\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}

// TestPDFExtractEmptyPage extracts a one-page PDF that carries no recoverable text (only a
// bordered empty box): extraction must not error and must produce (near-)empty output.
//
// Note: a truly whitespace-only body cannot be used here because html→pdf conversion emits a
// zero-page PDF for it, which the parser then rejects with "document has no pages" (see the
// caveat in the accompanying report). A vector-only page is a valid, parseable, text-free
// document that exercises the same empty-extraction path.
func TestPDFExtractEmptyPage(t *testing.T) {
	pdf := roundTripPDF(t, `<!DOCTYPE html><html><head><style>body{margin:0}
		div{width:100px;height:60px;border:2px solid black}</style></head><body><div></div></body></html>`)

	var md bytes.Buffer
	if err := convertPDFToMarkdown(context.Background(), bytes.NewReader(pdf), &md, MarkdownOptions{}); err != nil {
		t.Fatalf("convertPDFToMarkdown on empty page: %v", err)
	}
	if strings.TrimSpace(md.String()) != "" {
		t.Errorf("empty page produced non-empty markdown: %q", md.String())
	}

	var html bytes.Buffer
	if err := convertPDFToHTML(context.Background(), bytes.NewReader(pdf), &html, HTMLWriteOptions{}); err != nil {
		t.Fatalf("convertPDFToHTML on empty page: %v", err)
	}
	// An empty document still emits scaffold, but must carry no body text.
	if got := html.String(); strings.Contains(got, "<p>") || strings.Contains(got, "<table>") {
		t.Errorf("empty page produced body content:\n%s", got)
	}
}

// TestConvertPDFToMarkdownInvalidPDF feeds non-PDF bytes: it must return an error (from the
// parser), not panic.
func TestConvertPDFToMarkdownInvalidPDF(t *testing.T) {
	var out bytes.Buffer
	err := convertPDFToMarkdown(context.Background(), strings.NewReader("this is not a PDF"), &out, MarkdownOptions{})
	if err == nil {
		t.Fatal("expected an error for non-PDF input, got nil")
	}
}
