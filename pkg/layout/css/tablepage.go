package css

import (
	"sort"

	"github.com/nathanstitt/doctaculous/pkg/layout"
)

// splitTableForPage splits a table fragment BETWEEN rows at pageBottom. The CSS table
// engine emits the table's cells directly as the table fragment's children (positioned by
// the grid), so a "row" is recovered as a vertical band of cells sharing a top Y. Bands
// whose bottom is fully above pageBottom stay in the head table; the rest (whole bands)
// move to the tail table (its Y shifted to the first kept band, top border suppressed).
// Mid-cell content is NOT split — a row (and any cell spanning rows) rides one page.
// Returns {head:tbl} if all rows fit, {tail:tbl} if the first row alone overflows the
// page from the top.
func splitTableForPage(tbl *Fragment, pageBottom float64) splitResult {
	rows := tableRowBands(tbl)
	if len(rows) <= 1 {
		// A single row (or no rows) cannot be split between rows: it either fits or
		// overflows whole.
		if tbl.Y+tbl.H <= pageBottom+0.5 {
			return splitResult{head: tbl}
		}
		return splitResult{tail: tbl}
	}
	k := 0
	for i, r := range rows {
		if r.bottom <= pageBottom+0.5 {
			k = i + 1
		} else {
			break
		}
	}
	if k >= len(rows) {
		return splitResult{head: tbl}
	}
	if k == 0 {
		return splitResult{tail: tbl} // first row taller than the page: move whole, overflow
	}
	splitY := rows[k].top
	head := *tbl
	tail := *tbl
	head.Children = cellsAbove(tbl, splitY)
	tail.Children = cellsFrom(tbl, splitY)
	head.H = rows[k-1].bottom - tbl.Y
	tail.Y = splitY
	tail.H = (tbl.Y + tbl.H) - splitY
	head.Border[layout.EdgeBottom] = BorderEdge{}
	tail.Border[layout.EdgeTop] = BorderEdge{}
	// Collapsed border strips, if any, are dropped on the split (a documented limitation —
	// the collapsed grid is computed table-wide and is not re-derived per page fragment).
	head.Collapsed, tail.Collapsed = nil, nil
	return splitResult{head: &head, tail: &tail}
}

// tableRow is one recovered table row: the vertical extent of a band of cells that share
// a top Y (a row in the grid). A cell spanning multiple rows extends its band's bottom,
// so a rowspanning cell keeps its rows together (it is not cut).
type tableRow struct{ top, bottom float64 }

// tableRowBands groups a table fragment's in-flow cell children into vertical row bands,
// merging cells whose Y-extents overlap (so a rowspanning cell joins the bands it covers),
// sorted top-to-bottom. Out-of-flow children (floats/positioned) are ignored.
func tableRowBands(tbl *Fragment) []tableRow {
	var bands []tableRow
	for _, c := range inFlowChildren(tbl) {
		t, b := c.Y, c.Y+c.H
		merged := false
		for i := range bands {
			if t < bands[i].bottom-0.5 && b > bands[i].top+0.5 { // overlap → same band
				if t < bands[i].top {
					bands[i].top = t
				}
				if b > bands[i].bottom {
					bands[i].bottom = b
				}
				merged = true
				break
			}
		}
		if !merged {
			bands = append(bands, tableRow{top: t, bottom: b})
		}
	}
	sort.Slice(bands, func(i, j int) bool { return bands[i].top < bands[j].top })
	return bands
}

// cellsAbove returns the table's child cells whose top is above splitY (the head page's
// rows). Out-of-flow children ride the head (they were positioned in the table's space).
func cellsAbove(tbl *Fragment, splitY float64) []*Fragment {
	var out []*Fragment
	for _, c := range tbl.Children {
		if c.Y < splitY-0.5 {
			out = append(out, c)
		}
	}
	return out
}

// cellsFrom returns the table's child cells whose top is at or below splitY (the tail
// page's rows).
func cellsFrom(tbl *Fragment, splitY float64) []*Fragment {
	var out []*Fragment
	for _, c := range tbl.Children {
		if c.Y >= splitY-0.5 {
			out = append(out, c)
		}
	}
	return out
}
