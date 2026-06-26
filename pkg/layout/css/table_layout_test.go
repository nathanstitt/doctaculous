package css

import (
	"context"
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// fixedCell builds a table-cell with a fixed width+height so geometry is deterministic.
// MaxWidth and MaxHeight are set to UnitAuto ("none") so they don't clamp the fixed sizes.
func fixedCell(wPx, hPx float64) *cssbox.Box {
	st := gcss.ComputedStyle{
		Width:     gcss.Length{Value: wPx, Unit: gcss.UnitPx},
		Height:    gcss.Length{Value: hPx, Unit: gcss.UnitPx},
		MaxWidth:  gcss.Length{Unit: gcss.UnitAuto},
		MaxHeight: gcss.Length{Unit: gcss.UnitAuto},
	}
	return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableCell,
		Formatting: cssbox.BlockFC, Style: st}
}

func TestFixedTableTwoByTwoGeometry(t *testing.T) {
	mk := func() *cssbox.Box {
		st := gcss.ComputedStyle{TableLayout: "fixed", BorderCollapse: "separate",
			Width: gcss.Length{Unit: gcss.UnitAuto}}
		tbl := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTable,
			Formatting: cssbox.TableFC, Style: st}
		rg := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRowGroup, Formatting: cssbox.TableFC}
		for r := 0; r < 2; r++ {
			row := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRow, Formatting: cssbox.TableFC}
			row.Children = []*cssbox.Box{fixedCell(50, 30), fixedCell(50, 30)}
			rg.Children = append(rg.Children, row)
		}
		tbl.Children = []*cssbox.Box{rg}
		return tbl
	}
	e := New(nil, nil, nil)
	body := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC,
		Children: []*cssbox.Box{mk()}}
	frag := e.layoutTree(context.Background(), body, 200)
	if frag == nil {
		t.Fatal("nil fragment")
	}
	var cells []*Fragment
	var walk func(f *Fragment)
	walk = func(f *Fragment) {
		if f == nil {
			return
		}
		if f.H == 30 && f.W > 0 && f.W < 200 {
			cells = append(cells, f)
		}
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(frag)
	if len(cells) != 4 {
		t.Fatalf("want 4 cell fragments with H=30, got %d", len(cells))
	}
	xs := map[float64]bool{}
	ys := map[float64]bool{}
	for _, c := range cells {
		xs[c.X] = true
		ys[c.Y] = true
	}
	if len(xs) != 2 || len(ys) != 2 {
		t.Fatalf("want 2 distinct column Xs and 2 row Ys; got xs=%v ys=%v", xs, ys)
	}
}

func TestTableRowHeightIsTallestCell(t *testing.T) {
	st := gcss.ComputedStyle{TableLayout: "fixed"}
	row := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRow, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{fixedCell(40, 20), fixedCell(40, 50)}} // tallest = 50
	rg := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRowGroup, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{row}}
	tbl := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTable, Formatting: cssbox.TableFC,
		Style: st, Children: []*cssbox.Box{rg}}
	body := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC,
		Children: []*cssbox.Box{tbl}}
	e := New(nil, nil, nil)
	frag := e.layoutTree(context.Background(), body, 200)
	var heights []float64
	var walk func(f *Fragment)
	walk = func(f *Fragment) {
		if f == nil {
			return
		}
		if f.W == 40 {
			heights = append(heights, f.H)
		}
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(frag)
	if len(heights) != 2 {
		t.Fatalf("want 2 cells width 40, got %d", len(heights))
	}
	for _, h := range heights {
		if h != 50 {
			t.Errorf("cell height should stretch to the 50px row; got %v", h)
		}
	}
}

func TestAutoTableColumnsSizeToContent(t *testing.T) {
	// col 0 narrow text, col 1 much wider text. Auto layout gives col 1 more width,
	// and the table must not exceed the available width.
	mkCell := func(text string) *cssbox.Box {
		st := gcss.ComputedStyle{FontSizePt: 16, FontFamily: "serif",
			Width: gcss.Length{Unit: gcss.UnitAuto}, MaxWidth: gcss.Length{Unit: gcss.UnitAuto}}
		txt := &cssbox.Box{Kind: cssbox.BoxText, Text: text, Display: cssbox.DisplayInline, Style: st}
		return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableCell,
			Formatting: cssbox.InlineFC, Style: st, Children: []*cssbox.Box{txt}}
	}
	st := gcss.ComputedStyle{TableLayout: "auto", Width: gcss.Length{Unit: gcss.UnitAuto}}
	row := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRow, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{mkCell("Hi"), mkCell("A much longer cell of content here")}}
	rg := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRowGroup, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{row}}
	tbl := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTable, Formatting: cssbox.TableFC,
		Style: st, Children: []*cssbox.Box{rg}}

	e := New(nil, nil, nil)
	g := buildGrid(tbl)
	e.solveAutoWidths(context.Background(), g, 600)
	if len(g.cols) != 2 {
		t.Fatalf("want 2 columns, got %d", len(g.cols))
	}
	if g.cols[1].width <= g.cols[0].width {
		t.Errorf("wider-content column should be wider: col0=%v col1=%v", g.cols[0].width, g.cols[1].width)
	}
	total := g.cols[0].width + g.cols[1].width
	if total > 600+0.5 {
		t.Errorf("table columns (%v) should fit the 600 available width", total)
	}
}

