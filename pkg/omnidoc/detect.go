package omnidoc

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"io"
	"strings"
)

// DetectFormat sniffs the format of a document from its content, using hint (a
// filename or path; "" for none) as a tiebreaker. Detection order:
//
//  1. Binary magic — content beats the extension, so a real PDF named
//     report.txt is still a PDF: a PDF header (%PDF- within the first 1 KiB,
//     where the spec allows it), PNG/JPEG signatures, and a ZIP whose central
//     directory identifies a WordprocessingML package (DOCX). Other ZIPs
//     (xlsx, epub, plain archives) fall through. A gzip signature decompresses
//     a small prefix to check for SVG (.svgz); anything else gzipped is
//     unknown.
//  2. The hint's extension (FormatFromPath) — the only signal for Markdown and
//     plain text, which have no magic. It runs before HTML sniffing so a
//     README.md full of raw HTML blocks stays Markdown, and it rescues a
//     binary file whose magic is damaged (e.g. a .pdf with more than 1 KiB of
//     junk before the header, which the parser's object-scan rebuild can still
//     open).
//  3. An SVG root-element sniff, checked before HTML so an XML-prologed SVG
//     (which also starts with "<?xml", an HTML sniff pattern) resolves to SVG
//     rather than HTML; genuine XHTML still falls through to the HTML sniff.
//  4. An HTML tag sniff modeled on the WHATWG MIME-sniffing pattern table.
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
	if sniffSVG(data) {
		return FormatSVG
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
	case bytes.HasPrefix(data, []byte(`{\rtf`)):
		return FormatRTF
	case bytes.HasPrefix(data, []byte{0x1f, 0x8b}):
		return detectGzip(data)
	case isRIFFWebP(data):
		return FormatWebP
	}
	if f := classifyISOBMFF(data); f != FormatUnknown {
		return f
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

// isRIFFWebP reports whether data is a RIFF container whose form type is
// WEBP. The check is deliberately narrow: RIFF also carries WAV and AVI, so
// the "WEBP" form type at offset 8 — not the "RIFF" magic alone — is what
// identifies the format. Animated files match too; they are refused later, by
// the decoder, with an error that names animation (see pkg/internal/webp), which is a
// better report than "unknown format" from here.
func isRIFFWebP(data []byte) bool {
	return len(data) >= 12 &&
		bytes.Equal(data[0:4], []byte("RIFF")) &&
		bytes.Equal(data[8:12], []byte("WEBP"))
}

// detectGzip identifies a gzip-compressed document by sniffing its
// decompressed prefix: only .svgz (a gzipped SVG) is recognized today. A
// generic .gz says nothing about its content on its own — mirroring the plain
// zip/octet-stream refusals in mimeFormats — so anything else, or a
// corrupt/truncated stream, returns FormatUnknown rather than guessing. The
// read is capped at 4 KiB, comfortably more than sniffSVG needs and small
// enough that a maliciously crafted gzip bomb can't force unbounded work.
func detectGzip(data []byte) Format {
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return FormatUnknown
	}
	defer func() { _ = zr.Close() }()
	prefix, err := io.ReadAll(io.LimitReader(zr, 4096))
	if err != nil && len(prefix) == 0 {
		return FormatUnknown
	}
	if sniffSVG(prefix) {
		return FormatSVG
	}
	return FormatUnknown
}

// heifBrands are the ftyp brands identifying HEVC-coded still HEIF. The
// allowlist is deliberately tight: .mp4/.mov/.avif files share the ftyp
// container signature and must stay FormatUnknown (a silent misdetection is
// worse than a clean error). The structural mif1 brand counts only when an
// heic-family brand also appears in the compatible list.
var heifBrands = map[string]bool{
	"heic": true, "heix": true, "heim": true, "heis": true,
	"hevc": true, "hevx": true, "hevm": true, "hevs": true,
}

