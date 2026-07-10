package doctaculous

import (
	"archive/zip"
	"bytes"
	"strings"
)

// DetectFormat sniffs the format of a document from its content, using hint (a
// filename or path; "" for none) as a tiebreaker. Detection order:
//
//  1. Binary magic — content beats the extension, so a real PDF named
//     report.txt is still a PDF: a PDF header (%PDF- within the first 1 KiB,
//     where the spec allows it), PNG/JPEG signatures, and a ZIP whose central
//     directory identifies a WordprocessingML package (DOCX). Other ZIPs
//     (xlsx, epub, plain archives) fall through.
//  2. The hint's extension (FormatFromPath) — the only signal for Markdown and
//     plain text, which have no magic. It runs before HTML sniffing so a
//     README.md full of raw HTML blocks stays Markdown, and it rescues a
//     binary file whose magic is damaged (e.g. a .pdf with more than 1 KiB of
//     junk before the header, which the parser's object-scan rebuild can still
//     open).
//  3. An HTML tag sniff modeled on the WHATWG MIME-sniffing pattern table.
//
// It returns FormatUnknown when nothing matches. There is deliberately no
// "decodes as UTF-8 ⇒ plain text" fallback: random binary often decodes, and a
// silent misdetection is worse than a clean error telling the caller to name
// the format (OpenAs, ConvertOptions.From, or the CLI --from flag).
func DetectFormat(data []byte, hint string) Format {
	if f := detectMagic(data); f != FormatUnknown {
		return f
	}
	if f := FormatFromPath(hint); f != FormatUnknown {
		return f
	}
	if sniffHTML(data) {
		return FormatHTML
	}
	return FormatUnknown
}

// pdfHeaderWindow is how far into the file the %PDF- header may appear: the
// PDF spec requires it within the first 1024 bytes (some producers prepend
// junk).
const pdfHeaderWindow = 1024

// detectMagic identifies a format from its binary signature, or FormatUnknown.
func detectMagic(data []byte) Format {
	switch {
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		return FormatPNG
	case bytes.HasPrefix(data, []byte("\xFF\xD8\xFF")):
		return FormatJPEG
	case bytes.HasPrefix(data, []byte("PK\x03\x04")), bytes.HasPrefix(data, []byte("PK\x05\x06")):
		return classifyOPC(data) // DOCX, XLSX, or Unknown (epub, plain archive)
	}
	window := data
	if len(window) > pdfHeaderWindow {
		window = window[:pdfHeaderWindow]
	}
	if bytes.Contains(window, []byte("%PDF-")) {
		return FormatPDF
	}
	return FormatUnknown
}

// classifyOPC identifies which Office package a ZIP's central directory holds:
// the main part at its conventional location (word/document.xml → DOCX,
// xl/workbook.xml → XLSX), or — tolerating a rels-redirected main part, as the
// readers do — an OPC [Content_Types].xml alongside any word/- or xl/-prefixed
// part. Other ZIPs (pptx, epub, plain archives) are Unknown. Reading the
// central directory only touches the end of the archive, so this is cheap.
func classifyOPC(data []byte) Format {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return FormatUnknown
	}
	hasContentTypes, hasWordPart, hasXLPart := false, false, false
	for _, f := range zr.File {
		switch {
		case f.Name == "word/document.xml":
			return FormatDOCX
		case f.Name == "xl/workbook.xml":
			return FormatXLSX
		case f.Name == "[Content_Types].xml":
			hasContentTypes = true
		case strings.HasPrefix(f.Name, "word/"):
			hasWordPart = true
		case strings.HasPrefix(f.Name, "xl/"):
			hasXLPart = true
		}
	}
	switch {
	case hasContentTypes && hasWordPart:
		return FormatDOCX
	case hasContentTypes && hasXLPart:
		return FormatXLSX
	}
	return FormatUnknown
}

// htmlSniffPatterns is the WHATWG MIME-sniffing §7.1 pattern table for
// identifying HTML in an unknown byte stream: each pattern, matched
// case-insensitively after skipping a UTF-8 BOM and leading whitespace, must
// be followed by a tag-terminating byte. "<!--" and "<?xml" (XHTML, which the
// lenient HTML parser handles) are accepted without a terminator.
var htmlSniffPatterns = []string{
	"<!DOCTYPE HTML",
	"<HTML",
	"<HEAD",
	"<SCRIPT",
	"<IFRAME",
	"<H1",
	"<DIV",
	"<FONT",
	"<TABLE",
	"<A",
	"<STYLE",
	"<TITLE",
	"<B",
	"<BODY",
	"<BR",
	"<P",
}

// sniffHTML reports whether data begins (after an optional UTF-8 BOM and
// leading whitespace) with an HTML tag from the WHATWG sniffing table.
func sniffHTML(data []byte) bool {
	data = bytes.TrimPrefix(data, []byte("\xEF\xBB\xBF"))
	data = bytes.TrimLeft(data, "\t\n\f\r ")
	if bytes.HasPrefix(data, []byte("<!--")) {
		return true
	}
	if len(data) >= 5 && bytes.EqualFold(data[:5], []byte("<?xml")) {
		return true
	}
	for _, pat := range htmlSniffPatterns {
		if len(data) <= len(pat) {
			continue
		}
		if !bytes.EqualFold(data[:len(pat)], []byte(pat)) {
			continue
		}
		switch data[len(pat)] {
		case ' ', '\t', '\n', '\f', '\r', '>':
			return true
		}
	}
	return false
}
