package css

import (
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
)

func autoLine() gcss.GridLine      { return gcss.GridLine{Kind: gcss.LineAuto} }
func numLine(n int) gcss.GridLine  { return gcss.GridLine{Kind: gcss.LineNum, N: n} }
func spanLine(n int) gcss.GridLine { return gcss.GridLine{Kind: gcss.LineSpan, N: n} }

// autoItem builds a placementInput whose four endpoints are all `auto` (so it is
// fully auto-placed).
func autoItem() placementInput {
	return placementInput{placement: gcss.GridPlacement{
		ColStart: autoLine(), ColEnd: autoLine(),
		RowStart: autoLine(), RowEnd: autoLine(),
	}}
}

func TestPlaceExplicitLines(t *testing.T) {
	// grid-column: 1 / 3, grid-row: 1 / 2 => cols [0,2), rows [0,1).
	in := placementInput{placement: gcss.GridPlacement{
		ColStart: numLine(1), ColEnd: numLine(3),
		RowStart: numLine(1), RowEnd: numLine(2),
	}}
	areas, cols, rows := placeItems([]placementInput{in}, gridDims{explicitCols: 2, explicitRows: 1}, "row")
	a := areas[0]
	if a.colStart != 0 || a.colEnd != 2 || a.rowStart != 0 || a.rowEnd != 1 {
		t.Fatalf("area = %+v want cols[0,2) rows[0,1)", a)
	}
	if cols != 2 || rows != 1 {
		t.Fatalf("dims = %d,%d want 2,1", cols, rows)
	}
}

func TestPlaceAutoRowFlow(t *testing.T) {
	// 2-col grid, three auto items, row flow => (0,0),(1,0),(0,1).
	items := []placementInput{autoItem(), autoItem(), autoItem()}
	areas, cols, rows := placeItems(items, gridDims{explicitCols: 2, explicitRows: 1}, "row")
	want := []gridArea{{0, 1, 0, 1}, {1, 2, 0, 1}, {0, 1, 1, 2}}
	for i, w := range want {
		if areas[i] != w {
			t.Errorf("item %d = %+v want %+v", i, areas[i], w)
		}
	}
	if cols != 2 || rows != 2 {
		t.Errorf("dims = %d,%d want 2,2 (implicit row added)", cols, rows)
	}
}

func TestPlaceAutoColumnFlow(t *testing.T) {
	// 1-col explicit, 2 explicit rows, three auto items, column flow => (0,0),(0,1),(1,0).
	items := []placementInput{autoItem(), autoItem(), autoItem()}
	areas, cols, _ := placeItems(items, gridDims{explicitCols: 1, explicitRows: 2}, "column")
	want := []gridArea{{0, 1, 0, 1}, {0, 1, 1, 2}, {1, 2, 0, 1}}
	for i, w := range want {
		if areas[i] != w {
			t.Errorf("item %d = %+v want %+v", i, areas[i], w)
		}
	}
	if cols != 2 {
		t.Errorf("cols=%d want 2 (implicit column added)", cols)
	}
}

// spanItem builds a placementInput that spans n columns (auto otherwise).
func spanColItem(n int) placementInput {
	return placementInput{placement: gcss.GridPlacement{
		ColStart: spanLine(n), ColEnd: autoLine(),
		RowStart: autoLine(), RowEnd: autoLine(),
	}}
}

