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
		st := gcss.ComputedStyle{TableLayout: "fixed", BorderCollapse: "separate"}
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
		if f.W == 50 && f.H == 30 {
			cells = append(cells, f)
		}
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(frag)
	if len(cells) != 4 {
		t.Fatalf("want 4 cell fragments 50x30, got %d", len(cells))
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
