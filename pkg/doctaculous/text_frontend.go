package doctaculous

import (
	"fmt"
	"os"
	"strings"
)

// OpenText reads and renders a plain-text (.txt) file, laying it out at the
// default viewport width into a single tall page. For additional options (e.g.
// WithPageSize) use OpenTextFile.
func OpenText(path string) (*Document, error) {
	return OpenTextFile(path)
}

// OpenTextFile reads and renders a plain-text file at path, applying any
// options.
func OpenTextFile(path string, opts ...HTMLOption) (*Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("doctaculous: open text %q: %w", path, err)
	}
	return OpenTextBytes(data, opts...)
}

// OpenTextBytes renders in-memory plain text, applying any options, and
// returns a Document ready to rasterize or convert. The text is wrapped in a
// single preformatted block with pre-wrap wrapping: hard line breaks — the
// only structure plain text has — are preserved exactly (logs, poems, aligned
// columns), while over-long lines still soft-wrap so nothing clips at the page
// edge. It renders monospace via the UA stylesheet, paginates through the
// normal mid-block line fragmentation, and — because <pre> carries the "pre"
// semantic tag — converts to Markdown as one verbatim fenced code block and to
// plain text as itself.
//
// The bytes are treated as UTF-8: a leading BOM is dropped, CRLF/CR line
// endings normalize to LF, and invalid sequences are replaced with U+FFFD.
func OpenTextBytes(data []byte, opts ...HTMLOption) (*Document, error) {
	doc, err := OpenHTMLBytes([]byte(textToHTML(data)), opts...)
	if err != nil {
		return nil, err
	}
	doc.format = FormatText
	return doc, nil
}

// textToHTML wraps normalized, escaped plain text in a minimal HTML document
// around one <pre> block.
func textToHTML(data []byte) string {
	s := strings.TrimPrefix(string(data), "\uFEFF") // strip a UTF-8 BOM
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.ToValidUTF8(s, "�")
	s = htmlEscaper.Replace(s)

	var b strings.Builder
	b.Grow(len(s) + 256)
	b.WriteString("<!DOCTYPE html>\n<html>\n<head>\n<meta charset=\"utf-8\">\n<style>\n")
	b.WriteString("pre { white-space: pre-wrap; margin: 32px; font-size: 12px; }\n")
	b.WriteString("</style>\n</head>\n<body><pre>")
	b.WriteString(s)
	b.WriteString("</pre></body>\n</html>\n")
	return b.String()
}

// htmlEscaper escapes the three characters with meaning in HTML text content.
var htmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
