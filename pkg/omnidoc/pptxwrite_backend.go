package omnidoc

import (
	"context"
	"fmt"
	"io"

	"github.com/nathanstitt/omnidoc/pkg/render/pptxwrite"
)

// PPTXOptions controls conversion to PPTX.
type PPTXOptions struct {
	// SlideWidthPt, SlideHeightPt set the slide size in points; default 16:9
	// (960x540, PowerPoint's default) when zero.
	SlideWidthPt, SlideHeightPt float64
	// Logf receives degradation diagnostics (nil -> no-op).
	Logf func(string, ...any)
}

func (o PPTXOptions) toWriterOptions() pptxwrite.Options {
	return pptxwrite.Options{
		SlideWidthPt:  o.SlideWidthPt,
		SlideHeightPt: o.SlideHeightPt,
		Logf:          o.Logf,
	}
}

// WritePPTX writes an opened document to out as a PresentationML (.pptx)
// deck. Like the Markdown and DOCX writers it works on any document that can
// produce a cssbox tree, and it writes structure, not layout: every <h1>/<h2>
// starts a new slide with that heading as the title, and the blocks that
// follow become the slide's body (text, lists, native tables, pictures).
func (d *Document) WritePPTX(ctx context.Context, out io.Writer, opts PPTXOptions) error {
	root, err := structureRoot(d, "WritePPTX")
	if err != nil {
		return err
	}
	wopts := opts.toWriterOptions()
	// Embed images through the source's own resource loader when the backend
	// retained one; without one the writer degrades images to their alt text
	// (data: URIs always embed).
	if rr, ok := d.r.(reflowResources); ok {
		wopts.Loader = rr.resourceLoader()
	}
	if err := pptxwrite.Write(ctx, root, out, wopts); err != nil {
		return fmt.Errorf("omnidoc: write pptx: %w", err)
	}
	return nil
}
