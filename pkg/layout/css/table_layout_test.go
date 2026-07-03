package css

import (
	"context"
	"image/color"
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout"
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

func TestCollapseProducesEdgesAndClearsCellBorders(t *testing.T) {
	mkCell := func() *cssbox.Box {
		st := gcss.ComputedStyle{
			Width:             gcss.Length{Value: 40, Unit: gcss.UnitPx},
			Height:            gcss.Length{Value: 20, Unit: gcss.UnitPx},
			MaxWidth:          gcss.Length{Unit: gcss.UnitAuto},
			MaxHeight:         gcss.Length{Unit: gcss.UnitAuto},
			BorderTopWidth:    gcss.Length{Value: 2, Unit: gcss.UnitPx},
			BorderRightWidth:  gcss.Length{Value: 2, Unit: gcss.UnitPx},
			BorderBottomWidth: gcss.Length{Value: 2, Unit: gcss.UnitPx},
			BorderLeftWidth:   gcss.Length{Value: 2, Unit: gcss.UnitPx},
			BorderTopStyle:    "solid",
			BorderRightStyle:  "solid",
			BorderBottomStyle: "solid",
			BorderLeftStyle:   "solid",
		}
		return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableCell, Formatting: cssbox.BlockFC, Style: st}
	}
	row := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRow, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{mkCell(), mkCell()}}
	rg := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRowGroup, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{row}}
	tbl := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTable, Formatting: cssbox.TableFC,
		Style:    gcss.ComputedStyle{TableLayout: "fixed", BorderCollapse: "collapse", Width: gcss.Length{Unit: gcss.UnitAuto}},
		Children: []*cssbox.Box{rg}}
	body := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC,
		Children: []*cssbox.Box{tbl}}
	e := New(nil, nil, nil)
	frag := e.layoutTree(context.Background(), body, 200)
	collapsed := 0
	cellBorders := 0
	var walk func(f *Fragment)
	walk = func(f *Fragment) {
		if f == nil {
			return
		}
		collapsed += len(f.Collapsed)
		if f.W == 40 {
			for _, be := range f.Border {
				if be.Width > 0 {
					cellBorders++
				}
			}
		}
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(frag)
	if collapsed == 0 {
		t.Error("collapse mode should produce resolved edge strips")
	}
	if cellBorders != 0 {
		t.Errorf("collapse mode should clear per-cell borders; found %d", cellBorders)
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

// TestPercentTableWidthFillsAvailable covers a table with a PERCENTAGE width (the
// classic <table width="100%">): its used width is the resolved percentage of the
// containing block (which the caller has already folded into contentW), NOT the natural
// content width. So when the content is far narrower than the available width, the
// surplus is distributed across the auto columns to fill it — the columns must span the
// full width, not sit shrink-wrapped at the left. (A width:auto table, by contrast,
// shrinks to content — TestAutoTableColumnsSizeToContent.)
func TestPercentTableWidthFillsAvailable(t *testing.T) {
	mkCell := func(text string) *cssbox.Box {
		st := gcss.ComputedStyle{FontSizePt: 16, FontFamily: "serif",
			Width: gcss.Length{Unit: gcss.UnitAuto}, MaxWidth: gcss.Length{Unit: gcss.UnitAuto}}
		txt := &cssbox.Box{Kind: cssbox.BoxText, Text: text, Display: cssbox.DisplayInline, Style: st}
		return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableCell,
			Formatting: cssbox.InlineFC, Style: st, Children: []*cssbox.Box{txt}}
	}
	// width:100% table with two short-content columns.
	st := gcss.ComputedStyle{TableLayout: "auto",
		Width: gcss.Length{Value: 100, Unit: gcss.UnitPercent}, MaxWidth: gcss.Length{Unit: gcss.UnitAuto}}
	row := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRow, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{mkCell("Hi"), mkCell("Go")}}
	rg := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRowGroup, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{row}}
	tbl := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTable, Formatting: cssbox.TableFC,
		Style: st, Children: []*cssbox.Box{rg}}

	e := New(nil, nil, nil)
	g := buildGrid(tbl)
	// contentW=600 is the table's resolved 100% content width (the caller resolved the
	// percentage before calling solveAutoWidths).
	e.solveAutoWidths(context.Background(), g, 600)
	total := g.cols[0].width + g.cols[1].width
	if total < 599 || total > 601 {
		t.Errorf("a width:100%% table must fill its 600 available width; got %v (col0=%v col1=%v)",
			total, g.cols[0].width, g.cols[1].width)
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

// TestVerticalAlignBaselineCoincides exercises real vertical-align:baseline on table
// cells (the backport of the shared baseline machinery). Two cells in one row carry
// text at DIFFERENT font sizes, so their first-baseline offsets from the band top
// differ. With vertical-align:baseline the engine must shift the smaller-font cell's
// CONTENT down so both cells' first text baselines land at the same page-space Y. (The
// pre-backport behavior treated baseline as top: the two baselines did NOT coincide.)
func TestVerticalAlignBaselineCoincides(t *testing.T) {
	auto := gcss.Length{Unit: gcss.UnitAuto}
	px0 := gcss.Length{Unit: gcss.UnitPx}
	mkCell := func(text string, fontSizePt float64) *cssbox.Box {
		st := gcss.ComputedStyle{
			Width: auto, Height: auto, MaxWidth: auto, MaxHeight: auto, MinWidth: px0, MinHeight: px0,
			FontFamily: "serif", FontSizePt: fontSizePt, LineHeight: auto, VerticalAlign: "baseline",
		}
		textSt := gcss.ComputedStyle{FontFamily: "serif", FontSizePt: fontSizePt, LineHeight: auto}
		txt := &cssbox.Box{Kind: cssbox.BoxText, Text: text, Display: cssbox.DisplayInline, Style: textSt}
		return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableCell,
			Formatting: cssbox.InlineFC, Style: st, Children: []*cssbox.Box{txt}}
	}
	small := mkCell("Hi", 10)
	large := mkCell("Hi", 24)
	row := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRow, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{small, large}}
	rg := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRowGroup, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{row}}
	tbl := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTable, Formatting: cssbox.TableFC,
		Style: gcss.ComputedStyle{TableLayout: "auto", Width: auto}, Children: []*cssbox.Box{rg}}
	body := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC,
		Children: []*cssbox.Box{tbl}}
	e := New(nil, nil, nil)
	frag := e.layoutTree(context.Background(), body, 300)

	// Collect the two table-cell fragments (display:table-cell) and read each one's
	// first line baseline (the cell directly carries Lines for an InlineFC cell).
	var smallLine, largeLine *LineFragment
	var walk func(f *Fragment)
	walk = func(f *Fragment) {
		if f == nil {
			return
		}
		if f.Box != nil && f.Box.Display == cssbox.DisplayTableCell && len(f.Lines) > 0 {
			if f.W < 20 { // the narrow 10pt "Hi" cell
				smallLine = &f.Lines[0]
			} else { // the wider 24pt "Hi" cell
				largeLine = &f.Lines[0]
			}
		}
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(frag)
	if smallLine == nil || largeLine == nil {
		t.Fatalf("missing cell baselines: small=%v large=%v", smallLine, largeLine)
	}
	const eps = 0.01
	if absf(smallLine.BaselineY-largeLine.BaselineY) > eps {
		t.Errorf("vertical-align:baseline should make cell first baselines coincide; small=%v large=%v",
			smallLine.BaselineY, largeLine.BaselineY)
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

func TestRTLTableDegradesGracefully(t *testing.T) {
	var logged bool
	logf := func(string, ...any) { logged = true }
	row := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRow, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{fixedCell(50, 20), fixedCell(50, 20)}}
	rg := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRowGroup, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{row}}
	tbl := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTable, Formatting: cssbox.TableFC,
		Style: gcss.ComputedStyle{TableLayout: "fixed", Direction: "rtl", Width: gcss.Length{Unit: gcss.UnitAuto}}, Children: []*cssbox.Box{rg}}
	body := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC,
		Children: []*cssbox.Box{tbl}}
	e := New(nil, nil, logf)
	frag := e.layoutTree(context.Background(), body, 200)
	if frag == nil {
		t.Fatal("RTL table should still produce a fragment")
	}
	if !logged {
		t.Error("RTL table should log a degradation message")
	}
}

