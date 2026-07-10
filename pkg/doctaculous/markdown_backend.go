package doctaculous

import (
	"context"
	"fmt"
	"io"

	"github.com/nathanstitt/doctaculous/pkg/render/markdown"
)

// MarkdownOptions controls HTML/DOCX -> Markdown (or plain-text) conversion.
type MarkdownOptions struct {
	// Plain renders plain text instead of Markdown: no heading hashes, emphasis
	// markers, or link URLs — just the document's text with block and table structure
	// preserved as whitespace. Default false (GFM Markdown).
	Plain bool
	// Logf receives degradation diagnostics (e.g. a table with a synthesized header
	// row). nil -> no-op.
	Logf func(string, ...any)
}

func (o MarkdownOptions) toWriterOptions() markdown.Options {
	return markdown.Options{Plain: o.Plain, Logf: o.Logf}
}

// ConvertHTMLToMarkdown reads HTML from in, lays it out, and writes GitHub-Flavored
// Markdown to out. Tables are emitted as GFM pipe tables (merged cells are expanded by
// duplicating their content across every covered slot, so the table stays rectangular);
// headings, lists, links, and emphasis map to their Markdown equivalents. Set
// opts.Plain to write plain text instead. It is a convenience wrapper over Convert.
func ConvertHTMLToMarkdown(ctx context.Context, in io.Reader, out io.Writer, opts MarkdownOptions) error {
	return Convert(ctx, in, out, ConvertOptions{
		From:     FormatHTML,
		To:       FormatMarkdown,
		Markdown: opts,
		Logf:     opts.Logf,
	})
}

// ConvertHTMLToText is ConvertHTMLToMarkdown in plain-text mode (opts.Plain forced
// true): it writes the document's text with structure preserved as whitespace, no
// Markdown syntax.
func ConvertHTMLToText(ctx context.Context, in io.Reader, out io.Writer, opts MarkdownOptions) error {
	opts.Plain = true
	return ConvertHTMLToMarkdown(ctx, in, out, opts)
}

// WriteMarkdown writes an opened reflow document (HTML or DOCX) to out as GitHub-
// Flavored Markdown. It works on any document that can produce a cssbox tree: an opened
// HTML or DOCX reflow document, or an opened PDF (whose logical structure is recovered by
// extraction). Set opts.Plain for plain text.
func (d *Document) WriteMarkdown(_ context.Context, out io.Writer, opts MarkdownOptions) error {
	rt, ok := d.r.(reflowTree)
	if !ok {
		return fmt.Errorf("doctaculous: WriteMarkdown: document is not a reflow document")
	}
	if err := markdown.Write(rt.cssboxRoot(), out, opts.toWriterOptions()); err != nil {
		return fmt.Errorf("doctaculous: write markdown: %w", err)
	}
	return nil
}

// WriteText writes an opened reflow document to out as plain text (WriteMarkdown with
// opts.Plain forced true).
func (d *Document) WriteText(ctx context.Context, out io.Writer, opts MarkdownOptions) error {
	opts.Plain = true
	return d.WriteMarkdown(ctx, out, opts)
}
