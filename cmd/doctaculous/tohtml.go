package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/nathanstitt/doctaculous/pkg/doctaculous"
)

// tohtmlCmd parses flags for the "tohtml" subcommand, which converts a document (PDF
// file, HTML file, http(s) URL, or DOCX) into HTML. A PDF's logical structure
// (paragraphs, headings, lists, tables) is recovered by extraction; HTML/DOCX serialize
// their box tree directly.
func tohtmlCmd(args []string) error {
	fs := flag.NewFlagSet("tohtml", flag.ContinueOnError)
	var (
		in       = fs.String("in", "", "input document (alternative to the positional argument)")
		out      = fs.String("out", "", "output file (default: stdout)")
		fragment = fs.Bool("fragment", false, "emit only body markup (no <html>/<head> wrapper)")
	)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: doctaculous tohtml <input.pdf|.html|.docx|URL> [--out file.html] [--fragment]\n") //nolint:errcheck // stderr write
		fs.PrintDefaults()
	}
	if err := fs.Parse(reorderTomdArgs(args)); err != nil {
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

	doc, err := openConvertibleDocument(input)
	if err != nil {
		return fmt.Errorf("open %s: %w", input, err)
	}

	w := os.Stdout
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			return fmt.Errorf("create %s: %w", *out, err)
		}
		defer func() { _ = f.Close() }()
		w = f
	}
	opts := doctaculous.HTMLWriteOptions{Fragment: *fragment}
	if err := doc.WriteHTML(context.Background(), w, opts); err != nil {
		return err
	}
	return nil
}
