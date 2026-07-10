package boxwalk

import (
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

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

// CellPlainText renders a cell's content as one plain-text field, mirroring
// the Markdown writer's renderCell: each block child renders to a collapsed
// inline string (runs concatenated without separators, so styled runs inside a
// word do not split it), then blocks join with a space. An image contributes
// its alt text.
func CellPlainText(cell *cssbox.Box) string {
	var parts []string
	if HasInlineContent(cell) {
		parts = append(parts, inlinePlain(cell))
	} else {
		for _, c := range cell.Children {
			parts = append(parts, inlinePlain(c))
		}
	}
	return strings.Join(FilterEmpty(parts), " ")
}

// inlinePlain flattens a box's inline subtree to collapsed plain text.
func inlinePlain(b *cssbox.Box) string {
	var runs []StyledRun
	CollectRuns(b, InlineState{}, func(rc *cssbox.ReplacedContent) string {
		return rc.Attrs["alt"]
	}, &runs)
	var sb strings.Builder
	for _, r := range runs {
		if r.Literal != "" {
			sb.WriteString(r.Literal)
			continue
		}
		sb.WriteString(r.Text)
	}
	return CollapseSpaces(sb.String())
}

// CollectTables gathers a tree's DisplayTable boxes in document order, not
// descending into a found table (a nested table's text is already part of its
// host cell). It also counts the non-table content blocks a tables-only writer
// (CSV, XLSX) drops, for the degradation log.
func CollectTables(root *cssbox.Box) (tables []*cssbox.Box, droppedBlocks int) {
	var walk func(b *cssbox.Box)
	walk = func(b *cssbox.Box) {
		if b.Display == cssbox.DisplayTable {
			tables = append(tables, b)
			return
		}
		if IsBlockContainer(b) && HasInlineContent(b) {
			droppedBlocks++
			return
		}
		for _, c := range b.Children {
			walk(c)
		}
	}
	if root != nil {
		walk(root)
	}
	return tables, droppedBlocks
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
