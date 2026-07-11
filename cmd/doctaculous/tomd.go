package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

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

	doc, err := openInput(input, doctaculous.FormatUnknown, "", *bundledFonts, false)
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

// tomdValueFlags lists the "tomd" flags that take their value as a separate
// token, for reorderArgs. tohtml shares this map (its flags are the same shape:
// --in/--out plus boolean flags).
var tomdValueFlags = map[string]bool{
	"-in": true, "--in": true,
	"-out": true, "--out": true,
}
