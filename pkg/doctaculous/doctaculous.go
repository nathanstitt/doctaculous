package doctaculous

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"math"
	"runtime"
	"sync"

	"github.com/nathanstitt/doctaculous/pkg/font"
)

// Document is an opened document ready for rendering. It is read-only after Open
// and safe for concurrent use across goroutines. It wraps a format-specific
// renderer behind a common API, so callers rasterize and convert any supported
// format the same way.
type Document struct {
	r renderer
	// format is the source format the document was opened from, stamped by every
	// opener. The generic Write path consults it to reject same-format conversion.
	format Format
}

// Format reports the format the document was opened from (e.g. FormatPDF for
// Open on a PDF file, FormatHTML for OpenURL).
func (d *Document) Format() Format { return d.format }

// renderer is the format-agnostic backend a Document delegates to. Each supported
// format provides one; the public Document and its worker pool are format-neutral.
type renderer interface {
	// pageCount reports the number of pages.
	pageCount() int
	// renderPage rasterizes one zero-based page to an image.
	renderPage(ctx context.Context, index int, opts RasterOptions) (image.Image, error)
	// pageSize reports the rendered size of page index in points — post-/Rotate
	// for PDF — i.e. the geometry a raster at DPI d maps to
	// ceil(w·d/72) × ceil(h·d/72) pixels.
	pageSize(index int) (wPt, hPt float64, err error)
}

// PageCount returns the number of pages.
func (d *Document) PageCount() int { return d.r.pageCount() }

// PageSize returns page index's size in points (1/72 inch — document space).
// For PDF pages the size reflects /Rotate — a 90°-rotated portrait page
// reports landscape — so it is always the aspect ratio the rasterized page
// image has. Reflow documents report their laid-out page size.
func (d *Document) PageSize(index int) (widthPt, heightPt float64, err error) {
	w, h, err := d.r.pageSize(index)
	if err != nil {
		return 0, 0, fmt.Errorf("doctaculous: page size %d: %w", index, err)
	}
	return w, h, nil
}

// RasterOptions controls rasterization.
type RasterOptions struct {
	// DPI is the output resolution (document space is 72 units/inch). Defaults to
	// 150 when zero or negative.
	DPI float64
	// Background fills the page before drawing. Defaults to opaque white. Set to a
	// transparent color (e.g. color.Transparent) for an alpha page.
	Background color.Color
	// Workers caps the goroutines used by RasterizePages. Defaults to GOMAXPROCS.
	Workers int
	// Logf, if set, receives debug messages about unsupported features. It must be
	// safe for concurrent use; RasterizePages calls it from multiple goroutines.
	Logf func(string, ...any)
	// FontProvider, if set, resolves a non-embedded font (a standard-14 /BaseFont or
	// an unknown family) to real font bytes before the bundled substitute is tried —
	// letting a caller supply system fonts, exact-metric faces, or a face for a family
	// the bundle has no look-alike for (Symbol, ZapfDingbats). nil (the default) uses
	// the bundled weighted substitutes only, keeping rendering hermetic. A
	// layoutfont.DiskFontProvider satisfies this.
	FontProvider font.Provider
	// BundledFonts selects hermetic bundled-font mode: non-embedded fonts resolve only
	// from the bundled substitutes (pkg/font/standard), never the host's installed
	// fonts. Default false = system mode, which installs an OSFontProvider so real
	// installed fonts are used. Ignored when FontProvider is set explicitly (that
	// provider always wins). The golden/reference tests set this true for reproducibility.
	BundledFonts bool
	// MaxWidthPx / MaxHeightPx, when either is > 0, switch sizing from "render
	// at DPI" to "render to fit within this pixel box, aspect preserved": each
	// page is rendered at exactly the resolution that makes it fit, so a
	// thumbnail is a direct sharp render, never a downscaled bitmap. With only
	// one side set, only that axis constrains. DPI then becomes an optional
	// resolution CEILING: zero (the default) means the page always fills the
	// box — a small page scales up, which for a vector re-render costs no
	// quality — while a positive DPI caps the render so pages already smaller
	// than the box at that DPI are not upscaled (classic downscale-only
	// thumbnail behavior, e.g. {MaxWidthPx: 480, MaxHeightPx: 360, DPI: 300}).
	// With both fields zero, DPI keeps its existing meaning (default 150).
	MaxWidthPx  int
	MaxHeightPx int
}

func (o RasterOptions) dpi() float64 {
	if o.DPI <= 0 {
		return 150
	}
	return o.DPI
}

