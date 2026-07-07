package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/nathanstitt/doctaculous/pkg/doctaculous"
)

// tomdCmd parses flags for the "tomd" subcommand, which converts a reflow document
// (HTML file, http(s) URL, or DOCX) into GitHub-Flavored Markdown, or plain text with
// --plain. PDF inputs are rejected — conversion reads the reflow engine's semantic box
// tree, which a parsed PDF does not provide.
func tomdCmd(args []string) error {
	fs := flag.NewFlagSet("tomd", flag.ContinueOnError)
	var (
		in    = fs.String("in", "", "input document (alternative to the positional argument)")
		out   = fs.String("out", "", "output file (default: stdout)")
		plain = fs.Bool("plain", false, "emit plain text instead of Markdown")
	)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: doctaculous tomd <input.html|.docx|URL> [--out file.md] [--plain]\n") //nolint:errcheck // stderr write
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

	// Markdown always lays the document out unpaginated (one tall page) — pagination
	// does not affect the semantic tree the conversion walks — so pass an empty page
	// size to openReflowDocument.
	doc, err := openReflowDocument(input, "")
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

// reorderTomdArgs moves non-flag arguments after flags so the input may appear before
// flags ("tomd in.html --out o.md"), matching reorderTopdfArgs.
func reorderTomdArgs(args []string) []string {
	valueFlags := map[string]bool{
		"-in": true, "--in": true,
		"-out": true, "--out": true,
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
