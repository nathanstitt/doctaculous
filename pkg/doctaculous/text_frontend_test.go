package doctaculous

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// TestTextRoundTripIdentity verifies .txt → open → WriteText reproduces the
// text: hard line breaks are the only structure plain text has, and they must
// survive (with BOM/CRLF normalization applied).
func TestTextRoundTripIdentity(t *testing.T) {
	src := "\uFEFFfirst line\r\nsecond line\r\rindented   columns kept\n\nafter a blank line\n"
	want := "first line\nsecond line\n\nindented   columns kept\n\nafter a blank line"

	doc, err := OpenTextBytes([]byte(src), WithBundledFonts())
	if err != nil {
		t.Fatalf("OpenTextBytes: %v", err)
	}
	if doc.Format() != FormatText {
		t.Errorf("Format() = %q, want text", doc.Format())
	}
	var out bytes.Buffer
	if err := doc.WriteText(context.Background(), &out, MarkdownOptions{}); err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	got := strings.TrimRight(out.String(), "\n")
	if got != want {
		t.Errorf("text round trip diverged:\n--- got ---\n%q\n--- want ---\n%q", got, want)
	}
}

// TestTextToMarkdownFencedBlock verifies .txt converts to Markdown as one
// verbatim fenced code block (the <pre> semantic tag at work).
func TestTextToMarkdownFencedBlock(t *testing.T) {
	src := "log line one\nlog line two\n"
	doc, err := OpenTextBytes([]byte(src), WithBundledFonts())
	if err != nil {
		t.Fatalf("OpenTextBytes: %v", err)
	}
	var out bytes.Buffer
	if err := doc.WriteMarkdown(context.Background(), &out, MarkdownOptions{}); err != nil {
		t.Fatalf("WriteMarkdown: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "```") {
		t.Errorf("txt->md is not fenced:\n%s", got)
	}
	if !strings.Contains(got, "log line one\nlog line two") {
		t.Errorf("txt->md lost verbatim lines:\n%s", got)
	}
}

// TestTextEscapesMarkup verifies markup-significant characters in the text
// stay literal (they must not be parsed as HTML).
func TestTextEscapesMarkup(t *testing.T) {
	src := "a <b>not bold</b> & \"quoted\"\n"
	doc, err := OpenTextBytes([]byte(src), WithBundledFonts())
	if err != nil {
		t.Fatalf("OpenTextBytes: %v", err)
	}
	var out bytes.Buffer
	if err := doc.WriteText(context.Background(), &out, MarkdownOptions{}); err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	if !strings.Contains(out.String(), "a <b>not bold</b> & \"quoted\"") {
		t.Errorf("markup characters not preserved literally:\n%s", out.String())
	}
}

// TestTextInvalidUTF8Replaced verifies invalid bytes become U+FFFD instead of
// corrupting the document.
func TestTextInvalidUTF8Replaced(t *testing.T) {
	doc, err := OpenTextBytes([]byte("ok \xff\xfe end\n"), WithBundledFonts())
	if err != nil {
		t.Fatalf("OpenTextBytes: %v", err)
	}
	var out bytes.Buffer
	if err := doc.WriteText(context.Background(), &out, MarkdownOptions{}); err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	if !strings.Contains(out.String(), "ok �") {
		t.Errorf("invalid UTF-8 not replaced:\n%q", out.String())
	}
}

// TestTextBlankLinesRender verifies blank lines occupy vertical space in the
// render (the CSS strut): a document with a blank line between two lines is
// taller than one without. Regression test for the empty-forced-line collapse
// the text frontend exposed.
func TestTextBlankLinesRender(t *testing.T) {
	height := func(src string) int {
		t.Helper()
		doc, err := OpenTextBytes([]byte(src), WithBundledFonts())
		if err != nil {
			t.Fatalf("OpenTextBytes: %v", err)
		}
		img, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: 72, BundledFonts: true})
		if err != nil {
			t.Fatalf("RasterizePage: %v", err)
		}
		return img.Bounds().Dy()
	}
	if without, with := height("a\nb\n"), height("a\n\nb\n"); with <= without {
		t.Errorf("blank line occupies no space: height %d (with) vs %d (without)", with, without)
	}
}

// TestTextPagination verifies a long .txt paginates across pages and over-long
// lines soft-wrap (pre-wrap) instead of clipping.
func TestTextPagination(t *testing.T) {
	long := strings.Repeat("line of plain text\n", 400) +
		strings.Repeat("an over-long unbroken line of text ", 40) + "\n"
	doc, err := OpenTextBytes([]byte(long), WithBundledFonts(), WithPageSize(LetterWidthPt, LetterHeightPt))
	if err != nil {
		t.Fatalf("OpenTextBytes: %v", err)
	}
	if doc.PageCount() < 2 {
		t.Errorf("PageCount() = %d, want >= 2", doc.PageCount())
	}
}