func TestAutoTableSpecifiedWidthPinsColumn(t *testing.T) {
	mkCell := func(w float64) *cssbox.Box {
		st := gcss.ComputedStyle{Width: gcss.Length{Value: w, Unit: gcss.UnitPx},
			MaxWidth: gcss.Length{Unit: gcss.UnitAuto}}
		return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableCell,
			Formatting: cssbox.BlockFC, Style: st}
	}
	st := gcss.ComputedStyle{TableLayout: "auto", Width: gcss.Length{Unit: gcss.UnitAuto}}
	row := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRow, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{mkCell(120), mkCell(40)}}
	rg := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRowGroup, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{row}}
	tbl := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTable, Formatting: cssbox.TableFC,
		Style: st, Children: []*cssbox.Box{rg}}
	e := New(nil, nil, nil)
	g := buildGrid(tbl)
	e.solveAutoWidths(context.Background(), g, 600)
	if g.cols[0].width < 119 || g.cols[0].width > 200 {
		t.Errorf("col0 should be near its 120 spec (with surplus distribution); got %v", g.cols[0].width)
	}
	if g.cols[0].width <= g.cols[1].width {
		t.Errorf("col0 (120 spec) should exceed col1 (40 spec): %v vs %v", g.cols[0].width, g.cols[1].width)
	}
}

func TestPercentColumnTakesShare(t *testing.T) {
	// col 0: width 25%; col 1: auto with short content. In a 400-wide table, col 0 ≈ 100.
	pctCell := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableCell, Formatting: cssbox.BlockFC,
		Style: gcss.ComputedStyle{Width: gcss.Length{Value: 25, Unit: gcss.UnitPercent},
			MaxWidth: gcss.Length{Unit: gcss.UnitAuto}}}
	autoCell := func() *cssbox.Box {
		st := gcss.ComputedStyle{FontSizePt: 16, FontFamily: "serif",
			Width: gcss.Length{Unit: gcss.UnitAuto}, MaxWidth: gcss.Length{Unit: gcss.UnitAuto}}
		txt := &cssbox.Box{Kind: cssbox.BoxText, Text: "x", Display: cssbox.DisplayInline, Style: st}
		return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableCell, Formatting: cssbox.InlineFC,
			Style: st, Children: []*cssbox.Box{txt}}
	}
	row := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRow, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{pctCell, autoCell()}}
	rg := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRowGroup, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{row}}
	// A fixed-width table so the percentage basis is unambiguous (400px).
	tbl := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTable, Formatting: cssbox.TableFC,
		Style:    gcss.ComputedStyle{TableLayout: "auto", Width: gcss.Length{Value: 400, Unit: gcss.UnitPx}},
		Children: []*cssbox.Box{rg}}
	e := New(nil, nil, nil)
	g := buildGrid(tbl)
	e.solveAutoWidths(context.Background(), g, 400)
	if g.cols[0].width < 90 || g.cols[0].width > 110 {
		t.Errorf("25%% column of a 400px table should be ~100; got %v", g.cols[0].width)
	}
	if g.cols[1].width <= 0 {
		t.Errorf("the auto column should still get the remaining width; got %v", g.cols[1].width)
	}
	// Conservation: the two columns must sum to the table's used width (~400, the
	// content width with no border-spacing). col1 must get the leftover, not a sliver.
	total := g.cols[0].width + g.cols[1].width
	if total < 395 || total > 405 {
		t.Errorf("columns should sum to ~400 (the used width); got %v (col0=%v col1=%v)", total, g.cols[0].width, g.cols[1].width)
	}
	if g.cols[1].width < 250 {
		t.Errorf("the auto column should get the ~300px leftover, not a sliver; got %v", g.cols[1].width)
	}
}

