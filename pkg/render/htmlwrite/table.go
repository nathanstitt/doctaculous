package htmlwrite

import (
	"strconv"
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// table renders a DisplayTable box as an HTML <table>. Unlike GFM pipe tables, HTML
// expresses merged cells natively, so colspan/rowspan are emitted directly on the cell as
// attributes — a spanned cell's content is written exactly once, never duplicated. Header
// rows (a DisplayTableHeaderGroup, or a row whose cells are all <th>) are wrapped in a
// <thead> and use <th>; other rows go in <tbody> with <td>. A DisplayTableCaption child
// becomes a <caption>.
func (w *writer) table(b *cssbox.Box, depth int) {
	rows, headerFlags := collectRows(b)
	caption := w.captionMarkup(b)
	if len(rows) == 0 {
		w.opts.Logf("html table: no rows")
		if caption == "" {
			return
		}
	}

	w.line(depth, "<table>")
	if caption != "" {
		w.line(depth+1, "<caption>"+caption+"</caption>")
	}

	// Partition the leading contiguous block of header rows into a <thead>; the rest go in
	// a <tbody>.
	headerCount := 0
	for headerCount < len(headerFlags) && headerFlags[headerCount] {
		headerCount++
	}
	if headerCount > 0 {
		w.line(depth+1, "<thead>")
		for i := 0; i < headerCount; i++ {
			w.row(rows[i], true, depth+2)
		}
		w.line(depth+1, "</thead>")
	}
	if headerCount < len(rows) {
		w.line(depth+1, "<tbody>")
		for i := headerCount; i < len(rows); i++ {
			w.row(rows[i], headerFlags[i], depth+2)
		}
		w.line(depth+1, "</tbody>")
	}
	w.line(depth, "</table>")
}

// row renders one table row as a <tr> containing its cells. headerRow forces every cell to
// <th>; otherwise each cell is <th> when it is itself a header cell (bold), else <td>.
func (w *writer) row(rowBox *cssbox.Box, headerRow bool, depth int) {
	w.line(depth, "<tr>")
	for _, cell := range cellBoxesOf(rowBox) {
		w.cell(cell, headerRow || isHeaderCell(cell), depth+1)
	}
	w.line(depth, "</tr>")
}

// cell renders one table cell as <td> (or <th> for a header cell), carrying its
// colspan/rowspan and text-align. The cell's content is inline-serialized (block structure
// inside a cell is joined with <br>).
func (w *writer) cell(cell *cssbox.Box, header bool, depth int) {
	tag := "td"
	if header {
		tag = "th"
	}
	var attrs strings.Builder
	if n := clampSpan(cell.ColSpan); n > 1 {
		attrs.WriteString(` colspan="` + strconv.Itoa(n) + `"`)
	}
	if n := clampSpan(cell.RowSpan); n > 1 {
		attrs.WriteString(` rowspan="` + strconv.Itoa(n) + `"`)
	}
	switch cell.Style.TextAlign {
	case "center":
		attrs.WriteString(` style="text-align:center"`)
	case "right":
		attrs.WriteString(` style="text-align:right"`)
	}
	w.line(depth, "<"+tag+attrs.String()+">"+w.cellContent(cell, header)+"</"+tag+">")
}

// cellContent renders a cell box's content to inline HTML. A cell holding multiple block
// children (paragraphs) joins them with <br>; a cell that is a single inline run renders as
// one string. header suppresses the UA bold on a <th> so the header text is not wrapped in
// a redundant <strong>.
func (w *writer) cellContent(cell *cssbox.Box, header bool) string {
	var parts []string
	if hasInlineContent(cell) {
		parts = append(parts, strings.TrimSpace(w.inlineOpt(cell, header)))
	} else {
		for _, c := range cell.Children {
			parts = append(parts, strings.TrimSpace(w.inlineOpt(c, header)))
		}
	}
	return strings.Join(filterEmpty(parts), "<br>")
}

// captionMarkup renders a table's caption box (DisplayTableCaption) as inline HTML, or ""
// if the table has no caption.
func (w *writer) captionMarkup(table *cssbox.Box) string {
	for _, c := range table.Children {
		if c.Display == cssbox.DisplayTableCaption {
			return strings.TrimSpace(w.inline(c))
		}
	}
	return ""
}

// collectRows returns the table's rows in document order (descending through row groups)
// along with a parallel slice flagging which rows are header rows (a row inside a
// DisplayTableHeaderGroup, or whose cells are all header cells). Mirrors markdown/table.go.
func collectRows(table *cssbox.Box) ([]*cssbox.Box, []bool) {
	var rows []*cssbox.Box
	var header []bool
	var walk func(b *cssbox.Box, inHeader bool)
	walk = func(b *cssbox.Box, inHeader bool) {
		for _, c := range b.Children {
			switch c.Display {
			case cssbox.DisplayTableRow:
				rows = append(rows, c)
				header = append(header, inHeader || rowIsAllHeader(c))
			case cssbox.DisplayTableHeaderGroup:
				walk(c, true)
			case cssbox.DisplayTableRowGroup, cssbox.DisplayTableFooterGroup:
				walk(c, false)
			}
		}
	}
	walk(table, false)
	return rows, header
}

// rowIsAllHeader reports whether every cell in a row is a header cell (a <th>).
func rowIsAllHeader(row *cssbox.Box) bool {
	cells := cellBoxesOf(row)
	if len(cells) == 0 {
		return false
	}
	for _, c := range cells {
		if !isHeaderCell(c) {
			return false
		}
	}
	return true
}

// isHeaderCell reports whether a table cell is a header cell. HTML <th> gets
// font-weight:bold from the UA sheet; we treat a bold cell as a header. Mirrors
// markdown/table.go.
func isHeaderCell(cell *cssbox.Box) bool {
	return cell.Style.Bold
}

// cellBoxesOf returns the DisplayTableCell children of a row box.
func cellBoxesOf(row *cssbox.Box) []*cssbox.Box {
	var out []*cssbox.Box
	for _, c := range row.Children {
		if c.Display == cssbox.DisplayTableCell {
			out = append(out, c)
		}
	}
	return out
}

// clampSpan reads a span value (0 = absent) as at least 1.
func clampSpan(n int) int {
	if n < 1 {
		return 1
	}
	return n
}

// filterEmpty returns the non-empty strings of in, order-preserving.
func filterEmpty(in []string) []string {
	var out []string
	for _, s := range in {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}