func TestEmptyTableNoPanic(t *testing.T) {
	tbl := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTable, Formatting: cssbox.TableFC,
		Style: gcss.ComputedStyle{Width: gcss.Length{Unit: gcss.UnitAuto}}}
	body := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC,
		Children: []*cssbox.Box{tbl}}
	e := New(nil, nil, nil)
	if f := e.layoutTree(context.Background(), body, 200); f == nil {
		t.Fatal("empty table should produce a (zero-size) fragment, not nil")
	}
}

func TestCellContainingFloatNoPanic(t *testing.T) {
	flt := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC,
		Float: cssbox.FloatLeft,
		Style: gcss.ComputedStyle{Width: gcss.Length{Value: 20, Unit: gcss.UnitPx},
			Height: gcss.Length{Value: 20, Unit: gcss.UnitPx}, Float: "left",
			MaxWidth: gcss.Length{Unit: gcss.UnitAuto}, MaxHeight: gcss.Length{Unit: gcss.UnitAuto}}}
	cell := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableCell, Formatting: cssbox.BlockFC,
		Style: gcss.ComputedStyle{Width: gcss.Length{Value: 60, Unit: gcss.UnitPx}, MaxWidth: gcss.Length{Unit: gcss.UnitAuto}}, Children: []*cssbox.Box{flt}}
	row := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRow, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{cell}}
	rg := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRowGroup, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{row}}
	tbl := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTable, Formatting: cssbox.TableFC,
		Style: gcss.ComputedStyle{TableLayout: "fixed", Width: gcss.Length{Unit: gcss.UnitAuto}}, Children: []*cssbox.Box{rg}}
	body := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC,
		Children: []*cssbox.Box{tbl}}
	e := New(nil, nil, nil)
	if f := e.layoutTree(context.Background(), body, 200); f == nil {
		t.Fatal("a cell containing a float should lay out without panic")
	}
}

