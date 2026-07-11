// Package docxwrite renders a cssbox tree (the shared box model produced by the
// HTML, DOCX, Markdown, and PDF-extraction frontends) to a WordprocessingML
// (.docx) package. Like the Markdown and HTML writers — and unlike the raster
// and PDF backends — it is a STRUCTURE writer: it walks the box tree directly,
// reading the semantic annotations (SemTag/HeadingLvl/Href), display kinds, and
// computed style, and emits headings, paragraphs, emphasis, hyperlinks, lists,
// block quotes, and code blocks as native Word constructs. It is deliberately
// not layout-faithful: floats, positioning, and exact CSS geometry are not
// represented (they degrade to normal flow), and headers/footers and footnotes
// are out of scope.
//
// Every mapping is chosen so this repo's own DOCX reader (pkg/docx +
// pkg/docx/cssbox) round-trips it: heading level rides on the HeadingN style
// id, quotes on the Quote style, code blocks on the CodeBlock style, emphasis
// on direct run properties, hyperlinks on external relationships.
package docxwrite

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
	"github.com/nathanstitt/doctaculous/pkg/render/internal/boxwalk"
)

// Options configures DOCX output.
type Options struct {
	// PageWidthPt, PageHeightPt set the section page size in points; default US
	// Letter (612x792) when zero.
	PageWidthPt, PageHeightPt float64
	// MarginPt is the uniform page margin in points; default 72 (1in, Word's
	// default) when zero. A negative value means no margin (0).
	MarginPt float64
	// Logf receives degradation diagnostics (e.g. a construct the writer cannot
	// represent). nil -> no-op.
	Logf func(string, ...any)
}

// Write renders the cssbox tree rooted at root to w as a complete .docx
// package. root is expected to be a finalized tree (post box-generation
// fixups). A nil root writes a valid empty document.
func Write(ctx context.Context, root *cssbox.Box, w io.Writer, opts Options) error {
	if opts.Logf == nil {
		opts.Logf = func(string, ...any) {}
	}
	wr := &writer{opts: opts, relURL: map[string]string{}}
	if root != nil {
		if err := wr.block(ctx, root); err != nil {
			return err
		}
	}
	pkg, err := assemblePackage(wr, opts)
	if err != nil {
		return fmt.Errorf("docxwrite: %w", err)
	}
	if _, err := w.Write(pkg); err != nil {
		return fmt.Errorf("docxwrite: %w", err)
	}
	return nil
}

// writer accumulates the document body XML and the package-level state the
// parts are assembled from (hyperlink relationships, ordered-list numbering
// instances).
type writer struct {
	opts Options
	body strings.Builder

	// rels are the document relationships in emission order (hyperlinks now;
	// images when the media path lands). Ids are assigned at assembly:
	// rId1/rId2 are reserved for styles/numbering, rels start at rId3.
	rels []docRel
	// relURL dedupes hyperlink targets: URL -> allocated rel id.
	relURL map[string]string

	// orderedLists counts the ordered-list numbering instances allocated so
	// far. Each ordered list gets its own w:num (counters are per-numId in the
	// reader, and Word restarts numbering per instance), all sharing the one
	// decimal abstract definition.
	orderedLists int
}

// docRel is one word/_rels/document.xml.rels relationship.
type docRel struct {
	id, relType, target string
	external            bool
}

// hyperlinkRel returns the rel id for an external hyperlink target, allocating
// one on first use.
func (w *writer) hyperlinkRel(url string) string {
	if id, ok := w.relURL[url]; ok {
		return id
	}
	id := fmt.Sprintf("rId%d", 3+len(w.rels))
	w.rels = append(w.rels, docRel{
		id:       id,
		relType:  "http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink",
		target:   url,
		external: true,
	})
	w.relURL[url] = id
	return id
}

// block dispatches one block-level box, mirroring the Markdown writer's walk. A
// box that is not itself a recognized construct recurses into its children so
// wrapper/anonymous boxes are transparent.
func (w *writer) block(ctx context.Context, b *cssbox.Box) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	switch {
	case b.HeadingLvl >= 1 && b.HeadingLvl <= 6:
		w.paragraph(b, fmt.Sprintf("Heading%d", b.HeadingLvl))
	case b.SemTag == "blockquote":
		return w.blockquote(ctx, b)
	case b.SemTag == "pre":
		w.codeBlock(b)
	case b.SemTag == "hr":
		w.horizontalRule()
	case b.Display == cssbox.DisplayTable:
		// Tables land with the media/table follow-up; keep the content by
		// emitting each cell's blocks as plain paragraphs.
		w.opts.Logf("docxwrite: table structure not yet supported; emitting cell content as paragraphs")
		return w.tableFallback(ctx, b)
	case boxwalk.IsListContainer(b):
		return w.list(ctx, b, 0)
	case b.Display == cssbox.DisplayListItem:
		// A stray list item outside a list container: render as a one-item list.
		return w.list(ctx, &cssbox.Box{Children: []*cssbox.Box{b}}, 0)
	case boxwalk.IsBlockContainer(b):
		if boxwalk.HasInlineContent(b) {
			w.paragraph(b, "")
		} else {
			return w.children(ctx, b)
		}
	default:
		return w.children(ctx, b)
	}
	return nil
}

// children recurses over a box's block-level children.
func (w *writer) children(ctx context.Context, b *cssbox.Box) error {
	for _, c := range b.Children {
		if err := w.block(ctx, c); err != nil {
			return err
		}
	}
	return nil
}

// blockquote emits the box's paragraphs with the Quote style (the reader maps
// the Quote style id back to the blockquote semantic). Non-paragraph content
// (a nested list or table inside the quote) keeps its own construct and loses
// the quote association — logged, a documented v1 limit.
func (w *writer) blockquote(ctx context.Context, b *cssbox.Box) error {
	if boxwalk.HasInlineContent(b) {
		w.paragraph(b, "Quote")
		return nil
	}
	for _, c := range b.Children {
		if boxwalk.IsBlockContainer(c) && boxwalk.HasInlineContent(c) &&
			(c.SemTag == "" || c.SemTag == "p") && c.HeadingLvl == 0 {
			w.paragraph(c, "Quote")
			continue
		}
		w.opts.Logf("docxwrite: non-paragraph content inside a blockquote keeps its own construct")
		if err := w.block(ctx, c); err != nil {
			return err
		}
	}
	return nil
}

// tableFallback emits a table's cell content as plain paragraphs (structure
// dropped, content kept).
func (w *writer) tableFallback(ctx context.Context, table *cssbox.Box) error {
	rows, _ := boxwalk.CollectRows(table)
	for _, row := range rows {
		for _, cell := range boxwalk.CellBoxesOf(row) {
			if boxwalk.HasInlineContent(cell) {
				w.paragraph(cell, "")
				continue
			}
			if err := w.children(ctx, cell); err != nil {
				return err
			}
		}
	}
	return nil
}
