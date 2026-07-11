package main

import (
	"errors"
	"flag"
	"fmt"
	"runtime"
	"strconv"
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/doctaculous"
)

// rasterizeCmd parses flags for the "rasterize" subcommand. Wiring to the
// rendering library lands in a later step; for now it validates input so the
// CLI surface is exercised end to end.
func rasterizeCmd(args []string) error {
	fs := flag.NewFlagSet("rasterize", flag.ContinueOnError)
	var (
		in       = fs.String("in", "", "input document (alternative to the positional argument)")
		page     = fs.Int("page", 1, "1-based page number to render")
		pages    = fs.String("pages", "", "page range, e.g. 1-3,5 or \"all\" (overrides --page)")
		out      = fs.String("out", "", "output file or pattern (use %d for page number when rendering a range)")
		dpi      = fs.Float64("dpi", 150, "render resolution in DPI")
		format   = fs.String("format", "png", "output image format: png or jpg")
		quality  = fs.Int("quality", 90, "JPEG quality 1-100 (jpg only)")
		workers  = fs.Int("workers", runtime.GOMAXPROCS(0), "max concurrent page renderers")
		pageSize = fs.String("page-size", "letter", "HTML page size: \"letter\" paginates onto US-Letter pages (default), \"tall\" renders one tall page (HTML only)")

		bundledFonts = fs.Bool("bundled-fonts", false, "use only the bundled substitute fonts (hermetic); default uses installed system fonts")
	)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: doctaculous rasterize <input.pdf|.docx|.html|URL> [flags]\n") //nolint:errcheck // stderr write
		fs.PrintDefaults()
	}
	// Go's flag package stops at the first non-flag argument, so reorder the
	// positional input to the end. This lets the input PDF appear before flags
	// (e.g. "rasterize in.pdf --out o.png") as users naturally expect.
	if err := fs.Parse(reorderArgs(args, rasterizeValueFlags)); err != nil {
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
	if *dpi <= 0 {
		return fmt.Errorf("--dpi must be positive, got %v", *dpi)
	}
	if *format != "png" && *format != "jpg" && *format != "jpeg" {
		return fmt.Errorf("unsupported --format %q (want png or jpg)", *format)
	}
	if *workers < 1 {
		return fmt.Errorf("--workers must be at least 1, got %d", *workers)
	}
	if *pageSize != "letter" && *pageSize != "tall" && *pageSize != "" {
		return fmt.Errorf("unsupported --page-size %q (want \"letter\" or \"tall\")", *pageSize)
	}

	doc, err := openInput(input, doctaculous.FormatUnknown, *pageSize, *bundledFonts, false)
	if err != nil {
		return fmt.Errorf("open %s: %w", input, err)
	}

	// Resolve the page selection to zero-based indices.
	indices, err := resolvePages(*pages, *page, doc.PageCount())
	if err != nil {
		return err
	}

	imgFormat := doctaculous.FormatPNG
	if *format == "jpg" || *format == "jpeg" {
		imgFormat = doctaculous.FormatJPEG
	}
	imgOpts := doctaculous.ImageOptions{
		Format:  imgFormat,
		Quality: *quality,
		Raster:  doctaculous.RasterOptions{DPI: *dpi, Workers: *workers, BundledFonts: *bundledFonts},
	}
	return renderPages(doc, indices, *out, imgOpts)
}

// isHTTPURL reports whether input is an http:// or https:// URL (the CLI fetches
// these as web pages via OpenURL). The check is a simple scheme-prefix test — a
// bare path or a file:// URL is not matched (OpenURL rejects non-http(s) schemes
// anyway).
func isHTTPURL(input string) bool {
	return strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://")
}

