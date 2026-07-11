package doctaculous

import (
	"errors"
	"fmt"
	"mime"
	"path/filepath"
	"strings"
)

// Format identifies a document or image format for detection and conversion.
// Not every format is valid in every role — see ValidInput, ValidOutput, and
// CanConvert. The zero value is FormatUnknown.
type Format string

// The formats the toolkit knows about.
const (
	// FormatUnknown is the zero Format: an undetected or unspecified format.
	FormatUnknown Format = ""
	// FormatPDF is a PDF document.
	FormatPDF Format = "pdf"
	// FormatDOCX is a WordprocessingML (.docx) document.
	FormatDOCX Format = "docx"
	// FormatHTML is an HTML document.
	FormatHTML Format = "html"
	// FormatMarkdown is GitHub-Flavored Markdown.
	FormatMarkdown Format = "markdown"
	// FormatText is plain text.
	FormatText Format = "text"
	// FormatCSV is comma-separated values (one table per document, or the
	// document's tables on output).
	FormatCSV Format = "csv"
	// FormatTSV is tab-separated values.
	FormatTSV Format = "tsv"
	// FormatXLSX is a SpreadsheetML (.xlsx) workbook.
	FormatXLSX Format = "xlsx"
	// FormatRTF is a Rich Text Format document.
	FormatRTF Format = "rtf"
	// FormatPPTX is a PresentationML (.pptx) presentation.
	FormatPPTX Format = "pptx"
	// FormatEPUB is an EPUB book.
	FormatEPUB Format = "epub"
	// FormatPNG is a PNG image (a rasterized page).
	FormatPNG Format = "png"
	// FormatJPEG is a JPEG image (a rasterized page).
	FormatJPEG Format = "jpeg"
)

// Sentinel errors for format handling, for callers to branch on via errors.Is.
var (
	// ErrUnknownFormat reports that a format could not be detected from content
	// and filename, or that a format name was not recognized.
	ErrUnknownFormat = errors.New("unknown document format")
	// ErrUnsupportedFormat reports a recognized format used in a role it does not
	// support (e.g. PNG as a conversion input).
	ErrUnsupportedFormat = errors.New("unsupported format")
	// ErrSameFormat reports a generic conversion whose source and target formats
	// are the same. Converting a document to its own format is deliberately not
	// supported by the generic Convert/Write path; the format-specific writers
	// (e.g. WriteHTML on an HTML document) remain unrestricted.
	ErrSameFormat = errors.New("source and target are the same format")
)

// formatCaps is the single capability table consulted by both the API and the
// CLI: which formats are valid conversion inputs and which are valid outputs.
// A format absent from the table is unknown. When a new frontend or writer
// lands, its bit flips here and the whole conversion surface picks it up.
var formatCaps = map[Format]struct{ input, output bool }{
	FormatPDF:      {input: true, output: true},
	FormatDOCX:     {input: true, output: true},
	FormatHTML:     {input: true, output: true},
	FormatMarkdown: {input: true, output: true},
	FormatText:     {input: true, output: true},
	FormatCSV:      {input: true, output: true},
	FormatTSV:      {input: true, output: true},
	FormatXLSX:     {input: true, output: true},
	FormatRTF:      {input: true, output: false}, // output lands with pkg/render/rtfwrite
	FormatPPTX:     {input: true, output: false}, // output lands with pkg/render/pptxwrite
	FormatEPUB:     {input: true, output: false}, // output lands with pkg/render/epubwrite
	FormatPNG:      {input: true, output: true},
	FormatJPEG:     {input: true, output: true},
}

// ValidInput reports whether f is a supported conversion input (a format the
// toolkit can open).
func (f Format) ValidInput() bool { return formatCaps[f].input }

// ValidOutput reports whether f is a supported conversion output (a format the
// toolkit can write).
func (f Format) ValidOutput() bool { return formatCaps[f].output }

// CanConvert reports whether the toolkit can convert a document from one
// format to another. It returns nil when the pair is convertible, and
// otherwise the most informative error: ErrUnknownFormat for a format it does
// not recognize, ErrUnsupportedFormat for a format used in a role it does not
// support, and ErrSameFormat when from == to (same-format conversion is not
// supported on the generic path).
func CanConvert(from, to Format) error {
	if _, ok := formatCaps[from]; !ok {
		return fmt.Errorf("doctaculous: input format %q: %w", string(from), ErrUnknownFormat)
	}
	if _, ok := formatCaps[to]; !ok {
		return fmt.Errorf("doctaculous: output format %q: %w", string(to), ErrUnknownFormat)
	}
	if !from.ValidInput() {
		return fmt.Errorf("doctaculous: %s is not a supported input format: %w", from, ErrUnsupportedFormat)
	}
	if !to.ValidOutput() {
		return fmt.Errorf("doctaculous: %s is not a supported output format: %w", to, ErrUnsupportedFormat)
	}
	if from == to {
		return fmt.Errorf("doctaculous: cannot convert %s to %s: %w", from, to, ErrSameFormat)
	}
	return nil
}

// ParseFormat maps a user-facing format name to a Format. It accepts the
// canonical names plus common aliases (case-insensitive, with or without a
// leading dot): pdf; docx; html, htm, xhtml; markdown, md; text, txt, plain;
// png; jpeg, jpg. Unrecognized names return ErrUnknownFormat.
func ParseFormat(s string) (Format, error) {
	switch strings.ToLower(strings.TrimPrefix(strings.TrimSpace(s), ".")) {
	case "pdf":
		return FormatPDF, nil
	case "docx":
		return FormatDOCX, nil
	case "html", "htm", "xhtml":
		return FormatHTML, nil
	case "markdown", "md":
		return FormatMarkdown, nil
	case "text", "txt", "plain":
		return FormatText, nil
	case "csv":
		return FormatCSV, nil
	case "tsv", "tab":
		return FormatTSV, nil
	case "xlsx", "xlsm":
		return FormatXLSX, nil
	case "rtf":
		return FormatRTF, nil
	case "pptx", "pptm":
		return FormatPPTX, nil
	case "epub":
		return FormatEPUB, nil
	case "png":
		return FormatPNG, nil
	case "jpeg", "jpg":
		return FormatJPEG, nil
	default:
		return FormatUnknown, fmt.Errorf("doctaculous: format %q: %w", s, ErrUnknownFormat)
	}
}

