package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/nathanstitt/doctaculous/pkg/doctaculous"
)

// todocxCmd parses flags for the "todocx" subcommand, which converts a document
// (PDF file, HTML file, Markdown, plain text, or http(s) URL) into a .docx with
// native Word structure — headings, paragraphs, emphasis, links, lists, quotes,
// and code blocks. A PDF's logical structure is recovered by extraction.
func todocxCmd(args []string) error {
	fs := flag.NewFlagSet("todocx", flag.ContinueOnError)
	var (
		in     = fs.String("in", "", "input document (alternative to the positional argument)")
		out    = fs.String("out", "", "output .docx file (required)")
		pageW  = fs.Float64("page-width", 0, "page width in points (default US Letter, 612)")
		pageH  = fs.Float64("page-height", 0, "page height in points (default US Letter, 792)")
		margin = fs.Float64("margin", -1, "page margin in points (default 72 = 1in; 0 for none)")

		bundledFonts = fs.Bool("bundled-fonts", false, "use only the bundled substitute fonts (hermetic); default uses installed system fonts")
	)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: doctaculous todocx <input.pdf|.html|.md|.txt|URL> --out file.docx [flags]\n") //nolint:errcheck // stderr write
		fs.PrintDefaults()
	}
	if err := fs.Parse(reorderArgs(args, todocxValueFlags)); err != nil {
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

	doc, err := openInput(input, doctaculous.FormatUnknown, "", *bundledFonts, false, nil)
	if err != nil {
		return fmt.Errorf("open %s: %w", input, err)
	}

	// The margin flag uses -1 as "unset" so 0 can mean "no margin"; DOCXOptions
	// treats 0 as the 1in default and a negative value as none.
	marginPt := *margin
	if marginPt < 0 {
		marginPt = 0
	}
	opts := doctaculous.ConvertOptions{DOCX: doctaculous.DOCXOptions{
		PageWidthPt:  *pageW,
		PageHeightPt: *pageH,
		MarginPt:     marginPt,
	}}

	f, err := os.Create(*out)
	if err != nil {
		return fmt.Errorf("create %s: %w", *out, err)
	}
	defer func() { _ = f.Close() }()
	return doc.Write(context.Background(), f, doctaculous.FormatDOCX, opts)
}

// todocxValueFlags lists the "todocx" flags that take their value as a separate
// token, for reorderArgs.
var todocxValueFlags = map[string]bool{
	"-in": true, "--in": true,
	"-out": true, "--out": true,
	"-page-width": true, "--page-width": true,
	"-page-height": true, "--page-height": true,
	"-margin": true, "--margin": true,
}
