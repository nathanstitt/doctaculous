package doctaculous

import (
	"encoding/csv"
	"fmt"
	"os"
	"strings"
)

// OpenCSV reads and renders a comma-separated-values file as a single ruled
// table, laying it out at the default viewport width into one tall page. For
// additional options (e.g. WithPageSize) use OpenCSVFile.
func OpenCSV(path string) (*Document, error) {
	return OpenCSVFile(path)
}

// OpenCSVFile reads and renders a CSV file at path, applying any options.
func OpenCSVFile(path string, opts ...HTMLOption) (*Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("doctaculous: open csv %q: %w", path, err)
	}
	return OpenCSVBytes(data, opts...)
}

// OpenCSVBytes renders in-memory CSV, applying any options, and returns a
// Document ready to rasterize or convert. The rows become one table with the
// first row as its header row — the convention that makes Markdown/DOCX table
// output read correctly; ragged rows are padded to the widest row. Parsing is
// tolerant of real-world files (lazy quotes, CRLF, a UTF-8 BOM). The table
// flows through the HTML pipeline, so every HTMLOption applies. Empty input
// yields a valid empty document.
func OpenCSVBytes(data []byte, opts ...HTMLOption) (*Document, error) {
	return openSeparatedValues(data, ',', FormatCSV, opts)
}

// OpenTSV reads and renders a tab-separated-values file. See OpenCSV.
func OpenTSV(path string) (*Document, error) {
	return OpenTSVFile(path)
}

// OpenTSVFile reads and renders a TSV file at path, applying any options.
func OpenTSVFile(path string, opts ...HTMLOption) (*Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("doctaculous: open tsv %q: %w", path, err)
	}
	return OpenTSVBytes(data, opts...)
}

// OpenTSVBytes renders in-memory TSV. See OpenCSVBytes.
func OpenTSVBytes(data []byte, opts ...HTMLOption) (*Document, error) {
	return openSeparatedValues(data, '\t', FormatTSV, opts)
}

// openSeparatedValues parses delimiter-separated rows and lowers them to an
// HTML table document.
func openSeparatedValues(data []byte, comma rune, format Format, opts []HTMLOption) (*Document, error) {
	rows, err := parseSeparatedValues(data, comma)
	if err != nil {
		return nil, fmt.Errorf("doctaculous: parse %s: %w", format, err)
	}
	doc, err := OpenHTMLBytes([]byte(rowsToHTML(rows)), opts...)
	if err != nil {
		return nil, err
	}
	doc.format = format
	return doc, nil
}

// parseSeparatedValues reads all rows, tolerating real-world quirks: a UTF-8
// BOM, lazy quoting, and ragged rows (padded to the widest row so the table
// stays rectangular).
func parseSeparatedValues(data []byte, comma rune) ([][]string, error) {
	s := strings.TrimPrefix(string(data), "\uFEFF")
	r := csv.NewReader(strings.NewReader(s))
	r.Comma = comma
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	rows, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	width := 0
	for _, row := range rows {
		if len(row) > width {
			width = len(row)
		}
	}
	for i, row := range rows {
		for len(row) < width {
			row = append(row, "")
		}
		rows[i] = row
	}
	return rows, nil
}

// rowsToHTML synthesizes the table document: the first row as the header, a
// ruled collapsed-border look (matching the Markdown frontend's table style),
// and everything escaped so cell content stays literal.
func rowsToHTML(rows [][]string) string {
	var b strings.Builder
	b.WriteString("<!DOCTYPE html>\n<html>\n<head>\n<meta charset=\"utf-8\">\n<style>\n")
	b.WriteString("body { font-family: sans-serif; margin: 32px; }\n")
	b.WriteString("table { border-collapse: collapse; }\n")
	b.WriteString("th, td { border: 1px solid #d0d7de; padding: 6px 13px; }\n")
	b.WriteString("</style>\n</head>\n<body>\n")
	if len(rows) > 0 {
		b.WriteString("<table>\n<thead><tr>")
		for _, cell := range rows[0] {
			b.WriteString("<th>" + htmlEscaper.Replace(cell) + "</th>")
		}
		b.WriteString("</tr></thead>\n<tbody>\n")
		for _, row := range rows[1:] {
			b.WriteString("<tr>")
			for _, cell := range row {
				b.WriteString("<td>" + htmlEscaper.Replace(cell) + "</td>")
			}
			b.WriteString("</tr>\n")
		}
		b.WriteString("</tbody>\n</table>\n")
	}
	b.WriteString("</body>\n</html>\n")
	return b.String()
}
