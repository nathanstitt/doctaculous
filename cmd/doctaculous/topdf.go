package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/doctaculous"
)

// topdfCmd parses flags for the "topdf" subcommand, which converts a reflow
// document (HTML file, http(s) URL, or DOCX) into a PDF with real, searchable text.
// PDF inputs are rejected — the writer emits from the reflow engine, not a re-serialized
// PDF.
func topdfCmd(args []string) error {
	fs := flag.NewFlagSet("topdf", flag.ContinueOnError)
	var (
		out      = fs.String("out", "", "output PDF file (required)")
		pageW    = fs.Float64("page-width", 0, "page width in points (default US Letter, 612)")
		pageH    = fs.Float64("page-height", 0, "page height in points (default US Letter, 792)")
		margin   = fs.Float64("margin", -1, "content margin in points (default 36 = 0.5in; 0 for none)")
		print    = fs.Bool("print", false, "honor @media print rules (exclude screen-only rules)")
		title    = fs.String("title", "", "PDF /Title metadata")
		workers  = fs.Int("workers", runtime.GOMAXPROCS(0), "max concurrent page renderers")
		pageSize = fs.String("page-size", "letter", "HTML pagination: \"letter\" paginates; empty for one tall page")
	)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: doctaculous topdf <input.html|.docx|URL> --out file.pdf [flags]\n") //nolint:errcheck // stderr write
		fs.PrintDefaults()
	}
	if err := fs.Parse(reorderTopdfArgs(args)); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil // -h/--help printed usage; not an error
		}
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return fmt.Errorf("expected exactly one input document, got %d", fs.NArg())
	}
	input := fs.Arg(0)
	if *out == "" {
		return fmt.Errorf("--out is required")
	}
	if *workers < 1 {
		return fmt.Errorf("--workers must be at least 1, got %d", *workers)
	}
	if *pageSize != "" && *pageSize != "letter" {
		return fmt.Errorf("unsupported --page-size %q (want \"letter\" or empty)", *pageSize)
	}

	doc, err := openReflowDocument(input, *pageSize)
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
	}

	f, err := os.Create(*out)
	if err != nil {
		return fmt.Errorf("create %s: %w", *out, err)
	}
	defer func() { _ = f.Close() }()
	if err := doc.WritePDF(context.Background(), f, opts); err != nil {
		return err
	}
	return nil
}

// openReflowDocument opens the input as a reflow document (the only kind WritePDF
// accepts): an http(s) URL and .html/.htm files go through the HTML pipeline, .docx
// through the DOCX pipeline. A .pdf (or any other extension) is rejected — the PDF
// writer emits from the reflow engine, not from a parsed PDF.
func openReflowDocument(input, pageSize string) (*doctaculous.Document, error) {
	if isHTTPURL(input) {
		return doctaculous.OpenURL(input, htmlOpts(pageSize)...)
	}
	switch strings.ToLower(filepath.Ext(input)) {
	case ".docx":
		return doctaculous.OpenDOCX(input)
	case ".html", ".htm":
		return doctaculous.OpenHTMLFile(input, htmlOpts(pageSize)...)
	default:
		return nil, fmt.Errorf("topdf input must be .html, .docx, or an http(s) URL (got %q)", input)
	}
}

// reorderTopdfArgs moves non-flag arguments after flags so Go's flag package (which
// stops at the first non-flag token) sees every flag, letting the input appear
// before flags ("topdf in.html --out o.pdf").
func reorderTopdfArgs(args []string) []string {
	valueFlags := map[string]bool{
		"-out": true, "--out": true,
		"-page-width": true, "--page-width": true,
		"-page-height": true, "--page-height": true,
		"-margin": true, "--margin": true,
		"-title": true, "--title": true,
		"-workers": true, "--workers": true,
		"-page-size": true, "--page-size": true,
	}
	var flags, positional []string
	for i := 0; i < len(args); i++ { //nolint:intrange // index i is mutated inside the loop
		a := args[i]
		if len(a) > 0 && a[0] == '-' {
			flags = append(flags, a)
			if valueFlags[a] && i+1 < len(args) {
				flags = append(flags, args[i+1])
				i++
			}
			continue
		}
		positional = append(positional, a)
	}
	return append(flags, positional...)
}
