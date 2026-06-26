package css

import (
	"context"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

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
	box    *cssbox.Box // the DisplayTableRow box (real or anonymous)
	cells  []*gridCell // cells ORIGINATING in this row (not spanned into it)
	height float64     // resolved row height (tallest non-spanning cell)
	y      float64     // y-offset of the row's top edge in the table content box
}

type gridCol struct {
	hasWidth bool
	width    float64 // specified/hint width (px) when hasWidth
	pct      float64 // percentage width [0..100], or <0 when none
	x        float64 // x-offset of the column's left edge in the table content box
	min, max float64 // content min/max-content widths (auto layout)
}

type gridCell struct {
	box      *cssbox.Box
	row, col int // origin slot (top-left), 0-based
	rowSpan  int
	colSpan  int
	frag     *Fragment // fragment produced by laying out this cell
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
			// defensive: a bare row with no group affiliation is treated as a body row
			// (fixup normally wraps these in a row-group first).
			bodyRows = append(bodyRows, c)
		}
	}
	visualRows := make([]*cssbox.Box, 0, len(headRows)+len(bodyRows)+len(footRows))
	visualRows = append(visualRows, headRows...)
	visualRows = append(visualRows, bodyRows...)
	visualRows = append(visualRows, footRows...)

	// 2. Occupancy scan: assign each cell to its origin slot.
	var occupied [][]bool
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
			// Ensure column `col` exists before the skip loop reads occupied[ri][col].
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
		// A percentage <col> width is not captured here; the width-solve step reads it
		// (later task). col.pct stays -1 for now.
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

// layoutTable is the TableFC entry point (called from layoutInterior). It builds the
// grid, solves column widths (fixed layout this slice), lays out + positions cells,
// and returns the interior (cell fragments in the local content-top-0 frame). A table
// establishes a BFC, so leading/trailing margins are 0. contentW is the table's
// content-box width; contentX the page-space left of that content box.
func (e *Engine) layoutTable(ctx context.Context, b *cssbox.Box, contentW, contentX, bandOriginY float64, fc *floatContext) interior {
	_ = bandOriginY // reserved for future use (float interactions)
	_ = fc          // reserved for future use (float interactions)
	g := buildGrid(b)
	if g.table.Style.Direction == "rtl" {
		e.logf("css layout: RTL tables not supported; laying out LTR")
	}
	if len(g.rows) == 0 || len(g.cols) == 0 {
		return interior{contentHeight: 0}
	}

	if g.fixed {
		e.solveFixedWidths(g, contentW)
	} else {
		e.solveAutoWidths(ctx, g, contentW)
	}

	// Column x-offsets (left content edge of each column), with border-spacing.
	x := g.spacingH
	for ci := range g.cols {
		g.cols[ci].x = x
		x += g.cols[ci].width + g.spacingH
	}

	// Lay out each cell at its column width; capture natural height.
	for _, gc := range g.cells {
		cw := g.cellWidth(gc)
		cellPos := &positionedContext{}
		res := e.layoutBlock(ctx, gc.box, cw, 0, 0, 0, &floatContext{cbLeft: 0, cbRight: cw}, cellPos, posCBOwner{isPage: true})
		gc.frag = res.frag
		// A cell is a BFC and its own positioned containing block; resolve any abs/fixed
		// descendants against the cell's provisional box now (mirroring the inline-block
		// atom path) so they are not silently dropped. (Positioning relative to the cell
		// is approximate here, like the inline-block atom case — exact cross-cell
		// positioning is out of scope.)
		if gc.frag != nil {
			e.resolveAbsolute(ctx, cellPos, gc.frag, cw, gc.frag.H)
		}
	}

	// Row natural heights = tallest non-spanning cell originating in the row.
	for _, gr := range g.rows {
		h := 0.0
		for _, gc := range gr.cells {
			if gc.rowSpan == 1 && gc.frag != nil && gc.frag.H > h {
				h = gc.frag.H
			}
		}
		gr.height = h
	}

	// Row y-offsets with vertical spacing.
	y := g.spacingV
	for _, gr := range g.rows {
		gr.y = y
		y += gr.height + g.spacingV
	}
	tableContentH := y

	// Position cells: a cell fills its column(s) × row(s) rectangle.
	var children []*Fragment
	for _, gc := range g.cells {
		if gc.frag == nil {
			continue
		}
		cx := contentX + g.cols[gc.col].x
		cy := g.rows[gc.row].y
		cw := g.cellWidth(gc)
		ch := gc.rowBandHeight(g)
		stretchCellFragment(gc.frag, cx, cy, cw, ch)
		children = append(children, gc.frag)
	}

	return interior{
		children:       children,
		contentHeight:  tableContentH,
		leadingMargin:  0,
		trailingMargin: 0,
	}
}

// cellWidth returns the column-band width allocated to a cell: the sum of its spanned
// column widths plus the inter-column border-spacing it spans over. This is passed to
// layoutBlock as the cell's containing-block width (layoutBlock derives the cell's own
// border/content box from it).
func (g *tableGrid) cellWidth(gc *gridCell) float64 {
	w := 0.0
	for i := 0; i < gc.colSpan; i++ {
		w += g.cols[gc.col+i].width
	}
	w += float64(gc.colSpan-1) * g.spacingH
	return w
}

// rowBandHeight is the total height a cell spans = sum of its rows' heights + the
// inter-row border-spacing between them.
func (gc *gridCell) rowBandHeight(g *tableGrid) float64 {
	h := 0.0
	for i := 0; i < gc.rowSpan; i++ {
		h += g.rows[gc.row+i].height
	}
	h += float64(gc.rowSpan-1) * g.spacingV
	return h
}

