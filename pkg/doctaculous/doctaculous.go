package doctaculous

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"runtime"
	"sync"

	"github.com/nathanstitt/doctaculous/pkg/pdf"
	"github.com/nathanstitt/doctaculous/pkg/render/raster"
)

// Document is an opened document ready for rendering. It is read-only after Open
// and safe for concurrent use across goroutines.
type Document struct {
	pdf *pdf.Document
}

// Open reads and parses a document from a file path.
func Open(path string) (*Document, error) {
	d, err := pdf.Open(path)
	if err != nil {
		return nil, err
	}
	return &Document{pdf: d}, nil
}

// OpenBytes parses a document from an in-memory byte slice. The slice is retained
// and must not be modified by the caller.
func OpenBytes(data []byte) (*Document, error) {
	d, err := pdf.Parse(data)
	if err != nil {
		return nil, err
	}
	return &Document{pdf: d}, nil
}

// PageCount returns the number of pages.
func (d *Document) PageCount() int { return d.pdf.PageCount() }

// RasterOptions controls rasterization.
type RasterOptions struct {
	// DPI is the output resolution (PDF user space is 72 units/inch). Defaults to
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

func (o RasterOptions) rasterOpts() raster.Options {
	return raster.Options{DPI: o.dpi(), Background: o.Background, Logf: o.Logf}
}

// RasterizePage renders a single page (zero-based index) to an image.
func (d *Document) RasterizePage(ctx context.Context, index int, opts RasterOptions) (image.Image, error) {
	pg, err := d.pdf.Page(index)
	if err != nil {
		return nil, fmt.Errorf("doctaculous: page %d: %w", index, err)
	}
	img, err := raster.RenderPage(ctx, pg, opts.rasterOpts())
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

	ropts := opts.rasterOpts()
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
				pg, err := d.pdf.Page(indices[pos])
				if err != nil {
					results[pos].Err = fmt.Errorf("doctaculous: page %d: %w", indices[pos], err)
					continue
				}
				img, err := raster.RenderPage(ctx, pg, ropts)
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
	idx := make([]int, d.pdf.PageCount())
	for i := range idx {
		idx[i] = i
	}
	return idx
}
