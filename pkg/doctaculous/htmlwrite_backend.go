package doctaculous

import (
	"context"
	"fmt"
	"io"

	"github.com/nathanstitt/doctaculous/pkg/render/htmlwrite"
)

// HTMLWriteOptions controls conversion to HTML (PDF/DOCX/HTML -> HTML).
type HTMLWriteOptions struct {
	// Fragment, when true, emits only the body markup (no <!DOCTYPE>/<html>/<head>
	// wrapper), for embedding. Default false (a complete document).
	Fragment bool
	// Logf receives degradation diagnostics (nil -> no-op).
	Logf func(string, ...any)
}

func (o HTMLWriteOptions) toWriterOptions() htmlwrite.Options {
	return htmlwrite.Options{Fragment: o.Fragment, Logf: o.Logf}
}

// WriteHTML writes an opened document to out as HTML. It works on any document that can
// produce a cssbox tree: an opened HTML or DOCX reflow document, or an opened PDF (whose
// logical structure is recovered by extraction). Unlike GFM, HTML expresses table cell
// spans natively, so extracted tables round-trip their colspan/rowspan losslessly.
func (d *Document) WriteHTML(_ context.Context, out io.Writer, opts HTMLWriteOptions) error {
	rt, ok := d.r.(reflowTree)
	if !ok {
		return fmt.Errorf("doctaculous: WriteHTML: document has no convertible structure")
	}
	if err := htmlwrite.Write(rt.cssboxRoot(), out, opts.toWriterOptions()); err != nil {
		return fmt.Errorf("doctaculous: write html: %w", err)
	}
	return nil
}

// ConvertPDFToMarkdown reads a PDF from in, recovers its logical structure, and writes
// GitHub-Flavored Markdown to out. Set opts.Plain for plain text. This is a convenience
// wrapper over OpenBytes + WriteMarkdown.
func ConvertPDFToMarkdown(ctx context.Context, in io.Reader, out io.Writer, opts MarkdownOptions) error {
	data, err := io.ReadAll(in)
	if err != nil {
		return fmt.Errorf("doctaculous: read pdf: %w", err)
	}
	doc, err := OpenBytes(data)
	if err != nil {
		return err
	}
	return doc.WriteMarkdown(ctx, out, opts)
}

// ConvertPDFToHTML reads a PDF from in, recovers its logical structure, and writes HTML
// to out. A convenience wrapper over OpenBytes + WriteHTML.
func ConvertPDFToHTML(ctx context.Context, in io.Reader, out io.Writer, opts HTMLWriteOptions) error {
	data, err := io.ReadAll(in)
	if err != nil {
		return fmt.Errorf("doctaculous: read pdf: %w", err)
	}
	doc, err := OpenBytes(data)
	if err != nil {
		return err
	}
	return doc.WriteHTML(ctx, out, opts)
}
