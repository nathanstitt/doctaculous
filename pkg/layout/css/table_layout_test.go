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
