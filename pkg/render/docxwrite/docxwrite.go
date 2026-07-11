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
// The walk BUILDS a *docx.Document — the public model in pkg/docx — and
// serializes it with docx.Write, so the repo has exactly one OPC emitter.
// Every mapping is chosen so this repo's own DOCX reader (pkg/docx +
// pkg/docx/cssbox) round-trips it: heading level rides on the HeadingN style
// id, quotes on the Quote style, code blocks on the CodeBlock style, emphasis
// on direct run properties, hyperlinks on external relationships.
package docxwrite

import (
	"context"
	"fmt"
	"io"
	"math"

	// The image decoders imageRun's DecodeConfig relies on, registered here so
	// the writer is self-sufficient.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"github.com/nathanstitt/doctaculous/pkg/docx"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
	"github.com/nathanstitt/doctaculous/pkg/render/internal/boxwalk"
	"github.com/nathanstitt/doctaculous/pkg/resource"
)

// Options configures DOCX output.
type Options struct {
	// PageWidthPt, PageHeightPt set the section page size in points; default US
	// Letter (612x792) when zero.
	PageWidthPt, PageHeightPt float64
	// MarginPt is the uniform page margin in points; default 72 (1in, Word's
	// default) when zero. A negative value means no margin (0).
	MarginPt float64
	// Loader resolves image refs (an <img> src) to bytes for embedding as media
	// parts. nil means images degrade to their alt text (logged).
	Loader resource.ResourceLoader
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
	wr := &writer{
		opts:  opts,
		ctx:   ctx,
		doc:   &docx.Document{Styles: docx.DefaultStyles()},
		media: map[string]mediaRef{},
	}
	if root != nil {
		if err := wr.block(ctx, root, &wr.doc.Body); err != nil {
			return err
		}
	}
	wr.doc.Numbering = buildNumbering(wr.orderedLists)
	wr.doc.Section = sectionProps(opts)
	if err := docx.Write(w, wr.doc); err != nil {
		return fmt.Errorf("docxwrite: %w", err)
	}
	return nil
}

// writer accumulates the docx.Document being built and the walk state the
// model's parts derive from (deduped media, ordered-list numbering instances).
type writer struct {
	opts Options
	// ctx is the Write call's context, held for the walk-driven image fetches
	// (the CollectRuns callback has no context parameter). The walk is
	// single-goroutine and the writer does not outlive Write.
	ctx context.Context
	doc *docx.Document

	// pending queues the model children (drawings, alt-text runs) produced by
	// the image callback, in walk order: CollectRuns returns literal marker
	// runs, and paraChildren pops one queued child per marker.
	pending []docx.ParaChild
	// media dedupes embedded images: src ref -> its media part's identity.
	media map[string]mediaRef

	// orderedLists counts the ordered-list numbering instances allocated so
	// far. Each ordered list gets its own w:num (counters are per-numId in the
	// reader, and Word restarts numbering per instance), all sharing the one
	// decimal abstract definition.
	orderedLists int
}

// mediaRef is a deduped embedded image: its rel id and intrinsic pixel size.
type mediaRef struct {
	relID    string
	pxW, pxH int
}

// sectionProps maps the options' page geometry to the body section (points ->
// twips; Word's Letter page and 1in margin defaults when unset).
func sectionProps(opts Options) docx.SectionProps {
	pageW, pageH := opts.PageWidthPt, opts.PageHeightPt
	if pageW <= 0 {
		pageW = 612 // US Letter
	}
	if pageH <= 0 {
		pageH = 792
	}
	margin := opts.MarginPt
	switch {
	case margin < 0:
		margin = 0
	case margin == 0:
		margin = 72 // Word's 1in default
	}
	m := twips(margin)
	return docx.SectionProps{
		PageW: twips(pageW), PageH: twips(pageH),
		MarginTop: m, MarginBottom: m, MarginLeft: m, MarginRight: m,
		Header: 720, Footer: 720,
	}
}

// twips converts points to twentieths of a point.
func twips(pt float64) docx.Twips { return docx.Twips(math.Round(pt * 20)) }

// block dispatches one block-level box into out, mirroring the Markdown
// writer's walk. A box that is not itself a recognized construct recurses into
// its children so wrapper/anonymous boxes are transparent.
func (w *writer) block(ctx context.Context, b *cssbox.Box, out *[]docx.Block) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	switch {
	case b.HeadingLvl >= 1 && b.HeadingLvl <= 6:
		w.appendParagraph(out, b, fmt.Sprintf("Heading%d", b.HeadingLvl))
	case b.SemTag == "blockquote":
		return w.blockquote(ctx, b, out)
	case b.SemTag == "pre":
		w.codeBlock(b, out)
	case b.SemTag == "hr":
		w.horizontalRule(out)
	case b.Display == cssbox.DisplayTable:
		return w.table(ctx, b, out)
	case boxwalk.IsListContainer(b):
		return w.list(ctx, b, 0, out)
	case b.Display == cssbox.DisplayListItem:
		// A stray list item outside a list container: render as a one-item list.
		return w.list(ctx, &cssbox.Box{Children: []*cssbox.Box{b}}, 0, out)
	case boxwalk.IsBlockContainer(b):
		if boxwalk.HasInlineContent(b) {
			w.appendParagraph(out, b, "")
		} else {
			return w.children(ctx, b, out)
		}
	default:
		return w.children(ctx, b, out)
	}
	return nil
}

// children recurses over a box's block-level children.
func (w *writer) children(ctx context.Context, b *cssbox.Box, out *[]docx.Block) error {
	for _, c := range b.Children {
		if err := w.block(ctx, c, out); err != nil {
			return err
		}
	}
	return nil
}

// blockquote emits the box's paragraphs with the Quote style (the reader maps
// the Quote style id back to the blockquote semantic). Non-paragraph content
// (a nested list or table inside the quote) keeps its own construct and loses
// the quote association — logged, a documented v1 limit.
func (w *writer) blockquote(ctx context.Context, b *cssbox.Box, out *[]docx.Block) error {
	if boxwalk.HasInlineContent(b) {
		w.appendParagraph(out, b, "Quote")
		return nil
	}
	for _, c := range b.Children {
		if boxwalk.IsBlockContainer(c) && boxwalk.HasInlineContent(c) &&
			(c.SemTag == "" || c.SemTag == "p") && c.HeadingLvl == 0 {
			w.appendParagraph(out, c, "Quote")
			continue
		}
		w.opts.Logf("docxwrite: non-paragraph content inside a blockquote keeps its own construct")
		if err := w.block(ctx, c, out); err != nil {
			return err
		}
	}
	return nil
}
