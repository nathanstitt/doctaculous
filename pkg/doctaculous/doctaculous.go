package doctaculous

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"runtime"
	"sync"
)

// Document is an opened document ready for rendering. It is read-only after Open
// and safe for concurrent use across goroutines. It wraps a format-specific
// renderer (PDF today; DOCX and other reflowable formats via OpenDOCX) behind a
// common API, so callers rasterize any supported format the same way.
type Document struct {
	r renderer
}

// renderer is the format-agnostic backend a Document delegates to. Each supported
// format provides one; the public Document and its worker pool are format-neutral.
type renderer interface {
	// pageCount reports the number of pages.
	pageCount() int
	// renderPage rasterizes one zero-based page to an image.
	renderPage(ctx context.Context, index int, opts RasterOptions) (image.Image, error)
}

// PageCount returns the number of pages.
func (d *Document) PageCount() int { return d.r.pageCount() }

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
}

func (o RasterOptions) dpi() float64 {
	if o.DPI <= 0 {
		return 150
	}
	return o.DPI
}

// RasterizePage renders a single page (zero-based index) to an image.
func (d *Document) RasterizePage(ctx context.Context, index int, opts RasterOptions) (image.Image, error) {
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
				img, err := d.r.renderPage(ctx, indices[pos], opts)
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
