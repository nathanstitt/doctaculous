package doctaculous

import (
	"context"
	"fmt"
	"io"

	"github.com/nathanstitt/doctaculous/pkg/render/epubwrite"
)

// EPUBOptions controls conversion to EPUB.
type EPUBOptions struct {
	// Title is the book's dc:title; the first <h1>'s text when empty, else
	// "Document".
	Title string
	// Logf receives degradation diagnostics (nil -> no-op).
	Logf func(string, ...any)
}

func (o EPUBOptions) toWriterOptions() epubwrite.Options {
	return epubwrite.Options{
		Title: o.Title,
		Logf:  o.Logf,
	}
}

// WriteEPUB writes an opened document to out as an EPUB 3 package. Like the
// Markdown and HTML writers it works on any document that can produce a
// cssbox tree, and it writes structure: chapters split at each <h1> (a
// heading-less document is one chapter), a nav.xhtml table of contents, and
// XHTML content documents serialized by the HTML writer.
func (d *Document) WriteEPUB(ctx context.Context, out io.Writer, opts EPUBOptions) error {
	rt, ok := d.r.(reflowTree)
	if !ok {
		return fmt.Errorf("doctaculous: WriteEPUB: document has no convertible structure")
	}
	wopts := opts.toWriterOptions()
	// Embed images through the source's own resource loader when the backend
	// retained one; without one, non-data references stay as-is (logged —
	// data: URIs are always kept inline and round-trip verbatim).
	if rr, ok := d.r.(reflowResources); ok {
		wopts.Loader = rr.resourceLoader()
	}
	if err := epubwrite.Write(ctx, rt.cssboxRoot(), out, wopts); err != nil {
		return fmt.Errorf("doctaculous: write epub: %w", err)
	}
	return nil
}

// ConvertHTMLToEPUB reads HTML from in, lays it out, and writes an .epub to
// out. It is a convenience wrapper over Convert.
func ConvertHTMLToEPUB(ctx context.Context, in io.Reader, out io.Writer, opts EPUBOptions) error {
	return Convert(ctx, in, out, ConvertOptions{
		From: FormatHTML,
		To:   FormatEPUB,
		EPUB: opts,
		Logf: opts.Logf,
	})
}
