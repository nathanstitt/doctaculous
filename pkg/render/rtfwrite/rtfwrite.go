// Package rtfwrite renders a cssbox tree (the shared box model produced by
// the HTML, DOCX, Markdown, and PDF-extraction frontends) to a Rich Text
// Format document. Like the Markdown and DOCX writers — and unlike the raster
// and PDF backends — it is a STRUCTURE writer: it walks the box tree directly,
// reading the semantic annotations (SemTag/HeadingLvl/Href), display kinds,
// and computed style, and emits headings, paragraphs, emphasis, hyperlinks,
// lists, block quotes, code blocks, tables, and embedded pictures as native
// RTF constructs. It is deliberately not layout-faithful: floats, positioning,
// and exact CSS geometry degrade to normal flow.
//
// Every mapping is chosen so this repo's own RTF reader (pkg/rtf) round-trips
// it: block semantics ride on stylesheet names (\s styles "heading N", Quote,
// CodeBlock, HorizontalRule — with \outlinelevel as the heading fallback any
// other reader also understands), lists on \ls/\ilvl plus a literal \pntext
// marker, hyperlinks on HYPERLINK fields, inline code on the monospace font,
// tables on \trowd/\cellx rows (\trhdr header rows; spanned cells duplicated
// into every covered slot, the same expansion the Markdown writer performs),
// and pictures on \pict png/jpeg blobs.
package rtfwrite

import (
	"context"
	"fmt"
	"io"
	"strings"

	// The image decoders imageRun's DecodeConfig relies on, registered here so
	// the writer is self-sufficient.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
	"github.com/nathanstitt/doctaculous/pkg/render/internal/boxwalk"
	"github.com/nathanstitt/doctaculous/pkg/resource"
)

// Options configures RTF output.
type Options struct {
	// PageWidthPt, PageHeightPt set the page size in points; default US Letter
	// (612x792) when zero.
	PageWidthPt, PageHeightPt float64
	// MarginPt is the uniform page margin in points; default 72 (1in) when
	// zero. A negative value means no margin (0).
	MarginPt float64
	// Loader resolves image refs (an <img> src) to bytes for embedding as
	// \pict blobs. data: URIs decode without a loader; for the rest, nil means
	// images degrade to their alt text (logged).
	Loader resource.ResourceLoader
	// Logf receives degradation diagnostics (e.g. a construct the writer
	// cannot represent). nil -> no-op.
	Logf func(string, ...any)
}

// Paragraph style numbers — the stylesheet entries the reader maps back to
// block semantics (heading levels are \s1..\s6).
const (
	styleQuote          = 15
	styleCodeBlock      = 16
	styleHorizontalRule = 17
)

