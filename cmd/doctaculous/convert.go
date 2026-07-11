package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"image"
	"os"
	"runtime"
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/doctaculous"
)

// convertCmd parses flags for the "convert" subcommand — the generic verb that
// converts any supported input (PDF, DOCX, HTML file, or http(s) URL) to any
// supported output (pdf, md, txt, html, png, jpg). The input format is
// detected from content and extension (--from overrides); the output format
// comes from the output filename's extension (--to overrides). Converting a
// document to its own format is not supported.
func convertCmd(args []string) error {
	fs := flag.NewFlagSet("convert", flag.ContinueOnError)
	var (
		in   = fs.String("in", "", "input document or http(s) URL (alternative to the first positional argument)")
		out  = fs.String("out", "", "output file (alternative to the second positional argument; \"-\" writes to stdout and requires --to)")
		from = fs.String("from", "", "input format override: pdf, docx, xlsx, html, md, txt, csv, or tsv (default: detected from content and extension)")
		to   = fs.String("to", "", "output format override: pdf, docx, xlsx, md, txt, html, csv, tsv, png, or jpg (default: from the output extension)")

		pageSize = fs.String("page-size", "letter", "HTML pagination: \"letter\" paginates onto US-Letter pages (default), \"tall\" renders one tall page")
		workers  = fs.Int("workers", runtime.GOMAXPROCS(0), "max concurrent page renderers (pdf and image outputs)")

		// pdf output
		pageW  = fs.Float64("page-width", 0, "PDF page width in points (default US Letter, 612)")
		pageH  = fs.Float64("page-height", 0, "PDF page height in points (default US Letter, 792)")
		margin = fs.Float64("margin", -1, "PDF content margin in points (default 36 = 0.5in; 0 for none)")
		print  = fs.Bool("print", false, "honor @media print rules (exclude screen-only rules)")
		title  = fs.String("title", "", "PDF /Title metadata")

		// md/txt output
		plain = fs.Bool("plain", false, "emit plain text instead of Markdown (md output)")

		// html output
		fragment = fs.Bool("fragment", false, "emit only body markup, no <html>/<head> wrapper (html output)")

		// png/jpg output
		dpi       = fs.Float64("dpi", 150, "render resolution in DPI (image output; with --max-width/--max-height: a resolution ceiling)")
		maxWidth  = fs.Int("max-width", 0, "fit the render within this many pixels wide, aspect preserved (image output; 0 = off)")
		maxHeight = fs.Int("max-height", 0, "fit the render within this many pixels tall, aspect preserved (image output; 0 = off)")
		quality   = fs.Int("quality", 90, "JPEG quality 1-100 (jpg output)")
		page      = fs.Int("page", 1, "1-based page to render (image output)")
		pages     = fs.String("pages", "", "page range for image output, e.g. 1-3,5 or \"all\" (overrides --page; needs %d in the output name)")

		bundledFonts = fs.Bool("bundled-fonts", false, "use only the bundled substitute fonts (hermetic); default uses installed system fonts")
	)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: doctaculous convert <input.pdf|.docx|.html|.md|.txt|URL> <output.pdf|.docx|.md|.txt|.html|.png|.jpg> [flags]\n") //nolint:errcheck // stderr write
		fs.PrintDefaults()
	}
	if err := fs.Parse(reorderArgs(args, convertValueFlags)); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil // -h/--help printed usage; not an error
		}
		return err
	}
	input, output, err := resolveInOut(*in, *out, fs.Args())
	if err != nil {
		fs.Usage()
		return err
	}
	if *pageSize != "letter" && *pageSize != "tall" && *pageSize != "" {
		return fmt.Errorf("unsupported --page-size %q (want \"letter\" or \"tall\")", *pageSize)
	}
	if *workers < 1 {
		return fmt.Errorf("--workers must be at least 1, got %d", *workers)
	}

	toFormat, err := resolveTargetFormat(*to, output)
	if err != nil {
		return err
	}
	var fromFormat doctaculous.Format
	if *from != "" {
		if fromFormat, err = doctaculous.ParseFormat(*from); err != nil {
			return fmt.Errorf("--from: %w", err)
		}
	}

	doc, err := openInput(input, fromFormat, *pageSize, *bundledFonts, *print)
	if err != nil {
		return fmt.Errorf("open %s: %w", input, err)
	}

	// Image targets fan out to one encoded file per selected page; everything
	// else streams one document to the writer.
	if toFormat == doctaculous.FormatPNG || toFormat == doctaculous.FormatJPEG {
		if output == "-" {
			return fmt.Errorf("image output requires a file path (use %%d for multi-page output)")
		}
		if err := doctaculous.CanConvert(doc.Format(), toFormat); err != nil {
			return err
		}
		if *maxWidth < 0 || *maxHeight < 0 {
			return fmt.Errorf("--max-width/--max-height must be non-negative, got %d/%d", *maxWidth, *maxHeight)
		}
		indices, err := resolvePages(*pages, *page, doc.PageCount())
		if err != nil {
			return err
		}
		imgOpts := doctaculous.ImageOptions{
			Format:  toFormat,
			Quality: *quality,
			Raster: doctaculous.RasterOptions{
				DPI:         fitDPI(fs, *dpi, *maxWidth, *maxHeight),
				MaxWidthPx:  *maxWidth,
				MaxHeightPx: *maxHeight,
				Workers:     *workers, BundledFonts: *bundledFonts,
			},
		}
		return renderPages(doc, indices, output, imgOpts)
	}

	// Validate the pair before creating the output, so a rejected conversion
	// (e.g. same-format) leaves no empty file behind.
	if err := doctaculous.CanConvert(doc.Format(), toFormat); err != nil {
		return err
	}

	// The margin flag uses -1 as "unset" so 0 can mean "no margin"; PDFOptions
	// treats 0 as the 0.5in default and a negative value as none.
	marginPt := *margin
	if marginPt < 0 {
		marginPt = 0
	}
	opts := doctaculous.ConvertOptions{
		PDF: doctaculous.PDFOptions{
			PageWidthPt:  *pageW,
			PageHeightPt: *pageH,
			MarginPt:     marginPt,
			Print:        *print,
			Title:        *title,
			Workers:      *workers,
			BundledFonts: *bundledFonts,
		},
		DOCX: doctaculous.DOCXOptions{
			PageWidthPt:  *pageW,
			PageHeightPt: *pageH,
			MarginPt:     marginPt,
		},
		Markdown:     doctaculous.MarkdownOptions{Plain: *plain},
		HTMLOut:      doctaculous.HTMLWriteOptions{Fragment: *fragment},
		BundledFonts: *bundledFonts,
	}

	w := os.Stdout
	if output != "-" {
		f, err := os.Create(output)
		if err != nil {
			return fmt.Errorf("create %s: %w", output, err)
		}
		defer func() { _ = f.Close() }()
		w = f
	}
	return doc.Write(context.Background(), w, toFormat, opts)
}

