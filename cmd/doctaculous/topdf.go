package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime"

	"github.com/nathanstitt/doctaculous/pkg/doctaculous"
)

// topdfCmd parses flags for the "topdf" subcommand, which converts a reflow
// document (HTML file, http(s) URL, or DOCX) into a PDF with real, searchable text.
// PDF inputs are rejected — the writer emits from the reflow engine, not a re-serialized
// PDF.
func topdfCmd(args []string) error {
	fs := flag.NewFlagSet("topdf", flag.ContinueOnError)
	var (
		in       = fs.String("in", "", "input document (alternative to the positional argument)")
		out      = fs.String("out", "", "output PDF file (required)")
		pageW    = fs.Float64("page-width", 0, "page width in points (default US Letter, 612)")
		pageH    = fs.Float64("page-height", 0, "page height in points (default US Letter, 792)")
		margin   = fs.Float64("margin", -1, "content margin in points (default 36 = 0.5in; 0 for none)")
		print    = fs.Bool("print", false, "honor @media print rules (exclude screen-only rules)")
		title    = fs.String("title", "", "PDF /Title metadata")
		workers  = fs.Int("workers", runtime.GOMAXPROCS(0), "max concurrent page renderers")
		pageSize = fs.String("page-size", "letter", "HTML pagination: \"letter\" paginates; empty for one tall page")

		bundledFonts = fs.Bool("bundled-fonts", false, "use only the bundled substitute fonts (hermetic); default uses installed system fonts")
	)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: doctaculous topdf <input.html|.docx|URL> --out file.pdf [flags]\n") //nolint:errcheck // stderr write
		fs.PrintDefaults()
	}
	if err := fs.Parse(reorderArgs(args, topdfValueFlags)); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil // -h/--help printed usage; not an error
		}
		return err
	}
	input, err := resolveInput(*in, fs.Args())
	if err != nil {
		fs.Usage()
		return err
	}
	if *out == "" {
		return fmt.Errorf("--out is required")
	}
	if *workers < 1 {
		return fmt.Errorf("--workers must be at least 1, got %d", *workers)
	}
	if *pageSize != "" && *pageSize != "letter" {
		return fmt.Errorf("unsupported --page-size %q (want \"letter\" or empty)", *pageSize)
	}

	doc, err := openInput(input, doctaculous.FormatUnknown, *pageSize, *bundledFonts, *print, nil)
	if err != nil {
		return fmt.Errorf("open %s: %w", input, err)
	}

	// The margin flag uses -1 as "unset" so 0 can mean "no margin"; PDFOptions treats
	// 0 as the 0.5in default and a negative value as none, so map -1 (unset) to 0.
	marginPt := *margin
	if marginPt < 0 {
		marginPt = 0 // unset -> PDFOptions applies the 0.5in default
	}
	opts := doctaculous.PDFOptions{
		PageWidthPt:  *pageW,
		PageHeightPt: *pageH,
		MarginPt:     marginPt,
		Print:        *print,
		Title:        *title,
		Workers:      *workers,
		BundledFonts: *bundledFonts,
	}

	f, err := os.Create(*out)
	if err != nil {
		return fmt.Errorf("create %s: %w", *out, err)
	}
	defer func() { _ = f.Close() }()
	// Writing through the generic dispatch (rather than WritePDF directly) gives a
	// PDF input the precise same-format error instead of a bare type failure.
	if err := doc.Write(context.Background(), f, doctaculous.FormatPDF, doctaculous.ConvertOptions{PDF: opts}); err != nil {
		return err
	}
	return nil
}

// topdfValueFlags lists the "topdf" flags that take their value as a separate
// token, for reorderArgs.
var topdfValueFlags = map[string]bool{
	"-in": true, "--in": true,
	"-out": true, "--out": true,
	"-page-width": true, "--page-width": true,
	"-page-height": true, "--page-height": true,
	"-margin": true, "--margin": true,
	"-title": true, "--title": true,
	"-workers": true, "--workers": true,
	"-page-size": true, "--page-size": true,
}
