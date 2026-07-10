package doctaculous

import (
	"errors"
	"fmt"
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
	FormatXLSX:     {input: false, output: false}, // input flips with the reader PR, output with the writer PR
	FormatPNG:      {input: false, output: true},
	FormatJPEG:     {input: false, output: true},
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
	case "png":
		return FormatPNG, nil
	case "jpeg", "jpg":
		return FormatJPEG, nil
	default:
		return FormatUnknown, fmt.Errorf("doctaculous: format %q: %w", s, ErrUnknownFormat)
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
	case ".png":
		return FormatPNG
	case ".jpg", ".jpeg":
		return FormatJPEG
	default:
		return FormatUnknown
	}
}
