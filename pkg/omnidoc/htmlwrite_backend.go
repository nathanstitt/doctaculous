package omnidoc

import (
	"context"
	"fmt"
	"io"

	"github.com/nathanstitt/omnidoc/pkg/internal/htmlwrite"
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
	root, err := structureRoot(d, "WriteHTML")
	if err != nil {
		return err
	}
	if err := htmlwrite.Write(root, out, opts.toWriterOptions()); err != nil {
		return fmt.Errorf("omnidoc: write html: %w", err)
	}
	return nil
}
