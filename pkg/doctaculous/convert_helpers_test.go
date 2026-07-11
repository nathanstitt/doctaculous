package doctaculous

import (
	"context"
	"io"
)

// Test-only shorthands over the generic Convert for the format pairs the
// tests exercise constantly. (The exported ConvertXToY convenience wrappers
// were removed from the public API; these keep the call sites readable.)

func convertHTMLToPDF(ctx context.Context, in io.Reader, out io.Writer, opts PDFOptions) error {
	return Convert(ctx, in, out, ConvertOptions{
		From:         FormatHTML,
		To:           FormatPDF,
		PDF:          opts,
		Logf:         opts.Logf,
		BundledFonts: opts.BundledFonts,
	})
}

func convertHTMLToMarkdown(ctx context.Context, in io.Reader, out io.Writer, opts MarkdownOptions) error {
	return Convert(ctx, in, out, ConvertOptions{
		From:     FormatHTML,
		To:       FormatMarkdown,
		Markdown: opts,
		Logf:     opts.Logf,
	})
}

func convertHTMLToText(ctx context.Context, in io.Reader, out io.Writer, opts MarkdownOptions) error {
	opts.Plain = true
	return convertHTMLToMarkdown(ctx, in, out, opts)
}

func convertPDFToMarkdown(ctx context.Context, in io.Reader, out io.Writer, opts MarkdownOptions) error {
	return Convert(ctx, in, out, ConvertOptions{
		From:     FormatPDF,
		To:       FormatMarkdown,
		Markdown: opts,
		Logf:     opts.Logf,
	})
}

func convertPDFToHTML(ctx context.Context, in io.Reader, out io.Writer, opts HTMLWriteOptions) error {
	return Convert(ctx, in, out, ConvertOptions{
		From:    FormatPDF,
		To:      FormatHTML,
		HTMLOut: opts,
		Logf:    opts.Logf,
	})
}