// solveFixedWidths implements CSS 17.5.2.1: column widths from the first row's cells
// + <col> hints + the table width, content-independent. The table used content width
// fills its container (width:auto), or grows to the column sum if larger. Auto columns
// split the leftover equally.
func (e *Engine) solveFixedWidths(g *tableGrid, contentW float64) {
	used := contentW
	if len(g.rows) > 0 {
		for _, gc := range g.rows[0].cells {
			if w, ok := specifiedFixedWidth(gc.box); ok {
				per := w / float64(gc.colSpan)
				for i := 0; i < gc.colSpan; i++ {
					if !g.cols[gc.col+i].hasWidth {
						g.cols[gc.col+i].hasWidth = true
						g.cols[gc.col+i].width = per
					}
				}
			}
		}
	}
	fixedSum := 0.0
	autoCount := 0
	for ci := range g.cols {
		if g.cols[ci].hasWidth {
			fixedSum += g.cols[ci].width
		} else {
			autoCount++
		}
	}
	spacing := g.spacingH * float64(len(g.cols)+1)
	remain := used - fixedSum - spacing
	if remain < 0 {
		remain = 0
	}
	if autoCount > 0 {
		per := remain / float64(autoCount)
		for ci := range g.cols {
			if !g.cols[ci].hasWidth {
				g.cols[ci].width = per
			}
		}
	} else if remain > 0 && fixedSum > 0 {
		for ci := range g.cols {
			g.cols[ci].width += remain * (g.cols[ci].width / fixedSum)
		}
	}
}

// solveAutoWidths implements CSS 17.5.2.2 (automatic table layout): per-column
// min/max content widths from non-spanning cells, the table used width, and a
// distribution of that width across columns between their min and max. Spanning-cell
// contributions (distributeSpanWidths) and percentage columns are added by later
// tasks; this handles the non-spanning, non-percentage common case.
func (e *Engine) solveAutoWidths(ctx context.Context, g *tableGrid, contentW float64) {
	for ci := range g.cols {
		g.cols[ci].min = 0
		g.cols[ci].max = 0
	}
	for _, gc := range g.cells {
		if gc.colSpan != 1 {
			continue
		}
		mn := e.measureMinContent(ctx, gc.box) + horizontalEdges(gc.box)
		mx := e.measureMaxContent(ctx, gc.box) + horizontalEdges(gc.box)
		if w, ok := specifiedFixedWidth(gc.box); ok {
			ew := w + horizontalEdges(gc.box)
			if ew > mn {
				mn = ew
			}
			mx = ew
			if mx < mn {
				mx = mn
			}
		}
		col := &g.cols[gc.col]
		if mn > col.min {
			col.min = mn
		}
		if mx > col.max {
			col.max = mx
		}
		if col.hasWidth && col.width > col.min {
			col.min = col.width
			if col.max < col.min {
				col.max = col.min
			}
		}
	}
	e.distributeSpanWidths(ctx, g) // Task 9; no-op until then

	for ci := range g.cols {
		if g.cols[ci].max < g.cols[ci].min {
			g.cols[ci].max = g.cols[ci].min
		}
	}

	spacing := g.spacingH * float64(len(g.cols)+1)
	sumMin, sumMax := 0.0, 0.0
	for ci := range g.cols {
		sumMin += g.cols[ci].min
		sumMax += g.cols[ci].max
	}
	avail := contentW - spacing
	if avail < 0 {
		avail = 0
	}
	var used float64
	if w, ok := specifiedFixedWidth(g.table); ok {
		used = w - spacing
	} else {
		used = sumMax
		if used > avail {
			used = avail
		}
	}
	if used < sumMin {
		used = sumMin
	}

	if used <= sumMin || sumMax == sumMin {
		for ci := range g.cols {
			g.cols[ci].width = g.cols[ci].min
		}
		// sumMax == sumMin (every column fully pinned): no proportional flex room exists,
		// so any surplus is split equally rather than by the (zero) flex ratio.
		if used > sumMin && len(g.cols) > 0 {
			extra := (used - sumMin) / float64(len(g.cols))
			for ci := range g.cols {
				g.cols[ci].width += extra
			}
		}
		return
	}
	// Distribute the surplus (used − sumMin) across columns in proportion to each
	// column's flex room (max − min). This conserves exactly:
	//   Σ(min + surplus·span/flex) = sumMin + surplus·(Σspan)/flex
	//                              = sumMin + surplus·flex/flex = sumMin + surplus = used,
	// so the column widths always sum to the table's used width. flex > 0 here because
	// this path is only reached when sumMax != sumMin.
	surplus := used - sumMin
	flex := sumMax - sumMin
	for ci := range g.cols {
		span := g.cols[ci].max - g.cols[ci].min
		g.cols[ci].width = g.cols[ci].min + surplus*(span/flex)
	}
}

// distributeSpanWidths adds spanning cells' min/max contributions to the columns
// they cross (CSS 17.5.2.2). Implemented in a later task; until then spanning cells
// do not influence column widths (they lay out at the summed column widths regardless).
func (e *Engine) distributeSpanWidths(ctx context.Context, g *tableGrid) {
	_ = ctx
	_ = g
}

// stretchCellFragment positions a cell's border-box fragment at (x,y) and stretches
// it to (w,h) — table cells fill their row band. It translates the fragment's whole
// subtree so its top-left lands at (x,y), then sets the border box to (w,h). Content
// keeps its top-left origin (top-aligned for now; vertical-align is a later task).
func stretchCellFragment(f *Fragment, x, y, w, h float64) {
	translateFragment(f, x-f.X, y-f.Y)
	f.W = w
	f.H = h
}