// resolveTargetFormat resolves the output format from the --to override or the
// output filename's extension.
func resolveTargetFormat(toFlag, output string) (doctaculous.Format, error) {
	if toFlag != "" {
		f, err := doctaculous.ParseFormat(toFlag)
		if err != nil {
			return doctaculous.FormatUnknown, fmt.Errorf("--to: %w", err)
		}
		return f, nil
	}
	if output == "-" {
		return doctaculous.FormatUnknown, fmt.Errorf("--to is required when writing to stdout")
	}
	f := doctaculous.FormatFromPath(output)
	if f == doctaculous.FormatUnknown {
		return f, fmt.Errorf("cannot infer the output format from %q; use a recognizable extension or --to (pdf, md, txt, html, png, jpg)", output)
	}
	return f, nil
}

// resolveInOut resolves the input and output from the --in/--out flags and the
// positional arguments (convert accepts "<input> <output>").
func resolveInOut(inFlag, outFlag string, positional []string) (in, out string, err error) {
	in, out = inFlag, outFlag
	switch len(positional) {
	case 0:
	case 1:
		switch {
		case in == "":
			in = positional[0]
		case out == "":
			out = positional[0]
		default:
			return "", "", fmt.Errorf("unexpected argument %q: input and output are already set via --in/--out", positional[0])
		}
	case 2:
		if in != "" || out != "" {
			return "", "", fmt.Errorf("give the input/output either as positional arguments or via --in/--out, not both")
		}
		in, out = positional[0], positional[1]
	default:
		return "", "", fmt.Errorf("expected <input> <output>, got %d positional arguments", len(positional))
	}
	if in == "" {
		return "", "", fmt.Errorf("no input document given (use --in <file> or a positional argument)")
	}
	if out == "" {
		return "", "", fmt.Errorf("no output given (use --out <file> or a second positional argument)")
	}
	return in, out, nil
}