// Write renders the cssbox tree rooted at root to w as a complete RTF
// document. root is expected to be a finalized tree (post box-generation
// fixups). A nil root writes a valid empty document.
func Write(ctx context.Context, root *cssbox.Box, w io.Writer, opts Options) error {
	if opts.Logf == nil {
		opts.Logf = func(string, ...any) {}
	}
	wr := &writer{opts: opts, ctx: ctx}
	if root != nil {
		if err := wr.block(ctx, root); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(w, wr.document()); err != nil {
		return fmt.Errorf("rtfwrite: %w", err)
	}
	return nil
}

// writer accumulates the document body and the numbering state.
type writer struct {
	opts Options
	// ctx is the Write call's context, held for the walk-driven image fetches
	// (the CollectRuns callback has no context parameter). The walk is
	// single-goroutine and the writer does not outlive Write.
	ctx  context.Context
	body strings.Builder

	// orderedLists counts the ordered-list instances allocated so far. Bullets
	// share \ls1; each ordered list gets its own \ls so adjacent ordered lists
	// stay separate through the reader (mirroring the DOCX writer's numbering
	// instances).
	orderedLists int
}

// document assembles the full RTF file around the accumulated body.
func (w *writer) document() string {
	pageW := w.opts.PageWidthPt
	pageH := w.opts.PageHeightPt
	if pageW <= 0 || pageH <= 0 {
		pageW, pageH = 612, 792
	}
	margin := w.opts.MarginPt
	switch {
	case margin < 0:
		margin = 0
	case margin == 0:
		margin = 72
	}
	var sb strings.Builder
	sb.WriteString(`{\rtf1\ansi\ansicpg1252\deff0\uc1` + "\n")
	sb.WriteString(`{\fonttbl{\f0\froman Times New Roman;}{\f1\fmodern Courier New;}}` + "\n")
	// Color 1 is the hyperlink blue.
	sb.WriteString(`{\colortbl ;\red5\green99\blue193;}` + "\n")
	sb.WriteString(`{\stylesheet{\s0 Normal;}` +
		`{\s1 heading 1;}{\s2 heading 2;}{\s3 heading 3;}{\s4 heading 4;}{\s5 heading 5;}{\s6 heading 6;}` +
		fmt.Sprintf(`{\s%d Quote;}{\s%d CodeBlock;}{\s%d HorizontalRule;}}`,
			styleQuote, styleCodeBlock, styleHorizontalRule) + "\n")
	fmt.Fprintf(&sb, `\paperw%d\paperh%d\margl%d\margr%d\margt%d\margb%d`+"\n",
		twips(pageW), twips(pageH), twips(margin), twips(margin), twips(margin), twips(margin))
	sb.WriteString(w.body.String())
	sb.WriteString("}\n")
	return sb.String()
}

// twips converts points to twentieths of a point.
func twips(pt float64) int { return int(pt*20 + 0.5) }

// block dispatches one block-level box, mirroring the DOCX writer's walk. A
// box that is not itself a recognized construct recurses into its children so
// wrapper/anonymous boxes are transparent.
func (w *writer) block(ctx context.Context, b *cssbox.Box) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	switch {
	case b.HeadingLvl >= 1 && b.HeadingLvl <= 6:
		w.heading(b)
	case b.SemTag == "blockquote":
		return w.blockquote(ctx, b)
	case b.SemTag == "pre":
		w.codeBlock(b)
	case b.SemTag == "hr":
		w.horizontalRule()
	case b.Display == cssbox.DisplayTable:
		return w.table(b)
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

// heading emits an <hN> box: the "heading N" style plus \outlinelevel, with
// bold and a level-scaled size as direct formatting so any RTF reader shows a
// heading even without stylesheet support.
func (w *writer) heading(b *cssbox.Box) {
	lvl := b.HeadingLvl
	// Half-point sizes tracking the HTML defaults (h1 24pt .. h6 8pt).
	sizes := [6]int{48, 36, 28, 24, 20, 16}
	props := fmt.Sprintf(`\s%d\outlinelevel%d\b\fs%d`, lvl, lvl-1, sizes[lvl-1])
	w.emitParagraph(w.inlineRuns(b), props, justifyOf(b))
}

// blockquote emits the box's paragraphs with the Quote style (the reader maps
// the style name back to the blockquote semantic). Non-paragraph content (a
// nested list or table inside the quote) keeps its own construct and loses
// the quote association — logged, a documented v1 limit.
func (w *writer) blockquote(ctx context.Context, b *cssbox.Box) error {
	quoteProps := fmt.Sprintf(`\s%d\li720`, styleQuote)
	if boxwalk.HasInlineContent(b) {
		w.emitParagraph(w.inlineRuns(b), quoteProps, justifyOf(b))
		return nil
	}
	for _, c := range b.Children {
		if boxwalk.IsBlockContainer(c) && boxwalk.HasInlineContent(c) &&
			(c.SemTag == "" || c.SemTag == "p") && c.HeadingLvl == 0 {
			w.emitParagraph(w.inlineRuns(c), quoteProps, justifyOf(c))
			continue
		}
		w.opts.Logf("rtfwrite: non-paragraph content inside a blockquote keeps its own construct")
		if err := w.block(ctx, c); err != nil {
			return err
		}
	}
	return nil
}

// codeBlock emits a <pre> box as ONE CodeBlock-styled paragraph in the
// monospace font whose lines are separated by \line. The reader rebuilds the
// newlines, so the block's raw text round-trips as a single fenced block.
func (w *writer) codeBlock(b *cssbox.Box) {
	text := strings.TrimRight(boxwalk.RawText(b), "\n")
	if text == "" {
		return
	}
	fmt.Fprintf(&w.body, `{\pard\s%d {\f1 %s}\par}`+"\n", styleCodeBlock, escapeRTF(text))
}

// horizontalRule emits an <hr> as an empty HorizontalRule-styled paragraph.
func (w *writer) horizontalRule() {
	fmt.Fprintf(&w.body, `{\pard\s%d \par}`+"\n", styleHorizontalRule)
}
