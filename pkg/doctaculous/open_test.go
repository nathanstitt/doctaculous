package doctaculous

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/nathanstitt/doctaculous/testdata/gen"
	gendocx "github.com/nathanstitt/doctaculous/testdata/gen/docx"
)

func writeTempFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

// TestOpenSniffs verifies the generic Open detects the format from content,
// regardless of the file's extension.
func TestOpenSniffs(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want Format
	}{
		{"document.pdf", gen.TextPDF(), FormatPDF},
		{"mislabeled.dat", gen.TextPDF(), FormatPDF}, // magic beats the missing extension
		{"report.docx", gendocx.Core[0].Bytes(), FormatDOCX},
		{"mislabeled.bin", gendocx.Core[0].Bytes(), FormatDOCX},
		{"page.html", []byte("<html><body><p>hi</p></body></html>"), FormatHTML},
		{"noext-html", []byte("<!DOCTYPE html><p>hi</p>"), FormatHTML},
		// Extension-only formats (no content magic).
		{"notes.md", []byte("# hi\n\nbody\n"), FormatMarkdown},
		{"notes.txt", []byte("line one\nline two\n"), FormatText},
	}
	for _, c := range cases {
		path := writeTempFile(t, c.name, c.data)
		doc, err := Open(path)
		if err != nil {
			t.Errorf("%s: Open: %v", c.name, err)
			continue
		}
		if doc.Format() != c.want {
			t.Errorf("%s: Format() = %q, want %q", c.name, doc.Format(), c.want)
		}
		if doc.PageCount() < 1 {
			t.Errorf("%s: PageCount() = %d, want >= 1", c.name, doc.PageCount())
		}
	}
}

func TestOpenErrors(t *testing.T) {
	// Undetectable content -> ErrUnknownFormat.
	garbage := writeTempFile(t, "noise.bin", []byte{0x00, 0x01, 0xFE, 0xBA})
	if _, err := Open(garbage); !errors.Is(err, ErrUnknownFormat) {
		t.Errorf("Open(garbage): want ErrUnknownFormat, got %v", err)
	}
	// An image is a recognized format but not an input.
	png := writeTempFile(t, "img.png", encodeTinyImage(t, FormatPNG))
	if _, err := Open(png); !errors.Is(err, ErrUnsupportedFormat) {
		t.Errorf("Open(.png): want ErrUnsupportedFormat, got %v", err)
	}
	// Missing file surfaces the underlying error.
	if _, err := Open(filepath.Join(t.TempDir(), "absent.pdf")); err == nil {
		t.Errorf("Open(absent): want error, got nil")
	}
	if _, err := OpenBytes([]byte("just some plain prose")); !errors.Is(err, ErrUnknownFormat) {
		t.Errorf("OpenBytes(prose): want ErrUnknownFormat, got nil-or-wrong error")
	}
}

func TestOpenAs(t *testing.T) {
	// OpenAs forces the format, skipping detection: a PDF hiding behind a
	// nonsense extension opens when named explicitly.
	path := writeTempFile(t, "data.blob", gen.TextPDF())
	doc, err := OpenAs(FormatPDF, path)
	if err != nil {
		t.Fatalf("OpenAs(pdf): %v", err)
	}
	if doc.Format() != FormatPDF {
		t.Errorf("Format() = %q, want pdf", doc.Format())
	}
	// Forcing the wrong format fails in that format's parser.
	if _, err := OpenAs(FormatDOCX, path); err == nil {
		t.Errorf("OpenAs(docx) on a PDF: want error, got nil")
	}
	// Image formats are not inputs.
	if _, err := OpenAs(FormatPNG, path); !errors.Is(err, ErrUnsupportedFormat) {
		t.Errorf("OpenAs(png): want ErrUnsupportedFormat, got %v", err)
	}
	// A URL is only openable as HTML.
	if _, err := OpenAs(FormatPDF, "http://example.invalid/x"); err == nil {
		t.Errorf("OpenAs(pdf, url): want error, got nil")
	}
	if _, err := OpenBytesAs(FormatUnknown, gen.TextPDF()); !errors.Is(err, ErrUnknownFormat) {
		t.Errorf("OpenBytesAs(unknown): want ErrUnknownFormat")
	}
}

// TestOpenURLRouting verifies Open on an http(s) URL fetches and renders it as
// a web page, stamped FormatHTML.
func TestOpenURLRouting(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html><body><h1>served</h1></body></html>"))
	}))
	defer srv.Close()

	doc, err := Open(srv.URL)
	if err != nil {
		t.Fatalf("Open(url): %v", err)
	}
	if doc.Format() != FormatHTML {
		t.Errorf("Format() = %q, want html", doc.Format())
	}
}

// TestDocumentFormatStamped verifies every opener records its source format.
func TestDocumentFormatStamped(t *testing.T) {
	pdfBytes := gen.TextPDF()
	docxBytes := gendocx.Core[0].Bytes()
	htmlBytes := []byte("<html><body><p>x</p></body></html>")

	docxPath := writeTempFile(t, "f.docx", docxBytes)
	htmlPath := writeTempFile(t, "f.html", htmlBytes)

	cases := []struct {
		name string
		open func() (*Document, error)
		want Format
	}{
		{"OpenBytes pdf", func() (*Document, error) { return OpenBytes(pdfBytes) }, FormatPDF},
		{"OpenDOCXBytes", func() (*Document, error) { return OpenDOCXBytes(docxBytes) }, FormatDOCX},
		{"OpenHTMLBytes", func() (*Document, error) { return OpenHTMLBytes(htmlBytes) }, FormatHTML},
		{"OpenDOCX", func() (*Document, error) { return OpenDOCX(docxPath) }, FormatDOCX},
		{"OpenHTMLFile", func() (*Document, error) { return OpenHTMLFile(htmlPath) }, FormatHTML},
		{"OpenBytesAs", func() (*Document, error) { return OpenBytesAs(FormatPDF, pdfBytes) }, FormatPDF},
	}
	for _, c := range cases {
		doc, err := c.open()
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if doc.Format() != c.want {
			t.Errorf("%s: Format() = %q, want %q", c.name, doc.Format(), c.want)
		}
	}
}
