package doctaculous

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"

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
	// An image opens as a single-page document at its pixel size.
	png := writeTempFile(t, "img.png", encodeTinyImage(t, FormatPNG))
	if doc, err := Open(png); err != nil || doc.Format() != FormatPNG {
		t.Errorf("Open(.png): got (%v, %v), want a FormatPNG document", doc, err)
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
	// Forcing an image format on non-image bytes fails in the decoder.
	if _, err := OpenAs(FormatPNG, path); err == nil {
		t.Errorf("OpenAs(png) on a PDF: want a decode error, got nil")
	}
	// A URL is only openable as HTML.
	if _, err := OpenAs(FormatPDF, "http://example.invalid/x"); err == nil {
		t.Errorf("OpenAs(pdf, url): want error, got nil")
	}
	if _, err := OpenBytesAs(FormatUnknown, gen.TextPDF()); !errors.Is(err, ErrUnknownFormat) {
		t.Errorf("OpenBytesAs(unknown): want ErrUnknownFormat")
	}
}

// TestOpenReader verifies the stream entry point detects the format from
// content (no filename hint exists), fully buffering the reader.
func TestOpenReader(t *testing.T) {
	doc, err := OpenReader(context.Background(), bytes.NewReader(gen.TextPDF()))
	if err != nil {
		t.Fatalf("OpenReader(pdf stream): %v", err)
	}
	if doc.Format() != FormatPDF {
		t.Errorf("Format() = %q, want pdf", doc.Format())
	}
	if doc.PageCount() < 1 {
		t.Errorf("PageCount() = %d, want >= 1", doc.PageCount())
	}
	// Extension-only formats have no content magic on a stream.
	if _, err := OpenReader(context.Background(), strings.NewReader("# just markdown\n")); !errors.Is(err, ErrUnknownFormat) {
		t.Errorf("OpenReader(markdown stream): want ErrUnknownFormat, got %v", err)
	}
}

// TestOpenReaderAs covers the MIME-driven composition the drive integration
// uses: FormatFromMIME names the format, the stream supplies the bytes.
func TestOpenReaderAs(t *testing.T) {
	doc, err := OpenReaderAs(context.Background(), FormatMarkdown, strings.NewReader("# Title\n\nbody\n"))
	if err != nil {
		t.Fatalf("OpenReaderAs(markdown): %v", err)
	}
	if doc.Format() != FormatMarkdown {
		t.Errorf("Format() = %q, want markdown", doc.Format())
	}

	f := FormatFromMIME("application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	doc, err = OpenReaderAs(context.Background(), f, bytes.NewReader(gendocx.Core[0].Bytes()))
	if err != nil {
		t.Fatalf("OpenReaderAs(FormatFromMIME(docx)): %v", err)
	}
	if doc.Format() != FormatDOCX {
		t.Errorf("Format() = %q, want docx", doc.Format())
	}

	if _, err := OpenReaderAs(context.Background(), FormatUnknown, strings.NewReader("x")); !errors.Is(err, ErrUnknownFormat) {
		t.Errorf("OpenReaderAs(unknown): want ErrUnknownFormat, got %v", err)
	}
}

// TestOpenReaderReadError verifies a failing reader surfaces its error from
// both stream entry points.
func TestOpenReaderReadError(t *testing.T) {
	sentinel := errors.New("stream broke")
	if _, err := OpenReader(context.Background(), iotest.ErrReader(sentinel)); !errors.Is(err, sentinel) {
		t.Errorf("OpenReader(ErrReader): want the read error, got %v", err)
	}
	if _, err := OpenReaderAs(context.Background(), FormatPDF, iotest.ErrReader(sentinel)); !errors.Is(err, sentinel) {
		t.Errorf("OpenReaderAs(ErrReader): want the read error, got %v", err)
	}
}

// TestOpenReaderCancelled pins the open-boundary contract: a cancelled context
// yields an error, never a silently truncated document (the layout engine
// degrades internally; the boundary converts that to a failure).
func TestOpenReaderCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := OpenReaderAs(ctx, FormatHTML, strings.NewReader("<html><body><p>hi</p></body></html>")); !errors.Is(err, context.Canceled) {
		t.Errorf("OpenReaderAs(cancelled, html): want context.Canceled, got %v", err)
	}
	if _, err := OpenReaderAs(ctx, FormatDOCX, bytes.NewReader(gendocx.Core[0].Bytes())); !errors.Is(err, context.Canceled) {
		t.Errorf("OpenReaderAs(cancelled, docx): want context.Canceled, got %v", err)
	}
	// A live context is unaffected.
	if _, err := OpenReaderAs(context.Background(), FormatHTML, strings.NewReader("<p>ok</p>")); err != nil {
		t.Errorf("OpenReaderAs(live ctx): %v", err)
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