// fitRaster resolves MaxWidthPx/MaxHeightPx against page index's geometry into
// a concrete DPI, returning opts with DPI substituted and the Max fields
// cleared — so the backends only ever see a DPI. With neither Max field set it
// returns opts untouched (the exact pre-existing DPI path).
func (d *Document) fitRaster(index int, opts RasterOptions) (RasterOptions, error) {
	if opts.MaxWidthPx <= 0 && opts.MaxHeightPx <= 0 {
		return opts, nil
	}
	wPt, hPt, err := d.r.pageSize(index)
	if err != nil {
		return opts, err
	}
	// Pixels per point: the tightest constrained axis wins.
	scale := math.Inf(1)
	if opts.MaxWidthPx > 0 {
		scale = float64(opts.MaxWidthPx) / wPt
	}
	if opts.MaxHeightPx > 0 {
		if s := float64(opts.MaxHeightPx) / hPt; s < scale {
			scale = s
		}
	}
	// A positive DPI is a resolution ceiling: a page smaller than the box at
	// that DPI is not upscaled.
	if opts.DPI > 0 && opts.DPI/72 < scale {
		scale = opts.DPI / 72
	}
	// Ceil-safety: the backends size images as ceil(pt·scale), and float
	// artifacts can push an exact fit one pixel past the box (612 × (480/612)
	// can round up). Step the scale down until the ceiling fits.
	for range 4 {
		overW := opts.MaxWidthPx > 0 && math.Ceil(wPt*scale) > float64(opts.MaxWidthPx)
		overH := opts.MaxHeightPx > 0 && math.Ceil(hPt*scale) > float64(opts.MaxHeightPx)
		if !overW && !overH {
			break
		}
		scale = math.Nextafter(scale, 0)
	}
	opts.DPI = scale * 72
	opts.MaxWidthPx, opts.MaxHeightPx = 0, 0
	return opts, nil
}

// RasterizePage renders a single page (zero-based index) to an image.
func (d *Document) RasterizePage(ctx context.Context, index int, opts RasterOptions) (image.Image, error) {
	opts, err := d.fitRaster(index, opts)
	if err != nil {
		return nil, fmt.Errorf("doctaculous: rasterize page %d: %w", index, err)
	}
	img, err := d.r.renderPage(ctx, index, opts)
	if err != nil {
		return nil, fmt.Errorf("doctaculous: rasterize page %d: %w", index, err)
	}
	return img, nil
}

// PageResult is the outcome of rasterizing one page in a batch.
type PageResult struct {
	Index int
	Image image.Image
	Err   error
}

// RasterizePages renders the given page indices concurrently using a bounded
// worker pool. Results are returned in the same order as indices; a per-page
// error is reported in that page's PageResult without failing the whole batch.
// The context cancels all outstanding work.
func (d *Document) RasterizePages(ctx context.Context, indices []int, opts RasterOptions) []PageResult {
	results := make([]PageResult, len(indices))
	for i, idx := range indices {
		results[i].Index = idx
	}

	workers := opts.Workers
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	if workers > len(indices) {
		workers = len(indices)
	}
	if workers <= 0 {
		return results
	}

	jobs := make(chan int) // sends positions into the indices/results slices
	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for pos := range jobs {
				if err := ctx.Err(); err != nil {
					results[pos].Err = err
					continue
				}
				// Fit sizing resolves per page — pages in one document can differ
				// in size, and each must fit the box independently.
				pageOpts, err := d.fitRaster(indices[pos], opts)
				if err != nil {
					results[pos].Err = fmt.Errorf("doctaculous: rasterize page %d: %w", indices[pos], err)
					continue
				}
				img, err := d.r.renderPage(ctx, indices[pos], pageOpts)
				if err != nil {
					results[pos].Err = fmt.Errorf("doctaculous: rasterize page %d: %w", indices[pos], err)
					continue
				}
				results[pos].Image = img
			}
		}()
	}

	// fed counts positions successfully handed to a worker. Positions [fed:] were
	// never dispatched (the context cancelled the feed loop) and are marked below.
	// Tracking dispatch explicitly avoids inferring "unrendered" from a nil image,
	// which is unreliable because a typed-nil *image.RGBA is a non-nil interface.
	fed := 0
	for pos := range indices {
		select {
		case <-ctx.Done():
		case jobs <- pos:
			fed++
			continue
		}
		break
	}
	close(jobs)
	wg.Wait()

	// Mark any positions that were never dispatched due to cancellation.
	if fed < len(indices) {
		err := ctx.Err()
		if err == nil {
			err = context.Canceled
		}
		for i := fed; i < len(indices); i++ {
			if results[i].Err == nil {
				results[i].Err = err
			}
		}
	}
	return results
}

// AllPages is a convenience that returns every page index in order.
func (d *Document) AllPages() []int {
	idx := make([]int, d.PageCount())
	for i := range idx {
		idx[i] = i
	}
	return idx
}
