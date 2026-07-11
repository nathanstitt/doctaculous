package doctaculous

import (
	"context"
	"fmt"
	"io"

	"github.com/nathanstitt/doctaculous/pkg/render/rtfwrite"
)

// RTFOptions controls conversion to RTF.
type RTFOptions struct {
	// PageWidthPt, PageHeightPt set the page size in points; default US Letter
	// (612x792) when zero.
	PageWidthPt, PageHeightPt float64
	// MarginPt is the uniform page margin in points; default 72 (1in) when
	// zero. A negative value means no margin (0).
	MarginPt float64
	// Logf receives degradation diagnostics (nil -> no-op).
	Logf func(string, ...any)
}

func (o RTFOptions) toWriterOptions() rtfwrite.Options {
	return rtfwrite.Options{
		PageWidthPt:  o.PageWidthPt,
		PageHeightPt: o.PageHeightPt,
		MarginPt:     o.MarginPt,
		Logf:         o.Logf,
	}
}

// WriteRTF writes an opened document to out as a Rich Text Format document.
// Like the Markdown and DOCX writers it works on any document that can
// produce a cssbox tree — an opened HTML, Markdown, or text reflow document,
// or an opened PDF (whose logical structure is recovered by extraction) — and
// it writes structure (headings, paragraphs, emphasis, links, lists, quotes,
// code blocks, tables, pictures), not layout geometry.
func (d *Document) WriteRTF(ctx context.Context, out io.Writer, opts RTFOptions) error {
	rt, ok := d.r.(reflowTree)
	if !ok {
		return fmt.Errorf("doctaculous: WriteRTF: document has no convertible structure")
	}
	wopts := opts.toWriterOptions()
	// Embed images through the source's own resource loader when the backend
	// retained one (HTML/Markdown files and URLs, DOCX media); without one the
	// writer degrades images to their alt text (data: URIs always embed).
	if rr, ok := d.r.(reflowResources); ok {
		wopts.Loader = rr.resourceLoader()
	}
	if err := rtfwrite.Write(ctx, rt.cssboxRoot(), out, wopts); err != nil {
		return fmt.Errorf("doctaculous: write rtf: %w", err)
	}
	return nil
}
