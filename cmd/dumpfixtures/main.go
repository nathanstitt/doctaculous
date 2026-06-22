// Command dumpfixtures materializes the in-memory test PDF fixtures to disk so
// they can be inspected or opened in a viewer.
//
// The project does not commit test PDFs; they are generated deterministically by
// testdata/gen. This is a thin development helper that writes those generated
// bytes out as real .pdf files.
//
// Usage:
//
//	go run ./cmd/dumpfixtures                 # write every fixture to ./fixtures-out
//	go run ./cmd/dumpfixtures -o /tmp/pdfs    # choose the output directory
//	go run ./cmd/dumpfixtures -list           # list fixture names and exit
//	go run ./cmd/dumpfixtures text objstm     # write only the named fixtures
//
// By default only the canonical gen.Core set is dumped. Pass -all to also write
// the extra (non-Core) fixtures, such as the intentionally-malformed ones.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/nathanstitt/doctaculous/testdata/gen"
)

// fixture pairs a stable name with a deterministic byte builder.
type fixture struct {
	name  string
	desc  string
	build func() []byte
}

// extras are useful fixtures that are not part of gen.Core — most notably the
// intentionally-malformed inputs, which are handy to eyeball in a viewer.
var extras = []fixture{
	{"bad-stream-length", "stream /Length disagrees with endstream position", gen.BadStreamLengthPDF},
	{"missing-endobj", "object missing its endobj keyword", gen.MissingEndobjPDF},
	{"no-header", "no %PDF- header line", gen.NoHeaderPDF},
	{"truncated", "file truncated mid-body", gen.TruncatedPDF},
}

func allFixtures(includeExtras bool) []fixture {
	out := make([]fixture, 0, len(gen.Core)+len(extras))
	for _, f := range gen.Core {
		f := f
		out = append(out, fixture{f.Name, f.Desc, f.Build})
	}
	if includeExtras {
		out = append(out, extras...)
	}
	return out
}

func main() {
	outDir := flag.String("o", "fixtures-out", "output directory for the written PDFs")
	all := flag.Bool("all", false, "include non-Core fixtures (e.g. malformed inputs)")
	list := flag.Bool("list", false, "list available fixture names and exit")
	flag.Parse()

	available := allFixtures(*all || *list)

	if *list {
		for _, f := range available {
			fmt.Printf("%-18s %s\n", f.name, f.desc)
		}
		return
	}

	// Select fixtures: positional args name a subset; none means "all available".
	selected := available
	if args := flag.Args(); len(args) > 0 {
		byName := make(map[string]fixture, len(available))
		for _, f := range available {
			byName[f.name] = f
		}
		selected = selected[:0]
		var unknown []string
		for _, name := range args {
			f, ok := byName[name]
			if !ok {
				unknown = append(unknown, name)
				continue
			}
			selected = append(selected, f)
		}
		if len(unknown) > 0 {
			sort.Strings(unknown)
			fmt.Fprintf(os.Stderr, "unknown fixture(s): %v\n", unknown)
			fmt.Fprintln(os.Stderr, "run with -list to see available names (add -all for non-Core fixtures)")
			os.Exit(1)
		}
	}

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "create output dir: %v\n", err)
		os.Exit(1)
	}

	for _, f := range selected {
		path := filepath.Join(*outDir, f.name+".pdf")
		if err := os.WriteFile(path, f.build(), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "write %s: %v\n", path, err)
			os.Exit(1)
		}
		fmt.Printf("wrote %s\n", path)
	}
}
