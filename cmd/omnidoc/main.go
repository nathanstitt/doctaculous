// Command omnidoc is the command-line interface to the omnidoc document
// toolkit. "convert" converts any supported input (a document file or an
// http(s) URL) to any supported output, detecting formats from content and
// extensions; "rasterize" renders document pages to images.
package main

import (
	"fmt"
	"os"
)

// version, commit, and date are overridden at build time via
// -ldflags "-X main.version=... -X main.commit=... -X main.date=..."
// (see .goreleaser.yaml). They default to a dev build.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "omnidoc:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return fmt.Errorf("no command given")
	}

	switch args[0] {
	case "convert":
		return convertCmd(args[1:])
	case "rasterize":
		return rasterizeCmd(args[1:])
	case "version", "-v", "--version":
		// A goreleaser build fills commit/date; a plain `go build` leaves the
		// defaults, so only decorate when they were injected.
		if commit == "none" {
			fmt.Println("omnidoc", version)
		} else {
			fmt.Printf("omnidoc %s (%s, %s)\n", version, commit, date)
		}
		return nil
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown command %q (want convert, rasterize, version, or help)", args[0])
	}
}

// resolveInput returns the single input document from the --in flag or a positional
// argument. Exactly one source must be given: it errors if both are set (ambiguous)
// or neither, or if more than one positional argument is present.
func resolveInput(inFlag string, positional []string) (string, error) {
	switch {
	case inFlag != "" && len(positional) > 0:
		return "", fmt.Errorf("give the input via either --in or a positional argument, not both")
	case inFlag != "":
		return inFlag, nil
	case len(positional) == 1:
		return positional[0], nil
	case len(positional) == 0:
		return "", fmt.Errorf("no input document given (use --in <file> or a positional argument)")
	default:
		return "", fmt.Errorf("expected exactly one input document, got %d", len(positional))
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `omnidoc - pure-Go document toolkit

usage:
  omnidoc convert   <input> <output> [flags]   (any format to any other)
  omnidoc rasterize <input> --out file.png [flags]
  omnidoc version
  omnidoc help

"convert" detects the input format from content and extension (--from overrides)
and takes the output format from the output extension (--to overrides).
Inputs:  pdf, docx, xlsx, pptx, epub, rtf, html, md, txt, csv, tsv, png, jpg,
         webp, heic, svg, and http(s) URLs.
Outputs: pdf, docx, xlsx, pptx, epub, rtf, html, md, txt, csv, tsv, png, jpg,
         webp (lossless), svg.
CSV/TSV/XLSX output carries the document's tables (prose is dropped). SVG output
is vector (paths, clips, gradients; text as glyph outlines) and holds one page
per file, so multi-page input needs a numbered output name, as image output
does. Converting a document to its own format is not supported.

"rasterize" renders one page, or a --pages range, of any input to PNG, JPEG, or
WebP. The input may be given via --in or as a positional argument.

run "omnidoc convert -h" or "omnidoc rasterize -h" for the flags.
`)
}
