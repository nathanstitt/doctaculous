// Package doctaculous is the public API for the doctaculous document toolkit.
//
// Open a document and rasterize its pages to images:
//
//	doc, err := doctaculous.Open("input.pdf")
//	if err != nil {
//		// handle error
//	}
//	img, err := doc.RasterizePage(ctx, 0, doctaculous.RasterOptions{DPI: 150})
//
// RasterizePages renders multiple pages concurrently with a bounded worker pool.
// A parsed document is read-only and safe to use from multiple goroutines.
package doctaculous
