// Command doctaculous is the command-line interface to the doctaculous document
// toolkit. The primary verb is "convert", which converts any supported input
// (PDF, DOCX, HTML file, or http(s) URL) to any supported output (pdf, md, txt,
// html, png, jpg), detecting formats from content and extensions. The focused
// subcommands remain: "rasterize" renders document pages to images, "topdf"
// converts a reflow document to a PDF with searchable text, "tomd" converts one
// to Markdown or plain text, and "tohtml" to HTML.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
		fmt.Fprintln(os.Stderr, "doctaculous:", err)
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
	case "topdf":
		return topdfCmd(args[1:])
	case "tomd":
		return tomdCmd(args[1:])
	case "tohtml":
		return tohtmlCmd(args[1:])
	case "todocx":
		return todocxCmd(args[1:])
	case "version", "-v", "--version":
		// A goreleaser build fills commit/date; a plain `go build` leaves the
		// defaults, so only decorate when they were injected.
		if commit == "none" {
			fmt.Println("doctaculous", version)
		} else {
			fmt.Printf("doctaculous %s (%s, %s)\n", version, commit, date)
		}
		return nil
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		// No explicit subcommand: infer the command from the --in/--out file extensions
		// (a .pdf output => topdf; a .md/.txt output => tomd; an image output =>
		// rasterize).
		cmd, err := inferCommand(args)
		if err != nil {
			usage()
			return err
		}
		switch cmd {
		case "topdf":
			return topdfCmd(args)
		case "tomd":
			return tomdCmd(args)
		case "tohtml":
			return tohtmlCmd(args)
		case "todocx":
			return todocxCmd(args)
		case "convert":
			return convertCmd(args)
		}
		return rasterizeCmd(args)
	}
}

// inferCommand picks a subcommand from the flags when none is named. It prefers the
// output extension (--out): a .pdf output means "topdf", an image output (.png/.jpg/
// .jpeg) means "rasterize". Failing that it falls back to the input: a .pdf input can
// only be rasterized, while an .html/.htm/.docx input or an http(s) URL means topdf.
// It errors (rather than guessing) when neither extension is conclusive.
func inferCommand(args []string) (string, error) {
	in := flagValue(args, "in")
	out := flagValue(args, "out")
	switch strings.ToLower(filepath.Ext(out)) {
	case ".pdf":
		return "topdf", nil
	case ".md", ".markdown", ".txt":
		return "tomd", nil
	case ".html", ".htm":
		return "tohtml", nil
	case ".docx":
		return "todocx", nil
	case ".csv", ".tsv", ".xlsx":
		return "convert", nil
	case ".png", ".jpg", ".jpeg":
		return "rasterize", nil
	}
	if isHTTPURL(in) {
		return "topdf", nil
	}
	switch strings.ToLower(filepath.Ext(in)) {
	case ".html", ".htm", ".docx", ".md", ".markdown", ".txt", ".text", ".csv", ".tsv", ".xlsx":
		return "topdf", nil
	case ".pdf":
		return "rasterize", nil
	}
	return "", fmt.Errorf("cannot infer command; use \"doctaculous convert <in> <out>\", name a subcommand, or use recognizable --in/--out extensions")
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

// flagValue returns the value of --name/-name from args, supporting both the
// "--name value" and "--name=value" forms. It returns "" when absent.
func flagValue(args []string, name string) string {
	long, short := "--"+name, "-"+name
	for i, a := range args {
		switch {
		case a == long || a == short:
			if i+1 < len(args) {
				return args[i+1]
			}
		case strings.HasPrefix(a, long+"="):
			return strings.TrimPrefix(a, long+"=")
		case strings.HasPrefix(a, short+"="):
			return strings.TrimPrefix(a, short+"=")
		}
	}
	return ""
}

func usage() {
	fmt.Fprint(os.Stderr, `doctaculous - pure-Go document toolkit

usage:
  doctaculous convert   <input> <output> [flags]   (any format to any other)
  doctaculous topdf     --in <file.html|.docx|URL> --out file.pdf [flags]
  doctaculous todocx    --in <file.pdf|.html|.md|URL> --out file.docx [flags]
  doctaculous tomd      --in <file.pdf|.html|.docx|URL> [--out file.md] [--plain]
  doctaculous tohtml    --in <file.pdf|.html|.docx|URL> [--out file.html] [--fragment]
  doctaculous rasterize  --in <file.pdf|.docx|.html|URL> --out file.png [flags]
  doctaculous --in <input> --out <output>   (subcommand inferred from extensions)
  doctaculous version
  doctaculous help

"convert" detects the input format from content and extension (--from overrides)
and takes the output format from the output extension (--to overrides). Inputs:
pdf, docx, xlsx, pptx, epub, rtf, html, md, txt, csv, tsv, png, jpg, http(s) URLs. Outputs: pdf, docx,
xlsx, pptx, epub, rtf, md, txt, html, csv, tsv, png, jpg. CSV/TSV/XLSX output carries the
document's tables (prose is dropped). Converting a document to its own format
is not supported.

The input may be given via --in or as a positional argument. When no subcommand is
named, it is inferred from the --out extension (.pdf => topdf; .md/.txt => tomd;
.png/.jpg => rasterize).

run "doctaculous convert -h" (or topdf/rasterize/... -h) for subcommand flags.
`)
}
