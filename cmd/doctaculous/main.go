// Command doctaculous is the command-line interface to the doctaculous document
// toolkit. Subcommands: "rasterize" renders document pages to images, and "topdf"
// converts a reflow document (HTML/URL/DOCX) to a PDF with searchable text.
package main

import (
	"fmt"
	"os"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

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

	cmd, rest := args[0], args[1:]
	switch cmd {
	case "rasterize":
		return rasterizeCmd(rest)
	case "topdf":
		return topdfCmd(rest)
	case "version", "-v", "--version":
		fmt.Println("doctaculous", version)
		return nil
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `doctaculous - pure-Go document toolkit

usage:
  doctaculous rasterize <input.pdf|.docx|.html|URL> [flags]
  doctaculous topdf <input.html|.docx|URL> --out file.pdf [flags]
  doctaculous version
  doctaculous help

run "doctaculous rasterize -h" or "doctaculous topdf -h" for subcommand flags.
`)
}
