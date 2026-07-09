package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/doctaculous"
)

// tomdCmd parses flags for the "tomd" subcommand, which converts a document (PDF file,
// HTML file, http(s) URL, or DOCX) into GitHub-Flavored Markdown, or plain text with
// --plain. A PDF's logical structure (paragraphs, headings, lists, tables) is recovered
// by extraction; HTML/DOCX read the reflow engine's semantic box tree directly.
func tomdCmd(args []string) error {
	fs := flag.NewFlagSet("tomd", flag.ContinueOnError)
	var (
		in    = fs.String("in", "", "input document (alternative to the positional argument)")
		out   = fs.String("out", "", "output file (default: stdout)")
		plain = fs.Bool("plain", false, "emit plain text instead of Markdown")

		bundledFonts = fs.Bool("bundled-fonts", false, "use only the bundled substitute fonts (hermetic); default uses installed system fonts")
	)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: doctaculous tomd <input.html|.docx|URL> [--out file.md] [--plain]\n") //nolint:errcheck // stderr write
		fs.PrintDefaults()
	}
	if err := fs.Parse(reorderArgs(args, tomdValueFlags)); err != nil {
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

	doc, err := openConvertibleDocument(input, *bundledFonts)
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
	opts := doctaculous.MarkdownOptions{Plain: *plain}
	if err := doc.WriteMarkdown(context.Background(), w, opts); err != nil {
		return err
	}
	return nil
}

// openConvertibleDocument opens the input for a text/markup conversion (tomd/tohtml):
// an http(s) URL and .html/.htm go through the HTML pipeline, .docx through the DOCX
// pipeline, and .pdf through the PDF pipeline (its logical structure is recovered by
// extraction on the first Write call). Unlike openReflowDocument (used by topdf, where a
// PDF input is meaningless), a .pdf here is a first-class input.
func openConvertibleDocument(input string, bundledFonts bool) (*doctaculous.Document, error) {
	if isHTTPURL(input) {
		return doctaculous.OpenURL(input, htmlOpts("", bundledFonts)...)
	}
	switch strings.ToLower(filepath.Ext(input)) {
	case ".pdf":
		return doctaculous.Open(input)
	case ".docx":
		return doctaculous.OpenDOCX(input)
	case ".html", ".htm":
		return doctaculous.OpenHTMLFile(input, htmlOpts("", bundledFonts)...)
	default:
		return nil, fmt.Errorf("input must be .pdf, .html, .docx, or an http(s) URL (got %q)", input)
	}
}

// tomdValueFlags lists the "tomd" flags that take their value as a separate
// token, for reorderArgs. tohtml shares this map (its flags are the same shape:
// --in/--out plus boolean flags).
var tomdValueFlags = map[string]bool{
	"-in": true, "--in": true,
	"-out": true, "--out": true,
}