func TestColspanRaisesSpannedColumns(t *testing.T) {
	// Row 0: a single wide cell spanning 2 columns (specified width 200).
	// Row 1: two narrow cells (spec 30 each).
	// Auto layout: the colspan-2 cell's 200 must push the two columns to sum >= ~200.
	wide := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableCell, Formatting: cssbox.BlockFC,
		ColSpan: 2, Style: gcss.ComputedStyle{Width: gcss.Length{Value: 200, Unit: gcss.UnitPx},
			MaxWidth: gcss.Length{Unit: gcss.UnitAuto}}}
	narrow := func() *cssbox.Box {
		return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableCell, Formatting: cssbox.BlockFC,
			Style: gcss.ComputedStyle{Width: gcss.Length{Value: 30, Unit: gcss.UnitPx},
				MaxWidth: gcss.Length{Unit: gcss.UnitAuto}}}
	}
	r0 := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRow, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{wide}}
	r1 := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRow, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{narrow(), narrow()}}
	rg := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRowGroup, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{r0, r1}}
	tbl := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTable, Formatting: cssbox.TableFC,
		Style:    gcss.ComputedStyle{TableLayout: "auto", Width: gcss.Length{Unit: gcss.UnitAuto}},
		Children: []*cssbox.Box{rg}}
	e := New(nil, nil, nil)
	g := buildGrid(tbl)
	e.solveAutoWidths(context.Background(), g, 600)
	sum := g.cols[0].width + g.cols[1].width
	if sum < 190 {
		t.Errorf("colspan-2 width 200 should raise the two columns to ~200; got sum %v", sum)
	}
}

func TestRowspanDistributesExcessHeight(t *testing.T) {
	// Col 0: a rowspan-2 cell of fixed height 100. Col 1: two cells of height 20 each.
	// The two rows must grow so they sum (spacing 0) to >= 100.
	rs := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableCell, Formatting: cssbox.BlockFC,
		RowSpan: 2, Style: gcss.ComputedStyle{Width: gcss.Length{Value: 40, Unit: gcss.UnitPx},
			Height: gcss.Length{Value: 100, Unit: gcss.UnitPx}, MaxWidth: gcss.Length{Unit: gcss.UnitAuto},
			MaxHeight: gcss.Length{Unit: gcss.UnitAuto}}}
	small := func() *cssbox.Box {
		return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableCell, Formatting: cssbox.BlockFC,
			Style: gcss.ComputedStyle{Width: gcss.Length{Value: 40, Unit: gcss.UnitPx},
				Height: gcss.Length{Value: 20, Unit: gcss.UnitPx}, MaxWidth: gcss.Length{Unit: gcss.UnitAuto},
				MaxHeight: gcss.Length{Unit: gcss.UnitAuto}}}
	}
	r0 := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRow, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{rs, small()}}
	r1 := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRow, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{small()}}
	rg := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRowGroup, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{r0, r1}}
	tbl := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTable, Formatting: cssbox.TableFC,
		Style: gcss.ComputedStyle{TableLayout: "fixed", Width: gcss.Length{Unit: gcss.UnitAuto}}, Children: []*cssbox.Box{rg}}
	body := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC,
		Children: []*cssbox.Box{tbl}}
	e := New(nil, nil, nil)
	frag := e.layoutTree(context.Background(), body, 200)
	// The rowspan cell's border box must be 100 tall (it fills both grown rows).
	var found bool
	var walk func(f *Fragment)
	walk = func(f *Fragment) {
		if f == nil {
			return
		}
		if f.W == 40 && f.H == 100 {
			found = true
		}
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(frag)
	if !found {
		t.Errorf("rowspan-2 cell should fill a 100-tall band across both grown rows")
	}
}