func TestTableInsideOverflowHiddenNoPanic(t *testing.T) {
	// Flag combination: a table inside an overflow:hidden box (the box clips/establishes a BFC).
	row := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRow, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{fixedCell(50, 20), fixedCell(50, 20)}}
	rg := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRowGroup, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{row}}
	tbl := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTable, Formatting: cssbox.TableFC,
		Style: gcss.ComputedStyle{TableLayout: "fixed", Width: gcss.Length{Unit: gcss.UnitAuto}}, Children: []*cssbox.Box{rg}}
	clip := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC,
		Style: gcss.ComputedStyle{Overflow: "hidden", Width: gcss.Length{Value: 60, Unit: gcss.UnitPx},
			Height: gcss.Length{Value: 30, Unit: gcss.UnitPx}, MaxWidth: gcss.Length{Unit: gcss.UnitAuto}, MaxHeight: gcss.Length{Unit: gcss.UnitAuto}},
		Children: []*cssbox.Box{tbl}}
	body := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC,
		Children: []*cssbox.Box{clip}}
	e := New(nil, nil, nil)
	if f := e.layoutTree(context.Background(), body, 200); f == nil {
		t.Fatal("a table inside overflow:hidden should lay out without panic")
	}
}

func TestAutoWidthTableBoxWrapsGrid(t *testing.T) {
	// A width:auto table with a table-level border and two 50px cells in a wide
	// viewport must have its BOX wrap the ~100px grid, not fill the container.
	mkCell := func() *cssbox.Box {
		return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableCell, Formatting: cssbox.BlockFC,
			Style: gcss.ComputedStyle{Width: gcss.Length{Value: 50, Unit: gcss.UnitPx},
				Height:   gcss.Length{Value: 20, Unit: gcss.UnitPx},
				MaxWidth: gcss.Length{Unit: gcss.UnitAuto}, MaxHeight: gcss.Length{Unit: gcss.UnitAuto}}}
	}
	row := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRow, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{mkCell(), mkCell()}}
	rg := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRowGroup, Formatting: cssbox.TableFC,
		Children: []*cssbox.Box{row}}
	// table-level border so the box width is observable; auto width; border-spacing 0.
	tbl := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTable, Formatting: cssbox.TableFC,
		Style: gcss.ComputedStyle{TableLayout: "auto", Width: gcss.Length{Unit: gcss.UnitAuto},
			MaxWidth:       gcss.Length{Unit: gcss.UnitAuto},
			BorderTopWidth: gcss.Length{Value: 1, Unit: gcss.UnitPx}, BorderTopStyle: "solid",
			BorderRightWidth: gcss.Length{Value: 1, Unit: gcss.UnitPx}, BorderRightStyle: "solid",
			BorderBottomWidth: gcss.Length{Value: 1, Unit: gcss.UnitPx}, BorderBottomStyle: "solid",
			BorderLeftWidth: gcss.Length{Value: 1, Unit: gcss.UnitPx}, BorderLeftStyle: "solid"},
		Children: []*cssbox.Box{rg}}
	body := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC,
		Style:    gcss.ComputedStyle{Width: gcss.Length{Unit: gcss.UnitAuto}, MaxWidth: gcss.Length{Unit: gcss.UnitAuto}},
		Children: []*cssbox.Box{tbl}}
	e := New(nil, nil, nil)
	frag := e.layoutTree(context.Background(), body, 600) // wide viewport
	// Find the table fragment: it's the one with a non-zero border (the 1px table border)
	// and contains the cells. Its W should be ~100 (grid: 50+50) + 2 (the 1px L/R borders)
	// = ~102, NOT ~600 (the container width).
	var tableW float64 = -1
	var walk func(f *Fragment)
	walk = func(f *Fragment) {
		if f == nil {
			return
		}
		// the table fragment has a top border width > 0 and is wider than a single cell
		if f.Border[0].Width > 0 && f.W > 60 {
			tableW = f.W
		}
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(frag)
	if tableW < 0 {
		t.Fatal("table fragment (with border) not found")
	}
	if tableW > 150 {
		t.Errorf("auto-width table box should wrap its ~100px grid (+borders), not fill the 600px container; got W=%v", tableW)
	}
	if tableW < 95 {
		t.Errorf("auto-width table box should be at least the ~100px grid width; got W=%v", tableW)
	}
}

