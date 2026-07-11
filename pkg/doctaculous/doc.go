// Package doctaculous is the public API for the doctaculous document toolkit.
//
// Open a document — the format (PDF, DOCX, HTML) is detected from content and
// filename — and rasterize its pages to images:
//
//	doc, err := doctaculous.Open("input.pdf")
//	if err != nil {
//		// handle error
//	}
//	img, err := doc.RasterizePage(ctx, 0, doctaculous.RasterOptions{DPI: 150})
//
// RasterizePages renders multiple pages concurrently with a bounded worker pool.
// A parsed document is read-only and safe to use from multiple goroutines.
//
// Convert between formats generically — any supported input (PDF, DOCX, HTML)
// to any supported output (PDF, HTML, Markdown, plain text, PNG, JPEG):
//
//	err := doctaculous.ConvertFile(ctx, "report.docx", "report.pdf", doctaculous.ConvertOptions{})
//
// or stream with explicit formats:
//
//	err := doctaculous.Convert(ctx, in, out, doctaculous.ConvertOptions{
//		From: doctaculous.FormatPDF,
//		To:   doctaculous.FormatMarkdown,
//	})
//
// Converting a document to its own format is not supported (ErrSameFormat).
// See CanConvert for the supported matrix, and DetectFormat for how formats
// are sniffed.
package doctaculous
