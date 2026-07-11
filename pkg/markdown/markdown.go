// Package markdown is the Markdown input frontend: it converts CommonMark +
// GFM source into a complete HTML document that the HTML pipeline (parse → box
// generation → CSS layout) renders. Reusing the HTML pipeline — rather than
// lowering a Markdown AST to boxes directly — inherits whitespace collapsing,
// anonymous-box fixups, list markers, tables, pagination, and the semantic
// annotations (SemTag/HeadingLvl/Href) the structure writers need, so a
// Markdown document round-trips back to Markdown.
package markdown

import (
	"bytes"
	"fmt"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	ghtml "github.com/yuin/goldmark/renderer/html"
)

// ToHTML converts CommonMark + GFM source (tables, strikethrough, task lists,
// autolinks) into a complete, self-contained HTML document with the embedded
// default stylesheet, ready for the HTML pipeline.
//
// Raw inline/block HTML in the source is passed through (goldmark's "unsafe"
// mode): it is core CommonMark, and the only consumer is this module's own
// layout engine, which executes nothing.
func ToHTML(src []byte) ([]byte, error) {
	// A fresh converter per call: goldmark does not document goroutine safety,
	// and construction is trivial next to a document layout.
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithRendererOptions(ghtml.WithUnsafe()),
	)
	var buf bytes.Buffer
	buf.WriteString("<!DOCTYPE html>\n<html>\n<head>\n<meta charset=\"utf-8\">\n<style>\n")
	buf.WriteString(DefaultCSS)
	buf.WriteString("</style>\n</head>\n<body>\n")
	if err := md.Convert(src, &buf); err != nil {
		return nil, fmt.Errorf("markdown: convert: %w", err)
	}
	buf.WriteString("</body>\n</html>\n")
	return buf.Bytes(), nil
}
