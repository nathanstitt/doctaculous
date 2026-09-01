package omnidoc_test

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/nathanstitt/omnidoc/pkg/omnidoc"
)

// Convert an in-memory document from one format to another.
//
// The input format is detected from content when ConvertOptions.From is unset.
// Markdown and plain text have no content magic, so converting FROM either
// in-memory needs an explicit From — HTML, as here, is detected.
func ExampleConvert() {
	html := strings.NewReader(`<h1>Quarterly report</h1><p>Revenue is <b>up</b>.</p>`)

	var out bytes.Buffer
	err := omnidoc.Convert(context.Background(), html, &out, omnidoc.ConvertOptions{
		To: omnidoc.FormatMarkdown,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(strings.TrimSpace(out.String()))
	// Output:
	// # Quarterly report
	//
	// Revenue is **up**.
}

// Naming the input format explicitly, which is required for Markdown and plain
// text input: neither has content magic for detection to key on.
func ExampleConvert_explicitFormat() {
	md := strings.NewReader("# Title\n\nA paragraph with *emphasis*.\n")

	var out bytes.Buffer
	err := omnidoc.Convert(context.Background(), md, &out, omnidoc.ConvertOptions{
		From: omnidoc.FormatMarkdown,
		To:   omnidoc.FormatText,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(strings.TrimSpace(out.String()))
	// Output:
	// Title
	//
	// A paragraph with emphasis.
}

// Convert a file on disk. Both formats are inferred from the filenames, so the
// common case needs no options at all.
func ExampleConvertFile() {
	dir, err := os.MkdirTemp("", "omnidoc-example")
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	in := filepath.Join(dir, "notes.html")
	if err := os.WriteFile(in, []byte(`<h2>Agenda</h2><ul><li>Budget</li></ul>`), 0o600); err != nil {
		log.Fatal(err)
	}

	out := filepath.Join(dir, "notes.md")
	if err := omnidoc.ConvertFile(context.Background(), in, out, omnidoc.ConvertOptions{}); err != nil {
		log.Fatal(err)
	}

	got, err := os.ReadFile(out)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(strings.TrimSpace(string(got)))
	// Output:
	// ## Agenda
	//
	// - Budget
}

// Open a document and inspect it without rendering. Open detects the format from
// content and filename, so the same call handles a PDF, a DOCX, or an HTML file
// regardless of how it is named.
func ExampleOpen() {
	dir, err := os.MkdirTemp("", "omnidoc-example")
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	path := filepath.Join(dir, "page.html")
	body := `<html><body style="margin:0">
		<h1>Chapter one</h1>
		<p>Some prose.</p>
	</body></html>`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		log.Fatal(err)
	}

	doc, err := omnidoc.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("format:", doc.Format())
	fmt.Println("pages:", doc.PageCount())
	// Output:
	// format: html
	// pages: 1
}

// Render one page to an image. Page indexes are zero-based, and DPI defaults to
// 150 when left zero.
//
// An HTML document opened this way is laid out as a web page — one tall page the
// width of the viewport — so the image is the viewport width by the content height.
// Use WithDefaultPaged (or a CSS @page rule) to paginate onto paper instead.
func ExampleDocument_RasterizePage() {
	doc, err := omnidoc.OpenHTMLBytes([]byte(
		`<html><body style="margin:0;height:200px">hello</body></html>`))
	if err != nil {
		log.Fatal(err)
	}
	img, err := doc.RasterizePage(context.Background(), 0, omnidoc.RasterOptions{DPI: 72})
	if err != nil {
		log.Fatal(err)
	}

	b := img.Bounds()
	fmt.Printf("rendered %dx%d at 72 DPI\n", b.Dx(), b.Dy())
	// Output:
	// rendered 1280x200 at 72 DPI
}

// Paginate an HTML document onto paper and write it as a PDF — the usual shape of
// an HTML-to-PDF conversion when you want page breaks rather than one tall page.
func ExampleOpenHTMLBytes_paged() {
	page := `<html><body>
		<h1>Report</h1>
		<p style="page-break-after:always">First page.</p>
		<p>Second page.</p>
	</body></html>`

	doc, err := omnidoc.OpenHTMLBytes([]byte(page), omnidoc.WithDefaultPaged())
	if err != nil {
		log.Fatal(err)
	}

	var pdf bytes.Buffer
	if err := doc.WritePDF(context.Background(), &pdf, omnidoc.PDFOptions{}); err != nil {
		log.Fatal(err)
	}

	fmt.Println("pages:", doc.PageCount())
	fmt.Println("starts with %PDF:", bytes.HasPrefix(pdf.Bytes(), []byte("%PDF")))
	// Output:
	// pages: 2
	// starts with %PDF: true
}
