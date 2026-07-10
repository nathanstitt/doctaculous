package doctaculous

import (
	"fmt"
	"os"
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/xlsx"
)

// OpenXLSX reads and renders a SpreadsheetML (.xlsx) workbook: every visible
// sheet becomes a ruled table (preceded by the sheet's name as a heading when
// more than one sheet is visible), laid out at the default viewport width. For
// additional options (e.g. WithPageSize) use OpenXLSXFile.
func OpenXLSX(path string) (*Document, error) {
	return OpenXLSXFile(path)
}

// OpenXLSXFile reads and renders an .xlsx file at path, applying any options.
func OpenXLSXFile(path string, opts ...HTMLOption) (*Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("doctaculous: open xlsx %q: %w", path, err)
	}
	return OpenXLSXBytes(data, opts...)
}

// OpenXLSXBytes renders an in-memory workbook, applying any options, and
// returns a Document ready to rasterize or convert. Cached cell values are the
// content (formulas are not evaluated); dates and times render through the
// cell's number format; merged ranges become native column/row spans; and
// bold/italic, solid fills, and explicit alignment carry into the table. The
// sheets flow through the HTML pipeline, so every HTMLOption applies and every
// output format follows.
func OpenXLSXBytes(data []byte, opts ...HTMLOption) (*Document, error) {
	wb, err := xlsx.OpenBytes(data)
	if err != nil {
		return nil, fmt.Errorf("doctaculous: %w", err)
	}
	doc, err := OpenHTMLBytes([]byte(workbookToHTML(wb)), opts...)
	if err != nil {
		return nil, err
	}
	doc.format = FormatXLSX
	return doc, nil
}

// workbookToHTML synthesizes the workbook's visible sheets as ruled tables.
func workbookToHTML(wb *xlsx.Workbook) string {
	var visible []xlsx.Sheet
	for _, s := range wb.Sheets {
		if !s.Hidden {
			visible = append(visible, s)
		}
	}
	var b strings.Builder
	b.WriteString("<!DOCTYPE html>\n<html>\n<head>\n<meta charset=\"utf-8\">\n<style>\n")
	b.WriteString("body { font-family: sans-serif; margin: 32px; }\n")
	b.WriteString("table { border-collapse: collapse; margin-bottom: 16px; }\n")
	b.WriteString("th, td { border: 1px solid #d0d7de; padding: 6px 13px; }\n")
	b.WriteString("</style>\n</head>\n<body>\n")
	for _, s := range visible {
		if len(visible) > 1 {
			b.WriteString("<h2>" + htmlEscaper.Replace(s.Name) + "</h2>\n")
		}
		sheetToHTML(&b, s)
	}
	b.WriteString("</body>\n</html>\n")
	return b.String()
}

// sheetToHTML emits one sheet's used range as a table, mapping merged ranges to
// colspan/rowspan (the origin carries the value; covered slots are omitted).
func sheetToHTML(b *strings.Builder, s xlsx.Sheet) {
	if len(s.Cells) == 0 {
		return
	}
	// origin maps a merge origin to its spans; covered marks non-origin slots.
	type span struct{ rows, cols int }
	origin := map[[2]int]span{}
	covered := map[[2]int]bool{}
	for _, m := range s.Merges {
		origin[[2]int{m.Row, m.Col}] = span{m.RowSpan, m.ColSpan}
		for r := m.Row; r < m.Row+m.RowSpan; r++ {
			for c := m.Col; c < m.Col+m.ColSpan; c++ {
				if r != m.Row || c != m.Col {
					covered[[2]int{r, c}] = true
				}
			}
		}
	}

	b.WriteString("<table>\n")
	for r, row := range s.Cells {
		b.WriteString("<tr>")
		for c, cell := range row {
			if covered[[2]int{r, c}] {
				continue
			}
			b.WriteString("<td")
			if sp, ok := origin[[2]int{r, c}]; ok {
				if sp.cols > 1 {
					fmt.Fprintf(b, ` colspan="%d"`, sp.cols)
				}
				if sp.rows > 1 {
					fmt.Fprintf(b, ` rowspan="%d"`, sp.rows)
				}
			}
			if style := cellStyleAttr(cell); style != "" {
				b.WriteString(` style="` + style + `"`)
			}
			b.WriteString(">" + htmlEscaper.Replace(cell.Text) + "</td>")
		}
		b.WriteString("</tr>\n")
	}
	b.WriteString("</table>\n")
}

// cellStyleAttr renders a cell's presentation facts as an inline style.
func cellStyleAttr(c xlsx.Cell) string {
	var parts []string
	if c.Bold {
		parts = append(parts, "font-weight: bold")
	}
	if c.Italic {
		parts = append(parts, "font-style: italic")
	}
	if c.FillRGB != "" {
		parts = append(parts, "background-color: #"+c.FillRGB)
	}
	if c.Align != "" {
		parts = append(parts, "text-align: "+c.Align)
	}
	return strings.Join(parts, "; ")
}
