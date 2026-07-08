package markdown

import (
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// table renders a DisplayTable box as a GFM pipe table. GFM pipe tables cannot express
// merged cells, so a cell that spans multiple columns/rows has its content duplicated
// into every grid slot it covers (the chosen high-fidelity strategy): the table stays
// rectangular and no content is lost, at the cost of showing a merged value more than
// once. A caption becomes a bold line above the table. In plain mode the table is
// rendered as space-padded columns instead of pipes.
func (w *writer) table(b *cssbox.Box) {
	grid := buildGrid(b)
	if len(grid.rows) == 0 {
		return
	}
	caption := w.captionText(b)
	if w.opts.Plain {
		w.emit(joinNonEmpty(caption, w.plainTable(grid)))
		return
	}
	w.emit(joinNonEmpty(caption, w.gfmTable(grid)))
}

// cellData is one rendered grid slot: its Markdown text and column alignment.
type cellData struct {
	text  string
	align string // "", "left", "center", "right" from the cell's text-align
}

// tableModel is the rectangular occupancy grid after span expansion. rows[r][c] is the
// slot at (r,c); every slot is filled (short rows are padded with empty cells).
type tableModel struct {
	rows       [][]cellData
	headerRows int // number of leading rows that are header rows
	cols       int
}

// buildGrid walks a DisplayTable subtree and produces the rectangular occupancy grid,
// expanding colspan/rowspan by duplicating cell content into every covered slot. It
// mirrors the occupancy scan the layout table builder uses (pkg/layout/css/table.go):
// slots is the sparse (row,col)→content map; for each origin cell we advance to the
// next free column in its row, then stamp its content into every ColSpan×RowSpan slot
// it covers. Materializing slots into a dense grid afterwards fills every position,
// which is exactly the "duplicate spanned content" behavior.
func buildGrid(table *cssbox.Box) tableModel {
	rowBoxes, headerFlags := collectRows(table)
	slots := map[[2]int]cellData{}
	maxCol := 0
	for r, rowBox := range rowBoxes {
		col := 0
		for _, cellBox := range cellBoxesOf(rowBox) {
			// Skip columns already occupied by a rowspan carried down from above.
			for {
				if _, taken := slots[[2]int{r, col}]; !taken {
					break
				}
				col++
			}
			colSpan := clampSpan(cellBox.ColSpan)
			rowSpan := clampSpan(cellBox.RowSpan)
			header := headerFlags[r] || isHeaderCell(cellBox)
			cd := cellData{
				text:  renderCell(cellBox, header),
				align: cellBox.Style.TextAlign,
			}
			for rs := 0; rs < rowSpan; rs++ {
				for cs := 0; cs < colSpan; cs++ {
					slots[[2]int{r + rs, col + cs}] = cd
				}
			}
			col += colSpan
			if col > maxCol {
				maxCol = col
			}
		}
	}

	// Total row count includes rows introduced purely by a rowspan overhang.
	nRows := len(rowBoxes)
	for key := range slots {
		if key[0]+1 > nRows {
			nRows = key[0] + 1
		}
	}

	var m tableModel
	m.cols = maxCol
	for r := 0; r < nRows; r++ {
		row := make([]cellData, maxCol)
		for c := 0; c < maxCol; c++ {
			if cd, ok := slots[[2]int{r, c}]; ok {
				row[c] = cd
			}
		}
		m.rows = append(m.rows, row)
		// Count a leading, contiguous block of header rows (from the top).
		if r < len(headerFlags) && headerFlags[r] && m.headerRows == r {
			m.headerRows = r + 1
		}
	}
	return m
}

// renderCell renders a cell box's content to inline Markdown, collapsing block
// structure inside the cell (multiple paragraphs joined by <br>) and escaping the pipe
// character so it does not break the table. header suppresses the UA bold on a <th> so
// the whole header text is not wrapped in "**".
func renderCell(cell *cssbox.Box, header bool) string {
	w := &writer{opts: Options{Logf: func(string, ...any) {}}}
	// Render each block child as a paragraph, then join with <br> (GFM's in-cell line
	// break). A cell that is a single inline run renders as one string.
	var parts []string
	if hasInlineContent(cell) {
		parts = append(parts, strings.TrimSpace(w.inlineOpt(cell, header)))
	} else {
		for _, c := range cell.Children {
			parts = append(parts, strings.TrimSpace(w.inlineOpt(c, header)))
		}
	}
	joined := strings.Join(filterEmpty(parts), " <br> ")
	return strings.ReplaceAll(joined, "|", `\|`)
}

// gfmTable renders the rectangular model as a GFM pipe table. The first row is used as
// the header (GFM requires a header row); if the model has no header-flagged rows, the
// first data row is promoted and the caller is told a header was synthesized only when
// the table genuinely had none.
func (w *writer) gfmTable(m tableModel) string {
	if m.cols == 0 {
		return ""
	}
	var sb strings.Builder
	headerRow := m.rows[0]
	bodyStart := 1
	if m.headerRows == 0 {
		w.opts.Logf("markdown table: no header row; promoting first row to GFM header")
	}
	writeRow(&sb, cellTexts(headerRow, m.cols))
	sb.WriteByte('\n')
	writeSeparator(&sb, headerRow, m.cols)
	for _, row := range m.rows[bodyStart:] {
		sb.WriteByte('\n')
		writeRow(&sb, cellTexts(row, m.cols))
	}
	return sb.String()
}

// writeRow writes "| a | b | c |" for the given cell texts.
func writeRow(sb *strings.Builder, texts []string) {
	sb.WriteString("|")
	for _, t := range texts {
		sb.WriteString(" ")
		if t == "" {
			sb.WriteString(" ")
		} else {
			sb.WriteString(t + " ")
		}
		sb.WriteString("|")
	}
}

// writeSeparator writes the GFM header/body separator, encoding per-column alignment
// from the header cells' text-align (":---", ":---:", "---:").
func writeSeparator(sb *strings.Builder, header []cellData, cols int) {
	sb.WriteString("|")
	for c := 0; c < cols; c++ {
		align := ""
		if c < len(header) {
			align = header[c].align
		}
		sb.WriteString(" " + separatorFor(align) + " |")
	}
}

// separatorFor maps a text-align keyword to its GFM separator token. "left" is the
// default alignment, so it maps to a plain "---" (an unstyled column) rather than
// ":---"; every cell inherits "left" by default and we do not want to emit an explicit
// left-alignment marker on every table.
func separatorFor(align string) string {
	switch align {
	case "center":
		return ":---:"
	case "right":
		return "---:"
	default:
		return "---"
	}
}

// plainTable renders the model as fixed-width space-padded columns (plain-text mode).
func (w *writer) plainTable(m tableModel) string {
	if m.cols == 0 {
		return ""
	}
	widths := make([]int, m.cols)
	for _, row := range m.rows {
		for c := 0; c < m.cols; c++ {
			if c < len(row) && len(row[c].text) > widths[c] {
				widths[c] = len(row[c].text)
			}
		}
	}
	var sb strings.Builder
	for r, row := range m.rows {
		if r > 0 {
			sb.WriteByte('\n')
		}
		for c := 0; c < m.cols; c++ {
			if c > 0 {
				sb.WriteString("  ")
			}
			t := ""
			if c < len(row) {
				t = row[c].text
			}
			sb.WriteString(t + strings.Repeat(" ", widths[c]-len(t)))
		}
	}
	return strings.TrimRight(sb.String(), " ")
}

// captionText renders a table's caption box (DisplayTableCaption) as a bold line, or
// "" if the table has no caption.
func (w *writer) captionText(table *cssbox.Box) string {
	for _, c := range table.Children {
		if c.Display == cssbox.DisplayTableCaption {
			t := strings.TrimSpace(w.inline(c))
			if t == "" {
				return ""
			}
			if w.opts.Plain {
				return t
			}
			return "**" + t + "**"
		}
	}
	return ""
}

// collectRows returns the table's rows in document order (descending through row
// groups) along with a parallel slice flagging which rows are header rows (a row inside
// a DisplayTableHeaderGroup, or whose cells are all header cells).
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
// font-weight:bold from the UA sheet; that alone is ambiguous, so we treat a cell as a
// header only when it is bold (the UA <th> signal). This is the pragmatic detector; a
// dedicated SemTag for <th> is a possible refinement.
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

// cellTexts extracts the text of a row's cells, padded to cols with empty strings.
func cellTexts(row []cellData, cols int) []string {
	out := make([]string, cols)
	for c := 0; c < cols; c++ {
		if c < len(row) {
			out[c] = row[c].text
		}
	}
	return out
}

// joinNonEmpty joins non-empty parts with a blank line between them.
func joinNonEmpty(parts ...string) string {
	return strings.Join(filterEmpty(parts), "\n\n")
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