// classifyISOBMFF identifies an ISO base-media file ("<size>ftyp<brand>...")
// as HEIC when its brands say so, else FormatUnknown.
func classifyISOBMFF(data []byte) Format {
	if len(data) < 12 || !bytes.Equal(data[4:8], []byte("ftyp")) {
		return FormatUnknown
	}
	size := int(binary.BigEndian.Uint32(data))
	if size < 16 || size > len(data) {
		return FormatUnknown
	}
	if heifBrands[string(data[8:12])] {
		return FormatHEIC
	}
	if string(data[8:12]) == "mif1" {
		// Compatible brands start after the minor version.
		for off := 16; off+4 <= size; off += 4 {
			if heifBrands[string(data[off:off+4])] {
				return FormatHEIC
			}
		}
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
	hasContentTypes, hasWordPart, hasXLPart, hasPPTPart := false, false, false, false
	for _, f := range zr.File {
		switch {
		case f.Name == "word/document.xml":
			return FormatDOCX
		case f.Name == "xl/workbook.xml":
			return FormatXLSX
		case f.Name == "ppt/presentation.xml":
			return FormatPPTX
		case f.Name == "mimetype":
			// The EPUB OCF signature: a "mimetype" entry declaring the type.
			if rc, err := f.Open(); err == nil {
				mt, _ := io.ReadAll(io.LimitReader(rc, 64))
				_ = rc.Close()
				if strings.TrimSpace(string(mt)) == "application/epub+zip" {
					return FormatEPUB
				}
			}
		case f.Name == "[Content_Types].xml":
			hasContentTypes = true
		case strings.HasPrefix(f.Name, "word/"):
			hasWordPart = true
		case strings.HasPrefix(f.Name, "xl/"):
			hasXLPart = true
		case strings.HasPrefix(f.Name, "ppt/"):
			hasPPTPart = true
		}
	}
	switch {
	case hasContentTypes && hasWordPart:
		return FormatDOCX
	case hasContentTypes && hasXLPart:
		return FormatXLSX
	case hasContentTypes && hasPPTPart:
		return FormatPPTX
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

// sniffSVG reports whether data's first element is an SVG root: after an
// optional BOM, whitespace, XML prolog, comments, and doctype, the next tag is
// "<svg" followed by a name-ending byte. It looks only at the first element,
// so HTML documents containing inline <svg> do not match.
func sniffSVG(data []byte) bool {
	data = bytes.TrimPrefix(data, []byte("\xEF\xBB\xBF"))
	for {
		data = bytes.TrimLeft(data, "\t\n\f\r ")
		switch {
		case bytes.HasPrefix(data, []byte("<?")):
			i := bytes.Index(data, []byte("?>"))
			if i < 0 {
				return false
			}
			data = data[i+2:]
		case bytes.HasPrefix(data, []byte("<!--")):
			i := bytes.Index(data, []byte("-->"))
			if i < 0 {
				return false
			}
			data = data[i+3:]
		case bytes.HasPrefix(data, []byte("<!")):
			i := bytes.IndexByte(data, '>')
			if i < 0 {
				return false
			}
			data = data[i+1:]
		default:
			if len(data) < 5 || !bytes.EqualFold(data[:4], []byte("<svg")) {
				return false
			}
			switch data[4] {
			case ' ', '\t', '\n', '\f', '\r', '>', '/':
				return true
			case ':':
				// A namespace-prefixed root, e.g. <svg:svg xmlns:svg="...">,
				// is genuinely valid SVG and worth recognizing since "svg" is
				// the near-universal prefix convention for that namespace.
				// But byte-level sniffing can't resolve an arbitrary prefix
				// bound to the SVG namespace (<s:svg>, <foo:svg>) without
				// real namespace processing, so don't treat ':' alone as a
				// terminator — that would match ANY element merely starting
				// "svg:" (e.g. <svg:zzz>). Require the local name after the
				// colon to actually be "svg" too.
				rest := data[5:]
				if len(rest) < 3 || !bytes.EqualFold(rest[:3], []byte("svg")) {
					return false
				}
				if len(rest) == 3 {
					return false
				}
				switch rest[3] {
				case ' ', '\t', '\n', '\f', '\r', '>', '/':
					return true
				}
			}
			return false
		}
	}
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