// openInput is the one detection-based opener behind every subcommand: an
// http(s) URL is fetched as a web page, and a file opens by content detection
// (or as the explicit from format, when set). pageSize/bundledFonts/printMedia
// shape the layout of inputs that flow through the HTML pipeline and are
// ignored by PDF/DOCX inputs.
func openInput(input string, from doctaculous.Format, pageSize string, bundledFonts, printMedia bool) (*doctaculous.Document, error) {
	opts := htmlOpts(pageSize, bundledFonts, printMedia)
	if isHTTPURL(input) {
		if from != doctaculous.FormatUnknown && from != doctaculous.FormatHTML {
			return nil, fmt.Errorf("an http(s) URL is always fetched as a web page; --from %s does not apply", from)
		}
		return doctaculous.OpenURL(input, opts...)
	}
	if from != doctaculous.FormatUnknown {
		return doctaculous.OpenAs(from, input, opts...)
	}
	return doctaculous.Open(input, opts...)
}

// renderPages rasterizes the selected pages concurrently and writes one
// encoded image file per page (a %d in outPattern is the 1-based page number,
// and is required for a multi-page render). Failed pages are reported to
// stderr; successful pages are still written, and the first error is returned
// so scripts and CI detect a partial batch.
func renderPages(doc *doctaculous.Document, indices []int, outPattern string, imgOpts doctaculous.ImageOptions) error {
	if len(indices) > 1 && !strings.Contains(outPattern, "%d") {
		ext := "png"
		if imgOpts.Format == doctaculous.FormatJPEG {
			ext = "jpg"
		}
		return fmt.Errorf("rendering %d pages requires a %%d placeholder in the output name (e.g. page-%%d.%s)", len(indices), ext)
	}
	results := doc.RasterizePages(context.Background(), indices, imgOpts.Raster)

	var firstErr error
	written := 0
	for _, r := range results {
		if r.Err != nil {
			fmt.Fprintf(os.Stderr, "doctaculous: page %d: %v\n", r.Index+1, r.Err) //nolint:errcheck // stderr
			if firstErr == nil {
				firstErr = r.Err
			}
			continue
		}
		path := outputPath(outPattern, r.Index)
		if err := writeImageFile(path, r.Image, imgOpts); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			fmt.Fprintf(os.Stderr, "doctaculous: writing %s: %v\n", path, err) //nolint:errcheck // stderr
			continue
		}
		written++
	}
	if firstErr != nil {
		return fmt.Errorf("%d of %d pages failed; first error: %w", len(indices)-written, len(indices), firstErr)
	}
	return nil
}

// writeImageFile encodes img to path in the format selected by opts.
func writeImageFile(path string, img image.Image, opts doctaculous.ImageOptions) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		// Surface a close error only if encoding itself succeeded (a flush of
		// buffered bytes can fail on a full disk).
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	return doctaculous.EncodeImage(f, img, opts)
}

// convertValueFlags lists the "convert" flags that take their value as a
// separate token, for reorderArgs.
var convertValueFlags = map[string]bool{
	"-in": true, "--in": true,
	"-out": true, "--out": true,
	"-from": true, "--from": true,
	"-to": true, "--to": true,
	"-page-size": true, "--page-size": true,
	"-workers": true, "--workers": true,
	"-page-width": true, "--page-width": true,
	"-page-height": true, "--page-height": true,
	"-margin": true, "--margin": true,
	"-title": true, "--title": true,
	"-dpi": true, "--dpi": true,
	"-max-width": true, "--max-width": true,
	"-max-height": true, "--max-height": true,
	"-quality": true, "--quality": true,
	"-page": true, "--page": true,
	"-pages": true, "--pages": true,
}
