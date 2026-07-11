package doctaculous

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// specimenMD exercises every GFM construct the frontend supports.
const specimenMD = `# Specimen

Body text with *emphasis*, **strong**, ~~strikethrough~~, and ` + "`code`" + `.

## Lists

- first
- second
  - nested

1. one
2. two

- [x] done task
- [ ] open task

## Quote and code

> A quoted line.

` + "```" + `
fenced code line
` + "```" + `

## Table

| Item | Qty |
| :--- | ---: |
| Widgets | 5 |

---

[a link](https://example.test/page) ends the specimen.
`

func openSpecimenMD(t *testing.T) *Document {
	t.Helper()
	doc, err := OpenMarkdownBytes([]byte(specimenMD), WithBundledFonts())
	if err != nil {
		t.Fatalf("OpenMarkdownBytes: %v", err)
	}
	return doc
}

// TestMarkdownRoundTrip verifies the frontend preserves every construct
// through open → WriteMarkdown, and that the result is a fixed point (a second
// round trip reproduces it byte-for-byte — the writer's canonical form).
func TestMarkdownRoundTrip(t *testing.T) {
	ctx := context.Background()
	doc := openSpecimenMD(t)

	var first bytes.Buffer
	if err := doc.WriteMarkdown(ctx, &first, MarkdownOptions{}); err != nil {
		t.Fatalf("WriteMarkdown: %v", err)
	}
	// Expectations are the writer's canonical forms: emphasis is _underscore_
	// style, and explicit left alignment (:---) normalizes to the equivalent
	// default (---) while right alignment survives.
	got := first.String()
	for _, want := range []string{
		"# Specimen",
		"## Lists",
		"_emphasis_",
		"**strong**",
		"~~strikethrough~~",
		"`code`",
		"- first",
		"- nested",
		"1. one",
		"2. two",
		"- [x] done task",
		"- [ ] open task",
		"> A quoted line.",
		"fenced code line",
		"| Item | Qty |",
		"| --- | ---: |",
		"| Widgets | 5 |",
		"[a link](https://example.test/page)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("round trip lost %q:\n%s", want, got)
		}
	}

	// Fixed point: reopening the writer's own output reproduces it exactly.
	doc2, err := OpenMarkdownBytes(first.Bytes(), WithBundledFonts())
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	var second bytes.Buffer
	if err := doc2.WriteMarkdown(ctx, &second, MarkdownOptions{}); err != nil {
		t.Fatalf("WriteMarkdown (2nd): %v", err)
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Errorf("markdown round trip is not a fixed point:\n--- first ---\n%s\n--- second ---\n%s", first.String(), second.String())
	}
}

// TestMarkdownParityWithHTML pins that a Markdown document and its equivalent
// HTML produce identical Markdown output — the frontend adds nothing the HTML
// pipeline would not.
func TestMarkdownParityWithHTML(t *testing.T) {
	ctx := context.Background()
	md := "# Title\n\nA paragraph with **bold** text.\n"
	html := "<html><body><h1>Title</h1><p>A paragraph with <strong>bold</strong> text.</p></body></html>"

	var fromMD, fromHTML bytes.Buffer
	doc, err := OpenMarkdownBytes([]byte(md), WithBundledFonts())
	if err != nil {
		t.Fatalf("OpenMarkdownBytes: %v", err)
	}
	if err := doc.WriteMarkdown(ctx, &fromMD, MarkdownOptions{}); err != nil {
		t.Fatalf("WriteMarkdown: %v", err)
	}
	if err := ConvertHTMLToMarkdown(ctx, strings.NewReader(html), &fromHTML, MarkdownOptions{}); err != nil {
		t.Fatalf("ConvertHTMLToMarkdown: %v", err)
	}
	if fromMD.String() != fromHTML.String() {
		t.Errorf("markdown/html parity broken:\n--- from md ---\n%s\n--- from html ---\n%s", fromMD.String(), fromHTML.String())
	}
}

// TestMarkdownRelativeImage verifies a relative image ref in a .md file
// resolves through the DirLoader rooted at the file's directory (the
// OpenHTMLFile behavior, inherited).
func TestMarkdownRelativeImage(t *testing.T) {
	dir := t.TempDir()
	// A tiny PNG the layout must fetch and decode through the DirLoader.
	if err := os.WriteFile(filepath.Join(dir, "pic.png"), encodeTinyImage(t, FormatPNG), 0o600); err != nil {
		t.Fatal(err)
	}
	mdPath := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(mdPath, []byte("![pic](pic.png)\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var logged bool
	doc, err := OpenMarkdownFile(mdPath, WithBundledFonts(), WithLogf(func(string, ...any) { logged = true }))
	if err != nil {
		t.Fatalf("OpenMarkdownFile: %v", err)
	}
	if logged {
		t.Errorf("image load logged a degradation; DirLoader did not resolve pic.png")
	}
	if doc.PageCount() < 1 {
		t.Fatalf("no pages")
	}
}

// TestMarkdownPagination verifies WithPageSize flows through to the generated
// document.
func TestMarkdownPagination(t *testing.T) {
	long := strings.Repeat("Paragraph of body text that occupies a line.\n\n", 200)
	doc, err := OpenMarkdownBytes([]byte("# Long\n\n"+long), WithBundledFonts(), WithPageSize(LetterWidthPt, LetterHeightPt))
	if err != nil {
		t.Fatalf("OpenMarkdownBytes: %v", err)
	}
	if doc.PageCount() < 2 {
		t.Errorf("PageCount() = %d, want >= 2 (pagination not applied)", doc.PageCount())
	}
	if doc.Format() != FormatMarkdown {
		t.Errorf("Format() = %q, want markdown", doc.Format())
	}
}
