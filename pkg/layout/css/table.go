package css

import "github.com/nathanstitt/doctaculous/pkg/layout/cssbox"

// tableGrid is the private intermediate the table layout algorithm operates on: the
// row/column grid recovered from a fixed-up table box, the single source of truth
// for the width solve, height solve, and border resolution. It never escapes the
// layoutTable call.
type tableGrid struct {
	table    *cssbox.Box
	caption  *cssbox.Box
	rows     []*gridRow
	cols     []gridCol
	cells    []*gridCell
	collapse bool
	fixed    bool
	spacingH float64
	spacingV float64
}

type gridRow struct {
	box   *cssbox.Box // the DisplayTableRow box (real or anonymous)
	cells []*gridCell // cells ORIGINATING in this row (not spanned into it)
}

type gridCol struct {
	hasWidth bool
	width    float64 // specified/hint width (px) when hasWidth
	pct      float64 // percentage width [0..100], or <0 when none
}

type gridCell struct {
	box      *cssbox.Box
	row, col int // origin slot (top-left), 0-based
	rowSpan  int
	colSpan  int
}

// buildGrid recovers the grid from a fixed-up table box. It flattens row-groups in
// visual order (header groups, then body row-groups in document order, then footer
// groups), reads <col>/<colgroup> width hints, and assigns each cell to its origin
// slot with an occupancy scan honoring colspan/rowspan.
func buildGrid(tbl *cssbox.Box) *tableGrid {
	g := &tableGrid{
		table:    tbl,
		collapse: tbl.Style.BorderCollapse == "collapse",
		fixed:    tbl.Style.TableLayout == "fixed",
	}
	if !g.collapse {
		g.spacingH = tbl.Style.BorderSpacingH
		g.spacingV = tbl.Style.BorderSpacingV
	}

	// 1. Collect caption + column hints + rows (visual order).
	var headRows, bodyRows, footRows []*cssbox.Box
	collectRows := func(group *cssbox.Box) []*cssbox.Box {
		var rows []*cssbox.Box
		for _, c := range group.Children {
			if c.Display == cssbox.DisplayTableRow {
				rows = append(rows, c)
			}
		}
		return rows
	}
	for _, c := range tbl.Children {
		switch c.Display {
		case cssbox.DisplayTableCaption:
			if g.caption == nil {
				g.caption = c
			}
		case cssbox.DisplayTableColumn:
			g.addColumnHint(c)
		case cssbox.DisplayTableColumnGroup:
			cols := 0
			for _, cc := range c.Children {
				if cc.Display == cssbox.DisplayTableColumn {
					g.addColumnHint(cc)
					cols++
				}
			}
			if cols == 0 {
				g.addColumnHintN(c, spanOf(c))
			}
		case cssbox.DisplayTableHeaderGroup:
			headRows = append(headRows, collectRows(c)...)
		case cssbox.DisplayTableFooterGroup:
			footRows = append(footRows, collectRows(c)...)
		case cssbox.DisplayTableRowGroup:
			bodyRows = append(bodyRows, collectRows(c)...)
		case cssbox.DisplayTableRow:
			bodyRows = append(bodyRows, c) // defensive: a bare row (fixup normally wraps these)
		}
	}
	visualRows := make([]*cssbox.Box, 0, len(headRows)+len(bodyRows)+len(footRows))
	visualRows = append(visualRows, headRows...)
	visualRows = append(visualRows, bodyRows...)
	visualRows = append(visualRows, footRows...)

	// 2. Occupancy scan: assign each cell to its origin slot.
	occupied := [][]bool{}
	ensure := func(r, c int) {
		for len(occupied) <= r {
			occupied = append(occupied, make([]bool, len(g.cols)))
		}
		if c >= len(g.cols) {
			grow := c + 1 - len(g.cols)
			for i := 0; i < grow; i++ {
				g.cols = append(g.cols, gridCol{pct: -1})
			}
			for ri := range occupied {
				for len(occupied[ri]) < len(g.cols) {
					occupied[ri] = append(occupied[ri], false)
				}
			}
		}
	}
	for ri, rb := range visualRows {
		gr := &gridRow{box: rb}
		col := 0
		for _, cb := range rb.Children {
			if cb.Display != cssbox.DisplayTableCell {
				continue
			}
			ensure(ri, col)
			for col < len(g.cols) && occupied[ri][col] {
				col++
				ensure(ri, col)
			}
			cs := cb.ColSpan
			if cs < 1 {
				cs = 1
			}
			rs := cb.RowSpan
			if rs < 1 {
				rs = 1
			}
			gc := &gridCell{box: cb, row: ri, col: col, colSpan: cs, rowSpan: rs}
			g.cells = append(g.cells, gc)
			gr.cells = append(gr.cells, gc)
			for dr := 0; dr < rs; dr++ {
				for dc := 0; dc < cs; dc++ {
					ensure(ri+dr, col+dc)
					occupied[ri+dr][col+dc] = true
				}
			}
			col += cs
		}
		g.rows = append(g.rows, gr)
	}

	// 3. Clamp each cell's spans to the final grid extent.
	for _, gc := range g.cells {
		if gc.col+gc.colSpan > len(g.cols) {
			gc.colSpan = len(g.cols) - gc.col
			if gc.colSpan < 1 {
				gc.colSpan = 1
			}
		}
		if gc.row+gc.rowSpan > len(g.rows) {
			gc.rowSpan = len(g.rows) - gc.row
			if gc.rowSpan < 1 {
				gc.rowSpan = 1
			}
		}
	}
	return g
}

// addColumnHint appends one column carrying cb's width hint (a <col> width).
func (g *tableGrid) addColumnHint(cb *cssbox.Box) {
	g.addColumnHintN(cb, spanOf(cb))
}

// addColumnHintN appends n columns carrying cb's width hint.
func (g *tableGrid) addColumnHintN(cb *cssbox.Box, n int) {
	for i := 0; i < n; i++ {
		col := gridCol{pct: -1}
		if w, ok := specifiedFixedWidth(cb); ok {
			col.hasWidth = true
			col.width = w
		}
		g.cols = append(g.cols, col)
	}
}

// spanOf reads a <col>/<colgroup> span (ColSpan field, >=1).
func spanOf(cb *cssbox.Box) int {
	if cb.ColSpan < 1 {
		return 1
	}
	return cb.ColSpan
}
