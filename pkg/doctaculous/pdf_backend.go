package doctaculous

import (
	"context"
	"fmt"
	"image"

	"github.com/nathanstitt/doctaculous/pkg/pdf"
	"github.com/nathanstitt/doctaculous/pkg/render/raster"
)

// pdfRenderer renders a parsed PDF document. The *pdf.Document is read-only after
// parsing, so it is shared across the page fan-out without locks.
type pdfRenderer struct {
	doc *pdf.Document
}

// Open reads and parses a PDF document from a file path. For DOCX and other
// reflowable formats use OpenDOCX.
func Open(path string) (*Document, error) {
	d, err := pdf.Open(path)
	if err != nil {
		return nil, err
	}
	return &Document{r: &pdfRenderer{doc: d}}, nil
}

// OpenBytes parses a PDF document from an in-memory byte slice. The slice is
// retained and must not be modified by the caller.
func OpenBytes(data []byte) (*Document, error) {
	d, err := pdf.Parse(data)
	if err != nil {
		return nil, err
	}
	return &Document{r: &pdfRenderer{doc: d}}, nil
}

func (r *pdfRenderer) pageCount() int { return r.doc.PageCount() }

func (r *pdfRenderer) renderPage(ctx context.Context, index int, opts RasterOptions) (image.Image, error) {
	pg, err := r.doc.Page(index)
	if err != nil {
		return nil, fmt.Errorf("page %d: %w", index, err)
	}
	return raster.RenderPage(ctx, pg, raster.Options{
		DPI:        opts.dpi(),
		Background: opts.Background,
		Logf:       opts.Logf,
	})
}
