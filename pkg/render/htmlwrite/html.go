// Package htmlwrite serializes a cssbox tree (the shared box model produced by the HTML
// and DOCX frontends) back to HTML. It is the mirror of the Markdown writer
// (pkg/render/markdown): a conversion backend that walks the box tree directly rather
// than the render.Device paint seam, because HTML output needs the document's structure —
// headings, lists, links, emphasis, tables — not positioned glyphs. Because DOCX and HTML
// converge on the same cssbox tree, one walker serves both formats (DOCX → HTML export
// reuses this exact code).
//
// The writer reads the semantic annotations the frontends record on each box
// (Box.SemTag / Box.HeadingLvl / Box.Href) together with the structural display kinds
// (DisplayListItem, DisplayTable, ...) and the inherited computed style (Bold / Italic /
// TextDecorationLine / TextAlign). It never mutates the tree, so the same read-only tree
// that feeds layout feeds conversion.
package htmlwrite

import (
	"io"
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
	"github.com/nathanstitt/doctaculous/pkg/render/internal/boxwalk"
)

// Options configures HTML serialization.
type Options struct {
	// Fragment, when true, emits only the body content (the block-level markup) with no
	// surrounding <!DOCTYPE>/<html>/<head>/<body> scaffold. The default (false) wraps the
	// content in a minimal, well-formed HTML document.
	Fragment bool
	// Logf receives degradation messages (e.g. a table with no rows). May be nil (messages
	// discarded).
	Logf func(string, ...any)
}

// Write renders the cssbox tree rooted at root to w as HTML. root is expected to be a
// finalized tree (post box-generation fixups), so list markers and anonymous boxes are
// already resolved. A nil root writes an empty document (or nothing, in Fragment mode).
func Write(root *cssbox.Box, w io.Writer, opts Options) error {
	if opts.Logf == nil {
		opts.Logf = func(string, ...any) {}
	}
	wr := &writer{opts: opts}
	if root != nil {
		wr.block(root, 0)
	}
	body := wr.sb.String()
	body = strings.TrimRight(body, "\n")

	var out strings.Builder
	if opts.Fragment {
		out.WriteString(body)
		if body != "" {
			out.WriteByte('\n')
		}
	} else {
		out.WriteString("<!DOCTYPE html>\n<html>\n<head>\n<meta charset=\"utf-8\">\n</head>\n<body>\n")
		if body != "" {
			out.WriteString(body)
			out.WriteByte('\n')
		}
		out.WriteString("</body>\n</html>\n")
	}
	_, err := io.WriteString(w, out.String())
	return err
}

// writer accumulates rendered HTML into sb. Indentation is tracked explicitly so nested
// constructs (blockquotes, lists, tables) read cleanly.
type writer struct {
	opts Options
	sb   strings.Builder
}

// line writes one indented line of HTML followed by a newline. depth is the indent level
// (two spaces per level).
func (w *writer) line(depth int, s string) {
	w.sb.WriteString(strings.Repeat("  ", depth))
	w.sb.WriteString(s)
	w.sb.WriteByte('\n')
}

// block dispatches a block-level box, mirroring markdown.go's block(). depth is the
// indentation level. A box that is not itself a recognized block construct recurses into
// its children so wrapper/anonymous boxes (html/body/div) are transparent.
func (w *writer) block(b *cssbox.Box, depth int) {
	switch {
	case b.HeadingLvl >= 1 && b.HeadingLvl <= 6:
		w.heading(b, depth)
	case b.SemTag == "blockquote":
		w.blockquote(b, depth)
	case b.SemTag == "pre":
		w.codeBlock(b, depth)
	case b.SemTag == "hr":
		w.line(depth, "<hr>")
	case b.Display == cssbox.DisplayTable:
		w.table(b, depth)
	case boxwalk.IsListContainer(b):
		w.list(b, depth)
	case b.Display == cssbox.DisplayListItem:
		// A stray list item not inside a recognized list container (rare); render it as a
		// one-item list.
		w.list(&cssbox.Box{Children: []*cssbox.Box{b}}, depth)
	case boxwalk.IsBlockContainer(b):
		// A block that holds inline content (an inline formatting context, or a paragraph)
		// becomes one <p>; a block holding further blocks recurses.
		if boxwalk.HasInlineContent(b) {
			w.paragraph(b, depth)
		} else {
			w.children(b, depth)
		}
	default:
		w.children(b, depth)
	}
}

// children recurses over a box's block-level children (used for transparent wrappers like
// html/body/div boxes).
func (w *writer) children(b *cssbox.Box, depth int) {
	for _, c := range b.Children {
		w.block(c, depth)
	}
}

// heading emits an <h1>..<h6> element with the box's inline content.
func (w *writer) heading(b *cssbox.Box, depth int) {
	text := strings.TrimSpace(w.inlineOpt(b, true)) // the <hN> tag conveys bold; drop UA bold
	if text == "" {
		return
	}
	tag := "h" + string(rune('0'+b.HeadingLvl))
	w.line(depth, "<"+tag+">"+text+"</"+tag+">")
}

// paragraph emits a box's inline content as one <p> element.
func (w *writer) paragraph(b *cssbox.Box, depth int) {
	text := strings.TrimSpace(w.inline(b))
	if text == "" {
		return
	}
	w.line(depth, "<p>"+text+"</p>")
}

// blockquote emits a <blockquote> whose block children are rendered (recursively) at a
// deeper indent, so a nested quote nests its markup.
func (w *writer) blockquote(b *cssbox.Box, depth int) {
	inner := &writer{opts: w.opts}
	if boxwalk.HasInlineContent(b) {
		inner.paragraph(b, depth+1)
	} else {
		inner.children(b, depth+1)
	}
	body := strings.TrimRight(inner.sb.String(), "\n")
	if body == "" {
		return
	}
	w.line(depth, "<blockquote>")
	w.sb.WriteString(body)
	w.sb.WriteByte('\n')
	w.line(depth, "</blockquote>")
}

// codeBlock emits a <pre><code> block from a <pre> box, preserving its text verbatim
// (whitespace-significant). The text is HTML-escaped but not otherwise reformatted.
func (w *writer) codeBlock(b *cssbox.Box, depth int) {
	text := strings.TrimRight(boxwalk.RawText(b), "\n")
	if text == "" {
		w.line(depth, "<pre><code></code></pre>")
		return
	}
	w.line(depth, "<pre><code>"+escapeText(text)+"</code></pre>")
}

// textEscaper escapes the characters that are significant in HTML text content.
var textEscaper = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
)

// escapeText escapes HTML metacharacters in ordinary text content (& < >).
func escapeText(s string) string { return textEscaper.Replace(s) }

// attrEscaper escapes the characters significant inside a double-quoted attribute value.
var attrEscaper = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	`"`, "&quot;",
)

// escapeAttr escapes HTML metacharacters in a double-quoted attribute value (& < > ").
func escapeAttr(s string) string { return attrEscaper.Replace(s) }