// TestPlaceDenseVsSparse is a genuine dense-vs-sparse divergence. In a 3-col grid
// (row flow): item A spans 2 cols, item B spans 2 cols, item C is 1x1.
//
//	SPARSE trace (cursor persists):
//	  A span2: cursor (r0,c0) -> place cols[0,2) row0; cursor -> (r0,c2).
//	  B span2: cursor (r0,c2), c2+2=4 > 3 -> wrap to row1; place cols[0,2) row1; cursor -> (r1,c2).
//	  C 1x1:   cursor (r1,c2), c2+1=3 <= 3, free -> place cols[2,3) row1.
//	  => C = {2,3,1,2}. The hole at (row0,col2) is NEVER revisited.
//
//	DENSE trace (cursor resets to origin per item):
//	  A span2: reset (r0,c0) -> place cols[0,2) row0.
//	  B span2: reset (r0,c0); cannot fit span2 anywhere in row0 (col0/col1 occupied,
//	           col1 start would overlap) -> row1; place cols[0,2) row1.
//	  C 1x1:   reset (r0,c0); col0 occ, col1 occ, col2 FREE -> place cols[2,3) row0.
//	  => C = {2,3,0,1}. Dense BACKFILLS the row-0 hole.
//
// The divergence is item C: sparse {2,3,1,2} vs dense {2,3,0,1}.
func TestPlaceDenseVsSparse(t *testing.T) {
	items := []placementInput{spanColItem(2), spanColItem(2), autoItem()}
	dims := gridDims{explicitCols: 3, explicitRows: 1}

	sparse, _, _ := placeItems(items, dims, "row")
	dense, _, _ := placeItems(items, dims, "row dense")

	// A and B agree in both modes.
	wantAB := []gridArea{{0, 2, 0, 1}, {0, 2, 1, 2}}
	for i, w := range wantAB {
		if sparse[i] != w {
			t.Errorf("sparse item %d = %+v want %+v", i, sparse[i], w)
		}
		if dense[i] != w {
			t.Errorf("dense item %d = %+v want %+v", i, dense[i], w)
		}
	}

	wantSparseC := gridArea{2, 3, 1, 2}
	wantDenseC := gridArea{2, 3, 0, 1}
	if sparse[2] != wantSparseC {
		t.Errorf("sparse item C = %+v want %+v (skips the hole)", sparse[2], wantSparseC)
	}
	if dense[2] != wantDenseC {
		t.Errorf("dense item C = %+v want %+v (backfills the hole)", dense[2], wantDenseC)
	}
	if sparse[2] == dense[2] {
		t.Fatalf("dense and sparse produced the SAME rect %+v — not distinguishable", sparse[2])
	}
}

func TestGridAreaSpans(t *testing.T) {
	// colSpan/rowSpan report the half-open track widths (the contract Task 7 reads).
	a := gridArea{colStart: 1, colEnd: 4, rowStart: 2, rowEnd: 3}
	if a.colSpan() != 3 {
		t.Errorf("colSpan = %d want 3", a.colSpan())
	}
	if a.rowSpan() != 1 {
		t.Errorf("rowSpan = %d want 1", a.rowSpan())
	}
}

func TestPlaceNamedArea(t *testing.T) {
	areas := gcss.GridAreas{
		Named: map[string]gcss.GridRect{"hd": {RowStart: 1, RowEnd: 1, ColStart: 1, ColEnd: 2}},
		Rows:  2, Cols: 2,
	}
	in := placementInput{placement: gcss.GridPlacement{AreaName: "hd"}}
	got, _, _ := placeItems([]placementInput{in}, gridDims{explicitCols: 2, explicitRows: 2, areas: areas}, "row")
	if got[0] != (gridArea{0, 2, 0, 1}) {
		t.Fatalf("named area = %+v want cols[0,2) rows[0,1)", got[0])
	}
}

// TestPlaceLockedCrossWithSpan exercises the locked-cross-axis branch of autoPlace:
// an item with a definite ROW line (row 2 -> index 1) but an auto column with a span.
// The row is locked; the column auto-places at the first free flow position (col 0)
// with span 2 -> cols[0,2) on row index 1.
func TestPlaceLockedCrossWithSpan(t *testing.T) {
	in := placementInput{placement: gcss.GridPlacement{
		ColStart: spanLine(2), ColEnd: autoLine(),
		RowStart: numLine(2), RowEnd: autoLine(), // row line 2 => row index 1
	}}
	got, _, rows := placeItems([]placementInput{in}, gridDims{explicitCols: 3, explicitRows: 1}, "row")
	if got[0] != (gridArea{0, 2, 1, 2}) {
		t.Fatalf("locked-row span-2 item = %+v want cols[0,2) row[1,2)", got[0])
	}
	if rows < 2 {
		t.Errorf("rows = %d want >= 2 (implicit row created for the locked row index 1)", rows)
	}
}

// TestPlaceMultipleLockedSameRow exercises the locked branch packing several items
// onto one cross line: three 1x1 items all locked to row 1 (row index 0) in a 1-col
// grid pack left-to-right, growing implicit columns.
func TestPlaceMultipleLockedSameRow(t *testing.T) {
	lockRow1 := func() placementInput {
		return placementInput{placement: gcss.GridPlacement{
			ColStart: autoLine(), ColEnd: autoLine(),
			RowStart: numLine(1), RowEnd: autoLine(), // row line 1 => row index 0
		}}
	}
	items := []placementInput{lockRow1(), lockRow1(), lockRow1()}
	got, cols, _ := placeItems(items, gridDims{explicitCols: 1, explicitRows: 1}, "row")
	want := []gridArea{{0, 1, 0, 1}, {1, 2, 0, 1}, {2, 3, 0, 1}}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("locked item %d = %+v want %+v", i, got[i], w)
		}
	}
	if cols < 3 {
		t.Errorf("cols = %d want >= 3 (implicit columns grown for the packed locked row)", cols)
	}
}
