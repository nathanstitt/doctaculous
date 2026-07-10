package doctaculous

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ConvertOptions configures the generic conversion entry points (Convert,
// ConvertFile, and Document.Write). Only the nested options matching the
// target format are consulted — e.g. PDF for To == FormatPDF — so a caller
// sets exactly the knobs its conversion uses. Logf and BundledFonts are
// cross-cutting and propagate into every stage that does not set its own.
type ConvertOptions struct {
	// From is the input format. FormatUnknown (the zero value) means detect it
	// from content — and, for ConvertFile, the input filename.
	From Format
	// To is the output format. Convert requires it; ConvertFile infers it from
	// the output filename's extension when unset.
	To Format

	// HTML configures input layout (viewport, pagination, resource loading,
	// fonts) for inputs that flow through the HTML pipeline; PDF and DOCX inputs
	// ignore it. Options here are applied last, so they win over the derived
	// defaults (Logf, BundledFonts, print media).
	HTML []HTMLOption
	// PDF applies when To == FormatPDF. PDF.Print additionally switches an HTML
	// input's cascade to the print media context.
	PDF PDFOptions
	// DOCX applies when To == FormatDOCX.
	DOCX DOCXOptions
	// Markdown applies when To == FormatMarkdown or FormatText.
	Markdown MarkdownOptions
	// CSV applies when To == FormatCSV or FormatTSV.
	CSV CSVOptions
	// XLSX applies when To == FormatXLSX.
	XLSX XLSXOptions
	// RTF applies when To == FormatRTF.
	RTF RTFOptions
	// HTMLOut applies when To == FormatHTML.
	HTMLOut HTMLWriteOptions
	// Image applies when To == FormatPNG or FormatJPEG; Image.Page selects the
	// single page an image conversion encodes (an io.Writer holds one image).
	Image ImageOptions

	// Logf receives layout/degradation diagnostics from every stage that does
	// not set its own logger. nil -> no-op.
	Logf func(string, ...any)
	// BundledFonts selects hermetic bundled-font mode for the whole conversion
	// (input layout and rasterization). Default false = installed system fonts.
	BundledFonts bool
}

// inputOptions assembles the open-time options for reflow inputs: the derived
// cross-cutting defaults first, then the caller's HTML options so they win.
// This mirrors what ConvertHTMLToPDF has always done.
func (o ConvertOptions) inputOptions() []OpenOption {
	var opts []OpenOption
	if o.Logf != nil {
		opts = append(opts, WithLogf(o.Logf))
	}
	if o.BundledFonts {
		opts = append(opts, WithBundledFonts())
	}
	if o.To == FormatPDF && o.PDF.Print {
		opts = append(opts, WithPrintMedia())
	}
	return append(opts, o.HTML...)
}

// Convert reads one document from in and writes it to out as opts.To. The
// input format is opts.From, or detected from content when unset (Markdown and
// plain text have no content magic, so in-memory conversion from those formats
// needs an explicit From). Unsupported and same-format pairs fail fast with
// ErrUnsupportedFormat / ErrSameFormat before any layout work.
func Convert(ctx context.Context, in io.Reader, out io.Writer, opts ConvertOptions) error {
	data, err := io.ReadAll(in)
	if err != nil {
		return fmt.Errorf("doctaculous: read input: %w", err)
	}
	from := opts.From
	if from == FormatUnknown {
		from = DetectFormat(data, "")
		if from == FormatUnknown {
			return fmt.Errorf("doctaculous: convert: cannot detect the input format (set ConvertOptions.From): %w", ErrUnknownFormat)
		}
	}
	if err := CanConvert(from, opts.To); err != nil {
		return err
	}
	doc, err := openDetected(ctx, from, data, "", opts.inputOptions())
	if err != nil {
		return err
	}
	return doc.Write(ctx, out, opts.To, opts)
}

// ConvertFile converts the document at inPath (a file path, or an http(s) URL
// fetched as a web page) into outPath, creating or truncating outPath. The
// input format comes from opts.From, else content + filename detection; the
// output format from opts.To, else the outPath extension.
func ConvertFile(ctx context.Context, inPath, outPath string, opts ConvertOptions) (err error) {
	if opts.To == FormatUnknown {
		opts.To = FormatFromPath(outPath)
		if opts.To == FormatUnknown {
			return fmt.Errorf("doctaculous: convert to %q: cannot infer the output format from the extension (set ConvertOptions.To): %w", outPath, ErrUnknownFormat)
		}
	}

	from := opts.From
	var (
		data []byte
		dir  string
	)
	if isHTTPURL(inPath) {
		// A URL is always fetched as a web page.
		if from != FormatUnknown && from != FormatHTML {
			return fmt.Errorf("doctaculous: convert %q: an http(s) URL is fetched as a web page (HTML), not %s: %w", inPath, from, ErrUnsupportedFormat)
		}
		from = FormatHTML
	} else {
		data, err = os.ReadFile(inPath)
		if err != nil {
			return fmt.Errorf("doctaculous: open %q: %w", inPath, err)
		}
		dir = filepath.Dir(inPath)
		if from == FormatUnknown {
			from = DetectFormat(data, inPath)
			if from == FormatUnknown {
				return fmt.Errorf("doctaculous: convert %q: cannot detect the input format (set ConvertOptions.From): %w", inPath, ErrUnknownFormat)
			}
		}
	}
	if err := CanConvert(from, opts.To); err != nil {
		return err
	}

	var doc *Document
	if isHTTPURL(inPath) {
		doc, err = OpenURL(inPath, opts.inputOptions()...)
	} else {
		doc, err = openDetected(ctx, from, data, dir, opts.inputOptions())
	}
	if err != nil {
		return err
	}

	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("doctaculous: create %q: %w", outPath, err)
	}
	defer func() {
		// Surface a close error only if the conversion itself succeeded (a flush
		// of buffered bytes can fail on a full disk).
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	return doc.Write(ctx, f, opts.To, opts)
}

// Write writes the document to out as the named format — the output half of
// Convert for an already-opened Document, and the one place output formats
// dispatch. It enforces CanConvert against the document's source format, so
// converting a document to its own format returns ErrSameFormat (the
// format-specific writers — WriteHTML, WritePDF, WriteMarkdown, ... — remain
// unrestricted).
func (d *Document) Write(ctx context.Context, out io.Writer, to Format, opts ConvertOptions) error {
	if err := CanConvert(d.Format(), to); err != nil {
		return err
	}
	switch to {
	case FormatPDF:
		pdfOpts := opts.PDF
		if pdfOpts.Logf == nil {
			pdfOpts.Logf = opts.Logf
		}
		if opts.BundledFonts {
			pdfOpts.BundledFonts = true
		}
		return d.WritePDF(ctx, out, pdfOpts)
	case FormatDOCX:
		docxOpts := opts.DOCX
		if docxOpts.Logf == nil {
			docxOpts.Logf = opts.Logf
		}
		return d.WriteDOCX(ctx, out, docxOpts)
	case FormatMarkdown, FormatText:
		mdOpts := opts.Markdown
		if mdOpts.Logf == nil {
			mdOpts.Logf = opts.Logf
		}
		if to == FormatText {
			return d.WriteText(ctx, out, mdOpts)
		}
		return d.WriteMarkdown(ctx, out, mdOpts)
	case FormatCSV, FormatTSV:
		csvOpts := opts.CSV
		if csvOpts.Logf == nil {
			csvOpts.Logf = opts.Logf
		}
		if to == FormatTSV {
			return d.WriteTSV(ctx, out, csvOpts)
		}
		return d.WriteCSV(ctx, out, csvOpts)
	case FormatXLSX:
		xlsxOpts := opts.XLSX
		if xlsxOpts.Logf == nil {
			xlsxOpts.Logf = opts.Logf
		}
		return d.WriteXLSX(ctx, out, xlsxOpts)
	case FormatRTF:
		rtfOpts := opts.RTF
		if rtfOpts.Logf == nil {
			rtfOpts.Logf = opts.Logf
		}
		return d.WriteRTF(ctx, out, rtfOpts)
	case FormatHTML:
		htmlOpts := opts.HTMLOut
		if htmlOpts.Logf == nil {
			htmlOpts.Logf = opts.Logf
		}
		return d.WriteHTML(ctx, out, htmlOpts)
	case FormatPNG, FormatJPEG:
		imgOpts := opts.Image
		imgOpts.Format = to
		if imgOpts.Raster.Logf == nil {
			imgOpts.Raster.Logf = opts.Logf
		}
		if opts.BundledFonts {
			imgOpts.Raster.BundledFonts = true
		}
		return d.WriteImage(ctx, out, imgOpts.Page, imgOpts)
	default:
		// CanConvert vets the output role above, so this is only reachable for a
		// format whose capability bit is on but whose writer case is missing — a
		// programming error worth a loud, precise message.
		return fmt.Errorf("doctaculous: write: no writer wired for %s output: %w", to, ErrUnsupportedFormat)
	}
}