// htmlOpts builds the HTML layout options shared by the URL and local-HTML paths:
// pageSize "letter" (the CLI default) paginates onto US-Letter pages; "tall" (or an
// empty value) renders a single tall page. printMedia switches the cascade to the
// @media print context (used for PDF output).
func htmlOpts(pageSize string, bundledFonts, printMedia bool) []doctaculous.HTMLOption {
	var opts []doctaculous.HTMLOption
	if pageSize == "letter" {
		opts = append(opts, doctaculous.WithPageSize(doctaculous.LetterWidthPt, doctaculous.LetterHeightPt))
	}
	if bundledFonts {
		opts = append(opts, doctaculous.WithBundledFonts())
	}
	if printMedia {
		opts = append(opts, doctaculous.WithPrintMedia())
	}
	return opts
}

// resolvePages converts the --pages/--page flags into zero-based, in-range page
// indices. --pages (e.g. "1-3,5", or "all" for every page) takes precedence when
// non-empty.
func resolvePages(rangeSpec string, single, count int) ([]int, error) {
	if count <= 0 {
		return nil, fmt.Errorf("document has no pages")
	}
	if strings.EqualFold(strings.TrimSpace(rangeSpec), "all") {
		indices := make([]int, count)
		for i := range indices {
			indices[i] = i
		}
		return indices, nil
	}
	if rangeSpec == "" {
		if single < 1 || single > count {
			return nil, fmt.Errorf("--page %d out of range [1,%d]", single, count)
		}
		return []int{single - 1}, nil
	}
	var indices []int
	seen := make(map[int]bool)
	for part := range strings.SplitSeq(rangeSpec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		lo, hi, err := parseRangePart(part, count)
		if err != nil {
			return nil, err
		}
		// Dedupe while preserving first-seen order so overlapping ranges (e.g.
		// "1-3,2-4") don't render or overwrite the same page twice.
		for p := lo; p <= hi; p++ {
			if !seen[p] {
				seen[p] = true
				indices = append(indices, p-1)
			}
		}
	}
	if len(indices) == 0 {
		return nil, fmt.Errorf("--pages %q selected no pages", rangeSpec)
	}
	return indices, nil
}

// parseRangePart parses one "N" or "N-M" token, validating against count.
func parseRangePart(part string, count int) (lo, hi int, err error) {
	if dash := strings.IndexByte(part, '-'); dash >= 0 {
		lo, err = strconv.Atoi(strings.TrimSpace(part[:dash]))
		if err != nil {
			return 0, 0, fmt.Errorf("bad page range %q: %w", part, err)
		}
		hi, err = strconv.Atoi(strings.TrimSpace(part[dash+1:]))
		if err != nil {
			return 0, 0, fmt.Errorf("bad page range %q: %w", part, err)
		}
	} else {
		lo, err = strconv.Atoi(part)
		if err != nil {
			return 0, 0, fmt.Errorf("bad page number %q: %w", part, err)
		}
		hi = lo
	}
	if lo > hi {
		lo, hi = hi, lo
	}
	if lo < 1 || hi > count {
		return 0, 0, fmt.Errorf("page range %q out of bounds [1,%d]", part, count)
	}
	return lo, hi, nil
}

// outputPath builds the output filename for a page. A "%d" in the pattern is
// replaced with the 1-based page number whenever present, so a single-page render
// to a "page-%d.png" pattern still yields "page-1.png" rather than a literal "%d"
// in the name. (Whether %d is *required* — for a multi-page render — is enforced by
// the caller, not here.)
func outputPath(pattern string, index int) string {
	if strings.Contains(pattern, "%d") {
		return fmt.Sprintf(pattern, index+1)
	}
	return pattern
}

// rasterizeValueFlags lists the "rasterize" flags that take their value as a
// separate token, for reorderArgs.
var rasterizeValueFlags = map[string]bool{
	"-in": true, "--in": true,
	"-page": true, "--page": true,
	"-pages": true, "--pages": true,
	"-out": true, "--out": true,
	"-dpi": true, "--dpi": true,
	"-format": true, "--format": true,
	"-quality": true, "--quality": true,
	"-workers": true, "--workers": true,
	"-page-size": true, "--page-size": true,
}
