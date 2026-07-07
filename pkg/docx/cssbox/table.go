package cssbox

import (
	"image/color"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/docx"
	"github.com/nathanstitt/doctaculous/pkg/docx/style"
	lcssbox "github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// lowerTable lowers a DOCX table into a cssbox DisplayTable subtree the CSS table
// layout engine consumes. Rows become DisplayTableRow boxes; cells become
// DisplayTableCell boxes carrying ColSpan (w:gridSpan) and RowSpan (derived from
// w:vMerge). Cell content lowers recursively via lowerBlocks. Borders, shading,
// and width map onto the table/cell ComputedStyle.
func lowerTable(tb *docx.Table, r *style.Resolver, num *docx.Numbering, rels map[string]docx.Relationship) *lcssbox.Box {
	table := &lcssbox.Box{
		Kind: lcssbox.BoxBlock, Display: lcssbox.DisplayTable, Formatting: lcssbox.TableFC,
		Style: tableStyle(tb.Props, tb.Grid),
	}
	rowSpans := computeRowSpans(tb)
	for ri, row := range tb.Rows {
		rowBox := &lcssbox.Box{
			Kind: lcssbox.BoxBlock, Display: lcssbox.DisplayTableRow, Formatting: lcssbox.TableFC,
			Style: gcss.InitialStyle(),
		}
		for ci, cell := range row.Cells {
			span := cell.GridSpan
			if span < 1 {
				span = 1
			}
			if cell.VMerge == docx.VMergeContinue {
				// Covered by the restart cell's RowSpan; drop it.
				continue
			}
			cellBox := &lcssbox.Box{
				Kind: lcssbox.BoxBlock, Display: lcssbox.DisplayTableCell, Formatting: lcssbox.BlockFC,
				Style:   cellStyle(cell.Props),
				ColSpan: span,
				RowSpan: rowSpans[cellKey{ri, ci}],
			}
			cellBox.Children = lowerBlocks(cell.Blocks, r, num, rels, newListCounter())
			rowBox.Children = append(rowBox.Children, cellBox)
		}
		table.Children = append(table.Children, rowBox)
	}
	return table
}

// cellKey identifies a cell by (rowIndex, cellIndexInRow) for the rowspan map.
type cellKey struct{ row, cell int }

// computeRowSpans resolves each restart cell's RowSpan = 1 + the number of
// continue cells directly below it in the same visual grid column. It tracks the
// running grid column of each cell (honoring gridSpan) so a continue cell is
// matched to the restart above it by column, per OOXML vMerge semantics.
func computeRowSpans(tb *docx.Table) map[cellKey]int {
	spans := map[cellKey]int{}
	// active maps a grid column -> the (row,cell) of the restart cell currently
	// open in that column.
	active := map[int]cellKey{}
	for ri, row := range tb.Rows {
		col := 0
		for ci, cell := range row.Cells {
			span := cell.GridSpan
			if span < 1 {
				span = 1
			}
			switch cell.VMerge {
			case docx.VMergeRestart:
				active[col] = cellKey{ri, ci}
				spans[cellKey{ri, ci}] = 1
			case docx.VMergeContinue:
				if k, ok := active[col]; ok {
					spans[k]++
				}
			}
			col += span
		}
	}
	return spans
}

// tableStyle maps table-level props onto a block ComputedStyle: width (dxa ->
// pt), border-collapse (DOCX tables collapse borders like Word's default grid),
// the four table borders, and background shading.
func tableStyle(p docx.TableProps, grid []docx.Twips) gcss.ComputedStyle {
	cs := gcss.InitialStyle()
	cs.Display = "table"
	cs.BorderCollapse = "collapse"
	switch {
	case p.WidthDxa > 0:
		cs.Width = pt(p.WidthDxa.Points())
	case p.WidthPct > 0:
		cs.Width = gcss.Length{Value: float64(p.WidthPct) / 50, Unit: gcss.UnitPercent}
	}
	applyBorders(&cs, p.Borders)
	if p.Shading.HasFill {
		cs.BackgroundColor = p.Shading.Fill
	}
	return cs
}

// cellStyle maps cell props onto a block ComputedStyle: width, vertical-align,
// borders, and shading.
func cellStyle(p docx.CellProps) gcss.ComputedStyle {
	cs := gcss.InitialStyle()
	cs.Display = "table-cell"
	if p.WidthDxa > 0 {
		cs.Width = pt(p.WidthDxa.Points())
	}
	cs.VerticalAlign = vAlignString(p.VAlign)
	applyBorders(&cs, p.Borders)
	if p.Shading.HasFill {
		cs.BackgroundColor = p.Shading.Fill
	}
	return cs
}

// applyBorders maps a BoxBorders onto the per-edge ComputedStyle border fields.
// A no-border edge is left at the initial "none"; an edge with a size becomes a
// solid border whose width is sz/8 pt (OOXML w:sz is eighths of a point).
func applyBorders(cs *gcss.ComputedStyle, b docx.BoxBorders) {
	applyEdge(&cs.BorderTopWidth, &cs.BorderTopStyle, &cs.BorderTopColor, b.Top)
	applyEdge(&cs.BorderBottomWidth, &cs.BorderBottomStyle, &cs.BorderBottomColor, b.Bottom)
	applyEdge(&cs.BorderLeftWidth, &cs.BorderLeftStyle, &cs.BorderLeftColor, b.Left)
	applyEdge(&cs.BorderRightWidth, &cs.BorderRightStyle, &cs.BorderRightColor, b.Right)
}

func applyEdge(width *gcss.Length, style *string, col *color.RGBA, e docx.Border) {
	if e.None || e.SizeEighthPt == 0 {
		return
	}
	*width = pt(float64(e.SizeEighthPt) / 8)
	*style = "solid"
	if e.HasColor {
		*col = e.Color
	}
}

// vAlignString maps a CellVAlign onto the CSS vertical-align keyword.
func vAlignString(v docx.CellVAlign) string {
	switch v {
	case docx.VAlignCenter:
		return "middle"
	case docx.VAlignBottom:
		return "bottom"
	default:
		return "top"
	}
}