func TestVerticalAlignBottomShiftsContent(t *testing.T) {
	// A 60-tall row (forced by a tall sibling). A short cell with vertical-align:bottom
	// must have its inner content near the bottom of the band, not the top.
	tall := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableCell, Formatting: cssbox.BlockFC,
		Style: gcss.ComputedStyle{Width: gcss.Length{Value: 40, Unit: gcss.UnitPx},
			Height: gcss.Length{Value: 60, Unit: gcss.UnitPx}, MaxWidth: gcss.Length{Unit: gcss.UnitAuto},
			MaxHeight: gcss.Length{Unit: gcss.UnitAuto}}}
	innerSt := gcss.ComputedStyle{Height: gcss.Length{Value: 10, Unit: gcss.UnitPx},
		Width: gcss.Length{Value: 20, Unit: gcss.UnitPx}, MaxWidth: gcss.Length{Unit: gcss.UnitAuto},
		MaxHeight: gcss.Length{Unit: gcss.UnitAuto}}
	inner := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC, Style: innerSt}
	short := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableCell, Formatting: cssbox.BlockFC,
		Style: gcss.ComputedStyle{Width: gcss.Length{Value: 40, Unit: gcss.UnitPx}, VerticalAlign: "bottom",
			Height: gcss.Length{Unit: gcss.UnitAuto}, MaxWidth: gcss.Length{Unit: gcss.UnitAuto},
			MaxHeight: gcss.Length{Unit: gcss.UnitAuto}},
		Children: []*cssbox.Box{inner}}
	row := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRow, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{tall, short}}
	rg := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRowGroup, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{row}}
	tbl := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTable, Formatting: cssbox.TableFC,
		Style: gcss.ComputedStyle{TableLayout: "fixed", Width: gcss.Length{Unit: gcss.UnitAuto}}, Children: []*cssbox.Box{rg}}
	body := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC,
		Children: []*cssbox.Box{tbl}}
	e := New(nil, nil, nil)
	frag := e.layoutTree(context.Background(), body, 200)
	// Find the inner 20x10 fragment and the short cell (40x60 with a child).
	var inner10, shortCell *Fragment
	var walk func(f *Fragment)
	walk = func(f *Fragment) {
		if f == nil {
			return
		}
		if f.W == 20 && f.H == 10 {
			inner10 = f
		}
		if f.W == 40 && f.H == 60 && len(f.Children) > 0 {
			shortCell = f
		}
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(frag)
	if inner10 == nil || shortCell == nil {
		t.Fatalf("missing fragments inner=%v cell=%v", inner10, shortCell)
	}
	offset := inner10.Y - shortCell.Y
	if offset < 45 || offset > 55 {
		t.Errorf("vertical-align:bottom should place 10-tall content ~50px down (band 60 − content 10); got %v", offset)
	}
}

func TestVerticalAlignTopIsDefault(t *testing.T) {
	// Without vertical-align, content stays at the band top (offset ~0).
	tall := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableCell, Formatting: cssbox.BlockFC,
		Style: gcss.ComputedStyle{Width: gcss.Length{Value: 40, Unit: gcss.UnitPx},
			Height: gcss.Length{Value: 60, Unit: gcss.UnitPx}, MaxWidth: gcss.Length{Unit: gcss.UnitAuto},
			MaxHeight: gcss.Length{Unit: gcss.UnitAuto}}}
	innerSt := gcss.ComputedStyle{Height: gcss.Length{Value: 10, Unit: gcss.UnitPx},
		Width: gcss.Length{Value: 20, Unit: gcss.UnitPx}, MaxWidth: gcss.Length{Unit: gcss.UnitAuto},
		MaxHeight: gcss.Length{Unit: gcss.UnitAuto}}
	inner := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC, Style: innerSt}
	short := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableCell, Formatting: cssbox.BlockFC,
		Style: gcss.ComputedStyle{Width: gcss.Length{Value: 40, Unit: gcss.UnitPx},
			Height: gcss.Length{Unit: gcss.UnitAuto}, MaxWidth: gcss.Length{Unit: gcss.UnitAuto},
			MaxHeight: gcss.Length{Unit: gcss.UnitAuto}},
		Children: []*cssbox.Box{inner}}
	row := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRow, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{tall, short}}
	rg := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRowGroup, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{row}}
	tbl := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTable, Formatting: cssbox.TableFC,
		Style: gcss.ComputedStyle{TableLayout: "fixed", Width: gcss.Length{Unit: gcss.UnitAuto}}, Children: []*cssbox.Box{rg}}
	body := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC,
		Children: []*cssbox.Box{tbl}}
	e := New(nil, nil, nil)
	frag := e.layoutTree(context.Background(), body, 200)
	var inner10, shortCell *Fragment
	var walk func(f *Fragment)
	walk = func(f *Fragment) {
		if f == nil {
			return
		}
		if f.W == 20 && f.H == 10 {
			inner10 = f
		}
		if f.W == 40 && f.H == 60 && len(f.Children) > 0 {
			shortCell = f
		}
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(frag)
	if inner10 == nil || shortCell == nil {
		t.Fatalf("missing fragments inner=%v cell=%v", inner10, shortCell)
	}
	offset := inner10.Y - shortCell.Y
	if offset > 5 {
		t.Errorf("default (top) vertical-align should keep content at the band top (~0); got offset %v", offset)
	}
}

