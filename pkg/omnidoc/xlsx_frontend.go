package omnidoc

import (
	"fmt"
	"os"
	"strings"

	"github.com/nathanstitt/omnidoc/pkg/xlsx"
)

// ErrSheetNotFound reports that a sheet name passed to WithSheets matches no
// worksheet in the workbook. Callers can branch on it via errors.Is; the wrapping
// error names the missing sheet.
//
// It IS [xlsx.ErrSheetNotFound], not a second sentinel that happens to mean the
// same thing. Until v1 these were distinct values whose messages differed by a
// single colon ("xlsx sheet not found" here, "xlsx: sheet not found" there), so
// errors.Is between them returned false in both directions -- a caller using both
// packages, which is the whole point of this one, would write the wrong check and
// it would compile, pass review, and silently never match.
//
// Aliasing rather than wrapping keeps both names working for existing callers
// while making the two interchangeable; pkg/xlsx owns the concept because it is
// the lower-level package that actually resolves sheet names.
var ErrSheetNotFound = xlsx.ErrSheetNotFound

// OpenXLSXFile reads and renders an .xlsx file at path, applying any options
// (e.g. WithSheets to select worksheets, WithPageSize for pagination).
func OpenXLSXFile(path string, opts ...OpenOption) (*Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("omnidoc: open xlsx %q: %w", path, err)
	}
	return OpenXLSXBytes(data, opts...)
}

// OpenXLSXBytes renders an in-memory workbook, applying any options, and
// returns a Document ready to rasterize or convert. Cached cell values are the
// content (formulas are not evaluated); dates and times render through the
// cell's number format; merged ranges become native column/row spans; and
// bold/italic, solid fills, and explicit alignment carry into the table. By
// default every visible sheet is rendered; WithSheets restricts the render to
// named worksheets (see that option). The sheets flow through the HTML pipeline,
// so every reflow OpenOption applies and every output format follows.
func OpenXLSXBytes(data []byte, opts ...OpenOption) (*Document, error) {
	cfg := defaultOpenConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	wb, err := xlsx.OpenBytes(cfg.ctx, data)
	if err != nil {
		return nil, fmt.Errorf("omnidoc: %w", err)
	}
	sheets, err := selectSheets(wb, cfg.sheets)
	if err != nil {
		return nil, err
	}
	doc, err := OpenHTMLBytes([]byte(workbookToHTML(sheets)), opts...)
	if err != nil {
		return nil, err
	}
	doc.format = FormatXLSX
	return doc, nil
}

// selectSheets resolves which sheets to render. With no names it returns the
// workbook's visible sheets in file order (the default). With names it returns
// exactly those sheets, in the requested order, resolving each by exact tab
// name — including a hidden sheet named explicitly — and fails with an error
// wrapping ErrSheetNotFound on the first name no sheet carries.
func selectSheets(wb *xlsx.Workbook, names []string) ([]xlsx.Sheet, error) {
	if len(names) == 0 {
		var visible []xlsx.Sheet
		for _, s := range wb.Sheets {
			if s.Visibility == xlsx.SheetVisible {
				visible = append(visible, s)
			}
		}
		return visible, nil
	}
	byName := make(map[string]xlsx.Sheet, len(wb.Sheets))
	for _, s := range wb.Sheets {
		byName[s.Name] = s
	}
	selected := make([]xlsx.Sheet, 0, len(names))
	for _, name := range names {
		s, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("omnidoc: xlsx: sheet %q: %w", name, ErrSheetNotFound)
		}
		selected = append(selected, s)
	}
	return selected, nil
}

// workbookToHTML synthesizes the given sheets as ruled tables. The per-sheet name
// heading is emitted only when more than one sheet is rendered, so a single
// selected (or single visible) sheet reads as a bare table.
func workbookToHTML(sheets []xlsx.Sheet) string {
	var b strings.Builder
	b.WriteString("<!DOCTYPE html>\n<html>\n<head>\n<meta charset=\"utf-8\">\n<style>\n")
	b.WriteString("body { font-family: sans-serif; margin: 32px; }\n")
	b.WriteString("table { border-collapse: collapse; margin-bottom: 16px; }\n")
	b.WriteString("th, td { border: 1px solid #d0d7de; padding: 6px 13px; }\n")
	b.WriteString("</style>\n</head>\n<body>\n")
	for _, s := range sheets {
		if len(sheets) > 1 {
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

// cellStyleAttr renders a cell's presentation facts as an inline style: the
// font's weight and slant, a solid fill with an explicit RGB (indexed and theme
// colours are not resolved here), and an explicit horizontal alignment.
func cellStyleAttr(c xlsx.Cell) string {
	st := c.Style
	if st == nil {
		return ""
	}
	var parts []string
	if st.Font.Bold {
		parts = append(parts, "font-weight: bold")
	}
	if st.Font.Italic {
		parts = append(parts, "font-style: italic")
	}
	if st.Fill.Pattern == "solid" && st.Fill.Fg.RGB != "" {
		parts = append(parts, "background-color: #"+st.Fill.Fg.RGB)
	}
	switch st.Alignment.Horizontal {
	case "left", "center", "right":
		parts = append(parts, "text-align: "+st.Alignment.Horizontal)
	}
	return strings.Join(parts, "; ")
}
