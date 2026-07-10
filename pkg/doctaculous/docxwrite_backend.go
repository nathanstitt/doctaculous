package doctaculous

import (
	"context"
	"fmt"
	"io"

	"github.com/nathanstitt/doctaculous/pkg/render/docxwrite"
)

// DOCXOptions controls conversion to DOCX.
type DOCXOptions struct {
	// PageWidthPt, PageHeightPt set the section page size in points; default US
	// Letter (612x792) when zero.
	PageWidthPt, PageHeightPt float64
	// MarginPt is the uniform page margin in points; default 72 (1in, Word's
	// default) when zero. A negative value means no margin (0).
	MarginPt float64
	// Logf receives degradation diagnostics (nil -> no-op).
	Logf func(string, ...any)
}

func (o DOCXOptions) toWriterOptions() docxwrite.Options {
	return docxwrite.Options{
		PageWidthPt:  o.PageWidthPt,
		PageHeightPt: o.PageHeightPt,
		MarginPt:     o.MarginPt,
		Logf:         o.Logf,
	}
}

// WriteDOCX writes an opened document to out as a WordprocessingML (.docx)
// package. Like the Markdown and HTML writers it works on any document that can
// produce a cssbox tree — an opened HTML, Markdown, or text reflow document, or
// an opened PDF (whose logical structure is recovered by extraction) — and it
// writes structure (headings, paragraphs, emphasis, links, lists, quotes, code
// blocks), not layout geometry.
func (d *Document) WriteDOCX(ctx context.Context, out io.Writer, opts DOCXOptions) error {
	rt, ok := d.r.(reflowTree)
	if !ok {
		return fmt.Errorf("doctaculous: WriteDOCX: document has no convertible structure")
	}
	wopts := opts.toWriterOptions()
	// Embed images through the source's own resource loader when the backend
	// retained one (HTML/Markdown files and URLs, DOCX media); without one the
	// writer degrades images to their alt text.
	if rr, ok := d.r.(reflowResources); ok {
		wopts.Loader = rr.resourceLoader()
	}
	if err := docxwrite.Write(ctx, rt.cssboxRoot(), out, wopts); err != nil {
		return fmt.Errorf("doctaculous: write docx: %w", err)
	}
	return nil
}

// ConvertHTMLToDOCX reads HTML from in, lays it out, and writes a .docx to out.
// It is a convenience wrapper over Convert.
func ConvertHTMLToDOCX(ctx context.Context, in io.Reader, out io.Writer, opts DOCXOptions) error {
	return Convert(ctx, in, out, ConvertOptions{
		From: FormatHTML,
		To:   FormatDOCX,
		DOCX: opts,
		Logf: opts.Logf,
	})
}
