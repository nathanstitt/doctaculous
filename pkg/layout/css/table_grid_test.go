package css

import (
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

func cell(colSpan, rowSpan int) *cssbox.Box {
	return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableCell,
		Formatting: cssbox.BlockFC, ColSpan: colSpan, RowSpan: rowSpan}
}
func rowOf(cells ...*cssbox.Box) *cssbox.Box {
	return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRow,
		Formatting: cssbox.TableFC, Children: cells}
}
func groupOf(d cssbox.DisplayKind, rows ...*cssbox.Box) *cssbox.Box {
	return &cssbox.Box{Kind: cssbox.BoxBlock, Display: d, Formatting: cssbox.TableFC, Children: rows}
}
func tableOf(kids ...*cssbox.Box) *cssbox.Box {
	return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTable, Formatting: cssbox.TableFC, Children: kids}
}

func TestGridColumnCountFromColspan(t *testing.T) {
	tbl := tableOf(groupOf(cssbox.DisplayTableRowGroup,
		rowOf(cell(2, 1), cell(1, 1)), // 3 columns
		rowOf(cell(1, 1), cell(1, 1), cell(1, 1)),
	))
	g := buildGrid(tbl)
	if len(g.cols) != 3 {
		t.Fatalf("want 3 columns, got %d", len(g.cols))
	}
	if len(g.rows) != 2 {
		t.Fatalf("want 2 rows, got %d", len(g.rows))
	}
}

func TestGridRowspanReservesLowerSlot(t *testing.T) {
	// Row 0: A(rowspan 2) at col 0, B at col 1. Row 1: C must land at col 1.
	tbl := tableOf(groupOf(cssbox.DisplayTableRowGroup,
		rowOf(cell(1, 2), cell(1, 1)),
		rowOf(cell(1, 1)),
	))
	g := buildGrid(tbl)
	var c *gridCell
	for _, gc := range g.cells {
		if gc.row == 1 {
			c = gc
		}
	}
	if c == nil {
		t.Fatal("no cell originating in row 1")
	}
	if c.col != 1 {
		t.Fatalf("row-1 cell should be pushed to col 1 by the rowspan above; got col %d", c.col)
	}
}

func TestGridHeaderFooterVisualOrder(t *testing.T) {
	// Document order: tbody, then thead, then tfoot. Visual order: thead, tbody, tfoot.
	tbl := tableOf(
		groupOf(cssbox.DisplayTableRowGroup, rowOf(cell(1, 1))),    // body  -> visual row 1
		groupOf(cssbox.DisplayTableHeaderGroup, rowOf(cell(1, 1))), // head  -> visual row 0
		groupOf(cssbox.DisplayTableFooterGroup, rowOf(cell(1, 1))), // foot  -> visual row 2
	)
	g := buildGrid(tbl)
	if len(g.rows) != 3 {
		t.Fatalf("want 3 rows, got %d", len(g.rows))
	}
	if g.rows[0].box != tbl.Children[1].Children[0] {
		t.Errorf("header row not placed first in visual order")
	}
	if g.rows[2].box != tbl.Children[2].Children[0] {
		t.Errorf("footer row not placed last in visual order")
	}
}

func TestGridColspanClamp(t *testing.T) {
	tbl := tableOf(groupOf(cssbox.DisplayTableRowGroup, rowOf(cell(9, 1))))
	g := buildGrid(tbl)
	if len(g.cols) != 9 {
		t.Fatalf("want 9 columns from a colspan-9 cell, got %d", len(g.cols))
	}
	if g.cells[0].colSpan != 9 {
		t.Fatalf("colSpan should be 9, got %d", g.cells[0].colSpan)
	}
}
