package doctaculous

import (
	"context"
	"fmt"
	"io"

	"github.com/nathanstitt/doctaculous/pkg/render/pdfwrite"
)

// PDFOptions controls HTML/DOCX -> PDF conversion.
type PDFOptions struct {
	// PageWidthPt, PageHeightPt set the PDF page size in points; default US Letter
	// (612x792) when zero.
	PageWidthPt, PageHeightPt float64
	// MarginPt is the uniform content margin in points; default 36 (0.5in) when zero.
	// A negative value means no margin (0).
	MarginPt float64
	// Print, when true, makes the cascade honor @media print rules (and exclude
	// screen-only rules). Default false (screen context).
	Print bool
	// Title sets the PDF /Info /Title metadata.
	Title string
	// Workers caps the goroutines used to render pages concurrently. Defaults to
	// GOMAXPROCS when zero (matching RasterOptions.Workers).
	Workers int
	// Logf receives degradation diagnostics (nil -> no-op).
	Logf func(string, ...any)
	// BundledFonts selects hermetic bundled-font mode for the HTML layout that feeds the
	// PDF writer: non-embedded families resolve only from the bundled substitutes, never
	// the host's installed OS fonts. Default false = system mode. Set this for
	// reproducible, reliably-extractable output (the bundled faces have stable ToUnicode
	// mappings). Ignored for a DOCX input (DOCX layout is already bundled-only).
	BundledFonts bool
}

// defaultMarginPt is the 0.5in content margin applied when PDFOptions.MarginPt is
// left zero. A negative MarginPt requests no margin.
const defaultMarginPt = 36

func (o PDFOptions) toWriterOptions() pdfwrite.Options {
	margin := o.MarginPt
	switch {
	case margin < 0:
		margin = 0
	case margin == 0:
		margin = defaultMarginPt
	}
	return pdfwrite.Options{
		PageWidthPt:  o.PageWidthPt,
		PageHeightPt: o.PageHeightPt,
		MarginPt:     margin,
		Title:        o.Title,
		Workers:      o.Workers,
		Logf:         o.Logf,
	}
}

// ConvertHTMLToPDF reads HTML from in, lays it out, and writes a PDF to out. Text is
// embedded as real, searchable/selectable glyphs (Type0/Identity-H for TrueType
// faces, a simple Type1 font for the bundled sans/serif substitutes). When
// opts.Print is set the cascade honors @media print rules.
func ConvertHTMLToPDF(ctx context.Context, in io.Reader, out io.Writer, opts PDFOptions) error {
	data, err := io.ReadAll(in)
	if err != nil {
		return fmt.Errorf("doctaculous: read html: %w", err)
	}
	htmlOpts := []HTMLOption{}
	if opts.Print {
		htmlOpts = append(htmlOpts, WithPrintMedia())
	}
	if opts.Logf != nil {
		htmlOpts = append(htmlOpts, WithLogf(opts.Logf))
	}
	if opts.BundledFonts {
		htmlOpts = append(htmlOpts, WithBundledFonts())
	}
	doc, err := OpenHTMLBytes(data, htmlOpts...)
	if err != nil {
		return err
	}
	return doc.WritePDF(ctx, out, opts)
}

// WritePDF writes an opened reflow document (HTML or DOCX) to out as a PDF. It
// returns an error if the document is not a reflow document (e.g. an opened PDF).
func (d *Document) WritePDF(ctx context.Context, out io.Writer, opts PDFOptions) error {
	rp, ok := d.r.(reflowPages)
	if !ok {
		return fmt.Errorf("doctaculous: WritePDF: document is not a reflow document")
	}
	if err := pdfwrite.WriteDocument(ctx, out, rp.layoutPages(), opts.toWriterOptions()); err != nil {
		return fmt.Errorf("doctaculous: write pdf: %w", err)
	}
	return nil
}
