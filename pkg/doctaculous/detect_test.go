package doctaculous

import (
	"archive/zip"
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/nathanstitt/doctaculous/testdata/gen"
	gendocx "github.com/nathanstitt/doctaculous/testdata/gen/docx"
	genxlsx "github.com/nathanstitt/doctaculous/testdata/gen/xlsx"
)

// buildZip assembles an in-memory ZIP with the given part names (contents are
// irrelevant to detection, which reads only the central directory).
func buildZip(t *testing.T, names ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range names {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := w.Write([]byte("x")); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func encodeTinyImage(t *testing.T, format Format) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	var buf bytes.Buffer
	var err error
	switch format {
	case FormatPNG:
		err = png.Encode(&buf, img)
	case FormatJPEG:
		err = jpeg.Encode(&buf, img, nil)
	default:
		t.Fatalf("encodeTinyImage: bad format %q", format)
	}
	if err != nil {
		t.Fatalf("encode %s: %v", format, err)
	}
	return buf.Bytes()
}

func TestDetectFormatMagic(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		hint string
		want Format
	}{
		{"pdf", gen.TextPDF(), "", FormatPDF},
		{"docx", gendocx.Core[0].Bytes(), "", FormatDOCX},
		{"png", encodeTinyImage(t, FormatPNG), "", FormatPNG},
		{"jpeg", encodeTinyImage(t, FormatJPEG), "", FormatJPEG},
		// Content beats the extension: a real PDF named .txt is a PDF.
		{"pdf named txt", gen.TextPDF(), "report.txt", FormatPDF},
		{"docx named zip", gendocx.Core[0].Bytes(), "archive.zip", FormatDOCX},
		// Unrecognized ZIPs are not documents.
		{"plain zip", buildZip(t, "a.txt", "dir/b.txt"), "", FormatUnknown},
		{"pptx-shaped zip", buildZip(t, "[Content_Types].xml", "ppt/presentation.xml"), "", FormatUnknown},
		// SpreadsheetML packages classify as XLSX, at the conventional location
		// or rels-redirected.
		{"xlsx", genxlsx.Core[0].Bytes(), "", FormatXLSX},
		{"xlsx-shaped zip", buildZip(t, "[Content_Types].xml", "xl/workbook.xml"), "", FormatXLSX},
		{"opc xl zip", buildZip(t, "[Content_Types].xml", "xl/book.xml"), "", FormatXLSX},
		// A rels-redirected OPC package: content types + word/ part, main part
		// not at the conventional location.
		{"opc word zip", buildZip(t, "[Content_Types].xml", "word/main.xml"), "", FormatDOCX},
	}
	for _, c := range cases {
		if got := DetectFormat(c.data, c.hint); got != c.want {
			t.Errorf("%s: DetectFormat = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDetectFormatExtensionHint(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		hint string
		want Format
	}{
		// The extension is the only signal for Markdown and text — and it wins
		// over HTML sniffing, so Markdown with raw HTML blocks stays Markdown.
		{"markdown with html", []byte("<div>\nraw block\n</div>\n\n# Title\n"), "README.md", FormatMarkdown},
		{"markdown", []byte("# Title\n\nbody\n"), "notes.md", FormatMarkdown},
		{"text", []byte("plain lines\n"), "notes.txt", FormatText},
		// The hint rescues a PDF whose header is beyond the 1 KiB magic window
		// (the parser's object-scan rebuild can still open it).
		{"damaged pdf", append(bytes.Repeat([]byte{'j'}, 1500), gen.TextPDF()...), "a.pdf", FormatPDF},
		{"html by extension", []byte("no tags here"), "page.html", FormatHTML},
	}
	for _, c := range cases {
		if got := DetectFormat(c.data, c.hint); got != c.want {
			t.Errorf("%s: DetectFormat = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDetectFormatHTMLSniff(t *testing.T) {
	positive := []string{
		"<!DOCTYPE html><html><body>x</body></html>",
		"<!doctype html>\n<p>x</p>",
		"<html><head></head></html>",
		"<HTML>",
		"\xEF\xBB\xBF  \n\t<html>",
		"<!-- a leading comment --><p>x</p>",
		"<?xml version=\"1.0\"?><html xmlns=\"http://www.w3.org/1999/xhtml\"></html>",
		"<div class=\"a\">x</div>",
		"<p>hello</p>",
		"<table><tr><td>x</td></tr></table>",
		"<a href=\"x\">link</a>",
		"<br>",
	}
	for _, s := range positive {
		if got := DetectFormat([]byte(s), ""); got != FormatHTML {
			t.Errorf("DetectFormat(%q) = %q, want html", s, got)
		}
	}
	negative := []string{
		"",
		"plain text, no markup",
		"# a markdown heading\n",
		"a < b and b > c",
		"<notatag>",
		"{\"json\": true}",
	}
	for _, s := range negative {
		if got := DetectFormat([]byte(s), ""); got != FormatUnknown {
			t.Errorf("DetectFormat(%q) = %q, want unknown", s, got)
		}
	}
}

func TestDetectFormatUnknown(t *testing.T) {
	garbage := []byte{0x00, 0x01, 0x02, 0xFE, 0xBA, 0xD0}
	if got := DetectFormat(garbage, ""); got != FormatUnknown {
		t.Errorf("DetectFormat(garbage) = %q, want unknown", got)
	}
	if got := DetectFormat(garbage, "noext"); got != FormatUnknown {
		t.Errorf("DetectFormat(garbage, noext) = %q, want unknown", got)
	}
	if got := DetectFormat(nil, ""); got != FormatUnknown {
		t.Errorf("DetectFormat(nil) = %q, want unknown", got)
	}
}