// mimeFormats maps a normalized MIME media type to its Format. A type mapped
// to FormatUnknown is a DELIBERATE refusal — a type the toolkit recognizes but
// cannot (or must not) read — and overrides the text/* fallback in
// FormatFromMIME. Notably the legacy binary Office types must never map to
// their OOXML cousins: a .doc is not a .docx.
var mimeFormats = map[string]Format{
	"application/pdf":   FormatPDF,
	"application/x-pdf": FormatPDF,
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": FormatDOCX,
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":       FormatXLSX,
	"text/html":                 FormatHTML,
	"application/xhtml+xml":     FormatHTML,
	"text/markdown":             FormatMarkdown,
	"text/x-markdown":           FormatMarkdown,
	"text/plain":                FormatText,
	"text/csv":                  FormatCSV,
	"application/csv":           FormatCSV,
	"text/tab-separated-values": FormatTSV,
	"application/rtf":           FormatRTF,
	"text/rtf":                  FormatRTF,
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": FormatPPTX,
	"application/epub+zip": FormatEPUB,
	"image/png":            FormatPNG,
	"image/jpeg":           FormatJPEG,
	"image/jpg":            FormatJPEG,

	// Deliberate refusals. Legacy binary Office formats have no pure-Go reader
	// and are not the OOXML formats their names resemble.
	"application/msword":            FormatUnknown,
	"application/vnd.ms-word":       FormatUnknown,
	"application/vnd.ms-excel":      FormatUnknown,
	"application/vnd.ms-powerpoint": FormatUnknown,
	// No viable pure-Go HEIC/HEIF decoder exists.
	"image/heic":          FormatUnknown,
	"image/heif":          FormatUnknown,
	"image/heic-sequence": FormatUnknown,
	"image/heif-sequence": FormatUnknown,
	// Generic containers say nothing about their content; use DetectFormat.
	"application/zip":          FormatUnknown,
	"application/octet-stream": FormatUnknown,
}

// FormatFromMIME maps a MIME media type to a Format. The type is normalized
// first — parameters such as "; charset=utf-8" are stripped and the type is
// case-folded (mime.ParseMediaType, with a manual strip for malformed values).
// Unrecognized types, and types the toolkit deliberately does not read (legacy
// binary Office, HEIC/HEIF, generic zip/octet-stream), return FormatUnknown;
// an unlisted text/* subtype falls back to FormatText (unknown text renders as
// plain text, the browser rule) — listed text/* rows like text/rtf take their
// own format first. Callers with untrusted or generic types compose with
// content detection: f := FormatFromMIME(mt); if f == FormatUnknown { f =
// DetectFormat(data, name) }.
func FormatFromMIME(mimeType string) Format {
	mt, _, err := mime.ParseMediaType(mimeType)
	if err != nil {
		// Malformed parameter section (or empty input) — still make a
		// best-effort match on the media type itself.
		mt = strings.ToLower(strings.TrimSpace(mimeType))
		if i := strings.IndexByte(mt, ';'); i >= 0 {
			mt = strings.TrimSpace(mt[:i])
		}
	}
	if f, ok := mimeFormats[mt]; ok {
		return f
	}
	if strings.HasPrefix(mt, "text/") {
		return FormatText
	}
	return FormatUnknown
}

// MIME returns f's canonical MIME type, or "" for FormatUnknown.
// FormatFromMIME(f.MIME()) == f holds for every format the toolkit knows.
func (f Format) MIME() string {
	switch f {
	case FormatPDF:
		return "application/pdf"
	case FormatDOCX:
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case FormatHTML:
		return "text/html"
	case FormatMarkdown:
		return "text/markdown"
	case FormatText:
		return "text/plain"
	case FormatCSV:
		return "text/csv"
	case FormatTSV:
		return "text/tab-separated-values"
	case FormatXLSX:
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case FormatRTF:
		return "application/rtf"
	case FormatPPTX:
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	case FormatEPUB:
		return "application/epub+zip"
	case FormatPNG:
		return "image/png"
	case FormatJPEG:
		return "image/jpeg"
	default:
		return ""
	}
}

// FormatFromPath maps a filename extension to a Format, returning
// FormatUnknown when the extension is missing or unrecognized. It is the
// extension half of DetectFormat and the way ConvertFile infers its output
// format.
func FormatFromPath(path string) Format {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pdf":
		return FormatPDF
	case ".docx":
		return FormatDOCX
	case ".html", ".htm", ".xhtml":
		return FormatHTML
	case ".md", ".markdown":
		return FormatMarkdown
	case ".txt", ".text":
		return FormatText
	case ".csv":
		return FormatCSV
	case ".tsv", ".tab":
		return FormatTSV
	case ".xlsx", ".xlsm":
		return FormatXLSX
	case ".rtf":
		return FormatRTF
	case ".pptx", ".pptm":
		return FormatPPTX
	case ".epub":
		return FormatEPUB
	case ".png":
		return FormatPNG
	case ".jpg", ".jpeg":
		return FormatJPEG
	default:
		return FormatUnknown
	}
}
