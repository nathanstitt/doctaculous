package boxwalk

import "github.com/nathanstitt/doctaculous/pkg/layout/cssbox"

// GridCell is one origin cell in an occupancy grid: the cell box, its origin
// slot, and the span it covers.
type GridCell struct {
	Box              *cssbox.Box
	Row, Col         int
	ColSpan, RowSpan int
	Header           bool
}

// Grid is a table's rectangular occupancy grid with origin identity preserved —
// the model for writers whose format expresses spans natively (DOCX gridSpan/
// vMerge, HTML colspan/rowspan), as opposed to the Markdown writer's
// duplicate-content expansion. Slots[r][c] indexes into Cells for the cell
// covering that slot (origin or continuation), or -1 for a gap (a short row).
type Grid struct {
	Cols      int
	Cells     []GridCell
	Slots     [][]int
	HeaderRow []bool // per row: a header row (from a header row group or all-header cells)
}

// BuildOccupancyGrid walks a DisplayTable subtree into a Grid. It mirrors the
// occupancy scan the layout table builder and the Markdown writer use: origin
// cells advance to the next free column in their row, then stamp every covered
// slot.
func BuildOccupancyGrid(table *cssbox.Box) Grid {
	rowBoxes, headerFlags := CollectRows(table)
	type slotKey struct{ r, c int }
	slots := map[slotKey]int{}
	var cells []GridCell
	maxCol := 0
	for r, rowBox := range rowBoxes {
		col := 0
		for _, cellBox := range CellBoxesOf(rowBox) {
			for {
				if _, taken := slots[slotKey{r, col}]; !taken {
					break
				}
				col++
			}
			colSpan := ClampSpan(cellBox.ColSpan)
			rowSpan := ClampSpan(cellBox.RowSpan)
			idx := len(cells)
			cells = append(cells, GridCell{
				Box: cellBox, Row: r, Col: col,
				ColSpan: colSpan, RowSpan: rowSpan,
				Header: headerFlags[r] || IsHeaderCell(cellBox),
			})
			for rs := 0; rs < rowSpan; rs++ {
				for cs := 0; cs < colSpan; cs++ {
					slots[slotKey{r + rs, col + cs}] = idx
				}
			}
			col += colSpan
			if col > maxCol {
				maxCol = col
			}
		}
	}

	nRows := len(rowBoxes)
	for key := range slots {
		if key.r+1 > nRows {
			nRows = key.r + 1
		}
	}

	g := Grid{Cols: maxCol, Cells: cells}
	for r := 0; r < nRows; r++ {
		row := make([]int, maxCol)
		for c := 0; c < maxCol; c++ {
			if idx, ok := slots[slotKey{r, c}]; ok {
				row[c] = idx
			} else {
				row[c] = -1
			}
		}
		g.Slots = append(g.Slots, row)
		g.HeaderRow = append(g.HeaderRow, r < len(headerFlags) && headerFlags[r])
	}
	return g
}
