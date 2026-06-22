package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"os"
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
		page    = fs.Int("page", 1, "1-based page number to render")
		pages   = fs.String("pages", "", "page range, e.g. 1-3,5 (overrides --page)")
		out     = fs.String("out", "", "output file or pattern (use %d for page number when rendering a range)")
		dpi     = fs.Float64("dpi", 150, "render resolution in DPI")
		format  = fs.String("format", "png", "output image format: png or jpg")
		workers = fs.Int("workers", runtime.GOMAXPROCS(0), "max concurrent page renderers")
	)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: doctaculous rasterize <input.pdf> [flags]\n") //nolint:errcheck // stderr write
		fs.PrintDefaults()
	}
	// Go's flag package stops at the first non-flag argument, so reorder the
	// positional input to the end. This lets the input PDF appear before flags
	// (e.g. "rasterize in.pdf --out o.png") as users naturally expect.
	if err := fs.Parse(reorderArgs(args)); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil // -h/--help printed usage; not an error
		}
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return fmt.Errorf("expected exactly one input PDF, got %d", fs.NArg())
	}
	input := fs.Arg(0)
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

	doc, err := doctaculous.Open(input)
	if err != nil {
		return fmt.Errorf("open %s: %w", input, err)
	}

	// Resolve the page selection to zero-based indices.
	indices, err := resolvePages(*pages, *page, doc.PageCount())
	if err != nil {
		return err
	}

	opts := doctaculous.RasterOptions{DPI: *dpi, Workers: *workers}
	results := doc.RasterizePages(context.Background(), indices, opts)

	multi := len(indices) > 1
	if multi && !strings.Contains(*out, "%d") {
		return fmt.Errorf("rendering %d pages requires a %%d placeholder in --out (e.g. page-%%d.%s)", len(indices), *format)
	}

	var firstErr error
	written := 0
	for _, r := range results {
		if r.Err != nil {
			fmt.Fprintf(os.Stderr, "doctaculous: page %d: %v\n", r.Index+1, r.Err) //nolint:errcheck // stderr
			if firstErr == nil {
				firstErr = r.Err
			}
			continue
		}
		path := outputPath(*out, r.Index, multi)
		if err := writeImage(path, r.Image, *format); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			fmt.Fprintf(os.Stderr, "doctaculous: writing %s: %v\n", path, err) //nolint:errcheck // stderr
			continue
		}
		written++
	}
	if firstErr != nil {
		// Successful pages are still written, but the command must report failure
		// so scripts and CI detect the partial batch.
		return fmt.Errorf("%d of %d pages failed; first error: %w", len(indices)-written, len(indices), firstErr)
	}
	return nil
}

// resolvePages converts the --pages/--page flags into zero-based, in-range page
// indices. --pages (e.g. "1-3,5") takes precedence when non-empty.
func resolvePages(rangeSpec string, single, count int) ([]int, error) {
	if count <= 0 {
		return nil, fmt.Errorf("document has no pages")
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

// outputPath builds the output filename for a page. When rendering multiple
// pages, a "%d" in the pattern is replaced with the 1-based page number.
func outputPath(pattern string, index int, multi bool) string {
	if multi && strings.Contains(pattern, "%d") {
		return fmt.Sprintf(pattern, index+1)
	}
	return pattern
}

// writeImage encodes img to path in the given format (png or jpg).
func writeImage(path string, img image.Image, format string) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		// Surface a close error only if encoding itself succeeded (a flush of
		// buffered bytes can fail on a full disk).
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	switch format {
	case "jpg", "jpeg":
		err = jpeg.Encode(f, img, &jpeg.Options{Quality: 90})
	default:
		err = png.Encode(f, img)
	}
	return err
}

// reorderArgs moves non-flag arguments after flag arguments so the flag package
// (which stops at the first non-flag token) sees all flags. Flags that take a
// value as a separate token (e.g. "--out file") keep that value adjacent.
//
// It assumes flags use a single value token at most via the "--flag value" form;
// the "--flag=value" form is always safe. Among our flags only boolean-free
// value flags exist, so a token following a known value flag is treated as its
// value.
func reorderArgs(args []string) []string {
	valueFlags := map[string]bool{
		"-page": true, "--page": true,
		"-pages": true, "--pages": true,
		"-out": true, "--out": true,
		"-dpi": true, "--dpi": true,
		"-format": true, "--format": true,
		"-workers": true, "--workers": true,
	}
	var flags, positional []string
	for i := 0; i < len(args); i++ { //nolint:intrange // index i is mutated inside the loop
		a := args[i]
		if len(a) > 0 && a[0] == '-' {
			flags = append(flags, a)
			// If this is a value flag in "--flag value" form, pull the next token too.
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
