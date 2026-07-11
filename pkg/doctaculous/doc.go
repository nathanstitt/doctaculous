// Package doctaculous is the public API for the doctaculous document toolkit.
//
// Open a document — the format is detected from content and filename — and
// rasterize its pages to images:
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
// Convert between formats generically — any supported input to any supported
// output:
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
//
// # Features
//
// The toolkit reads and writes thirteen formats — PDF, DOCX, XLSX, PPTX, EPUB,
// RTF, HTML, Markdown, plain text, CSV, TSV, PNG, and JPEG. Every format is
// both an input and an output, and HTML input may also be an http(s) URL.
// Highlights:
//
//   - A pure-Go PDF interpreter: classic and stream xrefs, object streams,
//     broken-file repair, RC4/AES-128/AES-256 encryption, the full filter set
//     (Flate, LZW, CCITT, DCT, JBIG2, ...), embedded TrueType/CFF/Type1/CID
//     fonts, blend modes, shadings, clipping, and inline images.
//   - A from-scratch CSS layout engine that drives HTML, DOCX, and every other
//     reflowable format: cascade, floats, positioning, stacking, both table
//     border models, flexbox, grid, web fonts (WOFF1/WOFF2), counters, and CSS
//     Paged Media (@page, margin boxes, running headers/footers).
//   - Structure-preserving conversion: headings, links, lists, and tables
//     survive — including tables recovered from a PDF's ruling lines and
//     whitespace, so pdf-to-xlsx extracts spreadsheet-ready data.
//   - Selectable-text PDF output with subsetted embedded fonts.
//   - Preservation-first office editing via the sibling pkg/xlsx (byte-identical
//     no-op saves) and pkg/docx (Parse-Write round-trip fixed point) packages.
//
// The complete feature inventory lives in [FEATURES.md]. Everything is pure Go
// — no CGo, no native bindings — and MIT licensed.
//
// [FEATURES.md]: https://github.com/nathanstitt/doctaculous/blob/main/FEATURES.md
package doctaculous
