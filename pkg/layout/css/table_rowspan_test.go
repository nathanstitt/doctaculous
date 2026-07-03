package css

import (
	"context"
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// rowspanCell builds an auto-width table cell holding a single text run, with the
// given colspan/rowspan. It mirrors the auto-layout cell helpers in
// table_layout_test.go (serif 16pt, width:auto so the column solve sizes it).
func rowspanCell(text string, colSpan, rowSpan int) *cssbox.Box {
	st := gcss.ComputedStyle{FontSizePt: 16, FontFamily: "serif",
		Width: gcss.Length{Unit: gcss.UnitAuto}, MaxWidth: gcss.Length{Unit: gcss.UnitAuto}}
	txt := &cssbox.Box{Kind: cssbox.BoxText, Text: text, Display: cssbox.DisplayInline, Style: st}
	return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableCell,
		Formatting: cssbox.InlineFC, Style: st, ColSpan: colSpan, RowSpan: rowSpan,
		Children: []*cssbox.Box{txt}}
}

// TestRowspanCellColumnGetsWidth guards the auto column-width solve for a table that
// combines a rowspan cell in column 0 with a colspan cell below it — the shape a DOCX
// table with vMerge + gridSpan lowers to. It locks in that the rowspan cell's column
// receives a real, content-sized width (it does NOT collapse to ~zero) and that the
// columns do not overlap. The grid is:
//
//	row 0: [ "Merged" (rowspan 2) | "B" | "C" ]
//	row 1: [   (covered by span)  | "Spans two columns" (colspan 2) ]
//
// This case previously had no coverage (the existing table tests exercise colspan and
// fixed-width layout, but not a rowspan origin cell's column width under auto layout).
func TestRowspanCellColumnGetsWidth(t *testing.T) {
	mk := func() *cssbox.Box {
		row0 := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRow, Formatting: cssbox.TableFC,
			Children: []*cssbox.Box{rowspanCell("Merged", 1, 2), rowspanCell("B", 1, 1), rowspanCell("C", 1, 1)}}
		row1 := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRow, Formatting: cssbox.TableFC,
			Children: []*cssbox.Box{rowspanCell("Spans two columns", 2, 1)}}
		rg := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRowGroup, Formatting: cssbox.TableFC,
			Children: []*cssbox.Box{row0, row1}}
		st := gcss.ComputedStyle{TableLayout: "auto", BorderCollapse: "collapse",
			Width: gcss.Length{Value: 600, Unit: gcss.UnitPx}, MaxWidth: gcss.Length{Unit: gcss.UnitAuto}}
		return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTable, Formatting: cssbox.TableFC,
			Style: st, Children: []*cssbox.Box{rg}}
	}
	e := New(nil, nil, nil)
	body := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC,
		Children: []*cssbox.Box{mk()}}
	frag := e.layoutTree(context.Background(), body, 800)
	if frag == nil {
		t.Fatal("nil fragment")
	}

	// Collect cell fragments by their text content (the leading glyph run's runes).
	cells := map[string]*Fragment{}
	var walk func(f *Fragment)
	walk = func(f *Fragment) {
		if f == nil {
			return
		}
		if txt := fragmentText(f); txt != "" {
			if _, seen := cells[txt]; !seen {
				cells[txt] = f
			}
		}
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(frag)

	merged := cells["Merged"]
	b := cells["B"]
	if merged == nil || b == nil {
		t.Fatalf("missing cell fragments: Merged=%v B=%v (found %d text fragments)", merged, b, len(cells))
	}

	// The rowspan cell's column must have a real width. "Merged" in serif 16pt is well
	// over 30px of min-content; a collapsed column would be near 0.
	if merged.W < 30 {
		t.Errorf("rowspan cell 'Merged' column collapsed: W=%.1f, want >= 30", merged.W)
	}
	// Column 0's right edge must not overrun column 1's left edge (no overlap).
	if merged.X+merged.W > b.X+0.5 {
		t.Errorf("column 0 overlaps column 1: Merged right edge = %.1f, B left edge = %.1f",
			merged.X+merged.W, b.X)
	}
	// Sanity: B sits to the right of Merged (distinct columns).
	if b.X <= merged.X {
		t.Errorf("B (col 1) not right of Merged (col 0): Merged.X=%.1f B.X=%.1f", merged.X, b.X)
	}
}

// fragmentText returns the concatenated runes of a fragment's own line glyphs (not
// descendants), or "" if it has none — used to identify a cell by its text.
func fragmentText(f *Fragment) string {
	var out []rune
	for i := range f.Lines {
		for _, g := range f.Lines[i].Glyphs {
			out = append(out, g.Runes...)
		}
	}
	return string(out)
}