// TestEmptyCellsHideSuppressesDecorations pins F3: in separate-borders mode, an EMPTY
// cell with empty-cells:hide paints no background or border, while a non-empty cell still
// does. Both cells have a yellow background + border in CSS; only the filled cell keeps
// them. We find the cells by their fragment and check Background/Border.
func TestEmptyCellsHideSuppressesDecorations(t *testing.T) {
	src := `<table style="border-collapse:separate;empty-cells:hide">
<tr>
<td style="width:40px;height:20px;background:#ffdd00;border:2px solid #cc0000">X</td>
<td style="width:40px;height:20px;background:#ffdd00;border:2px solid #cc0000"></td>
</tr></table>`
	root := layoutTreeFor(t, src, 200, nil)
	// Collect the two table cells (border-box ~ 44x24 incl. the 2px border each side).
	var cells []*Fragment
	var walk func(f *Fragment)
	walk = func(f *Fragment) {
		if f == nil {
			return
		}
		if f.Box != nil && f.Box.Display == cssbox.DisplayTableCell {
			cells = append(cells, f)
		}
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(root)
	if len(cells) != 2 {
		t.Fatalf("want 2 cell fragments, got %d", len(cells))
	}
	// cells[0] has text "X" (non-empty): keeps its background + border.
	full := cells[0]
	if full.Background.A == 0 {
		t.Errorf("non-empty cell lost its background")
	}
	hasBorder := false
	for _, e := range full.Border {
		if e.Width > 0 && e.Style != 0 {
			hasBorder = true
		}
	}
	if !hasBorder {
		t.Errorf("non-empty cell lost its border")
	}
	// cells[1] is empty: empty-cells:hide suppresses its background + border.
	empty := cells[1]
	if empty.Background.A != 0 {
		t.Errorf("empty cell kept its background %v, want suppressed (empty-cells:hide)", empty.Background)
	}
	for _, e := range empty.Border {
		if e.Width > 0 && e.Style != 0 {
			t.Errorf("empty cell kept a border edge, want suppressed (empty-cells:hide)")
		}
	}
}

// TestEmptyCellsShowKeepsDecorations guards the default: empty-cells:show (initial) keeps
// an empty cell's border + background.
func TestEmptyCellsShowKeepsDecorations(t *testing.T) {
	src := `<table style="border-collapse:separate">
<tr><td style="width:40px;height:20px;background:#ffdd00"></td></tr></table>`
	root := layoutTreeFor(t, src, 200, nil)
	var cell *Fragment
	var walk func(f *Fragment)
	walk = func(f *Fragment) {
		if f == nil {
			return
		}
		if f.Box != nil && f.Box.Display == cssbox.DisplayTableCell {
			cell = f
		}
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(root)
	if cell == nil {
		t.Fatal("no cell fragment")
	}
	if cell.Background.A == 0 {
		t.Errorf("empty-cells:show (default) must keep the empty cell's background")
	}
}

// TestPercentColumnWithNoCells pins F4: a <col> with a percentage width but NO cell
// originating in its column still reserves its share of the table width (and the other
// columns get the leftover). Here a 200px table has two <col>s — the second is 40% — but
// only ONE cell in the single row, so column 1 has no cell. Column 1 must still claim
// 40% (80px), leaving column 0's cell at 120px. (Verified already-correct; this locks it.)
func TestPercentColumnWithNoCells(t *testing.T) {
	for _, layout := range []string{"auto", "fixed"} {
		t.Run(layout, func(t *testing.T) {
			src := `<table style="width:200px;table-layout:` + layout +
				`;border-collapse:separate"><colgroup><col><col style="width:40%"></colgroup>` +
				`<tr><td>A</td></tr></table>`
			root := layoutTreeFor(t, src, 400, nil)
			var cell *Fragment
			var walk func(f *Fragment)
			walk = func(f *Fragment) {
				if f.Box != nil && f.Box.Display == cssbox.DisplayTableCell {
					cell = f
				}
				for _, c := range f.Children {
					walk(c)
				}
			}
			walk(root)
			if cell == nil {
				t.Fatalf("[%s] no cell fragment", layout)
			}
			// Column 1 (40% of 200 = 80) is reserved even with no cell, so the cell in
			// column 0 gets the leftover 120 (± a couple px for borders/spacing rounding).
			if cell.W < 110 || cell.W > 125 {
				t.Errorf("[%s] cell W = %.1f, want ~120 (col 1's 40%% reserved without a cell)", layout, cell.W)
			}
		})
	}
}

// TestTableBackgroundLayers pins F2 (CSS 17.5.1 background layers): a column background and
// a row background are emitted behind the cells, in CSS paint order (columns before rows,
// so the row paints on top where they overlap). A 2x2 table with a background on column 1
// (<col>) and on row 1 (<tr>). Mutation-verify: drop backgroundLayers() and neither bg
// fragment is emitted.
func TestTableBackgroundLayers(t *testing.T) {
	src := `<table style="border-collapse:separate;border-spacing:0">
<colgroup><col style="background:rgb(170,204,255)"><col></colgroup>
<tr style="background:rgb(255,204,153)"><td>a</td><td>b</td></tr>
<tr><td>c</td><td>d</td></tr></table>`
	root := layoutTreeFor(t, src, 200, nil)
	page := root.Page(200, root.Y+root.H)
	colBg := color.RGBA{170, 204, 255, 255}
	rowBg := color.RGBA{255, 204, 153, 255}
	colIdx, rowIdx := -1, -1
	for i := range page.Items {
		if page.Items[i].Kind != layout.BackgroundKind {
			continue
		}
		switch page.Items[i].Rule.Color {
		case colBg:
			if colIdx < 0 {
				colIdx = i
			}
		case rowBg:
			if rowIdx < 0 {
				rowIdx = i
			}
		}
	}
	if colIdx < 0 {
		t.Errorf("no column background emitted (F2)")
	}
	if rowIdx < 0 {
		t.Errorf("no row background emitted (F2)")
	}
	if colIdx >= 0 && rowIdx >= 0 && colIdx > rowIdx {
		t.Errorf("column bg (item %d) must paint BEFORE the row bg (item %d)", colIdx, rowIdx)
	}
}

// TestFixedPercentColumnBasisExcludesSpacing pins F6: in fixed layout a percentage column
// width resolves against the table content width MINUS the inter-column border-spacing
// (matching auto layout), not the full content width. A 2-col fixed table, width 200,
// border-spacing 10 (3 gaps = 30), col 0 = 50%. The 50% column must be 0.5×(200-30)=85,
// not 0.5×200=100. Mutation-verify: revert `used = contentW - spacing` to `used = contentW`
// and the column becomes 100.
func TestFixedPercentColumnBasisExcludesSpacing(t *testing.T) {
	src := `<table style="width:200px;table-layout:fixed;border-collapse:separate;border-spacing:10px">
<tr><td style="width:50%">A</td><td>B</td></tr></table>`
	root := layoutTreeFor(t, src, 400, nil)
	var first *Fragment
	var walk func(f *Fragment)
	walk = func(f *Fragment) {
		if first == nil && f.Box != nil && f.Box.Display == cssbox.DisplayTableCell {
			first = f
		}
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(root)
	if first == nil {
		t.Fatal("no cell fragment")
	}
	// 50% of (200 - 30 spacing) = 85 (± a couple px for borders).
	if first.W < 82 || first.W > 88 {
		t.Errorf("fixed %% column W = %.1f, want ~85 (50%% of contentW-spacing); the F6 bug gives ~100", first.W)
	}
}
