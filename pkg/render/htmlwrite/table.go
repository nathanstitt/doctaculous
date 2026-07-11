package htmlwrite

import (
	"strconv"
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
	"github.com/nathanstitt/doctaculous/pkg/render/internal/boxwalk"
)

// table renders a DisplayTable box as an HTML <table>. Unlike GFM pipe tables, HTML
// expresses merged cells natively, so colspan/rowspan are emitted directly on the cell as
// attributes — a spanned cell's content is written exactly once, never duplicated. Header
// rows (a DisplayTableHeaderGroup, or a row whose cells are all <th>) are wrapped in a
// <thead> and use <th>; other rows go in <tbody> with <td>. A DisplayTableCaption child
// becomes a <caption>.
func (w *writer) table(b *cssbox.Box, depth int) {
	rows, headerFlags := boxwalk.CollectRows(b)
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
	for _, cell := range boxwalk.CellBoxesOf(rowBox) {
		w.cell(cell, headerRow || boxwalk.IsHeaderCell(cell), depth+1)
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
	if n := boxwalk.ClampSpan(cell.ColSpan); n > 1 {
		attrs.WriteString(` colspan="` + strconv.Itoa(n) + `"`)
	}
	if n := boxwalk.ClampSpan(cell.RowSpan); n > 1 {
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
	if boxwalk.HasInlineContent(cell) {
		parts = append(parts, strings.TrimSpace(w.inlineOpt(cell, header)))
	} else {
		for _, c := range cell.Children {
			parts = append(parts, strings.TrimSpace(w.inlineOpt(c, header)))
		}
	}
	br := "<br>"
	if w.opts.XHTML {
		br = "<br/>"
	}
	return strings.Join(boxwalk.FilterEmpty(parts), br)
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