func TestCaptionTopShiftsGridDown(t *testing.T) {
	capSt := gcss.ComputedStyle{Width: gcss.Length{Unit: gcss.UnitAuto}, Height: gcss.Length{Value: 24, Unit: gcss.UnitPx},
		MaxWidth: gcss.Length{Unit: gcss.UnitAuto}, MaxHeight: gcss.Length{Unit: gcss.UnitAuto},
		CaptionSide: "top"}
	caption := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableCaption,
		Formatting: cssbox.BlockFC, Style: capSt}
	row := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRow, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{fixedCell(50, 30)}}
	rg := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRowGroup, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{row}}
	tbl := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTable, Formatting: cssbox.TableFC,
		Style:    gcss.ComputedStyle{TableLayout: "fixed", CaptionSide: "top", Width: gcss.Length{Unit: gcss.UnitAuto}},
		Children: []*cssbox.Box{caption, rg}}
	body := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC,
		Children: []*cssbox.Box{tbl}}
	e := New(nil, nil, nil)
	frag := e.layoutTree(context.Background(), body, 200)
	// The cell (50x30) must sit at y >= 24 (below the caption).
	var cellY float64 = -1
	var walk func(f *Fragment)
	walk = func(f *Fragment) {
		if f == nil {
			return
		}
		if f.W == 50 && f.H == 30 {
			cellY = f.Y
		}
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(frag)
	if cellY < 24 {
		t.Errorf("caption-side:top should push the grid below the 24px caption; cell Y=%v", cellY)
	}
}

func TestCaptionBottomBelowGrid(t *testing.T) {
	capSt := gcss.ComputedStyle{Width: gcss.Length{Unit: gcss.UnitAuto}, Height: gcss.Length{Value: 24, Unit: gcss.UnitPx},
		MaxWidth: gcss.Length{Unit: gcss.UnitAuto}, MaxHeight: gcss.Length{Unit: gcss.UnitAuto},
		CaptionSide: "bottom"}
	caption := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableCaption,
		Formatting: cssbox.BlockFC, Style: capSt}
	row := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRow, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{fixedCell(50, 30)}}
	rg := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRowGroup, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{row}}
	tbl := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTable, Formatting: cssbox.TableFC,
		Style:    gcss.ComputedStyle{TableLayout: "fixed", CaptionSide: "bottom", Width: gcss.Length{Unit: gcss.UnitAuto}},
		Children: []*cssbox.Box{caption, rg}}
	body := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC,
		Children: []*cssbox.Box{tbl}}
	e := New(nil, nil, nil)
	frag := e.layoutTree(context.Background(), body, 200)
	// caption-side:bottom: the cell sits at the TOP (y ~0), the caption below it (y >= 30).
	var cellY float64 = -1
	var capY float64 = -1
	var walk func(f *Fragment)
	walk = func(f *Fragment) {
		if f == nil {
			return
		}
		if f.W == 50 && f.H == 30 {
			cellY = f.Y
		}
		if f.H == 24 && f.W > 0 {
			capY = f.Y
		} // the caption fragment (24 tall)
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(frag)
	if cellY < 0 || cellY > 5 {
		t.Errorf("caption-side:bottom should keep the grid at the top (~0); cell Y=%v", cellY)
	}
	if capY < 30 {
		t.Errorf("caption-side:bottom should place the caption below the 30px row; caption Y=%v", capY)
	}
}

func TestCaptionSideOnCaptionElementHonored(t *testing.T) {
	// caption-side:bottom set DIRECTLY on <caption> (not the table) must place the
	// caption below the grid — exercises the inherited property read from the caption.
	src := `<table style="border-spacing:0">
		<caption style="caption-side:bottom; height:20px">C</caption>
		<tr><td style="height:30px;width:40px">X</td></tr>
	</table>`
	root := build(t, src, nil)
	e := New(nil, nil, nil)
	frag := e.layoutTree(context.Background(), root, 200)
	// Find the cell (height ~30) and the caption (height ~20). The caption Y must be
	// BELOW the cell Y (caption-side:bottom). With the BUG, the caption would be at the top.
	var cellY, capY float64 = -1, -1
	var walk func(f *Fragment)
	walk = func(f *Fragment) {
		if f == nil {
			return
		}
		// cell: a 40-wide, ~30-tall fragment
		if f.W >= 38 && f.W <= 42 && f.H >= 28 && f.H <= 32 {
			cellY = f.Y
		}
		// caption: ~20 tall (its specified height); H differs from the cell's ~30
		if f.H >= 18 && f.H <= 22 {
			capY = f.Y
		}
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(frag)
	if cellY < 0 {
		t.Fatalf("cell fragment not found")
	}
	if capY < 0 {
		t.Fatalf("caption fragment not found")
	}
	if capY <= cellY {
		t.Errorf("caption-side:bottom on <caption> should place caption (Y=%v) below the cell (Y=%v)", capY, cellY)
	}
}
